package httpapi_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/httpapi"
)

const (
	testSecret   = "test-secret"
	testPassword = "correct-horse"
	testAcctID   = "acct-1"
	testOrigin   = "https://app.example.invalid"
)

func testEmail() string { return "owner" + "@" + "example.invalid" }

type fakeAccounts struct {
	record  auth.LoginRecord
	current auth.Account
}

type authSpyRepository struct {
	*fakeAccounts
	registerCalls       int
	findCalls           int
	consumeCalls        int
	rotateCalls         int
	revokeCalls         int
	limiterCalls        int
	resetPasswordCalls  int
	credentialCalls     int
	changePasswordCalls int
	registerCreated     bool
	consumeAccount      auth.Account
	rotateErr           error
	limiterRetryAfter   time.Duration
	limiterAllowed      bool
	limiterErr          error
	limiterAction       auth.AuthAction
	limiterSourceDigest string
	resetPasswordErr    error
	credentialErr       error
	changePasswordErr   error
}

func (r *authSpyRepository) RegisterAccount(ctx context.Context, mode auth.RegistrationAdmissionMode, record auth.RegistrationRecord) (bool, error) {
	r.registerCalls++
	return r.registerCreated, nil
}

func (r *authSpyRepository) FindLoginRecord(ctx context.Context, email string) (auth.LoginRecord, bool, error) {
	r.findCalls++
	return r.fakeAccounts.FindLoginRecord(ctx, email)
}

func (r *authSpyRepository) ConsumeActionAndActivate(context.Context, auth.ActionProof, auth.RefreshSeed, time.Time) (auth.Account, error) {
	r.consumeCalls++
	if r.consumeAccount.ID == "" {
		return auth.Account{}, auth.ErrInvalidOrExpiredActionToken
	}
	return r.consumeAccount, nil
}

func (r *authSpyRepository) RotateRefresh(_ context.Context, _ auth.RefreshProof, successor auth.RefreshSeed, _ *auth.ReplayCipher, now time.Time) (auth.RefreshRotation, error) {
	r.rotateCalls++
	if r.rotateErr != nil {
		return auth.RefreshRotation{}, r.rotateErr
	}
	return auth.RefreshRotation{
		AccountID: testAcctID, FamilyID: "family-id", GenerationID: successor.GenerationID,
		WireToken: successor.WireToken, IdleExpiresAt: now.Add(14 * 24 * time.Hour),
		AbsoluteExpiresAt: now.Add(30 * 24 * time.Hour),
	}, nil
}

func (r *authSpyRepository) RevokeRefresh(context.Context, auth.RefreshProof, time.Time) error {
	r.revokeCalls++
	return nil
}

func (r *authSpyRepository) Consume(
	_ context.Context,
	action auth.AuthAction,
	_, sourceDigest string,
	_ time.Time,
) (time.Duration, bool, error) {
	r.limiterCalls++
	r.limiterAction = action
	r.limiterSourceDigest = sourceDigest
	return r.limiterRetryAfter, r.limiterAllowed, r.limiterErr
}

func (r *authSpyRepository) ResetSubject(context.Context, auth.AuthAction, string) error { return nil }

func (r *authSpyRepository) ResetPassword(
	context.Context, auth.ActionProof, string, time.Time,
) (string, error) {
	r.resetPasswordCalls++
	return r.record.Account.ID, r.resetPasswordErr
}

func (r *authSpyRepository) PasswordCredential(context.Context, string) (string, error) {
	r.credentialCalls++
	return r.record.PasswordHash, r.credentialErr
}

func (r *authSpyRepository) ChangePassword(context.Context, auth.PasswordChangeCommand) error {
	r.changePasswordCalls++
	return r.changePasswordErr
}

func (r *authSpyRepository) Calls() int {
	return r.registerCalls + r.findCalls + r.consumeCalls + r.rotateCalls + r.revokeCalls + r.limiterCalls +
		r.resetPasswordCalls + r.credentialCalls + r.changePasswordCalls
}

type testMailSender struct {
	mu    sync.Mutex
	mails []auth.AuthMail
	err   error
}

func (m *testMailSender) Send(_ context.Context, mail auth.AuthMail) (auth.MailReceipt, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mails = append(m.mails, mail)
	return auth.MailReceipt{ProviderMessageID: "synthetic-message", AcceptedAt: mail.ExpiresAt.Add(-time.Minute)}, m.err
}

func (m *testMailSender) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.mails)
}

func (m *testMailSender) Last() auth.AuthMail {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.mails[len(m.mails)-1]
}

type authHTTPFixture struct {
	handler http.Handler
	repo    *authSpyRepository
	mail    *testMailSender
	logs    *bytes.Buffer
	now     time.Time
}

func newAuthHTTPFixture(t *testing.T, registrationEnabled bool) authHTTPFixture {
	t.Helper()
	hash, err := auth.HashPassword(testPassword)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	now := time.Date(2026, 7, 31, 8, 9, 10, 0, time.UTC)
	account := auth.Account{
		ID: testAcctID, Email: testEmail(), Status: auth.AccountActive,
		CreatedAt: time.Date(2026, 7, 6, 0, 0, 0, 0, time.UTC),
	}
	repo := &authSpyRepository{
		fakeAccounts: &fakeAccounts{
			record:  auth.LoginRecord{Account: account, PasswordHash: hash},
			current: account,
		},
		consumeAccount: account,
		limiterAllowed: true,
	}
	mail := &testMailSender{}
	logs := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(logs, nil))
	issuer := auth.NewTokenIssuer(testSecret).WithClock(func() time.Time { return now })
	service := auth.NewService(repo, issuer,
		auth.WithAuthClock(func() time.Time { return now }),
		auth.WithAuthMailSender(mail),
		auth.WithPublicBaseURL(testOrigin),
		auth.WithAttemptLimiter(repo),
		auth.WithAuthResponseDelay(func(context.Context, time.Duration) error { return nil }),
	)
	handler := httpapi.NewRouter(httpapi.RouterDeps{
		Logger: logger, DB: fakePinger{}, Auth: service,
		PublicBaseURL: testOrigin, PublicRegistrationEnabled: registrationEnabled,
		Now: func() time.Time { return now },
	})
	return authHTTPFixture{handler: handler, repo: repo, mail: mail, logs: logs, now: now}
}

func (f *fakeAccounts) AccountCount(context.Context) (int64, error) { return 1, nil }
func (f *fakeAccounts) RegisterAccount(context.Context, auth.RegistrationAdmissionMode, auth.RegistrationRecord) (bool, error) {
	return false, nil
}
func (f *fakeAccounts) ReplaceVerificationToken(context.Context, string, string, []byte, time.Time, time.Time) (bool, error) {
	return false, nil
}
func (f *fakeAccounts) ReplacePasswordResetToken(context.Context, string, string, []byte, time.Time, time.Time) (bool, error) {
	return false, nil
}
func (f *fakeAccounts) ConsumeActionAndActivate(context.Context, auth.ActionProof, auth.RefreshSeed, time.Time) (auth.Account, error) {
	return auth.Account{}, auth.ErrInvalidOrExpiredActionToken
}
func (f *fakeAccounts) ResetPassword(context.Context, auth.ActionProof, string, time.Time) (string, error) {
	return f.record.Account.ID, nil
}
func (f *fakeAccounts) PasswordCredential(context.Context, string) (string, error) {
	return f.record.PasswordHash, nil
}
func (f *fakeAccounts) ChangePassword(context.Context, auth.PasswordChangeCommand) error { return nil }
func (f *fakeAccounts) Consume(context.Context, auth.AuthAction, string, string, time.Time) (time.Duration, bool, error) {
	return 0, true, nil
}
func (f *fakeAccounts) ResetSubject(context.Context, auth.AuthAction, string) error { return nil }
func (f *fakeAccounts) FindLoginRecord(_ context.Context, email string) (auth.LoginRecord, bool, error) {
	if email != f.record.Account.Email {
		return auth.LoginRecord{}, false, nil
	}
	return f.record, true, nil
}
func (f *fakeAccounts) CreateRefreshSession(context.Context, string, auth.RefreshSeed) error {
	return nil
}
func (f *fakeAccounts) RotateRefresh(context.Context, auth.RefreshProof, auth.RefreshSeed, *auth.ReplayCipher, time.Time) (auth.RefreshRotation, error) {
	return auth.RefreshRotation{}, auth.ErrInvalidToken
}
func (f *fakeAccounts) RevokeRefresh(context.Context, auth.RefreshProof, time.Time) error { return nil }
func (f *fakeAccounts) CurrentAccount(_ context.Context, id string) (auth.Account, error) {
	account := f.current
	account.ID = id
	return account, nil
}
func (f *fakeAccounts) PlanLegacyClaim(context.Context, string) (auth.LegacyClaimRecord, error) {
	return auth.LegacyClaimRecord{State: auth.LegacyClaimNoTarget}, nil
}
func (f *fakeAccounts) BeginLegacyClaim(context.Context, auth.LegacyClaimCommand) (auth.LegacyClaimRecord, error) {
	return auth.LegacyClaimRecord{State: auth.LegacyClaimNoTarget}, nil
}
func (f *fakeAccounts) InspectLegacyState(context.Context) (auth.LegacyAuthState, error) {
	return auth.LegacyAuthState{}, nil
}
func (f *fakeAccounts) SweepExpiredReplayCiphertexts(context.Context, time.Time, int) (int64, error) {
	return 0, nil
}

func newAuthRouter(t *testing.T) http.Handler {
	return newAuthRouterWithTimezone(t, nil)
}

func newAuthRouterWithTimezone(t *testing.T, timezone httpapi.AccountTimezoneProvider) http.Handler {
	t.Helper()
	hash, err := auth.HashPassword(testPassword)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	account := auth.Account{
		ID: testAcctID, Email: testEmail(), Status: auth.AccountActive,
		CreatedAt: time.Date(2026, 7, 6, 0, 0, 0, 0, time.UTC),
	}
	repo := &fakeAccounts{record: auth.LoginRecord{Account: account, PasswordHash: hash}, current: account}
	svc := auth.NewService(
		repo,
		auth.NewTokenIssuer(testSecret),
		auth.WithPublicBaseURL(testOrigin),
		auth.WithAttemptLimiter(repo),
		auth.WithAuthResponseDelay(func(context.Context, time.Duration) error { return nil }),
	)
	return httpapi.NewRouter(httpapi.RouterDeps{
		Logger:          slog.New(slog.DiscardHandler),
		DB:              fakePinger{},
		Auth:            svc,
		AccountTimezone: timezone,
		PublicBaseURL:   testOrigin,
	})
}

type fakeTimezoneProvider struct {
	timezone string
}

func (p fakeTimezoneProvider) TimezoneForAccount(context.Context, string) (string, error) {
	return p.timezone, nil
}

func postLogin(t *testing.T, h http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", testOrigin)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func getMe(t *testing.T, h http.Handler, authorization string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// A2 + A5：登录 → token → /me 往返 200；/me 永不含 password_hash。
func TestLoginMeRoundtrip(t *testing.T) {
	h := newAuthRouter(t)

	rec := postLogin(t, h, `{"email":"`+testEmail()+`","password":"`+testPassword+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("login with correct password: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var loginResp struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &loginResp); err != nil || loginResp.AccessToken == "" {
		t.Fatalf("login response should carry token, got %s", rec.Body.String())
	}

	rec = getMe(t, h, "Bearer "+loginResp.AccessToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("/me with valid token: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var me struct {
		ID        string    `json:"id"`
		CreatedAt time.Time `json:"created_at"`
		Timezone  string    `json:"timezone"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil {
		t.Fatalf("decode /me: %v", err)
	}
	if me.ID != testAcctID || me.CreatedAt.IsZero() || me.Timezone != "Asia/Shanghai" {
		t.Fatalf("/me should return account id, created_at, and timezone, got %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "password_hash") {
		t.Fatalf("/me must never contain password_hash: %s", rec.Body.String())
	}
}

func TestMeUsesInjectedAccountTimezoneProvider(t *testing.T) {
	h := newAuthRouterWithTimezone(t, fakeTimezoneProvider{timezone: "America/New_York"})
	rec := postLogin(t, h, `{"email":"`+testEmail()+`","password":"`+testPassword+`"}`)
	var loginResp struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &loginResp); err != nil || loginResp.AccessToken == "" {
		t.Fatalf("login response: %s err=%v", rec.Body.String(), err)
	}
	rec = getMe(t, h, "Bearer "+loginResp.AccessToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("/me with injected timezone: %d %s", rec.Code, rec.Body.String())
	}
	var me struct {
		Timezone string `json:"timezone"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil || me.Timezone != "America/New_York" {
		t.Fatalf("injected timezone mismatch: %s err=%v", rec.Body.String(), err)
	}
}

// A3：错误密码 → 401 unauthorized 封套。
func TestLoginWrongPassword(t *testing.T) {
	rec := postLogin(t, newAuthRouter(t), `{"email":"`+testEmail()+`","password":"wrong-password"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}
	if env := decodeEnvelope(t, rec); env.Error.Code != "unauthorized" {
		t.Fatalf("want unauthorized envelope, got %+v", env)
	}
}

// A4：缺 password / 非法 body → 400 validation_failed 封套。
func TestLoginMissingPassword(t *testing.T) {
	h := newAuthRouter(t)
	for _, body := range []string{`{}`, ``, `not-json`, `{"password":"` + testPassword + `"}`} {
		rec := postLogin(t, h, body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body %q: want 400, got %d", body, rec.Code)
		}
		if env := decodeEnvelope(t, rec); env.Error.Code != "validation_failed" {
			t.Fatalf("body %q: want validation_failed envelope, got %+v", body, env)
		}
	}
}

func TestLegacyPasswordOnlyShapeRejected(t *testing.T) {
	t.Parallel()
	rec := postLogin(t, newAuthRouter(t), `{"password":"`+testPassword+`"}`)
	if rec.Code != http.StatusBadRequest || decodeEnvelope(t, rec).Error.Code != "validation_failed" {
		t.Fatalf("legacy_password_only_shape_rejected: status=%d code=%q",
			rec.Code, decodeEnvelope(t, rec).Error.Code)
	}
	if rec.Header().Get("Set-Cookie") != "" || len(rec.Result().Cookies()) != 0 {
		t.Fatal("legacy_password_only_shape_rejected: response mutated refresh cookie")
	}
}

// A6：无 token / 篡改 token / 过期 token → 均 401 封套。
func TestMeUnauthorizedPaths(t *testing.T) {
	h := newAuthRouter(t)

	// 有效 token 作篡改底料
	rec := postLogin(t, h, `{"email":"`+testEmail()+`","password":"`+testPassword+`"}`)
	var loginResp struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &loginResp); err != nil {
		t.Fatalf("login: %v", err)
	}

	// 过期 token：同 secret 直接签一个 exp 在过去的
	expired, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Subject:   testAcctID,
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
	}).SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("sign expired token: %v", err)
	}

	cases := map[string]string{
		"无 token":  "",
		"非 Bearer": "Basic abc",
		"篡改 token": "Bearer " + loginResp.AccessToken + "x",
		"过期 token": "Bearer " + expired,
		"空 Bearer": "Bearer ",
	}
	for name, header := range cases {
		rec := getMe(t, h, header)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s: want 401, got %d", name, rec.Code)
		}
		if env := decodeEnvelope(t, rec); env.Error.Code != "unauthorized" {
			t.Fatalf("%s: want unauthorized envelope, got %+v", name, env)
		}
	}
}

func TestAuthCapabilitiesAndRegistrationGate(t *testing.T) {
	disabled := newAuthHTTPFixture(t, false)
	rec := authRequest(t, disabled.handler, http.MethodGet, "/api/v1/auth/capabilities", "", "", "")
	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("disabled capabilities response: status=%d cache=%q", rec.Code, rec.Header().Get("Cache-Control"))
	}
	var capabilities httpapi.AuthCapabilities
	if err := json.Unmarshal(rec.Body.Bytes(), &capabilities); err != nil || capabilities.PublicRegistrationEnabled {
		t.Fatalf("disabled capabilities body: enabled=%t err=%v", capabilities.PublicRegistrationEnabled, err)
	}
	for _, body := range []string{"", "not-json", `{"email":"invalid"}`, `{"email":"` + testEmail() + `","password":"` + testPassword + `"}`} {
		rec = authRequest(t, disabled.handler, http.MethodPost, "/api/v1/auth/register", body, "", "")
		if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("disabled register must be fixed 503/no-store: status=%d cache=%q", rec.Code, rec.Header().Get("Cache-Control"))
		}
		if env := decodeEnvelope(t, rec); env.Error.Code != "registration_disabled" {
			t.Fatalf("disabled register code=%q", env.Error.Code)
		}
	}
	if disabled.repo.Calls() != 0 || disabled.mail.Count() != 0 {
		t.Fatalf("disabled register had side effects: repository_calls=%d mail_calls=%d", disabled.repo.Calls(), disabled.mail.Count())
	}

	enabled := newAuthHTTPFixture(t, true)
	rec = authRequest(t, enabled.handler, http.MethodGet, "/api/v1/auth/capabilities", "", "", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &capabilities); err != nil || !capabilities.PublicRegistrationEnabled {
		t.Fatalf("enabled capabilities body: enabled=%t err=%v", capabilities.PublicRegistrationEnabled, err)
	}
	enabled.repo.registerCreated = true
	rec = authRequest(t, enabled.handler, http.MethodPost, "/api/v1/auth/register",
		`{"email":"`+testEmail()+`","password":"`+testPassword+`"}`, "", "")
	if rec.Code != http.StatusAccepted || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("enabled register response: status=%d cache=%q", rec.Code, rec.Header().Get("Cache-Control"))
	}
	var dispatch httpapi.VerificationDispatch
	if err := json.Unmarshal(rec.Body.Bytes(), &dispatch); err != nil || dispatch.Status != httpapi.VerificationRequired {
		t.Fatalf("register public dispatch: status=%q err=%v", dispatch.Status, err)
	}
	if enabled.mail.Count() != 1 {
		t.Fatalf("new registration mail attempts=%d want=1", enabled.mail.Count())
	}
	action, err := url.Parse(enabled.mail.Last().ActionURL)
	if err != nil || action.Scheme != "https" || action.Host != "app.example.invalid" ||
		action.Path != "/verify-email" || action.RawQuery != "" || action.Fragment == "" {
		t.Fatalf("provider-neutral action URL shape: scheme=%q host=%q path=%q query=%t fragment=%t err=%v",
			action.Scheme, action.Host, action.Path, action.RawQuery != "", action.Fragment != "", err)
	}
	enabled.repo.registerCreated = false
	rec = authRequest(t, enabled.handler, http.MethodPost, "/api/v1/auth/register",
		`{"email":"`+testEmail()+`","password":"`+testPassword+`"}`, "", "")
	if rec.Code != http.StatusAccepted || enabled.mail.Count() != 1 {
		t.Fatalf("duplicate registration must keep generic outcome: status=%d mail_calls=%d", rec.Code, enabled.mail.Count())
	}
}

func TestRateLimitedResponseAndEventExposeOnlyAllowlistedMetadata(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name       string
		retryAfter time.Duration
		wantHeader string
	}{
		{name: "ceil seconds", retryAfter: 1250 * time.Millisecond, wantHeader: "2"},
		{name: "minimum one", retryAfter: 0, wantHeader: "1"},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fixture := newAuthHTTPFixture(t, true)
			fixture.repo.limiterAllowed = false
			fixture.repo.limiterRetryAfter = test.retryAfter
			rec := authRequest(
				t,
				fixture.handler,
				http.MethodPost,
				"/api/v1/auth/login",
				`{"email":"`+testEmail()+`","password":"`+testPassword+`"}`,
				testOrigin,
				"",
			)
			if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") != test.wantHeader {
				t.Fatalf("rate limited response: status=%d retry_after=%q", rec.Code, rec.Header().Get("Retry-After"))
			}
			if env := decodeEnvelope(t, rec); env.Error.Code != httpapi.CodeRateLimited {
				t.Fatalf("rate limited code=%q", env.Error.Code)
			}
			body := rec.Body.String()
			for _, forbidden := range []string{"subject", "source", "remaining", "deadline"} {
				if strings.Contains(body, forbidden) {
					t.Fatalf("rate limited body leaked limiter metadata %q: %s", forbidden, body)
				}
			}
			logged := fixture.logs.String()
			if !strings.Contains(logged, `"event":"auth.rate_limited"`) ||
				!strings.Contains(logged, `"action":"login"`) ||
				fixture.repo.limiterSourceDigest == "" ||
				!strings.Contains(logged, `"source_digest":"`+fixture.repo.limiterSourceDigest+`"`) {
				t.Fatalf("rate limited event missing allowlisted fields: %s", logged)
			}
			for _, forbidden := range []string{testEmail(), "192.0.2.1", "subject_digest", testPassword} {
				if strings.Contains(logged, forbidden) {
					t.Fatalf("rate limited event leaked %q", forbidden)
				}
			}
		})
	}
}

func TestCookieMutatingAuthEndpointsRejectUntrustedOriginBeforeApplication(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		body   string
		cookie string
	}{
		{name: "verify", method: http.MethodPost, path: "/api/v1/auth/email/verify", body: `{"token":"` + syntheticBearerWire() + `"}`},
		{name: "login", method: http.MethodPost, path: "/api/v1/auth/login", body: `{"email":"` + testEmail() + `","password":"` + testPassword + `"}`},
		{name: "refresh", method: http.MethodPost, path: "/api/v1/auth/refresh", cookie: syntheticBearerWire()},
		{name: "logout", method: http.MethodPost, path: "/api/v1/auth/logout", cookie: syntheticBearerWire()},
	}
	for _, test := range tests {
		test := test
		for _, origin := range []string{"", "https://wrong.example.invalid"} {
			t.Run(test.name+"/origin="+origin, func(t *testing.T) {
				fixture := newAuthHTTPFixture(t, true)
				rec := authRequest(t, fixture.handler, test.method, test.path, test.body, origin, test.cookie)
				if rec.Code != http.StatusForbidden {
					t.Fatalf("untrusted Origin status=%d want=403", rec.Code)
				}
				if env := decodeEnvelope(t, rec); env.Error.Code != "forbidden" {
					t.Fatalf("untrusted Origin code=%q", env.Error.Code)
				}
				if fixture.repo.Calls() != 0 || len(rec.Result().Cookies()) != 0 {
					t.Fatalf("untrusted Origin crossed boundary: repository_calls=%d cookie_mutations=%d",
						fixture.repo.Calls(), len(rec.Result().Cookies()))
				}
			})
		}
	}
}

func TestAuthSessionCookieAndErrorMatrix(t *testing.T) {
	fixture := newAuthHTTPFixture(t, true)
	rec := authRequest(t, fixture.handler, http.MethodPost, "/api/v1/auth/login",
		`{"email":"`+testEmail()+`","password":"`+testPassword+`"}`, testOrigin, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("trusted login status=%d", rec.Code)
	}
	refreshCookie := requireRefreshCookie(t, rec)
	if !refreshCookie.HttpOnly || !refreshCookie.Secure || refreshCookie.SameSite != http.SameSiteStrictMode ||
		refreshCookie.Path != "/" || refreshCookie.Domain != "" || refreshCookie.MaxAge != int((14*24*time.Hour)/time.Second) ||
		!refreshCookie.Expires.Equal(fixture.now.Add(14*24*time.Hour)) {
		t.Fatalf("refresh cookie profile: http_only=%t secure=%t same_site=%d path=%q domain=%q max_age=%d expires=%s",
			refreshCookie.HttpOnly, refreshCookie.Secure, refreshCookie.SameSite, refreshCookie.Path,
			refreshCookie.Domain, refreshCookie.MaxAge, refreshCookie.Expires)
	}
	var access httpapi.AccessTokenResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &access); err != nil || access.AccessToken == "" ||
		access.TokenType != httpapi.AccessTokenResponseTokenType("Bearer") || access.ExpiresIn != 600 {
		t.Fatalf("access response shape: has_token=%t token_type=%q expires_in=%d err=%v",
			access.AccessToken != "", access.TokenType, access.ExpiresIn, err)
	}

	rec = authRequest(t, fixture.handler, http.MethodPost, "/api/v1/auth/refresh", "", testOrigin, refreshCookie.Value)
	if rec.Code != http.StatusOK || fixture.repo.rotateCalls != 1 {
		t.Fatalf("trusted refresh: status=%d rotate_calls=%d", rec.Code, fixture.repo.rotateCalls)
	}
	rotatedCookie := requireRefreshCookie(t, rec)
	if rotatedCookie.Value == refreshCookie.Value {
		t.Fatal("refresh did not rotate the cookie bearer")
	}

	fixture.repo.rotateErr = auth.ErrInvalidToken
	rec = authRequest(t, fixture.handler, http.MethodPost, "/api/v1/auth/refresh", "", testOrigin, rotatedCookie.Value)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("invalid refresh status=%d", rec.Code)
	}
	cleared := requireRefreshCookie(t, rec)
	if cleared.MaxAge >= 0 || !cleared.Expires.Before(fixture.now) {
		t.Fatalf("invalid refresh did not clear cookie: max_age=%d expires=%s", cleared.MaxAge, cleared.Expires)
	}

	rec = authRequest(t, fixture.handler, http.MethodPost, "/api/v1/auth/logout", "", testOrigin, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("missing-cookie logout status=%d", rec.Code)
	}
	cleared = requireRefreshCookie(t, rec)
	if cleared.MaxAge >= 0 || !cleared.Expires.Before(fixture.now) {
		t.Fatalf("logout did not clear cookie: max_age=%d expires=%s", cleared.MaxAge, cleared.Expires)
	}
}

func TestAuthStructuredEventsUseAllowlistAndRedactedReferences(t *testing.T) {
	fixture := newAuthHTTPFixture(t, true)
	login := authRequest(t, fixture.handler, http.MethodPost, "/api/v1/auth/login",
		`{"email":"`+testEmail()+`","password":"`+testPassword+`"}`, testOrigin, "")
	if login.Code != http.StatusOK {
		t.Fatalf("login status=%d", login.Code)
	}
	refreshCookie := requireRefreshCookie(t, login)
	var access httpapi.AccessTokenResponse
	if err := json.Unmarshal(login.Body.Bytes(), &access); err != nil || access.AccessToken == "" {
		t.Fatalf("decode login access response: %v", err)
	}

	verification := authRequest(t, fixture.handler, http.MethodPost, "/api/v1/auth/email/verify",
		`{"token":"`+syntheticBearerWire()+`"}`, testOrigin, "")
	if verification.Code != http.StatusOK {
		t.Fatalf("verification status=%d", verification.Code)
	}

	fixture.repo.rotateErr = auth.ErrRefreshReuse
	reuse := authRequest(t, fixture.handler, http.MethodPost, "/api/v1/auth/refresh", "", testOrigin, refreshCookie.Value)
	if reuse.Code != http.StatusUnauthorized {
		t.Fatalf("refresh reuse status=%d", reuse.Code)
	}

	changed := authRequestWithAccess(t, fixture.handler, http.MethodPost, "/api/v1/auth/password/change",
		`{"current_password":"`+testPassword+`","new_password":"updated-password-123"}`,
		testOrigin, "", access.AccessToken)
	if changed.Code != http.StatusNoContent {
		t.Fatalf("change password status=%d body=%s", changed.Code, changed.Body.String())
	}

	records := authEventRecords(t, fixture.logs.String())
	if len(records) != 4 {
		t.Fatalf("auth event count=%d want=4", len(records))
	}
	byEvent := make(map[string]map[string]any, len(records))
	for _, record := range records {
		event, _ := record["event"].(string)
		byEvent[event] = record
		for key := range record {
			switch key {
			case "time", "level", "msg", "event", "result", "failure_class", "account_ref", "session_ref", "family_ref", "action", "source_digest", "provider_message_id", "dry_run":
			default:
				t.Fatalf("event %q emitted non-allowlisted key %q", event, key)
			}
		}
	}
	for _, event := range []string{"auth.login", "auth.email_verified", "auth.refresh_reuse", "auth.password_changed"} {
		if byEvent[event] == nil {
			t.Fatalf("missing structured event %q", event)
		}
	}
	if byEvent["auth.login"]["result"] != "success" || byEvent["auth.email_verified"]["result"] != "success" ||
		byEvent["auth.refresh_reuse"]["failure_class"] != "reuse" ||
		byEvent["auth.password_changed"]["action"] != "change_password" {
		t.Fatalf("event outcomes do not match fixed contract: %#v", byEvent)
	}
	for _, event := range []string{"auth.login", "auth.email_verified"} {
		accountRef, _ := byEvent[event]["account_ref"].(string)
		sessionRef, _ := byEvent[event]["session_ref"].(string)
		if !strings.HasPrefix(accountRef, "account:") || !strings.HasPrefix(sessionRef, "session:") {
			t.Fatalf("event %q omitted redacted references", event)
		}
	}

	loggedBytes, err := json.Marshal(records)
	if err != nil {
		t.Fatalf("encode auth event records: %v", err)
	}
	logged := string(loggedBytes)
	for _, forbidden := range []string{
		testEmail(), testPassword, testAcctID, access.AccessToken, refreshCookie.Value, syntheticBearerWire(),
		"Authorization", "__Host-crm_refresh",
	} {
		if forbidden != "" && strings.Contains(logged, forbidden) {
			t.Fatalf("structured auth log contains forbidden synthetic marker %q", forbidden)
		}
	}
}

func authEventRecords(t *testing.T, logged string) []map[string]any {
	t.Helper()
	records := make([]map[string]any, 0, 3)
	for _, line := range strings.Split(strings.TrimSpace(logged), "\n") {
		if line == "" {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("decode structured log: %v", err)
		}
		if event, _ := record["event"].(string); strings.HasPrefix(event, "auth.") {
			records = append(records, record)
		}
	}
	return records
}

func TestAuthTypedErrorsAndActionPageReferrerPolicy(t *testing.T) {
	fixture := newAuthHTTPFixture(t, true)
	fixture.repo.record.Account.Status = auth.AccountPendingVerification
	rec := authRequest(t, fixture.handler, http.MethodPost, "/api/v1/auth/login",
		`{"email":"`+testEmail()+`","password":"`+testPassword+`"}`, testOrigin, "")
	if rec.Code != http.StatusForbidden || decodeEnvelope(t, rec).Error.Code != "email_verification_required" {
		t.Fatalf("pending login mapping: status=%d code=%q", rec.Code, decodeEnvelope(t, rec).Error.Code)
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Fatal("pending login must not set refresh cookie")
	}

	for _, path := range []string{"/verify-email", "/reset-password"} {
		rec = authRequest(t, fixture.handler, http.MethodGet, path, "", "", "")
		if rec.Header().Get("Referrer-Policy") != "no-referrer" {
			t.Fatalf("%s referrer policy=%q", path, rec.Header().Get("Referrer-Policy"))
		}
	}

	shareHTML := authRequest(t, fixture.handler, http.MethodGet, "/shared/plans/sp1.test.token", "", "", "")
	if shareHTML.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("shared plan HTML referrer policy=%q", shareHTML.Header().Get("Referrer-Policy"))
	}
	if shareHTML.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("shared plan HTML cache=%q", shareHTML.Header().Get("Cache-Control"))
	}
	const wantCSP = "default-src 'self'; connect-src 'self'; img-src 'self' data:; script-src 'self'; style-src 'self'; font-src 'self'; object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"
	if shareHTML.Header().Get("Content-Security-Policy") != wantCSP {
		t.Fatalf("shared plan HTML CSP=%q", shareHTML.Header().Get("Content-Security-Policy"))
	}
}

func TestPasswordRoutesOriginCookieAndErrorMatrix(t *testing.T) {
	t.Parallel()
	t.Run("forgot has no Origin gate and never mutates cookie", func(t *testing.T) {
		fixture := newAuthHTTPFixture(t, true)
		rec := authRequest(t, fixture.handler, http.MethodPost, "/api/v1/auth/password/forgot",
			`{"email":"`+testEmail()+`"}`, "", syntheticBearerWire())
		if rec.Code != http.StatusAccepted || rec.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("forgot response: status=%d cache=%q", rec.Code, rec.Header().Get("Cache-Control"))
		}
		var body struct {
			Status string `json:"status"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Status != "accepted" {
			t.Fatalf("forgot body=%s err=%v", rec.Body.String(), err)
		}
		if len(rec.Result().Cookies()) != 0 {
			t.Fatal("forgot mutated refresh cookie")
		}
	})

	for _, operation := range []struct {
		name string
		path string
		body string
	}{
		{name: "reset", path: "/api/v1/auth/password/reset", body: `{"token":"` + syntheticBearerWire() + `","new_password":"new-password-1"}`},
		{name: "change", path: "/api/v1/auth/password/change", body: `{"current_password":"` + testPassword + `","new_password":"new-password-1"}`},
	} {
		operation := operation
		for _, origin := range []string{"", "https://wrong.example.invalid"} {
			t.Run(operation.name+"/origin="+origin, func(t *testing.T) {
				fixture := newAuthHTTPFixture(t, true)
				access := loginAccess(t, fixture)
				before := fixture.repo.Calls()
				rec := authRequestWithAccess(
					t, fixture.handler, http.MethodPost, operation.path, operation.body, origin,
					syntheticBearerWire(), access,
				)
				if rec.Code != http.StatusForbidden || decodeEnvelope(t, rec).Error.Code != "forbidden" {
					t.Fatalf("untrusted origin response: status=%d body=%s", rec.Code, rec.Body.String())
				}
				if fixture.repo.Calls() != before || len(rec.Result().Cookies()) != 0 {
					t.Fatalf("untrusted origin crossed boundary: calls=%d/%d cookies=%d",
						fixture.repo.Calls(), before, len(rec.Result().Cookies()))
				}
			})
		}
	}

	t.Run("reset outcomes", func(t *testing.T) {
		tests := []struct {
			name          string
			body          string
			configure     func(*authHTTPFixture)
			wantStatus    int
			wantCode      string
			wantResetCall int
			wantCookie    bool
		}{
			{name: "validation", body: `{"token":"` + syntheticBearerWire() + `","new_password":"short"}`, wantStatus: 400, wantCode: "validation_failed"},
			{name: "limited", body: `{"token":"` + syntheticBearerWire() + `","new_password":"new-password-1"}`, configure: func(f *authHTTPFixture) { f.repo.limiterAllowed = false }, wantStatus: 429, wantCode: "rate_limited"},
			{name: "invalid", body: `{"token":"` + syntheticBearerWire() + `","new_password":"new-password-1"}`, configure: func(f *authHTTPFixture) { f.repo.resetPasswordErr = auth.ErrInvalidOrExpiredActionToken }, wantStatus: 400, wantCode: "invalid_or_expired_token", wantResetCall: 1},
			{name: "internal", body: `{"token":"` + syntheticBearerWire() + `","new_password":"new-password-1"}`, configure: func(f *authHTTPFixture) { f.repo.limiterErr = errors.New("limiter unavailable") }, wantStatus: 500, wantCode: "internal"},
			{name: "success", body: `{"token":"` + syntheticBearerWire() + `","new_password":"new-password-1"}`, wantStatus: 204, wantResetCall: 1, wantCookie: true},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				fixture := newAuthHTTPFixture(t, true)
				if test.configure != nil {
					test.configure(&fixture)
				}
				rec := authRequest(t, fixture.handler, http.MethodPost, "/api/v1/auth/password/reset",
					test.body, testOrigin, syntheticBearerWire())
				assertPasswordMutationResponse(t, rec, test.wantStatus, test.wantCode, test.wantCookie, fixture.now)
				if fixture.repo.resetPasswordCalls != test.wantResetCall {
					t.Fatalf("reset calls=%d want=%d", fixture.repo.resetPasswordCalls, test.wantResetCall)
				}
			})
		}
	})

	t.Run("change outcomes", func(t *testing.T) {
		tests := []struct {
			name           string
			body           string
			configure      func(*authHTTPFixture)
			wantStatus     int
			wantCode       string
			wantChangeCall int
			wantCookie     bool
		}{
			{name: "validation", body: `{"current_password":"short","new_password":"new-password-1"}`, wantStatus: 400, wantCode: "validation_failed"},
			{name: "limited", body: `{"current_password":"` + testPassword + `","new_password":"new-password-1"}`, configure: func(f *authHTTPFixture) { f.repo.limiterAllowed = false }, wantStatus: 429, wantCode: "rate_limited"},
			{name: "wrong current", body: `{"current_password":"incorrect-pass","new_password":"new-password-1"}`, wantStatus: 401, wantCode: "unauthorized"},
			{name: "internal", body: `{"current_password":"` + testPassword + `","new_password":"new-password-1"}`, configure: func(f *authHTTPFixture) { f.repo.limiterErr = errors.New("limiter unavailable") }, wantStatus: 500, wantCode: "internal"},
			{name: "success", body: `{"current_password":"` + testPassword + `","new_password":"new-password-1"}`, wantStatus: 204, wantChangeCall: 1, wantCookie: true},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				fixture := newAuthHTTPFixture(t, true)
				access := loginAccess(t, fixture)
				if test.configure != nil {
					test.configure(&fixture)
				}
				rec := authRequestWithAccess(t, fixture.handler, http.MethodPost, "/api/v1/auth/password/change",
					test.body, testOrigin, syntheticBearerWire(), access)
				assertPasswordMutationResponse(t, rec, test.wantStatus, test.wantCode, test.wantCookie, fixture.now)
				if fixture.repo.changePasswordCalls != test.wantChangeCall {
					t.Fatalf("change calls=%d want=%d", fixture.repo.changePasswordCalls, test.wantChangeCall)
				}
			})
		}
	})
}

func TestPublicAuthRejectsOversizedBodiesBeforeApplication(t *testing.T) {
	t.Parallel()
	fixture := newAuthHTTPFixture(t, true)
	body := `{"token":"` + syntheticBearerWire() + `","new_password":"new-password-1"}` + strings.Repeat(" ", 8<<10)
	rec := authRequest(
		t, fixture.handler, http.MethodPost, "/api/v1/auth/password/reset", body, testOrigin, syntheticBearerWire(),
	)
	if rec.Code != http.StatusBadRequest || decodeEnvelope(t, rec).Error.Code != "validation_failed" {
		t.Fatalf("oversized auth body response: status=%d body=%s", rec.Code, rec.Body.String())
	}
	if fixture.repo.Calls() != 0 || len(rec.Result().Cookies()) != 0 {
		t.Fatalf("oversized auth body crossed application boundary: calls=%d cookies=%d",
			fixture.repo.Calls(), len(rec.Result().Cookies()))
	}
}

func TestMalformedActionTokensDoNotReachLimiterOrRepository(t *testing.T) {
	t.Parallel()
	secret := base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("s", 32)))
	for _, token := range []string{
		strings.Repeat("a", 31) + "." + secret,
		strings.Repeat("a", 33) + "." + secret,
		strings.Repeat("A", 32) + "." + secret,
		strings.Repeat("g", 32) + "." + secret,
		strings.Repeat("a", 32) + "." + secret[:len(secret)-1],
		strings.Repeat("a", 32) + "." + secret + "A",
	} {
		token := token
		t.Run(fmt.Sprintf("wire_length_%d", len(token)), func(t *testing.T) {
			t.Parallel()
			fixture := newAuthHTTPFixture(t, true)
			body, err := json.Marshal(map[string]string{"token": token, "new_password": "new-password-1"})
			if err != nil {
				t.Fatalf("encode malformed token body: %v", err)
			}
			rec := authRequest(
				t, fixture.handler, http.MethodPost, "/api/v1/auth/password/reset", string(body), testOrigin, "",
			)
			if rec.Code != http.StatusBadRequest || decodeEnvelope(t, rec).Error.Code != "invalid_or_expired_token" {
				t.Fatalf("malformed token response: status=%d body=%s", rec.Code, rec.Body.String())
			}
			if fixture.repo.Calls() != 0 || len(rec.Result().Cookies()) != 0 {
				t.Fatalf("malformed token crossed protected boundary: calls=%d cookies=%d",
					fixture.repo.Calls(), len(rec.Result().Cookies()))
			}
		})
	}
}

func loginAccess(t *testing.T, fixture authHTTPFixture) string {
	t.Helper()
	rec := authRequest(t, fixture.handler, http.MethodPost, "/api/v1/auth/login",
		`{"email":"`+testEmail()+`","password":"`+testPassword+`"}`, testOrigin, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("login for access status=%d body=%s", rec.Code, rec.Body.String())
	}
	var response httpapi.AccessTokenResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil || response.AccessToken == "" {
		t.Fatalf("decode login access: %v", err)
	}
	return response.AccessToken
}

func assertPasswordMutationResponse(
	t *testing.T,
	rec *httptest.ResponseRecorder,
	wantStatus int,
	wantCode string,
	wantCookie bool,
	now time.Time,
) {
	t.Helper()
	if rec.Code != wantStatus {
		t.Fatalf("status=%d want=%d body=%s", rec.Code, wantStatus, rec.Body.String())
	}
	if wantCode != "" && decodeEnvelope(t, rec).Error.Code != wantCode {
		t.Fatalf("code=%q want=%q", decodeEnvelope(t, rec).Error.Code, wantCode)
	}
	if !wantCookie {
		if len(rec.Result().Cookies()) != 0 || rec.Header().Get("Set-Cookie") != "" {
			t.Fatalf("non-success response mutated cookie: %q", rec.Header().Get("Set-Cookie"))
		}
		return
	}
	cookie := requireRefreshCookie(t, rec)
	if cookie.MaxAge >= 0 || !cookie.Expires.Before(now) {
		t.Fatalf("success did not clear cookie: max_age=%d expires=%s", cookie.MaxAge, cookie.Expires)
	}
}

func authRequest(t *testing.T, handler http.Handler, method, path, body, origin, refresh string) *httptest.ResponseRecorder {
	return authRequestWithAccess(t, handler, method, path, body, origin, refresh, "")
}

func authRequestWithAccess(
	t *testing.T,
	handler http.Handler,
	method, path, body, origin, refresh, access string,
) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if refresh != "" {
		req.AddCookie(&http.Cookie{Name: "__Host-crm_refresh", Value: refresh})
	}
	if access != "" {
		req.Header.Set("Authorization", "Bearer "+access)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func requireRefreshCookie(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == "__Host-crm_refresh" {
			return cookie
		}
	}
	t.Fatal("response omitted refresh cookie")
	return nil
}

func syntheticBearerWire() string {
	return strings.Repeat("a", 32) + "." + base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("s", 32)))
}
