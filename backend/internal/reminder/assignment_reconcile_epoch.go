package reminder

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/planshare"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning/planningreminder"
)

// ReconcileEpochPlan is one durable plan row in an epoch work set.
type ReconcileEpochPlan struct {
	EpochID     string
	PlanID      string
	State       string
	LeaseOwner  *string
	LeaseUntil  *time.Time
	CompletedAt *time.Time
}

// ReminderReconcileEpochRepository is the reminder-owned epoch port.
type ReminderReconcileEpochRepository interface {
	ListProjectedSourcePlanIDsInScope(ctx context.Context, tx store.TxAccountScope) ([]string, error)
	CreateEpochInScope(ctx context.Context, tx store.TxAccountScope, epochID string, targetGeneration int64, sortedPlanIDs []string) error
	ClaimEpochPlansInScope(ctx context.Context, tx store.TxAccountScope, epochID string, limit int, leaseOwner string, leaseFor time.Duration) ([]ReconcileEpochPlan, error)
	MarkEpochPlanDoneInScope(ctx context.Context, tx store.TxAccountScope, epochID, planID string) error
	TryCompleteOrSupersedeEpochInScope(ctx context.Context, tx store.TxAccountScope, epochID string, lockedTargetGeneration int64) (string, error)
	LoadEpochStatusInScope(ctx context.Context, tx store.TxAccountScope, epochID string) (status string, target int64, err error)
	reminderReconcileEpochRepositorySeal()
}

type reminderReconcileEpochRepository struct{}

// NewReminderReconcileEpochRepository returns the reminder-owned epoch repository.
func NewReminderReconcileEpochRepository() ReminderReconcileEpochRepository {
	return reminderReconcileEpochRepository{}
}

func (reminderReconcileEpochRepository) reminderReconcileEpochRepositorySeal() {}

func (reminderReconcileEpochRepository) ListProjectedSourcePlanIDsInScope(
	ctx context.Context,
	tx store.TxAccountScope,
) ([]string, error) {
	rows, err := tx.Query(ctx, "plan_assignment_reminder_sources", "plan_id", "TRUE")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[string]struct{}{}
	out := make([]string, 0)
	for rows.Next() {
		var planID string
		if err := rows.Scan(&planID); err != nil {
			return nil, err
		}
		if _, ok := seen[planID]; ok {
			continue
		}
		seen[planID] = struct{}{}
		out = append(out, planID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

func (reminderReconcileEpochRepository) CreateEpochInScope(
	ctx context.Context,
	tx store.TxAccountScope,
	epochID string,
	targetGeneration int64,
	sortedPlanIDs []string,
) error {
	if epochID == "" {
		return errors.New("epoch id required")
	}
	if targetGeneration < 0 {
		return errors.New("target generation must be non-negative")
	}
	// Ensure stable sorted unique set.
	ids := append([]string(nil), sortedPlanIDs...)
	sort.Strings(ids)
	uniq := ids[:0]
	var last string
	for _, id := range ids {
		if id == "" || id == last {
			continue
		}
		uniq = append(uniq, id)
		last = id
	}
	if err := tx.Insert(ctx, "plan_assignment_reminder_reconcile_epochs",
		[]string{"epoch_id", "target_generation", "status", "created_at"},
		epochID, targetGeneration, EpochStatusOpen, time.Now().UTC(),
	); err != nil {
		return fmt.Errorf("insert reconcile epoch: %w", err)
	}
	for _, planID := range uniq {
		if err := tx.Insert(ctx, "plan_assignment_reminder_reconcile_epoch_plans",
			[]string{"epoch_id", "plan_id", "state"},
			epochID, planID, EpochPlanPending,
		); err != nil {
			return fmt.Errorf("insert reconcile epoch plan: %w", err)
		}
	}
	return nil
}

func (r reminderReconcileEpochRepository) ClaimEpochPlansInScope(
	ctx context.Context,
	tx store.TxAccountScope,
	epochID string,
	limit int,
	leaseOwner string,
	leaseFor time.Duration,
) ([]ReconcileEpochPlan, error) {
	if epochID == "" || leaseOwner == "" || limit < 1 {
		return nil, errors.New("epoch claim args incomplete")
	}
	if leaseFor <= 0 {
		leaseFor = 30 * time.Second
	}
	now := time.Now().UTC()
	leaseUntil := now.Add(leaseFor)
	// Reclaim expired leases + pending rows, ordered by plan_id.
	rows, err := tx.QueryPage(ctx, "plan_assignment_reminder_reconcile_epoch_plans",
		"plan_id, state, lease_owner, lease_until, completed_at",
		"epoch_id = $2 AND (state = $3 OR (state = $4 AND lease_until < $5))",
		[]store.OrderBy{{Column: "plan_id", Desc: false}},
		limit, 0,
		epochID, EpochPlanPending, EpochPlanLeased, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	candidates := make([]string, 0, limit)
	for rows.Next() {
		var planID, state string
		var leaseOwnerCol sql.NullString
		var leaseUntilCol, completedAt sql.NullTime
		if err := rows.Scan(&planID, &state, &leaseOwnerCol, &leaseUntilCol, &completedAt); err != nil {
			return nil, err
		}
		candidates = append(candidates, planID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]ReconcileEpochPlan, 0, len(candidates))
	for _, planID := range candidates {
		n, err := tx.Update(ctx, "plan_assignment_reminder_reconcile_epoch_plans",
			"state = $2, lease_owner = $3, lease_until = $4",
			"epoch_id = $5 AND plan_id = $6 AND (state = $7 OR (state = $8 AND lease_until < $9))",
			EpochPlanLeased, leaseOwner, leaseUntil, epochID, planID,
			EpochPlanPending, EpochPlanLeased, now)
		if err != nil {
			return nil, err
		}
		if n != 1 {
			continue
		}
		owner := leaseOwner
		until := leaseUntil
		out = append(out, ReconcileEpochPlan{
			EpochID: epochID, PlanID: planID, State: EpochPlanLeased,
			LeaseOwner: &owner, LeaseUntil: &until,
		})
	}
	return out, nil
}

func (reminderReconcileEpochRepository) MarkEpochPlanDoneInScope(
	ctx context.Context,
	tx store.TxAccountScope,
	epochID, planID string,
) error {
	n, err := tx.Update(ctx, "plan_assignment_reminder_reconcile_epoch_plans",
		"state = $2, completed_at = $3, lease_owner = NULL, lease_until = NULL",
		"epoch_id = $4 AND plan_id = $5 AND state = $6",
		EpochPlanDone, time.Now().UTC(), epochID, planID, EpochPlanLeased)
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("epoch plan not leased")
	}
	return nil
}

func (r reminderReconcileEpochRepository) TryCompleteOrSupersedeEpochInScope(
	ctx context.Context,
	tx store.TxAccountScope,
	epochID string,
	lockedTargetGeneration int64,
) (string, error) {
	status, target, err := r.LoadEpochStatusInScope(ctx, tx, epochID)
	if err != nil {
		return "", err
	}
	if status != EpochStatusOpen {
		return status, nil
	}
	if lockedTargetGeneration != target {
		if err := r.finishEpoch(ctx, tx, epochID, EpochStatusSuperseded); err != nil {
			return "", err
		}
		return EpochStatusSuperseded, nil
	}
	pending, err := tx.Count(ctx, "plan_assignment_reminder_reconcile_epoch_plans",
		"epoch_id = $2 AND state <> $3", epochID, EpochPlanDone)
	if err != nil {
		return "", err
	}
	if pending > 0 {
		return "", ErrEpochCannotComplete
	}
	if err := assertEpochTargetSettled(ctx, tx, target); err != nil {
		return "", err
	}
	if err := r.finishEpoch(ctx, tx, epochID, EpochStatusComplete); err != nil {
		return "", err
	}
	return EpochStatusComplete, nil
}

func (reminderReconcileEpochRepository) LoadEpochStatusInScope(
	ctx context.Context,
	tx store.TxAccountScope,
	epochID string,
) (string, int64, error) {
	var status string
	var target int64
	err := tx.QueryRow(ctx, "plan_assignment_reminder_reconcile_epochs",
		"status, target_generation", "epoch_id = $2", epochID).Scan(&status, &target)
	if err != nil {
		return "", 0, err
	}
	return status, target, nil
}

func (reminderReconcileEpochRepository) finishEpoch(
	ctx context.Context,
	tx store.TxAccountScope,
	epochID, status string,
) error {
	n, err := tx.Update(ctx, "plan_assignment_reminder_reconcile_epochs",
		"status = $2, completed_at = $3",
		"epoch_id = $4 AND status = $5",
		status, time.Now().UTC(), epochID, EpochStatusOpen)
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("epoch not open")
	}
	return nil
}

func assertEpochTargetSettled(ctx context.Context, tx store.TxAccountScope, target int64) error {
	if target < 1 {
		return nil
	}
	blocking, err := tx.Count(ctx, "planning_reminder_generation_work",
		"generation <= $2 AND state IN ($3, $4)", target,
		string(planningreminder.WorkPending), string(planningreminder.WorkQuarantined))
	if err != nil {
		return err
	}
	if blocking > 0 {
		return fmt.Errorf("%w: pending or quarantined work remains", ErrEpochCannotComplete)
	}
	applied, err := tx.Count(ctx, "planning_reminder_generation_work",
		"generation <= $2 AND state = $3", target, string(planningreminder.WorkApplied))
	if err != nil {
		return err
	}
	resolutions, err := tx.Count(ctx, "planning_reminder_generation_resolutions",
		"generation <= $2", target)
	if err != nil {
		return err
	}
	if applied != resolutions {
		return fmt.Errorf("%w: applied work missing matching resolution", ErrEpochCannotComplete)
	}
	unresolved, err := tx.Count(ctx, "plan_assignment_reminder_quarantines", "resolved_at IS NULL")
	if err != nil {
		return err
	}
	if unresolved > 0 {
		return fmt.Errorf("%w: unresolved quarantine remains", ErrEpochCannotComplete)
	}
	var watermark int64
	if err := tx.QueryRow(ctx, "planning_reminder_account_generations",
		"applied_generation", "TRUE").Scan(&watermark); err != nil {
		return err
	}
	if watermark < target {
		return fmt.Errorf("%w: watermark %d behind target %d", ErrEpochCannotComplete, watermark, target)
	}
	return nil
}

// ReconcileEpochCoordinator combines the three owner ports into one epoch.
type ReconcileEpochCoordinator struct {
	WorkReader   planningreminder.PlanningGenerationWorkReader
	SourceReader planshare.AssignmentReminderSourceReader
	Epochs       ReminderReconcileEpochRepository
}

// CreateEpoch locks the fence, captures target, unions three plan-ID sets, and materializes.
func (c ReconcileEpochCoordinator) CreateEpoch(ctx context.Context, scope store.AccountScope) (epochID string, target int64, err error) {
	if c.SourceReader == nil || c.Epochs == nil {
		return "", 0, errors.New("reconcile epoch coordinator incomplete")
	}
	epochID = "pare_" + uuid.NewString()
	err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		workReader := tx.PlanningGenerationWorkReader()
		if c.WorkReader != nil {
			workReader = c.WorkReader
		}
		if _, err := tx.PlanningReminderFence().LockCurrentAccount(ctx); err != nil {
			return err
		}
		captured, err := tx.CaptureFenceTargetGeneration(ctx)
		if err != nil {
			return err
		}
		target = captured
		workPlans, err := workReader.ListPlanIDsThroughTarget(ctx, target)
		if err != nil {
			return err
		}
		retained, err := c.SourceReader.ListRetainedPlanIDsInScope(ctx, tx)
		if err != nil {
			return err
		}
		projected, err := c.Epochs.ListProjectedSourcePlanIDsInScope(ctx, tx)
		if err != nil {
			return err
		}
		union := uniqueSortedStrings(append(append(workPlans, retained...), projected...))
		return c.Epochs.CreateEpochInScope(ctx, tx, epochID, target, union)
	})
	return epochID, target, err
}

// CompleteEpoch re-locks the fence and completes or supersedes.
func (c ReconcileEpochCoordinator) CompleteEpoch(ctx context.Context, scope store.AccountScope, epochID string) (string, error) {
	var status string
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if _, err := tx.PlanningReminderFence().LockCurrentAccount(ctx); err != nil {
			return err
		}
		target, err := tx.CaptureFenceTargetGeneration(ctx)
		if err != nil {
			return err
		}
		status, err = c.Epochs.TryCompleteOrSupersedeEpochInScope(ctx, tx, epochID, target)
		return err
	})
	return status, err
}

// LoadOrCreateOpenEpoch resumes a crash-surviving open epoch or materializes a new one.
func (c ReconcileEpochCoordinator) LoadOrCreateOpenEpoch(ctx context.Context, scope store.AccountScope) (epochID string, target int64, err error) {
	err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if _, err := tx.PlanningReminderFence().LockCurrentAccount(ctx); err != nil {
			return err
		}
		rows, err := tx.QueryPage(ctx, "plan_assignment_reminder_reconcile_epochs",
			"epoch_id, target_generation", "status = $2",
			[]store.OrderBy{{Column: "created_at", Desc: true}, {Column: "epoch_id", Desc: true}},
			1, 0, EpochStatusOpen)
		if err != nil {
			return err
		}
		defer rows.Close()
		if rows.Next() {
			if err := rows.Scan(&epochID, &target); err != nil {
				return err
			}
		}
		return rows.Err()
	})
	if err != nil || epochID != "" {
		return epochID, target, err
	}
	return c.CreateEpoch(ctx, scope)
}

func uniqueSortedStrings(in []string) []string {
	sort.Strings(in)
	out := make([]string, 0, len(in))
	var last string
	for _, s := range in {
		if s == "" || s == last {
			continue
		}
		out = append(out, s)
		last = s
	}
	return out
}

var _ ReminderReconcileEpochRepository = reminderReconcileEpochRepository{}
