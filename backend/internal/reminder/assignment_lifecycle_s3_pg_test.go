package reminder_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/reminder"
	"github.com/samson/customer-manage-platform/backend/internal/settings"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning/crm"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning/planningreminder"
)

func TestCRMLifecycleAdapterLinkUnlinkRelinkAndReschedule(t *testing.T) {
	s := startAssignmentReminderPostgres(t)
	scope, planID, orderID, slotID, _ := seedAssignmentReminderFixture(t, s, "s3-crm-link")
	ctx := context.Background()
	fixedNow := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	adapter := reminder.NewCRMReminderLifecycleAdapter(nil)
	engine, err := crm.NewEngine().WithReminder(adapter, true)
	if err != nil {
		t.Fatal(err)
	}
	_ = engine

	// Direct link path: schedule fact with future shoot slot → create group.
	err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		locked, err := tx.PlanningReminderFence().LockCurrentAccount(ctx)
		if err != nil {
			return err
		}
		gen, err := locked.ReserveGeneration(ctx, planningreminder.MutationFact{
			PlanID: planID, MutationKind: planningreminder.MutationCRMPlanLinkChanged,
		})
		if err != nil {
			return err
		}
		status := "scheduled"
		slotStart := time.Date(2026, 8, 20, 2, 0, 0, 0, time.UTC)
		if err := adapter.RecomputeForCRMFactInScope(ctx, tx, crm.CRMPlanLinkChangedFactV1{
			PlanID: planID, Change: crm.PlanLinkChangeLink,
			OrderID: &orderID, OrderStatus: &status,
			CurrentShootSlot: &crm.ReminderShootSlotFactV1{
				SlotID: slotID, OrderID: orderID, Timezone: "Asia/Shanghai",
				StartsAt: slotStart, EndsAt: slotStart.Add(2 * time.Hour), Type: "shoot",
			},
		}, gen, fixedNow); err != nil {
			return err
		}
		if err := crm.WriteGenerationResolution(ctx, tx, planID, gen); err != nil {
			return err
		}
		return locked.MarkApplied(ctx, gen)
	})
	if err != nil {
		t.Fatalf("link recompute: %v", err)
	}
	assertCurrentGroupCount(t, scope, planID, 1)

	// Unlink → withdraw.
	err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		locked, err := tx.PlanningReminderFence().LockCurrentAccount(ctx)
		if err != nil {
			return err
		}
		gen, err := locked.ReserveGeneration(ctx, planningreminder.MutationFact{
			PlanID: planID, MutationKind: planningreminder.MutationCRMPlanLinkChanged,
		})
		if err != nil {
			return err
		}
		if err := adapter.RecomputeForCRMFactInScope(ctx, tx, crm.CRMPlanLinkChangedFactV1{
			PlanID: planID, Change: crm.PlanLinkChangeUnlink,
		}, gen, fixedNow); err != nil {
			return err
		}
		if err := crm.WriteGenerationResolution(ctx, tx, planID, gen); err != nil {
			return err
		}
		return locked.MarkApplied(ctx, gen)
	})
	if err != nil {
		t.Fatalf("unlink recompute: %v", err)
	}
	assertCurrentGroupCount(t, scope, planID, 0)

	// Relink with same slot → new group.
	err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		locked, err := tx.PlanningReminderFence().LockCurrentAccount(ctx)
		if err != nil {
			return err
		}
		gen, err := locked.ReserveGeneration(ctx, planningreminder.MutationFact{
			PlanID: planID, MutationKind: planningreminder.MutationCRMPlanLinkChanged,
		})
		if err != nil {
			return err
		}
		status := "scheduled"
		slotStart := time.Date(2026, 8, 20, 2, 0, 0, 0, time.UTC)
		if err := adapter.RecomputeForCRMFactInScope(ctx, tx, crm.CRMPlanLinkChangedFactV1{
			PlanID: planID, Change: crm.PlanLinkChangeLink,
			OrderID: &orderID, OrderStatus: &status,
			CurrentShootSlot: &crm.ReminderShootSlotFactV1{
				SlotID: slotID, OrderID: orderID, Timezone: "Asia/Shanghai",
				StartsAt: slotStart, EndsAt: slotStart.Add(2 * time.Hour), Type: "shoot",
			},
		}, gen, fixedNow); err != nil {
			return err
		}
		if err := crm.WriteGenerationResolution(ctx, tx, planID, gen); err != nil {
			return err
		}
		return locked.MarkApplied(ctx, gen)
	})
	if err != nil {
		t.Fatalf("relink recompute: %v", err)
	}
	assertCurrentGroupCount(t, scope, planID, 1)

	// Same-day time change: fingerprint unchanged, only valid_until / validity_revision.
	beforeFP, beforeRev, beforeValid := loadCurrentGroupMeta(t, scope, planID)
	err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		locked, err := tx.PlanningReminderFence().LockCurrentAccount(ctx)
		if err != nil {
			return err
		}
		gen, err := locked.ReserveGeneration(ctx, planningreminder.MutationFact{
			PlanID: planID, MutationKind: planningreminder.MutationCRMScheduleChanged,
		})
		if err != nil {
			return err
		}
		status := "scheduled"
		// Same local date in Asia/Shanghai (UTC+8): 04:00 UTC = 12:00 CST still Aug 20.
		slotStart := time.Date(2026, 8, 20, 4, 0, 0, 0, time.UTC)
		if err := adapter.RecomputeForCRMFactInScope(ctx, tx, crm.CRMScheduleChangedFactV1{
			PlanID: planID, Change: crm.ScheduleUpdate, SourceSlotID: slotID,
			OrderID: &orderID, OrderStatus: &status,
			CurrentShootSlot: &crm.ReminderShootSlotFactV1{
				SlotID: slotID, OrderID: orderID, Timezone: "Asia/Shanghai",
				StartsAt: slotStart, EndsAt: slotStart.Add(2 * time.Hour), Type: "shoot",
			},
		}, gen, fixedNow); err != nil {
			return err
		}
		if err := crm.WriteGenerationResolution(ctx, tx, planID, gen); err != nil {
			return err
		}
		return locked.MarkApplied(ctx, gen)
	})
	if err != nil {
		t.Fatalf("same-day reschedule: %v", err)
	}
	afterFP, afterRev, afterValid := loadCurrentGroupMeta(t, scope, planID)
	if afterFP != beforeFP {
		t.Fatalf("same-day time change must keep fingerprint: before=%s after=%s", beforeFP, afterFP)
	}
	if !afterValid.After(beforeValid) {
		t.Fatalf("valid_until should advance: before=%s after=%s", beforeValid, afterValid)
	}
	if afterRev != beforeRev+1 {
		t.Fatalf("validity_revision want %d got %d", beforeRev+1, afterRev)
	}
}

func TestCRMLifecycleCancelDeleteAndWholeTxRollback(t *testing.T) {
	s := startAssignmentReminderPostgres(t)
	scope, planID, orderID, slotID, _ := seedAssignmentReminderFixture(t, s, "s3-crm-cancel")
	ctx := context.Background()
	fixedNow := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	adapter := reminder.NewCRMReminderLifecycleAdapter(nil)
	seedCurrentGroupViaCRM(t, scope, adapter, planID, orderID, slotID, fixedNow)

	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		locked, err := tx.PlanningReminderFence().LockCurrentAccount(ctx)
		if err != nil {
			return err
		}
		gen, err := locked.ReserveGeneration(ctx, planningreminder.MutationFact{
			PlanID: planID, MutationKind: planningreminder.MutationCRMOrderLifecycleChanged,
		})
		if err != nil {
			return err
		}
		status := "cancelled"
		if err := adapter.RecomputeForCRMFactInScope(ctx, tx, crm.CRMOrderLifecycleChangedFactV1{
			PlanID: planID, Change: crm.OrderLifecycleCancelled, SourceOrderID: orderID,
			CurrentOrderID: &orderID, CurrentOrderStatus: &status,
		}, gen, fixedNow); err != nil {
			return err
		}
		if err := crm.WriteGenerationResolution(ctx, tx, planID, gen); err != nil {
			return err
		}
		return locked.MarkApplied(ctx, gen)
	})
	if err != nil {
		t.Fatalf("cancel recompute: %v", err)
	}
	assertCurrentGroupCount(t, scope, planID, 0)

	// Whole-tx rollback: participant failure rolls back generation/work/resolution.
	scope2, planID2, orderID2, slotID2, _ := seedAssignmentReminderFixture(t, s, "s3-crm-rollback")
	seedCurrentGroupViaCRM(t, scope2, adapter, planID2, orderID2, slotID2, fixedNow)
	rollbackErr := errors.New("forced participant failure")
	err = scope2.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		locked, err := tx.PlanningReminderFence().LockCurrentAccount(ctx)
		if err != nil {
			return err
		}
		if _, err := locked.ReserveGeneration(ctx, planningreminder.MutationFact{
			PlanID: planID2, MutationKind: planningreminder.MutationCRMScheduleChanged,
		}); err != nil {
			return err
		}
		return rollbackErr
	})
	if !errors.Is(err, rollbackErr) {
		t.Fatalf("want forced rollback, got %v", err)
	}
	var pending int64
	_ = scope2.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		pending, err = tx.Count(ctx, "planning_reminder_generation_work", "state = $2", "pending")
		return err
	})
	if pending != 0 {
		t.Fatalf("rolled back tx must not leave pending work, got %d", pending)
	}
	assertCurrentGroupCount(t, scope2, planID2, 1)
}

func TestPlanArchiveReminderExactEffects(t *testing.T) {
	s := startAssignmentReminderPostgres(t)
	scope, planID, orderID, slotID, _ := seedAssignmentReminderFixture(t, s, "s3-archive")
	ctx := context.Background()
	fixedNow := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	crmAdapter := reminder.NewCRMReminderLifecycleAdapter(nil)
	seedCurrentGroupViaCRM(t, scope, crmAdapter, planID, orderID, slotID, fixedNow)

	archive := reminder.NewPlanArchiveReminderAdapter()
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		locked, err := tx.PlanningReminderFence().LockCurrentAccount(ctx)
		if err != nil {
			return err
		}
		gen, err := locked.ReserveGeneration(ctx, planningreminder.MutationFact{
			PlanID: planID, MutationKind: planningreminder.MutationPlanArchived,
		})
		if err != nil {
			return err
		}
		if _, err := tx.Update(ctx, "shoot_plans",
			"status = $2, archived_at = $3, revision = revision + 1",
			"id = $4", "archived", fixedNow, planID); err != nil {
			return err
		}
		return archive.OnPlanArchivedInScope(ctx, tx, planID, 2, gen, fixedNow)
	})
	if err != nil {
		t.Fatalf("archive adapter: %v", err)
	}
	assertCurrentGroupCount(t, scope, planID, 0)
	var remStatus string
	_ = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		return tx.QueryRow(ctx, "reminders", "status", "plan_id = $2", planID).Scan(&remStatus)
	})
	if remStatus != reminder.StatusDismissed {
		t.Fatalf("pending reminder should dismiss on archive, got %s", remStatus)
	}
	var resolution string
	_ = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		return tx.QueryRow(ctx, "planning_reminder_generation_resolutions",
			"resolution_kind", "plan_id = $2 AND mutation_kind = $3",
			planID, string(planningreminder.MutationPlanArchived)).Scan(&resolution)
	})
	if resolution != reminder.ResolutionLifecycleApplied {
		t.Fatalf("archive resolution = %s", resolution)
	}
}

func TestSettingsTimezoneSameIANAZeroGenDifferentZoneRebuilds(t *testing.T) {
	s := startAssignmentReminderPostgres(t)
	scope, planID, orderID, slotID, _ := seedAssignmentReminderFixture(t, s, "s3-tz")
	ctx := context.Background()
	fixedNow := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	crmAdapter := reminder.NewCRMReminderLifecycleAdapter(nil)
	seedCurrentGroupViaCRM(t, scope, crmAdapter, planID, orderID, slotID, fixedNow)

	svc, err := settings.NewService(settings.NewPostgresRepository()).
		WithPlanningReminderTimezone(true, reminder.NewTimezoneChangePlanningParticipant(nil, nil))
	if err != nil {
		t.Fatal(err)
	}

	same := "Asia/Shanghai"
	got, err := svc.Patch(ctx, scope, settings.PatchInput{Timezone: &same})
	if err != nil {
		t.Fatal(err)
	}
	if got.Timezone != same {
		t.Fatalf("timezone = %s", got.Timezone)
	}
	var pending int64
	_ = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		pending, err = tx.Count(ctx, "planning_reminder_generation_work",
			"mutation_kind = $2 AND state = $3",
			string(planningreminder.MutationSettingsTimezoneChanged), "pending")
		return err
	})
	if pending != 0 {
		t.Fatalf("same IANA must reserve 0 generations, got pending=%d", pending)
	}

	beforeFP, beforeRev, _ := loadCurrentGroupMeta(t, scope, planID)
	singapore := "Asia/Singapore"
	if _, err := svc.Patch(ctx, scope, settings.PatchInput{Timezone: &singapore}); err != nil {
		t.Fatal(err)
	}
	_ = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		pending, err = tx.Count(ctx, "planning_reminder_generation_work",
			"mutation_kind = $2 AND state = $3",
			string(planningreminder.MutationSettingsTimezoneChanged), "pending")
		return err
	})
	if pending < 1 {
		t.Fatalf("different IANA must reserve settings work, pending=%d", pending)
	}

	err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		locked, err := tx.PlanningReminderFence().LockCurrentAccount(ctx)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, "planning_reminder_generation_work",
			"generation", "mutation_kind = $2 AND state = $3",
			string(planningreminder.MutationSettingsTimezoneChanged), "pending")
		if err != nil {
			return err
		}
		defer rows.Close()
		gens := make([]int64, 0)
		for rows.Next() {
			var g int64
			if err := rows.Scan(&g); err != nil {
				return err
			}
			gens = append(gens, g)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		for _, gen := range gens {
			work, err := tx.PlanningGenerationWorkReader().LoadWork(ctx, gen)
			if err != nil {
				return err
			}
			if err := reminder.ApplySettingsTimezoneChangedInScope(
				ctx, tx, locked, work, nil, reminder.DefaultPlanFactReader{}, fixedNow,
			); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("settings rebuild: %v", err)
	}
	afterFP, afterRev, _ := loadCurrentGroupMeta(t, scope, planID)
	var tzSnap string
	_ = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		return tx.QueryRow(ctx, "plan_assignment_reminder_groups",
			"timezone_snapshot", "plan_id = $2 AND state = $3", planID, "current").Scan(&tzSnap)
	})
	if tzSnap != singapore {
		t.Fatalf("timezone_snapshot want %s got %s", singapore, tzSnap)
	}
	if afterFP != beforeFP {
		t.Fatalf("timezone-only change should keep fingerprint")
	}
	if afterRev != beforeRev+1 {
		t.Fatalf("validity_revision want %d got %d", beforeRev+1, afterRev)
	}
}

func TestTemporalMarkerWinnerLoserAndOldValidity(t *testing.T) {
	s := startAssignmentReminderPostgres(t)
	scope, planID, orderID, slotID, _ := seedAssignmentReminderFixture(t, s, "s3-temporal")
	ctx := context.Background()
	fixedNow := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	adapter := reminder.NewCRMReminderLifecycleAdapter(nil)
	seedCurrentGroupViaCRM(t, scope, adapter, planID, orderID, slotID, fixedNow)
	_, _, validUntil := loadCurrentGroupMeta(t, scope, planID)

	var winnerGen int64
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		locked, err := tx.PlanningReminderFence().LockCurrentAccount(ctx)
		if err != nil {
			return err
		}
		claim, err := reminder.EnsureTemporalInvalidationInScope(ctx, tx, locked, planID, slotID, validUntil)
		if err != nil {
			return err
		}
		if !claim.Won {
			return errors.New("first constructor must win")
		}
		winnerGen = claim.Generation
		return nil
	})
	if err != nil {
		t.Fatalf("winner: %v", err)
	}

	err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		locked, err := tx.PlanningReminderFence().LockCurrentAccount(ctx)
		if err != nil {
			return err
		}
		_, err = reminder.EnsureTemporalInvalidationInScope(ctx, tx, locked, planID, slotID, validUntil)
		return err
	})
	if !errors.Is(err, reminder.ErrTemporalInvalidationPending) {
		t.Fatalf("loser want pending err, got %v", err)
	}

	// Reschedule to future (new current group) while old marker still pending.
	futureStart := time.Date(2026, 9, 1, 2, 0, 0, 0, time.UTC)
	err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if _, err := tx.Update(ctx, "schedule_slots",
			"start_at = $2, end_at = $3", "id = $4",
			futureStart, futureStart.Add(2*time.Hour), slotID); err != nil {
			return err
		}
		locked, err := tx.PlanningReminderFence().LockCurrentAccount(ctx)
		if err != nil {
			return err
		}
		gen, err := locked.ReserveGeneration(ctx, planningreminder.MutationFact{
			PlanID: planID, MutationKind: planningreminder.MutationCRMScheduleChanged,
		})
		if err != nil {
			return err
		}
		status := "scheduled"
		if err := adapter.RecomputeForCRMFactInScope(ctx, tx, crm.CRMScheduleChangedFactV1{
			PlanID: planID, Change: crm.ScheduleUpdate, SourceSlotID: slotID,
			OrderID: &orderID, OrderStatus: &status,
			CurrentShootSlot: &crm.ReminderShootSlotFactV1{
				SlotID: slotID, OrderID: orderID, Timezone: "Asia/Shanghai",
				StartsAt: futureStart, EndsAt: futureStart.Add(2 * time.Hour), Type: "shoot",
			},
		}, gen, fixedNow); err != nil {
			return err
		}
		if err := crm.WriteGenerationResolution(ctx, tx, planID, gen); err != nil {
			return err
		}
		return locked.MarkApplied(ctx, gen)
	})
	if err != nil {
		t.Fatalf("future reschedule: %v", err)
	}
	assertCurrentGroupCount(t, scope, planID, 1)
	newFP, _, newValid := loadCurrentGroupMeta(t, scope, planID)
	if !newValid.Equal(futureStart) {
		t.Fatalf("new valid_until=%s want %s", newValid, futureStart)
	}

	err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		locked, err := tx.PlanningReminderFence().LockCurrentAccount(ctx)
		if err != nil {
			return err
		}
		work, err := tx.PlanningGenerationWorkReader().LoadWork(ctx, winnerGen)
		if err != nil {
			return err
		}
		return reminder.ApplyTemporalShootStartedInScope(ctx, tx, locked, work, fixedNow)
	})
	if err != nil {
		t.Fatalf("apply old temporal marker: %v", err)
	}
	// New current group (future validity) must remain.
	assertCurrentGroupCount(t, scope, planID, 1)
	stillFP, _, stillValid := loadCurrentGroupMeta(t, scope, planID)
	if stillFP != newFP || !stillValid.Equal(newValid) {
		t.Fatalf("old marker must not withdraw new current group")
	}
	_ = orderID
}

func TestMultiPlanTimezoneGenerationsContiguous(t *testing.T) {
	s := startAssignmentReminderPostgres(t)
	scope1, plan1, order1, slot1, _ := seedAssignmentReminderFixture(t, s, "s3-mp-a")
	// Second plan in same account.
	ctx := context.Background()
	fixedNow := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	plan2 := "plan_s3-mp-b"
	order2 := "ord_s3-mp-b"
	slot2 := "slot_s3-mp-b"
	asgn2 := "asgn_s3-mp-b"
	cust2 := "cust_s3-mp-b"
	gen2 := "gen_s3-mp-b"
	commitment := make([]byte, 32)
	receipt := make([]byte, 32)
	slotStart := time.Date(2026, 8, 21, 2, 0, 0, 0, time.UTC)
	err := scope1.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.Insert(ctx, "customers", []string{"id", "display_name", "channel", "status"},
			cust2, "客户乙", "other", "active"); err != nil {
			return err
		}
		if err := tx.Insert(ctx, "shoot_plans", []string{"id", "title", "subject", "status"},
			plan2, "策划2", "主体2", "ready"); err != nil {
			return err
		}
		if err := tx.Insert(ctx, "orders", []string{"id", "customer_id", "title", "status"},
			order2, cust2, "成片2", "scheduled"); err != nil {
			return err
		}
		if err := tx.Insert(ctx, "schedule_slots", []string{"id", "start_at", "end_at", "type", "order_id"},
			slot2, slotStart, slotStart.Add(2*time.Hour), "shoot", order2); err != nil {
			return err
		}
		expires := time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
		issued := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
		if err := tx.Insert(ctx, "share_generations",
			[]string{"id", "plan_id", "view_level", "generation", "selector", "secret_commitment", "fingerprint", "state", "expires_at", "issued_at", "revision"},
			gen2, plan2, "proposal", int64(1), "sel-b", commitment, "fp-b", "active", expires, issued, int64(1)); err != nil {
			return err
		}
		if err := tx.Insert(ctx, "share_assignments",
			[]string{"id", "plan_id", "token_generation_id", "assignment_kind", "readiness_item_id",
				"content_snapshot", "claimed_by_display_name", "preparation_lead_days_snapshot",
				"lead_rule_version", "status", "claim_receipt_commitment", "revision"},
			asgn2, plan2, gen2, "readiness", "ready-2", "妆造2", "昵称乙", 3, "platform-default-v1", "active", receipt, int64(1)); err != nil {
			return err
		}
		snapshot := []byte(`{"order_id":"` + order2 + `","customer_id":"` + cust2 + `","status_at_link":"scheduled","linked_at":"2026-08-01T00:00:00Z"}`)
		if err := tx.Insert(ctx, "plan_crm_connections",
			[]string{"plan_id", "customer_id", "order_id", "link_epoch_id", "linked_order_snapshot", "state", "connection_revision", "next_event_seq"},
			plan2, cust2, order2, "ple_b", snapshot, "order_linked", int64(1), int64(1)); err != nil {
			return err
		}
		return tx.Insert(ctx, "plan_schedule_projections",
			[]string{"plan_id", "order_id", "slot_id", "slot_source_fingerprint", "starts_at", "ends_at", "timezone", "status", "apply_suppressed", "rule_version", "projection_revision"},
			plan2, order2, slot2, "fp-slot-b", slotStart, slotStart.Add(2*time.Hour), "Asia/Shanghai", "active_applied", false, 1, int64(1))
	})
	if err != nil {
		t.Fatal(err)
	}
	adapter := reminder.NewCRMReminderLifecycleAdapter(nil)
	seedCurrentGroupViaCRM(t, scope1, adapter, plan1, order1, slot1, fixedNow)
	seedCurrentGroupViaCRM(t, scope1, adapter, plan2, order2, slot2, fixedNow)

	svc, err := settings.NewService(settings.NewPostgresRepository()).
		WithPlanningReminderTimezone(true, reminder.NewTimezoneChangePlanningParticipant(nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	tz := "Asia/Tokyo"
	if _, err := svc.Patch(ctx, scope1, settings.PatchInput{Timezone: &tz}); err != nil {
		t.Fatal(err)
	}
	var gens []int64
	_ = scope1.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		rows, err := tx.Query(ctx, "planning_reminder_generation_work",
			"generation", "mutation_kind = $2", string(planningreminder.MutationSettingsTimezoneChanged))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var g int64
			if err := rows.Scan(&g); err != nil {
				return err
			}
			gens = append(gens, g)
		}
		return rows.Err()
	})
	if len(gens) < 2 {
		t.Fatalf("want >=2 settings generations, got %v", gens)
	}
	if gens[1] != gens[0]+1 && gens[0] != gens[1]+1 {
		// Contiguous pair regardless of scan order.
		a, b := gens[0], gens[1]
		if a > b {
			a, b = b, a
		}
		if b != a+1 {
			t.Fatalf("generations not contiguous: %v", gens)
		}
	}
}

func seedCurrentGroupViaCRM(
	t *testing.T,
	scope store.AccountScope,
	adapter *reminder.CRMReminderLifecycleAdapter,
	planID, orderID, slotID string,
	now time.Time,
) {
	t.Helper()
	err := scope.WithTxScope(context.Background(), func(tx store.TxAccountScope) error {
		locked, err := tx.PlanningReminderFence().LockCurrentAccount(context.Background())
		if err != nil {
			return err
		}
		gen, err := locked.ReserveGeneration(context.Background(), planningreminder.MutationFact{
			PlanID: planID, MutationKind: planningreminder.MutationCRMPlanLinkChanged,
		})
		if err != nil {
			return err
		}
		status := "scheduled"
		slotStart := time.Date(2026, 8, 20, 2, 0, 0, 0, time.UTC)
		if planID == "plan_s3-mp-b" {
			slotStart = time.Date(2026, 8, 21, 2, 0, 0, 0, time.UTC)
		}
		if err := adapter.RecomputeForCRMFactInScope(context.Background(), tx, crm.CRMPlanLinkChangedFactV1{
			PlanID: planID, Change: crm.PlanLinkChangeLink,
			OrderID: &orderID, OrderStatus: &status,
			CurrentShootSlot: &crm.ReminderShootSlotFactV1{
				SlotID: slotID, OrderID: orderID, Timezone: "Asia/Shanghai",
				StartsAt: slotStart, EndsAt: slotStart.Add(2 * time.Hour), Type: "shoot",
			},
		}, gen, now); err != nil {
			return err
		}
		if err := crm.WriteGenerationResolution(context.Background(), tx, planID, gen); err != nil {
			return err
		}
		return locked.MarkApplied(context.Background(), gen)
	})
	if err != nil {
		t.Fatalf("seed current group: %v", err)
	}
}

func assertCurrentGroupCount(t *testing.T, scope store.AccountScope, planID string, want int64) {
	t.Helper()
	var n int64
	err := scope.WithTxScope(context.Background(), func(tx store.TxAccountScope) error {
		var err error
		n, err = tx.Count(context.Background(), "plan_assignment_reminder_groups",
			"plan_id = $2 AND state = $3", planID, "current")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != want {
		t.Fatalf("current groups for %s: want %d got %d", planID, want, n)
	}
}

func loadCurrentGroupMeta(t *testing.T, scope store.AccountScope, planID string) (fp string, rev int64, validUntil time.Time) {
	t.Helper()
	err := scope.WithTxScope(context.Background(), func(tx store.TxAccountScope) error {
		return tx.QueryRow(context.Background(), "plan_assignment_reminder_groups",
			"group_fingerprint, validity_revision, valid_until",
			"plan_id = $2 AND state = $3", planID, "current",
		).Scan(&fp, &rev, &validUntil)
	})
	if err != nil {
		t.Fatalf("load current group meta: %v", err)
	}
	return fp, rev, validUntil.UTC()
}
