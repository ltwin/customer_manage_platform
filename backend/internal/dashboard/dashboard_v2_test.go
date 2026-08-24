package dashboard_test

import (
	"context"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/dashboard"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/settings"
)

// stubV2Settings 同时提供时区与可约偏好（settings.Service 满足同一接口）。
type stubV2Settings struct {
	tz           string
	tzErr        error
	availability settings.ScheduleAvailability
}

func (s stubV2Settings) TimezoneForAccount(context.Context, string) (string, error) {
	return s.tz, s.tzErr
}

func (s stubV2Settings) AvailabilityForAccount(context.Context, string) (settings.ScheduleAvailability, error) {
	return s.availability, nil
}

func availabilityPtr(start, end string) *settings.ScheduleAvailabilityWindow {
	return &settings.ScheduleAvailabilityWindow{Start: start, End: end}
}

func v2TestAvailability() settings.ScheduleAvailability {
	return settings.ScheduleAvailability{
		Weekly: settings.ScheduleAvailabilityWeekly{
			Monday:    availabilityPtr("10:00", "19:00"),
			Tuesday:   availabilityPtr("10:00", "19:00"),
			Wednesday: availabilityPtr("10:00", "19:00"),
			Thursday:  availabilityPtr("10:00", "19:00"),
			Friday:    availabilityPtr("10:00", "19:00"),
			Saturday:  availabilityPtr("09:00", "20:00"),
		},
		MinOpeningMinutes: 120,
		TurnaroundMinutes: 60,
	}
}

type v2OrderSeed struct {
	id                    string
	customerID            string
	title                 string
	status                string
	balancePaid           bool
	depositPaid           bool
	price                 *int
	createdAt             time.Time
	shotAt                *time.Time
	deliveredAt           *time.Time
	deliveryDueAt         *time.Time
	deliveryDueIsOverride bool
	amountPaid            int
	outstanding           *int
	paidAt                *time.Time
	channelSnapshot       string
	shootTypeSnapshot     *string
}

func seedV2Customer(t *testing.T, scope store.AccountScope, id, name, channel string) {
	t.Helper()
	if err := scope.Insert(context.Background(), "customers",
		[]string{"id", "display_name", "channel", "status"}, id, name, channel, "active"); err != nil {
		t.Fatalf("seed customer %s: %v", id, err)
	}
}

func seedV2Order(t *testing.T, scope store.AccountScope, o v2OrderSeed) {
	t.Helper()
	cols := []string{"id", "customer_id", "title", "status", "balance_paid", "deposit_paid", "created_at", "amount_paid", "channel_snapshot"}
	args := []any{o.id, o.customerID, o.title, o.status, o.balancePaid, o.depositPaid, o.createdAt, o.amountPaid, o.channelSnapshot}
	if o.price != nil {
		cols = append(cols, "price")
		args = append(args, *o.price)
	}
	if o.shotAt != nil {
		cols = append(cols, "shot_at")
		args = append(args, *o.shotAt)
	}
	if o.deliveredAt != nil {
		cols = append(cols, "delivered_at")
		args = append(args, *o.deliveredAt)
	}
	if o.deliveryDueAt != nil {
		cols = append(cols, "delivery_due_at", "delivery_due_is_override")
		args = append(args, *o.deliveryDueAt, o.deliveryDueIsOverride)
	}
	if o.outstanding != nil {
		cols = append(cols, "outstanding_amount")
		args = append(args, *o.outstanding)
	}
	if o.paidAt != nil {
		cols = append(cols, "paid_at")
		args = append(args, *o.paidAt)
	}
	if o.shootTypeSnapshot != nil {
		cols = append(cols, "shoot_type_snapshot")
		args = append(args, *o.shootTypeSnapshot)
	}
	if err := scope.Insert(context.Background(), "orders", cols, args...); err != nil {
		t.Fatalf("seed order %s: %v", o.id, err)
	}
}

func seedV2Slot(t *testing.T, scope store.AccountScope, id string, start, end time.Time, slotType string, orderID *string) {
	t.Helper()
	cols := []string{"id", "start_at", "end_at", "type"}
	args := []any{id, start, end, slotType}
	if orderID != nil {
		cols = append(cols, "order_id")
		args = append(args, *orderID)
	}
	if err := scope.Insert(context.Background(), "schedule_slots", cols, args...); err != nil {
		t.Fatalf("seed slot %s: %v", id, err)
	}
}

func seedV2Reminder(t *testing.T, scope store.AccountScope, id, kind string, customerID *string, dueDate, status, dedupKey string) {
	t.Helper()
	var customerArg any
	if customerID != nil {
		customerArg = *customerID
	}
	if err := scope.Insert(context.Background(), "reminders",
		[]string{"id", "type", "customer_id", "due_date", "content", "status", "dedup_key"},
		id, kind, customerArg, dueDate, "提醒内容 "+id, status, dedupKey); err != nil {
		t.Fatalf("seed reminder %s: %v", id, err)
	}
}

func intPtr(v int) *int         { return &v }
func strPtrV2(v string) *string { return &v }

// TestDashboardV2AggregatesReadModel 锁住 /dashboard/v2 八块口径（roadmap §4.3 2026-08-24 定稿）：
// next_shoot 跨日/取消、today_openings 权威算法、交付队列逾期、瀑布三段+环比+AOV+复购+账龄、
// 月利用率 parity、渠道矩阵未归因桶、待办客户摘要投影。
func TestDashboardV2AggregatesReadModel(t *testing.T) {
	ctx := context.Background()
	s := openDashboardStore(t)
	scope := createDashboardAccount(t, s, "acct-v2")
	seedV2Customer(t, scope, "cus-1", "阿茶", "douyin")
	seedV2Customer(t, scope, "cus-2", "Ki酱", "weibo")
	seedV2Customer(t, scope, "cus-3", "苏晚", "xiaohongshu")

	// now = 2026-07-14 11:00 SHA → today=07-14（周二）；
	// 近 30 天窗 [06-15, 07-15)，上一窗 [05-16, 06-15)，90 天复购窗 [04-15, 07-15)。
	now := time.Date(2026, 7, 14, 3, 0, 0, 0, time.UTC)
	createdAt := time.Date(2026, 7, 1, 4, 0, 0, 0, time.UTC)

	// --- 瀑布：已确认（当前窗 90000 / 上一窗 60000 → 环比 +0.5） ---
	seedV2Order(t, scope, v2OrderSeed{id: "ord-conf-cur", customerID: "cus-1", title: "已结清当前窗", status: "delivered",
		balancePaid: true, depositPaid: true, price: intPtr(90000), createdAt: createdAt,
		deliveredAt: ptrTime(time.Date(2026, 7, 12, 4, 0, 0, 0, time.UTC)),
		amountPaid:  90000, paidAt: ptrTime(time.Date(2026, 7, 12, 5, 0, 0, 0, time.UTC)),
		channelSnapshot: "douyin", shootTypeSnapshot: strPtrV2("portrait")})
	seedV2Order(t, scope, v2OrderSeed{id: "ord-conf-prev", customerID: "cus-3", title: "已结清上一窗", status: "delivered",
		balancePaid: true, depositPaid: true, price: intPtr(40000), createdAt: createdAt,
		deliveredAt: ptrTime(time.Date(2026, 6, 10, 4, 0, 0, 0, time.UTC)),
		amountPaid:  40000, channelSnapshot: "xiaohongshu"})
	seedV2Order(t, scope, v2OrderSeed{id: "ord-mtx", customerID: "cus-2", title: "已完结上一窗", status: "closed",
		balancePaid: true, depositPaid: true, price: intPtr(20000), createdAt: createdAt,
		deliveredAt: ptrTime(time.Date(2026, 5, 20, 4, 0, 0, 0, time.UTC)),
		amountPaid:  20000, channelSnapshot: "weibo", shootTypeSnapshot: strPtrV2("cosplay")})

	// --- 待收：outstanding 求和 + 计笔数不计金额（录满未点收讫）+ 账龄锚最早 delivered_at ---
	seedV2Order(t, scope, v2OrderSeed{id: "ord-recv", customerID: "cus-1", title: "已交付未结清", status: "delivered",
		balancePaid: false, depositPaid: true, price: intPtr(80000), createdAt: createdAt,
		deliveredAt: ptrTime(time.Date(2026, 7, 11, 4, 0, 0, 0, time.UTC)),
		amountPaid:  30000, outstanding: intPtr(50000), channelSnapshot: "douyin"})
	seedV2Order(t, scope, v2OrderSeed{id: "ord-recv-zero", customerID: "cus-3", title: "录满未点收讫", status: "delivered",
		balancePaid: false, depositPaid: true, price: intPtr(80000), createdAt: createdAt,
		deliveredAt: ptrTime(time.Date(2026, 7, 12, 6, 0, 0, 0, time.UTC)),
		amountPaid:  80000, outstanding: intPtr(0), channelSnapshot: "xiaohongshu"})

	// --- 在途（scheduled/shot/selected/retouching 的 price）+ 已收现金（paid_at 落窗） ---
	seedV2Order(t, scope, v2OrderSeed{id: "ord-next", customerID: "cus-1", title: "今日拍摄", status: "scheduled",
		price: intPtr(50000), createdAt: createdAt, amountPaid: 0, channelSnapshot: "douyin"})
	seedV2Order(t, scope, v2OrderSeed{id: "ord-cross", customerID: "cus-2", title: "跨日拍摄", status: "scheduled",
		price: intPtr(10000), createdAt: createdAt, amountPaid: 0, channelSnapshot: "weibo"})
	seedV2Order(t, scope, v2OrderSeed{id: "ord-shot", customerID: "cus-3", title: "已拍逾期", status: "shot",
		price: intPtr(60000), createdAt: createdAt, shotAt: ptrTime(time.Date(2026, 7, 10, 4, 0, 0, 0, time.UTC)),
		deliveryDueAt: ptrTime(time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)), amountPaid: 0, channelSnapshot: "xiaohongshu"})
	seedV2Order(t, scope, v2OrderSeed{id: "ord-sel", customerID: "cus-2", title: "已选片", status: "selected",
		price: intPtr(70000), createdAt: createdAt, shotAt: ptrTime(time.Date(2026, 7, 8, 4, 0, 0, 0, time.UTC)),
		deliveryDueAt: ptrTime(time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)), amountPaid: 0, channelSnapshot: "weibo"})
	seedV2Order(t, scope, v2OrderSeed{id: "ord-cash", customerID: "cus-2", title: "定金已收未定档价", status: "scheduled",
		createdAt: createdAt, amountPaid: 25000, paidAt: ptrTime(time.Date(2026, 7, 10, 4, 0, 0, 0, time.UTC)),
		channelSnapshot: "weibo"})
	seedV2Order(t, scope, v2OrderSeed{id: "ord-cancel", customerID: "cus-1", title: "已取消", status: "cancelled",
		balancePaid: true, price: intPtr(99000), createdAt: createdAt,
		deliveredAt: ptrTime(time.Date(2026, 7, 12, 4, 0, 0, 0, time.UTC)),
		amountPaid:  99000, paidAt: ptrTime(time.Date(2026, 7, 12, 4, 0, 0, 0, time.UTC)), channelSnapshot: "douyin"})

	// --- 90 天复购窗：cus-1 两拍（ord-r1/ord-r2）、cus-2 两拍（ord-r3/ord-sel）、
	// cus-3 一拍（ord-shot）→ 复购客户 2/3 ---
	seedV2Order(t, scope, v2OrderSeed{id: "ord-r1", customerID: "cus-1", title: "复购一", status: "shot",
		price: intPtr(1000), createdAt: createdAt, shotAt: ptrTime(time.Date(2026, 7, 1, 4, 0, 0, 0, time.UTC)),
		deliveryDueAt: ptrTime(time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)), amountPaid: 0, channelSnapshot: "douyin"})
	seedV2Order(t, scope, v2OrderSeed{id: "ord-r2", customerID: "cus-1", title: "复购二", status: "shot",
		price: intPtr(1000), createdAt: createdAt, shotAt: ptrTime(time.Date(2026, 7, 8, 4, 0, 0, 0, time.UTC)),
		deliveryDueAt: ptrTime(time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC)), amountPaid: 0, channelSnapshot: "douyin"})
	seedV2Order(t, scope, v2OrderSeed{id: "ord-r3", customerID: "cus-2", title: "单拍", status: "shot",
		price: intPtr(1000), createdAt: createdAt, shotAt: ptrTime(time.Date(2026, 7, 5, 4, 0, 0, 0, time.UTC)),
		deliveryDueAt: ptrTime(time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC)), amountPaid: 0, channelSnapshot: "weibo"})

	// --- 档期：next_shoot 取跨日进行中（07-13 23:00 → 07-14 01:00 SHA）；取消 shoot 不入候选 ---
	seedV2Slot(t, scope, "slot-cross", time.Date(2026, 7, 13, 15, 0, 0, 0, time.UTC), time.Date(2026, 7, 13, 17, 0, 0, 0, time.UTC), "shoot", strPtrV2("ord-cross"))
	seedV2Slot(t, scope, "slot-today", time.Date(2026, 7, 14, 4, 0, 0, 0, time.UTC), time.Date(2026, 7, 14, 6, 0, 0, 0, time.UTC), "shoot", strPtrV2("ord-next"))
	seedV2Slot(t, scope, "slot-cancel", time.Date(2026, 7, 14, 5, 0, 0, 0, time.UTC), time.Date(2026, 7, 14, 7, 0, 0, 0, time.UTC), "shoot", strPtrV2("ord-cancel"))
	seedV2Slot(t, scope, "slot-busy", time.Date(2026, 7, 14, 7, 0, 0, 0, time.UTC), time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC), "busy", nil)

	// --- 待办：近 3 天窗 + 客户摘要（无客户 → summary null） ---
	cus1 := "cus-1"
	cus2 := "cus-2"
	seedV2Reminder(t, scope, "rem-1", "custom", &cus1, "2026-07-13", "pending", "k-v2-1")
	seedV2Reminder(t, scope, "rem-3", "custom", nil, "2026-07-14", "pending", "k-v2-3")
	seedV2Reminder(t, scope, "rem-2", "birthday", &cus2, "2026-07-15", "pending", "k-v2-2")

	svc := dashboard.NewService(dashboard.NewPostgresRepository(), stubV2Settings{tz: "Asia/Shanghai", availability: v2TestAvailability()}).WithClock(fixedClock(now))
	got, err := svc.GetV2(ctx, scope, "acct-v2")
	if err != nil {
		t.Fatalf("dashboard v2 get: %v", err)
	}

	// next_shoot：跨日进行中的 shoot（end_at 越过今日 00:00 即候选，start_at 最早优先于取消档期）。
	if got.NextShoot == nil || got.NextShoot.ID != "slot-cross" {
		t.Fatalf("next_shoot = %+v, want slot-cross（进行中的跨日拍摄）", got.NextShoot)
	}
	// today_slots：与今日相交 4 条（跨日、今日 shoot、取消 shoot、busy）。
	if len(got.TodaySlots) != 4 {
		t.Fatalf("today_slots = %d, want 4", len(got.TodaySlots))
	}
	// today_openings：周二窗 10:00–19:00；占用 = 12–14 shoot + 15–16 busy（取消跳过、跨日 00–01 在窗外）
	// → 空档 [10:00,12:00] 与 [16:00,19:00]（14:00–15:00 仅 60 分钟 <120 不计）。
	if got.TodayOpenings.WorkingWindow == nil {
		t.Fatal("today working_window = nil, want 10:00–19:00")
	}
	if !got.TodayOpenings.WorkingWindow.StartAt.Equal(time.Date(2026, 7, 14, 2, 0, 0, 0, time.UTC)) ||
		!got.TodayOpenings.WorkingWindow.EndAt.Equal(time.Date(2026, 7, 14, 11, 0, 0, 0, time.UTC)) {
		t.Fatalf("today working_window = [%s, %s]", got.TodayOpenings.WorkingWindow.StartAt.UTC(), got.TodayOpenings.WorkingWindow.EndAt.UTC())
	}
	if len(got.TodayOpenings.Openings) != 2 {
		t.Fatalf("today openings = %d, want 2", len(got.TodayOpenings.Openings))
	}
	if !got.TodayOpenings.Openings[0].StartAt.Equal(time.Date(2026, 7, 14, 2, 0, 0, 0, time.UTC)) ||
		!got.TodayOpenings.Openings[0].EndAt.Equal(time.Date(2026, 7, 14, 4, 0, 0, 0, time.UTC)) {
		t.Fatalf("first opening = [%s, %s], want [10:00, 12:00] SHA", got.TodayOpenings.Openings[0].StartAt.UTC(), got.TodayOpenings.Openings[0].EndAt.UTC())
	}
	if !got.TodayOpenings.Openings[1].StartAt.Equal(time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC)) ||
		!got.TodayOpenings.Openings[1].EndAt.Equal(time.Date(2026, 7, 14, 11, 0, 0, 0, time.UTC)) {
		t.Fatalf("second opening = [%s, %s], want [16:00, 19:00] SHA", got.TodayOpenings.Openings[1].StartAt.UTC(), got.TodayOpenings.Openings[1].EndAt.UTC())
	}

	// delivery_queue：应交日 ASC；逾期在最前；days_left 按账号本地自然日。
	if got.DeliveryQueue.Count != 5 || len(got.DeliveryQueue.Items) != 5 {
		t.Fatalf("delivery queue = %d/%d, want 5/5", got.DeliveryQueue.Count, len(got.DeliveryQueue.Items))
	}
	wantQueue := []struct {
		id       string
		daysLeft int
		overdue  bool
	}{
		{"ord-shot", -4, true},
		{"ord-r1", 1, false},
		{"ord-sel", 2, false},
		{"ord-r3", 5, false},
		{"ord-r2", 8, false},
	}
	for i, want := range wantQueue {
		item := got.DeliveryQueue.Items[i]
		if item.Order.ID != want.id || item.DaysLeft == nil || *item.DaysLeft != want.daysLeft || item.Overdue != want.overdue {
			t.Fatalf("queue[%d] = %s days=%v overdue=%v, want %s days=%d overdue=%v",
				i, item.Order.ID, item.DaysLeft, item.Overdue, want.id, want.daysLeft, want.overdue)
		}
	}

	// 瀑布：已确认 90000/60000（环比 0.5）；待收 50000×2 笔；在途 193000×7 笔（price NULL 不计）；
	// 已收现金 115000（paid_at 落窗；cancelled 的 99000 不计）；AOV 90000；复购 2/3；账龄 3 天。
	wf := got.Waterfall
	if wf.ConfirmedCurrent != 90000 || wf.ConfirmedPrevious != 60000 {
		t.Fatalf("confirmed = %d/%d, want 90000/60000", wf.ConfirmedCurrent, wf.ConfirmedPrevious)
	}
	if wf.ConfirmedChangeRatio == nil || *wf.ConfirmedChangeRatio != 0.5 {
		t.Fatalf("confirmed change ratio = %v, want 0.5", wf.ConfirmedChangeRatio)
	}
	if wf.ReceivableTotal != 50000 || wf.ReceivableCount != 2 {
		t.Fatalf("receivable = %d/%d, want 50000/2（录满未点收讫计笔数不计金额）", wf.ReceivableTotal, wf.ReceivableCount)
	}
	if wf.PipelineTotal != 193000 || wf.PipelineCount != 7 {
		t.Fatalf("pipeline = %d/%d, want 193000/7", wf.PipelineTotal, wf.PipelineCount)
	}
	if wf.CashReceived30d != 115000 {
		t.Fatalf("cash_received_30d = %d, want 115000", wf.CashReceived30d)
	}
	if wf.AverageOrderValue == nil || *wf.AverageOrderValue != 90000 {
		t.Fatalf("average_order_value = %v, want 90000", wf.AverageOrderValue)
	}
	if wf.RepeatCustomerRatio90d == nil {
		t.Fatal("repeat_customer_ratio_90d = nil, want 0.6667")
	} else if *wf.RepeatCustomerRatio90d != 0.6667 {
		t.Fatalf("repeat_customer_ratio_90d = %v, want 0.6667", *wf.RepeatCustomerRatio90d)
	}
	if wf.OldestReceivableAgeDays == nil || *wf.OldestReceivableAgeDays != 3 {
		t.Fatalf("oldest_receivable_age_days = %v, want 3", wf.OldestReceivableAgeDays)
	}

	// 月利用率 parity：2026-07 分母 = 23 个工作日×540 + 4 个周六×660 = 15060；
	// 分子 = 今日 shoot 120 分钟（跨日段在窗外、busy/取消不计）→ 0.8%。
	if got.Utilization.Month != "2026-07" {
		t.Fatalf("utilization month = %s, want 2026-07", got.Utilization.Month)
	}
	if got.Utilization.Utilization == nil || *got.Utilization.Utilization != 0.8 {
		t.Fatalf("utilization = %v, want 0.8", got.Utilization.Utilization)
	}
	if got.Utilization.ShootCount != 2 || got.Utilization.HoldDays != 0 || got.Utilization.ConflictDays != 0 {
		t.Fatalf("utilization counts = %d/%d/%d, want 2/0/0（取消 slot 不计 shoot_count）",
			got.Utilization.ShootCount, got.Utilization.HoldDays, got.Utilization.ConflictDays)
	}
	if got.Utilization.OpenDays != 27 {
		t.Fatalf("open_days = %d, want 27（27 个已配置工作日均有 ≥120 分钟空档）", got.Utilization.OpenDays)
	}

	// 渠道矩阵：累计、按下单时快照归因；NULL shoot_type → 未归因桶；行按 total DESC。
	if len(got.ChannelMatrix.Rows) != 3 {
		t.Fatalf("matrix rows = %d, want 3", len(got.ChannelMatrix.Rows))
	}
	if row := got.ChannelMatrix.Rows[0]; row.Channel != "douyin" || row.Portrait != 90000 || row.Total != 90000 || row.OrderCount != 1 || row.CustomerCount != 1 {
		t.Fatalf("matrix row 0 = %+v, want douyin/portrait 90000", row)
	}
	if row := got.ChannelMatrix.Rows[1]; row.Channel != "xiaohongshu" || row.Unattributed != 40000 {
		t.Fatalf("matrix row 1 = %+v, want xiaohongshu unattributed 40000", row)
	}
	if row := got.ChannelMatrix.Rows[2]; row.Channel != "weibo" || row.Cosplay != 20000 {
		t.Fatalf("matrix row 2 = %+v, want weibo cosplay 20000", row)
	}
	if got.ChannelMatrix.GrandTotal != 150000 {
		t.Fatalf("matrix grand = %d, want 150000", got.ChannelMatrix.GrandTotal)
	}

	// 待办行样式：排序 due ASC；客户摘要批量投影；无客户 → nil。
	if len(got.DueReminders) != 3 {
		t.Fatalf("due reminders = %d, want 3", len(got.DueReminders))
	}
	if got.DueReminders[0].ID != "rem-1" || got.DueReminders[0].CustomerSummary == nil ||
		got.DueReminders[0].CustomerSummary.DisplayName != "阿茶" || got.DueReminders[0].CustomerSummary.Channel != "douyin" {
		t.Fatalf("reminder 0 = %+v, want rem-1 with 阿茶/douyin", got.DueReminders[0])
	}
	if got.DueReminders[1].ID != "rem-3" || got.DueReminders[1].CustomerSummary != nil {
		t.Fatalf("reminder 1 = %+v, want rem-3 with nil summary", got.DueReminders[1])
	}
	if got.DueReminders[2].ID != "rem-2" || got.DueReminders[2].CustomerSummary == nil ||
		got.DueReminders[2].CustomerSummary.DisplayName != "Ki酱" || got.DueReminders[2].CustomerSummary.Channel != "weibo" {
		t.Fatalf("reminder 2 = %+v, want rem-2 with Ki酱/weibo", got.DueReminders[2])
	}

	// S12 同源约束：时区读取失败 → error，绝不回退默认时区继续算。
	failing := dashboard.NewService(dashboard.NewPostgresRepository(), stubV2Settings{tzErr: context.DeadlineExceeded, availability: v2TestAvailability()}).WithClock(fixedClock(now))
	if _, err := failing.GetV2(ctx, scope, "acct-v2"); err == nil {
		t.Fatal("timezone failure should propagate error, not compute with default")
	}
	invalid := dashboard.NewService(dashboard.NewPostgresRepository(), stubV2Settings{tz: "Not/AZone", availability: v2TestAvailability()}).WithClock(fixedClock(now))
	if _, err := invalid.GetV2(ctx, scope, "acct-v2"); err == nil {
		t.Fatal("invalid IANA timezone should error, not fall back to default")
	}
}

// TestDashboardV2NoAvailabilityBoundaries：周日未配置 → 今日 working_window 缺省、
// 全月分母 0 → utilization 缺省；今日无档期 → openings 为整窗。
func TestDashboardV2NoAvailabilityBoundaries(t *testing.T) {
	ctx := context.Background()
	s := openDashboardStore(t)
	scope := createDashboardAccount(t, s, "acct-v2-empty")
	seedV2Customer(t, scope, "cus-1", "客户甲", "other")

	// 周日全停用（今日 2026-07-19 是周日）。
	empty := settings.ScheduleAvailability{
		Weekly:            settings.ScheduleAvailabilityWeekly{},
		MinOpeningMinutes: 120,
		TurnaroundMinutes: 60,
	}
	now := time.Date(2026, 7, 19, 3, 0, 0, 0, time.UTC) // 11:00 SHA, today 07-19 周日
	seedV2Order(t, scope, v2OrderSeed{id: "ord-1", customerID: "cus-1", title: "空账号线索", status: "consulting",
		createdAt: now, amountPaid: 0, channelSnapshot: "other"})

	svc := dashboard.NewService(dashboard.NewPostgresRepository(), stubV2Settings{tz: "Asia/Shanghai", availability: empty}).WithClock(fixedClock(now))
	got, err := svc.GetV2(ctx, scope, "acct-v2-empty")
	if err != nil {
		t.Fatalf("get v2: %v", err)
	}
	if got.TodayOpenings.WorkingWindow != nil || len(got.TodayOpenings.Openings) != 0 {
		t.Fatalf("no-availability day: window=%v openings=%d, want nil/0", got.TodayOpenings.WorkingWindow, len(got.TodayOpenings.Openings))
	}
	if got.Utilization.Utilization != nil {
		t.Fatalf("empty month utilization = %v, want nil（分母 0）", got.Utilization.Utilization)
	}
	if got.Utilization.Month != "2026-07" {
		t.Fatalf("month = %s, want 2026-07", got.Utilization.Month)
	}
	if got.NextShoot != nil {
		t.Fatalf("next_shoot = %+v, want nil", got.NextShoot)
	}
	if got.Waterfall.ConfirmedChangeRatio != nil || got.Waterfall.AverageOrderValue != nil ||
		got.Waterfall.RepeatCustomerRatio90d != nil || got.Waterfall.OldestReceivableAgeDays != nil {
		t.Fatalf("空数据派生指标应全部缺省: %+v", got.Waterfall)
	}
}

func ptrTime(t time.Time) *time.Time { return &t }
