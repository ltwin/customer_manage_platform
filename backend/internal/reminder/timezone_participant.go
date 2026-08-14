package reminder

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/planshare"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning/planningreminder"
)

// TimezoneChangePlanningParticipant lists plans that must recompute after a timezone change.
type TimezoneChangePlanningParticipant interface {
	ListAffectedPlanIDsInScope(ctx context.Context, tx store.TxAccountScope, oldTimezone, newTimezone string) ([]string, error)
}

// TimezoneChangePlanningAdapter implements TimezoneChangePlanningParticipant.
type TimezoneChangePlanningAdapter struct {
	sources planshare.AssignmentReminderSourceReader
	epochs  ReminderReconcileEpochRepository
}

// NewTimezoneChangePlanningParticipant returns retained-assignment ∪ projected-source plan IDs.
func NewTimezoneChangePlanningParticipant(
	sources planshare.AssignmentReminderSourceReader,
	epochs ReminderReconcileEpochRepository,
) *TimezoneChangePlanningAdapter {
	if sources == nil {
		sources = planshare.NewAssignmentReminderSourceReader()
	}
	if epochs == nil {
		epochs = NewReminderReconcileEpochRepository()
	}
	return &TimezoneChangePlanningAdapter{sources: sources, epochs: epochs}
}

func (p *TimezoneChangePlanningAdapter) ListAffectedPlanIDsInScope(
	ctx context.Context,
	tx store.TxAccountScope,
	_, _ string,
) ([]string, error) {
	if p == nil {
		return nil, errors.New("timezone change planning adapter is nil")
	}
	retained, err := p.sources.ListRetainedPlanIDsInScope(ctx, tx)
	if err != nil {
		return nil, err
	}
	projected, err := p.epochs.ListProjectedSourcePlanIDsInScope(ctx, tx)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(retained)+len(projected))
	out := make([]string, 0, len(retained)+len(projected))
	for _, id := range append(append([]string{}, retained...), projected...) {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sort.Strings(out)
	return out, nil
}

// ApplySettingsTimezoneChangedInScope recomputes one plan under the committed timezone.
func ApplySettingsTimezoneChangedInScope(
	ctx context.Context,
	tx store.TxAccountScope,
	locked planningreminder.LockedFenceTx,
	work planningreminder.GenerationWork,
	sources planshare.AssignmentReminderSourceReader,
	facts PlanFactReader,
	now time.Time,
) error {
	if work.MutationKind != planningreminder.MutationSettingsTimezoneChanged {
		return fmt.Errorf("expected settings_timezone_changed, got %s", work.MutationKind)
	}
	if work.SourceEventID != nil {
		return errors.New("settings timezone work must not carry source_event_id")
	}
	if sources == nil {
		sources = planshare.NewAssignmentReminderSourceReader()
	}
	if facts == nil {
		facts = DefaultPlanFactReader{}
	}
	before, err := CountInboxInScope(ctx, tx)
	if err != nil {
		return err
	}
	sidecar, timezone, err := facts.LoadPlanFactsInScope(ctx, tx, work.PlanID, now)
	if err != nil {
		return err
	}
	assignments, err := loadSafeAssignmentsForPlan(ctx, tx, sources, work.PlanID)
	if err != nil {
		return err
	}
	if _, err := ReconcilePlanProjection(ctx, tx, NewAssignmentProjectionRepository(), ReduceDesiredReminderGroupsInput{
		AccountID:            tx.AccountID(),
		PlanID:               work.PlanID,
		Assignments:          assignments,
		Sidecar:              sidecar,
		Timezone:             timezone,
		Now:                  now.UTC(),
		ActivationGeneration: work.Generation,
	}); err != nil {
		return err
	}
	if err := WriteGenerationResolutionInScope(ctx, tx, GenerationResolutionInput{
		Generation: work.Generation, PlanID: work.PlanID, MutationKind: work.MutationKind,
		ResolutionKind: ResolutionSettingsRebuilt,
	}); err != nil {
		return err
	}
	if err := MarkAppliedAfterResolution(ctx, locked, tx, work.Generation); err != nil {
		return err
	}
	after, err := CountInboxInScope(ctx, tx)
	if err != nil {
		return err
	}
	if after != before {
		return errors.New("settings timezone settlement must not write assignment inbox")
	}
	return nil
}

// ApplyLifecycleWorkInScope settles CRM/archive pending work by recomputing from current facts.
func ApplyLifecycleWorkInScope(
	ctx context.Context,
	tx store.TxAccountScope,
	locked planningreminder.LockedFenceTx,
	work planningreminder.GenerationWork,
	sources planshare.AssignmentReminderSourceReader,
	facts PlanFactReader,
	now time.Time,
) error {
	switch work.MutationKind {
	case planningreminder.MutationCRMPlanLinkChanged,
		planningreminder.MutationCRMOrderLifecycleChanged,
		planningreminder.MutationCRMScheduleChanged,
		planningreminder.MutationPlanArchived:
	default:
		return fmt.Errorf("unsupported lifecycle mutation kind %q", work.MutationKind)
	}
	if work.SourceEventID != nil {
		return errors.New("lifecycle work must not carry source_event_id")
	}
	if sources == nil {
		sources = planshare.NewAssignmentReminderSourceReader()
	}
	if facts == nil {
		facts = DefaultPlanFactReader{}
	}
	before, err := CountInboxInScope(ctx, tx)
	if err != nil {
		return err
	}
	if work.MutationKind == planningreminder.MutationPlanArchived {
		if err := withdrawCurrentGroupsForPlan(ctx, tx, work.PlanID, WithdrawReasonPlanArchived, now); err != nil {
			return err
		}
	} else {
		sidecar, timezone, err := facts.LoadPlanFactsInScope(ctx, tx, work.PlanID, now)
		if err != nil {
			return err
		}
		assignments, err := loadSafeAssignmentsForPlan(ctx, tx, sources, work.PlanID)
		if err != nil {
			return err
		}
		if _, err := ReconcilePlanProjection(ctx, tx, NewAssignmentProjectionRepository(), ReduceDesiredReminderGroupsInput{
			AccountID:            tx.AccountID(),
			PlanID:               work.PlanID,
			Assignments:          assignments,
			Sidecar:              sidecar,
			Timezone:             timezone,
			Now:                  now.UTC(),
			ActivationGeneration: work.Generation,
		}); err != nil {
			return err
		}
	}
	if err := WriteGenerationResolutionInScope(ctx, tx, GenerationResolutionInput{
		Generation: work.Generation, PlanID: work.PlanID, MutationKind: work.MutationKind,
		ResolutionKind: ResolutionLifecycleApplied,
	}); err != nil {
		return err
	}
	if err := MarkAppliedAfterResolution(ctx, locked, tx, work.Generation); err != nil {
		return err
	}
	after, err := CountInboxInScope(ctx, tx)
	if err != nil {
		return err
	}
	if after != before {
		return errors.New("lifecycle settlement must not write assignment inbox")
	}
	return nil
}
