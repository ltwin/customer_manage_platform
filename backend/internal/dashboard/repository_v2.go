// repository_v2.go 是 GET /dashboard/v2 的聚合读面：经 AccountScope 直查多表装配事实行，
// 纯算法派生（空档/利用率/环比/矩阵排序）留在 Service（ADR-001 / compound cross-domain-read-model）。
package dashboard

import (
	"context"
	"sort"
	"time"

	orderdomain "github.com/samson/customer-manage-platform/backend/internal/order"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	reminderdomain "github.com/samson/customer-manage-platform/backend/internal/reminder"
	scheduledomain "github.com/samson/customer-manage-platform/backend/internal/schedule"
)

// V2Window 是 Service 依账号时区算好、交给 Repository 的 v2 日界与窗口集合。
type V2Window struct {
	Today       time.Time // date-only：账号本地今日
	DueBefore   time.Time // date-only：今日+2
	DayStart    time.Time // 瞬时：本地今日 00:00（含）
	DayEnd      time.Time // 瞬时：本地次日 00:00（不含）
	RecentStart time.Time // 瞬时：近 30 天窗下界
	RecentEnd   time.Time // 瞬时：近 30 天窗上界（= DayEnd）
	// PreviousStart：环比上一窗 [今日-59, 今日-29) 下界。
	PreviousStart time.Time
	// RepeatStart：90 天复购窗 [今日-89, 今日+1) 下界。
	RepeatStart time.Time
	// MonthStart：账号本地当前自然月 1 号（date-only）；MonthDays 天数；MonthEnd 次月 1 日瞬时。
	MonthStart time.Time
	MonthDays  int
	MonthEnd   time.Time
	// NextShootTo：next_shoot 候选扫描上界（DayStart + 365 天工程上限）。
	NextShootTo time.Time
}

// MatrixSourceRow 是渠道矩阵的原始行（balance_paid ∧ 非 cancelled ∧ price 非空）。
type MatrixSourceRow struct {
	ChannelSnapshot   string
	ShootTypeSnapshot *string
	Price             int
	CustomerID        string
}

// V2Facts 是 Repository 一次装配出的 v2 原始事实；派生指标由 Service 计算。
type V2Facts struct {
	DueReminders  []DueReminderItem
	TodaySlots    []scheduledomain.ListItem
	NextShootPool []scheduledomain.ListItem // [DayStart, NextShootTo) 全量档期（已按 start_at 排序）
	MonthSlots    []scheduledomain.ListItem // [MonthStart, MonthEnd) 全量档期
	Unpaid        UnpaidOrders              // 复用 v1 行集：待收段金额/笔数/最早账龄同源
	Queue         []orderdomain.ListItem    // shot/selected/retouching，应交日 ASC NULLS LAST

	ConfirmedCurrent      int
	ConfirmedCurrentCount int // price 非空的已确认订单数（AOV 分母）
	ConfirmedPrevious     int
	PipelineTotal         int
	PipelineCount         int
	Cash30d               int

	// RepeatWindowShots：90 天窗内 shot_at 落窗且非 cancelled 订单的 customer_id 逐行列表。
	RepeatWindowShots []string
	MatrixRows        []MatrixSourceRow

	// HealthCustomers / HealthOrders：健康度分层的原始事实行（customer-health-tiers，
	// 瞬时时刻；service 按账号时区折算 date-only 后进纯函数）。
	HealthCustomers []healthCustomerSource
	HealthOrders    []healthOrderSource
}

// healthCustomerSource 是健康度盘点的客户事实行：仅 status=active（archived/merged 不入）。
type healthCustomerSource struct {
	ID          string
	DisplayName string
	Channel     string
	CreatedAt   time.Time // 瞬时
}

// healthOrderSource 是健康度盘点的订单事实行：非 cancelled 全量
// （节奏样本与双金额口径共用）。ShotAt nil = 未拍摄（金额照计、不入节奏样本）。
type healthOrderSource struct {
	CustomerID  string
	ShotAt      *time.Time // 瞬时
	Price       *int
	BalancePaid bool
	AmountPaid  *int
}

// pipelineStatuses 是「在途」冻结口径的状态集（roadmap §4.3：已定档进入执行链但尚未交付确认）。
var pipelineStatuses = []string{
	orderdomain.StatusScheduled, orderdomain.StatusShot, orderdomain.StatusSelected, orderdomain.StatusRetouching,
}

// deliveryQueueStatuses 是交付队列口径：已拍未交付（应交日事实来自 ITEM-1）。
var deliveryQueueStatuses = []string{
	orderdomain.StatusShot, orderdomain.StatusSelected, orderdomain.StatusRetouching,
}

// LoadDashboardV2 按窗口一次装配 v2 事实块。
func (PostgresRepository) LoadDashboardV2(
	ctx context.Context,
	scope store.AccountScope,
	window V2Window,
) (V2Facts, error) {
	reminders, err := loadDueReminders(ctx, scope, window.DueBefore)
	if err != nil {
		return V2Facts{}, err
	}
	dueItems, err := assembleDueReminderItems(ctx, scope, reminders)
	if err != nil {
		return V2Facts{}, err
	}
	todaySlots, err := scheduledomain.AssembleListItems(ctx, scope, scheduledomain.ListFilter{
		From: window.DayStart,
		To:   window.DayEnd,
	})
	if err != nil {
		return V2Facts{}, err
	}
	nextShootPool, err := scheduledomain.AssembleListItems(ctx, scope, scheduledomain.ListFilter{
		From: window.DayStart,
		To:   window.NextShootTo,
	})
	if err != nil {
		return V2Facts{}, err
	}
	monthSlots, err := scheduledomain.AssembleListItems(ctx, scope, scheduledomain.ListFilter{
		From: window.MonthStart,
		To:   window.MonthEnd,
	})
	if err != nil {
		return V2Facts{}, err
	}
	unpaid, err := loadUnpaidOrders(ctx, scope)
	if err != nil {
		return V2Facts{}, err
	}
	queue, err := loadDeliveryQueue(ctx, scope)
	if err != nil {
		return V2Facts{}, err
	}
	confirmedCurrent, confirmedCurrentCount, confirmedPrevious, err := loadConfirmedWindowSums(ctx, scope, window)
	if err != nil {
		return V2Facts{}, err
	}
	pipelineTotal, pipelineCount, err := loadPipelineSums(ctx, scope)
	if err != nil {
		return V2Facts{}, err
	}
	cash, err := loadCashReceived(ctx, scope, window)
	if err != nil {
		return V2Facts{}, err
	}
	repeatShots, err := loadRepeatWindowShots(ctx, scope, window)
	if err != nil {
		return V2Facts{}, err
	}
	matrixRows, err := loadMatrixRows(ctx, scope)
	if err != nil {
		return V2Facts{}, err
	}
	healthCustomers, err := loadHealthCustomers(ctx, scope)
	if err != nil {
		return V2Facts{}, err
	}
	healthOrders, err := loadHealthOrders(ctx, scope)
	if err != nil {
		return V2Facts{}, err
	}
	return V2Facts{
		DueReminders:          dueItems,
		TodaySlots:            todaySlots,
		NextShootPool:         nextShootPool,
		MonthSlots:            monthSlots,
		Unpaid:                unpaid,
		Queue:                 queue,
		ConfirmedCurrent:      confirmedCurrent,
		ConfirmedCurrentCount: confirmedCurrentCount,
		ConfirmedPrevious:     confirmedPrevious,
		PipelineTotal:         pipelineTotal,
		PipelineCount:         pipelineCount,
		Cash30d:               cash,
		RepeatWindowShots:     repeatShots,
		MatrixRows:            matrixRows,
		HealthCustomers:       healthCustomers,
		HealthOrders:          healthOrders,
	}, nil
}

// assembleDueReminderItems 批量装配待办客户摘要（display_name + channel），无 N+1。
func assembleDueReminderItems(ctx context.Context, scope store.AccountScope, reminders []reminderdomain.Reminder) ([]DueReminderItem, error) {
	items := make([]DueReminderItem, 0, len(reminders))
	customerIDs := make([]string, 0, len(reminders))
	for _, r := range reminders {
		if r.CustomerID != nil {
			customerIDs = append(customerIDs, *r.CustomerID)
		}
	}
	summaries := make(map[string]CustomerSummary)
	if len(customerIDs) > 0 {
		rows, err := scope.Query(ctx, "customers", "id, display_name, channel",
			"id = ANY($2::text[])", uniqueStrings(customerIDs))
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var id, name, channel string
			if err := rows.Scan(&id, &name, &channel); err != nil {
				return nil, err
			}
			summaries[id] = CustomerSummary{DisplayName: name, Channel: channel}
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	for _, r := range reminders {
		item := DueReminderItem{Reminder: r}
		if r.CustomerID != nil {
			if summary, ok := summaries[*r.CustomerID]; ok {
				item.CustomerSummary = &summary
			}
		}
		items = append(items, item)
	}
	return items, nil
}

// loadDeliveryQueue：status ∈ {shot, selected, retouching}；排序 delivery_due_at ASC NULLS LAST, id ASC。
func loadDeliveryQueue(ctx context.Context, scope store.AccountScope) ([]orderdomain.ListItem, error) {
	rows, err := scope.Query(ctx, "orders", orderColumns,
		"status = ANY($2::text[])", deliveryQueueStatuses)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	orders := make([]orderdomain.Order, 0)
	for rows.Next() {
		order, err := scanOrder(rows)
		if err != nil {
			return nil, err
		}
		orders = append(orders, order)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(orders, func(i, j int) bool {
		a, b := orders[i].DeliveryDueAt, orders[j].DeliveryDueAt
		switch {
		case a == nil && b == nil:
			return orders[i].ID < orders[j].ID
		case a == nil:
			return false // NULL 排在后
		case b == nil:
			return true
		case !a.Equal(*b):
			return a.Before(*b)
		default:
			return orders[i].ID < orders[j].ID
		}
	})
	return assembleOrderListItems(ctx, scope, orders)
}

// assembleOrderListItems 批量装配订单列表摘要（客户名/套系名），无 N+1。
func assembleOrderListItems(ctx context.Context, scope store.AccountScope, orders []orderdomain.Order) ([]orderdomain.ListItem, error) {
	customerIDs := make([]string, 0, len(orders))
	packageIDs := make([]string, 0, len(orders))
	for _, order := range orders {
		customerIDs = append(customerIDs, order.CustomerID)
		if order.PackageID != nil {
			packageIDs = append(packageIDs, *order.PackageID)
		}
	}
	customerNames, err := fetchNameMap(ctx, scope, "customers", "display_name", uniqueStrings(customerIDs))
	if err != nil {
		return nil, err
	}
	packageNames, err := fetchNameMap(ctx, scope, "packages", "name", uniqueStrings(packageIDs))
	if err != nil {
		return nil, err
	}
	items := make([]orderdomain.ListItem, 0, len(orders))
	for _, order := range orders {
		item := orderdomain.ListItem{
			Order:               order,
			CustomerDisplayName: customerNames[order.CustomerID],
		}
		if order.PackageID != nil {
			if name, ok := packageNames[*order.PackageID]; ok {
				item.PackageName = &name
			}
		}
		items = append(items, item)
	}
	return items, nil
}

// loadConfirmedWindowSums：已确认收入（既有 revenue_confirmed 口径）当前窗与上一窗，
// 外加当前窗 price 非空订单数（AOV 分母；count(price) 天然跳过 NULL）。
func loadConfirmedWindowSums(ctx context.Context, scope store.AccountScope, window V2Window) (current, currentCount, previous int, err error) {
	confirmedCond := "delivered_at >= $2 AND delivered_at < $3 AND balance_paid = true AND status <> $4"
	var cur, prev int64
	if err = scope.ScalarAggregate(ctx, "orders", store.AggregateSum, "price", confirmedCond,
		window.RecentStart, window.RecentEnd, orderdomain.StatusCancelled).Scan(&cur); err != nil {
		return 0, 0, 0, err
	}
	if err = scope.ScalarAggregate(ctx, "orders", store.AggregateSum, "price", confirmedCond,
		window.PreviousStart, window.RecentStart, orderdomain.StatusCancelled).Scan(&prev); err != nil {
		return 0, 0, 0, err
	}
	var count int64
	if err = scope.ScalarAggregate(ctx, "orders", store.AggregateCount, "price", confirmedCond,
		window.RecentStart, window.RecentEnd, orderdomain.StatusCancelled).Scan(&count); err != nil {
		return 0, 0, 0, err
	}
	return int(cur), int(count), int(prev), nil
}

// loadPipelineSums：在途 = status ∈ {scheduled,shot,selected,retouching} 且 price 非空的订单合计。
func loadPipelineSums(ctx context.Context, scope store.AccountScope) (total, count int, err error) {
	cond := "status = ANY($2::text[]) AND price IS NOT NULL"
	var sum, cnt int64
	if err = scope.ScalarAggregate(ctx, "orders", store.AggregateSum, "price", cond, pipelineStatuses).Scan(&sum); err != nil {
		return 0, 0, err
	}
	if err = scope.ScalarAggregate(ctx, "orders", store.AggregateCount, "price", cond, pipelineStatuses).Scan(&cnt); err != nil {
		return 0, 0, err
	}
	return int(sum), int(cnt), nil
}

// loadCashReceived：已收现金 = 近 30 天窗内 paid_at 落窗且非 cancelled 的 amount_paid 之和。
func loadCashReceived(ctx context.Context, scope store.AccountScope, window V2Window) (int, error) {
	var sum int64
	err := scope.ScalarAggregate(ctx, "orders", store.AggregateSum, "amount_paid",
		"paid_at >= $2 AND paid_at < $3 AND status <> $4",
		window.RecentStart, window.RecentEnd, orderdomain.StatusCancelled).Scan(&sum)
	if err != nil {
		return 0, err
	}
	return int(sum), nil
}

// loadRepeatWindowShots：90 天复购窗内 shot_at 落窗且非 cancelled 的 customer_id 行集。
func loadRepeatWindowShots(ctx context.Context, scope store.AccountScope, window V2Window) ([]string, error) {
	rows, err := scope.Query(ctx, "orders", "customer_id",
		"shot_at >= $2 AND shot_at < $3 AND status <> $4",
		window.RepeatStart, window.RecentEnd, orderdomain.StatusCancelled)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	customerIDs := make([]string, 0)
	for rows.Next() {
		var customerID string
		if err := rows.Scan(&customerID); err != nil {
			return nil, err
		}
		customerIDs = append(customerIDs, customerID)
	}
	return customerIDs, rows.Err()
}

// loadMatrixRows：矩阵原始行 = balance_paid=true ∧ 非 cancelled ∧ price 非空，按下单时快照归因。
func loadMatrixRows(ctx context.Context, scope store.AccountScope) ([]MatrixSourceRow, error) {
	rows, err := scope.Query(ctx, "orders",
		"channel_snapshot, shoot_type_snapshot, price, customer_id",
		"balance_paid = true AND status <> $2 AND price IS NOT NULL",
		orderdomain.StatusCancelled)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]MatrixSourceRow, 0)
	for rows.Next() {
		var row MatrixSourceRow
		var shootType *string
		if err := rows.Scan(&row.ChannelSnapshot, &shootType, &row.Price, &row.CustomerID); err != nil {
			return nil, err
		}
		row.ShootTypeSnapshot = shootType
		out = append(out, row)
	}
	return out, rows.Err()
}

// loadHealthCustomers：健康度盘点对象 = status=active 客户全员（archived/merged 不入，§4.3）。
func loadHealthCustomers(ctx context.Context, scope store.AccountScope) ([]healthCustomerSource, error) {
	rows, err := scope.Query(ctx, "customers", "id, display_name, channel, created_at", "status = 'active'")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]healthCustomerSource, 0)
	for rows.Next() {
		var row healthCustomerSource
		if err := rows.Scan(&row.ID, &row.DisplayName, &row.Channel, &row.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// loadHealthOrders：健康度订单事实 = 非 cancelled 全量（节奏样本 + 双金额口径共用一查）。
func loadHealthOrders(ctx context.Context, scope store.AccountScope) ([]healthOrderSource, error) {
	rows, err := scope.Query(ctx, "orders",
		"customer_id, shot_at, price, balance_paid, amount_paid",
		"status <> $2",
		orderdomain.StatusCancelled)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]healthOrderSource, 0)
	for rows.Next() {
		var row healthOrderSource
		if err := rows.Scan(&row.CustomerID, &row.ShotAt, &row.Price, &row.BalancePaid, &row.AmountPaid); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
