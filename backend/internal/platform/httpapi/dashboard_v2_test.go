package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
)

// TestGetDashboardV2Route 锁住 GET /api/v1/dashboard/v2 的路由暴露与关键投影：
// 深口径已由 dashboard 域测试覆盖，这里验证鉴权边界与真实 settings 默认可用性路径。
func TestGetDashboardV2Route(t *testing.T) {
	router, s, issuer := newCustomerAPIRouter(t)
	token := issueToken(t, issuer, testAcctID)
	scope := s.ScopeFor(auth.AccountContext{AccountID: testAcctID})
	ctx := context.Background()

	now := time.Now().UTC()
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load shanghai: %v", err)
	}
	local := now.In(shanghai)
	// date-only 锚 UTC 午夜，保证 DaysBetween 按自然日差不偏移。
	todayDate := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)

	if err := scope.Insert(ctx, "customers",
		[]string{"id", "display_name", "channel", "status"},
		"cus-v2", "v2路由客户", "douyin", "active"); err != nil {
		t.Fatalf("seed customer: %v", err)
	}

	// 已结清当前窗：confirmed / cash / matrix（douyin portrait）。
	if err := scope.Insert(ctx, "orders",
		[]string{"id", "customer_id", "title", "status", "balance_paid", "deposit_paid", "created_at",
			"price", "delivered_at", "amount_paid", "paid_at", "channel_snapshot", "shoot_type_snapshot"},
		"ord-v2-paid", "cus-v2", "v2已结清", "delivered", true, true, now,
		120000, now, 120000, now, "douyin", "portrait"); err != nil {
		t.Fatalf("seed paid order: %v", err)
	}
	// 在途（scheduled）+ 明日 shoot 档期 → next_shoot。
	if err := scope.Insert(ctx, "orders",
		[]string{"id", "customer_id", "title", "status", "created_at", "price", "amount_paid", "channel_snapshot"},
		"ord-v2-next", "cus-v2", "v2明日拍摄", "scheduled", now, 50000, 0, "douyin"); err != nil {
		t.Fatalf("seed scheduled order: %v", err)
	}
	if err := scope.Insert(ctx, "schedule_slots",
		[]string{"id", "start_at", "end_at", "type", "order_id"},
		"slot-v2-next", now.Add(24*time.Hour), now.Add(25*time.Hour), "shoot", "ord-v2-next"); err != nil {
		t.Fatalf("seed next shoot slot: %v", err)
	}
	// 逾期交付队列项：应交日 10 天前。
	if err := scope.Insert(ctx, "orders",
		[]string{"id", "customer_id", "title", "status", "created_at", "shot_at",
			"price", "delivery_due_at", "amount_paid", "channel_snapshot"},
		"ord-v2-shot", "cus-v2", "v2已拍", "shot", now, now.Add(-240*time.Hour),
		60000, todayDate.AddDate(0, 0, -10), 0, "douyin"); err != nil {
		t.Fatalf("seed shot order: %v", err)
	}
	if err := scope.Insert(ctx, "reminders",
		[]string{"id", "type", "customer_id", "due_date", "content", "status", "dedup_key"},
		"rem-v2", "custom", "cus-v2", local.Format("2006-01-02"), "v2待办", "pending", "dedup-v2"); err != nil {
		t.Fatalf("seed reminder: %v", err)
	}

	// 未认证 → 401。
	unauth := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/v2", nil)
	unauthRec := httptest.NewRecorder()
	router.ServeHTTP(unauthRec, unauth)
	if unauthRec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want 401", unauthRec.Code)
	}

	rec := authenticatedRequest(t, router, http.MethodGet, "/api/v1/dashboard/v2", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get dashboard v2: %d %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode dashboard v2: %v", err)
	}

	nextShoot, _ := body["next_shoot"].(map[string]any)
	if nextShoot == nil || nextShoot["id"] != "slot-v2-next" {
		t.Fatalf("next_shoot = %v, want slot-v2-next", body["next_shoot"])
	}

	waterfall, _ := body["revenue_waterfall"].(map[string]any)
	confirmed, _ := waterfall["confirmed"].(map[string]any)
	assertJSONNumber(t, 120000, confirmed["current"], "confirmed.current")
	pipeline, _ := waterfall["pipeline"].(map[string]any)
	assertJSONNumber(t, 110000, pipeline["total"], "pipeline.total")
	assertJSONNumber(t, 120000, waterfall["cash_received_30d"], "cash_received_30d")

	matrix, _ := body["channel_matrix"].(map[string]any)
	assertJSONNumber(t, 120000, matrix["grand_total"], "matrix.grand_total")
	rows, _ := matrix["rows"].([]any)
	if len(rows) != 1 {
		t.Fatalf("matrix rows = %d, want 1", len(rows))
	}
	row, _ := rows[0].(map[string]any)
	if row["channel"] != "douyin" {
		t.Fatalf("matrix row channel = %v, want douyin", row["channel"])
	}
	assertJSONNumber(t, 120000, row["portrait"], "matrix.row.portrait")
	assertJSONNumber(t, 120000, row["total"], "matrix.row.total")

	queue, _ := body["delivery_queue"].(map[string]any)
	items, _ := queue["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("delivery queue items = %d, want 1", len(items))
	}
	item, _ := items[0].(map[string]any)
	if item["overdue"] != true {
		t.Fatalf("queue overdue = %v, want true", item["overdue"])
	}
	assertJSONNumber(t, -10, item["days_left"], "queue.days_left")

	utilization, _ := body["schedule_utilization"].(map[string]any)
	if utilization["utilization"] == nil {
		t.Fatal("utilization = nil, want number（默认 availability 全周配置）")
	}
	openings, _ := body["today_openings"].(map[string]any)
	if openings["working_window"] == nil {
		t.Fatal("today working_window = nil, want default window")
	}

	due, _ := body["due_reminders"].([]any)
	if len(due) != 1 {
		t.Fatalf("due_reminders = %d, want 1", len(due))
	}
	reminder, _ := due[0].(map[string]any)
	summary, _ := reminder["customer_summary"].(map[string]any)
	if summary == nil || summary["display_name"] != "v2路由客户" || summary["channel"] != "douyin" {
		t.Fatalf("due reminder summary = %v", reminder["customer_summary"])
	}
}

// assertJSONNumber 比较 JSON 解码后的 number（float64）与期望值。
func assertJSONNumber(t *testing.T, want float64, got any, label string) {
	t.Helper()
	value, ok := got.(float64)
	if !ok || value != want {
		t.Fatalf("%s = %v, want %v", label, got, want)
	}
}
