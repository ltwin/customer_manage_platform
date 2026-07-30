package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// TestDashboardEndpointReturnsFiveKeys 覆盖 step 2 骨架 + S9：
// 鉴权 GET /dashboard 返回 200 且五键齐全（空数组 / 零值），无 token 返回 401。
func TestDashboardEndpointReturnsFiveKeys(t *testing.T) {
	h, _, issuer := newCustomerAPIRouter(t)
	token := issueToken(t, issuer, testAcctID)

	rec := authenticatedRequest(t, h, http.MethodGet, "/api/v1/dashboard", "", nil)
	if rec.Code != http.StatusUnauthorized || decodeEnvelope(t, rec).Error.Code != "unauthorized" {
		t.Fatalf("missing token should be 401, got %d %s", rec.Code, rec.Body.String())
	}

	rec = authenticatedRequest(t, h, http.MethodGet, "/api/v1/dashboard", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("dashboard: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode dashboard: %v", err)
	}
	for _, key := range []string{"due_reminders", "today_slots", "unpaid_orders", "churn_alerts", "recent_stats"} {
		if _, ok := raw[key]; !ok {
			t.Fatalf("dashboard missing key %q; body=%s", key, rec.Body.String())
		}
	}

	var body struct {
		DueReminders []json.RawMessage `json:"due_reminders"`
		TodaySlots   []json.RawMessage `json:"today_slots"`
		ChurnAlerts  []json.RawMessage `json:"churn_alerts"`
		UnpaidOrders struct {
			Count int               `json:"count"`
			Items []json.RawMessage `json:"items"`
		} `json:"unpaid_orders"`
		RecentStats struct {
			OrdersCreated    int `json:"orders_created"`
			OrdersDelivered  int `json:"orders_delivered"`
			RevenueConfirmed int `json:"revenue_confirmed"`
		} `json:"recent_stats"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode typed dashboard: %v", err)
	}
	if body.DueReminders == nil || body.TodaySlots == nil || body.ChurnAlerts == nil || body.UnpaidOrders.Items == nil {
		t.Fatalf("empty blocks must be [] not null; body=%s", rec.Body.String())
	}
	if body.UnpaidOrders.Count != 0 || len(body.UnpaidOrders.Items) != 0 {
		t.Fatalf("empty unpaid should be count 0; got %+v", body.UnpaidOrders)
	}
	if body.RecentStats.OrdersCreated != 0 || body.RecentStats.OrdersDelivered != 0 || body.RecentStats.RevenueConfirmed != 0 {
		t.Fatalf("empty recent_stats should be zero; got %+v", body.RecentStats)
	}
}

// TestDashboardTodaySlotsParityWithScheduleList 覆盖 S13：
// dashboard today_slots 与 GET /schedule/slots 同窗口逐字段、逐顺序一致（design D8）。
func TestDashboardTodaySlotsParityWithScheduleList(t *testing.T) {
	h, s, issuer := newCustomerAPIRouter(t)
	token := issueToken(t, issuer, testAcctID)
	scope := s.ScopeFor(auth.AccountContext{AccountID: testAcctID})
	ctx := context.Background()

	seedParityCustomer(t, scope, "cus-p", "帕里蒂")
	seedParityOrder(t, scope, "ord-p", "cus-p", "婚纱跟拍", "consulting")

	// 账号默认时区 Asia/Shanghai；构造落在今日本地窗内（09:00-11:00）的 shoot 档期。
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load shanghai: %v", err)
	}
	now := time.Now().In(loc)
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	tomorrow := todayStart.AddDate(0, 0, 1)
	slotStart := todayStart.Add(9 * time.Hour)
	slotEnd := todayStart.Add(11 * time.Hour)
	if err := scope.Insert(ctx, "schedule_slots",
		[]string{"id", "start_at", "end_at", "type", "order_id"},
		"slot-p", slotStart.UTC(), slotEnd.UTC(), "shoot", "ord-p"); err != nil {
		t.Fatalf("seed shoot slot: %v", err)
	}

	rec := authenticatedRequest(t, h, http.MethodGet, "/api/v1/dashboard", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("dashboard: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var dash struct {
		TodaySlots []json.RawMessage `json:"today_slots"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &dash); err != nil {
		t.Fatalf("decode dashboard: %v", err)
	}

	scheduleURL := "/api/v1/schedule/slots?from=" + url.QueryEscape(todayStart.Format(time.RFC3339)) + "&to=" + url.QueryEscape(tomorrow.Format(time.RFC3339))
	rec = authenticatedRequest(t, h, http.MethodGet, scheduleURL, token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("schedule list: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var scheduleItems []json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &scheduleItems); err != nil {
		t.Fatalf("decode schedule list: %v", err)
	}

	if len(dash.TodaySlots) != 1 || len(scheduleItems) != 1 {
		t.Fatalf("expected exactly one slot both sides; dashboard=%d schedule=%d", len(dash.TodaySlots), len(scheduleItems))
	}
	if string(dash.TodaySlots[0]) != string(scheduleItems[0]) {
		t.Fatalf("today_slots must match schedule list item byte-for-byte:\n dashboard=%s\n schedule =%s", dash.TodaySlots[0], scheduleItems[0])
	}
}

func seedParityCustomer(t *testing.T, scope store.AccountScope, id, name string) {
	t.Helper()
	if err := scope.Insert(context.Background(), "customers",
		[]string{"id", "display_name", "channel", "status"}, id, name, "other", "active"); err != nil {
		t.Fatalf("seed customer: %v", err)
	}
}

func seedParityOrder(t *testing.T, scope store.AccountScope, id, customerID, title, status string) {
	t.Helper()
	if err := scope.Insert(context.Background(), "orders",
		[]string{"id", "customer_id", "title", "status"}, id, customerID, title, status); err != nil {
		t.Fatalf("seed order: %v", err)
	}
}
