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

// PlanFactReader loads plan/order/slot sidecar + timezone for projection.
type PlanFactReader interface {
	LoadPlanFactsInScope(ctx context.Context, tx store.TxAccountScope, planID string, now time.Time) (PlanOrderSlotSidecar, string, error)
}

// AssignmentEventConsumer applies pending assignment generation work.
type AssignmentEventConsumer struct {
	WorkReader   planningreminder.PlanningGenerationWorkReader
	SourceReader planshare.AssignmentReminderSourceReader
	Projection   AssignmentProjectionRepository
	Facts        PlanFactReader
	Now          func() time.Time
}

// ProcessAccountOnce locks the fence, claims pending work, and applies each unit.
func (c AssignmentEventConsumer) ProcessAccountOnce(ctx context.Context, scope store.AccountScope, limit int) (processed int, err error) {
	if c.SourceReader == nil {
		return 0, errors.New("assignment event consumer incomplete")
	}
	if limit < 1 {
		limit = 8
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	type corruptPayload struct {
		work planningreminder.GenerationWork
		ev   planshare.AssignmentReminderSourceEvent
		code string
	}
	var corrupt *corruptPayload

	err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		locked, err := tx.PlanningReminderFence().LockCurrentAccount(ctx)
		if err != nil {
			return err
		}
		target, err := tx.CaptureFenceTargetGeneration(ctx)
		if err != nil {
			return err
		}
		workReader := tx.PlanningGenerationWorkReader()
		if c.WorkReader != nil {
			workReader = c.WorkReader
		}
		works, err := workReader.ClaimPending(ctx, target, limit)
		if err != nil {
			return err
		}
		for _, work := range works {
			switch work.MutationKind {
			case planningreminder.MutationAssignmentActivated, planningreminder.MutationAssignmentRevoked:
				n, cerr := c.applyAssignmentWork(ctx, tx, locked, work)
				if errors.Is(cerr, ErrCorruptAssignmentPayload) || errors.Is(cerr, ErrGenerationIdentityMismatch) {
					ev, _ := c.SourceReader.LoadSourceEventInScope(ctx, tx, deref(work.SourceEventID))
					code := QuarantineErrorCorruptPayload
					if errors.Is(cerr, ErrGenerationIdentityMismatch) {
						code = QuarantineErrorIdentityMismatch
					}
					corrupt = &corruptPayload{work: work, ev: ev, code: code}
					return cerr
				}
				if cerr != nil {
					return cerr
				}
				processed += n
			default:
				if err := ApplyMixedKindStubInScope(ctx, tx, locked, work, c.Facts, c.Now()); err != nil {
					return err
				}
				processed++
			}
		}
		return nil
	})
	if corrupt != nil {
		qerr := PersistCorruptQuarantine(ctx, scope, QuarantineRecord{
			SourceEventID:           deref(corrupt.work.SourceEventID),
			PlanID:                  corrupt.work.PlanID,
			AssignmentID:            corrupt.ev.AssignmentID,
			AssignmentRevision:      corrupt.ev.AssignmentRevision,
			AccountSourceGeneration: corrupt.work.Generation,
			ErrorCode:               corrupt.code,
			PayloadFingerprint:      corrupt.ev.ContentFingerprint,
		}, corrupt.work.Generation)
		if qerr != nil {
			return processed, fmt.Errorf("persist quarantine after corrupt: %w (cause: %v)", qerr, err)
		}
		return processed, err
	}
	return processed, err
}

func (c AssignmentEventConsumer) applyAssignmentWork(
	ctx context.Context,
	tx store.TxAccountScope,
	locked planningreminder.LockedFenceTx,
	work planningreminder.GenerationWork,
) (int, error) {
	if work.SourceEventID == nil || *work.SourceEventID == "" {
		return 0, ErrGenerationIdentityMismatch
	}
	ev, err := c.SourceReader.LoadSourceEventInScope(ctx, tx, *work.SourceEventID)
	if err != nil {
		return 0, err
	}
	if err := verifyWorkEventIdentity(work, ev); err != nil {
		return 0, err
	}
	// Already consumed → idempotent MarkApplied path if somehow pending.
	acked, err := tx.Exists(ctx, "plan_assignment_reminder_inbox", "source_event_id = $2", ev.EventID)
	if err != nil {
		return 0, err
	}
	if acked {
		if err := ensureResolutionAndApply(ctx, tx, locked, work, ev, ResolutionEventApplied); err != nil {
			return 0, err
		}
		return 1, nil
	}

	current, err := c.SourceReader.LoadCurrentAssignmentSourceInScope(ctx, tx, planshare.AssignmentSourceRef{
		PlanID: ev.PlanID, AssignmentID: ev.AssignmentID,
	})
	if err != nil && !errors.Is(err, planshare.ErrAssignmentNotFound) {
		return 0, err
	}

	projectedRev, projectedFP, err := loadProjectedAssignmentMeta(ctx, tx, ev.PlanID, ev.AssignmentID)
	if err != nil {
		return 0, err
	}

	switch {
	case projectedRev > 0 && ev.AssignmentRevision < projectedRev:
		// Low revision: recompute from current safe snapshot; idempotent apply.
	case projectedRev > 0 && ev.AssignmentRevision == projectedRev:
		if ev.ContentFingerprint != projectedFP {
			return 0, ErrCorruptAssignmentPayload
		}
		// Same revision + same fingerprint → material no-op, still settle work.
	case !errors.Is(err, planshare.ErrAssignmentNotFound) && current.AssignmentID != "":
		if current.AssignmentRevision == ev.AssignmentRevision &&
			current.ContentFingerprint != ev.ContentFingerprint {
			return 0, ErrCorruptAssignmentPayload
		}
	}

	assignments, err := c.buildSafeAssignments(ctx, tx, ev.PlanID, current, ev)
	if err != nil {
		return 0, err
	}
	sidecar, timezone, err := c.Facts.LoadPlanFactsInScope(ctx, tx, ev.PlanID, c.Now())
	if err != nil {
		return 0, err
	}
	_, err = ReconcilePlanProjection(ctx, tx, c.Projection, ReduceDesiredReminderGroupsInput{
		AccountID:            tx.AccountID(),
		PlanID:               ev.PlanID,
		Assignments:          assignments,
		Sidecar:              sidecar,
		Timezone:             timezone,
		Now:                  c.Now().UTC(),
		ActivationGeneration: work.Generation,
	})
	if err != nil {
		return 0, err
	}
	if err := UpsertAssignmentInboxInScope(ctx, tx, AssignmentInboxRecord{
		SourceEventID:           ev.EventID,
		PlanID:                  ev.PlanID,
		AssignmentID:            ev.AssignmentID,
		AssignmentRevision:      ev.AssignmentRevision,
		AccountSourceGeneration: work.Generation,
		EventKind:               ev.EventKind,
		PayloadFingerprint:      ev.ContentFingerprint,
		ResolutionKind:          InboxResolutionEventApplied,
	}); err != nil {
		return 0, err
	}
	if err := WriteGenerationResolutionInScope(ctx, tx, GenerationResolutionInput{
		Generation: work.Generation, PlanID: work.PlanID, MutationKind: work.MutationKind,
		ResolutionKind: ResolutionEventApplied, SourceEventID: work.SourceEventID,
	}); err != nil {
		return 0, err
	}
	if err := MarkAppliedAfterResolution(ctx, locked, tx, work.Generation); err != nil {
		return 0, err
	}
	return 1, nil
}

func (c AssignmentEventConsumer) buildSafeAssignments(
	ctx context.Context,
	tx store.TxAccountScope,
	planID string,
	current planshare.AssignmentReminderSourceSnapshot,
	ev planshare.AssignmentReminderSourceEvent,
) ([]SafeAssignmentInput, error) {
	snaps, err := c.SourceReader.ListAssignmentSourcesForPlanInScope(ctx, tx, planID)
	if err != nil {
		return nil, err
	}
	out := make([]SafeAssignmentInput, 0, len(snaps))
	found := false
	for _, s := range snaps {
		if s.AssignmentID == ev.AssignmentID {
			found = true
			// Prefer authoritative current snapshot; never revive a superseded activation.
			out = append(out, snapshotToSafe(s, ev.OccurredAt))
			continue
		}
		out = append(out, snapshotToSafe(s, s.ClaimedAt))
	}
	if !found && current.AssignmentID == ev.AssignmentID {
		out = append(out, snapshotToSafe(current, ev.OccurredAt))
	}
	return out, nil
}

func snapshotToSafe(s planshare.AssignmentReminderSourceSnapshot, occurred time.Time) SafeAssignmentInput {
	state := SourceStateActive
	if s.Status != planshare.AssignmentStatusActive {
		state = SourceStateRevoked
	}
	var display *string
	if s.ClaimedByDisplayNameSnapshot != "" {
		display = &s.ClaimedByDisplayNameSnapshot
	}
	return SafeAssignmentInput{
		AssignmentID: s.AssignmentID, AssignmentRevision: s.AssignmentRevision,
		AssignmentKind: string(s.AssignmentKind), SourceState: state,
		ReadinessItemID: s.ReadinessItemID, ContentSnapshot: s.ContentSnapshot,
		ClaimedByDisplayNameSnapshot: display, ContentFingerprint: s.ContentFingerprint,
		PreparationLeadDaysSnapshot: s.PreparationLeadDaysSnapshot, LeadRuleVersion: s.LeadRuleVersion,
		SourceOccurredAt: occurred.UTC(),
	}
}

func verifyWorkEventIdentity(work planningreminder.GenerationWork, ev planshare.AssignmentReminderSourceEvent) error {
	if work.PlanID != ev.PlanID ||
		work.Generation != ev.AccountSourceGeneration ||
		string(work.MutationKind) != ev.EventKind ||
		work.SourceEventID == nil || *work.SourceEventID != ev.EventID {
		return ErrGenerationIdentityMismatch
	}
	return nil
}

func ensureResolutionAndApply(
	ctx context.Context,
	tx store.TxAccountScope,
	locked planningreminder.LockedFenceTx,
	work planningreminder.GenerationWork,
	ev planshare.AssignmentReminderSourceEvent,
	kind string,
) error {
	exists, err := tx.Exists(ctx, "planning_reminder_generation_resolutions", "generation = $2", work.Generation)
	if err != nil {
		return err
	}
	if !exists {
		if err := WriteGenerationResolutionInScope(ctx, tx, GenerationResolutionInput{
			Generation: work.Generation, PlanID: work.PlanID, MutationKind: work.MutationKind,
			ResolutionKind: kind, SourceEventID: work.SourceEventID,
		}); err != nil {
			return err
		}
	}
	return MarkAppliedAfterResolution(ctx, locked, tx, work.Generation)
}

func loadProjectedAssignmentMeta(ctx context.Context, tx store.TxAccountScope, planID, assignmentID string) (int64, string, error) {
	var rev int64
	var fp string
	err := tx.QueryRow(ctx, "plan_assignment_reminder_sources",
		"assignment_revision, content_fingerprint",
		"plan_id = $2 AND assignment_id = $3", planID, assignmentID).Scan(&rev, &fp)
	if errors.Is(err, store.ErrNoRows) {
		return 0, "", nil
	}
	return rev, fp, err
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// RepairQuarantinedAssignment rebuilds from the safe current snapshot.
func (c AssignmentEventConsumer) RepairQuarantinedAssignment(
	ctx context.Context,
	scope store.AccountScope,
	sourceEventID string,
) error {
	if c.SourceReader == nil || c.Facts == nil {
		return errors.New("assignment repair incomplete")
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	return scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		locked, err := tx.PlanningReminderFence().LockCurrentAccount(ctx)
		if err != nil {
			return err
		}
		q, err := LoadQuarantineInScope(ctx, tx, sourceEventID)
		if err != nil {
			return err
		}
		if q.ResolvedAt != nil {
			return nil
		}
		ev, err := c.SourceReader.LoadSourceEventInScope(ctx, tx, sourceEventID)
		if err != nil {
			return err
		}
		workReader := tx.PlanningGenerationWorkReader()
		if c.WorkReader != nil {
			workReader = c.WorkReader
		}
		work, err := workReader.LoadWork(ctx, q.AccountSourceGeneration)
		if err != nil {
			return err
		}
		if work.State != planningreminder.WorkQuarantined && work.State != planningreminder.WorkPending {
			return errors.New("generation work not repairable")
		}
		current, err := c.SourceReader.LoadCurrentAssignmentSourceInScope(ctx, tx, planshare.AssignmentSourceRef{
			PlanID: ev.PlanID, AssignmentID: ev.AssignmentID,
		})
		if err != nil {
			return err
		}
		// Rebuild must validate against current fingerprint authority.
		if current.AssignmentRevision < ev.AssignmentRevision {
			return errors.New("current assignment revision behind quarantined event")
		}
		assignments, err := c.buildSafeAssignments(ctx, tx, ev.PlanID, current, ev)
		if err != nil {
			return err
		}
		sidecar, timezone, err := c.Facts.LoadPlanFactsInScope(ctx, tx, ev.PlanID, c.Now())
		if err != nil {
			return err
		}
		if _, err := ReconcilePlanProjection(ctx, tx, c.Projection, ReduceDesiredReminderGroupsInput{
			AccountID: tx.AccountID(), PlanID: ev.PlanID, Assignments: assignments,
			Sidecar: sidecar, Timezone: timezone, Now: c.Now().UTC(),
			ActivationGeneration: work.Generation,
		}); err != nil {
			return err
		}
		if err := UpsertAssignmentInboxInScope(ctx, tx, AssignmentInboxRecord{
			SourceEventID: ev.EventID, PlanID: ev.PlanID, AssignmentID: ev.AssignmentID,
			AssignmentRevision: current.AssignmentRevision, AccountSourceGeneration: work.Generation,
			EventKind: ev.EventKind, PayloadFingerprint: current.ContentFingerprint,
			ResolutionKind: InboxResolutionRebuildVerified,
		}); err != nil {
			return err
		}
		if err := WriteGenerationResolutionInScope(ctx, tx, GenerationResolutionInput{
			Generation: work.Generation, PlanID: work.PlanID, MutationKind: work.MutationKind,
			ResolutionKind: ResolutionRebuildVerified, SourceEventID: work.SourceEventID,
		}); err != nil {
			return err
		}
		if err := ResolveQuarantineInScope(ctx, tx, sourceEventID, ResolutionRebuildVerified); err != nil {
			return err
		}
		return MarkAppliedAfterResolution(ctx, locked, tx, work.Generation)
	})
}

// DefaultPlanFactReader reads CRM sidecar + settings timezone without PII.
type DefaultPlanFactReader struct{}

func (DefaultPlanFactReader) LoadPlanFactsInScope(
	ctx context.Context,
	tx store.TxAccountScope,
	planID string,
	now time.Time,
) (PlanOrderSlotSidecar, string, error) {
	var archivedAt sql.NullTime
	var status string
	err := tx.QueryRow(ctx, "shoot_plans", "status, archived_at", "id = $2", planID).
		Scan(&status, &archivedAt)
	if err != nil {
		return PlanOrderSlotSidecar{}, "", err
	}
	sidecar := PlanOrderSlotSidecar{PlanArchived: archivedAt.Valid || status == "archived"}

	var orderID sql.NullString
	_ = tx.QueryRow(ctx, "plan_crm_connections", "order_id", "plan_id = $2", planID).Scan(&orderID)
	if orderID.Valid {
		value := orderID.String
		sidecar.OrderID = &value
		var orderStatus string
		if err := tx.QueryRow(ctx, "orders", "status", "id = $2", value).Scan(&orderStatus); err != nil {
			return PlanOrderSlotSidecar{}, "", err
		}
		sidecar.OrderActive = orderStatus != "cancelled" && orderStatus != "deleted"
	}

	var slotID, slotType sql.NullString
	var startAt sql.NullTime
	_ = tx.QueryRow(ctx, "plan_schedule_projections",
		"slot_id, starts_at", "plan_id = $2", planID).Scan(&slotID, &startAt)
	if slotID.Valid {
		value := slotID.String
		sidecar.SlotID = &value
		if err := tx.QueryRow(ctx, "schedule_slots", "type, start_at", "id = $2", value).
			Scan(&slotType, &startAt); err == nil {
			sidecar.SlotType = slotType.String
			if startAt.Valid {
				t := startAt.Time.UTC()
				sidecar.SlotStartAt = &t
			}
		}
	} else if orderID.Valid {
		// Fallback: earliest future shoot slot on the linked order.
		rows, err := tx.Query(ctx, "schedule_slots",
			"id, type, start_at",
			"order_id = $2 AND type = $3", orderID.String, "shoot")
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var id, typ string
				var start time.Time
				if err := rows.Scan(&id, &typ, &start); err != nil {
					break
				}
				start = start.UTC()
				if start.After(now) {
					sidecar.SlotID = &id
					sidecar.SlotType = typ
					sidecar.SlotStartAt = &start
					break
				}
			}
		}
	}

	timezone := "Asia/Shanghai"
	var tz sql.NullString
	if err := tx.QueryRow(ctx, "settings", "timezone", "TRUE").Scan(&tz); err == nil && tz.Valid && tz.String != "" {
		timezone = tz.String
	}
	return sidecar, timezone, nil
}
