package llmgateway

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// maxAttempts bounds real provider attempts for one request identity.
const maxAttempts = 2

// finishTimeout bounds the persistence that follows a real attempt. It runs on
// a context detached from the caller, so it needs a limit of its own.
const finishTimeout = 30 * time.Second

// PrepareInput turns a reservation into a fixed request identity. Nothing is
// sent to a provider by preparing.
type PrepareInput struct {
	CallerService     string
	CallerOperationID string
	CallerGroupID     string
	ReservationID     string
	ConsumerKey       string
	Chat              ChatRequest
	Deadline          time.Time
}

// storedChat is the crash-recovery projection of a request. Media appears as a
// pinned reference, never as bytes or a signed URL.
type storedChat struct {
	ContractVersion string              `json:"contract_version"`
	ModelKey        string              `json:"model_key"`
	OutputLimit     int                 `json:"output_limit"`
	ToolChoice      string              `json:"tool_choice,omitempty"`
	ResponseFormat  string              `json:"response_format,omitempty"`
	Temperature     *float64            `json:"temperature,omitempty"`
	Tools           []ToolDefinition    `json:"tools,omitempty"`
	Messages        []storedChatMessage `json:"messages"`
}

type storedChatMessage struct {
	Role       string            `json:"role"`
	ToolCallID string            `json:"tool_call_id,omitempty"`
	Blocks     []storedChatBlock `json:"blocks"`
	ToolCalls  []storedCall      `json:"tool_calls,omitempty"`
}

type storedChatBlock struct {
	Kind       string `json:"kind"`
	Text       string `json:"text,omitempty"`
	RevisionID string `json:"content_revision_id,omitempty"`
	Digest     string `json:"digest,omitempty"`
	MIME       string `json:"mime,omitempty"`
	ByteSize   int64  `json:"byte_size,omitempty"`
}

func newStoredChat(r ChatRequest) storedChat {
	stored := storedChat{
		ContractVersion: r.ContractVersion,
		ModelKey:        r.ModelKey,
		OutputLimit:     r.OutputLimit,
		ToolChoice:      r.ToolChoice,
		ResponseFormat:  r.ResponseFormat,
		Temperature:     r.Temperature,
		Tools:           r.Tools,
	}
	for _, m := range r.Messages {
		message := storedChatMessage{Role: string(m.Role), ToolCallID: m.ToolCallID}
		for _, b := range m.Blocks {
			block := storedChatBlock{Kind: string(b.Kind), Text: b.Text}
			if b.Image != nil {
				block.RevisionID, block.Digest = b.Image.RevisionID, b.Image.Digest
				block.MIME, block.ByteSize = b.Image.MIME, b.Image.ByteSize
			}
			message.Blocks = append(message.Blocks, block)
		}
		for _, c := range m.ToolCalls {
			message.ToolCalls = append(message.ToolCalls, storedCall(c))
		}
		stored.Messages = append(stored.Messages, message)
	}
	return stored
}

// PrepareInTx fixes the request identity, the model and price snapshot and the
// caller's retention claim. The initial reservation can be claimed only once.
func (s *Service) PrepareInTx(ctx context.Context, tx store.TxAccountScope, in PrepareInput) (RequestView, error) {
	if err := validateCallerIdentity(in.CallerService, in.CallerOperationID, in.CallerGroupID); err != nil {
		return RequestView{}, err
	}
	if in.ConsumerKey == "" {
		return RequestView{}, fmt.Errorf("%w: consumer key required", ErrValidation)
	}
	if err := in.Chat.Validate(); err != nil {
		return RequestView{}, err
	}
	model, err := s.catalog.Model(in.Chat.ModelKey)
	if err != nil {
		return RequestView{}, err
	}
	if err := CheckCapability(in.Chat, model); err != nil {
		return RequestView{}, err
	}
	snapshot := s.catalog.Snapshot(model)
	hash, err := RequestHash(in.Chat, snapshot)
	if err != nil {
		return RequestView{}, err
	}

	existing, err := scanRequest(tx.QueryRow(ctx, "llm_requests", requestColumns,
		"caller_service=$2 AND caller_operation_id=$3", in.CallerService, in.CallerOperationID))
	switch {
	case err == nil:
		if existing.requestHash != hash {
			return RequestView{}, fmt.Errorf("%w: operation reused with different content", ErrConflict)
		}
		// A replay reports the money state the reservation actually reached;
		// claiming "reserved" would hide a released or settled hold.
		settlement, err := settlementOf(ctx, tx, existing.id)
		if err != nil {
			return RequestView{}, err
		}
		return existing.view(settlement), nil
	case errors.Is(err, ErrNotFound):
	default:
		return RequestView{}, err
	}

	now := s.now().UTC()
	if !in.Deadline.After(now) {
		return RequestView{}, fmt.Errorf("%w: deadline already passed", ErrDeadline)
	}
	reservation, err := scanReservation(tx.QueryRowForUpdate(ctx, "llm_usage_reservations", reservationColumns, "id=$2", in.ReservationID))
	if err != nil {
		return RequestView{}, err
	}
	if reservation.priceVersion != model.Price.Version {
		return RequestView{}, fmt.Errorf("%w: reservation price version differs from the model", ErrConflict)
	}
	if reservation.callerService != in.CallerService || reservation.groupID != in.CallerGroupID {
		return RequestView{}, fmt.Errorf("%w: reservation belongs to another caller", ErrConflict)
	}
	// A request may never be larger than what was admitted: growing it has to
	// go through a new admission, not through a hold that no longer covers it.
	bound := model.Price.UpperBound()
	required := CostMicros(EstimateInputTokens(in.Chat), bound.InputUncached) +
		CostMicros(int64(in.Chat.OutputLimit), bound.Output)
	if required > reservation.initialMicros {
		return RequestView{}, fmt.Errorf("%w: request exceeds the reserved upper bound", ErrBudget)
	}

	id := newID("llmr")
	payload, err := json.Marshal(newStoredChat(in.Chat))
	if err != nil {
		return RequestView{}, err
	}
	modelJSON, err := json.Marshal(snapshot)
	if err != nil {
		return RequestView{}, err
	}
	priceJSON, err := json.Marshal(model.Price)
	if err != nil {
		return RequestView{}, err
	}
	if err := tx.Insert(ctx, "llm_requests",
		[]string{"id", "caller_service", "caller_operation_id", "caller_group_id", "request_hash", "model_key",
			"catalog_version", "price_version", "model_snapshot", "price_snapshot", "request_payload",
			"output_limit", "deadline_at", "state", "created_at", "updated_at", "retained_until"},
		id, in.CallerService, in.CallerOperationID, in.CallerGroupID, hash, model.ModelKey,
		snapshot.CatalogVersion, model.Price.Version, modelJSON, priceJSON, payload,
		in.Chat.OutputLimit, in.Deadline.UTC(), string(StatePrepared), now, now, now.Add(s.retention)); err != nil {
		return RequestView{}, err
	}

	// The initial reservation may be claimed by exactly one request.
	claimed, err := tx.Update(ctx, "llm_usage_reservations",
		"request_id=$3,settlement_state=$4,revision=revision+1,updated_at=$5",
		"id=$2 AND request_id IS NULL AND settlement_state=$6",
		reservation.id, id, string(SettlementReserved), now, string(SettlementUnclaimed))
	if err != nil {
		return RequestView{}, err
	}
	if claimed == 0 {
		return RequestView{}, fmt.Errorf("%w: reservation already claimed", ErrConflict)
	}
	if err := tx.Insert(ctx, "llm_result_consumers",
		[]string{"request_id", "caller_service", "consumer_key", "state", "registered_at"},
		id, in.CallerService, in.ConsumerKey, "pending", now); err != nil {
		return RequestView{}, err
	}

	return RequestView{
		ID:            id,
		CallerService: in.CallerService,
		OperationID:   in.CallerOperationID,
		GroupID:       in.CallerGroupID,
		ModelKey:      model.ModelKey,
		RequestHash:   hash,
		State:         StatePrepared,
		Deadline:      in.Deadline.UTC(),
		Settlement:    SettlementReserved,
		RetainedUntil: now.Add(s.retention),
		Revision:      1,
	}, nil
}

// Permit is a one-shot dispatch capability. It only becomes real when the
// transaction that created it commits, and it can never be claimed twice.
type Permit struct {
	RequestID   string
	AttemptID   string
	AttemptNo   int
	Token       string
	Digest      string
	Snapshot    ModelSnapshot
	Price       PriceSchedule
	RequestHash string
	Deadline    time.Time
	LimitKey    string
	DispatchAt  time.Time
}

// BeginDispatchInTx records the single dispatch intent together with the
// provider admission permit. The caller must already have completed its own
// business, consent and purpose checks.
func (s *Service) BeginDispatchInTx(ctx context.Context, tx store.TxAccountScope, limits LimitView, requestID string) (Permit, error) {
	if limits == nil {
		return Permit{}, fmt.Errorf("%w: platform admission view required", ErrValidation)
	}
	// Global lock order: the shared provider admission record comes before any
	// account-scoped row. The model key is immutable on a request, so reading
	// it without a lock first is safe.
	peek, err := scanRequest(tx.QueryRow(ctx, "llm_requests", requestColumns, "id=$2", requestID))
	if err != nil {
		return Permit{}, err
	}
	model, err := s.catalog.Model(peek.modelKey)
	if err != nil {
		return Permit{}, err
	}
	now := s.now().UTC()
	granted, err := limits.Acquire(ctx, model.LimitKey, model.Concurrency, model.RateLimitPerMin, 60, now)
	if err != nil {
		return Permit{}, err
	}
	if !granted {
		return Permit{}, fmt.Errorf("%w: %s", ErrRateLimited, model.LimitKey)
	}

	request, err := scanRequest(tx.QueryRowForUpdate(ctx, "llm_requests", requestColumns, "id=$2", requestID))
	if err != nil {
		return Permit{}, err
	}
	if request.state != StatePrepared {
		return Permit{}, fmt.Errorf("%w: request state %s cannot dispatch", ErrState, request.state)
	}
	if request.cancelAt != nil || request.closedAt != nil {
		return Permit{}, ErrCancelled
	}
	if !request.deadline.After(now) {
		return Permit{}, ErrDeadline
	}
	if request.attemptsUsed >= maxAttempts {
		return Permit{}, ErrAttemptsUsed
	}
	if err := s.assertNoLiveAttempt(ctx, tx, requestID); err != nil {
		return Permit{}, err
	}
	reservation, err := scanReservation(tx.QueryRowForUpdate(ctx, "llm_usage_reservations", reservationColumns, "request_id=$2", requestID))
	if err != nil {
		return Permit{}, err
	}
	if reservation.state != SettlementReserved || reservation.holdMicros <= 0 {
		return Permit{}, fmt.Errorf("%w: reservation holds no budget", ErrBudget)
	}
	token, err := randomToken()
	if err != nil {
		return Permit{}, err
	}
	permit := Permit{
		RequestID:   requestID,
		AttemptID:   newID("llma"),
		AttemptNo:   request.attemptsUsed + 1,
		Token:       token,
		Digest:      digest([]byte(token)),
		Snapshot:    request.modelSnapshot,
		Price:       request.priceSnapshot,
		RequestHash: request.requestHash,
		Deadline:    request.deadline,
		LimitKey:    model.LimitKey,
		DispatchAt:  now,
	}
	if err := tx.Insert(ctx, "llm_attempts",
		[]string{"id", "request_id", "attempt_number", "reservation_id", "reservation_generation",
			"dispatch_state", "dispatch_epoch", "permit_digest", "limit_key", "dispatched_at", "created_at", "updated_at"},
		permit.AttemptID, requestID, permit.AttemptNo, reservation.id, reservation.generation,
		"dispatching", reservation.generation, permit.Digest, model.LimitKey, now, now, now); err != nil {
		return Permit{}, err
	}
	if _, err := tx.Update(ctx, "llm_requests",
		"state=$3,attempts_used=attempts_used+1,retry_eligible=FALSE,revision=revision+1,updated_at=$4",
		"id=$2 AND revision=$5", requestID, string(StateDispatching), now, request.revision); err != nil {
		return Permit{}, err
	}
	return permit, nil
}

// RedispatchInTx re-admits a request the gateway proved was never accepted and
// takes its dispatch permit in the same transaction.
//
// Re-arming in a transaction of its own is never correct. Between the two
// commits the reservation is held again while the request still says it is
// waiting to be re-admitted, so a crash — or simply a dispatch the shared
// provider limit refuses — strands the identity: every later attempt fails
// RearmInTx with "reservation is not released for retry" and the hold stays
// occupied until it expires.
func (s *Service) RedispatchInTx(
	ctx context.Context,
	tx store.TxAccountScope,
	limits func(store.TxAccountScope) LimitView,
	requestID string,
	expectedGeneration int64,
	accountLimitMicros, accountTokenLimit int64,
) (Permit, Reservation, error) {
	if limits == nil {
		return Permit{}, Reservation{}, fmt.Errorf("%w: platform admission view required", ErrValidation)
	}
	// Global lock order: the shared provider record comes before the group and
	// budget rows re-admission is about to take. The model key is immutable on a
	// request, so the unlocked read that resolves the limit key is safe.
	peek, err := scanRequest(tx.QueryRow(ctx, "llm_requests", requestColumns, "id=$2", requestID))
	if err != nil {
		return Permit{}, Reservation{}, err
	}
	model, err := s.catalog.Model(peek.modelKey)
	if err != nil {
		return Permit{}, Reservation{}, err
	}
	// The row exists: this request has dispatched at least once already, which is
	// what made it re-admissible. Without it lockSharedAdmission would silently do
	// nothing and the Acquire below would create the row after the level 2 and 4
	// locks — exactly the inversion this ordering exists to prevent.
	if err := lockSharedAdmission(ctx, tx, limits, model.LimitKey); err != nil {
		return Permit{}, Reservation{}, err
	}
	reservation, err := s.RearmInTx(ctx, tx, requestID, expectedGeneration, accountLimitMicros, accountTokenLimit)
	if err != nil {
		return Permit{}, Reservation{}, err
	}
	permit, err := s.BeginDispatchInTx(ctx, tx, limits(tx), requestID)
	if err != nil {
		return Permit{}, Reservation{}, err
	}
	return permit, reservation, nil
}

// Execute performs the network call outside any transaction and then persists
// exactly what the attempt proved. A permit is consumed at most once; after a
// restart the outcome is verified instead of re-sent.
func (s *Service) Execute(
	ctx context.Context,
	scope store.AccountScope,
	limits func(store.TxAccountScope) LimitView,
	permit Permit,
	chat ChatRequest,
	stream bool,
) (Result, error) {
	model, err := s.catalog.Model(chat.ModelKey)
	if err != nil {
		return Result{}, err
	}
	// The hold, the price and the identity were all fixed against the deployment
	// frozen at Prepare. A catalog update between then and now may not move this
	// call onto a different paid model, so the live entry has to still be that
	// deployment; when it is not, nothing is sent.
	if err := sameDeployment(permit.Snapshot, s.catalog.Snapshot(model)); err != nil {
		return Result{}, err
	}
	hash, err := RequestHash(chat, permit.Snapshot)
	if err != nil {
		return Result{}, err
	}
	if hash != permit.RequestHash {
		return Result{}, fmt.Errorf("%w: dispatch payload does not match the prepared request", ErrConflict)
	}
	provider, ok := s.providers[model.Provider]
	if !ok {
		return Result{}, fmt.Errorf("%w: provider %q", ErrUnavailable, model.Provider)
	}
	credential, ok := s.credential(model.CredentialEnv)
	if !ok {
		return Result{}, fmt.Errorf("%w: credential %s missing", ErrUnavailable, model.CredentialEnv)
	}

	claimed, err := s.claimPermit(ctx, scope, permit)
	if err != nil {
		return Result{}, err
	}
	if !claimed {
		return Result{}, fmt.Errorf("%w: dispatch permit already consumed", ErrConflict)
	}

	remaining := min(permit.Deadline.Sub(s.now().UTC()), permit.DispatchAt.Add(transportLifetime).Sub(s.now().UTC()))
	transportCtx, stopTransport := context.WithTimeout(ctx, remaining)
	defer stopTransport()
	if err := transportCtx.Err(); err != nil {
		return Result{}, s.finishFailure(ctx, scope, limits, permit, classifyTransport(err))
	}
	result, callErr := provider.Invoke(transportCtx, ProviderRequest{
		Model:      model,
		Snapshot:   permit.Snapshot,
		Chat:       chat,
		Credential: credential,
		Stream:     stream,
	})
	if callErr != nil {
		return Result{}, s.finishFailure(ctx, scope, limits, permit, callErr)
	}
	if err := s.finishSuccess(ctx, scope, limits, permit, result); err != nil {
		return Result{}, err
	}
	return result, nil
}

// sameDeployment reports whether the live catalog entry is still the deployment
// a request was prepared against. Everything compared here decides where the
// money goes: the wire identity, the protocol and the price version.
func sameDeployment(frozen, live ModelSnapshot) error {
	switch {
	case frozen.CatalogVersion != live.CatalogVersion:
		return fmt.Errorf("%w: request was prepared against catalog %s, this process serves %s",
			ErrConflict, frozen.CatalogVersion, live.CatalogVersion)
	case frozen.DeploymentKey != live.DeploymentKey || frozen.Provider != live.Provider ||
		frozen.RequestModelID != live.RequestModelID || !slices.Equal(frozen.AcceptedModelIDs, live.AcceptedModelIDs):
		return fmt.Errorf("%w: deployment %s/%s no longer describes the prepared request",
			ErrConflict, live.DeploymentKey, live.RequestModelID)
	case frozen.PriceVersion != live.PriceVersion || frozen.Currency != live.Currency:
		return fmt.Errorf("%w: price version moved from %s to %s after the hold was taken",
			ErrConflict, frozen.PriceVersion, live.PriceVersion)
	}
	return nil
}

// finishContext detaches persistence from the caller's own cancellation. A turn
// the caller stopped waiting for still has to record what the attempt proved and
// give the shared provider slot back; on the caller's context both would fail
// and leave the attempt dispatching with the slot taken.
func finishContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), finishTimeout)
}

// claimPermit consumes the one-shot capability. A worker that took over the
// lease, or an old process arriving late, cannot obtain a second claim.
func (s *Service) claimPermit(ctx context.Context, scope store.AccountScope, permit Permit) (bool, error) {
	var claimed bool
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		// The claim proves possession of the one-shot token, not just knowledge
		// of the attempt id.
		affected, err := tx.Update(ctx, "llm_attempts",
			"permit_claimed_at=$3,updated_at=$3",
			"id=$2 AND permit_digest=$4 AND permit_claimed_at IS NULL AND dispatch_state='dispatching' AND dispatched_at>$5",
			permit.AttemptID, s.now().UTC(), digest([]byte(permit.Token)), s.now().UTC().Add(-transportLifetime))
		if err != nil {
			return err
		}
		claimed = affected == 1
		return nil
	})
	return claimed, err
}

func (s *Service) finishSuccess(
	ctx context.Context,
	scope store.AccountScope,
	limits func(store.TxAccountScope) LimitView,
	permit Permit,
	result Result,
) error {
	stored, err := json.Marshal(newStoredResult(result))
	if err != nil {
		return err
	}
	resultHash, err := ResultHash(result)
	if err != nil {
		return err
	}
	ctx, cancel := finishContext(ctx)
	defer cancel()
	now := s.now().UTC()
	// The paid, complete result is committed on its own. Accounting is a
	// separate fact: a settlement problem must never roll back a result the
	// provider already charged for.
	if err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := lockSharedAdmission(ctx, tx, limits, permit.LimitKey); err != nil {
			return err
		}
		if err := s.lockBudgetForRequest(ctx, tx, permit.RequestID, now); err != nil {
			return err
		}
		if err := s.lockFinishingAttempt(ctx, tx, permit); err != nil {
			return err
		}
		if _, err := tx.Update(ctx, "llm_attempts",
			"dispatch_state='succeeded',provider_request_id=$3,finished_at=$4,transport_finished_at=$4,updated_at=$4",
			"id=$2 AND dispatch_state IN ('dispatching','streaming','unknown')",
			permit.AttemptID, nullableText(result.ProviderRequestID), now); err != nil {
			return err
		}
		if _, err := tx.Update(ctx, "llm_requests",
			"state='succeeded',result_payload=$3,result_hash=$4,result_revision=result_revision+1,"+
				"revision=revision+1,updated_at=$5",
			"id=$2 AND state IN ('dispatching','streaming','unknown')",
			permit.RequestID, stored, resultHash, now); err != nil {
			return err
		}
		return s.releasePermit(ctx, tx, limits, permit, now)
	}); err != nil {
		return err
	}

	if err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		return s.settleFromUsage(ctx, tx, permit, result.Usage, now)
	}); err != nil {
		// The hold stays in place and the money question stays open; the known
		// result is already readable and is never re-sent to recover a number.
		s.logger.ErrorContext(ctx, "llm gateway settlement deferred",
			"request_id", permit.RequestID, "attempt_id", permit.AttemptID, "error", err)
		return nil
	}
	return nil
}

func (s *Service) finishFailure(
	ctx context.Context,
	scope store.AccountScope,
	limits func(store.TxAccountScope) LimitView,
	permit Permit,
	callErr error,
) error {
	var providerErr *ProviderError
	if !errors.As(callErr, &providerErr) {
		providerErr = &ProviderError{Outcome: OutcomeUnknown, Class: "adapter", Detail: callErr.Error()}
	}
	// A cancelled turn is the common case here, so this must not run on the
	// caller's context.
	ctx, cancel := finishContext(ctx)
	defer cancel()
	now := s.now().UTC()
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		// Lock order: shared admission record, then the month bucket this
		// failure may have to give a hold back to, then the request itself.
		if err := lockSharedAdmission(ctx, tx, limits, permit.LimitKey); err != nil {
			return err
		}
		if err := s.lockBudgetForRequest(ctx, tx, permit.RequestID, now); err != nil {
			return err
		}
		request, err := scanRequest(tx.QueryRowForUpdate(ctx, "llm_requests", requestColumns, "id=$2", permit.RequestID))
		if err != nil {
			return err
		}
		if err := s.lockFinishingAttempt(ctx, tx, permit); err != nil {
			return err
		}
		attemptState, requestState := "unknown", StateUnknown
		retryEligible := false
		switch providerErr.Outcome {
		case OutcomeUnaccepted:
			// Proven not accepted and not billed: the identity may be re-admitted.
			attemptState = "rejected"
			retryEligible = request.attemptsUsed < maxAttempts && request.cancelAt == nil &&
				request.closedAt == nil && request.deadline.After(now)
			if retryEligible {
				requestState = StatePrepared
			} else {
				requestState = StateFailed
			}
		case OutcomeRejected:
			attemptState, requestState = "rejected", StateFailed
		case OutcomeProtocol:
			// The provider answered outside the contract; it may still have billed.
			attemptState, requestState = "unknown", StateFailed
		}

		if _, err := tx.Update(ctx, "llm_attempts",
			"dispatch_state=$3,error_class=$4,finished_at=$5,transport_finished_at=$5,updated_at=$5",
			"id=$2 AND dispatch_state IN ('dispatching','streaming','unknown')",
			permit.AttemptID, attemptState, providerErr.Class, now); err != nil {
			return err
		}
		if _, err := tx.Update(ctx, "llm_requests",
			"state=$3,failure_class=$4,retry_eligible=$5,revision=revision+1,updated_at=$6",
			"id=$2 AND state IN ('dispatching','streaming','unknown')",
			permit.RequestID, string(requestState), providerErr.Class, retryEligible, now); err != nil {
			return err
		}
		if err := s.releasePermit(ctx, tx, limits, permit, now); err != nil {
			return err
		}
		if attemptState == "rejected" {
			// Zero-cost evidence is recorded, then the hold is released.
			if err := s.recordVerifiedRejection(ctx, tx, permit, providerErr.Class, now); err != nil {
				return err
			}
			if err := s.releaseHoldForRetry(ctx, tx, permit.RequestID, retryEligible, now); err != nil {
				return err
			}
			return nil
		}
		// Unknown keeps the hold and the money question open.
		_, err = tx.Update(ctx, "llm_usage_reservations",
			"settlement_state=$3,revision=revision+1,updated_at=$4",
			"request_id=$2 AND settlement_state<>'released'",
			permit.RequestID, string(SettlementUnknown), now)
		return err
	})
	if err != nil {
		return err
	}
	return callErr
}

// lockSharedAdmission takes the shared provider record before any
// account-scoped row of the same transaction, which is the first level of the
// global lock order. Without it a finishing transaction would take that record
// after the request it already holds, and could deadlock against a dispatching
// one that takes them in the documented order.
func lockSharedAdmission(
	ctx context.Context,
	tx store.TxAccountScope,
	limits func(store.TxAccountScope) LimitView,
	limitKey string,
) error {
	if limits == nil || limitKey == "" {
		return nil
	}
	view := limits(tx)
	if view == nil {
		return nil
	}
	return view.Lock(ctx, limitKey)
}

// releasePermit frees the shared provider slot once local transport ended. It
// never asserts that the provider stopped working on the remote side.
func (s *Service) releasePermit(
	ctx context.Context,
	tx store.TxAccountScope,
	limits func(store.TxAccountScope) LimitView,
	permit Permit,
	now time.Time,
) error {
	affected, err := tx.Update(ctx, "llm_attempts", "permit_released_at=$3,updated_at=$3",
		"id=$2 AND permit_released_at IS NULL", permit.AttemptID, now)
	if err != nil {
		return err
	}
	if affected == 0 || limits == nil {
		return nil
	}
	view := limits(tx)
	if view == nil {
		return nil
	}
	return view.Release(ctx, permit.LimitKey)
}

// releaseHoldForRetry closes the money side of an attempt that was proven not
// to be accepted. Unlike abandoning a wait, this transition is backed by
// zero-cost evidence, so it does mark the reservation released.
func (s *Service) releaseHoldForRetry(ctx context.Context, tx store.TxAccountScope, requestID string, retryEligible bool, now time.Time) error {
	reservation, err := s.lockReservationAfterBudget(ctx, tx, "request_id=$2", requestID)
	if err != nil {
		return err
	}
	return s.releaseHold(ctx, tx, reservation, retryEligible, now)
}

// VerifyUnacceptedInTx applies trusted, sourced evidence that an unknown attempt
// was never accepted and never billed. Deployments without a provider query API
// reach this only through an operator entry, never through a photographer.
func (s *Service) VerifyUnacceptedInTx(ctx context.Context, tx store.TxAccountScope, requestID, evidenceSource string) (RequestView, error) {
	if evidenceSource == "" {
		return RequestView{}, fmt.Errorf("%w: verification needs a source", ErrValidation)
	}
	now := s.now().UTC()
	// The zero-cost evidence below moves the month bucket, so it is locked
	// before the request.
	if err := s.lockBudgetForRequest(ctx, tx, requestID, now); err != nil {
		return RequestView{}, err
	}
	request, err := scanRequest(tx.QueryRowForUpdate(ctx, "llm_requests", requestColumns, "id=$2", requestID))
	if err != nil {
		return RequestView{}, err
	}
	if request.state != StateUnknown {
		return RequestView{}, fmt.Errorf("%w: only unknown requests are verified", ErrState)
	}
	var attemptID string
	err = tx.QueryRowForUpdate(ctx, "llm_attempts", "id",
		"request_id=$2 AND dispatch_state='unknown'", requestID).Scan(&attemptID)
	if errors.Is(err, store.ErrNoRows) {
		return RequestView{}, fmt.Errorf("%w: no unknown attempt", ErrState)
	}
	if err != nil {
		return RequestView{}, err
	}
	if _, err := tx.Update(ctx, "llm_attempts",
		"dispatch_state='rejected',error_class='verified_not_accepted',finished_at=$3,updated_at=$3",
		"id=$2 AND dispatch_state='unknown'", attemptID, now); err != nil {
		return RequestView{}, err
	}
	retryEligible := request.attemptsUsed < maxAttempts && request.cancelAt == nil &&
		request.closedAt == nil && request.deadline.After(now)
	state := StateFailed
	if retryEligible {
		state = StatePrepared
	}
	if _, err := tx.Update(ctx, "llm_requests",
		"state=$3,failure_class='verified_not_accepted',retry_eligible=$4,revision=revision+1,updated_at=$5",
		"id=$2 AND state='unknown'", requestID, string(state), retryEligible, now); err != nil {
		return RequestView{}, err
	}
	if err := s.recordVerifiedRejection(ctx, tx, Permit{AttemptID: attemptID, RequestID: requestID,
		Snapshot: request.modelSnapshot}, "verified_not_accepted:"+evidenceSource, now); err != nil {
		return RequestView{}, err
	}
	if err := s.releaseHoldForRetry(ctx, tx, requestID, retryEligible, now); err != nil {
		return RequestView{}, err
	}
	request.state, request.retryEligible = state, retryEligible
	return request.view(SettlementReleased), nil
}

// CloseInTx records that the caller stopped waiting. It never claims the call
// was free, and the cost question stays open for reconciliation.
func (s *Service) CloseInTx(ctx context.Context, tx store.TxAccountScope, requestID string) error {
	now := s.now().UTC()
	affected, err := tx.Update(ctx, "llm_requests",
		"closed_at=COALESCE(closed_at,$3),revision=revision+1,updated_at=$3",
		"id=$2", requestID, now)
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func randomToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", raw), nil
}

func nullableText(value string) any {
	if value == "" {
		return nil
	}
	return value
}
