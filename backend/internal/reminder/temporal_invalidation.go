package reminder

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning/planningreminder"
)

var (
	// ErrTemporalInvalidationPending means another pending marker already owns the exact validity.
	ErrTemporalInvalidationPending = errors.New("temporal_invalidation_pending")
	// ErrTemporalInvariantCorruption means an applied marker still has a matching current group.
	ErrTemporalInvariantCorruption = errors.New("temporal_invariant_corruption")
)

// TemporalInvalidationClaim is returned when this constructor wins the unique marker.
type TemporalInvalidationClaim struct {
	PlanID     string
	SlotID     string
	ValidUntil time.Time
	Generation int64
	Won        bool
}

// EnsureTemporalInvalidationInScope inserts the unique marker; only the first winner reserves shoot_started.
// Validity checks that need "now" must use PostgreSQL clock_timestamp() in guarded SQL, not application clocks.
func EnsureTemporalInvalidationInScope(
	ctx context.Context,
	tx store.TxAccountScope,
	locked planningreminder.LockedFenceTx,
	planID, slotID string,
	validUntil time.Time,
) (TemporalInvalidationClaim, error) {
	if locked == nil {
		return TemporalInvalidationClaim{}, planningreminder.ErrFenceNotLocked
	}
	if planID == "" || slotID == "" || validUntil.IsZero() {
		return TemporalInvalidationClaim{}, errors.New("temporal invalidation input incomplete")
	}
	validUntil = validUntil.UTC()

	var insertedPlan string
	err := tx.InsertOnConflictDoNothingReturning(ctx,
		"plan_assignment_reminder_temporal_invalidations",
		[]string{"plan_id", "slot_id", "valid_until", "state"},
		[]string{"account_id", "plan_id", "slot_id", "valid_until"},
		[]string{"plan_id"},
		planID, slotID, validUntil, "pending",
	).Scan(&insertedPlan)
	if err == nil {
		generation, err := locked.ReserveGeneration(ctx, planningreminder.MutationFact{
			PlanID: planID, MutationKind: planningreminder.MutationShootStarted,
		})
		if err != nil {
			return TemporalInvalidationClaim{}, err
		}
		n, err := tx.Update(ctx, "plan_assignment_reminder_temporal_invalidations",
			"generation = $2",
			"plan_id = $3 AND slot_id = $4 AND valid_until = $5 AND state = $6 AND generation IS NULL",
			generation, planID, slotID, validUntil, "pending")
		if err != nil {
			return TemporalInvalidationClaim{}, err
		}
		if n != 1 {
			return TemporalInvalidationClaim{}, fmt.Errorf("temporal marker generation backfill: expected 1 row, got %d", n)
		}
		return TemporalInvalidationClaim{
			PlanID: planID, SlotID: slotID, ValidUntil: validUntil, Generation: generation, Won: true,
		}, nil
	}
	if !errors.Is(err, store.ErrNoRows) {
		return TemporalInvalidationClaim{}, fmt.Errorf("insert temporal invalidation: %w", err)
	}

	var state string
	var generation sql.NullInt64
	if err := tx.QueryRow(ctx, "plan_assignment_reminder_temporal_invalidations",
		"state, generation",
		"plan_id = $2 AND slot_id = $3 AND valid_until = $4",
		planID, slotID, validUntil,
	).Scan(&state, &generation); err != nil {
		return TemporalInvalidationClaim{}, err
	}
	switch state {
	case "pending":
		return TemporalInvalidationClaim{}, ErrTemporalInvalidationPending
	case "applied":
		stillCurrent, err := hasCurrentGroupAtValidity(ctx, tx, planID, slotID, validUntil)
		if err != nil {
			return TemporalInvalidationClaim{}, err
		}
		if stillCurrent {
			return TemporalInvalidationClaim{}, ErrTemporalInvariantCorruption
		}
		return TemporalInvalidationClaim{
			PlanID: planID, SlotID: slotID, ValidUntil: validUntil, Won: false,
		}, nil
	default:
		return TemporalInvalidationClaim{}, fmt.Errorf("unknown temporal marker state %q", state)
	}
}

// ApplyTemporalShootStartedInScope withdraws only groups matching the marker exact validity.
func ApplyTemporalShootStartedInScope(
	ctx context.Context,
	tx store.TxAccountScope,
	locked planningreminder.LockedFenceTx,
	work planningreminder.GenerationWork,
	now time.Time,
) error {
	if work.MutationKind != planningreminder.MutationShootStarted {
		return fmt.Errorf("expected shoot_started, got %s", work.MutationKind)
	}
	if work.SourceEventID != nil {
		return errors.New("temporal work must not carry source_event_id")
	}
	var slotID string
	var validUntil time.Time
	var state string
	err := tx.QueryRowForUpdate(ctx, "plan_assignment_reminder_temporal_invalidations",
		"slot_id, valid_until, state",
		"plan_id = $2 AND generation = $3",
		work.PlanID, work.Generation,
	).Scan(&slotID, &validUntil, &state)
	if errors.Is(err, store.ErrNoRows) {
		return errors.New("temporal marker missing for shoot_started work")
	}
	if err != nil {
		return err
	}
	if state != "pending" {
		return fmt.Errorf("temporal marker state %q not pending", state)
	}
	validUntil = validUntil.UTC()

	before, err := CountInboxInScope(ctx, tx)
	if err != nil {
		return err
	}
	if err := withdrawGroupsAtExactValidity(ctx, tx, work.PlanID, slotID, validUntil, now); err != nil {
		return err
	}
	if err := WriteGenerationResolutionInScope(ctx, tx, GenerationResolutionInput{
		Generation: work.Generation, PlanID: work.PlanID, MutationKind: work.MutationKind,
		ResolutionKind: ResolutionTemporalApplied,
	}); err != nil {
		return err
	}
	n, err := tx.Update(ctx, "plan_assignment_reminder_temporal_invalidations",
		"state = $2, applied_at = $3",
		"plan_id = $4 AND slot_id = $5 AND valid_until = $6 AND state = $7 AND generation = $8",
		"applied", now.UTC(), work.PlanID, slotID, validUntil, "pending", work.Generation)
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("mark temporal marker applied: expected 1 row, got %d", n)
	}
	if err := MarkAppliedAfterResolution(ctx, locked, tx, work.Generation); err != nil {
		return err
	}
	after, err := CountInboxInScope(ctx, tx)
	if err != nil {
		return err
	}
	if after != before {
		return errors.New("temporal settlement must not write assignment inbox")
	}
	return nil
}

func hasCurrentGroupAtValidity(
	ctx context.Context,
	tx store.TxAccountScope,
	planID, slotID string,
	validUntil time.Time,
) (bool, error) {
	return tx.Exists(ctx, "plan_assignment_reminder_groups",
		"plan_id = $2 AND slot_id = $3 AND valid_until = $4 AND state = $5",
		planID, slotID, validUntil.UTC(), GroupStateCurrent)
}

func withdrawGroupsAtExactValidity(
	ctx context.Context,
	tx store.TxAccountScope,
	planID, slotID string,
	validUntil time.Time,
	now time.Time,
) error {
	rows, err := tx.Query(ctx, "plan_assignment_reminder_groups",
		"group_id, reminder_id",
		"plan_id = $2 AND slot_id = $3 AND valid_until = $4 AND state = $5",
		planID, slotID, validUntil.UTC(), GroupStateCurrent)
	if err != nil {
		return err
	}
	defer rows.Close()
	type pair struct{ groupID, reminderID string }
	pairs := make([]pair, 0)
	for rows.Next() {
		var p pair
		if err := rows.Scan(&p.groupID, &p.reminderID); err != nil {
			return err
		}
		pairs = append(pairs, p)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, p := range pairs {
		var status string
		if err := tx.QueryRow(ctx, "reminders", "status", "id = $2", p.reminderID).Scan(&status); err != nil {
			return err
		}
		if err := withdrawGroup(ctx, tx, GroupWithdrawal{
			GroupID: p.groupID, ReminderID: p.reminderID,
			Reason: WithdrawReasonShootStarted, Dismiss: status == StatusPending,
		}, now); err != nil {
			return err
		}
	}
	return nil
}

// ProbeExpiredCurrentGroupsInScope finds current groups whose valid_until has passed per clock_timestamp().
func ProbeExpiredCurrentGroupsInScope(ctx context.Context, tx store.TxAccountScope) ([]struct {
	PlanID, SlotID string
	ValidUntil     time.Time
}, error) {
	rows, err := tx.Query(ctx, "plan_assignment_reminder_groups",
		"plan_id, slot_id, valid_until",
		"state = $2 AND valid_until <= clock_timestamp()",
		GroupStateCurrent)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]struct {
		PlanID, SlotID string
		ValidUntil     time.Time
	}, 0)
	for rows.Next() {
		var planID, slotID string
		var validUntil time.Time
		if err := rows.Scan(&planID, &slotID, &validUntil); err != nil {
			return nil, err
		}
		out = append(out, struct {
			PlanID, SlotID string
			ValidUntil     time.Time
		}{PlanID: planID, SlotID: slotID, ValidUntil: validUntil.UTC()})
	}
	return out, rows.Err()
}
