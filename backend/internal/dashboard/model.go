// Package dashboard 是登录后经营台的跨域只读读模型：一次聚合五块口径
// （近 3 天待办、今日档期、待收尾款、流失预警、近 30 天统计），
// 口径单点落在本包，前端不再散拼（design D1）。
package dashboard

import (
	orderdomain "github.com/samson/customer-manage-platform/backend/internal/order"
	reminderdomain "github.com/samson/customer-manage-platform/backend/internal/reminder"
	scheduledomain "github.com/samson/customer-manage-platform/backend/internal/schedule"
)

// Dashboard 是经营台聚合值对象（五块）。
type Dashboard struct {
	// DueReminders：pending 且 due_date ≤ 今日+2（近 3 天窗含逾期，含全部 type 含 churn，design D6）。
	DueReminders []reminderdomain.Reminder
	// TodaySlots：与账号本地今日半开日界相交的档期；shoot 含订单/客户摘要（D8）。
	TodaySlots []scheduledomain.ListItem
	// UnpaidOrders：status=delivered 且 balance_paid=false（窄口径，D5）。
	UnpaidOrders UnpaidOrders
	// ChurnAlerts：type=churn 且 pending（无日期窗，可与 DueReminders 重叠）。
	ChurnAlerts []reminderdomain.Reminder
	// RecentStats：近 30 天滚动窗统计。
	RecentStats RecentStats
}

// UnpaidOrders 是待收尾款卡；Count 恒等于 len(Items)（不截断，D5）。
type UnpaidOrders struct {
	Count int
	Items []orderdomain.ListItem
}

// RecentStats 是近 30 天滚动窗三统计。
type RecentStats struct {
	// OrdersCreated：created_at 落窗，含全部状态。
	OrdersCreated int
	// OrdersDelivered：delivered_at 落窗且当前非 cancelled。
	OrdersDelivered int
	// RevenueConfirmed：分；delivered_at 落窗、balance_paid 且非 cancelled 的 price 之和（NULL→0）。
	RevenueConfirmed int
}
