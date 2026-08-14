package shootplanning_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/customer"
	"github.com/samson/customer-manage-platform/backend/internal/order"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/schedule"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning/crm"
)

var (
	_ customer.PlanCustomerMergeParticipant   = (*crm.Engine)(nil)
	_ order.PlanningOrderLifecycleParticipant = (*crm.Engine)(nil)
	_ schedule.PlanningScheduleProjectionSink = (*crm.Engine)(nil)
)

func TestCRMLinkConflictUnlinkAndPlanningSummaryQueryDelta(t *testing.T) {
	ctx := context.Background()
	db := openPlanningStore(t)
	scope := createPlanningAccount(t, db, "crm-link-summary-acct")
	engine := crm.NewEngine()
	customers := customer.NewService(customer.NewPostgresRepository())
	orders := order.NewService(order.NewPostgresRepository())
	repo := shootplanning.NewPostgresRepository()

	plan, err := repo.Create(ctx, scope, shootplanning.CreatePlanInput{Title: "夏日双 look", Subject: "和服"})
	if err != nil {
		t.Fatal(err)
	}
	first := createCRMCustomer(t, customers, scope, "阿晚")
	second := createCRMCustomer(t, customers, scope, "苏晚")
	title := "和服写真"
	linkedOrder, err := orders.Create(ctx, scope, order.CreateInput{
		CreationMode: order.CreationModeNew,
		CustomerID:   first.ID,
		Title:        &title,
	})
	if err != nil {
		t.Fatal(err)
	}

	outcome := applyCRM(t, scope, engine, plan.ID, plan.Revision, crm.Command{
		Kind: crm.KindLinkCustomer, CustomerID: &first.ID,
	})
	if outcome.Connection.State != crm.StateCustomerLinked || outcome.Revision != plan.Revision {
		t.Fatalf("link customer outcome = %+v", outcome)
	}

	_, err = applyCRMErr(t, scope, engine, plan.ID, outcome.Revision, crm.Command{
		Kind: crm.KindLinkCustomer, CustomerID: &second.ID,
	})
	if !errors.Is(err, crm.ErrCustomerLinkConflict) {
		t.Fatalf("different customer err = %v", err)
	}

	outcome = applyCRM(t, scope, engine, plan.ID, outcome.Revision, crm.Command{
		Kind: crm.KindLinkOrder, OrderID: &linkedOrder.ID,
	})
	if outcome.Connection.State != crm.StateOrderLinked || derefCRM(outcome.Connection.CustomerID) != first.ID {
		t.Fatalf("direct remaining order link = %+v", outcome.Connection)
	}

	otherTitle := "另一单"
	otherOrder, err := orders.Create(ctx, scope, order.CreateInput{
		CreationMode: order.CreationModeNew,
		CustomerID:   first.ID,
		Title:        &otherTitle,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = applyCRMErr(t, scope, engine, plan.ID, outcome.Revision, crm.Command{
		Kind: crm.KindLinkOrder, OrderID: &otherOrder.ID,
	})
	if !errors.Is(err, crm.ErrOrderLinkConflict) {
		t.Fatalf("different order err = %v", err)
	}

	counter := &countingQueryScope{inner: scope}
	empty, err := crm.LoadSummaries(ctx, counter, crm.SummaryViewByCustomer, nil)
	if err != nil {
		t.Fatal(err)
	}
	if counter.n != 0 || len(empty) != 0 {
		t.Fatalf("empty IDs queries=%d summaries=%d", counter.n, len(empty))
	}
	for _, size := range []int{10, 100} {
		ids := make([]string, size)
		for i := range ids {
			ids[i] = fmt.Sprintf("cus_missing_%03d", i)
		}
		ids[0] = first.ID
		counter.n = 0
		summaries, err := crm.LoadSummaries(ctx, counter, crm.SummaryViewByCustomer, ids)
		if err != nil {
			t.Fatal(err)
		}
		if counter.n != 1 {
			t.Fatalf("size %d queries = %d want 1", size, counter.n)
		}
		summary, ok := summaries[first.ID]
		if !ok || summary.PlanCount != 1 || summary.ActivePlanCount != 1 || summary.PrimaryPlanID != plan.ID {
			t.Fatalf("size %d summary = ok=%v %+v", size, ok, summary)
		}
	}

	listed, err := customers.List(ctx, scope, customer.ListFilter{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatal(err)
	}
	var found *customer.ListItem
	for i := range listed.Items {
		if listed.Items[i].ID == first.ID {
			found = &listed.Items[i]
			break
		}
		if listed.Items[i].ID == second.ID && listed.Items[i].PlanningSummary != nil {
			t.Fatal("unlinked customer leaked a planning summary")
		}
	}
	if found == nil || found.PlanningSummary == nil || found.PlanningSummary.PlanCount != 1 {
		t.Fatalf("linked customer list summary = %+v", found)
	}

	outcome = applyCRM(t, scope, engine, plan.ID, outcome.Revision, crm.Command{Kind: crm.KindUnlinkOrder})
	if outcome.Connection.State != crm.StateCustomerLinked || outcome.Connection.OrderID != nil {
		t.Fatalf("unlink order = %+v", outcome.Connection)
	}
	outcome = applyCRM(t, scope, engine, plan.ID, outcome.Revision, crm.Command{Kind: crm.KindUnlinkCustomer})
	if outcome.Connection.State != crm.StateIndependent || outcome.Connection.CustomerID != nil {
		t.Fatalf("unlink customer = %+v", outcome.Connection)
	}
	summaries, err := crm.LoadSummaries(ctx, scope, crm.SummaryViewByCustomer, []string{first.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := summaries[first.ID]; ok {
		t.Fatalf("unlinked customer still in summary: %+v", summaries[first.ID])
	}
}

func TestCRMOrderCancelDeleteMergeAndIndependentLinkOrder(t *testing.T) {
	ctx := context.Background()
	db := openPlanningStore(t)
	scope := createPlanningAccount(t, db, "crm-lifecycle-acct")
	engine := crm.NewEngine()
	customers := customer.NewService(customer.NewPostgresRepository().WithPlanMergeParticipant(engine))
	orders := order.NewService(order.NewPostgresRepository().WithLifecycleParticipant(engine))
	repo := shootplanning.NewPostgresRepository()

	source := createCRMCustomer(t, customers, scope, "源客户")
	target := createCRMCustomer(t, customers, scope, "目标客户")
	title := "取消后删除"
	linkedOrder, err := orders.Create(ctx, scope, order.CreateInput{
		CreationMode: order.CreationModeNew,
		CustomerID:   source.ID,
		Title:        &title,
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := repo.Create(ctx, scope, shootplanning.CreatePlanInput{Title: "独立直连订单", Subject: "主体"})
	if err != nil {
		t.Fatal(err)
	}

	outcome := applyCRM(t, scope, engine, plan.ID, plan.Revision, crm.Command{
		Kind: crm.KindLinkOrder, OrderID: &linkedOrder.ID,
	})
	if outcome.Connection.State != crm.StateOrderLinked || derefCRM(outcome.Connection.CustomerID) != source.ID {
		t.Fatalf("independent link_order = %+v", outcome.Connection)
	}
	if outcome.Connection.LinkedOrderSnapshot == nil || outcome.Connection.LinkedOrderSnapshot.OrderID != linkedOrder.ID {
		t.Fatalf("missing epoch snapshot %+v", outcome.Connection.LinkedOrderSnapshot)
	}

	cancelled := order.StatusCancelled
	if _, err := orders.Update(ctx, scope, linkedOrder.ID, order.UpdateInput{Status: &cancelled}); err != nil {
		t.Fatal(err)
	}
	conn := loadConnection(t, scope, plan.ID)
	if conn.State != crm.StateOrderCancelled || derefCRM(conn.OrderID) != linkedOrder.ID {
		t.Fatalf("after cancel %+v", conn)
	}

	if err := orders.Delete(ctx, scope, linkedOrder.ID); err != nil {
		t.Fatal(err)
	}
	conn = loadConnection(t, scope, plan.ID)
	if conn.State != crm.StateOrderDeleted || conn.OrderID != nil || derefCRM(conn.CustomerID) != source.ID {
		t.Fatalf("after delete %+v", conn)
	}
	if conn.LinkedOrderSnapshot == nil {
		t.Fatal("order_deleted cleared append-only snapshot")
	}

	outcome = applyCRM(t, scope, engine, plan.ID, plan.Revision, crm.Command{Kind: crm.KindUnlinkCustomer})
	if outcome.Connection.State != crm.StateIndependent || outcome.Connection.LinkedOrderSnapshot != nil {
		t.Fatalf("unlink after delete = %+v", outcome.Connection)
	}

	linkedPlan, err := repo.Create(ctx, scope, shootplanning.CreatePlanInput{Title: "待合并", Subject: "主体"})
	if err != nil {
		t.Fatal(err)
	}
	applyCRM(t, scope, engine, linkedPlan.ID, linkedPlan.Revision, crm.Command{
		Kind: crm.KindLinkCustomer, CustomerID: &source.ID,
	})
	if _, err := customers.Merge(ctx, scope, target.ID, source.ID); err != nil {
		t.Fatal(err)
	}
	conn = loadConnection(t, scope, linkedPlan.ID)
	if derefCRM(conn.CustomerID) != target.ID {
		t.Fatalf("merge did not retarget customer: %+v", conn)
	}
}

func TestCRMScheduleProjectionAdoptAndSameFingerprintNoop(t *testing.T) {
	ctx := context.Background()
	db := openPlanningStore(t)
	scope := createPlanningAccount(t, db, "crm-schedule-acct")
	engine := crm.NewEngine()
	customers := customer.NewService(customer.NewPostgresRepository())
	orders := order.NewService(order.NewPostgresRepository().WithLifecycleParticipant(engine))
	slots := schedule.NewService(schedule.NewPostgresRepository().WithProjectionSink(engine), schedule.ClockFunc(time.Now))
	repo := shootplanning.NewPostgresRepository()

	cus := createCRMCustomer(t, customers, scope, "档期客户")
	title := "未来拍摄"
	linkedOrder, err := orders.Create(ctx, scope, order.CreateInput{
		CreationMode: order.CreationModeNew,
		CustomerID:   cus.ID,
		Title:        &title,
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := repo.Create(ctx, scope, shootplanning.CreatePlanInput{Title: "跟档期", Subject: "主体"})
	if err != nil {
		t.Fatal(err)
	}
	outcome := applyCRM(t, scope, engine, plan.ID, plan.Revision, crm.Command{
		Kind: crm.KindLinkOrder, OrderID: &linkedOrder.ID,
	})
	if outcome.Connection.State != crm.StateOrderLinked {
		t.Fatalf("link order before slot = %+v", outcome.Connection)
	}
	start := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Minute)
	end := start.Add(2 * time.Hour)
	created, err := slots.Create(ctx, scope, schedule.CreateInput{
		StartAt: start, EndAt: end, Type: schedule.TypeShoot, OrderID: &linkedOrder.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	proj := loadProjection(t, scope, plan.ID)
	if proj == nil || proj.Status != crm.StatusActiveApplied || derefCRM(proj.SlotID) != created.Slot.ID {
		t.Fatalf("after schedule create %+v", proj)
	}
	afterCreate := loadConnection(t, scope, plan.ID)
	beforeRevision := proj.ProjectionRevision
	if _, err := slots.Update(ctx, scope, created.Slot.ID, schedule.UpdateInput{
		StartAt: &start, EndAt: &end,
	}); err != nil {
		t.Fatal(err)
	}
	again := loadProjection(t, scope, plan.ID)
	if again == nil || again.ProjectionRevision != beforeRevision {
		t.Fatalf("same-value schedule patch bumped projection %+v -> %+v", proj, again)
	}
	conn := loadConnection(t, scope, plan.ID)
	if conn.ConnectionRevision != afterCreate.ConnectionRevision {
		t.Fatalf("same-value patch bumped connection %d -> %d", afterCreate.ConnectionRevision, conn.ConnectionRevision)
	}

	var window *crm.Window
	if err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		var loadErr error
		window, loadErr = crm.LoadWindow(ctx, tx, plan.ID)
		return loadErr
	}); err != nil {
		t.Fatal(err)
	}
	if window == nil || window.Source != "schedule_slot" {
		t.Fatalf("expected schedule window, got %+v", window)
	}

	detail, err := repo.Detail(ctx, scope, plan.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	stale := beforeRevision - 1
	if stale < 1 {
		stale = beforeRevision
	}
	_, err = applyCRMErr(t, scope, engine, plan.ID, detail.Revision, crm.Command{
		Kind: crm.KindAdopt, ProjectionRevision: &stale,
	})
	if !errors.Is(err, crm.ErrProjectionRevisionConflict) && !errors.Is(err, crm.ErrProjectionNotActive) {
		t.Fatalf("stale/unneeded adopt err = %v", err)
	}
}

type countingQueryScope struct {
	inner store.AccountScope
	n     int
}

func (c *countingQueryScope) Query(ctx context.Context, table, columns, cond string, args ...any) (store.Rows, error) {
	c.n++
	return c.inner.Query(ctx, table, columns, cond, args...)
}

func createCRMCustomer(t *testing.T, customers *customer.Service, scope store.AccountScope, name string) customer.Customer {
	t.Helper()
	created, err := customers.Create(context.Background(), scope, customer.CreateInput{
		DisplayName: name,
		Channel:     customer.ChannelOther,
		Identities:  []customer.IdentityInput{{Platform: customer.PlatformOther, Handle: name}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return created
}

func applyCRM(t *testing.T, scope store.AccountScope, engine *crm.Engine, planID string, revision int64, command crm.Command) crm.ApplyOutcome {
	t.Helper()
	outcome, err := applyCRMErr(t, scope, engine, planID, revision, command)
	if err != nil {
		t.Fatal(err)
	}
	return outcome
}

func applyCRMErr(t *testing.T, scope store.AccountScope, engine *crm.Engine, planID string, revision int64, command crm.Command) (crm.ApplyOutcome, error) {
	t.Helper()
	var outcome crm.ApplyOutcome
	err := scope.WithTxScope(context.Background(), func(tx store.TxAccountScope) error {
		var applyErr error
		outcome, applyErr = engine.ApplyCommandInScope(context.Background(), tx, planID, revision, command)
		return applyErr
	})
	return outcome, err
}

func loadConnection(t *testing.T, scope store.AccountScope, planID string) crm.Connection {
	t.Helper()
	var conn crm.Connection
	if err := scope.WithTxScope(context.Background(), func(tx store.TxAccountScope) error {
		var err error
		conn, err = crm.LoadConnection(context.Background(), tx, planID, false)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return conn
}

func loadProjection(t *testing.T, scope store.AccountScope, planID string) *crm.Projection {
	t.Helper()
	var proj *crm.Projection
	if err := scope.WithTxScope(context.Background(), func(tx store.TxAccountScope) error {
		var err error
		proj, err = crm.LoadProjection(context.Background(), tx, planID, false)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return proj
}

func derefCRM(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
