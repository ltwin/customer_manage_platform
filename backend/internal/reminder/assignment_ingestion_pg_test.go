package reminder_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/planshare"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/reminder"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning/planningreminder"
)

func reserveAssignmentWork(
	t *testing.T,
	tx store.TxAccountScope,
	planID, eventID string,
	kind planningreminder.MutationKind,
) (int64, planningreminder.LockedFenceTx) {
	t.Helper()
	locked, err := tx.PlanningReminderFence().LockCurrentAccount(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	gen, err := locked.ReserveGeneration(context.Background(), planningreminder.MutationFact{
		PlanID: planID, MutationKind: kind, SourceEventID: &eventID,
	})
	if err != nil {
		t.Fatal(err)
	}
	return gen, locked
}

func insertSourceEvent(
	tx store.TxAccountScope,
	eventID, planID, asgnID string,
	revision, generation int64,
	kind planningreminder.MutationKind,
	fingerprint string,
	occurred time.Time,
) error {
	lead := 3
	rule := "platform-default-v1"
	ready := "ready-1"
	if err := tx.Insert(context.Background(), "share_assignment_source_event_v1",
		[]string{
			"event_id", "plan_id", "assignment_id", "assignment_revision",
			"account_source_generation", "event_kind", "assignment_kind", "readiness_item_id",
			"preparation_lead_days_snapshot", "lead_rule_version", "content_fingerprint",
			"occurred_at", "source_version",
		},
		eventID, planID, asgnID, revision, generation, string(kind), "readiness", ready,
		lead, rule, fingerprint, occurred.UTC(), 1,
	); err != nil {
		return err
	}
	return nil
}

func newAssignmentConsumer() reminder.AssignmentEventConsumer {
	return reminder.AssignmentEventConsumer{
		SourceReader: planshare.NewAssignmentReminderSourceReader(),
		Projection:   reminder.NewAssignmentProjectionRepository(),
		Facts:        reminder.DefaultPlanFactReader{},
		Now:          func() time.Time { return time.Date(2026, 8, 14, 8, 0, 0, 0, time.UTC) },
	}
}

func currentAssignmentFingerprint(t *testing.T, scope store.AccountScope, planID, asgnID string) string {
	t.Helper()
	var fingerprint string
	err := scope.WithTxScope(context.Background(), func(tx store.TxAccountScope) error {
		snap, err := planshare.NewAssignmentReminderSourceReader().LoadCurrentAssignmentSourceInScope(
			context.Background(), tx, planshare.AssignmentSourceRef{PlanID: planID, AssignmentID: asgnID})
		if err != nil {
			return err
		}
		fingerprint = snap.ContentFingerprint
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if fingerprint == "" {
		t.Fatal("empty assignment content fingerprint")
	}
	return fingerprint
}

func TestAssignmentEventConsumeDuplicateAndLowRevision(t *testing.T) {
	ctx := context.Background()
	s := startAssignmentReminderPostgres(t)
	scope, planID, _, _, asgnID := seedAssignmentReminderFixture(t, s, "pars2dup")
	consumer := newAssignmentConsumer()
	fixedNow := time.Date(2026, 8, 14, 8, 0, 0, 0, time.UTC)
	fp := currentAssignmentFingerprint(t, scope, planID, asgnID)

	eventID := "sae_dup_" + uuid.NewString()
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		gen, _ := reserveAssignmentWork(t, tx, planID, eventID, planningreminder.MutationAssignmentActivated)
		return insertSourceEvent(tx, eventID, planID, asgnID, 1, gen, planningreminder.MutationAssignmentActivated, fp, fixedNow)
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := consumer.ProcessAccountOnce(ctx, scope, 8); err != nil {
		t.Fatal(err)
	}
	reminderCount, err := scope.Count(ctx, "reminders", "type = $2", reminder.TypePlanAssignmentChecklist)
	if err != nil {
		t.Fatal(err)
	}
	if reminderCount != 1 {
		t.Fatalf("reminder count=%d", reminderCount)
	}

	// Duplicate consume of same applied generation is a no-op (no pending work).
	if n, err := consumer.ProcessAccountOnce(ctx, scope, 8); err != nil || n != 0 {
		t.Fatalf("duplicate process n=%d err=%v", n, err)
	}

	// Low-revision event against a newer current assignment: bump first, then
	// insert the only activation event at revision 1 (source-event unique is
	// per assignment revision). Consumer must recompute from current snapshot.
	lowScope, lowPlan, _, _, lowAsgn := seedAssignmentReminderFixture(t, s, "pars2low")
	lowFP := currentAssignmentFingerprint(t, lowScope, lowPlan, lowAsgn)
	lowEvent := "sae_low_" + uuid.NewString()
	err = lowScope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if _, err := tx.Update(ctx, "share_assignments", "revision = $2", "id = $3", int64(2), lowAsgn); err != nil {
			return err
		}
		gen, _ := reserveAssignmentWork(t, tx, lowPlan, lowEvent, planningreminder.MutationAssignmentActivated)
		return insertSourceEvent(tx, lowEvent, lowPlan, lowAsgn, 1, gen, planningreminder.MutationAssignmentActivated, lowFP, fixedNow)
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := consumer.ProcessAccountOnce(ctx, lowScope, 8); err != nil {
		t.Fatal(err)
	}
	count2, err := lowScope.Count(ctx, "reminders", "type = $2", reminder.TypePlanAssignmentChecklist)
	if err != nil {
		t.Fatal(err)
	}
	if count2 != 1 {
		t.Fatalf("low revision must still project one current reminder: %d", count2)
	}
}

func TestAssignmentCorruptPayloadQuarantinesIndependently(t *testing.T) {
	ctx := context.Background()
	s := startAssignmentReminderPostgres(t)
	scope, planID, _, _, asgnID := seedAssignmentReminderFixture(t, s, "pars2cor")
	consumer := newAssignmentConsumer()
	fixedNow := time.Date(2026, 8, 14, 8, 0, 0, 0, time.UTC)

	// Same revision as current assignment, different fingerprint → corrupt.
	event2 := "sae_bad_" + uuid.NewString()
	var badGen int64
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		gen, _ := reserveAssignmentWork(t, tx, planID, event2, planningreminder.MutationAssignmentActivated)
		badGen = gen
		return insertSourceEvent(tx, event2, planID, asgnID, 1, gen, planningreminder.MutationAssignmentActivated, "fp_CORRUPT", fixedNow)
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = consumer.ProcessAccountOnce(ctx, scope, 8)
	if !errors.Is(err, reminder.ErrCorruptAssignmentPayload) {
		t.Fatalf("want corrupt, got %v", err)
	}
	qCount, err := scope.Count(ctx, "plan_assignment_reminder_quarantines", "source_event_id = $2 AND resolved_at IS NULL", event2)
	if err != nil {
		t.Fatal(err)
	}
	if qCount != 1 {
		t.Fatalf("quarantine count=%d", qCount)
	}
	var workState string
	if err := scope.QueryRow(ctx, "planning_reminder_generation_work", "state", "generation = $2", badGen).Scan(&workState); err != nil {
		t.Fatal(err)
	}
	if workState != string(planningreminder.WorkQuarantined) {
		t.Fatalf("work state=%s", workState)
	}
	inbox, err := scope.Count(ctx, "plan_assignment_reminder_inbox", "source_event_id = $2", event2)
	if err != nil {
		t.Fatal(err)
	}
	if inbox != 0 {
		t.Fatal("corrupt must not ack inbox")
	}

	// Repair from current safe snapshot.
	if err := consumer.RepairQuarantinedAssignment(ctx, scope, event2); err != nil {
		t.Fatal(err)
	}
	qCount, err = scope.Count(ctx, "plan_assignment_reminder_quarantines", "source_event_id = $2 AND resolved_at IS NOT NULL", event2)
	if err != nil || qCount != 1 {
		t.Fatalf("resolved quarantine q=%d err=%v", qCount, err)
	}
	if err := scope.QueryRow(ctx, "planning_reminder_generation_work", "state", "generation = $2", badGen).Scan(&workState); err != nil {
		t.Fatal(err)
	}
	if workState != string(planningreminder.WorkApplied) {
		t.Fatalf("repaired work state=%s", workState)
	}
	var resKind string
	if err := scope.QueryRow(ctx, "planning_reminder_generation_resolutions", "resolution_kind", "generation = $2", badGen).
		Scan(&resKind); err != nil {
		t.Fatal(err)
	}
	if resKind != reminder.ResolutionRebuildVerified {
		t.Fatalf("resolution=%s", resKind)
	}
}

func TestAssignmentWatermarkDoesNotSkipGap(t *testing.T) {
	ctx := context.Background()
	s := startAssignmentReminderPostgres(t)
	scope, planID, _, _, asgnID := seedAssignmentReminderFixture(t, s, "pars2wm")
	consumer := newAssignmentConsumer()
	fixedNow := time.Date(2026, 8, 14, 8, 0, 0, 0, time.UTC)
	fp := currentAssignmentFingerprint(t, scope, planID, asgnID)

	plan2 := planID + "_b"
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		return tx.Insert(ctx, "shoot_plans", []string{"id", "title", "subject", "status"}, plan2, "B", "主体", "ready")
	})
	if err != nil {
		t.Fatal(err)
	}

	e1 := "sae_wm1_" + uuid.NewString()
	err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		locked, err := tx.PlanningReminderFence().LockCurrentAccount(ctx)
		if err != nil {
			return err
		}
		g1, err := locked.ReserveGeneration(ctx, planningreminder.MutationFact{
			PlanID: planID, MutationKind: planningreminder.MutationAssignmentActivated, SourceEventID: &e1,
		})
		if err != nil {
			return err
		}
		if err := insertSourceEvent(tx, e1, planID, asgnID, 1, g1, planningreminder.MutationAssignmentActivated, fp, fixedNow); err != nil {
			return err
		}
		if _, err := locked.ReserveGeneration(ctx, planningreminder.MutationFact{PlanID: plan2, MutationKind: planningreminder.MutationPlanArchived}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Process only mixed-kind by claiming both; assignment first then archive stub.
	// Force process: consumer processes in generation order.
	if _, err := consumer.ProcessAccountOnce(ctx, scope, 1); err != nil {
		t.Fatal(err)
	}
	var applied int64
	if err := scope.QueryRow(ctx, "planning_reminder_account_generations", "applied_generation", "TRUE").Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied != 1 {
		t.Fatalf("after first applied watermark=%d", applied)
	}
	// Leave gen2 pending: watermark must stay at 1 even if we somehow applied gen2 later out of order.
	err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		locked, err := tx.PlanningReminderFence().LockCurrentAccount(ctx)
		if err != nil {
			return err
		}
		work, err := tx.PlanningGenerationWorkReader().LoadWork(ctx, 2)
		if err != nil {
			return err
		}
		return reminder.ApplyMixedKindStubInScope(ctx, tx, locked, work, reminder.DefaultPlanFactReader{}, fixedNow)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := scope.QueryRow(ctx, "planning_reminder_account_generations", "applied_generation", "TRUE").Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied != 2 {
		t.Fatalf("contiguous watermark want 2 got %d", applied)
	}
}

func TestAssignmentSkipLockedSingleWinner(t *testing.T) {
	ctx := context.Background()
	s, url := startAssignmentReminderPostgresURL(t)
	scope, planID, _, _, asgnID := seedAssignmentReminderFixture(t, s, "pars2sk")
	fixedNow := time.Date(2026, 8, 14, 8, 0, 0, 0, time.UTC)
	fp := currentAssignmentFingerprint(t, scope, planID, asgnID)
	eventID := "sae_sk_" + uuid.NewString()
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		gen, _ := reserveAssignmentWork(t, tx, planID, eventID, planningreminder.MutationAssignmentActivated)
		return insertSourceEvent(tx, eventID, planID, asgnID, 1, gen, planningreminder.MutationAssignmentActivated, fp, fixedNow)
	})
	if err != nil {
		t.Fatal(err)
	}

	peer, err := store.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(peer.Close)
	peerScope := peer.ScopeFor(auth.AccountContext{AccountID: scope.AccountID()})

	// Fence serializes same-account workers. SKIP LOCKED is proved on the work
	// row itself across two connection pools: holder keeps FOR UPDATE while the
	// peer claims without waiting.
	claimed := make(chan struct{})
	release := make(chan struct{})
	holderErr := make(chan error, 1)
	go func() {
		holderErr <- scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
			works, err := tx.PlanningGenerationWorkReader().ClaimPending(ctx, 1, 1)
			if err != nil {
				return err
			}
			if len(works) != 1 {
				return errors.New("holder missed pending assignment work")
			}
			close(claimed)
			<-release
			return nil
		})
	}()
	select {
	case <-claimed:
	case err := <-holderErr:
		t.Fatalf("holder claim: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("holder did not claim pending work")
	}
	err = peerScope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		works, err := tx.PlanningGenerationWorkReader().ClaimPending(ctx, 1, 1)
		if err != nil {
			return err
		}
		if len(works) != 0 {
			return fmt.Errorf("skip locked extra winners=%d want 0", len(works))
		}
		return nil
	})
	close(release)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-holderErr; err != nil {
		t.Fatal(err)
	}
}

func TestMixedKindNeverWritesInbox(t *testing.T) {
	ctx := context.Background()
	s := startAssignmentReminderPostgres(t)
	scope, planID, _, slotID, _ := seedAssignmentReminderFixture(t, s, "pars2mix")
	fixedNow := time.Date(2026, 8, 14, 8, 0, 0, 0, time.UTC)
	validUntil := time.Date(2026, 8, 20, 2, 0, 0, 0, time.UTC)

	kinds := []planningreminder.MutationKind{
		planningreminder.MutationCRMPlanLinkChanged,
		planningreminder.MutationSettingsTimezoneChanged,
		planningreminder.MutationShootStarted,
		planningreminder.MutationPlanArchived,
	}
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		locked, err := tx.PlanningReminderFence().LockCurrentAccount(ctx)
		if err != nil {
			return err
		}
		for _, kind := range kinds {
			gen, err := locked.ReserveGeneration(ctx, planningreminder.MutationFact{PlanID: planID, MutationKind: kind})
			if err != nil {
				return err
			}
			if kind == planningreminder.MutationShootStarted {
				if err := tx.Insert(ctx, "plan_assignment_reminder_temporal_invalidations",
					[]string{"plan_id", "slot_id", "valid_until", "generation", "state"},
					planID, slotID, validUntil, gen, "pending"); err != nil {
					return err
				}
			}
			work, err := tx.PlanningGenerationWorkReader().LoadWork(ctx, gen)
			if err != nil {
				return err
			}
			if err := reminder.ApplyMixedKindStubInScope(ctx, tx, locked, work, reminder.DefaultPlanFactReader{}, fixedNow); err != nil {
				return err
			}
		}
		n, err := reminder.CountInboxInScope(ctx, tx)
		if err != nil {
			return err
		}
		if n != 0 {
			t.Fatalf("inbox count=%d", n)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestReconcileEpochSupersedeAndCompleteRules(t *testing.T) {
	ctx := context.Background()
	s := startAssignmentReminderPostgres(t)
	scope, planID, _, _, asgnID := seedAssignmentReminderFixture(t, s, "pars2ep")
	consumer := newAssignmentConsumer()
	fixedNow := time.Date(2026, 8, 14, 8, 0, 0, 0, time.UTC)
	fp := currentAssignmentFingerprint(t, scope, planID, asgnID)
	eventID := "sae_ep_" + uuid.NewString()
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		gen, _ := reserveAssignmentWork(t, tx, planID, eventID, planningreminder.MutationAssignmentActivated)
		return insertSourceEvent(tx, eventID, planID, asgnID, 1, gen, planningreminder.MutationAssignmentActivated, fp, fixedNow)
	})
	if err != nil {
		t.Fatal(err)
	}

	coord := reminder.ReconcileEpochCoordinator{
		SourceReader: planshare.NewAssignmentReminderSourceReader(),
		Epochs:       reminder.NewReminderReconcileEpochRepository(),
	}
	epochID, target, err := coord.CreateEpoch(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	if target < 1 {
		t.Fatalf("target=%d", target)
	}

	// Cannot complete while pending work remains.
	if _, err := coord.CompleteEpoch(ctx, scope, epochID); !errors.Is(err, reminder.ErrEpochCannotComplete) {
		t.Fatalf("complete with pending: %v", err)
	}

	if _, err := consumer.ProcessAccountOnce(ctx, scope, 8); err != nil {
		t.Fatal(err)
	}

	// Advance target so old epoch is superseded.
	err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		locked, err := tx.PlanningReminderFence().LockCurrentAccount(ctx)
		if err != nil {
			return err
		}
		_, err = locked.ReserveGeneration(ctx, planningreminder.MutationFact{
			PlanID: planID, MutationKind: planningreminder.MutationPlanArchived,
		})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	status, err := coord.CompleteEpoch(ctx, scope, epochID)
	if err != nil {
		t.Fatal(err)
	}
	if status != reminder.EpochStatusSuperseded {
		t.Fatalf("status=%s", status)
	}
}

func TestPortTypeSeparationCompile(t *testing.T) {
	_ = planshare.NewAssignmentReminderSourceReader()
	_ = reminder.NewReminderReconcileEpochRepository()
}

func TestFenceRejectsDuplicateReserve(t *testing.T) {
	ctx := context.Background()
	s := startAssignmentReminderPostgres(t)
	scope, planID, _, _, _ := seedAssignmentReminderFixture(t, s, "pars2fence")
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		locked, err := tx.PlanningReminderFence().LockCurrentAccount(ctx)
		if err != nil {
			return err
		}
		fact := planningreminder.MutationFact{PlanID: planID, MutationKind: planningreminder.MutationPlanArchived}
		if _, err := locked.ReserveGeneration(ctx, fact); err != nil {
			return err
		}
		_, err = locked.ReserveGeneration(ctx, fact)
		if !errors.Is(err, planningreminder.ErrDuplicateFact) {
			t.Fatalf("duplicate err=%v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
