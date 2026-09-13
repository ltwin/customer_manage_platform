package llmgateway

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// transportLifetime is a fixed protocol bound measured from the committed
// dispatch timestamp, not from a worker's eventual start. Changing it requires
// draining old executors first. Both shipped HTTP adapters honor cancellation.
const transportLifetime = 2 * time.Minute
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
		rows, err := tx.QueryPage(ctx, "llm_attempts", "id", "dispatch_state IN ('dispatching','streaming') AND dispatched_at<=$2 AND permit_released_at IS NULL", []store.OrderBy{{Column: "dispatched_at"}, {Column: "id"}}, batch, 0, cutoff)
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
		if _, err := s.RecoverExpired(ctx, db.ScopeFor(auth.AccountContext{AccountID: id}), batch); err != nil {
			errs = append(errs, fmt.Errorf("account %s: %w", id, err))
		}
	}
	next := ""
	if len(ids) == batch {
		next = ids[len(ids)-1]
	}
	return next, errors.Join(errs...)
}
