package dashboard_test

import (
	"context"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/samson/customer-manage-platform/backend/internal/dashboard"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	reminderdomain "github.com/samson/customer-manage-platform/backend/internal/reminder"
	"github.com/samson/customer-manage-platform/backend/internal/settings"
)

// stubTimezone 是注入 dashboard.Service 的设置 seam（design D9；v2 起同接口还需可约偏好）。
type stubTimezone struct {
	tz  string
	err error
}

func (s stubTimezone) TimezoneForAccount(context.Context, string) (string, error) {
	return s.tz, s.err
}

func (s stubTimezone) AvailabilityForAccount(context.Context, string) (settings.ScheduleAvailability, error) {
	return settings.DefaultScheduleAvailability(), nil
}

func (s stubTimezone) HealthTiersForAccount(context.Context, string) (settings.HealthTiers, error) {
	return settings.DefaultHealthTiers(), nil
}

func fixedClock(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

// TestDashboardGetAggregatesFiveBlocks 覆盖 S2/S3/S5/S6：
// due 近 3 天窗（含逾期、含 churn）、churn 全量、待收尾款窄口径、recent_stats（含 NULL price）。
func TestDashboardGetAggregatesFiveBlocks(t *testing.T) {
	ctx := context.Background()
	s := openDashboardStore(t)
	scope := createDashboardAccount(t, s, "acct-main")
	seedCustomer(t, scope, "cus-1", "客户甲")

	// now = 2026-07-14（Asia/Shanghai）→ today=07-14，dueBefore=07-16。
	now := time.Date(2026, 7, 14, 3, 0, 0, 0, time.UTC) // 07-14 11:00 SHA

	// --- reminders：due 窗 in/out/overdue + churn 双卡 ---
	seedReminder(t, scope, "rem-overdue", "custom", "cus-1", "2026-07-13", "pending", "k-overdue")    // 逾期 → due
	seedReminder(t, scope, "rem-today", "birthday", "cus-1", "2026-07-14", "pending", "k-today")      // 今日 → due
	seedReminder(t, scope, "rem-churn-in", "churn", "cus-1", "2026-07-15", "pending", "k-churn-in")   // due ∧ churn
	seedReminder(t, scope, "rem-t2", "follow_up", "cus-1", "2026-07-16", "pending", "k-t2")           // today+2 → due
	seedReminder(t, scope, "rem-future", "custom", "cus-1", "2026-07-17", "pending", "k-future")      // today+3 → 排除
	seedReminder(t, scope, "rem-done", "custom", "cus-1", "2026-07-14", "done", "k-done")             // 非 pending → 排除
	seedReminder(t, scope, "rem-churn-out", "churn", "cus-1", "2026-07-25", "pending", "k-churn-out") // churn only

	// --- orders：unpaid 窄口径 + recent_stats 窗口/取消/NULL price ---
	inWindow := time.Date(2026, 7, 10, 4, 0, 0, 0, time.UTC)    // 07-10 12:00 SHA，落 [today-29,today]
	deliveredIn := time.Date(2026, 7, 11, 4, 0, 0, 0, time.UTC) // 07-11 12:00 SHA
	deliveryDue := time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC) // 订单级覆盖的应交付日（date-only）
	outWindow := time.Date(2026, 5, 1, 4, 0, 0, 0, time.UTC)    // 远早于窗口

	price := func(v int) *int { return &v }
	seedOrder(t, scope, orderSeed{id: "ord-du", customerID: "cus-1", title: "已交付未结清", status: "delivered", balancePaid: false, depositPaid: true, price: price(80000), createdAt: inWindow, deliveredAt: &deliveredIn, shotAt: &deliveredIn, deliveryDueAt: &deliveryDue, deliveryDueIsOverride: true})
	seedOrder(t, scope, orderSeed{id: "ord-shot", customerID: "cus-1", title: "已拍未结清", status: "shot", balancePaid: false, depositPaid: true, price: price(60000), createdAt: inWindow})
	seedOrder(t, scope, orderSeed{id: "ord-dp", customerID: "cus-1", title: "已交付已结清", status: "delivered", balancePaid: true, depositPaid: true, price: price(50000), createdAt: inWindow, deliveredAt: &deliveredIn})
	seedOrder(t, scope, orderSeed{id: "ord-dp-null", customerID: "cus-1", title: "已结清无报价", status: "delivered", balancePaid: true, depositPaid: true, price: nil, createdAt: inWindow, deliveredAt: &deliveredIn})
	seedOrder(t, scope, orderSeed{id: "ord-cancel", customerID: "cus-1", title: "已取消", status: "cancelled", balancePaid: true, depositPaid: true, price: price(99999), createdAt: inWindow, deliveredAt: &deliveredIn})
	seedOrder(t, scope, orderSeed{id: "ord-old", customerID: "cus-1", title: "窗口外", status: "delivered", balancePaid: true, depositPaid: true, price: price(70000), createdAt: outWindow, deliveredAt: &outWindow})

	svc := dashboard.NewService(dashboard.NewPostgresRepository(), stubTimezone{tz: "Asia/Shanghai"}).WithClock(fixedClock(now))
	got, err := svc.Get(ctx, scope, "acct-main")
	if err != nil {
		t.Fatalf("dashboard get: %v", err)
	}

	// S2：due 窗含逾期与全部类型，不含 today+3 与 done，且排序 due_date ASC, id ASC。
	wantDue := []string{"rem-overdue", "rem-today", "rem-churn-in", "rem-t2"}
	if ids := reminderIDs(got.DueReminders); !equalStrings(ids, wantDue) {
		t.Fatalf("due_reminders = %v, want %v", ids, wantDue)
	}
	// S3：churn 全量 pending（含窗外），rem-churn-in 同时出现在两卡。
	wantChurn := []string{"rem-churn-in", "rem-churn-out"}
	if ids := reminderIDs(got.ChurnAlerts); !equalStrings(ids, wantChurn) {
		t.Fatalf("churn_alerts = %v, want %v", ids, wantChurn)
	}

	// S5：待收尾款仅 delivered ∧ !balance_paid，窄于 unpaid_balance（排除 shot）；count==len。
	if got.UnpaidOrders.Count != 1 || len(got.UnpaidOrders.Items) != 1 {
		t.Fatalf("unpaid count/len = %d/%d, want 1/1", got.UnpaidOrders.Count, len(got.UnpaidOrders.Items))
	}
	item := got.UnpaidOrders.Items[0]
	if item.ID != "ord-du" || item.CustomerDisplayName != "客户甲" {
		t.Fatalf("unpaid item = %+v", item)
	}
	// 交付事实必须原样投影：漏投影会让 dashboard 对每个订单谎报「非覆盖」。
	if item.DeliveryDueAt == nil || !item.DeliveryDueAt.Equal(deliveryDue) {
		t.Fatalf("unpaid item delivery_due_at = %v, want %v", item.DeliveryDueAt, deliveryDue)
	}
	if !item.DeliveryDueIsOverride {
		t.Fatalf("unpaid item delivery_due_is_override = false, want true（订单级覆盖不得被谎报为自动派生）")
	}

	// S6：recent_stats 三口径；NULL price 计 delivered 不抬 revenue；cancelled 排除 delivered/revenue 但计 created。
	if got.RecentStats.OrdersCreated != 5 {
		t.Fatalf("orders_created = %d, want 5", got.RecentStats.OrdersCreated)
	}
	if got.RecentStats.OrdersDelivered != 3 {
		t.Fatalf("orders_delivered = %d, want 3", got.RecentStats.OrdersDelivered)
	}
	if got.RecentStats.RevenueConfirmed != 50000 {
		t.Fatalf("revenue_confirmed = %d, want 50000", got.RecentStats.RevenueConfirmed)
	}

	// S12：时区读取失败 → error，绝不回退默认时区继续算。
	failing := dashboard.NewService(dashboard.NewPostgresRepository(), stubTimezone{err: context.DeadlineExceeded}).WithClock(fixedClock(now))
	if _, err := failing.Get(ctx, scope, "acct-main"); err == nil {
		t.Fatal("timezone failure should propagate error, not compute with default")
	}
	invalid := dashboard.NewService(dashboard.NewPostgresRepository(), stubTimezone{tz: "Not/AZone"}).WithClock(fixedClock(now))
	if _, err := invalid.Get(ctx, scope, "acct-main"); err == nil {
		t.Fatal("invalid IANA timezone should error, not fall back to default")
	}
}

// TestDashboardDayBoundaryFollowsTimezone 覆盖 S11：due 窗与今日档期日界随账号时区移动。
func TestDashboardDayBoundaryFollowsTimezone(t *testing.T) {
	ctx := context.Background()
	s := openDashboardStore(t)
	scope := createDashboardAccount(t, s, "acct-tz")
	seedCustomer(t, scope, "cus-1", "客户甲")

	// now = 2026-07-14 02:00 UTC。Shanghai(+8)=07-14 10:00 → today 07-14；
	// New York(-4 EDT)=07-13 22:00 → today 07-13。
	now := time.Date(2026, 7, 14, 2, 0, 0, 0, time.UTC)

	// due 窗判别：due 07-16 在上海窗内（dueBefore=07-16）、在纽约窗外（dueBefore=07-15）。
	seedReminder(t, scope, "rem-0716", "custom", "cus-1", "2026-07-16", "pending", "k-0716")

	// 今日档期判别：busy slot 07-14 08:00-09:00 UTC 落上海今日、不落纽约今日。
	seedBusySlot(t, scope, "slot-sha", time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC), time.Date(2026, 7, 14, 9, 0, 0, 0, time.UTC))

	shanghai := dashboard.NewService(dashboard.NewPostgresRepository(), stubTimezone{tz: "Asia/Shanghai"}).WithClock(fixedClock(now))
	sha, err := shanghai.Get(ctx, scope, "acct-tz")
	if err != nil {
		t.Fatalf("shanghai get: %v", err)
	}
	if ids := reminderIDs(sha.DueReminders); !equalStrings(ids, []string{"rem-0716"}) {
		t.Fatalf("shanghai due = %v, want [rem-0716]", ids)
	}
	if len(sha.TodaySlots) != 1 || sha.TodaySlots[0].ID != "slot-sha" {
		t.Fatalf("shanghai today_slots = %+v, want [slot-sha]", sha.TodaySlots)
	}

	newYork := dashboard.NewService(dashboard.NewPostgresRepository(), stubTimezone{tz: "America/New_York"}).WithClock(fixedClock(now))
	ny, err := newYork.Get(ctx, scope, "acct-tz")
	if err != nil {
		t.Fatalf("new york get: %v", err)
	}
	if len(ny.DueReminders) != 0 {
		t.Fatalf("new york due = %v, want empty (07-16 out of window)", reminderIDs(ny.DueReminders))
	}
	if len(ny.TodaySlots) != 0 {
		t.Fatalf("new york today_slots = %+v, want empty (slot not in NY today)", ny.TodaySlots)
	}
}

// --- fixtures ---

func openDashboardStore(t *testing.T) *store.Store {
	t.Helper()
	ctx := context.Background()
	ctr, err := tcpostgres.Run(ctx, "postgres:17-alpine",
		tcpostgres.WithDatabase("crm_test"),
		tcpostgres.WithUsername("crm_test"),
		tcpostgres.WithPassword("crm_test"),
		testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(60*time.Second)),
	)
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(ctr) })
	url, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}
	if err := store.MigrateUp(url); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	st, err := store.Open(ctx, url)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(st.Close)
	return st
}

func createDashboardAccount(t *testing.T, s *store.Store, id string) store.AccountScope {
	t.Helper()
	if err := s.CreateAccount(context.Background(), id, "test-hash"); err != nil {
		t.Fatalf("create account %s: %v", id, err)
	}
	return s.ScopeFor(auth.AccountContext{AccountID: id})
}

func seedCustomer(t *testing.T, scope store.AccountScope, id, name string) {
	t.Helper()
	if err := scope.Insert(context.Background(), "customers",
		[]string{"id", "display_name", "channel", "status"}, id, name, "other", "active"); err != nil {
		t.Fatalf("seed customer: %v", err)
	}
}

func seedReminder(t *testing.T, scope store.AccountScope, id, kind, customerID, dueDate, status, dedupKey string) {
	t.Helper()
	if err := scope.Insert(context.Background(), "reminders",
		[]string{"id", "type", "customer_id", "due_date", "content", "status", "dedup_key"},
		id, kind, customerID, dueDate, "提醒内容 "+id, status, dedupKey); err != nil {
		t.Fatalf("seed reminder %s: %v", id, err)
	}
}

type orderSeed struct {
	id          string
	customerID  string
	title       string
	status      string
	balancePaid bool
	depositPaid bool
	price       *int
	createdAt   time.Time
	deliveredAt *time.Time
	shotAt      *time.Time
	// 应交付日与覆盖标记：验证 dashboard 投影不谎报交付事实。
	deliveryDueAt         *time.Time
	deliveryDueIsOverride bool
}

func seedOrder(t *testing.T, scope store.AccountScope, o orderSeed) {
	t.Helper()
	cols := []string{"id", "customer_id", "title", "status", "balance_paid", "deposit_paid", "created_at"}
	args := []any{o.id, o.customerID, o.title, o.status, o.balancePaid, o.depositPaid, o.createdAt}
	if o.price != nil {
		cols = append(cols, "price")
		args = append(args, *o.price)
	}
	if o.deliveredAt != nil {
		cols = append(cols, "delivered_at")
		args = append(args, *o.deliveredAt)
	}
	if o.shotAt != nil {
		cols = append(cols, "shot_at")
		args = append(args, *o.shotAt)
	}
	if o.deliveryDueAt != nil {
		cols = append(cols, "delivery_due_at", "delivery_due_is_override")
		args = append(args, *o.deliveryDueAt, o.deliveryDueIsOverride)
	}
	if err := scope.Insert(context.Background(), "orders", cols, args...); err != nil {
		t.Fatalf("seed order %s: %v", o.id, err)
	}
}

func seedBusySlot(t *testing.T, scope store.AccountScope, id string, start, end time.Time) {
	t.Helper()
	if err := scope.Insert(context.Background(), "schedule_slots",
		[]string{"id", "start_at", "end_at", "type"}, id, start, end, "busy"); err != nil {
		t.Fatalf("seed slot %s: %v", id, err)
	}
}

func reminderIDs(items []reminderdomain.Reminder) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.ID)
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
