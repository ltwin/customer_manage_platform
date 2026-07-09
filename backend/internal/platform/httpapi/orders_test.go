package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	customerdomain "github.com/samson/customer-manage-platform/backend/internal/customer"
	orderdomain "github.com/samson/customer-manage-platform/backend/internal/order"
	pkgcatalog "github.com/samson/customer-manage-platform/backend/internal/package"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/httpapi"
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
}
