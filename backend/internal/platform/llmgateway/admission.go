package llmgateway

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// ReserveInput is a conservative claim on money and tokens made before any
// request exists. The caller's own ceiling may only lower the server policy.
type ReserveInput struct {
	CallerService        string
	CallerOperationID    string
	CallerGroupID        string
	GroupLimitMicros     int64
	GroupTokenLimit      int64
	GroupDeadline        time.Time
	ModelKey             string
	InputTokenUpperBound int64
	OutputLimit          int
	AccountLimitMicros   int64
	AccountTokenLimit    int64
	ExpiresAt            time.Time
}

// Reservation is the money identity a request later claims exactly once.
type Reservation struct {
	ID             string
	BudgetPeriod   time.Time
	Currency       string
	HoldMicros     int64
	HoldTokens     int64
	HoldGeneration int64
	PriceVersion   string
	State          SettlementState
	RequestID      string
	Replayed       bool
}

const reservationColumns = "id,caller_service,caller_operation_id,caller_group_id,group_limit_micros," +
	"group_token_limit,request_id,budget_period,currency,initial_reserved_micros,initial_reserved_tokens," +
	"remaining_hold_micros,remaining_hold_tokens,hold_generation,retry_eligible,actual_micros,actual_tokens," +
	"settlement_state,price_version,expires_at,revision"

type reservationRow struct {
	id            string
	callerService string
	operationID   string
	groupID       string
	groupLimit    int64
	groupTokens   int64
	requestID     *string
	period        time.Time
	currency      string
	initialMicros int64
	initialTokens int64
	holdMicros    int64
	holdTokens    int64
	generation    int64
	retryEligible bool
	actualMicros  *int64
	actualTokens  *int64
	state         SettlementState
	priceVersion  string
	expiresAt     time.Time
	revision      int64
}

func scanReservation(row store.Row) (reservationRow, error) {
	var r reservationRow
	err := row.Scan(&r.id, &r.callerService, &r.operationID, &r.groupID, &r.groupLimit, &r.groupTokens,
		&r.requestID, &r.period, &r.currency, &r.initialMicros, &r.initialTokens, &r.holdMicros, &r.holdTokens,
		&r.generation, &r.retryEligible, &r.actualMicros, &r.actualTokens, &r.state, &r.priceVersion,
		&r.expiresAt, &r.revision)
	if errors.Is(err, store.ErrNoRows) {
		return reservationRow{}, ErrNotFound
	}
	return r, err
}

func (r reservationRow) reservation(replayed bool) Reservation {
	out := Reservation{
		ID:             r.id,
		BudgetPeriod:   r.period,
		Currency:       r.currency,
		HoldMicros:     r.holdMicros,
		HoldTokens:     r.holdTokens,
		HoldGeneration: r.generation,
		PriceVersion:   r.priceVersion,
		State:          r.state,
		Replayed:       replayed,
	}
	if r.requestID != nil {
		out.RequestID = *r.requestID
	}
	return out
}

// ReserveInTx holds a conservative upper bound against the account month bucket
// and the caller group ceiling. A replay of the same caller operation returns
// the original reservation and never occupies budget twice.
func (s *Service) ReserveInTx(ctx context.Context, tx store.TxAccountScope, in ReserveInput) (Reservation, error) {
	if err := validateCallerIdentity(in.CallerService, in.CallerOperationID, in.CallerGroupID); err != nil {
		return Reservation{}, err
	}
	if in.InputTokenUpperBound < 0 || in.OutputLimit <= 0 {
		return Reservation{}, fmt.Errorf("%w: reservation bounds", ErrValidation)
	}
	model, err := s.catalog.Model(in.ModelKey)
	if err != nil {
		return Reservation{}, err
	}
	if in.ExpiresAt.IsZero() || in.GroupDeadline.IsZero() {
		return Reservation{}, fmt.Errorf("%w: reservation deadlines", ErrValidation)
	}

	// Replay wins before any lock is taken so a retry cannot double-book.
	existing, err := scanReservation(tx.QueryRow(ctx, "llm_usage_reservations", reservationColumns,
		"caller_service=$2 AND caller_operation_id=$3", in.CallerService, in.CallerOperationID))
	switch {
	case err == nil:
		return existing.reservation(true), nil
	case errors.Is(err, ErrNotFound):
	default:
		return Reservation{}, err
	}

	now := s.now().UTC()
	bound := model.Price.UpperBound()
	holdMicros := CostMicros(in.InputTokenUpperBound, bound.InputUncached) +
		CostMicros(int64(in.OutputLimit), bound.Output)
	holdTokens := in.InputTokenUpperBound + int64(in.OutputLimit)
	period := monthStart(now)
	currency := s.catalog.Currency()

	group, err := s.lockGroup(ctx, tx, in, currency, now)
	if err != nil {
		return Reservation{}, err
	}
	if err := s.checkGroupCeiling(ctx, tx, in.CallerService, in.CallerGroupID, group, holdMicros, holdTokens); err != nil {
		return Reservation{}, err
	}
	if err := s.applyBudgetDelta(ctx, tx, period, currency, budgetDelta{
		reservedMicros: holdMicros,
		reservedTokens: holdTokens,
	}, in.AccountLimitMicros, in.AccountTokenLimit, now); err != nil {
		return Reservation{}, err
	}

	id := newID("llmv")
	if err := tx.Insert(ctx, "llm_usage_reservations",
		[]string{"id", "caller_service", "caller_operation_id", "caller_group_id", "group_limit_micros",
			"group_token_limit", "budget_period", "currency", "initial_reserved_micros", "initial_reserved_tokens",
			"remaining_hold_micros", "remaining_hold_tokens", "settlement_state", "price_version", "expires_at"},
		id, in.CallerService, in.CallerOperationID, in.CallerGroupID, group.limitMicros, group.tokenLimit,
		period, currency, holdMicros, holdTokens, holdMicros, holdTokens, string(SettlementUnclaimed),
		model.Price.Version, in.ExpiresAt.UTC()); err != nil {
		return Reservation{}, err
	}
	return Reservation{
		ID:             id,
		BudgetPeriod:   period,
		Currency:       currency,
		HoldMicros:     holdMicros,
		HoldTokens:     holdTokens,
		HoldGeneration: 1,
		PriceVersion:   model.Price.Version,
		State:          SettlementUnclaimed,
	}, nil
}

type groupRow struct {
	limitMicros int64
	tokenLimit  int64
	deadline    time.Time
}

// lockGroup serializes every reservation of one caller group, including across
// 月 boundaries, and fixes the ceiling at first use.
func (s *Service) lockGroup(ctx context.Context, tx store.TxAccountScope, in ReserveInput, currency string, now time.Time) (groupRow, error) {
	limitMicros := in.GroupLimitMicros
	if limitMicros <= 0 || limitMicros > s.policy.MonthlyLimitMicros {
		limitMicros = min64(s.policy.MonthlyLimitMicros, maxPositive(in.GroupLimitMicros, s.policy.MonthlyLimitMicros))
	}
	tokenLimit := in.GroupTokenLimit
	if tokenLimit <= 0 || tokenLimit > s.policy.MonthlyTokenLimit {
		tokenLimit = min64(s.policy.MonthlyTokenLimit, maxPositive(in.GroupTokenLimit, s.policy.MonthlyTokenLimit))
	}
	row := tx.QueryRowForUpdate(ctx, "llm_call_groups", "limit_micros,token_limit,deadline_at",
		"caller_service=$2 AND caller_group_id=$3", in.CallerService, in.CallerGroupID)
	var group groupRow
	err := row.Scan(&group.limitMicros, &group.tokenLimit, &group.deadline)
	switch {
	case err == nil:
		return group, nil
	case errors.Is(err, store.ErrNoRows):
	default:
		return groupRow{}, err
	}
	if err := tx.Insert(ctx, "llm_call_groups",
		[]string{"caller_service", "caller_group_id", "limit_micros", "token_limit", "currency", "deadline_at", "created_at", "updated_at"},
		in.CallerService, in.CallerGroupID, limitMicros, tokenLimit, currency, in.GroupDeadline.UTC(), now, now); err != nil {
		return groupRow{}, err
	}
	return groupRow{limitMicros: limitMicros, tokenLimit: tokenLimit, deadline: in.GroupDeadline.UTC()}, nil
}

// checkGroupCeiling aggregates every month of the group from gateway facts, so
// a new month can never reset a run's total ceiling.
func (s *Service) checkGroupCeiling(
	ctx context.Context,
	tx store.TxAccountScope,
	callerService, groupID string,
	group groupRow,
	holdMicros, holdTokens int64,
) error {
	// Held and already booked amounts of every month of this group are summed,
	// so continuing into a new month cannot reset the run's total ceiling.
	var usedMicros, usedTokens int64
	for _, sum := range []struct {
		column string
		target *int64
	}{
		{"remaining_hold_micros", &usedMicros},
		{"actual_micros", &usedMicros},
		{"remaining_hold_tokens", &usedTokens},
		{"actual_tokens", &usedTokens},
	} {
		var value int64
		if err := tx.ScalarAggregate(ctx, "llm_usage_reservations", store.AggregateSum, sum.column,
			"caller_service=$2 AND caller_group_id=$3", callerService, groupID).Scan(&value); err != nil {
			return err
		}
		*sum.target += value
	}
	if usedMicros+holdMicros > group.limitMicros {
		return fmt.Errorf("%w: caller group cost ceiling", ErrBudget)
	}
	if usedTokens+holdTokens > group.tokenLimit {
		return fmt.Errorf("%w: caller group token ceiling", ErrBudget)
	}
	return nil
}

// budgetDelta is one atomic adjustment of the month bucket. spent may exceed
// the limit so real money is always recorded; reserved may not.
type budgetDelta struct {
	reservedMicros int64
	reservedTokens int64
	spentMicros    int64
	spentTokens    int64
}

func (s *Service) applyBudgetDelta(
	ctx context.Context,
	tx store.TxAccountScope,
	period time.Time,
	currency string,
	delta budgetDelta,
	accountLimitMicros, accountTokenLimit int64,
	now time.Time,
) error {
	limitMicros := min64(s.policy.MonthlyLimitMicros, maxPositive(accountLimitMicros, s.policy.MonthlyLimitMicros))
	tokenLimit := min64(s.policy.MonthlyTokenLimit, maxPositive(accountTokenLimit, s.policy.MonthlyTokenLimit))

	var limit, reserved, spent, tokens, reservedTokens, usedTokens int64
	err := tx.QueryRowForUpdate(ctx, "llm_budgets",
		"limit_micros,reserved_micros,spent_micros,token_limit,reserved_tokens,used_tokens",
		"period_start=$2 AND currency=$3", period, currency).
		Scan(&limit, &reserved, &spent, &tokens, &reservedTokens, &usedTokens)
	switch {
	case errors.Is(err, store.ErrNoRows):
		if err := tx.Insert(ctx, "llm_budgets",
			[]string{"period_start", "currency", "limit_micros", "token_limit", "created_at", "updated_at"},
			period, currency, limitMicros, tokenLimit, now, now); err != nil {
			return err
		}
		limit, tokens = limitMicros, tokenLimit
	case err != nil:
		return err
	}

	nextReserved := reserved + delta.reservedMicros
	nextReservedTokens := reservedTokens + delta.reservedTokens
	nextSpent := spent + delta.spentMicros
	nextUsedTokens := usedTokens + delta.spentTokens
	if nextReserved < 0 || nextReservedTokens < 0 || nextSpent < 0 || nextUsedTokens < 0 {
		return fmt.Errorf("%w: budget delta would go negative", ErrConflict)
	}
	// Only new admission is refused. Real spend beyond the limit is still booked.
	if delta.reservedMicros > 0 && nextSpent+nextReserved > limit {
		return fmt.Errorf("%w: monthly cost limit", ErrBudget)
	}
	if delta.reservedTokens > 0 && nextUsedTokens+nextReservedTokens > tokens {
		return fmt.Errorf("%w: monthly token limit", ErrBudget)
	}
	_, err = tx.Update(ctx, "llm_budgets",
		"reserved_micros=$4,reserved_tokens=$5,spent_micros=$6,used_tokens=$7,revision=revision+1,updated_at=$8",
		"period_start=$2 AND currency=$3",
		period, currency, nextReserved, nextReservedTokens, nextSpent, nextUsedTokens, now)
	return err
}

// releaseReservation drops the remaining hold of a request that will never
// dispatch again. It never invents a settled cost.
func (s *Service) releaseReservation(ctx context.Context, tx store.TxAccountScope, requestID string, now time.Time) error {
	row, err := s.lockReservationAfterBudget(ctx, tx, "request_id=$2", requestID)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	// Only a reserved hold is unused money. A settled or still-unknown
	// reservation states a fact about real spend, and abandoning the wait never
	// rewrites that fact as "released".
	if row.state != SettlementReserved {
		return nil
	}
	return s.releaseHold(ctx, tx, row, row.retryEligible, now)
}

// ReleaseReservationInTx drops an unclaimed hold whose turn never reached a
// request. Without it a refused dispatch would leave budget occupied until the
// expiry sweep runs.
func (s *Service) ReleaseReservationInTx(ctx context.Context, tx store.TxAccountScope, reservationID string) error {
	now := s.now().UTC()
	row, err := s.lockReservationAfterBudget(ctx, tx, "id=$2", reservationID)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if row.requestID != nil {
		return fmt.Errorf("%w: a claimed reservation is released through its request", ErrState)
	}
	if row.state != SettlementUnclaimed {
		return nil
	}
	return s.releaseHold(ctx, tx, row, false, now)
}

// releaseHold gives one reservation's remaining hold back to the month bucket.
// What prevents returning the same hold twice is that the row passed in was
// read FOR UPDATE in this transaction: the amount deducted is the amount that
// row holds right now, and it is zeroed immediately. The revision condition is
// an assertion against a stale in-transaction read — it is not the concurrency
// control, so a caller must keep re-reading the row under its lock rather than
// trusting the condition. A future expiry sweep has to come through here (or
// repeat lock → read hold → deduct that value → zero) for the same reason.
func (s *Service) releaseHold(
	ctx context.Context,
	tx store.TxAccountScope,
	row reservationRow,
	retryEligible bool,
	now time.Time,
) error {
	if err := s.applyBudgetDelta(ctx, tx, row.period, row.currency, budgetDelta{
		reservedMicros: -row.holdMicros,
		reservedTokens: -row.holdTokens,
	}, 0, 0, now); err != nil {
		return err
	}
	affected, err := tx.Update(ctx, "llm_usage_reservations",
		"remaining_hold_micros=0,remaining_hold_tokens=0,settlement_state=$3,retry_eligible=$4,"+
			"revision=revision+1,updated_at=$5",
		"id=$2 AND revision=$6", row.id, string(SettlementReleased), retryEligible, now, row.revision)
	if err != nil {
		return err
	}
	if affected == 0 {
		return fmt.Errorf("%w: reservation moved while its hold was released", ErrConflict)
	}
	return nil
}

// RearmInTx rebuilds the hold of a request that was reliably not accepted. It
// re-checks the ceilings at the current moment, so a competitor that took the
// freed budget in between makes this fail instead of overdrawing.
//
// It must commit together with the dispatch intent it re-admits for: on its own
// it leaves a reservation that is held again while the request still says it is
// waiting to be re-admitted, which no later call can resolve. Use
// RedispatchInTx unless the caller is itself composing that one transaction.
func (s *Service) RearmInTx(
	ctx context.Context,
	tx store.TxAccountScope,
	requestID string,
	expectedGeneration int64,
	accountLimitMicros, accountTokenLimit int64,
) (Reservation, error) {
	now := s.now().UTC()
	// Lock order (gateway.md: 再次派发事务先锁分组/原预算桶，再锁 request/reservation).
	// The caller group, period, currency and the original upper bound are all
	// immutable on a reservation, so the bounded pre-read needs no lock; every
	// value that can move is re-read under the locks below. Any failure rolls
	// the whole transaction back, so the re-armed hold never survives alone.
	peek, err := scanReservation(tx.QueryRow(ctx, "llm_usage_reservations", reservationColumns, "request_id=$2", requestID))
	if err != nil {
		return Reservation{}, err
	}
	group := groupRow{limitMicros: peek.groupLimit, tokenLimit: peek.groupTokens}
	row := tx.QueryRowForUpdate(ctx, "llm_call_groups", "limit_micros,token_limit,deadline_at",
		"caller_service=$2 AND caller_group_id=$3", peek.callerService, peek.groupID)
	if err := row.Scan(&group.limitMicros, &group.tokenLimit, &group.deadline); err != nil {
		if errors.Is(err, store.ErrNoRows) {
			// Every reservation creates its group row, so a missing one means the
			// ledger lost it. Continuing would re-admit without the row lock that
			// serializes a group's reservations across months.
			return Reservation{}, fmt.Errorf("%w: caller group %s/%s has no admission row",
				ErrConflict, peek.callerService, peek.groupID)
		}
		return Reservation{}, err
	}
	if err := s.lockBudget(ctx, tx, peek.period, peek.currency); err != nil {
		return Reservation{}, err
	}

	request, err := scanRequest(tx.QueryRowForUpdate(ctx, "llm_requests", requestColumns, "id=$2", requestID))
	if err != nil {
		return Reservation{}, err
	}
	if request.state != StatePrepared || !request.retryEligible {
		return Reservation{}, fmt.Errorf("%w: request is not re-admissible", ErrState)
	}
	if request.cancelAt != nil || request.closedAt != nil {
		return Reservation{}, fmt.Errorf("%w: request cancelled or closed", ErrCancelled)
	}
	if !request.deadline.After(now) {
		return Reservation{}, fmt.Errorf("%w: request deadline passed", ErrDeadline)
	}
	if request.attemptsUsed >= maxAttempts {
		return Reservation{}, ErrAttemptsUsed
	}
	if err := s.assertNoLiveAttempt(ctx, tx, requestID); err != nil {
		return Reservation{}, err
	}

	reservation, err := scanReservation(tx.QueryRowForUpdate(ctx, "llm_usage_reservations", reservationColumns, "request_id=$2", requestID))
	if err != nil {
		return Reservation{}, err
	}
	if reservation.generation != expectedGeneration {
		return Reservation{}, fmt.Errorf("%w: hold generation moved", ErrConflict)
	}
	if reservation.state != SettlementReleased || !reservation.retryEligible {
		return Reservation{}, fmt.Errorf("%w: reservation is not released for retry", ErrState)
	}
	// Everything the pre-read decided — which group row was locked, which bucket
	// was locked, and how much may be held — must still be what the locked
	// reservation says.
	if reservation.initialMicros != peek.initialMicros || reservation.initialTokens != peek.initialTokens ||
		!reservation.period.Equal(peek.period) || reservation.currency != peek.currency ||
		reservation.callerService != peek.callerService || reservation.groupID != peek.groupID {
		return Reservation{}, fmt.Errorf("%w: reservation bound moved while re-admitting", ErrConflict)
	}

	// Both ceilings are re-checked at this moment, on rows already locked above:
	// a competitor that took the freed budget in between makes this fail instead
	// of overdrawing. The retry keeps the original period, currency and price; it
	// never moves an existing hold into a cheaper month.
	if err := s.checkGroupCeiling(ctx, tx, reservation.callerService, reservation.groupID, group,
		reservation.initialMicros, reservation.initialTokens); err != nil {
		return Reservation{}, err
	}
	if err := s.applyBudgetDelta(ctx, tx, reservation.period, reservation.currency, budgetDelta{
		reservedMicros: reservation.initialMicros,
		reservedTokens: reservation.initialTokens,
	}, accountLimitMicros, accountTokenLimit, now); err != nil {
		return Reservation{}, err
	}
	generation := reservation.generation + 1
	if _, err := tx.Update(ctx, "llm_usage_reservations",
		"remaining_hold_micros=$3,remaining_hold_tokens=$4,hold_generation=$5,retry_eligible=FALSE,"+
			"settlement_state=$6,revision=revision+1,updated_at=$7",
		"id=$2 AND hold_generation=$8",
		reservation.id, reservation.initialMicros, reservation.initialTokens, generation,
		string(SettlementReserved), now, expectedGeneration); err != nil {
		return Reservation{}, err
	}
	reservation.holdMicros, reservation.holdTokens = reservation.initialMicros, reservation.initialTokens
	reservation.generation, reservation.state = generation, SettlementReserved
	return reservation.reservation(false), nil
}

func (s *Service) assertNoLiveAttempt(ctx context.Context, tx store.TxAccountScope, requestID string) error {
	live, err := tx.Exists(ctx, "llm_attempts",
		"request_id=$2 AND dispatch_state IN ('dispatching','streaming','unknown')", requestID)
	if err != nil {
		return err
	}
	if live {
		return fmt.Errorf("%w: an attempt is still live", ErrState)
	}
	return nil
}

func validateCallerIdentity(service, operationID, groupID string) error {
	if service == "" || groupID == "" || len(groupID) > 64 {
		return fmt.Errorf("%w: caller identity", ErrValidation)
	}
	id, err := uuid.Parse(operationID)
	if err != nil || id == uuid.Nil || id.String() != operationID {
		return fmt.Errorf("%w: caller operation id must be a server-generated uuid", ErrValidation)
	}
	return nil
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

// maxPositive keeps a caller-supplied ceiling only when it is a real value.
func maxPositive(value, fallback int64) int64 {
	if value > 0 {
		return value
	}
	return fallback
}
