package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	openapi_types "github.com/oapi-codegen/runtime/types"

	dashboarddomain "github.com/samson/customer-manage-platform/backend/internal/dashboard"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/clock"
)

// dashboardV2Response 是 GET /dashboard/v2 的封装形；字段与 openapi getDashboardV2 内联
// schema 对齐，复用契约既有 ScheduleSlotListItem / OrderListItem / Reminder / CustomerChannel，
// 不新增 Dashboard 专用 DTO（沿用 v1 design D2 先例）。
type dashboardV2Response struct {
	NextShoot           *ScheduleSlotListItem       `json:"next_shoot"`
	TodaySlots          []ScheduleSlotListItem      `json:"today_slots"`
	TodayOpenings       dashboardV2TodayOpenings    `json:"today_openings"`
	DeliveryQueue       dashboardV2DeliveryQueue    `json:"delivery_queue"`
	RevenueWaterfall    dashboardV2RevenueWaterfall `json:"revenue_waterfall"`
	ScheduleUtilization dashboardV2Utilization      `json:"schedule_utilization"`
	ChannelMatrix       dashboardV2ChannelMatrix    `json:"channel_matrix"`
	CustomerHealth      dashboardV2CustomerHealth   `json:"customer_health"`
	DueReminders        []dashboardV2DueReminder    `json:"due_reminders"`
}

// dashboardV2CustomerHealth 是客户资产盘点块；行/桶复用契约生成类型，
// tiers 五字段对象为内联 schema 手写形。
type dashboardV2CustomerHealth struct {
	Total      int                    `json:"total"`
	Thresholds HealthTiers            `json:"thresholds"`
	Tiers      dashboardV2HealthTiers `json:"tiers"`
}

type dashboardV2HealthTiers struct {
	Active   CustomerHealthTier `json:"active"`
	Sleeping CustomerHealthTier `json:"sleeping"`
	AtRisk   CustomerHealthTier `json:"at_risk"`
	Lost     CustomerHealthTier `json:"lost"`
	New      CustomerHealthTier `json:"new"`
}

type dashboardV2TodayOpenings struct {
	WorkingWindow *dashboardV2DayWindow `json:"working_window"`
	Openings      []dashboardV2Opening  `json:"openings"`
}

type dashboardV2DayWindow struct {
	Start   string    `json:"start"`
	End     string    `json:"end"`
	StartAt time.Time `json:"start_at"`
	EndAt   time.Time `json:"end_at"`
}

type dashboardV2Opening struct {
	Date    string    `json:"date"`
	Start   string    `json:"start"`
	End     string    `json:"end"`
	StartAt time.Time `json:"start_at"`
	EndAt   time.Time `json:"end_at"`
}

type dashboardV2DeliveryQueue struct {
	Count int                            `json:"count"`
	Items []dashboardV2DeliveryQueueItem `json:"items"`
}

type dashboardV2DeliveryQueueItem struct {
	Order    OrderListItem `json:"order"`
	DaysLeft *int          `json:"days_left,omitempty"`
	Overdue  bool          `json:"overdue"`
}

type dashboardV2RevenueWaterfall struct {
	Confirmed               dashboardV2Confirmed  `json:"confirmed"`
	Receivable              dashboardV2MoneyCount `json:"receivable"`
	Pipeline                dashboardV2MoneyCount `json:"pipeline"`
	CashReceived30d         int                   `json:"cash_received_30d"`
	AverageOrderValue       *int                  `json:"average_order_value,omitempty"`
	RepeatCustomerRatio90d  *float64              `json:"repeat_customer_ratio_90d,omitempty"`
	OldestReceivableAgeDays *int                  `json:"oldest_receivable_age_days,omitempty"`
}

type dashboardV2Confirmed struct {
	Current     int      `json:"current"`
	Previous    int      `json:"previous"`
	ChangeRatio *float64 `json:"change_ratio,omitempty"`
}

type dashboardV2MoneyCount struct {
	Total int `json:"total"`
	Count int `json:"count"`
}

type dashboardV2Utilization struct {
	Month        string   `json:"month"`
	Utilization  *float64 `json:"utilization,omitempty"`
	ShootCount   int      `json:"shoot_count"`
	HoldDays     int      `json:"hold_days"`
	OpenDays     int      `json:"open_days"`
	ConflictDays int      `json:"conflict_days"`
}

type dashboardV2ChannelMatrix struct {
	Rows       []dashboardV2MatrixRow `json:"rows"`
	GrandTotal int                    `json:"grand_total"`
}

type dashboardV2MatrixRow struct {
	Channel       CustomerChannel `json:"channel"`
	Portrait      int             `json:"portrait"`
	Cosplay       int             `json:"cosplay"`
	Other         int             `json:"other"`
	Unattributed  int             `json:"unattributed"`
	Total         int             `json:"total"`
	OrderCount    int             `json:"order_count"`
	CustomerCount int             `json:"customer_count"`
}

type dashboardV2DueReminder struct {
	Reminder
	CustomerSummary *dashboardV2CustomerSummary `json:"customer_summary,omitempty"`
}

type dashboardV2CustomerSummary struct {
	DisplayName string          `json:"display_name"`
	Channel     CustomerChannel `json:"channel"`
}

// GetDashboardV2 处理 GET /dashboard/v2：薄适配，聚合口径全在 dashboard.Service.GetV2
// （ADR-003）。鉴权失败 401；设置/时区/DB 错误一律 500 封套，绝不回退默认继续算。
func (h *handlers) GetDashboardV2(c *gin.Context) {
	ac, ok := auth.AccountContextFrom(c.Request.Context())
	if !ok {
		abortError(c, http.StatusUnauthorized, CodeUnauthorized, "未认证")
		return
	}
	if h.scopeFactory == nil || h.dashboard == nil {
		_ = c.Error(errors.New("dashboard route dependencies missing"))
		return
	}
	scope := h.scopeFactory.ScopeFor(ac)
	data, err := h.dashboard.GetV2(c.Request.Context(), scope, ac.AccountID)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response, err := toDashboardV2Response(data)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, response)
}

func toDashboardV2Response(data dashboarddomain.V2) (dashboardV2Response, error) {
	var nextShoot *ScheduleSlotListItem
	if data.NextShoot != nil {
		converted, err := toAPIScheduleListItem(*data.NextShoot)
		if err != nil {
			return dashboardV2Response{}, err
		}
		nextShoot = &converted
	}
	slots := make([]ScheduleSlotListItem, 0, len(data.TodaySlots))
	for _, item := range data.TodaySlots {
		converted, err := toAPIScheduleListItem(item)
		if err != nil {
			return dashboardV2Response{}, err
		}
		slots = append(slots, converted)
	}

	var workingWindow *dashboardV2DayWindow
	if data.TodayOpenings.WorkingWindow != nil {
		workingWindow = &dashboardV2DayWindow{
			Start:   data.TodayOpenings.WorkingWindow.Start,
			End:     data.TodayOpenings.WorkingWindow.End,
			StartAt: data.TodayOpenings.WorkingWindow.StartAt,
			EndAt:   data.TodayOpenings.WorkingWindow.EndAt,
		}
	}
	openings := make([]dashboardV2Opening, 0, len(data.TodayOpenings.Openings))
	for _, opening := range data.TodayOpenings.Openings {
		openings = append(openings, dashboardV2Opening{
			Date:    clock.FormatDate(opening.Date),
			Start:   opening.Start,
			End:     opening.End,
			StartAt: opening.StartAt,
			EndAt:   opening.EndAt,
		})
	}

	queueItems := make([]dashboardV2DeliveryQueueItem, 0, len(data.DeliveryQueue.Items))
	for _, item := range data.DeliveryQueue.Items {
		queueItems = append(queueItems, dashboardV2DeliveryQueueItem{
			Order:    toAPIOrderListItem(item.Order),
			DaysLeft: item.DaysLeft,
			Overdue:  item.Overdue,
		})
	}

	matrixRows := make([]dashboardV2MatrixRow, 0, len(data.ChannelMatrix.Rows))
	for _, row := range data.ChannelMatrix.Rows {
		matrixRows = append(matrixRows, dashboardV2MatrixRow{
			Channel:       CustomerChannel(row.Channel),
			Portrait:      row.Portrait,
			Cosplay:       row.Cosplay,
			Other:         row.Other,
			Unattributed:  row.Unattributed,
			Total:         row.Total,
			OrderCount:    row.OrderCount,
			CustomerCount: row.CustomerCount,
		})
	}

	due := make([]dashboardV2DueReminder, 0, len(data.DueReminders))
	for _, item := range data.DueReminders {
		entry := dashboardV2DueReminder{Reminder: toAPIReminder(item.Reminder)}
		if item.CustomerSummary != nil {
			entry.CustomerSummary = &dashboardV2CustomerSummary{
				DisplayName: item.CustomerSummary.DisplayName,
				Channel:     CustomerChannel(item.CustomerSummary.Channel),
			}
		}
		due = append(due, entry)
	}

	return dashboardV2Response{
		NextShoot:  nextShoot,
		TodaySlots: slots,
		TodayOpenings: dashboardV2TodayOpenings{
			WorkingWindow: workingWindow,
			Openings:      openings,
		},
		DeliveryQueue: dashboardV2DeliveryQueue{Count: data.DeliveryQueue.Count, Items: queueItems},
		RevenueWaterfall: dashboardV2RevenueWaterfall{
			Confirmed: dashboardV2Confirmed{
				Current:     data.Waterfall.ConfirmedCurrent,
				Previous:    data.Waterfall.ConfirmedPrevious,
				ChangeRatio: data.Waterfall.ConfirmedChangeRatio,
			},
			Receivable: dashboardV2MoneyCount{
				Total: data.Waterfall.ReceivableTotal,
				Count: data.Waterfall.ReceivableCount,
			},
			Pipeline: dashboardV2MoneyCount{
				Total: data.Waterfall.PipelineTotal,
				Count: data.Waterfall.PipelineCount,
			},
			CashReceived30d:         data.Waterfall.CashReceived30d,
			AverageOrderValue:       data.Waterfall.AverageOrderValue,
			RepeatCustomerRatio90d:  data.Waterfall.RepeatCustomerRatio90d,
			OldestReceivableAgeDays: data.Waterfall.OldestReceivableAgeDays,
		},
		ScheduleUtilization: dashboardV2Utilization{
			Month:        data.Utilization.Month,
			Utilization:  data.Utilization.Utilization,
			ShootCount:   data.Utilization.ShootCount,
			HoldDays:     data.Utilization.HoldDays,
			OpenDays:     data.Utilization.OpenDays,
			ConflictDays: data.Utilization.ConflictDays,
		},
		ChannelMatrix:  dashboardV2ChannelMatrix{Rows: matrixRows, GrandTotal: data.ChannelMatrix.GrandTotal},
		CustomerHealth: toAPICustomerHealth(data.CustomerHealth),
		DueReminders:   due,
	}, nil
}

func toAPICustomerHealth(health dashboarddomain.CustomerHealth) dashboardV2CustomerHealth {
	toTier := func(bucket dashboarddomain.CustomerHealthBucket) CustomerHealthTier {
		items := make([]CustomerHealthItem, 0, len(bucket.Items))
		for _, item := range bucket.Items {
			items = append(items, toAPICustomerHealthItem(item))
		}
		return CustomerHealthTier{Count: bucket.Count, Items: items}
	}
	return dashboardV2CustomerHealth{
		Total: health.Total,
		Thresholds: HealthTiers{
			SleepingRatio:       float32(health.Thresholds.SleepingRatio),
			AtRiskRatio:         float32(health.Thresholds.AtRiskRatio),
			LostRatio:           float32(health.Thresholds.LostRatio),
			FallbackCadenceDays: health.Thresholds.FallbackCadenceDays,
		},
		Tiers: dashboardV2HealthTiers{
			Active:   toTier(health.Tiers.Active),
			Sleeping: toTier(health.Tiers.Sleeping),
			AtRisk:   toTier(health.Tiers.AtRisk),
			Lost:     toTier(health.Tiers.Lost),
			New:      toTier(health.Tiers.New),
		},
	}
}

func toAPICustomerHealthItem(item dashboarddomain.CustomerHealthItem) CustomerHealthItem {
	out := CustomerHealthItem{
		Baseline:      CustomerHealthItemBaseline(item.Baseline),
		Channel:       CustomerChannel(item.Channel),
		CreatedAt:     openapi_types.Date{Time: item.CreatedAt},
		CustomerId:    item.CustomerID,
		DisplayName:   item.DisplayName,
		SettledLtv:    item.SettledLTV,
		Shots:         item.Shots,
		UnsettledPaid: item.UnsettledPaid,
	}
	if item.SinceDays != nil {
		out.SinceDays.Set(*item.SinceDays)
	} else {
		out.SinceDays.SetNull()
	}
	if item.CadenceDays != nil {
		out.CadenceDays.Set(*item.CadenceDays)
	} else {
		out.CadenceDays.SetNull()
	}
	if item.Ratio != nil {
		out.Ratio.Set(float32(*item.Ratio))
	} else {
		out.Ratio.SetNull()
	}
	return out
}
