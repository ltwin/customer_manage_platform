package reminder

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning/planningreminder"
)

// PlanArchiveReminderAdapter withdraws current assignment reminder groups on archive.
// Assignment / terminal reminder history is retained; only current groups are withdrawn.
type PlanArchiveReminderAdapter struct {
	Projection AssignmentProjectionRepository
}

// NewPlanArchiveReminderAdapter returns the core PlanArchiveReminderParticipant adapter.
func NewPlanArchiveReminderAdapter() *PlanArchiveReminderAdapter {
	return &PlanArchiveReminderAdapter{Projection: NewAssignmentProjectionRepository()}
}

var _ shootplanning.PlanArchiveReminderParticipant = (*PlanArchiveReminderAdapter)(nil)

// OnPlanArchivedInScope withdraws current groups, writes lifecycle_applied, then MarkApplied.
// Fence is already locked and generation reserved by the core archive path.
func (a *PlanArchiveReminderAdapter) OnPlanArchivedInScope(
	ctx context.Context,
	tx store.TxAccountScope,
	planID string,
	_ int64,
	generation int64,
	now time.Time,
) error {
	if a == nil {
		return errors.New("plan archive reminder adapter is nil")
	}
	if planID == "" || generation < 1 {
		return errors.New("plan archive reminder input incomplete")
	}
	before, err := CountInboxInScope(ctx, tx)
	if err != nil {
		return err
	}
	if err := withdrawCurrentGroupsForPlan(ctx, tx, planID, WithdrawReasonPlanArchived, now); err != nil {
		return err
	}
	if err := WriteGenerationResolutionInScope(ctx, tx, GenerationResolutionInput{
		Generation: generation, PlanID: planID,
		MutationKind:   planningreminder.MutationPlanArchived,
		ResolutionKind: ResolutionLifecycleApplied,
	}); err != nil {
		return err
	}
	locked, err := tx.PlanningReminderFence().LockCurrentAccount(ctx)
	if err != nil {
		return err
	}
	if err := MarkAppliedAfterResolution(ctx, locked, tx, generation); err != nil {
		return err
	}
	after, err := CountInboxInScope(ctx, tx)
	if err != nil {
		return err
	}
	if after != before {
		return errors.New("archive reminder adapter must not write assignment inbox")
	}
	return nil
}

func withdrawCurrentGroupsForPlan(
	ctx context.Context,
	tx store.TxAccountScope,
	planID string,
	reason string,
	now time.Time,
) error {
	groups, err := NewAssignmentProjectionRepository().ListCurrentGroupsInScope(ctx, tx, planID)
	if err != nil {
		return err
	}
	for _, g := range groups {
		if err := withdrawGroup(ctx, tx, GroupWithdrawal{
			GroupID:    g.GroupID,
			ReminderID: g.ReminderID,
			Reason:     reason,
			Dismiss:    g.ReminderStatus == StatusPending,
		}, now); err != nil {
			return fmt.Errorf("withdraw group on archive: %w", err)
		}
	}
	return nil
}
