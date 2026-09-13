package llmgateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// CostComponent is one non-overlapping billable part. Cached input is never
// added to uncached input, and reasoning stays inside output.
type CostComponent string

const (
	ComponentInputCached   CostComponent = "input_cached"
	ComponentInputUncached CostComponent = "input_uncached"
	ComponentOutput        CostComponent = "output"
	ComponentBudgetTokens  CostComponent = "budget_total_tokens"
)

// EvidenceKind ranks how a number was obtained. A provider invoice outranks a
// locally derived number, which outranks an operator statement.
type EvidenceKind string

const (
	EvidenceProviderReported  EvidenceKind = "provider_reported"
	EvidenceDerivedFromUsage  EvidenceKind = "derived_from_usage"
	EvidenceVerifiedRejection EvidenceKind = "verified_rejection"
	EvidenceOperatorAttested  EvidenceKind = "operator_attested"
)

func (k EvidenceKind) rank() int {
	switch k {
	case EvidenceProviderReported:
		return 10
	case EvidenceVerifiedRejection:
		return 8
	case EvidenceDerivedFromUsage:
		return 5
	case EvidenceOperatorAttested:
		return 3
	default:
		return 1
	}
}

// Measurement is one immutable piece of usage or cost evidence.
type Measurement struct {
	AttemptID      string
	Key            string
	Component      CostComponent
	Currency       string
	Dimensions     map[string]any
	Price          Rate
	CostMicros     *int64
	TokenCount     *int64
	Certainty      string
	Kind           EvidenceKind
	ProviderRev    string
	EvidenceDigest string
	// Supersedes names the evidence this correction replaces. When it does not
	// match what is currently booked, the correction is parked instead of
	// silently rolling the position back.
	Supersedes string
}

// SettlementReceipt is the replayable proof of exactly one ledger move.
type SettlementReceipt struct {
	ID               string
	AttemptID        string
	PositionKind     string
	PositionKey      string
	PositionRevision int64
	OldBooked        int64
	NewBooked        int64
	OldHold          int64
	NewHold          int64
	Applied          bool
}

// settleFromUsage converts a real answer's usage into disjoint components at
// the rate of the dispatch moment. A dimension the provider did not report
// stays unknown; it is never treated as zero.
func (s *Service) settleFromUsage(ctx context.Context, tx store.TxAccountScope, permit Permit, usage UsageEvidence, now time.Time) error {
	rate := permit.Price.RateAt(permit.DispatchAt)
	currency := permit.Price.Currency
	measurements := make([]Measurement, 0, 4)

	cached, cachedKnown := int64(0), false
	switch {
	case usage.InputCachedTokens != nil:
		cached, cachedKnown = *usage.InputCachedTokens, true
	case rate.InputCached == rate.InputUncached:
		// The deployment prices cache hits identically, so the split cannot
		// change the amount owed.
		cachedKnown = true
	}
	// An inconsistent input split is not billable evidence. The components that
	// are known are still booked; the suspect part simply keeps its hold.
	inputUsable := usage.InputTotalTokens != nil && cachedKnown && cached <= *usage.InputTotalTokens
	if usage.InputTotalTokens != nil && cachedKnown && !inputUsable {
		s.logger.ErrorContext(ctx, "llm gateway refused an inconsistent input split",
			"request_id", permit.RequestID, "attempt_id", permit.AttemptID,
			"input_total_tokens", *usage.InputTotalTokens, "input_cached_tokens", cached)
	}
	if inputUsable {
		total := *usage.InputTotalTokens
		measurements = append(measurements,
			s.measurement(permit, ComponentInputCached, currency, rate.InputCached, cached, usage, now),
			s.measurement(permit, ComponentInputUncached, currency, rate.InputUncached, total-cached, usage, now))
	}
	if usage.OutputTokens != nil {
		measurements = append(measurements,
			s.measurement(permit, ComponentOutput, currency, rate.Output, *usage.OutputTokens, usage, now))
	}
	// The token dimension never uses the cache split, so a split the provider
	// reported inconsistently — or did not report at all — only leaves the cost
	// question open. Booking zero tokens here would hold the account's token
	// budget against a turn whose token count is perfectly well known.
	if usage.InputTotalTokens != nil && usage.OutputTokens != nil {
		tokens := *usage.InputTotalTokens + *usage.OutputTokens
		m := s.measurement(permit, ComponentBudgetTokens, currency, 0, tokens, usage, now)
		m.CostMicros = nil
		m.TokenCount = &tokens
		measurements = append(measurements, m)
	}
	for _, m := range measurements {
		if _, err := s.applyMeasurement(ctx, tx, permit.RequestID, m, now); err != nil {
			return err
		}
	}
	return s.refreshSettlement(ctx, tx, permit.RequestID, now)
}

func (s *Service) measurement(
	permit Permit,
	component CostComponent,
	currency string,
	ratePerMillion, tokens int64,
	usage UsageEvidence,
	now time.Time,
) Measurement {
	cost := CostMicros(tokens, ratePerMillion)
	count := tokens
	dimensions := map[string]any{
		"tokens":        tokens,
		"dispatched_at": permit.DispatchAt.UTC().Format(time.RFC3339),
		"price_version": permit.Price.Version,
	}
	payload, _ := json.Marshal(struct {
		Attempt   string `json:"attempt_id"`
		Component string `json:"component"`
		Tokens    int64  `json:"tokens"`
		Rate      int64  `json:"rate_per_million"`
	}{permit.AttemptID, string(component), tokens, ratePerMillion})
	// The snapshot records the rate against the component it actually prices.
	price := Rate{}
	switch component {
	case ComponentInputCached:
		price.InputCached = ratePerMillion
	case ComponentInputUncached:
		price.InputUncached = ratePerMillion
	case ComponentOutput:
		price.Output = ratePerMillion
	}
	return Measurement{
		AttemptID:      permit.AttemptID,
		Key:            string(component) + ":" + permit.Price.Version,
		Component:      component,
		Currency:       currency,
		Dimensions:     dimensions,
		Price:          price,
		CostMicros:     &cost,
		TokenCount:     &count,
		Certainty:      "known",
		Kind:           EvidenceDerivedFromUsage,
		EvidenceDigest: digest(payload),
	}
}

// recordVerifiedRejection stores explicit zero-cost evidence. A rejection is
// never expressed by flipping a settled boolean.
func (s *Service) recordVerifiedRejection(ctx context.Context, tx store.TxAccountScope, permit Permit, reason string, now time.Time) error {
	zero := int64(0)
	payload, _ := json.Marshal(struct {
		Attempt string `json:"attempt_id"`
		Reason  string `json:"reason"`
	}{permit.AttemptID, reason})
	for _, component := range []CostComponent{ComponentInputCached, ComponentInputUncached, ComponentOutput} {
		cost, tokens := zero, zero
		m := Measurement{
			AttemptID:      permit.AttemptID,
			Key:            string(component) + ":verified_rejection",
			Component:      component,
			Currency:       permit.Snapshot.Currency,
			Dimensions:     map[string]any{"reason": reason},
			CostMicros:     &cost,
			TokenCount:     &tokens,
			Certainty:      "known",
			Kind:           EvidenceVerifiedRejection,
			EvidenceDigest: digest(payload),
		}
		if m.Currency == "" {
			m.Currency = s.catalog.Currency()
		}
		if _, err := s.applyMeasurement(ctx, tx, permit.RequestID, m, now); err != nil {
			return err
		}
	}
	return nil
}

// RecordMeasurementInTx is the trusted entry for later evidence such as a
// provider invoice or an operator reconciliation.
func (s *Service) RecordMeasurementInTx(ctx context.Context, tx store.TxAccountScope, requestID string, m Measurement) (SettlementReceipt, error) {
	if m.AttemptID == "" || m.Key == "" || m.Currency == "" {
		return SettlementReceipt{}, fmt.Errorf("%w: measurement identity", ErrValidation)
	}
	switch m.Certainty {
	case "known", "unknown":
	default:
		return SettlementReceipt{}, fmt.Errorf("%w: measurement certainty", ErrValidation)
	}
	if m.Certainty == "known" && m.CostMicros == nil && m.TokenCount == nil {
		return SettlementReceipt{}, fmt.Errorf("%w: known measurement needs a value", ErrValidation)
	}
	if m.Certainty == "unknown" && (m.CostMicros != nil || m.TokenCount != nil) {
		return SettlementReceipt{}, fmt.Errorf("%w: unknown measurement carries no value", ErrValidation)
	}
	// ADR-008: explicit key relations are guarded by the service, not by a
	// database foreign key. Booking an attempt of another request would move
	// money into the wrong budget bucket.
	owned, err := tx.Exists(ctx, "llm_attempts", "id=$2 AND request_id=$3", m.AttemptID, requestID)
	if err != nil {
		return SettlementReceipt{}, err
	}
	if !owned {
		return SettlementReceipt{}, fmt.Errorf("%w: attempt does not belong to this request", ErrConflict)
	}
	now := s.now().UTC()
	// Ordering: the month bucket comes before any request-level row, so a
	// reconciliation transaction cannot deadlock against a re-admission one.
	if err := s.lockBudgetForRequest(ctx, tx, requestID, now); err != nil {
		return SettlementReceipt{}, err
	}
	receipt, err := s.applyMeasurement(ctx, tx, requestID, m, now)
	if err != nil {
		return SettlementReceipt{}, err
	}
	if err := s.refreshSettlement(ctx, tx, requestID, now); err != nil {
		return SettlementReceipt{}, err
	}
	return receipt, nil
}

// applyMeasurement books the difference between the new value and what is
// already booked. Two concurrent corrections of the same position can never
// both advance it; the loser is parked for human reconciliation.
func (s *Service) applyMeasurement(
	ctx context.Context,
	tx store.TxAccountScope,
	requestID string,
	m Measurement,
	now time.Time,
) (SettlementReceipt, error) {
	dimensions, err := json.Marshal(m.Dimensions)
	if err != nil {
		return SettlementReceipt{}, err
	}
	if m.Dimensions == nil {
		dimensions = []byte("{}")
	}
	price, err := json.Marshal(m.Price)
	if err != nil {
		return SettlementReceipt{}, err
	}
	id := newID("llmu")
	row := tx.InsertOnConflictDoNothingReturning(ctx, "llm_usage_measurements",
		[]string{"id", "attempt_id", "measurement_key", "cost_component", "currency", "usage_dimensions",
			"price_snapshot", "cost_micros", "token_count", "certainty", "evidence_kind", "evidence_rank",
			"provider_revision", "supersedes_id", "evidence_digest", "created_at"},
		[]string{"account_id", "attempt_id", "measurement_key"},
		[]string{"id"},
		id, m.AttemptID, m.Key, string(m.Component), m.Currency, dimensions, price,
		m.CostMicros, m.TokenCount, m.Certainty, string(m.Kind), m.Kind.rank(),
		nullableText(m.ProviderRev), nullableText(m.Supersedes), m.EvidenceDigest, now)
	var storedID string
	switch err := row.Scan(&storedID); {
	case err == nil:
	case errors.Is(err, store.ErrNoRows):
		// The same evidence was already recorded; never book it twice.
		return SettlementReceipt{AttemptID: m.AttemptID, PositionKey: string(m.Component)}, nil
	default:
		return SettlementReceipt{}, err
	}
	if err := tx.Insert(ctx, "llm_measurement_dispositions",
		[]string{"measurement_id", "state", "reason", "created_at", "updated_at"},
		storedID, "pending", "", now, now); err != nil {
		return SettlementReceipt{}, err
	}
	if m.Certainty != "known" {
		return SettlementReceipt{ID: storedID, AttemptID: m.AttemptID, PositionKey: string(m.Component)}, nil
	}
	// A request settles in exactly one currency: the one its reservation holds.
	// Evidence in another currency is kept as history and parked, never added to
	// a bucket that means something else.
	mismatch, err := s.currencyMismatch(ctx, tx, requestID, m.Currency)
	if err != nil {
		return SettlementReceipt{}, err
	}
	if mismatch != "" {
		return s.park(ctx, tx, storedID, m, mismatch, now)
	}
	if m.Component == ComponentBudgetTokens {
		return s.applyTokenPosition(ctx, tx, requestID, storedID, m, now)
	}
	return s.applyCostPosition(ctx, tx, requestID, storedID, m, now)
}

func (s *Service) applyCostPosition(
	ctx context.Context,
	tx store.TxAccountScope,
	requestID, measurementID string,
	m Measurement,
	now time.Time,
) (SettlementReceipt, error) {
	// Global lock order: budget bucket before request, reservation and
	// position. The period is read without a lock first because it is fixed
	// for the lifetime of a reservation.
	reservation, err := s.lockReservationAfterBudget(ctx, tx, "request_id=$2", requestID)
	if err != nil {
		return SettlementReceipt{}, err
	}
	var (
		booked   int64
		revision int64
		current  string
	)
	err = tx.QueryRowForUpdate(ctx, "llm_cost_positions", "booked_cost_micros,revision,current_measurement_id",
		"attempt_id=$2 AND cost_component=$3 AND currency=$4", m.AttemptID, string(m.Component), m.Currency).
		Scan(&booked, &revision, &current)
	switch {
	case errors.Is(err, store.ErrNoRows):
		if err := tx.Insert(ctx, "llm_cost_positions",
			[]string{"attempt_id", "cost_component", "currency", "current_measurement_id",
				"booked_cost_micros", "certainty", "created_at", "updated_at"},
			m.AttemptID, string(m.Component), m.Currency, measurementID, *m.CostMicros, "known", now, now); err != nil {
			return SettlementReceipt{}, err
		}
		revision, booked = 0, 0
	case err != nil:
		return SettlementReceipt{}, err
	default:
		outranked, err := s.outrankedByCurrent(ctx, tx, current, m)
		if err != nil {
			return SettlementReceipt{}, err
		}
		if outranked != "" {
			return s.park(ctx, tx, measurementID, m, outranked, now)
		}
		affected, err := tx.Update(ctx, "llm_cost_positions",
			"current_measurement_id=$5,booked_cost_micros=$6,certainty='known',revision=revision+1,updated_at=$7",
			"attempt_id=$2 AND cost_component=$3 AND currency=$4 AND revision=$8",
			m.AttemptID, string(m.Component), m.Currency, measurementID, *m.CostMicros, now, revision)
		if err != nil {
			return SettlementReceipt{}, err
		}
		if affected == 0 {
			// Another correction advanced the position first; park this one.
			return s.park(ctx, tx, measurementID, m, "position advanced concurrently", now)
		}
	}

	delta := *m.CostMicros - booked
	if err := s.applyBudgetDelta(ctx, tx, reservation.period, reservation.currency, budgetDelta{
		spentMicros: delta,
	}, 0, 0, now); err != nil {
		return SettlementReceipt{}, err
	}
	oldHold := reservation.holdMicros
	newHold, err := s.recomputeCostHold(ctx, tx, requestID, m.AttemptID, reservation, now)
	if err != nil {
		return SettlementReceipt{}, err
	}
	if err := s.markApplied(ctx, tx, measurementID, now); err != nil {
		return SettlementReceipt{}, err
	}
	receipt := SettlementReceipt{
		ID:               newID("llmc"),
		AttemptID:        m.AttemptID,
		PositionKind:     "cost",
		PositionKey:      string(m.Component),
		PositionRevision: revision + 1,
		OldBooked:        booked,
		NewBooked:        *m.CostMicros,
		OldHold:          oldHold,
		NewHold:          newHold,
		Applied:          true,
	}
	if err := tx.Insert(ctx, "llm_settlement_receipts",
		[]string{"id", "attempt_id", "position_kind", "position_key", "position_revision", "measurement_id",
			"old_booked_value", "new_booked_value", "old_remaining_hold", "new_remaining_hold",
			"budget_period", "currency", "created_at"},
		receipt.ID, m.AttemptID, "cost", string(m.Component), receipt.PositionRevision, measurementID,
		booked, *m.CostMicros, oldHold, newHold, reservation.period, reservation.currency, now); err != nil {
		return SettlementReceipt{}, err
	}
	return receipt, nil
}

func (s *Service) applyTokenPosition(
	ctx context.Context,
	tx store.TxAccountScope,
	requestID, measurementID string,
	m Measurement,
	now time.Time,
) (SettlementReceipt, error) {
	reservation, err := s.lockReservationAfterBudget(ctx, tx, "request_id=$2", requestID)
	if err != nil {
		return SettlementReceipt{}, err
	}
	var booked, revision int64
	var currentID string
	err = tx.QueryRowForUpdate(ctx, "llm_token_positions", "booked_tokens,revision,current_measurement_id",
		"attempt_id=$2 AND dimension=$3", m.AttemptID, string(ComponentBudgetTokens)).Scan(&booked, &revision, &currentID)
	switch {
	case errors.Is(err, store.ErrNoRows):
		if err := tx.Insert(ctx, "llm_token_positions",
			[]string{"attempt_id", "dimension", "current_measurement_id", "booked_tokens", "certainty", "created_at", "updated_at"},
			m.AttemptID, string(ComponentBudgetTokens), measurementID, *m.TokenCount, "known", now, now); err != nil {
			return SettlementReceipt{}, err
		}
		revision, booked = 0, 0
	case err != nil:
		return SettlementReceipt{}, err
	default:
		outranked, err := s.outrankedByCurrent(ctx, tx, currentID, m)
		if err != nil {
			return SettlementReceipt{}, err
		}
		if outranked != "" {
			return s.park(ctx, tx, measurementID, m, outranked, now)
		}
		affected, err := tx.Update(ctx, "llm_token_positions",
			"current_measurement_id=$4,booked_tokens=$5,certainty='known',revision=revision+1,updated_at=$6",
			"attempt_id=$2 AND dimension=$3 AND revision=$7",
			m.AttemptID, string(ComponentBudgetTokens), measurementID, *m.TokenCount, now, revision)
		if err != nil {
			return SettlementReceipt{}, err
		}
		if affected == 0 {
			return s.park(ctx, tx, measurementID, m, "position advanced concurrently", now)
		}
	}

	delta := *m.TokenCount - booked
	if err := s.applyBudgetDelta(ctx, tx, reservation.period, reservation.currency, budgetDelta{
		spentTokens: delta,
	}, 0, 0, now); err != nil {
		return SettlementReceipt{}, err
	}
	oldHold := reservation.holdTokens
	newHold := int64(0)
	if _, err := tx.Update(ctx, "llm_usage_reservations",
		"remaining_hold_tokens=$3,actual_tokens=$4,revision=revision+1,updated_at=$5",
		"id=$2", reservation.id, newHold, *m.TokenCount, now); err != nil {
		return SettlementReceipt{}, err
	}
	if err := s.applyBudgetDelta(ctx, tx, reservation.period, reservation.currency, budgetDelta{
		reservedTokens: -oldHold,
	}, 0, 0, now); err != nil {
		return SettlementReceipt{}, err
	}
	if err := s.markApplied(ctx, tx, measurementID, now); err != nil {
		return SettlementReceipt{}, err
	}
	receipt := SettlementReceipt{
		ID:               newID("llmc"),
		AttemptID:        m.AttemptID,
		PositionKind:     "token",
		PositionKey:      string(ComponentBudgetTokens),
		PositionRevision: revision + 1,
		OldBooked:        booked,
		NewBooked:        *m.TokenCount,
		OldHold:          oldHold,
		NewHold:          newHold,
		Applied:          true,
	}
	err = tx.Insert(ctx, "llm_settlement_receipts",
		[]string{"id", "attempt_id", "position_kind", "position_key", "position_revision", "measurement_id",
			"old_booked_value", "new_booked_value", "old_remaining_hold", "new_remaining_hold",
			"budget_period", "currency", "created_at"},
		receipt.ID, m.AttemptID, "token", string(ComponentBudgetTokens), receipt.PositionRevision, measurementID,
		booked, *m.TokenCount, oldHold, newHold, reservation.period, reservation.currency, now)
	return receipt, err
}

// lockReservationAfterBudget takes the budget bucket before the reservation, so
// the whole gateway follows one order: group admission, budget, request,
// reservation, attempt, position. The reservation's period and currency are
// immutable, so the bounded pre-read needs no lock.
func (s *Service) lockReservationAfterBudget(
	ctx context.Context,
	tx store.TxAccountScope,
	cond, key string,
) (reservationRow, error) {
	peek, err := scanReservation(tx.QueryRow(ctx, "llm_usage_reservations", reservationColumns, cond, key))
	if err != nil {
		return reservationRow{}, err
	}
	if err := s.lockBudget(ctx, tx, peek.period, peek.currency); err != nil {
		return reservationRow{}, err
	}
	return scanReservation(tx.QueryRowForUpdate(ctx, "llm_usage_reservations", reservationColumns, cond, key))
}

// lockBudgetForRequest takes the month bucket of a request's reservation before
// any request-level row is locked. A request whose reservation does not exist
// yet has no bucket to order against.
func (s *Service) lockBudgetForRequest(ctx context.Context, tx store.TxAccountScope, requestID string, now time.Time) error {
	var period time.Time
	var currency string
	err := tx.QueryRow(ctx, "llm_usage_reservations", "budget_period,currency", "request_id=$2", requestID).
		Scan(&period, &currency)
	if errors.Is(err, store.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.lockBudget(ctx, tx, period, currency)
}

// lockBudget takes the month bucket row lock without changing any value. Every
// reservation creates its own bucket, so a missing bucket means the ledger lost
// a row: inventing an empty one here would hide that and pin the month's limit
// to a policy default instead of the account's.
func (s *Service) lockBudget(ctx context.Context, tx store.TxAccountScope, period time.Time, currency string) error {
	var limit int64
	err := tx.QueryRowForUpdate(ctx, "llm_budgets", "limit_micros", "period_start=$2 AND currency=$3", period, currency).Scan(&limit)
	if errors.Is(err, store.ErrNoRows) {
		return fmt.Errorf("%w: no %s budget bucket for %s", ErrConflict, currency, period.Format("2006-01"))
	}
	return err
}

// currencyMismatch reports why evidence cannot settle against this request. The
// reservation's currency is fixed at admission, so evidence in another currency
// is a reconciliation task, not a ledger move.
func (s *Service) currencyMismatch(ctx context.Context, tx store.TxAccountScope, requestID, currency string) (string, error) {
	var expected string
	err := tx.QueryRow(ctx, "llm_usage_reservations", "currency", "request_id=$2", requestID).Scan(&expected)
	if errors.Is(err, store.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if expected != currency {
		return "evidence currency " + currency + " differs from the reserved " + expected, nil
	}
	return "", nil
}

// outrankedByCurrent reports why new evidence must not replace the booked one.
// A provider invoice is never overwritten by a locally derived number or an
// operator estimate, and stale evidence never rolls a position back.
func (s *Service) outrankedByCurrent(ctx context.Context, tx store.TxAccountScope, currentID string, m Measurement) (string, error) {
	var currentRank int
	var currentRev *string
	err := tx.QueryRow(ctx, "llm_usage_measurements", "evidence_rank,provider_revision", "id=$2", currentID).
		Scan(&currentRank, &currentRev)
	if errors.Is(err, store.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if m.Kind.rank() < currentRank {
		return "lower ranked evidence than the booked one", nil
	}
	if m.Supersedes != "" {
		if m.Supersedes != currentID {
			return "supersedes evidence that is no longer current", nil
		}
		return "", nil
	}
	// Equal rank carries no inherent order: arrival time is not evidence of
	// which fact is newer. Only the provider's own increasing revision, or an
	// explicit correction chain, may move a booked amount.
	if m.Kind.rank() == currentRank && !revisionAdvances(currentRev, m.ProviderRev) {
		return "same ranked evidence without a decidable order", nil
	}
	return "", nil
}

// revisionAdvances reports whether the provider's own revision marker proves the
// new evidence supersedes the booked one. Only a comparable, strictly
// increasing integer counts; any other form is undecidable here and belongs to
// an explicit Supersedes chain instead of a guess about money.
func revisionAdvances(current *string, next string) bool {
	if current == nil || *current == "" || next == "" {
		return false
	}
	booked, errBooked := strconv.ParseInt(*current, 10, 64)
	incoming, errIncoming := strconv.ParseInt(next, 10, 64)
	return errBooked == nil && errIncoming == nil && incoming > booked
}

// park keeps evidence for human reconciliation without touching the ledger.
func (s *Service) park(
	ctx context.Context,
	tx store.TxAccountScope,
	measurementID string,
	m Measurement,
	reason string,
	now time.Time,
) (SettlementReceipt, error) {
	if _, err := tx.Update(ctx, "llm_measurement_dispositions",
		"state='pending',reason=$3,updated_at=$4", "measurement_id=$2", measurementID, reason, now); err != nil {
		return SettlementReceipt{}, err
	}
	return SettlementReceipt{ID: measurementID, AttemptID: m.AttemptID, PositionKey: string(m.Component)}, nil
}

// recomputeCostHold keeps only the upper bound of components that are still
// undecided for the attempt that is settling, so a previous attempt's
// zero-cost rejection evidence can never release the current attempt's hold.
func (s *Service) recomputeCostHold(
	ctx context.Context,
	tx store.TxAccountScope,
	requestID, attemptID string,
	reservation reservationRow,
	now time.Time,
) (int64, error) {
	var outputLimit int
	var priceRaw []byte
	if err := tx.QueryRow(ctx, "llm_requests", "output_limit,price_snapshot", "id=$2", requestID).
		Scan(&outputLimit, &priceRaw); err != nil {
		return 0, err
	}
	var price PriceSchedule
	if err := json.Unmarshal(priceRaw, &price); err != nil {
		return 0, err
	}
	outputBound := CostMicros(int64(outputLimit), price.UpperBound().Output)
	inputBound := reservation.initialMicros - outputBound
	if inputBound < 0 {
		inputBound = 0
	}

	rows, err := tx.Query(ctx, "llm_cost_positions", "attempt_id,cost_component,currency,booked_cost_micros",
		"attempt_id IN (SELECT id FROM llm_attempts WHERE account_id=$1 AND request_id=$2)", requestID)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	// booked is scoped to the settling attempt; total is the request's real
	// spend across every attempt it ever made. Only the reserved currency
	// decides this hold — a position in another currency is not comparable.
	booked := map[string]int64{}
	var total int64
	for rows.Next() {
		var attempt, component, currency string
		var value int64
		if err := rows.Scan(&attempt, &component, &currency, &value); err != nil {
			return 0, err
		}
		if currency != reservation.currency {
			continue
		}
		if attempt == attemptID {
			booked[component] += value
		}
		total += value
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	hold := int64(0)
	_, cachedKnown := booked[string(ComponentInputCached)]
	_, uncachedKnown := booked[string(ComponentInputUncached)]
	if !cachedKnown || !uncachedKnown {
		hold += inputBound
	}
	if _, ok := booked[string(ComponentOutput)]; !ok {
		hold += outputBound
	}
	if hold != reservation.holdMicros {
		if err := s.applyBudgetDelta(ctx, tx, reservation.period, reservation.currency, budgetDelta{
			reservedMicros: hold - reservation.holdMicros,
		}, 0, 0, now); err != nil {
			return 0, err
		}
	}
	if _, err := tx.Update(ctx, "llm_usage_reservations",
		"remaining_hold_micros=$3,actual_micros=$4,revision=revision+1,updated_at=$5",
		"id=$2", reservation.id, hold, total, now); err != nil {
		return 0, err
	}
	return hold, nil
}

func (s *Service) markApplied(ctx context.Context, tx store.TxAccountScope, measurementID string, now time.Time) error {
	_, err := tx.Update(ctx, "llm_measurement_dispositions", "state='applied',updated_at=$3",
		"measurement_id=$2 AND state='pending'", measurementID, now)
	return err
}

// refreshSettlement recomputes the reservation's money state from the facts.
func (s *Service) refreshSettlement(ctx context.Context, tx store.TxAccountScope, requestID string, now time.Time) error {
	reservation, err := scanReservation(tx.QueryRowForUpdate(ctx, "llm_usage_reservations", reservationColumns, "request_id=$2", requestID))
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if reservation.state == SettlementReleased {
		return nil
	}
	state := SettlementPartial
	switch {
	case reservation.holdMicros == 0 && reservation.holdTokens == 0:
		state = SettlementSettled
	case reservation.actualMicros == nil:
		state = SettlementReserved
	}
	unknown, err := tx.Exists(ctx, "llm_attempts",
		"request_id=$2 AND dispatch_state='unknown'", requestID)
	if err != nil {
		return err
	}
	if unknown {
		state = SettlementUnknown
	}
	_, err = tx.Update(ctx, "llm_usage_reservations",
		"settlement_state=$3,revision=revision+1,updated_at=$4", "id=$2", reservation.id, string(state), now)
	return err
}

// UsageReport is the read model for one request's money facts.
type UsageReport struct {
	RequestID    string
	Currency     string
	BookedMicros int64
	BookedTokens int64
	HoldMicros   int64
	HoldTokens   int64
	Settlement   SettlementState
	OpenEvidence int
}

// GetUsage reports booked cost and remaining exposure without the prompt.
func (s *Service) GetUsage(ctx context.Context, scope store.AccountScope, requestID string) (UsageReport, error) {
	report := UsageReport{RequestID: requestID}
	err := scope.WithReadSnapshot(ctx, func(tx store.ReadTxAccountScope) error {
		var (
			currency   string
			hold, held int64
			actual     *int64
			tokens     *int64
			state      string
		)
		err := tx.QueryRow(ctx, "llm_usage_reservations",
			"currency,remaining_hold_micros,remaining_hold_tokens,actual_micros,actual_tokens,settlement_state",
			"request_id=$2", requestID).Scan(&currency, &hold, &held, &actual, &tokens, &state)
		if errors.Is(err, store.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		report.Currency, report.HoldMicros, report.HoldTokens = currency, hold, held
		report.Settlement = SettlementState(state)
		if actual != nil {
			report.BookedMicros = *actual
		}
		if tokens != nil {
			report.BookedTokens = *tokens
		}
		open, err := tx.Count(ctx, "llm_measurement_dispositions",
			"state IN ('pending','disputed') AND measurement_id IN "+
				"(SELECT id FROM llm_usage_measurements WHERE account_id=$1 AND attempt_id IN "+
				"(SELECT id FROM llm_attempts WHERE account_id=$1 AND request_id=$2))", requestID)
		if err != nil {
			return err
		}
		report.OpenEvidence = int(open)
		return nil
	})
	return report, err
}
