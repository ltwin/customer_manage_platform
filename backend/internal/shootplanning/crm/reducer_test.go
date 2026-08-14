package crm

import (
	"testing"
	"time"
)

func TestReduceIndependentLinkOrderDerivesCustomerAndProjection(t *testing.T) {
	now := time.Date(2026, 8, 16, 2, 0, 0, 0, time.UTC)
	start := now.Add(24 * time.Hour)
	end := start.Add(4 * time.Hour)
	result, err := Reduce(ReduceInput{
		Connection: Connection{PlanID: "spl_1", State: StateIndependent, ConnectionRevision: 1},
		Plan:       PlanFact{ID: "spl_1", Status: "draft", Revision: 3},
		Command:    Command{Kind: KindLinkOrder, OrderID: stringPtr("ord_1")},
		Customer:   &CustomerFact{ID: "cus_1", Status: "active"},
		Order:      &OrderFact{ID: "ord_1", CustomerID: "cus_1", Status: "scheduled", Title: stringPtr("双 look")},
		Slot:       &SlotFact{ID: "slot_1", OrderID: "ord_1", Type: "shoot", StartAt: start, EndAt: end, Revision: 1},
		Timezone:   "Asia/Shanghai",
		Now:        now,
		NewEpochID: "ple_new",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Connection.State != StateOrderLinked || deref(result.Connection.CustomerID) != "cus_1" {
		t.Fatalf("connection = %+v", result.Connection)
	}
	if result.Connection.ConnectionRevision != 2 || result.Projection == nil || result.Projection.ProjectionRevision != 1 {
		t.Fatalf("revisions connection=%d projection=%v", result.Connection.ConnectionRevision, result.Projection)
	}
	if result.Projection.Status != StatusActiveApplied || result.Window == nil || result.Window.Source != "schedule_slot" {
		t.Fatalf("projection=%+v window=%+v", result.Projection, result.Window)
	}
	if result.Window.LiveWindowStartsAt != start.Add(-2*time.Hour) || result.Window.LiveWindowEndsAt != end.Add(2*time.Hour) {
		t.Fatalf("live bounds %+v", result.Window)
	}
	if len(result.Events) == 0 || result.Events[0].ConnectionRevisionAfter == nil || result.Events[0].ProjectionRevisionAfter == nil {
		t.Fatalf("events %+v", result.Events)
	}
	if result.Events[0].PlanRevisionAfter == nil || *result.Events[0].PlanRevisionAfter != 4 {
		t.Fatalf("plan revision after %+v", result.Events[0].PlanRevisionAfter)
	}
	if result.Events[0].WindowRevisionAfter == nil || *result.Events[0].WindowRevisionAfter != result.Window.Revision {
		t.Fatalf("window revision after %+v", result.Events[0].WindowRevisionAfter)
	}
}

func TestReduceLinkOrderConflictAndSameTargetNoop(t *testing.T) {
	base := Connection{
		PlanID: "spl_1", State: StateOrderLinked, ConnectionRevision: 4,
		CustomerID: stringPtr("cus_1"), OrderID: stringPtr("ord_1"), LinkEpochID: stringPtr("ple_1"),
		LinkedOrderSnapshot: &LinkedOrderSnapshot{OrderID: "ord_1", CustomerID: "cus_1", StatusAtLink: "scheduled"},
	}
	_, err := Reduce(ReduceInput{
		Connection: base,
		Plan:       PlanFact{ID: "spl_1", Status: "draft", Revision: 1},
		Command:    Command{Kind: KindLinkOrder, OrderID: stringPtr("ord_2")},
		Customer:   &CustomerFact{ID: "cus_1", Status: "active"},
		Order:      &OrderFact{ID: "ord_2", CustomerID: "cus_1", Status: "consulting"},
		Now:        time.Now().UTC(),
	})
	if err != ErrOrderLinkConflict {
		t.Fatalf("different order err=%v", err)
	}
	_, err = Reduce(ReduceInput{
		Connection: base,
		Plan:       PlanFact{ID: "spl_1", Status: "draft", Revision: 1},
		Command:    Command{Kind: KindLinkOrder, OrderID: stringPtr("ord_1")},
		Customer:   &CustomerFact{ID: "cus_2", Status: "active"},
		Order:      &OrderFact{ID: "ord_1", CustomerID: "cus_2", Status: "scheduled"},
		Now:        time.Now().UTC(),
	})
	if err != ErrCustomerLinkConflict {
		t.Fatalf("different customer err=%v", err)
	}
	result, err := Reduce(ReduceInput{
		Connection: base,
		Plan:       PlanFact{ID: "spl_1", Status: "draft", Revision: 1},
		Command:    Command{Kind: KindLinkOrder, OrderID: stringPtr("ord_1")},
		Customer:   &CustomerFact{ID: "cus_1", Status: "active"},
		Order:      &OrderFact{ID: "ord_1", CustomerID: "cus_1", Status: "scheduled"},
		Now:        time.Now().UTC(),
		NewEpochID: "ple_ignored",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ConnectionChanged || result.ProjectionChanged || len(result.Events) != 0 {
		t.Fatalf("same-target should no-op: %+v", result)
	}
}

func TestReduceOrderDeletedThenUnlinkCustomerClearsEpoch(t *testing.T) {
	snap := &LinkedOrderSnapshot{OrderID: "ord_1", CustomerID: "cus_1", StatusAtLink: "closed", LinkedAt: time.Now().UTC()}
	conn := Connection{
		PlanID: "spl_1", State: StateOrderLinked, ConnectionRevision: 2,
		CustomerID: stringPtr("cus_1"), OrderID: stringPtr("ord_1"), LinkEpochID: stringPtr("ple_1"),
		LinkedOrderSnapshot: snap,
	}
	deleted, err := Reduce(ReduceInput{
		Connection: conn,
		Projection: &Projection{PlanID: "spl_1", Exists: true, Status: StatusActiveApplied, ProjectionRevision: 3, ApplySuppressed: true},
		Plan:       PlanFact{ID: "spl_1", Status: "draft", Revision: 8},
		Command:    Command{Kind: KindOrderDeleted, OrderID: stringPtr("ord_1")},
		Now:        time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if deleted.Connection.State != StateOrderDeleted || deleted.Connection.OrderID != nil || deleted.Connection.LinkedOrderSnapshot == nil {
		t.Fatalf("deleted connection %+v", deleted.Connection)
	}
	if deleted.Projection.Status != StatusInactiveOrderDeleted || deleted.Connection.ConnectionRevision != 3 {
		t.Fatalf("deleted projection %+v revision %d", deleted.Projection, deleted.Connection.ConnectionRevision)
	}
	cleared, err := Reduce(ReduceInput{
		Connection: deleted.Connection,
		Projection: deleted.Projection,
		Plan:       PlanFact{ID: "spl_1", Status: "draft", Revision: 8},
		Command:    Command{Kind: KindUnlinkCustomer},
		Now:        time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if cleared.Connection.State != StateIndependent || cleared.Connection.CustomerID != nil || cleared.Connection.LinkedOrderSnapshot != nil {
		t.Fatalf("unlinked %+v", cleared.Connection)
	}
	if cleared.Projection.ApplySuppressed || cleared.Projection.Status != StatusInactiveUnlinked {
		t.Fatalf("suppression/status %+v", cleared.Projection)
	}
	if len(cleared.Events) == 0 || cleared.Events[0].LinkEpochSnapshot == nil {
		t.Fatalf("unlink event must keep snapshot %+v", cleared.Events)
	}
}

func TestReduceSuppressedFutureSlotDoesNotAutoApply(t *testing.T) {
	now := time.Date(2026, 8, 16, 2, 0, 0, 0, time.UTC)
	start := now.Add(time.Hour)
	end := start.Add(2 * time.Hour)
	result, err := Reduce(ReduceInput{
		Connection: Connection{
			PlanID: "spl_1", State: StateOrderLinked, ConnectionRevision: 2,
			CustomerID: stringPtr("cus_1"), OrderID: stringPtr("ord_1"), LinkEpochID: stringPtr("ple_1"),
			LinkedOrderSnapshot: &LinkedOrderSnapshot{OrderID: "ord_1", CustomerID: "cus_1"},
		},
		Projection: &Projection{
			PlanID: "spl_1", Exists: true, Status: StatusActiveUnapplied, ApplySuppressed: true,
			ProjectionRevision: 4, OrderID: stringPtr("ord_1"),
		},
		Plan:     PlanFact{ID: "spl_1", Status: "ready", Revision: 2},
		Command:  Command{Kind: KindSchedule, ScheduleChange: ScheduleUpdate},
		Order:    &OrderFact{ID: "ord_1", CustomerID: "cus_1", Status: "scheduled"},
		Slot:     &SlotFact{ID: "slot_2", OrderID: "ord_1", Type: "shoot", StartAt: start, EndAt: end, Revision: 2},
		Now:      now,
		Timezone: "Asia/Shanghai",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Projection.Status != StatusActiveUnapplied || result.WindowChanged {
		t.Fatalf("suppressed future slot must stay unapplied: %+v windowChanged=%t", result.Projection, result.WindowChanged)
	}
}

func TestReduceAdoptRequiresFutureActiveProjection(t *testing.T) {
	now := time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)
	end := now.Add(-time.Minute)
	start := end.Add(-2 * time.Hour)
	_, err := Reduce(ReduceInput{
		Connection: Connection{PlanID: "spl_1", State: StateOrderLinked, ConnectionRevision: 1, OrderID: stringPtr("ord_1"), CustomerID: stringPtr("cus_1")},
		Projection: &Projection{
			PlanID: "spl_1", Exists: true, Status: StatusActiveUnapplied, ProjectionRevision: 3,
			StartsAt: &start, EndsAt: &end, SlotID: stringPtr("slot_1"), ApplySuppressed: true,
		},
		Plan:    PlanFact{ID: "spl_1", Status: "draft", Revision: 1},
		Command: Command{Kind: KindAdopt, ProjectionRevision: int64Ptr(3)},
		Now:     now,
	})
	if err != ErrProjectionNotFuture {
		t.Fatalf("past adopt err=%v", err)
	}
}

func TestReduceClockPassingEndDoesNotClearStoredWindow(t *testing.T) {
	now := time.Date(2026, 8, 16, 20, 0, 0, 0, time.UTC)
	start := now.Add(-10 * time.Hour)
	end := now.Add(-time.Hour)
	window := &Window{Source: "schedule_slot", SourceRef: stringPtr("slot_1"), StartsAt: start, EndsAt: end, Timezone: "Asia/Shanghai", Revision: 2}
	result, err := Reduce(ReduceInput{
		Connection: Connection{
			PlanID: "spl_1", State: StateOrderLinked, ConnectionRevision: 2,
			CustomerID: stringPtr("cus_1"), OrderID: stringPtr("ord_1"), LinkEpochID: stringPtr("ple_1"),
			LinkedOrderSnapshot: &LinkedOrderSnapshot{OrderID: "ord_1", CustomerID: "cus_1"},
		},
		Projection: &Projection{
			PlanID: "spl_1", Exists: true, Status: StatusActiveApplied, ProjectionRevision: 2,
			SlotID: stringPtr("slot_1"), StartsAt: &start, EndsAt: &end, OrderID: stringPtr("ord_1"),
			SlotSourceFingerprint: fingerprint(stringPtr("ord_1"), stringPtr("slot_1"), &start, &end, "shoot", 1, "scheduled", "Asia/Shanghai", false, StatusActiveApplied),
		},
		Window:   window,
		Plan:     PlanFact{ID: "spl_1", Status: "in_progress", Revision: 4},
		Command:  Command{Kind: KindSchedule, ScheduleChange: ScheduleUpdate},
		Order:    &OrderFact{ID: "ord_1", CustomerID: "cus_1", Status: "scheduled"},
		Slot:     &SlotFact{ID: "slot_1", OrderID: "ord_1", Type: "shoot", StartAt: start, EndAt: end, Revision: 1},
		Now:      now,
		Timezone: "Asia/Shanghai",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.WindowChanged && result.ClearWindow {
		t.Fatal("clock passing end must not auto-clear stored window on identical source")
	}
}

func TestSealedReminderFactsCompilePositive(t *testing.T) {
	var facts []CRMReminderLifecycleFactV1
	facts = append(facts,
		CRMPlanLinkChangedFactV1{PlanID: "spl_1", Change: PlanLinkChangeLink, ConnectionRevision: 1},
		CRMOrderLifecycleChangedFactV1{PlanID: "spl_1", Change: OrderLifecycleDeleted, SourceOrderID: "ord_1", ConnectionRevision: 2},
		CRMScheduleChangedFactV1{PlanID: "spl_1", Change: ScheduleUpdate, SourceSlotID: "slot_1", ConnectionRevision: 2, ProjectionRevision: 3},
	)
	if len(facts) != 3 {
		t.Fatal("expected three sealed variants")
	}
}
