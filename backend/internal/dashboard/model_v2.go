// model_v2.go 是 GET /dashboard/v2 的聚合值对象（dashboard-v2-redesign ITEM-4；
// 语义权威 roadmap §4.3 2026-08-24 定稿块）。
package dashboard

import (
	orderdomain "github.com/samson/customer-manage-platform/backend/internal/order"
	reminderdomain "github.com/samson/customer-manage-platform/backend/internal/reminder"
	"github.com/samson/customer-manage-platform/backend/internal/schedule"
)

// V2 是经营台 v2 分层读模型：今日驾驶舱 / 未来空档 / 收入瀑布 / 渠道矩阵 / 档期利用率。
type V2 struct {
	// NextShoot：今日及之后最早的未取消 shoot（end_at 越过本地今日 00:00 即候选，
	// 含进行中的跨日拍摄；无则 nil）。
	NextShoot *schedule.ListItem
	// TodaySlots：与账号本地今日半开日界相交的档期（口径与 v1 五块一致）。
	TodaySlots []schedule.ListItem
	// TodayOpenings：今日工作窗与 ≥ 最小可约时长的空档（schedule 包权威算法，DEC-9）。
	TodayOpenings TodayOpenings
	// DeliveryQueue：已拍未交付（shot/selected/retouching）按应交日升序。
	DeliveryQueue DeliveryQueue
	// Waterfall：收入瀑布三段 + 环比 + 客单价 + 复购 + 待收账龄（口径见 roadmap §4.3）。
	Waterfall RevenueWaterfall
	// Utilization：账号本地当前自然月利用率概览（与 Calendar v2 同口径，DEC-2）。
	Utilization UtilizationOverview
	// ChannelMatrix：累计渠道×类型收入（下单时归因快照，含未归因桶）。
	ChannelMatrix ChannelMatrix
	// DueReminders：近 3 天待办 + 客户摘要投影（还原 v2 行样式，批量装配）。
	DueReminders []DueReminderItem
}

// TodayOpenings 是今日空档块；当日未配置工作窗 → WorkingWindow=nil、Openings 为空。
type TodayOpenings struct {
	WorkingWindow *schedule.DayWindow
	Openings      []schedule.Opening
}

// DeliveryQueueItem 是交付队列行；应交日缺省（理论不可达，防御）→ DaysLeft=nil。
type DeliveryQueueItem struct {
	Order    orderdomain.ListItem
	DaysLeft *int
	Overdue  bool
}

// DeliveryQueue 是交付队列；Count == len(Items)（不截断）。
type DeliveryQueue struct {
	Count int
	Items []DeliveryQueueItem
}

// RevenueWaterfall 是收入构成瀑布。
// 已确认=近 30 天窗既有 revenue_confirmed 口径；待收=当前 delivered ∧ 未结清订单的
// outstanding 之和（存量无窗口）；在途=scheduled..retouching 非 cancelled 的 price 之和
// （存量无窗口）；已收现金=近 30 天窗 paid_at 落窗的 amount_paid 之和。
type RevenueWaterfall struct {
	ConfirmedCurrent  int
	ConfirmedPrevious int
	// ConfirmedChangeRatio：(current−previous)/previous，4 位小数；previous=0 → nil。
	ConfirmedChangeRatio *float64
	ReceivableTotal      int
	ReceivableCount      int
	PipelineTotal        int
	PipelineCount        int
	CashReceived30d      int
	// AverageOrderValue：当前窗已确认收入 ÷ 计入订单笔数；无 → nil。
	AverageOrderValue *int
	// RepeatCustomerRatio90d：近 90 天窗内拍摄 ≥2 单客户占比，4 位小数；0 客户 → nil。
	RepeatCustomerRatio90d *float64
	// OldestReceivableAgeDays：今日 − 最早未结清已交付订单 delivered_at（本地自然日差）。
	OldestReceivableAgeDays *int
}

// UtilizationOverview 是月度利用率概览（字段与 Calendar v2 MonthOverview 对齐以支撑 parity）。
type UtilizationOverview struct {
	Month string // "2006-01"
	// Utilization：未取消 shoot+hold 工作窗相交分钟占比，两位小数、封顶 100；分母 0 → nil。
	Utilization  *float64
	ShootCount   int
	HoldDays     int
	OpenDays     int
	ConflictDays int
}

// ChannelMatrixRow 是渠道×类型收入矩阵行；类型列外增设 unattributed（无套系快照）未归因桶。
type ChannelMatrixRow struct {
	Channel       string
	Portrait      int
	Cosplay       int
	Other         int
	Unattributed  int
	Total         int
	OrderCount    int
	CustomerCount int
}

// ChannelMatrix 是矩阵聚合；行按 Total DESC、Channel ASC，未出现渠道不出空行。
type ChannelMatrix struct {
	Rows       []ChannelMatrixRow
	GrandTotal int
}

// CustomerSummary 是待办行的客户投影（display_name + 渠道）。
type CustomerSummary struct {
	DisplayName string
	Channel     string
}

// DueReminderItem 是 v2 待办行：Reminder + 客户摘要（无客户 → nil）。
type DueReminderItem struct {
	reminderdomain.Reminder
	CustomerSummary *CustomerSummary
}
