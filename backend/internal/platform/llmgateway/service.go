package llmgateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/google/uuid"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/platform/txcap"
)

// LimitView is the sealed platform admission capability bound by the store to
// one physical transaction.
type LimitView = txcap.LLMLimitView

// Config assembles the gateway. Credentials are resolved from the environment
// at dispatch time; they are never stored in the catalog, database or logs.
type Config struct {
	Catalog          *Catalog
	Providers        map[ProviderKey]Provider
	Credential       func(env string) (string, bool)
	Clock            func() time.Time
	Logger           *slog.Logger
	RequestRetention time.Duration
	DefaultBudget    BudgetPolicy
}

// BudgetPolicy is the server-side ceiling. The photographer's own limit may
// only lower it, never raise it.
type BudgetPolicy struct {
	MonthlyLimitMicros int64
	MonthlyTokenLimit  int64
}

// Service is the only way a server-side caller reaches a model.
type Service struct {
	catalog    *Catalog
	providers  map[ProviderKey]Provider
	credential func(string) (string, bool)
	now        func() time.Time
	logger     *slog.Logger
	retention  time.Duration
	policy     BudgetPolicy
}

// New validates the assembly. Every enabled model must have an adapter.
func New(cfg Config) (*Service, error) {
	if cfg.Catalog == nil {
		return nil, fmt.Errorf("%w: catalog required", ErrValidation)
	}
	if cfg.DefaultBudget.MonthlyLimitMicros <= 0 || cfg.DefaultBudget.MonthlyTokenLimit <= 0 {
		return nil, fmt.Errorf("%w: default budget policy required", ErrValidation)
	}
	s := &Service{
		catalog:    cfg.Catalog,
		providers:  make(map[ProviderKey]Provider, len(cfg.Providers)),
		credential: cfg.Credential,
		now:        cfg.Clock,
		logger:     cfg.Logger,
		retention:  cfg.RequestRetention,
		policy:     cfg.DefaultBudget,
	}
	for key, provider := range cfg.Providers {
		if provider == nil || provider.Key() != key {
			return nil, fmt.Errorf("%w: provider %q mismatch", ErrValidation, key)
		}
		s.providers[key] = provider
	}
	for _, entry := range cfg.Catalog.List() {
		if !entry.Available {
			continue
		}
		model, err := cfg.Catalog.Model(entry.ModelKey)
		if err != nil {
			return nil, err
		}
		if _, ok := s.providers[model.Provider]; !ok {
			return nil, fmt.Errorf("%w: model %q has no adapter", ErrValidation, entry.ModelKey)
		}
		if s.credential == nil {
			s.credential = osCredential
		}
		if _, ok := s.credential(model.CredentialEnv); !ok {
			return nil, fmt.Errorf("%w: model %q credential %s is not set", ErrUnavailable, entry.ModelKey, model.CredentialEnv)
		}
	}
	if s.credential == nil {
		s.credential = osCredential
	}
	if s.now == nil {
		s.now = time.Now
	}
	if s.logger == nil {
		s.logger = slog.Default()
	}
	if s.retention <= 0 {
		s.retention = 90 * 24 * time.Hour
	}
	return s, nil
}

func osCredential(env string) (string, bool) {
	value := os.Getenv(env)
	return value, value != ""
}

// Models lists the selectable directory. Callers only ever choose a model key.
func (s *Service) Models() []CatalogEntry { return s.catalog.List() }

// RequestState is the gateway's own fact about obtaining a complete result.
// It never stands in for a run state.
type RequestState string

const (
	StatePrepared    RequestState = "prepared"
	StateDispatching RequestState = "dispatching"
	StateStreaming   RequestState = "streaming"
	StateSucceeded   RequestState = "succeeded"
	StateFailed      RequestState = "failed"
	StateCancelled   RequestState = "cancelled"
	StateUnknown     RequestState = "unknown"
)

// SettlementState is the reservation's money fact, reported separately from the
// result state so a missing usage field never re-runs a successful call.
type SettlementState string

const (
	SettlementUnclaimed SettlementState = "unclaimed"
	SettlementReserved  SettlementState = "reserved"
	SettlementPartial   SettlementState = "partially_settled"
	SettlementSettled   SettlementState = "settled"
	SettlementUnknown   SettlementState = "unknown"
	SettlementReleased  SettlementState = "released"
)

// RequestView is what a trusted caller may read back.
type RequestView struct {
	ID              string
	CallerService   string
	OperationID     string
	GroupID         string
	ModelKey        string
	RequestHash     string
	State           RequestState
	FailureClass    string
	RetryEligible   bool
	AttemptsUsed    int
	Deadline        time.Time
	CancelRequested bool
	Closed          bool
	Result          *Result
	ResultHash      string
	ResultRevision  int64
	Settlement      SettlementState
	RetainedUntil   time.Time
	Revision        int64
}

// Terminal reports whether the request can never dispatch again.
func (v RequestView) Terminal() bool {
	switch v.State {
	case StateSucceeded, StateFailed, StateCancelled:
		return true
	}
	return false
}

const (
	requestColumns = "id,caller_service,caller_operation_id,caller_group_id,request_hash,model_key," +
		"catalog_version,price_version,model_snapshot,price_snapshot,request_payload,payload_state," +
		"output_limit,deadline_at,state,failure_class,attempts_used,retry_eligible,cancel_requested_at," +
		"closed_at,result_payload,result_hash,result_revision,revision,retained_until"
)

type requestRow struct {
	id             string
	callerService  string
	operationID    string
	groupID        string
	requestHash    string
	modelKey       string
	catalogVersion string
	priceVersion   string
	modelSnapshot  ModelSnapshot
	priceSnapshot  PriceSchedule
	payload        []byte
	payloadState   string
	outputLimit    int
	deadline       time.Time
	state          RequestState
	failureClass   *string
	attemptsUsed   int
	retryEligible  bool
	cancelAt       *time.Time
	closedAt       *time.Time
	resultPayload  []byte
	resultHash     *string
	resultRevision int64
	revision       int64
	retainedUntil  time.Time
}

func scanRequest(row store.Row) (requestRow, error) {
	var (
		r             requestRow
		modelSnapshot []byte
		priceSnapshot []byte
	)
	err := row.Scan(&r.id, &r.callerService, &r.operationID, &r.groupID, &r.requestHash, &r.modelKey,
		&r.catalogVersion, &r.priceVersion, &modelSnapshot, &priceSnapshot, &r.payload, &r.payloadState,
		&r.outputLimit, &r.deadline, &r.state, &r.failureClass, &r.attemptsUsed, &r.retryEligible,
		&r.cancelAt, &r.closedAt, &r.resultPayload, &r.resultHash, &r.resultRevision, &r.revision, &r.retainedUntil)
	if errors.Is(err, store.ErrNoRows) {
		return requestRow{}, ErrNotFound
	}
	if err != nil {
		return requestRow{}, err
	}
	if err := json.Unmarshal(modelSnapshot, &r.modelSnapshot); err != nil {
		return requestRow{}, err
	}
	if err := json.Unmarshal(priceSnapshot, &r.priceSnapshot); err != nil {
		return requestRow{}, err
	}
	return r, nil
}

func (r requestRow) view(settlement SettlementState) RequestView {
	v := RequestView{
		ID:              r.id,
		CallerService:   r.callerService,
		OperationID:     r.operationID,
		GroupID:         r.groupID,
		ModelKey:        r.modelKey,
		RequestHash:     r.requestHash,
		State:           r.state,
		RetryEligible:   r.retryEligible,
		AttemptsUsed:    r.attemptsUsed,
		Deadline:        r.deadline,
		CancelRequested: r.cancelAt != nil,
		Closed:          r.closedAt != nil,
		ResultRevision:  r.resultRevision,
		Settlement:      settlement,
		RetainedUntil:   r.retainedUntil,
		Revision:        r.revision,
	}
	if r.failureClass != nil {
		v.FailureClass = *r.failureClass
	}
	if r.resultHash != nil {
		v.ResultHash = *r.resultHash
	}
	if len(r.resultPayload) > 0 {
		var result storedResult
		if json.Unmarshal(r.resultPayload, &result) == nil {
			decoded := result.toResult()
			v.Result = &decoded
		}
	}
	return v
}

// storedResult is the persisted projection of a complete result. Media bytes
// and credentials never appear here.
type storedResult struct {
	Text              string       `json:"text"`
	ToolCalls         []storedCall `json:"tool_calls,omitempty"`
	FinishReason      string       `json:"finish_reason"`
	Model             string       `json:"model"`
	ProviderRequestID string       `json:"provider_request_id,omitempty"`
	Usage             *storedUsage `json:"usage,omitempty"`
}

type storedCall struct {
	ID        string          `json:"id"`
	Index     int             `json:"index"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type storedUsage struct {
	InputTotalTokens  *int64 `json:"input_total_tokens,omitempty"`
	InputCachedTokens *int64 `json:"input_cached_tokens,omitempty"`
	OutputTokens      *int64 `json:"output_tokens,omitempty"`
	ReasoningTokens   *int64 `json:"reasoning_tokens,omitempty"`
}

func newStoredResult(r Result) storedResult {
	stored := storedResult{
		Text:              r.Text,
		FinishReason:      string(r.FinishReason),
		Model:             r.Model,
		ProviderRequestID: r.ProviderRequestID,
		Usage: &storedUsage{
			InputTotalTokens:  r.Usage.InputTotalTokens,
			InputCachedTokens: r.Usage.InputCachedTokens,
			OutputTokens:      r.Usage.OutputTokens,
			ReasoningTokens:   r.Usage.ReasoningTokens,
		},
	}
	for _, c := range r.ToolCalls {
		stored.ToolCalls = append(stored.ToolCalls, storedCall(c))
	}
	return stored
}

func (s storedResult) toResult() Result {
	result := Result{
		Text:              s.Text,
		FinishReason:      FinishReason(s.FinishReason),
		Model:             s.Model,
		ProviderRequestID: s.ProviderRequestID,
	}
	if s.Usage != nil {
		result.Usage = UsageEvidence{
			InputTotalTokens:  s.Usage.InputTotalTokens,
			InputCachedTokens: s.Usage.InputCachedTokens,
			OutputTokens:      s.Usage.OutputTokens,
			ReasoningTokens:   s.Usage.ReasoningTokens,
		}
	}
	for _, c := range s.ToolCalls {
		result.ToolCalls = append(result.ToolCalls, ToolCall(c))
	}
	return result
}

// rowReader is the read surface both a write transaction and a read snapshot
// offer, so the money state of a request is derived the same way in both.
type rowReader interface {
	QueryRow(ctx context.Context, table, columns, cond string, args ...any) store.Row
}

// settlementOf reports the money state of the reservation that claimed a
// request. A request nothing has claimed yet holds no money.
func settlementOf(ctx context.Context, tx rowReader, requestID string) (SettlementState, error) {
	var state string
	err := tx.QueryRow(ctx, "llm_usage_reservations", "settlement_state", "request_id=$2", requestID).Scan(&state)
	switch {
	case err == nil:
		return SettlementState(state), nil
	case errors.Is(err, store.ErrNoRows):
		return SettlementUnclaimed, nil
	default:
		return "", err
	}
}

// Get reads one request without exposing provider material.
func (s *Service) Get(ctx context.Context, scope store.AccountScope, requestID string) (RequestView, error) {
	var view RequestView
	err := scope.WithReadSnapshot(ctx, func(tx store.ReadTxAccountScope) error {
		row, err := scanRequest(tx.QueryRow(ctx, "llm_requests", requestColumns, "id=$2", requestID))
		if err != nil {
			return err
		}
		settlement, err := settlementOf(ctx, tx, requestID)
		if err != nil {
			return err
		}
		view = row.view(settlement)
		return nil
	})
	return view, err
}

// RequestForBinding resolves the request a caller's own durable step name
// produced. The binding key is the same one Call was given, so a caller that
// persisted a step can find what it bought without storing a gateway id of its
// own — which matters because the id only exists after admission, while the
// step exists before it.
func (s *Service) RequestForBinding(ctx context.Context, scope store.AccountScope, callerService, bindingKey string) (RequestView, error) {
	if callerService == "" || bindingKey == "" || len(bindingKey) > maxBindingKeyLen {
		return RequestView{}, fmt.Errorf("%w: caller binding key", ErrValidation)
	}
	operationID := derivedOperationID(callerService, bindingKey, "request")
	var view RequestView
	err := scope.WithReadSnapshot(ctx, func(tx store.ReadTxAccountScope) error {
		row, err := scanRequest(tx.QueryRow(ctx, "llm_requests", requestColumns,
			"caller_service=$2 AND caller_operation_id=$3", callerService, operationID))
		if err != nil {
			return err
		}
		settlement, err := settlementOf(ctx, tx, row.id)
		if err != nil {
			return err
		}
		view = row.view(settlement)
		return nil
	})
	return view, err
}

// CancelInTx stops a request. A prepared request ends atomically; a dispatched
// or unknown one only records the intent, because the provider may already have
// accepted and billed the call.
func (s *Service) CancelInTx(ctx context.Context, tx store.TxAccountScope, requestID string) (RequestView, error) {
	now := s.now().UTC()
	// Cancelling a prepared request gives its hold back, so the month bucket is
	// locked before the request row.
	if err := s.lockBudgetForRequest(ctx, tx, requestID, now); err != nil {
		return RequestView{}, err
	}
	row, err := scanRequest(tx.QueryRowForUpdate(ctx, "llm_requests", requestColumns, "id=$2", requestID))
	if err != nil {
		return RequestView{}, err
	}
	switch row.state {
	case StatePrepared:
		if _, err := tx.Update(ctx, "llm_requests",
			"state='cancelled',cancel_requested_at=COALESCE(cancel_requested_at,$3),closed_at=$3,revision=revision+1,updated_at=$3",
			"id=$2 AND revision=$4", requestID, now, row.revision); err != nil {
			return RequestView{}, err
		}
		if err := s.releaseReservation(ctx, tx, requestID, now); err != nil {
			return RequestView{}, err
		}
		row.state, row.revision = StateCancelled, row.revision+1
	case StateSucceeded, StateFailed, StateCancelled:
		return row.view(SettlementUnclaimed), fmt.Errorf("%w: request already terminal", ErrState)
	default:
		// Dispatched or unknown: record the intent, keep the money facts open.
		if _, err := tx.Update(ctx, "llm_requests",
			"cancel_requested_at=COALESCE(cancel_requested_at,$3),revision=revision+1,updated_at=$3",
			"id=$2 AND revision=$4", requestID, now, row.revision); err != nil {
			return RequestView{}, err
		}
		row.revision++
	}
	return row.view(SettlementUnclaimed), nil
}

// ConsumeInTx hands one complete result to the caller's own persistence and
// atomically releases that consumer's retention claim. A repeat consumption
// replays the same result instead of producing a second set of tool steps.
func (s *Service) ConsumeInTx(
	ctx context.Context,
	tx store.TxAccountScope,
	requestID, callerService, consumerKey string,
	save func(Result) error,
) (Result, bool, error) {
	row, err := scanRequest(tx.QueryRowForUpdate(ctx, "llm_requests", requestColumns, "id=$2", requestID))
	if err != nil {
		return Result{}, false, err
	}
	if row.state != StateSucceeded || len(row.resultPayload) == 0 {
		return Result{}, false, fmt.Errorf("%w: request has no complete result", ErrState)
	}
	var stored storedResult
	if err := json.Unmarshal(row.resultPayload, &stored); err != nil {
		return Result{}, false, err
	}
	result := stored.toResult()

	var state string
	err = tx.QueryRowForUpdate(ctx, "llm_result_consumers", "state",
		"request_id=$2 AND caller_service=$3 AND consumer_key=$4", requestID, callerService, consumerKey).Scan(&state)
	if errors.Is(err, store.ErrNoRows) {
		return Result{}, false, fmt.Errorf("%w: consumer not registered", ErrState)
	}
	if err != nil {
		return Result{}, false, err
	}
	if state == "consumed" {
		return result, true, nil
	}
	if state == "abandoned" {
		return Result{}, false, fmt.Errorf("%w: consumer abandoned", ErrState)
	}
	if save != nil {
		if err := save(result); err != nil {
			return Result{}, false, err
		}
	}
	if _, err := tx.Update(ctx, "llm_result_consumers", "state='consumed',released_at=$5",
		"request_id=$2 AND caller_service=$3 AND consumer_key=$4", requestID, callerService, consumerKey, s.now().UTC()); err != nil {
		return Result{}, false, err
	}
	return result, false, nil
}

// AbandonConsumerInTx releases a retention claim when the caller stopped
// waiting. It is the only way a pending claim disappears without consumption.
func (s *Service) AbandonConsumerInTx(ctx context.Context, tx store.TxAccountScope, requestID, callerService, consumerKey string) error {
	affected, err := tx.Update(ctx, "llm_result_consumers", "state='abandoned',released_at=$5",
		"request_id=$2 AND caller_service=$3 AND consumer_key=$4 AND state='pending'",
		requestID, callerService, consumerKey, s.now().UTC())
	if err != nil {
		return err
	}
	if affected == 0 {
		return fmt.Errorf("%w: no pending consumer", ErrState)
	}
	return nil
}

func newID(prefix string) string { return prefix + "_" + uuid.NewString() }

func monthStart(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}
