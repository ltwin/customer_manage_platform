package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	customerdomain "github.com/samson/customer-manage-platform/backend/internal/customer"
	orderdomain "github.com/samson/customer-manage-platform/backend/internal/order"
	pkgcatalog "github.com/samson/customer-manage-platform/backend/internal/package"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/httpapi"
	scheduledomain "github.com/samson/customer-manage-platform/backend/internal/schedule"
)

func TestOrderAPIRoundtripAndErrorPaths(t *testing.T) {
	h, s, issuer := newCustomerAPIRouter(t)
	ctx := context.Background()
	tokenA := issueToken(t, issuer, testAcctID)
	scopeA := s.ScopeFor(auth.AccountContext{AccountID: testAcctID})
	customers := customerdomain.NewService(customerdomain.NewPostgresRepository())
	packages := pkgcatalog.NewService(pkgcatalog.NewPostgresRepository())
	orders := orderdomain.NewService(orderdomain.NewPostgresRepository())

	customerA, err := customers.Create(ctx, scopeA, customerdomain.CreateInput{
		DisplayName: "阿芷",
		Channel:     customerdomain.ChannelOther,
		Identities:  []customerdomain.IdentityInput{{Platform: customerdomain.PlatformWechat, Handle: "azhi-order"}},
	})
	if err != nil {
		t.Fatalf("create customer A: %v", err)
	}
	archivedCustomer, err := customers.Create(ctx, scopeA, customerdomain.CreateInput{
		DisplayName: "已归档客户",
		Channel:     customerdomain.ChannelOther,
		Identities:  []customerdomain.IdentityInput{{Platform: customerdomain.PlatformWechat, Handle: "archived-order"}},
	})
	if err != nil {
		t.Fatalf("create archived customer: %v", err)
	}
	archived := customerdomain.StatusArchived
	if _, err := customers.Update(ctx, scopeA, archivedCustomer.ID, customerdomain.UpdateInput{Status: &archived}); err != nil {
		t.Fatalf("archive customer: %v", err)
	}
	packageA, err := packages.Create(ctx, scopeA, pkgcatalog.CreateInput{
		Name:        "轻写真",
		ShootType:   pkgcatalog.ShootTypePortrait,
		PricingMode: pkgcatalog.PricingModeFixed,
		BasePrice:   68000,
	})
	if err != nil {
		t.Fatalf("create package A: %v", err)
	}

	hash, err := auth.HashPassword("second-password")
	if err != nil {
		t.Fatalf("hash second account password: %v", err)
	}
	if err := s.CreateAccount(ctx, "acct-b", hash); err != nil {
		t.Fatalf("create second account: %v", err)
	}
	scopeB := s.ScopeFor(auth.AccountContext{AccountID: "acct-b"})
	customerB, err := customers.Create(ctx, scopeB, customerdomain.CreateInput{
		DisplayName: "B 账号客户",
		Channel:     customerdomain.ChannelOther,
		Identities:  []customerdomain.IdentityInput{{Platform: customerdomain.PlatformWechat, Handle: "b-order"}},
	})
	if err != nil {
		t.Fatalf("create customer B: %v", err)
	}
	orderB, err := orders.Create(ctx, scopeB, orderdomain.CreateInput{CustomerID: customerB.ID})
	if err != nil {
		t.Fatalf("create order B: %v", err)
	}

	rec := authenticatedRequest(t, h, http.MethodGet, "/api/v1/orders", "", nil)
	if rec.Code != http.StatusUnauthorized || decodeEnvelope(t, rec).Error.Code != "unauthorized" {
		t.Fatalf("missing auth should be 401 unauthorized, got %d %s", rec.Code, rec.Body.String())
	}

	rec = authenticatedRequest(t, h, http.MethodPost, "/api/v1/orders", tokenA, []byte(`{}`))
	if rec.Code != http.StatusBadRequest || decodeEnvelope(t, rec).Error.Code != "validation_failed" {
		t.Fatalf("missing customer_id should be 400 validation_failed, got %d %s", rec.Code, rec.Body.String())
	}

	rec = authenticatedRequest(t, h, http.MethodPost, "/api/v1/orders", tokenA, []byte(`{"customer_id":"`+archivedCustomer.ID+`"}`))
	if rec.Code != http.StatusConflict || decodeEnvelope(t, rec).Error.Code != "customer_archived" {
		t.Fatalf("archived customer should be 409 customer_archived, got %d %s", rec.Code, rec.Body.String())
	}

	rec = authenticatedRequest(t, h, http.MethodPost, "/api/v1/orders", tokenA, []byte(`{
		"customer_id":"`+customerA.ID+`",
		"package_id":"`+packageA.ID+`",
		"title":"婚纱约拍",
		"price":68000
	}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create order: want 201, got %d body=%s", rec.Code, rec.Body.String())
	}
	var created httpapi.Order
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created order: %v", err)
	}
	if created.Id == nil || created.AccountId == nil || *created.AccountId != testAcctID || created.Status != "consulting" || created.CustomerId != customerA.ID {
		t.Fatalf("created order shape mismatch: %+v", created)
	}

	rec = authenticatedRequest(t, h, http.MethodGet, "/api/v1/orders", tokenA, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list orders: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var list struct {
		Items []httpapi.OrderListItem `json:"items"`
		Total int64                   `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode order list: %v", err)
	}
	if list.Total != 1 || len(list.Items) != 1 || list.Items[0].CustomerDisplayName != customerA.DisplayName || list.Items[0].PackageName == nil || *list.Items[0].PackageName != packageA.Name {
		t.Fatalf("list summary mismatch: %+v", list)
	}

	rec = authenticatedRequest(t, h, http.MethodPatch, "/api/v1/orders/"+*created.Id, tokenA, []byte(`{"status":"shot"}`))
	if rec.Code != http.StatusConflict || decodeEnvelope(t, rec).Error.Code != "invalid_status_transition" {
		t.Fatalf("jump to shot should be 409 invalid_status_transition, got %d %s", rec.Code, rec.Body.String())
	}

	rec = authenticatedRequest(t, h, http.MethodPatch, "/api/v1/orders/"+orderB.ID, tokenA, []byte(`{"status":"scheduled"}`))
	if rec.Code != http.StatusNotFound || decodeEnvelope(t, rec).Error.Code != "not_found" {
		t.Fatalf("cross-account patch should be 404 not_found, got %d %s", rec.Code, rec.Body.String())
	}

	rec = authenticatedRequest(t, h, http.MethodDelete, "/api/v1/orders/"+*created.Id, tokenA, nil)
	if rec.Code != http.StatusConflict || decodeEnvelope(t, rec).Error.Code != "order_not_terminal" {
		t.Fatalf("non-terminal delete should be 409 order_not_terminal, got %d %s", rec.Code, rec.Body.String())
	}

	rec = authenticatedRequest(t, h, http.MethodPatch, "/api/v1/orders/"+*created.Id, tokenA, []byte(`{"status":"scheduled"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("schedule order: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var scheduled httpapi.Order
	if err := json.Unmarshal(rec.Body.Bytes(), &scheduled); err != nil || scheduled.Status != "scheduled" {
		t.Fatalf("scheduled response mismatch: err=%v order=%+v", err, scheduled)
	}

	rec = authenticatedRequest(t, h, http.MethodPatch, "/api/v1/orders/"+*created.Id, tokenA, []byte(`{"status":"shot","shot_at":null}`))
	if rec.Code != http.StatusBadRequest || decodeEnvelope(t, rec).Error.Code != "validation_failed" {
		t.Fatalf("scheduled->shot with shot_at null should be 400 validation_failed, got %d %s", rec.Code, rec.Body.String())
	}

	rec = authenticatedRequest(t, h, http.MethodPatch, "/api/v1/orders/"+*created.Id, tokenA, []byte(`{"status":"cancelled","shot_at":"2026-06-01T09:00:00Z"}`))
	if rec.Code != http.StatusBadRequest || decodeEnvelope(t, rec).Error.Code != "validation_failed" {
		t.Fatalf("scheduled->cancelled with shot_at should be 400 validation_failed, got %d %s", rec.Code, rec.Body.String())
	}

	rec = authenticatedRequest(t, h, http.MethodPatch, "/api/v1/orders/"+*created.Id, tokenA, []byte(`{"status":"cancelled","note":"客户取消"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("cancel order: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	rec = authenticatedRequest(t, h, http.MethodDelete, "/api/v1/orders/"+*created.Id, tokenA, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete cancelled order: want 204, got %d body=%s", rec.Code, rec.Body.String())
	}

	shotAt := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC).Format(time.RFC3339)
	deliveredAt := time.Date(2026, 6, 10, 9, 0, 0, 0, time.UTC).Format(time.RFC3339)
	rec = authenticatedRequest(t, h, http.MethodPost, "/api/v1/orders", tokenA, []byte(`{
		"creation_mode":"backfill",
		"customer_id":"`+customerA.ID+`",
		"status":"delivered",
		"shot_at":"`+shotAt+`",
		"delivered_at":"`+deliveredAt+`"
	}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("backfill delivered order: want 201, got %d body=%s", rec.Code, rec.Body.String())
	}
	var delivered httpapi.Order
	if err := json.Unmarshal(rec.Body.Bytes(), &delivered); err != nil {
		t.Fatalf("decode delivered order: %v", err)
	}
	rec = authenticatedRequest(t, h, http.MethodPatch, "/api/v1/orders/"+*delivered.Id, tokenA, []byte(`{"status":"closed","delivered_at":null}`))
	if rec.Code != http.StatusBadRequest || decodeEnvelope(t, rec).Error.Code != "validation_failed" {
		t.Fatalf("delivered->closed with delivered_at null should be 400 validation_failed, got %d %s", rec.Code, rec.Body.String())
	}
	rec = authenticatedRequest(t, h, http.MethodPatch, "/api/v1/orders/"+*delivered.Id, tokenA, []byte(`{"status":"closed"}`))
	if rec.Code != http.StatusConflict || decodeEnvelope(t, rec).Error.Code != "unpaid_balance" {
		t.Fatalf("close unpaid order should be 409 unpaid_balance, got %d %s", rec.Code, rec.Body.String())
	}

	idempotentBody := []byte(`{"creation_mode":"new","customer_id":"` + customerA.ID + `","title":"幂等订单"}`)
	first := authenticatedOrderCreate(t, h, tokenA, "order-idempotency-key", idempotentBody)
	second := authenticatedOrderCreate(t, h, tokenA, "order-idempotency-key", idempotentBody)
	if first.Code != http.StatusCreated || second.Code != http.StatusCreated || first.Body.String() != second.Body.String() {
		t.Fatalf("idempotent create replay mismatch: first=%d %s second=%d %s", first.Code, first.Body.String(), second.Code, second.Body.String())
	}
	changed := authenticatedOrderCreate(t, h, tokenA, "order-idempotency-key", []byte(`{"creation_mode":"new","customer_id":"`+customerA.ID+`","title":"修改后的请求"}`))
	if changed.Code != http.StatusConflict || decodeEnvelope(t, changed).Error.Code != "idempotency_conflict" {
		t.Fatalf("same key different body: want idempotency_conflict, got %d %s", changed.Code, changed.Body.String())
	}

	rec = authenticatedRequest(t, h, http.MethodGet, "/api/v1/orders?id="+*delivered.Id+"&page_size=1", tokenA, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("exact order lookup: want 200, got %d %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || list.Total != 1 || len(list.Items) != 1 || *list.Items[0].Id != *delivered.Id {
		t.Fatalf("exact order lookup mismatch: list=%+v err=%v", list, err)
	}
	rec = authenticatedRequest(t, h, http.MethodGet, "/api/v1/orders?id="+orderB.ID+"&page_size=1", tokenA, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("cross-account exact order lookup: want scoped 200, got %d %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || list.Total != 0 || len(list.Items) != 0 {
		t.Fatalf("cross-account exact order must be hidden: list=%+v err=%v", list, err)
	}

	future := time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339)
	rec = authenticatedRequest(t, h, http.MethodGet, "/api/v1/orders?schedulable_at="+future+"&page=1&page_size=1", tokenA, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("schedulable_at list: want 200, got %d %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || list.Total != 1 || len(list.Items) != 1 || list.Items[0].Status != "consulting" {
		t.Fatalf("schedulable_at should filter before pagination: list=%+v err=%v", list, err)
	}
	rec = authenticatedRequest(t, h, http.MethodGet, "/api/v1/orders?schedulable_at=not-a-time", tokenA, nil)
	if rec.Code != http.StatusBadRequest || decodeEnvelope(t, rec).Error.Code != "validation_failed" {
		t.Fatalf("invalid schedulable_at: want validation_failed, got %d %s", rec.Code, rec.Body.String())
	}

	schedule := scheduledomain.NewService(scheduledomain.NewPostgresRepository(), scheduledomain.ClockFunc(func() time.Time {
		return time.Date(2026, 7, 10, 8, 0, 0, 0, time.UTC)
	}))
	slotStart := time.Date(2026, 6, 20, 8, 0, 0, 0, time.UTC)
	slot, err := schedule.Create(ctx, scopeA, scheduledomain.CreateInput{
		StartAt: slotStart,
		EndAt:   slotStart.Add(time.Hour),
		Type:    scheduledomain.TypeShoot,
		OrderID: delivered.Id,
	})
	if err != nil {
		t.Fatalf("attach historical shoot: %v", err)
	}
	rec = authenticatedRequest(t, h, http.MethodPatch, "/api/v1/orders/"+*delivered.Id, tokenA, []byte(`{"status":"cancelled"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("cancel referenced order: %d %s", rec.Code, rec.Body.String())
	}
	rec = authenticatedRequest(t, h, http.MethodDelete, "/api/v1/orders/"+*delivered.Id, tokenA, nil)
	env := decodeEnvelope(t, rec)
	var details httpapi.ScheduleConflictDetails
	if env.Error.Details != nil {
		details, err = env.Error.Details.AsScheduleConflictDetails()
	}
	if rec.Code != http.StatusConflict || env.Error.Code != "order_in_use" || env.Error.Details == nil || err != nil ||
		details.ScheduleSlotId != slot.Slot.ID || !details.ScheduleStartAt.Equal(slotStart) {
		t.Fatalf("order_in_use details mismatch: status=%d envelope=%+v", rec.Code, env)
	}
}

func authenticatedOrderCreate(
	t *testing.T,
	h http.Handler,
	token string,
	idempotencyKey string,
	body []byte,
) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Idempotency-Key", idempotencyKey)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// TestDeliveryDueDerivedOnBothCreatePaths 锁住 POST /orders 两条分支的应交付日语义一致：
// 带 Idempotency-Key（前端补录/档期组合流程走这条）与不带键必须都派生应交付日。
// 回归背景：幂等分支曾误调不带账号上下文的 PrepareCreate，静默跳过派生，而当时的
// 领域测试直接给未导出的 deliveryPolicy 赋值，绕过 service 接线因而无法发现。
func TestDeliveryDueDerivedOnBothCreatePaths(t *testing.T) {
	h, s, issuer := newCustomerAPIRouter(t)
	ctx := context.Background()
	token := issueToken(t, issuer, testAcctID)
	scope := s.ScopeFor(auth.AccountContext{AccountID: testAcctID})
	customers := customerdomain.NewService(customerdomain.NewPostgresRepository())

	customer, err := customers.Create(ctx, scope, customerdomain.CreateInput{
		DisplayName: "交付队列客户",
		Channel:     customerdomain.ChannelOther,
		Identities: []customerdomain.IdentityInput{
			{Platform: customerdomain.PlatformWechat, Handle: "delivery-due-paths"},
		},
	})
	if err != nil {
		t.Fatalf("create customer: %v", err)
	}

	// 默认账号时区 Asia/Shanghai + 默认 SLA 14 天：本地 2026-07-09 拍摄 → 2026-07-23 应交付。
	body := []byte(`{
		"creation_mode": "backfill",
		"customer_id": "` + customer.ID + `",
		"status": "shot",
		"shot_at": "2026-07-09T02:00:00Z"
	}`)

	withKey := authenticatedOrderCreate(t, h, token, "delivery-due-idempotent-1", body)
	if withKey.Code != http.StatusCreated {
		t.Fatalf("idempotent create: %d %s", withKey.Code, withKey.Body.String())
	}
	withoutKey := authenticatedRequest(t, h, http.MethodPost, "/api/v1/orders", token, body)
	if withoutKey.Code != http.StatusCreated {
		t.Fatalf("plain create: %d %s", withoutKey.Code, withoutKey.Body.String())
	}

	for _, created := range []struct {
		label string
		rec   *httptest.ResponseRecorder
	}{
		{label: "带 Idempotency-Key", rec: withKey},
		{label: "不带 Idempotency-Key", rec: withoutKey},
	} {
		var order httpapi.Order
		if err := json.Unmarshal(created.rec.Body.Bytes(), &order); err != nil {
			t.Fatalf("%s: decode order: %v", created.label, err)
		}
		if order.DeliveryDueAt == nil {
			t.Fatalf("%s: delivery_due_at = nil，两条建单路径都必须派生应交付日", created.label)
		}
		if got, want := order.DeliveryDueAt.Format("2006-01-02"), "2026-07-23"; got != want {
			t.Fatalf("%s: delivery_due_at = %q, want %q", created.label, got, want)
		}
		if order.DeliveryDueIsOverride == nil || *order.DeliveryDueIsOverride {
			t.Fatalf("%s: 自动派生不应标记为订单级覆盖", created.label)
		}
	}
}

// TestDeliveryDueOverrideCanBeClearedViaPatch 端到端验证三态：
// PATCH 显式传 null 撤销订单级覆盖并回到自动派生；未传该字段则保持现状。
func TestDeliveryDueOverrideCanBeClearedViaPatch(t *testing.T) {
	h, s, issuer := newCustomerAPIRouter(t)
	ctx := context.Background()
	token := issueToken(t, issuer, testAcctID)
	scope := s.ScopeFor(auth.AccountContext{AccountID: testAcctID})
	customers := customerdomain.NewService(customerdomain.NewPostgresRepository())

	customer, err := customers.Create(ctx, scope, customerdomain.CreateInput{
		DisplayName: "撤销覆盖客户",
		Channel:     customerdomain.ChannelOther,
		Identities: []customerdomain.IdentityInput{
			{Platform: customerdomain.PlatformWechat, Handle: "clear-override"},
		},
	})
	if err != nil {
		t.Fatalf("create customer: %v", err)
	}

	// 建单即带订单级覆盖（默认时区上海 + SLA 14，自动派生本应为 2026-07-23）。
	rec := authenticatedRequest(t, h, http.MethodPost, "/api/v1/orders", token, []byte(`{
		"creation_mode": "backfill",
		"customer_id": "`+customer.ID+`",
		"status": "shot",
		"shot_at": "2026-07-09T02:00:00Z",
		"delivery_due_at": "2026-08-01"
	}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create order: %d %s", rec.Code, rec.Body.String())
	}
	var created httpapi.Order
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created order: %v", err)
	}
	if created.DeliveryDueIsOverride == nil || !*created.DeliveryDueIsOverride {
		t.Fatalf("建单显式给值应标记为订单级覆盖")
	}

	// 未传 delivery_due_at：保持覆盖值不变。
	rec = authenticatedRequest(t, h, http.MethodPatch, "/api/v1/orders/"+*created.Id, token,
		[]byte(`{"note":"只改备注"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("patch note: %d %s", rec.Code, rec.Body.String())
	}
	var untouched httpapi.Order
	if err := json.Unmarshal(rec.Body.Bytes(), &untouched); err != nil {
		t.Fatalf("decode untouched order: %v", err)
	}
	if untouched.DeliveryDueAt == nil || untouched.DeliveryDueAt.Format("2006-01-02") != "2026-08-01" {
		t.Fatalf("未传字段不应改动应交付日，got %v", untouched.DeliveryDueAt)
	}
	if untouched.DeliveryDueIsOverride == nil || !*untouched.DeliveryDueIsOverride {
		t.Fatalf("未传字段不应撤销覆盖标记")
	}

	// 显式 null：撤销覆盖并回到自动派生 2026-07-23。
	rec = authenticatedRequest(t, h, http.MethodPatch, "/api/v1/orders/"+*created.Id, token,
		[]byte(`{"delivery_due_at":null}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("patch null: %d %s", rec.Code, rec.Body.String())
	}
	var cleared httpapi.Order
	if err := json.Unmarshal(rec.Body.Bytes(), &cleared); err != nil {
		t.Fatalf("decode cleared order: %v", err)
	}
	if cleared.DeliveryDueIsOverride == nil || *cleared.DeliveryDueIsOverride {
		t.Fatalf("显式 null 应撤销覆盖标记")
	}
	if cleared.DeliveryDueAt == nil || cleared.DeliveryDueAt.Format("2006-01-02") != "2026-07-23" {
		t.Fatalf("撤销后应回到自动派生 2026-07-23，got %v", cleared.DeliveryDueAt)
	}
}
