package httpapi_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

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
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	reminderdomain "github.com/samson/customer-manage-platform/backend/internal/reminder"
	digestdomain "github.com/samson/customer-manage-platform/backend/internal/reminder/digest"
	scheduledomain "github.com/samson/customer-manage-platform/backend/internal/schedule"
	settingsdomain "github.com/samson/customer-manage-platform/backend/internal/settings"
)

type verifiedHTTPAccount struct {
	accountID    string
	email        string
	accessToken  string
	refreshToken string
}

type accountAccessE2EFixture struct {
	handler http.Handler
	store   *store.Store
	objects customerdomain.AvatarObjectStore
	mail    *testMailSender
	logs    *bytes.Buffer
	now     time.Time
}

func TestAccountAccessHTTPPostgresE2EAndFullIsolation(t *testing.T) {
	fixture := newAccountAccessE2EFixture(t)
	accountA := registerAndVerifyHTTPAccount(t, fixture, syntheticHTTPEmail("isolation-a"))
	accountB := registerAndVerifyHTTPAccount(t, fixture, syntheticHTTPEmail("isolation-b"))
	if accountA.accountID == "" || accountB.accountID == "" || accountA.accountID == accountB.accountID {
		t.Fatal("verified account identities are not distinct")
	}

	refreshed := authRequest(t, fixture.handler, http.MethodPost, "/api/v1/auth/refresh", "", testOrigin, accountA.refreshToken)
	if refreshed.Code != http.StatusOK {
		t.Fatalf("refresh status=%d", refreshed.Code)
	}
	refreshedCookie := requireRefreshCookie(t, refreshed)
	var refreshedAccess httpapi.AccessTokenResponse
	if err := json.Unmarshal(refreshed.Body.Bytes(), &refreshedAccess); err != nil || refreshedAccess.AccessToken == "" {
		t.Fatalf("decode refreshed access response: %v", err)
	}
	if refreshedCookie.Value == accountA.refreshToken {
		t.Fatal("refresh token did not rotate")
	}
	accountA.accessToken = refreshedAccess.AccessToken
	accountA.refreshToken = refreshedCookie.Value
	assertCurrentAccount(t, fixture.handler, accountA)

	avatarA := seedAccountIsolationData(t, fixture.store, fixture.objects, accountA.accountID, "a")
	avatarB := seedAccountIsolationData(t, fixture.store, fixture.objects, accountB.accountID, "b")
	assertFullAccountIsolation(t, fixture.handler, fixture.store, accountA, accountB, avatarA, avatarB)

	pending := authRequest(t, fixture.handler, http.MethodPost, "/api/v1/auth/register",
		`{"email":"`+syntheticHTTPEmail("pending")+`","password":"`+testPassword+`"}`, "", "")
	if pending.Code != http.StatusAccepted {
		t.Fatalf("pending registration status=%d", pending.Code)
	}
	legacyHash, err := auth.HashPassword("legacy-password")
	if err != nil {
		t.Fatalf("hash legacy fixture password: %v", err)
	}
	if err := fixture.store.CreateAccount(context.Background(), "legacy-unclaimed-isolation", legacyHash); err != nil {
		t.Fatalf("create legacy fixture: %v", err)
	}
	scopes, err := fixture.store.AccountScopes(context.Background())
	if err != nil {
		t.Fatalf("enumerate active account scopes: %v", err)
	}
	if len(scopes) != 2 || !containsScopedAccount(scopes, accountA.accountID) || !containsScopedAccount(scopes, accountB.accountID) {
		t.Fatalf("AccountScopes active-only count=%d want=2", len(scopes))
	}

	logout := authRequest(t, fixture.handler, http.MethodPost, "/api/v1/auth/logout", "", testOrigin, accountA.refreshToken)
	if logout.Code != http.StatusNoContent {
		t.Fatalf("logout status=%d", logout.Code)
	}
	cleared := requireRefreshCookie(t, logout)
	if cleared.MaxAge >= 0 || !cleared.Expires.Before(fixture.now) {
		t.Fatal("logout did not clear refresh cookie")
	}
	postLogout := authRequest(t, fixture.handler, http.MethodPost, "/api/v1/auth/refresh", "", testOrigin, accountA.refreshToken)
	if postLogout.Code != http.StatusUnauthorized {
		t.Fatalf("post-logout refresh status=%d", postLogout.Code)
	}
	cleared = requireRefreshCookie(t, postLogout)
	if cleared.MaxAge >= 0 || !cleared.Expires.Before(fixture.now) {
		t.Fatal("post-logout refresh did not clear cookie")
	}

	logged := fixture.logs.String()
	for _, forbidden := range []string{
		accountA.email, accountB.email, testPassword,
		accountA.accessToken, accountB.accessToken, accountA.refreshToken, accountB.refreshToken,
		"Authorization", "__Host-crm_refresh",
	} {
		if forbidden != "" && strings.Contains(logged, forbidden) {
			t.Fatal("E2E logs contain a forbidden authentication value")
		}
	}
}

func newAccountAccessE2EFixture(t *testing.T) accountAccessE2EFixture {
	t.Helper()
	databaseURL, _ := startCustomerPostgres(t)
	if err := store.MigrateUp(databaseURL); err != nil {
		t.Fatalf("migrate account access E2E database: %v", err)
	}
	database, err := store.Open(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("open account access E2E store: %v", err)
	}
	t.Cleanup(database.Close)

	now := time.Date(2026, time.July, 31, 10, 11, 12, 0, time.UTC)
	mail := &testMailSender{}
	logs := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(logs, nil))
	tokens := auth.NewTokenIssuer("e2e-synthetic-root-secret").WithClock(func() time.Time { return now })
	authService := auth.NewService(database, tokens,
		auth.WithAuthClock(func() time.Time { return now }),
		auth.WithAuthMailSender(mail),
		auth.WithAttemptLimiter(database),
		auth.WithRegistrationAdmissionMode(auth.RegistrationPublic),
		auth.WithPublicBaseURL(testOrigin),
		auth.WithAuthResponseDelay(func(context.Context, time.Duration) error { return nil }),
	)
	objects, err := avatarstore.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("create E2E avatar object store: %v", err)
	}
	settingsService := settingsdomain.NewService(settingsdomain.NewPostgresRepository()).WithScopeFactory(
		func(accountID string) store.AccountScope {
			return database.ScopeFor(auth.AccountContext{AccountID: accountID})
		},
	)
	reminderService := reminderdomain.NewService(
		reminderdomain.NewPostgresRepository(),
		reminderdomain.NewSettingsAdapter(settingsService),
		slog.New(slog.DiscardHandler),
	)
	digestRepo := digestdomain.NewPostgresBindingRepository()
	bindingService := digestdomain.NewBindingService(
		digestRepo,
		digestdomain.NewBindTokenResolver(database, digestRepo),
		digestdomain.NewRecipientGate(),
		"studio_digest_bot",
	)
	handler := httpapi.NewRouter(httpapi.RouterDeps{
		Logger:                    logger,
		DB:                        database,
		ScopeFactory:              database,
		Auth:                      authService,
		Customer:                  customerdomain.NewService(customerdomain.NewPostgresRepository()),
		Orders:                    orderdomain.NewService(orderdomain.NewPostgresRepository()),
		Packages:                  pkgcatalog.NewService(pkgcatalog.NewPostgresRepository()),
		Idempotency:               idempotency.NewExecutor(),
		AccountTimezone:           settingsService,
		Schedule:                  scheduledomain.NewService(scheduledomain.NewPostgresRepository(), scheduledomain.ClockFunc(func() time.Time { return now })),
		Avatar:                    customerdomain.NewAvatarApplication(customerdomain.NewPostgresAvatarRepository(), objects),
		AvatarProcessor:           avatarimage.NewProcessor(),
		Settings:                  settingsService,
		Reminders:                 reminderService,
		Dashboard:                 dashboarddomain.NewService(dashboarddomain.NewPostgresRepository(), settingsService),
		DataExport:                dataexport.NewService(dataexport.NewPostgresRepository(), dataexport.ClockFunc(func() time.Time { return now })),
		TelegramBinding:           bindingService,
		PublicBaseURL:             testOrigin,
		PublicRegistrationEnabled: true,
		Now:                       func() time.Time { return now },
	})
	return accountAccessE2EFixture{handler: handler, store: database, objects: objects, mail: mail, logs: logs, now: now}
}

func registerAndVerifyHTTPAccount(t *testing.T, fixture accountAccessE2EFixture, email string) verifiedHTTPAccount {
	t.Helper()
	beforeMail := fixture.mail.Count()
	registered := authRequest(t, fixture.handler, http.MethodPost, "/api/v1/auth/register",
		`{"email":"`+email+`","password":"`+testPassword+`"}`, "", "")
	if registered.Code != http.StatusAccepted || fixture.mail.Count() != beforeMail+1 {
		t.Fatalf("register status=%d mail_delta=%d", registered.Code, fixture.mail.Count()-beforeMail)
	}
	actionURL, err := url.Parse(fixture.mail.Last().ActionURL)
	if err != nil || actionURL.Fragment == "" || actionURL.RawQuery != "" {
		t.Fatalf("verification action URL contract invalid: %v", err)
	}
	fragment, err := url.ParseQuery(actionURL.Fragment)
	if err != nil || fragment.Get("token") == "" {
		t.Fatalf("verification action fragment contract invalid: %v", err)
	}
	verificationBody, err := json.Marshal(map[string]string{"token": fragment.Get("token")})
	if err != nil {
		t.Fatalf("encode verification request: %v", err)
	}
	verified := authRequest(t, fixture.handler, http.MethodPost, "/api/v1/auth/email/verify",
		string(verificationBody), testOrigin, "")
	if verified.Code != http.StatusOK {
		t.Fatalf("verify status=%d", verified.Code)
	}
	var access httpapi.AccessTokenResponse
	if err := json.Unmarshal(verified.Body.Bytes(), &access); err != nil || access.AccessToken == "" {
		t.Fatalf("decode verification response: %v", err)
	}
	refresh := requireRefreshCookie(t, verified)
	account := verifiedHTTPAccount{email: email, accessToken: access.AccessToken, refreshToken: refresh.Value}
	account.accountID = assertCurrentAccount(t, fixture.handler, account)
	return account
}

func assertCurrentAccount(t *testing.T, handler http.Handler, account verifiedHTTPAccount) string {
	t.Helper()
	me := authenticatedRequest(t, handler, http.MethodGet, "/api/v1/me", account.accessToken, nil)
	if me.Code != http.StatusOK {
		t.Fatalf("current account status=%d", me.Code)
	}
	var current httpapi.Account
	if err := json.Unmarshal(me.Body.Bytes(), &current); err != nil || current.Id == nil || current.Email == nil || *current.Email != account.email {
		t.Fatalf("decode current account: %v", err)
	}
	return *current.Id
}

func seedAccountIsolationData(
	t *testing.T,
	database *store.Store,
	objects customerdomain.AvatarObjectStore,
	accountID, marker string,
) []byte {
	t.Helper()
	ctx := context.Background()
	scope := database.ScopeFor(auth.AccountContext{AccountID: accountID})
	customerID := "isolation-customer-" + marker
	orderID := "isolation-order-" + marker
	avatarContent := syntheticAvatarContent(t, marker)
	avatarBody := avatarContent.Bytes()
	avatarVersion := avatarContent.Checksum()
	avatarObjectID := strings.Repeat(marker, 32)
	createdAt := time.Date(2026, time.July, 31, 9, 0, 0, 0, time.UTC)
	avatarKey, err := customerdomain.AvatarObjectKey(accountID, customerID, customerdomain.ObjectRef{
		AvatarVersion: avatarVersion, AvatarObjectID: avatarObjectID,
	})
	if err != nil {
		t.Fatalf("build isolated avatar key: %v", err)
	}
	if _, err := objects.PutImmutable(ctx, avatarKey, avatarContent, customerdomain.ObjectMeta{
		MediaType: "image/png", Size: avatarContent.Size(), Checksum: avatarVersion, ModifiedAt: createdAt,
	}); err != nil {
		t.Fatalf("seed isolated avatar object: %v", err)
	}
	if err := scope.Insert(ctx, "customers",
		[]string{"id", "created_at", "display_name", "channel", "status", "avatar_revision", "avatar_version", "avatar_object_id", "avatar_media_type", "avatar_size", "avatar_updated_at"},
		customerID, createdAt, "isolation-marker-"+marker, "other", "active", 1, avatarVersion, avatarObjectID, "image/png", avatarContent.Size(), createdAt); err != nil {
		t.Fatalf("seed isolated customer: %v", err)
	}
	if err := scope.Insert(ctx, "orders",
		[]string{"id", "created_at", "customer_id", "title"},
		orderID, createdAt, customerID, "isolation-order-marker-"+marker); err != nil {
		t.Fatalf("seed isolated order: %v", err)
	}
	if err := scope.Insert(ctx, "schedule_slots",
		[]string{"id", "created_at", "start_at", "end_at", "type", "order_id", "note"},
		"isolation-slot-"+marker, createdAt, createdAt.Add(time.Hour), createdAt.Add(2*time.Hour), "shoot", orderID, "isolation-slot-marker-"+marker); err != nil {
		t.Fatalf("seed isolated schedule: %v", err)
	}
	if err := scope.Insert(ctx, "reminders",
		[]string{"id", "created_at", "type", "customer_id", "order_id", "due_date", "content", "dedup_key"},
		"isolation-reminder-"+marker, createdAt, "custom", customerID, orderID, "2026-08-01", "isolation-reminder-marker-"+marker, "isolation-dedup-"+marker); err != nil {
		t.Fatalf("seed isolated reminder: %v", err)
	}
	if err := scope.Insert(ctx, "settings", []string{"timezone", "updated_at"}, map[string]string{"a": "Asia/Tokyo", "b": "Europe/Paris"}[marker], createdAt); err != nil {
		t.Fatalf("seed isolated settings: %v", err)
	}
	if err := scope.Insert(ctx, "avatar_reconciliation_checkpoint",
		[]string{"object_inventory_cycle", "pointer_cycle", "updated_at"},
		map[string]int{"a": 11, "b": 22}[marker], map[string]int{"a": 33, "b": 44}[marker], createdAt); err != nil {
		t.Fatalf("seed isolated avatar checkpoint: %v", err)
	}
	return avatarBody
}

func syntheticAvatarContent(t *testing.T, marker string) customerdomain.AvatarContent {
	t.Helper()
	digest := sha256.Sum256([]byte(marker))
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	fill := color.NRGBA{R: digest[0], G: digest[1], B: digest[2], A: 255}
	for y := range 2 {
		for x := range 2 {
			img.SetNRGBA(x, y, fill)
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatalf("encode isolated avatar content: %v", err)
	}
	content, err := customerdomain.NewAvatarContent(encoded.Bytes(), "image/png")
	if err != nil {
		t.Fatalf("create isolated avatar content: %v", err)
	}
	return content
}

func assertFullAccountIsolation(
	t *testing.T,
	handler http.Handler,
	database *store.Store,
	accountA verifiedHTTPAccount,
	accountB verifiedHTTPAccount,
	avatarA []byte,
	avatarB []byte,
) {
	t.Helper()
	assertAccountDomainReads(t, handler, accountA, "a", "b", "Asia/Tokyo", "Europe/Paris", accountB.accountID)
	assertAccountDomainReads(t, handler, accountB, "b", "a", "Europe/Paris", "Asia/Tokyo", accountA.accountID)

	assertOwnAvatarContent(t, handler, accountA, "a", avatarA)
	assertOwnAvatarContent(t, handler, accountB, "b", avatarB)

	crossAccountRequests := []struct {
		method string
		path   string
		body   []byte
	}{
		{method: http.MethodGet, path: "/api/v1/customers/isolation-customer-b"},
		{method: http.MethodGet, path: "/api/v1/customers/isolation-customer-b/avatar/content?v=sha256-" + strings.Repeat("b", 64)},
		{method: http.MethodPatch, path: "/api/v1/orders/isolation-order-b", body: []byte(`{"note":"blocked"}`)},
		{method: http.MethodPatch, path: "/api/v1/schedule/slots/isolation-slot-b", body: []byte(`{"note":"blocked"}`)},
		{method: http.MethodPost, path: "/api/v1/reminders/isolation-reminder-b/done"},
	}
	for _, request := range crossAccountRequests {
		response := authenticatedRequest(t, handler, request.method, request.path, accountA.accessToken, request.body)
		if response.Code != http.StatusNotFound || decodeEnvelope(t, response).Error.Code != "not_found" {
			t.Fatalf("cross-account request %s %s status=%d", request.method, request.path, response.Code)
		}
	}

	scopeA := database.ScopeFor(auth.AccountContext{AccountID: accountA.accountID})
	var visibleBCheckpoint int
	err := scopeA.QueryRow(context.Background(), "avatar_reconciliation_checkpoint", "object_inventory_cycle", "object_inventory_cycle = $2", 22).Scan(&visibleBCheckpoint)
	if !errors.Is(err, store.ErrNoRows) {
		t.Fatal("account A scope exposed account B avatar checkpoint")
	}
}

func assertAccountDomainReads(
	t *testing.T,
	handler http.Handler,
	account verifiedHTTPAccount,
	ownMarker, foreignMarker, ownTimezone, foreignTimezone, foreignAccountID string,
) {
	t.Helper()
	from := time.Date(2026, time.July, 31, 8, 0, 0, 0, time.UTC).Format(time.RFC3339)
	to := time.Date(2026, time.July, 31, 12, 0, 0, 0, time.UTC).Format(time.RFC3339)
	reads := []struct {
		name      string
		path      string
		required  string
		forbidden string
	}{
		{name: "customers", path: "/api/v1/customers?page=1&page_size=50", required: "isolation-marker-" + ownMarker, forbidden: "isolation-marker-" + foreignMarker},
		{name: "orders", path: "/api/v1/orders?page=1&page_size=50", required: "isolation-order-marker-" + ownMarker, forbidden: "isolation-order-marker-" + foreignMarker},
		{name: "schedule", path: "/api/v1/schedule/slots?from=" + url.QueryEscape(from) + "&to=" + url.QueryEscape(to), required: "isolation-slot-marker-" + ownMarker, forbidden: "isolation-slot-marker-" + foreignMarker},
		{name: "reminders", path: "/api/v1/reminders?page=1&page_size=50", required: "isolation-reminder-marker-" + ownMarker, forbidden: "isolation-reminder-marker-" + foreignMarker},
		{name: "settings", path: "/api/v1/settings", required: ownTimezone, forbidden: foreignTimezone},
		{name: "export", path: "/api/v1/export", required: "isolation-marker-" + ownMarker, forbidden: "isolation-marker-" + foreignMarker},
	}
	for _, read := range reads {
		response := authenticatedRequest(t, handler, http.MethodGet, read.path, account.accessToken, nil)
		if response.Code != http.StatusOK {
			t.Fatalf("%s read status=%d body=%s", read.name, response.Code, response.Body.String())
		}
		body := response.Body.String()
		if !strings.Contains(body, read.required) {
			t.Fatalf("%s read omitted own account marker", read.name)
		}
		if strings.Contains(body, read.forbidden) || strings.Contains(body, foreignAccountID) {
			t.Fatalf("%s read exposed foreign account data", read.name)
		}
	}
}

func assertOwnAvatarContent(
	t *testing.T,
	handler http.Handler,
	account verifiedHTTPAccount,
	marker string,
	want []byte,
) {
	t.Helper()
	version := ""
	customer := authenticatedRequest(t, handler, http.MethodGet, "/api/v1/customers/isolation-customer-"+marker, account.accessToken, nil)
	if customer.Code != http.StatusOK {
		t.Fatalf("own avatar customer status=%d", customer.Code)
	}
	var detail httpapi.CustomerDetail
	if err := json.Unmarshal(customer.Body.Bytes(), &detail); err != nil || detail.AvatarVersion == nil {
		t.Fatalf("decode own avatar customer: %v", err)
	}
	version = *detail.AvatarVersion
	content := authenticatedRequest(t, handler, http.MethodGet,
		"/api/v1/customers/isolation-customer-"+marker+"/avatar/content?v="+url.QueryEscape(version),
		account.accessToken, nil)
	if content.Code != http.StatusOK || !bytes.Equal(content.Body.Bytes(), want) {
		t.Fatalf("own avatar content status=%d body=%q", content.Code, content.Body.Bytes())
	}
}

func containsScopedAccount(scopes []store.ScopedAccount, accountID string) bool {
	for _, scoped := range scopes {
		if scoped.AccountID == accountID {
			return true
		}
	}
	return false
}

func syntheticHTTPEmail(local string) string {
	return local + "@example.invalid"
}
