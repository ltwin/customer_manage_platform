package httpapi_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
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
)

// fakeAccounts 满足 auth.AccountReader（单账号内存替身）。
type fakeAccounts struct {
	hash string
}

func (f fakeAccounts) FirstAccount(context.Context) (string, string, error) {
	return testAcctID, f.hash, nil
}

func (f fakeAccounts) AccountByID(_ context.Context, id string) (auth.Account, error) {
	return auth.Account{ID: id, CreatedAt: time.Date(2026, 7, 6, 0, 0, 0, 0, time.UTC)}, nil
}

func newAuthRouter(t *testing.T) http.Handler {
	t.Helper()
	hash, err := auth.HashPassword(testPassword)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	svc := auth.NewService(fakeAccounts{hash: hash}, auth.NewTokenIssuer(testSecret))
	return httpapi.NewRouter(httpapi.RouterDeps{
		Logger: slog.New(slog.DiscardHandler),
		DB:     fakePinger{},
		Auth:   svc,
	})
}

func postLogin(t *testing.T, h http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
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

	rec := postLogin(t, h, `{"password":"`+testPassword+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("login with correct password: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var loginResp struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &loginResp); err != nil || loginResp.Token == "" {
		t.Fatalf("login response should carry token, got %s", rec.Body.String())
	}

	rec = getMe(t, h, "Bearer "+loginResp.Token)
	if rec.Code != http.StatusOK {
		t.Fatalf("/me with valid token: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var me struct {
		ID        string    `json:"id"`
		CreatedAt time.Time `json:"created_at"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil {
		t.Fatalf("decode /me: %v", err)
	}
	if me.ID != testAcctID || me.CreatedAt.IsZero() {
		t.Fatalf("/me should return account id and created_at, got %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "password_hash") {
		t.Fatalf("/me must never contain password_hash: %s", rec.Body.String())
	}
}

// A3：错误密码 → 401 unauthorized 封套。
func TestLoginWrongPassword(t *testing.T) {
	rec := postLogin(t, newAuthRouter(t), `{"password":"wrong"}`)
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
	for _, body := range []string{`{}`, ``, `not-json`} {
		rec := postLogin(t, h, body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body %q: want 400, got %d", body, rec.Code)
		}
		if env := decodeEnvelope(t, rec); env.Error.Code != "validation_failed" {
			t.Fatalf("body %q: want validation_failed envelope, got %+v", body, env)
		}
	}
}

// A6：无 token / 篡改 token / 过期 token → 均 401 封套。
func TestMeUnauthorizedPaths(t *testing.T) {
	h := newAuthRouter(t)

	// 有效 token 作篡改底料
	rec := postLogin(t, h, `{"password":"`+testPassword+`"}`)
	var loginResp struct {
		Token string `json:"token"`
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
		"篡改 token": "Bearer " + loginResp.Token + "x",
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
