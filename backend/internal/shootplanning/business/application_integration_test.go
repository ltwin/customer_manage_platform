package business_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/customer"
	"github.com/samson/customer-manage-platform/backend/internal/order"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/idempotency"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/reminder"
	"github.com/samson/customer-manage-platform/backend/internal/schedule"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning/business"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning/crm"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning/planningreminder"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestBusinessFactsDraftsOrderAndScheduleApply(t *testing.T) {
	ctx := context.Background()
	db := openBusinessStore(t)
	scope := createBusinessAccount(t, db, "business-account")
	otherScope := createBusinessAccount(t, db, "business-other")
	executor := idempotency.NewExecutor()
	core, err := shootplanning.NewApplication(
		shootplanning.NewPostgresRepository(), executor,
		shootplanning.WithCRMReminder(reminder.NewCRMReminderLifecycleAdapter(nil), true),
	)
	if err != nil {
		t.Fatal(err)
	}
	engine := core.CRM()
	orderParticipant := order.NewBusinessAdjustmentParticipant()
	scheduleParticipant := schedule.NewBusinessDurationParticipant(engine)
	app, err := business.NewApplication(
		business.NewRepository(), executor,
		orderParticipant,
		scheduleParticipant,
	)
	if err != nil {
		t.Fatal(err)
	}

	plan, err := core.CreatePlan(ctx, scope, "business-create-plan", shootplanning.CreatePlanInput{
		Title: "经营草稿测试", Subject: "角色 A",
	})
	if err != nil {
		t.Fatal(err)
	}
	emptyDetail, err := app.GetDetail(ctx, scope, plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if emptyDetail.OrderAdjustment != nil || emptyDetail.ScheduleDuration != nil {
		t.Fatalf("zero-draft detail = %+v", emptyDetail)
	}
	lookCount := 2
	mutation, err := core.ApplyPlanCommand(ctx, scope, "business-public-scale", plan.ID, plan.Revision,
		shootplanning.SetPublicScaleCommand{
			PlannedLookCount: shootplanning.Optional[int]{Specified: true, Value: lookCount},
		})
	if err != nil {
		t.Fatal(err)
	}

	customers := customer.NewService(customer.NewPostgresRepository())
	createdCustomer, err := customers.Create(ctx, scope, customer.CreateInput{
		DisplayName: "阿晚", Channel: customer.ChannelOther,
		Identities: []customer.IdentityInput{{Platform: customer.PlatformOther, Handle: "business-a-wan"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	basePrice := 268000
	title := "和服创作"
	createdOrder, err := order.NewService(order.NewPostgresRepository()).Create(ctx, scope, order.CreateInput{
		CreationMode: order.CreationModeNew,
		CustomerID:   createdCustomer.ID,
		Title:        &title,
		Price:        &basePrice,
	})
	if err != nil {
		t.Fatal(err)
	}
	mutation, err = core.ApplyCRMLink(ctx, scope, "business-link-order", plan.ID, mutation.Revision, crm.Command{
		Kind: crm.KindLinkOrder, OrderID: &createdOrder.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	facts := business.Facts{
		RentedLocationCount:      intPointer(1),
		AssistantCount:           intPointer(1),
		RetouchedPhotoCount:      intPointer(18),
		EstimatedDurationMinutes: intPointer(420),
	}
	factsResult, err := app.SetFacts(ctx, scope, "business-facts", plan.ID, mutation.Revision, 0, facts)
	if err != nil {
		t.Fatal(err)
	}
	replayedFacts, err := app.SetFacts(ctx, scope, "business-facts", plan.ID, mutation.Revision, 0, facts)
	if err != nil || replayedFacts.Revision != factsResult.Revision {
		t.Fatalf("facts replay = %+v err=%v", replayedFacts, err)
	}

	generated, err := app.Generate(ctx, scope, "business-order-generate", plan.ID, business.GenerateInput{
		ExpectedPlanRevision:  factsResult.Revision,
		ExpectedFactsRevision: 1,
		DraftKinds:            []business.DraftKind{business.DraftOrderAdjustment},
	})
	if err != nil {
		t.Fatal(err)
	}
	orderDraft := generated.OrderAdjustment.OrderDraft
	if orderDraft == nil || orderDraft.ProposedTotal == nil || *orderDraft.ProposedTotal != 316000 {
		t.Fatalf("order draft = %+v", generated.OrderAdjustment)
	}
	oneDraftDetail, err := app.GetDetail(ctx, scope, plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if oneDraftDetail.OrderAdjustment == nil || oneDraftDetail.OrderAdjustment.ID != orderDraft.ID || oneDraftDetail.ScheduleDuration != nil {
		t.Fatalf("one-draft detail = %+v", oneDraftDetail)
	}
	decision, err := app.Decide(ctx, scope, "business-order-apply", plan.ID, orderDraft.ID, business.DecisionInput{
		ExpectedDraftRevision: orderDraft.Revision,
		Decision:              business.DecisionApplyOrder,
		Acknowledgement:       orderDraft.RequiredAcknowledgement,
	})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Status != business.StatusApplied || decision.AppliedTarget == nil || decision.AppliedTarget.AfterPrice == nil || *decision.AppliedTarget.AfterPrice != 316000 {
		t.Fatalf("order decision = %+v", decision)
	}
	var storedPrice int
	if err := scope.QueryRow(ctx, "orders", "price", "id = $2", createdOrder.ID).Scan(&storedPrice); err != nil {
		t.Fatal(err)
	}
	if storedPrice != 316000 {
		t.Fatalf("stored price = %d", storedPrice)
	}
	auditCount, err := scope.Count(ctx, "order_price_adjustments", "draft_id = $2", orderDraft.ID)
	if err != nil || auditCount != 1 {
		t.Fatalf("adjustment audit count = %d err=%v", auditCount, err)
	}
	if _, err := scope.Update(ctx, "order_price_adjustments", "after_price = $2", "draft_id = $3", 1, orderDraft.ID); err == nil {
		t.Fatal("order price adjustment update should be rejected by the append-only trigger")
	}
	if _, err := scope.Delete(ctx, "order_price_adjustments", "draft_id = $2", orderDraft.ID); err == nil {
		t.Fatal("order price adjustment delete should be rejected by the append-only trigger")
	}
	auditCount, err = scope.Count(ctx, "order_price_adjustments", "draft_id = $2", orderDraft.ID)
	if err != nil || auditCount != 1 {
		t.Fatalf("immutable adjustment audit count = %d err=%v", auditCount, err)
	}

	directGeneration, err := app.Generate(ctx, scope, "business-order-guard-generate", plan.ID, business.GenerateInput{
		ExpectedPlanRevision:  factsResult.Revision,
		ExpectedFactsRevision: 1,
		DraftKinds:            []business.DraftKind{business.DraftOrderAdjustment},
	})
	if err != nil || directGeneration.OrderAdjustment == nil || directGeneration.OrderAdjustment.OrderDraft == nil {
		t.Fatalf("generate order participant guard draft: result=%+v err=%v", directGeneration, err)
	}
	assertOrderParticipantGuards(t, ctx, scope, orderParticipant, createdOrder.ID, plan.ID, *directGeneration.OrderAdjustment.OrderDraft, 316000)
	assertDifferentKeyConcurrentOrderApply(t, ctx, scope, app, plan.ID, createdOrder.ID, *directGeneration.OrderAdjustment.OrderDraft)

	start := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Second)
	end := start.Add(2 * time.Hour)
	scheduleService := schedule.NewService(
		schedule.NewPostgresRepository().WithProjectionSink(engine),
		schedule.ClockFunc(time.Now),
	)
	createdSlot, err := scheduleService.Create(ctx, scope, schedule.CreateInput{
		StartAt: start, EndAt: end, Type: "shoot", OrderID: &createdOrder.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	currentPlan, err := core.GetPlan(ctx, scope, plan.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	scheduleGeneration, err := app.Generate(ctx, scope, "business-schedule-generate", plan.ID, business.GenerateInput{
		ExpectedPlanRevision:  currentPlan.Revision,
		ExpectedFactsRevision: 1,
		DraftKinds:            []business.DraftKind{business.DraftScheduleDuration},
	})
	if err != nil {
		t.Fatal(err)
	}
	scheduleDraft := scheduleGeneration.ScheduleDuration.ScheduleDraft
	if scheduleDraft == nil || scheduleDraft.ProposedEndAt == nil || !scheduleDraft.ProposedEndAt.Equal(start.Add(7*time.Hour)) {
		t.Fatalf("schedule draft = %+v", scheduleGeneration.ScheduleDuration)
	}
	twoDraftDetail, err := app.GetDetail(ctx, scope, plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if twoDraftDetail.OrderAdjustment == nil || twoDraftDetail.ScheduleDuration == nil ||
		twoDraftDetail.OrderAdjustment.ID != directGeneration.OrderAdjustment.OrderDraft.ID ||
		twoDraftDetail.ScheduleDuration.ID != scheduleDraft.ID {
		t.Fatalf("two-draft detail = %+v", twoDraftDetail)
	}
	beforeReminderWork := countScheduleReminderRows(t, ctx, scope, "planning_reminder_generation_work", plan.ID)
	beforeReminderResolutions := countScheduleReminderRows(t, ctx, scope, "planning_reminder_generation_resolutions", plan.ID)
	beforeTarget, beforeApplied := loadReminderWatermarks(t, ctx, scope)
	_, err = app.Decide(ctx, scope, "business-schedule-apply", plan.ID, scheduleDraft.ID, business.DecisionInput{
		ExpectedDraftRevision: scheduleDraft.Revision,
		Decision:              business.DecisionApplySchedule,
		Acknowledgement:       scheduleDraft.RequiredAcknowledgement,
	})
	if err != nil {
		t.Fatal(err)
	}
	storedSlot, err := scheduleService.Get(ctx, scope, createdSlot.Slot.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !storedSlot.EndAt.Equal(start.Add(7 * time.Hour)) {
		t.Fatalf("stored end = %v", storedSlot.EndAt)
	}
	afterReminderWork := countScheduleReminderRows(t, ctx, scope, "planning_reminder_generation_work", plan.ID)
	afterReminderResolutions := countScheduleReminderRows(t, ctx, scope, "planning_reminder_generation_resolutions", plan.ID)
	afterTarget, afterApplied := loadReminderWatermarks(t, ctx, scope)
	if afterReminderWork != beforeReminderWork+1 || afterReminderResolutions != beforeReminderResolutions+1 ||
		afterTarget != beforeTarget+1 || afterApplied != beforeApplied+1 || afterTarget != afterApplied {
		t.Fatalf("schedule reminder generation: work %d->%d resolutions %d->%d watermarks (%d,%d)->(%d,%d)",
			beforeReminderWork, afterReminderWork, beforeReminderResolutions, afterReminderResolutions,
			beforeTarget, beforeApplied, afterTarget, afterApplied)
	}
	assertScheduleParticipantGuards(t, ctx, scope, scheduleParticipant, plan.ID, createdSlot.Slot.ID, scheduleDraft.ID, start, storedSlot.EndAt)

	latestPlan, err := core.GetPlan(ctx, scope, plan.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	var latestOrderDraftID string
	for i := 0; i < 12; i++ {
		historyGeneration, err := app.Generate(ctx, scope, fmt.Sprintf("business-bounded-history-%02d", i), plan.ID, business.GenerateInput{
			ExpectedPlanRevision: latestPlan.Revision, ExpectedFactsRevision: 1,
			DraftKinds: []business.DraftKind{business.DraftOrderAdjustment},
		})
		if err != nil || historyGeneration.OrderAdjustment == nil || historyGeneration.OrderAdjustment.OrderDraft == nil {
			t.Fatalf("generate bounded history %d: result=%+v err=%v", i, historyGeneration, err)
		}
		latestOrderDraftID = historyGeneration.OrderAdjustment.OrderDraft.ID
	}
	boundedDetail, err := app.GetDetail(ctx, scope, plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if boundedDetail.OrderAdjustment == nil || boundedDetail.OrderAdjustment.ID != latestOrderDraftID ||
		boundedDetail.ScheduleDuration == nil || boundedDetail.ScheduleDuration.ID != scheduleDraft.ID {
		t.Fatalf("bounded latest detail = %+v", boundedDetail)
	}
	orderHistoryCount, err := scope.Count(ctx, "planning_business_drafts", "plan_id = $2 AND kind = $3", plan.ID, string(business.DraftOrderAdjustment))
	if err != nil || orderHistoryCount != 14 {
		t.Fatalf("order draft history count = %d err=%v", orderHistoryCount, err)
	}

	if _, err := app.GetDetail(ctx, otherScope, plan.ID); !errors.Is(err, business.ErrNotFound) {
		t.Fatalf("cross-account detail error = %v", err)
	}
}

func assertDifferentKeyConcurrentOrderApply(
	t *testing.T,
	ctx context.Context,
	scope store.AccountScope,
	app *business.Application,
	planID, orderID string,
	draft business.OrderDraftView,
) {
	t.Helper()
	if draft.RequiredAcknowledgement == nil || draft.ProposedTotal == nil {
		t.Fatalf("concurrent apply draft is not actionable: %+v", draft)
	}
	start := make(chan struct{})
	errs := make(chan error, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	for i := 0; i < 2; i++ {
		go func(index int) {
			ready.Done()
			<-start
			_, err := app.Decide(ctx, scope, fmt.Sprintf("business-order-concurrent-%d", index), planID, draft.ID, business.DecisionInput{
				ExpectedDraftRevision: draft.Revision,
				Decision:              business.DecisionApplyOrder,
				Acknowledgement:       draft.RequiredAcknowledgement,
			})
			errs <- err
		}(i)
	}
	ready.Wait()
	close(start)
	successes := 0
	for i := 0; i < 2; i++ {
		err := <-errs
		if err == nil {
			successes++
			continue
		}
		if !errors.Is(err, business.ErrRevisionConflict) {
			t.Fatalf("concurrent apply loser error = %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent apply successes = %d, want 1", successes)
	}
	assertOrderPriceAndAudit(t, ctx, scope, orderID, draft.ID, *draft.ProposedTotal, 1)
}

func countScheduleReminderRows(t *testing.T, ctx context.Context, scope store.AccountScope, table, planID string) int64 {
	t.Helper()
	count, err := scope.Count(ctx, table, "plan_id = $2 AND mutation_kind = $3", planID, string(planningreminder.MutationCRMScheduleChanged))
	if err != nil {
		t.Fatal(err)
	}
	return count
}

func loadReminderWatermarks(t *testing.T, ctx context.Context, scope store.AccountScope) (int64, int64) {
	t.Helper()
	var target, applied int64
	if err := scope.QueryRow(ctx, "planning_reminder_account_generations", "target_generation, applied_generation", "TRUE").Scan(&target, &applied); err != nil {
		t.Fatal(err)
	}
	return target, applied
}

func assertOrderParticipantGuards(
	t *testing.T,
	ctx context.Context,
	scope store.AccountScope,
	participant order.BusinessAdjustmentParticipant,
	orderID, planID string,
	draft business.OrderDraftView,
	wantPrice int,
) {
	t.Helper()
	var escaped business.LockedOrderScope
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		_, participantErr := participant.WithLockedTargetInScope(ctx, tx, orderID, func(locked business.LockedOrderScope) error {
			escaped = locked
			return nil
		})
		return participantErr
	})
	if !errors.Is(err, order.ErrBusinessAdjustmentScope) {
		t.Fatalf("order callback success without apply error = %v", err)
	}
	if escaped == nil {
		t.Fatal("order scope was not captured")
	}
	if _, err := escaped.Apply(ctx, business.ApplyOrderCommand{}); !errors.Is(err, order.ErrBusinessAdjustmentScope) {
		t.Fatalf("order scope use after callback error = %v", err)
	}

	commandFor := func(adjustmentID string, locked business.LockedOrderScope) business.ApplyOrderCommand {
		fingerprint, _, err := business.FingerprintOrderTarget(locked.Target())
		if err != nil {
			t.Fatal(err)
		}
		return business.ApplyOrderCommand{
			AdjustmentID:            adjustmentID,
			PlanID:                  planID,
			DraftID:                 draft.ID,
			AfterPrice:              wantPrice + 100,
			CalculationMode:         draft.CalculationMode,
			BasePrice:               draft.BasePrice,
			Lines:                   draft.Lines,
			Warnings:                draft.Warnings,
			RuleVersion:             draft.RuleVersion,
			BeforeTargetFingerprint: fingerprint,
		}
	}
	err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		_, participantErr := participant.WithLockedTargetInScope(ctx, tx, orderID, func(locked business.LockedOrderScope) error {
			command := commandFor("opa_guard_second", locked)
			if _, err := locked.Apply(ctx, command); err != nil {
				return err
			}
			_, err := locked.Apply(ctx, command)
			return err
		})
		return participantErr
	})
	if !errors.Is(err, order.ErrBusinessAdjustmentScope) {
		t.Fatalf("order second apply error = %v", err)
	}
	assertOrderPriceAndAudit(t, ctx, scope, orderID, draft.ID, wantPrice, 0)

	injected := errors.New("order callback injected failure")
	err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		_, participantErr := participant.WithLockedTargetInScope(ctx, tx, orderID, func(locked business.LockedOrderScope) error {
			if _, err := locked.Apply(ctx, commandFor("opa_guard_callback_error", locked)); err != nil {
				return err
			}
			return injected
		})
		return participantErr
	})
	if !errors.Is(err, injected) {
		t.Fatalf("order apply then callback error = %v", err)
	}
	assertOrderPriceAndAudit(t, ctx, scope, orderID, draft.ID, wantPrice, 0)
}

func assertOrderPriceAndAudit(
	t *testing.T,
	ctx context.Context,
	scope store.AccountScope,
	orderID, draftID string,
	wantPrice int,
	wantAudit int64,
) {
	t.Helper()
	var price int
	if err := scope.QueryRow(ctx, "orders", "price", "id = $2", orderID).Scan(&price); err != nil {
		t.Fatal(err)
	}
	count, err := scope.Count(ctx, "order_price_adjustments", "draft_id = $2", draftID)
	if err != nil || price != wantPrice || count != wantAudit {
		t.Fatalf("order participant rollback: price=%d audit=%d err=%v", price, count, err)
	}
}

func assertScheduleParticipantGuards(
	t *testing.T,
	ctx context.Context,
	scope store.AccountScope,
	participant schedule.BusinessDurationParticipant,
	planID, slotID, draftID string,
	start, originalEnd time.Time,
) {
	t.Helper()
	var escaped business.LockedScheduleScope
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		_, participantErr := participant.WithLockedTargetInScope(ctx, tx, planID, slotID, func(locked business.LockedScheduleScope) error {
			escaped = locked
			return nil
		})
		return participantErr
	})
	if !errors.Is(err, schedule.ErrBusinessDurationScope) {
		t.Fatalf("schedule callback success without apply error = %v", err)
	}
	if escaped == nil {
		t.Fatal("schedule scope was not captured")
	}
	if _, err := escaped.ApplyEnd(ctx, business.ApplyScheduleCommand{}); !errors.Is(err, schedule.ErrBusinessDurationScope) {
		t.Fatalf("schedule scope use after callback error = %v", err)
	}

	command := business.ApplyScheduleCommand{PlanID: planID, DraftID: draftID, ProposedEndAt: start.Add(8 * time.Hour)}
	err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		_, participantErr := participant.WithLockedTargetInScope(ctx, tx, planID, slotID, func(locked business.LockedScheduleScope) error {
			if _, err := locked.ApplyEnd(ctx, command); err != nil {
				return err
			}
			_, err := locked.ApplyEnd(ctx, command)
			return err
		})
		return participantErr
	})
	if !errors.Is(err, schedule.ErrBusinessDurationScope) {
		t.Fatalf("schedule second apply error = %v", err)
	}
	assertScheduleEnd(t, ctx, scope, slotID, originalEnd)

	injected := errors.New("schedule callback injected failure")
	err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		_, participantErr := participant.WithLockedTargetInScope(ctx, tx, planID, slotID, func(locked business.LockedScheduleScope) error {
			if _, err := locked.ApplyEnd(ctx, command); err != nil {
				return err
			}
			return injected
		})
		return participantErr
	})
	if !errors.Is(err, injected) {
		t.Fatalf("schedule apply then callback error = %v", err)
	}
	assertScheduleEnd(t, ctx, scope, slotID, originalEnd)
}

func assertScheduleEnd(t *testing.T, ctx context.Context, scope store.AccountScope, slotID string, want time.Time) {
	t.Helper()
	var end time.Time
	if err := scope.QueryRow(ctx, "schedule_slots", "end_at", "id = $2", slotID).Scan(&end); err != nil {
		t.Fatal(err)
	}
	if !end.Equal(want) {
		t.Fatalf("schedule participant rollback end = %v, want %v", end, want)
	}
}

func openBusinessStore(t *testing.T) *store.Store {
	t.Helper()
	ctx := context.Background()
	container, err := tcpostgres.Run(ctx, "postgres:17-alpine",
		tcpostgres.WithDatabase("planning_business_test"),
		tcpostgres.WithUsername("planning_business_test"),
		tcpostgres.WithPassword("planning_business_test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(container) })
	databaseURL, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MigrateUp(databaseURL); err != nil {
		t.Fatalf("migrate business database: %v", err)
	}
	db, err := store.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	return db
}

func createBusinessAccount(t *testing.T, db *store.Store, id string) store.AccountScope {
	t.Helper()
	if err := db.CreateAccount(context.Background(), id, "test-hash"); err != nil {
		t.Fatal(err)
	}
	return db.ScopeFor(auth.AccountContext{AccountID: id})
}

func intPointer(value int) *int { return &value }
