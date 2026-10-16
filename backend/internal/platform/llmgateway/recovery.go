package llmgateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// transportLifetime is a fixed protocol bound measured from the committed
// dispatch timestamp, not from a worker's eventual start. Changing it requires
// draining old executors first. Both shipped HTTP adapters honor cancellation.
//
// It bounds one model call, not one turn, and a thinking model spends most of
// a hard step before its first visible byte. On 2026-10-16 the old two-minute
// bound cut 26 Claude agent calls in one evening, every one at exactly 120 s:
// the same request, resent unchanged, once finished in 112.7 s with 4733
// output tokens of which about 500 bytes were visible. A long document written
// as tool arguments takes longer still at the relay's 45-75 tokens a second.
// The cost of a longer bound is recovery: an orphaned slot is reclaimed this
// much later.
const transportLifetime = 15 * time.Minute
const recoveryGrace = time.Minute

// NewRecoveryService constructs the database-only maintenance surface. It cannot
// dispatch and needs no live model directory, so credential removal does not
// prevent reclaiming old capacity.
type RecoveryService struct{ service Service }

func NewRecoveryService() *RecoveryService { return &RecoveryService{service: Service{now: time.Now}} }

// SweepExpiredDispatches reconciles one account page without exposing a model
// dispatch service with incomplete dependencies.
func (r *RecoveryService) SweepExpiredDispatches(ctx context.Context, db *store.Store, after string, batch int) (string, error) {
	return r.service.SweepExpiredDispatches(ctx, db, after, batch)
}

// RecoverExpired fences overdue dispatches before releasing their local slots.
// Expiry proves neither non-delivery nor zero cost: the money hold survives.
// Each account is processed in bounded batches and each attempt in a short tx.
func (s *Service) RecoverExpired(ctx context.Context, scope store.AccountScope, batch int) (int, error) {
	if batch < 1 || batch > 1000 {
		return 0, ErrValidation
	}
	cutoff := s.now().UTC().Add(-transportLifetime - recoveryGrace)
	var ids []string
	err := scope.WithReadSnapshot(ctx, func(tx store.ReadTxAccountScope) error {
		rows, err := tx.QueryPage(ctx, "llm_attempts", "id", "dispatch_state IN ('dispatching','streaming') AND dispatched_at<=$2 AND permit_released_at IS NULL AND request_id IN (SELECT id FROM llm_requests WHERE account_id=$1 AND request_kind='chat')", []store.OrderBy{{Column: "dispatched_at"}, {Column: "id"}}, batch, 0, cutoff)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return err
			}
			ids = append(ids, id)
		}
		return rows.Err()
	})
	if err != nil {
		return 0, err
	}
	count := 0
	for _, id := range ids {
		changed := false
		err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
			var p Permit
			p.AttemptID = id
			if err := tx.QueryRow(ctx, "llm_attempts", "request_id,limit_key", "id=$2", id).Scan(&p.RequestID, &p.LimitKey); err != nil {
				return err
			}
			limits := func(t store.TxAccountScope) LimitView { return t.LLMLimitView() }
			if err := lockSharedAdmission(ctx, tx, limits, p.LimitKey); err != nil {
				return err
			}
			if err := s.lockBudgetForRequest(ctx, tx, p.RequestID, s.now().UTC()); err != nil {
				return err
			}
			request, err := scanRequest(tx.QueryRowForUpdate(ctx, "llm_requests", requestColumns, "id=$2", p.RequestID))
			if err != nil {
				return err
			}
			if request.state != StateDispatching && request.state != StateStreaming {
				return nil
			}
			// This CAS also fences claimPermit: either its claim committed first and
			// its bounded transport has expired, or no executor can ever claim now.
			now := s.now().UTC()
			n, err := tx.Update(ctx, "llm_attempts", "dispatch_state='unknown',error_class='orphaned_dispatch',finished_at=$3,updated_at=$3", "id=$2 AND dispatch_state IN ('dispatching','streaming') AND dispatched_at<=$4 AND permit_released_at IS NULL", id, now, cutoff)
			if err != nil || n == 0 {
				return err
			}
			if _, err = tx.Update(ctx, "llm_requests", "state='unknown',failure_class='orphaned_dispatch',retry_eligible=FALSE,revision=revision+1,updated_at=$3", "id=$2", p.RequestID, now); err != nil {
				return err
			}
			if _, err = tx.Update(ctx, "llm_usage_reservations", "settlement_state='unknown',revision=revision+1,updated_at=$3", "request_id=$2 AND settlement_state<>'released'", p.RequestID, now); err != nil {
				return err
			}
			if err = s.releasePermit(ctx, tx, limits, p, now); err != nil {
				return err
			}
			changed = true
			return nil
		})
		if err != nil {
			return count, err
		}
		if changed {
			count++
		}
	}
	return count, nil
}

// unverifiableAfter is how long after dispatch an attempt whose outcome is still
// unknown gets closed with an estimate. The transport ended long before; the
// window leaves room for a late result or an operator verification first.
const unverifiableAfter = transportLifetime + 8*time.Minute

// estimateMarker ends the measurement keys of an unverifiable estimate.
const estimateMarker = ":unverified_estimate"

// SettleUnverifiable closes chat attempts whose outcome can no longer be
// learned: a dropped stream, or a dispatch fenced as orphaned. Without it the
// money hold — sized for the maximum output — would stay forever. The input is
// booked at its reserved upper bound and the output at zero (owner
// 2026-10-14); a provider bill or a verified rejection outranks the estimate.
func (s *Service) SettleUnverifiable(ctx context.Context, scope store.AccountScope, batch int) (int, error) {
	if batch < 1 || batch > 1000 {
		return 0, ErrValidation
	}
	cutoff := s.now().UTC().Add(-unverifiableAfter)
	type candidate struct{ attempt, request string }
	var found []candidate
	err := scope.WithReadSnapshot(ctx, func(tx store.ReadTxAccountScope) error {
		rows, err := tx.QueryPage(ctx, "llm_attempts", "id,request_id",
			"dispatch_state='unknown' AND dispatched_at<=$2 AND request_id IN (SELECT id FROM llm_requests WHERE account_id=$1 AND request_kind='chat')"+
				" AND NOT EXISTS (SELECT 1 FROM llm_usage_measurements m WHERE m.account_id=$1 AND m.attempt_id=llm_attempts.id AND m.measurement_key=$3)",
			[]store.OrderBy{{Column: "dispatched_at"}, {Column: "id"}}, batch, 0, cutoff, estimateKey(ComponentBudgetTokens))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var c candidate
			if err := rows.Scan(&c.attempt, &c.request); err != nil {
				return err
			}
			found = append(found, c)
		}
		return rows.Err()
	})
	if err != nil {
		return 0, err
	}
	count := 0
	for _, c := range found {
		settled := false
		err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
			var err error
			settled, err = s.settleUnverifiableInTx(ctx, tx, c.request, c.attempt, cutoff)
			return err
		})
		if err != nil {
			return count, err
		}
		if settled {
			count++
		}
	}
	return count, nil
}

func estimateKey(component CostComponent) string { return string(component) + estimateMarker }

func (s *Service) settleUnverifiableInTx(ctx context.Context, tx store.TxAccountScope, requestID, attemptID string, cutoff time.Time) (bool, error) {
	now := s.now().UTC()
	// Same order as every other settlement: month bucket, request, attempt.
	if err := s.lockBudgetForRequest(ctx, tx, requestID, now); err != nil {
		return false, err
	}
	request, err := scanRequest(tx.QueryRowForUpdate(ctx, "llm_requests", requestColumns, "id=$2", requestID))
	if err != nil {
		return false, err
	}
	if request.kind != requestKindChat || request.state != StateUnknown && request.state != StateFailed {
		return false, nil
	}
	var state string
	var dispatched time.Time
	if err = tx.QueryRowForUpdate(ctx, "llm_attempts", "dispatch_state,dispatched_at", "id=$2 AND request_id=$3", attemptID, requestID).
		Scan(&state, &dispatched); err != nil {
		return false, err
	}
	if state != "unknown" || dispatched.After(cutoff) {
		return false, nil
	}
	reservation, err := scanReservation(tx.QueryRow(ctx, "llm_usage_reservations", reservationColumns, "request_id=$2", requestID))
	if err != nil {
		return false, err
	}
	// The reservation was sized as input bound + output limit, both at the
	// schedule's upper bound; the input half is what the estimate books.
	inputMicros := max(reservation.initialMicros-CostMicros(int64(request.outputLimit), request.priceSnapshot.UpperBound().Output), 0)
	inputTokens := max(reservation.initialTokens-int64(request.outputLimit), 0)
	zero := int64(0)
	payload, _ := json.Marshal(struct {
		Attempt     string `json:"attempt_id"`
		InputTokens int64  `json:"input_tokens"`
		InputMicros int64  `json:"input_micros"`
	}{attemptID, inputTokens, inputMicros})
	evidence := digest(payload)
	dimensions := map[string]any{"basis": "reserved_input_upper_bound", "input_tokens": inputTokens}
	for _, part := range []struct {
		component CostComponent
		cost      *int64
		tokens    *int64
	}{
		{ComponentInputCached, &zero, &zero},
		{ComponentInputUncached, &inputMicros, &inputTokens},
		{ComponentOutput, &zero, &zero},
		{ComponentBudgetTokens, nil, &inputTokens},
	} {
		m := Measurement{AttemptID: attemptID, Key: estimateKey(part.component), Component: part.component,
			Currency: reservation.currency, Dimensions: dimensions, CostMicros: part.cost, TokenCount: part.tokens,
			Certainty: "known", Kind: EvidenceOperatorAttested, EvidenceDigest: evidence}
		if _, err := s.applyMeasurement(ctx, tx, requestID, m, now); err != nil {
			return false, err
		}
	}
	if request.state == StateUnknown {
		if _, err := tx.Update(ctx, "llm_requests", "state='failed',retry_eligible=FALSE,revision=revision+1,updated_at=$3",
			"id=$2 AND state='unknown'", requestID, now); err != nil {
			return false, err
		}
	}
	return true, s.refreshSettlement(ctx, tx, requestID, now)
}

// lockFinishingAttempt allows a late complete result to resolve a recovered
// unknown, but never lets an old attempt overwrite a verified/rearmed request.
// Caller holds shared admission and budget locks before entering here.
func (s *Service) lockFinishingAttempt(ctx context.Context, tx store.TxAccountScope, p Permit) error {
	r, err := scanRequest(tx.QueryRowForUpdate(ctx, "llm_requests", requestColumns, "id=$2", p.RequestID))
	if err != nil {
		return err
	}
	if r.attemptsUsed != p.AttemptNo || (r.state != StateDispatching && r.state != StateStreaming && r.state != StateUnknown) {
		return ErrState
	}
	var state string
	err = tx.QueryRowForUpdate(ctx, "llm_attempts", "dispatch_state", "id=$2 AND request_id=$3 AND permit_digest=$4", p.AttemptID, p.RequestID, digest([]byte(p.Token))).Scan(&state)
	if err != nil {
		return err
	}
	if state != "dispatching" && state != "streaming" && state != "unknown" {
		return ErrState
	}
	return nil
}

// SweepExpiredDispatches uses account scopes even for disabled accounts: their
// orphaned transports can otherwise exhaust a shared provider for everyone.
// The cursor makes each tick bounded and prevents starvation past the first page.
func (s *Service) SweepExpiredDispatches(ctx context.Context, db *store.Store, after string, batch int) (string, error) {
	ids, err := db.MaintenanceAccountIDs(ctx, after, batch)
	if err != nil {
		return after, err
	}
	var errs []error
	for _, id := range ids {
		scope := db.ScopeFor(auth.AccountContext{AccountID: id})
		if _, err := s.RecoverExpired(ctx, scope, batch); err != nil {
			errs = append(errs, fmt.Errorf("account %s: %w", id, err))
		}
		if _, err := s.SettleUnverifiable(ctx, scope, batch); err != nil {
			errs = append(errs, fmt.Errorf("account %s: %w", id, err))
		}
	}
	next := ""
	if len(ids) == batch {
		next = ids[len(ids)-1]
	}
	return next, errors.Join(errs...)
}
