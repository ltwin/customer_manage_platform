package httpapi

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	dashboarddomain "github.com/samson/customer-manage-platform/backend/internal/dashboard"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
)

// dashboardResponse 是 GET /dashboard 的封装形；字段与 openapi getDashboard 内联 schema 对齐，
// 复用契约既有 Reminder / ScheduleSlotListItem / OrderListItem，不新增 Dashboard 专用 DTO（design D2）。
type dashboardResponse struct {
	DueReminders []Reminder             `json:"due_reminders"`
	TodaySlots   []ScheduleSlotListItem `json:"today_slots"`
	UnpaidOrders dashboardUnpaidOrders  `json:"unpaid_orders"`
	ChurnAlerts  []Reminder             `json:"churn_alerts"`
	RecentStats  dashboardRecentStats   `json:"recent_stats"`
}

type dashboardUnpaidOrders struct {
	Count int             `json:"count"`
	Items []OrderListItem `json:"items"`
}

type dashboardRecentStats struct {
	OrdersCreated    int `json:"orders_created"`
	OrdersDelivered  int `json:"orders_delivered"`
	RevenueConfirmed int `json:"revenue_confirmed"`
}

// GetDashboard 处理 GET /dashboard：薄适配，聚合口径全在 dashboard.Service（ADR-003）。
// 鉴权失败 401；时区/DB 错误一律 500 封套，绝不回退默认时区继续算（design D3 / S12）。
func (h *handlers) GetDashboard(c *gin.Context) {
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
	data, err := h.dashboard.Get(c.Request.Context(), scope, ac.AccountID)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response, err := toDashboardResponse(data)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, response)
}

func toDashboardResponse(data dashboarddomain.Dashboard) (dashboardResponse, error) {
	due := make([]Reminder, 0, len(data.DueReminders))
	for _, r := range data.DueReminders {
		due = append(due, toAPIReminder(r))
	}
	churn := make([]Reminder, 0, len(data.ChurnAlerts))
	for _, r := range data.ChurnAlerts {
		churn = append(churn, toAPIReminder(r))
	}
	slots := make([]ScheduleSlotListItem, 0, len(data.TodaySlots))
	for _, item := range data.TodaySlots {
		converted, err := toAPIScheduleListItem(item)
		if err != nil {
			return dashboardResponse{}, err
		}
		slots = append(slots, converted)
	}
	unpaidItems := make([]OrderListItem, 0, len(data.UnpaidOrders.Items))
	for _, item := range data.UnpaidOrders.Items {
		unpaidItems = append(unpaidItems, toAPIOrderListItem(item))
	}
	return dashboardResponse{
		DueReminders: due,
		TodaySlots:   slots,
		UnpaidOrders: dashboardUnpaidOrders{
			Count: data.UnpaidOrders.Count,
			Items: unpaidItems,
		},
		ChurnAlerts: churn,
		RecentStats: dashboardRecentStats{
			OrdersCreated:    data.RecentStats.OrdersCreated,
			OrdersDelivered:  data.RecentStats.OrdersDelivered,
			RevenueConfirmed: data.RecentStats.RevenueConfirmed,
		},
	}, nil
}
