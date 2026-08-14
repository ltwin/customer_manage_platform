package reminder

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/planshare"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning/planningreminder"
)

// ApplyMixedKindStubInScope is retained for S2 test names; S3 routes to real settlement.
// Prefer ApplyMixedKindInScope for new call sites.
func ApplyMixedKindStubInScope(
	ctx context.Context,
	tx store.TxAccountScope,
	locked planningreminder.LockedFenceTx,
	work planningreminder.GenerationWork,
	facts PlanFactReader,
	now time.Time,
) error {
	return ApplyMixedKindInScope(ctx, tx, locked, work, nil, facts, now)
}

// ApplyMixedKindInScope settles non-assignment work via authoritative S3 participants.
// It must never write assignment Inbox.
func ApplyMixedKindInScope(
	ctx context.Context,
	tx store.TxAccountScope,
	locked planningreminder.LockedFenceTx,
	work planningreminder.GenerationWork,
	sources planshare.AssignmentReminderSourceReader,
	facts PlanFactReader,
	now time.Time,
) error {
	if work.SourceEventID != nil {
		return errors.New("non-assignment work must not carry source_event_id")
	}
	if exists, err := tx.Exists(ctx, "planning_reminder_generation_resolutions", "generation = $2", work.Generation); err != nil {
		return err
	} else if exists {
		return MarkAppliedAfterResolution(ctx, locked, tx, work.Generation)
	}
	switch work.MutationKind {
	case planningreminder.MutationCRMPlanLinkChanged,
		planningreminder.MutationCRMOrderLifecycleChanged,
		planningreminder.MutationCRMScheduleChanged,
		planningreminder.MutationPlanArchived:
		return ApplyLifecycleWorkInScope(ctx, tx, locked, work, sources, facts, now)
	case planningreminder.MutationSettingsTimezoneChanged:
		var tz sql.NullString
		if err := tx.QueryRow(ctx, "settings", "timezone", "TRUE").Scan(&tz); err != nil {
			return err
		}
		if !tz.Valid || tz.String == "" {
			return errors.New("settings timezone missing")
		}
		return ApplySettingsTimezoneChangedInScope(ctx, tx, locked, work, sources, facts, now)
	case planningreminder.MutationShootStarted:
		return ApplyTemporalShootStartedInScope(ctx, tx, locked, work, now)
	default:
		return fmt.Errorf("unsupported mixed mutation kind %q", work.MutationKind)
	}
}
