package reminder_test

import (
	"context"
	"net/netip"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store/storetest"
	"github.com/samson/customer-manage-platform/backend/internal/reminder"
)

func TestMain(m *testing.M) { storetest.Main(m, store.MigrateUp) }

func startAssignmentReminderPostgres(t *testing.T) *store.Store {
	t.Helper()
	s, _ := startAssignmentReminderPostgresURL(t)
	return s
}

func startAssignmentReminderPostgresURL(t *testing.T) (*store.Store, string) {
	t.Helper()
	ctx := context.Background()
	url := storetest.NewURL(t)
	s, err := store.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s, url
}

func activateAssignmentTestAccount(t *testing.T, s *store.Store, id string) store.AccountScope {
	t.Helper()
	if err := s.CreateAccount(context.Background(), id, "hash"); err != nil {
		t.Fatalf("create account: %v", err)
	}
	mail := &activationMailSender{}
	service := auth.NewService(
		s,
		auth.NewTokenIssuer("assignment-reminder-test-root"),
		auth.WithAuthMailSender(mail),
		auth.WithAttemptLimiter(s),
		auth.WithPublicBaseURL("https://assignment-reminder.test"),
	)
	result, err := service.BeginLegacyClaim(context.Background(), id+"@assignment-reminder.test", false)
	if err != nil || result.State != auth.LegacyClaimReady || mail.wire == "" {
		t.Fatalf("begin legacy activation: state=%s err=%v", result.State, err)
	}
	if _, err := service.VerifyEmail(context.Background(), mail.wire, auth.ClientMeta{SourceIP: netip.MustParseAddr("127.0.0.1")}); err != nil {
		t.Fatalf("verify activation: %v", err)
	}
	return s.ScopeFor(auth.AccountContext{AccountID: id})
}

func seedAssignmentReminderFixture(t *testing.T, s *store.Store, accountID string) (
	scope store.AccountScope,
	planID, orderID, slotID, asgnID string,
) {
	t.Helper()
	ctx := context.Background()
	scope = activateAssignmentTestAccount(t, s, accountID)
	planID = "plan_" + accountID
	orderID = "ord_" + accountID
	slotID = "slot_" + accountID
	asgnID = "asgn_" + accountID
	custID := "cust_" + accountID
	genID := "gen_" + accountID
	commitment := make([]byte, 32)
	for i := range commitment {
		commitment[i] = 0xaa
	}
	receipt := make([]byte, 32)
	for i := range receipt {
		receipt[i] = 0xbb
	}
	slotStart := time.Date(2026, 8, 20, 2, 0, 0, 0, time.UTC)
	slotEnd := time.Date(2026, 8, 20, 4, 0, 0, 0, time.UTC)
	expires := time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
	issued := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)

	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.Insert(ctx, "customers",
			[]string{"id", "display_name", "channel", "status"},
			custID, "客户甲", "other", "active"); err != nil {
			return err
		}
		if err := tx.Insert(ctx, "settings",
			[]string{"timezone"}, "Asia/Shanghai"); err != nil {
			return err
		}
		if err := tx.Insert(ctx, "shoot_plans",
			[]string{"id", "title", "subject", "status"},
			planID, "策划", "主体", "ready"); err != nil {
			return err
		}
		if err := tx.Insert(ctx, "orders",
			[]string{"id", "customer_id", "title", "status"},
			orderID, custID, "成片", "scheduled"); err != nil {
			return err
		}
		if err := tx.Insert(ctx, "schedule_slots",
			[]string{"id", "start_at", "end_at", "type", "order_id"},
			slotID, slotStart, slotEnd, "shoot", orderID); err != nil {
			return err
		}
		if err := tx.Insert(ctx, "share_generations",
			[]string{
				"id", "plan_id", "view_level", "generation", "selector",
				"secret_commitment", "fingerprint", "state", "expires_at", "issued_at", "revision",
			},
			genID, planID, "proposal", int64(1), "sel-"+accountID,
			commitment, "fp-"+accountID, "active", expires, issued, int64(1)); err != nil {
			return err
		}
		if err := tx.Insert(ctx, "share_assignments",
			[]string{
				"id", "plan_id", "token_generation_id", "assignment_kind", "readiness_item_id",
				"content_snapshot", "claimed_by_display_name", "preparation_lead_days_snapshot",
				"lead_rule_version", "status", "claim_receipt_commitment", "revision",
			},
			asgnID, planID, genID, "readiness", "ready-1",
			"妆造准备", "昵称甲", 3, "platform-default-v1", "active", receipt, int64(1)); err != nil {
			return err
		}
		snapshot := []byte(`{"order_id":"` + orderID + `","customer_id":"` + custID + `","status_at_link":"scheduled","linked_at":"2026-08-01T00:00:00Z"}`)
		if err := tx.Insert(ctx, "plan_crm_connections",
			[]string{"plan_id", "customer_id", "order_id", "link_epoch_id", "linked_order_snapshot", "state", "connection_revision", "next_event_seq"},
			planID, custID, orderID, "ple_"+accountID, snapshot, "order_linked", int64(1), int64(1)); err != nil {
			return err
		}
		return tx.Insert(ctx, "plan_schedule_projections",
			[]string{"plan_id", "order_id", "slot_id", "slot_source_fingerprint", "starts_at", "ends_at", "timezone", "status", "apply_suppressed", "rule_version", "projection_revision"},
			planID, orderID, slotID, "fp-slot-"+accountID, slotStart, slotEnd, "Asia/Shanghai", "active_applied", false, 1, int64(1))
	})
	if err != nil {
		t.Fatalf("seed fixture: %v", err)
	}
	return scope, planID, orderID, slotID, asgnID
}

func TestPlanAssignmentReminderTruthTablePG(t *testing.T) {
	ctx := context.Background()
	s := startAssignmentReminderPostgres(t)
	scope, planID, orderID, slotID, asgnID := seedAssignmentReminderFixture(t, s, "partruth")
	repo := reminder.NewAssignmentProjectionRepository()
	fixedNow := time.Date(2026, 8, 14, 8, 0, 0, 0, time.UTC)
	slotStart := time.Date(2026, 8, 20, 2, 0, 0, 0, time.UTC)
	lead := 3
	rule := "platform-default-v1"
	readyID := "ready-1"

	assignment := reminder.SafeAssignmentInput{
		AssignmentID: asgnID, AssignmentRevision: 1, AssignmentKind: reminder.AssignmentKindReadiness,
		SourceState: reminder.SourceStateActive, ReadinessItemID: &readyID,
		ContentSnapshot: "妆造准备", ContentFingerprint: "fp_ready_1",
		PreparationLeadDaysSnapshot: &lead, LeadRuleVersion: &rule, SourceOccurredAt: fixedNow,
	}
	sidecar := reminder.PlanOrderSlotSidecar{
		OrderID: &orderID, OrderActive: true, SlotID: &slotID,
		SlotType: "shoot", SlotStartAt: &slotStart,
	}
	input := reminder.ReduceDesiredReminderGroupsInput{
		AccountID: scope.AccountID(), PlanID: planID, Assignments: []reminder.SafeAssignmentInput{assignment},
		Sidecar: sidecar, Timezone: "Asia/Shanghai", Now: fixedNow, ActivationGeneration: 1,
	}

	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		plan, err := reminder.ReconcilePlanProjection(ctx, tx, repo, input)
		if err != nil {
			return err
		}
		if len(plan.Creates) != 1 {
			t.Fatalf("first create: %+v", plan)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		plan, err := reminder.ReconcilePlanProjection(ctx, tx, repo, input)
		if err != nil {
			return err
		}
		if len(plan.Creates)+len(plan.Withdrawals)+len(plan.TemporalUpdates) != 0 || len(plan.NoOpGroupIDs) != 1 {
			t.Fatalf("exact replay must 0 write: %+v", plan)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	slotLater := slotStart.Add(45 * time.Minute)
	inputTemporal := input
	inputTemporal.Sidecar.SlotStartAt = &slotLater
	inputTemporal.ActivationGeneration = 2
	err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		plan, err := reminder.ReconcilePlanProjection(ctx, tx, repo, inputTemporal)
		if err != nil {
			return err
		}
		if len(plan.TemporalUpdates) != 1 || plan.TemporalUpdates[0].NextValidityRev != 2 {
			t.Fatalf("temporal update: %+v", plan)
		}
		groups, err := repo.ListCurrentGroupsInScope(ctx, tx, planID)
		if err != nil {
			return err
		}
		if len(groups) != 1 || groups[0].ValidityRevision != 2 {
			t.Fatalf("validity_revision want 2: %+v", groups)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		unscheduled := inputTemporal
		unscheduled.Sidecar.SlotID = nil
		unscheduled.Sidecar.SlotStartAt = nil
		unscheduled.ActivationGeneration = 3
		plan, err := reminder.ReconcilePlanProjection(ctx, tx, repo, unscheduled)
		if err != nil {
			return err
		}
		if len(plan.Withdrawals) != 1 || !plan.Withdrawals[0].Dismiss {
			t.Fatalf("withdraw pending: %+v", plan)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	var oldReminderID string
	err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		return tx.QueryRow(ctx, "plan_assignment_reminder_groups",
			"reminder_id", "plan_id = $2 AND state = $3", planID, reminder.GroupStateWithdrawn,
		).Scan(&oldReminderID)
	})
	if err != nil {
		t.Fatal(err)
	}

	err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		restore := inputTemporal
		restore.ActivationGeneration = 4
		plan, err := reminder.ReconcilePlanProjection(ctx, tx, repo, restore)
		if err != nil {
			return err
		}
		if len(plan.Creates) != 1 {
			t.Fatalf("recurrence create: %+v", plan)
		}
		groups, err := repo.ListCurrentGroupsInScope(ctx, tx, planID)
		if err != nil {
			return err
		}
		if len(groups) != 1 || groups[0].ReminderID == oldReminderID {
			t.Fatalf("must create new reminder occurrence, not resurrect: %+v", groups)
		}
		rem, err := reminder.NewPostgresRepository().Find(ctx, tx.BoundAccountScope(), groups[0].ReminderID)
		if err != nil {
			return err
		}
		if rem.Type != reminder.TypePlanAssignmentChecklist || rem.Status != reminder.StatusPending {
			t.Fatalf("new reminder: %+v", rem)
		}
		if rem.DedupKey != reminder.PlanAssignmentChecklistDedupKey(groups[0].GroupID) {
			t.Fatalf("dedup_key=%s", rem.DedupKey)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	assignmentNewFP := assignment
	assignmentNewFP.AssignmentRevision = 2
	assignmentNewFP.ContentFingerprint = "fp_ready_2"
	err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		next := inputTemporal
		next.Assignments = []reminder.SafeAssignmentInput{assignmentNewFP}
		next.ActivationGeneration = 5
		before, err := repo.ListCurrentGroupsInScope(ctx, tx, planID)
		if err != nil {
			return err
		}
		oldID := before[0].GroupID
		plan, err := reminder.ReconcilePlanProjection(ctx, tx, repo, next)
		if err != nil {
			return err
		}
		if len(plan.Withdrawals) != 1 || !plan.Withdrawals[0].Dismiss || len(plan.Creates) != 1 {
			t.Fatalf("pending fingerprint change: %+v", plan)
		}
		groups, err := repo.ListCurrentGroupsInScope(ctx, tx, planID)
		if err != nil {
			return err
		}
		if len(groups) != 1 || groups[0].GroupID == oldID {
			t.Fatalf("expected new group after fingerprint change: %+v", groups)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		groups, err := repo.ListCurrentGroupsInScope(ctx, tx, planID)
		if err != nil {
			return err
		}
		if _, err := reminder.NewPostgresRepository().SetStatus(ctx, tx.BoundAccountScope(), groups[0].ReminderID, reminder.StatusDone); err != nil {
			return err
		}
		assignmentNewFP3 := assignmentNewFP
		assignmentNewFP3.AssignmentRevision = 3
		assignmentNewFP3.ContentFingerprint = "fp_ready_3"
		next := inputTemporal
		next.Assignments = []reminder.SafeAssignmentInput{assignmentNewFP3}
		next.ActivationGeneration = 6
		terminalID := groups[0].ReminderID
		plan, err := reminder.ReconcilePlanProjection(ctx, tx, repo, next)
		if err != nil {
			return err
		}
		if len(plan.Withdrawals) != 1 || plan.Withdrawals[0].Dismiss || len(plan.Creates) != 1 {
			t.Fatalf("done fingerprint change: %+v", plan)
		}
		rem, err := reminder.NewPostgresRepository().Find(ctx, tx.BoundAccountScope(), terminalID)
		if err != nil {
			return err
		}
		if rem.Status != reminder.StatusDone {
			t.Fatalf("terminal must stay done: %s", rem.Status)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
