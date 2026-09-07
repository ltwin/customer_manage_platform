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

	"github.com/samson/customer-manage-platform/backend/internal/creativeworkspace"
	customerdomain "github.com/samson/customer-manage-platform/backend/internal/customer"
	"github.com/samson/customer-manage-platform/backend/internal/customer/avatarimage"
	"github.com/samson/customer-manage-platform/backend/internal/customer/avatarstore"
	dashboarddomain "github.com/samson/customer-manage-platform/backend/internal/dashboard"
	"github.com/samson/customer-manage-platform/backend/internal/dataexport"
	orderdomain "github.com/samson/customer-manage-platform/backend/internal/order"
	pkgcatalog "github.com/samson/customer-manage-platform/backend/internal/package"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/httpapi"
	"github.com/samson/customer-manage-platform/backend/internal/platform/idempotency"
	"github.com/samson/customer-manage-platform/backend/internal/platform/immutablefs"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store/storetest"
	reminderdomain "github.com/samson/customer-manage-platform/backend/internal/reminder"
	digestdomain "github.com/samson/customer-manage-platform/backend/internal/reminder/digest"
	scheduledomain "github.com/samson/customer-manage-platform/backend/internal/schedule"
	settingsdomain "github.com/samson/customer-manage-platform/backend/internal/settings"
	shootplanningdomain "github.com/samson/customer-manage-platform/backend/internal/shootplanning"
	planningbusiness "github.com/samson/customer-manage-platform/backend/internal/shootplanning/business"
)

func TestMain(m *testing.M) { storetest.Main(m, store.MigrateUp) }

func startCustomerPostgres(t *testing.T) string {
	t.Helper()
	return storetest.NewURL(t)
}

func newCustomerAPIRouter(t *testing.T) (http.Handler, *store.Store, *auth.TokenIssuer) {
	t.Helper()
	router, s, tokens, _ := newCustomerAPIRouterWithURL(t)
	return router, s, tokens
}

// newCustomerAPIRouterWithURL 第四个返回值是本测试库的连接串，供需要对同一个库
// 另开一条连接的测试使用（并发事务场景）。原先返回容器句柄，容器整包共享后
// 容器上的默认库不再是本测试的库，拿容器取连接串会连错。
func newCustomerAPIRouterWithURL(t *testing.T) (http.Handler, *store.Store, *auth.TokenIssuer, string) {
	t.Helper()
	url := startCustomerPostgres(t)
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
	settingsSvc := settingsdomain.NewService(settingsdomain.NewPostgresRepository()).WithScopeFactory(func(accountID string) store.AccountScope {
		return s.ScopeFor(auth.AccountContext{AccountID: accountID})
	})
	reminderSvc := reminderdomain.NewService(
		reminderdomain.NewPostgresRepository(),
		reminderdomain.NewSettingsAdapter(settingsSvc),
		slog.New(slog.DiscardHandler),
	).WithFreshness(reminderdomain.NewAssignmentReminderFreshness())
	digestRepo := digestdomain.NewPostgresBindingRepository()
	bindingSvc := digestdomain.NewBindingService(
		digestRepo,
		digestdomain.NewBindTokenResolver(s, digestRepo),
		digestdomain.NewRecipientGate(),
		"studio_digest_bot",
	)
	idempotencyExecutor := idempotency.NewExecutor()
	shootPlanningApp, err := shootplanningdomain.NewApplication(shootplanningdomain.NewPostgresRepository(), idempotencyExecutor)
	if err != nil {
		t.Fatalf("new shoot planning application: %v", err)
	}
	shootPlanningBusiness, err := planningbusiness.NewApplication(
		planningbusiness.NewRepository(), idempotencyExecutor,
		orderdomain.NewBusinessAdjustmentParticipant(),
		scheduledomain.NewBusinessDurationParticipant(shootPlanningApp.CRM()),
	)
	if err != nil {
		t.Fatalf("new shoot planning business application: %v", err)
	}
	creativeObjects, err := immutablefs.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	creativeRepo := creativeworkspace.NewPostgresRepository()
	creativePilot := creativeworkspace.NewPilot(creativeRepo)
	router := httpapi.NewRouter(httpapi.RouterDeps{
		CreativePilot:             creativePilot,
		CreativeEnrollmentAllowed: func(id string) bool { return id == testAcctID },
		CreativeWorkspace:         creativeworkspace.NewService(creativeRepo, creativeObjects, creativePilot),
		Logger:                    slog.New(slog.DiscardHandler),
		DB:                        s,
		ScopeFactory:              s,
		Auth:                      auth.NewService(s, tokens),
		Customer:                  customerdomain.NewService(customerdomain.NewPostgresRepository()),
		Orders: orderdomain.NewService(orderdomain.NewPostgresRepository()).
			WithDeliveryPolicyProvider(orderdomain.NewSettingsDeliveryPolicyAdapter(settingsSvc)),
		Packages:              pkgcatalog.NewService(pkgcatalog.NewPostgresRepository()),
		Idempotency:           idempotencyExecutor,
		AccountTimezone:       settingsSvc,
		Schedule:              scheduledomain.NewService(scheduledomain.NewPostgresRepository(), scheduledomain.ClockFunc(time.Now)),
		Avatar:                customerdomain.NewAvatarApplication(avatarRepo, objects),
		AvatarProcessor:       avatarimage.NewProcessor(),
		Settings:              settingsSvc,
		Reminders:             reminderSvc,
		Dashboard:             dashboarddomain.NewService(dashboarddomain.NewPostgresRepository(), settingsSvc),
		DataExport:            dataexport.NewService(dataexport.NewPostgresRepository(), dataexport.ClockFunc(time.Now)),
		TelegramBinding:       bindingSvc,
		ShootPlanning:         shootPlanningApp,
		ShootPlanningBusiness: shootPlanningBusiness,
	})
	return router, s, tokens, url
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
