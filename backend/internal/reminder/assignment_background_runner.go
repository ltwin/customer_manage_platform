package reminder

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/planshare"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const (
	planningAssignmentInterval   = 2 * time.Second
	planningAssignmentWorkLimit  = 16
	planningQuarantineClaimLimit = 8
	planningEpochPlanClaimLimit  = 16
	planningReconcileEveryTicks  = 15
	planningRetentionEveryTicks  = 30
	planningQuarantineLease      = 45 * time.Second
	planningEpochPlanLease       = 45 * time.Second
	digestIntentRedactAfter      = 7 * 24 * time.Hour
	digestIntentDeleteAfter      = 90 * 24 * time.Hour
)

// PlanningAssignmentRunner is the production PG lease/SKIP LOCKED background loop
// for assignment consume, quarantine repair, temporal shoot_started, epoch reconcile,
// and terminal digest-intent retention. It shares the same backgroundRunner lifecycle
// as ScanRunner; it is not an in-memory queue and not a second Telegram bot.
type PlanningAssignmentRunner struct {
	accounts AccountScopeEnumerator
	consumer AssignmentEventConsumer
	epochs   ReconcileEpochCoordinator
	logger   *slog.Logger
	interval time.Duration
	ownerID  string
	tick     atomic.Uint64
	running  atomic.Bool
}

// NewPlanningAssignmentRunner wires production ports for the assignment reminder loop.
func NewPlanningAssignmentRunner(accounts AccountScopeEnumerator, logger *slog.Logger) *PlanningAssignmentRunner {
	if logger == nil {
		logger = slog.Default()
	}
	host, _ := os.Hostname()
	owner := fmt.Sprintf("par_%s_%s", host, uuid.NewString()[:8])
	return &PlanningAssignmentRunner{
		accounts: accounts,
		consumer: AssignmentEventConsumer{
			SourceReader: planshare.NewAssignmentReminderSourceReader(),
			Projection:   NewAssignmentProjectionRepository(),
			Facts:        DefaultPlanFactReader{},
		},
		epochs: ReconcileEpochCoordinator{
			SourceReader: planshare.NewAssignmentReminderSourceReader(),
			Epochs:       NewReminderReconcileEpochRepository(),
		},
		logger:   logger,
		interval: planningAssignmentInterval,
		ownerID:  owner,
	}
}

// Run blocks until ctx is cancelled; first tick runs immediately.
func (r *PlanningAssignmentRunner) Run(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	r.logger.Info("planning assignment reminder runner started")
	r.RunOnce(ctx)
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			r.logger.Info("planning assignment reminder runner stopped")
			return
		case <-ticker.C:
			r.RunOnce(ctx)
		}
	}
}

// RunOnce enumerates accounts and advances consume / temporal / quarantine / epoch / retention.
func (r *PlanningAssignmentRunner) RunOnce(ctx context.Context) {
	if !r.running.CompareAndSwap(false, true) {
		return
	}
	defer r.running.Store(false)

	tick := r.tick.Add(1)
	accounts, err := r.accounts.AccountScopes(ctx)
	if err != nil {
		r.logError("planning assignment account enumeration failed", err)
		return
	}
	for _, account := range accounts {
		if ctx.Err() != nil {
			return
		}
		if err := r.processAccount(ctx, account, tick); err != nil {
			r.logError("planning assignment account tick failed", err, slog.String("account_id", account.AccountID))
		}
	}
}

func (r *PlanningAssignmentRunner) processAccount(ctx context.Context, account store.ScopedAccount, tick uint64) error {
	if _, err := r.consumer.ProcessAccountOnce(ctx, account.Scope, planningAssignmentWorkLimit); err != nil {
		return err
	}
	if err := r.ensureTemporalInvalidations(ctx, account.Scope); err != nil {
		return err
	}
	if _, err := r.consumer.ProcessAccountOnce(ctx, account.Scope, planningAssignmentWorkLimit); err != nil {
		return err
	}
	if err := r.repairDueQuarantines(ctx, account.Scope); err != nil {
		return err
	}
	if tick%planningReconcileEveryTicks == 0 {
		if err := r.reconcileEpochTick(ctx, account.Scope); err != nil {
			return err
		}
	}
	if tick%planningRetentionEveryTicks == 0 {
		if err := ApplyDigestIntentRetentionInScope(ctx, account.Scope); err != nil {
			return err
		}
	}
	return nil
}

func (r *PlanningAssignmentRunner) ensureTemporalInvalidations(ctx context.Context, scope store.AccountScope) error {
	return scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		locked, err := tx.PlanningReminderFence().LockCurrentAccount(ctx)
		if err != nil {
			return err
		}
		expired, err := ProbeExpiredCurrentGroupsInScope(ctx, tx)
		if err != nil {
			return err
		}
		for _, row := range expired {
			if _, err := EnsureTemporalInvalidationInScope(ctx, tx, locked, row.PlanID, row.SlotID, row.ValidUntil); err != nil {
				if errors.Is(err, ErrTemporalInvalidationPending) {
					continue
				}
				return err
			}
		}
		return nil
	})
}

func (r *PlanningAssignmentRunner) repairDueQuarantines(ctx context.Context, scope store.AccountScope) error {
	var claimed []store.ClaimedAssignmentQuarantine
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if _, err := tx.PlanningReminderFence().LockCurrentAccount(ctx); err != nil {
			return err
		}
		var cerr error
		claimed, cerr = tx.ClaimDueAssignmentQuarantinesInScope(ctx, planningQuarantineClaimLimit, r.ownerID, planningQuarantineLease)
		return cerr
	})
	if err != nil {
		return err
	}
	for _, row := range claimed {
		if err := r.consumer.RepairQuarantinedAssignment(ctx, scope, row.SourceEventID); err != nil {
			r.logError("quarantine repair failed", err,
				slog.String("account_id", scope.AccountID()),
				slog.String("source_event_id", row.SourceEventID),
				slog.String("plan_id", row.PlanID),
			)
		}
	}
	return nil
}

func (r *PlanningAssignmentRunner) reconcileEpochTick(ctx context.Context, scope store.AccountScope) error {
	epochID, _, err := r.epochs.LoadOrCreateOpenEpoch(ctx, scope)
	if err != nil {
		return err
	}
	if epochID == "" {
		return nil
	}
	for {
		var plans []ReconcileEpochPlan
		err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
			if _, err := tx.PlanningReminderFence().LockCurrentAccount(ctx); err != nil {
				return err
			}
			var cerr error
			plans, cerr = r.epochs.Epochs.ClaimEpochPlansInScope(
				ctx, tx, epochID, planningEpochPlanClaimLimit, r.ownerID, planningEpochPlanLease,
			)
			return cerr
		})
		if err != nil {
			return err
		}
		if len(plans) == 0 {
			break
		}
		for _, plan := range plans {
			if err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
				if _, err := tx.PlanningReminderFence().LockCurrentAccount(ctx); err != nil {
					return err
				}
				return r.epochs.Epochs.MarkEpochPlanDoneInScope(ctx, tx, plan.EpochID, plan.PlanID)
			}); err != nil {
				return err
			}
		}
	}
	_, err = r.epochs.CompleteEpoch(ctx, scope, epochID)
	if errors.Is(err, ErrEpochCannotComplete) {
		return nil
	}
	return err
}

func (r *PlanningAssignmentRunner) logError(message string, err error, attrs ...slog.Attr) {
	if r.logger == nil {
		return
	}
	args := make([]any, 0, len(attrs)+1)
	if err != nil {
		args = append(args, slog.Any("error", err))
	}
	for _, attr := range attrs {
		args = append(args, attr)
	}
	r.logger.Error(message, args...)
}
