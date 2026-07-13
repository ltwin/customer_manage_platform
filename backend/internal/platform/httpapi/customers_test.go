package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	customerdomain "github.com/samson/customer-manage-platform/backend/internal/customer"
	"github.com/samson/customer-manage-platform/backend/internal/customer/avatarimage"
	"github.com/samson/customer-manage-platform/backend/internal/customer/avatarstore"
	orderdomain "github.com/samson/customer-manage-platform/backend/internal/order"
	pkgcatalog "github.com/samson/customer-manage-platform/backend/internal/package"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/httpapi"
	"github.com/samson/customer-manage-platform/backend/internal/platform/idempotency"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	scheduledomain "github.com/samson/customer-manage-platform/backend/internal/schedule"
)

func startCustomerPostgres(t *testing.T) (string, *tcpostgres.PostgresContainer) {
	t.Helper()
	ctx := context.Background()
	ctr, err := tcpostgres.Run(ctx, "postgres:17-alpine",
		tcpostgres.WithDatabase("crm_test"),
		tcpostgres.WithUsername("crm_test"),
		tcpostgres.WithPassword("crm_test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(60*time.Second)),
	)
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(ctr); err != nil {
			t.Logf("terminate postgres container: %v", err)
		}
	})
	url, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("container connection string: %v", err)
	}
	return url, ctr
}

func newCustomerAPIRouter(t *testing.T) (http.Handler, *store.Store, *auth.TokenIssuer) {
	t.Helper()
	router, s, tokens, _ := newCustomerAPIRouterWithContainer(t)
	return router, s, tokens
}

func newCustomerAPIRouterWithContainer(t *testing.T) (http.Handler, *store.Store, *auth.TokenIssuer, *tcpostgres.PostgresContainer) {
	t.Helper()
	url, ctr := startCustomerPostgres(t)
	if err := store.MigrateUp(url); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	s, err := store.Open(context.Background(), url)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(s.Close)

	hash, err := auth.HashPassword(testPassword)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if err := s.CreateAccount(context.Background(), testAcctID, hash); err != nil {
		t.Fatalf("create default account: %v", err)
	}
	tokens := auth.NewTokenIssuer(testSecret)
	objects, err := avatarstore.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("new avatar store: %v", err)
	}
	avatarRepo := customerdomain.NewPostgresAvatarRepository()
	router := httpapi.NewRouter(httpapi.RouterDeps{
		Logger:          slog.New(slog.DiscardHandler),
		DB:              s,
		ScopeFactory:    s,
		Auth:            auth.NewService(s, tokens),
		Customer:        customerdomain.NewService(customerdomain.NewPostgresRepository()),
		Orders:          orderdomain.NewService(orderdomain.NewPostgresRepository()),
		Packages:        pkgcatalog.NewService(pkgcatalog.NewPostgresRepository()),
		Idempotency:     idempotency.NewExecutor(),
		Schedule:        scheduledomain.NewService(scheduledomain.NewPostgresRepository(), scheduledomain.ClockFunc(time.Now)),
		Avatar:          customerdomain.NewAvatarApplication(avatarRepo, objects),
		AvatarProcessor: avatarimage.NewProcessor(),
	})
	return router, s, tokens, ctr
}

func authenticatedRequest(t *testing.T, h http.Handler, method, path, token string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func issueToken(t *testing.T, issuer *auth.TokenIssuer, accountID string) string {
	t.Helper()
	token, err := issuer.Issue(accountID)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	return token
}

func TestCustomerAPIRoundtrip(t *testing.T) {
	h, _, issuer := newCustomerAPIRouter(t)
	token := issueToken(t, issuer, testAcctID)

	createBody := []byte(`{
		"display_name":"阿芷",
		"channel":"xiaohongshu",
		"identities":[
			{"platform":"wechat","handle":"azhi-photo"},
			{"platform":"xiaohongshu","handle":"阿芷写真","remark":"主页私信"}
		]
	}`)
	rec := authenticatedRequest(t, h, http.MethodPost, "/api/v1/customers", token, createBody)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create customer: want 201, got %d body=%s", rec.Code, rec.Body.String())
	}
	var created httpapi.Customer
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created customer: %v", err)
	}
	if created.Id == nil || created.AccountId == nil || *created.AccountId != testAcctID {
		t.Fatalf("created customer should include server account_id, got %+v", created)
	}

	rec = authenticatedRequest(t, h, http.MethodGet, "/api/v1/customers?q=azhi&channel=xiaohongshu", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list customers: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var list struct {
		Items []httpapi.CustomerListItem `json:"items"`
		Total int64                      `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if list.Total != 1 || len(list.Items) != 1 || list.Items[0].OrdersCount != 0 || !list.Items[0].LastShotAt.IsNull() {
		t.Fatalf("list aggregate shape mismatch: %+v", list)
	}

	rec = authenticatedRequest(t, h, http.MethodGet, "/api/v1/customers/"+*created.Id, token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get customer: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var detail httpapi.CustomerDetail
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if len(detail.Identities) != 2 || len(detail.Notes) != 0 || !detail.Referrer.IsNull() {
		t.Fatalf("detail relation shape mismatch: %+v", detail)
	}
	if detail.Stats.OrdersCount != 0 || detail.Stats.TotalOrderAmount != 0 || !detail.Stats.LastShotAt.IsNull() {
		t.Fatalf("detail stats should be zero shape, got %+v", detail.Stats)
	}
}

func TestCustomerAPIErrorPathsAndScope(t *testing.T) {
	h, s, issuer := newCustomerAPIRouter(t)
	tokenA := issueToken(t, issuer, testAcctID)
	ctx := context.Background()
	hash, err := auth.HashPassword("second-password")
	if err != nil {
		t.Fatalf("hash second account password: %v", err)
	}
	if err := s.CreateAccount(ctx, "acct-b", hash); err != nil {
		t.Fatalf("create second account: %v", err)
	}
	scopeB := s.ScopeFor(auth.AccountContext{AccountID: "acct-b"})
	otherCustomer, err := customerdomain.NewService(customerdomain.NewPostgresRepository()).Create(ctx, scopeB, customerdomain.CreateInput{
		DisplayName: "B 账号客户",
		Channel:     customerdomain.ChannelOther,
		Identities:  []customerdomain.IdentityInput{{Platform: customerdomain.PlatformWechat, Handle: "b-handle"}},
	})
	if err != nil {
		t.Fatalf("create B customer: %v", err)
	}

	rec := authenticatedRequest(t, h, http.MethodPost, "/api/v1/customers", tokenA, []byte(`{
		"display_name":"坏数据",
		"channel":"other",
		"identities":[]
	}`))
	if rec.Code != http.StatusBadRequest || decodeEnvelope(t, rec).Error.Code != "validation_failed" {
		t.Fatalf("empty identities should be 400 validation_failed, got %d %s", rec.Code, rec.Body.String())
	}

	rec = authenticatedRequest(t, h, http.MethodGet, "/api/v1/customers?page=-1", tokenA, nil)
	if rec.Code != http.StatusBadRequest || decodeEnvelope(t, rec).Error.Code != "validation_failed" {
		t.Fatalf("invalid page should be 400 validation_failed, got %d %s", rec.Code, rec.Body.String())
	}

	// 契约 minimum:1——显式 0 不允许被静默纠偏成默认值（REV-002）。
	for _, path := range []string{"/api/v1/customers?page=0", "/api/v1/customers?page_size=0"} {
		rec = authenticatedRequest(t, h, http.MethodGet, path, tokenA, nil)
		if rec.Code != http.StatusBadRequest || decodeEnvelope(t, rec).Error.Code != "validation_failed" {
			t.Fatalf("%s should be 400 validation_failed, got %d %s", path, rec.Code, rec.Body.String())
		}
	}

	rec = authenticatedRequest(t, h, http.MethodGet, "/api/v1/customers/"+otherCustomer.ID, tokenA, nil)
	if rec.Code != http.StatusNotFound || decodeEnvelope(t, rec).Error.Code != "not_found" {
		t.Fatalf("cross-account detail should be 404, got %d %s", rec.Code, rec.Body.String())
	}

	rec = authenticatedRequest(t, h, http.MethodPost, "/api/v1/customers", tokenA, []byte(`{
		"display_name":"跨账号介绍",
		"channel":"referral",
		"referrer_customer_id":"`+otherCustomer.ID+`",
		"identities":[{"platform":"wechat","handle":"cross-ref"}]
	}`))
	if rec.Code != http.StatusNotFound || decodeEnvelope(t, rec).Error.Code != "not_found" {
		t.Fatalf("cross-account referrer should be 404, got %d %s", rec.Code, rec.Body.String())
	}

	rec = authenticatedRequest(t, h, http.MethodGet, "/api/v1/customers", "", nil)
	if rec.Code != http.StatusUnauthorized || decodeEnvelope(t, rec).Error.Code != "unauthorized" {
		t.Fatalf("missing auth should be 401, got %d %s", rec.Code, rec.Body.String())
	}

	rec = authenticatedRequest(t, h, http.MethodPatch, "/api/v1/customers/"+otherCustomer.ID, tokenA, []byte(`{"display_name":"x"}`))
	if rec.Code != http.StatusNotFound || decodeEnvelope(t, rec).Error.Code != "not_found" {
		t.Fatalf("unimplemented patch should stay 404, got %d %s", rec.Code, rec.Body.String())
	}
}
