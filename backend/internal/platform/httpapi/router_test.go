package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/samson/customer-manage-platform/backend/internal/platform/httpapi"
)

type fakePinger struct {
	err error
}

func (f fakePinger) Ping(context.Context) error { return f.err }

func newTestRouter(t *testing.T, db httpapi.Pinger) http.Handler {
	t.Helper()
	return httpapi.NewRouter(httpapi.RouterDeps{
		Logger: slog.New(slog.DiscardHandler),
		DB:     db,
	})
}

func doRequest(t *testing.T, h http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decodeEnvelope(t *testing.T, rec *httptest.ResponseRecorder) httpapi.ErrorEnvelope {
	t.Helper()
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("response is not an ErrorEnvelope: %v; body=%s", err, rec.Body.String())
	}
	return env
}

// A7：healthz 无鉴权 200；DB 不可达（注入失败 pinger）503。
func TestHealthz(t *testing.T) {
	rec := doRequest(t, newTestRouter(t, fakePinger{}), http.MethodGet, "/healthz")
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz should be 200 without auth, got %d", rec.Code)
	}

	rec = doRequest(t, newTestRouter(t, fakePinger{err: errors.New("db down")}), http.MethodGet, "/healthz")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("healthz with unreachable DB should be 503, got %d", rec.Code)
	}
	var body struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Status != "degraded" {
		t.Fatalf("healthz 503 body should be degraded, got %s", rec.Body.String())
	}
}

// A7：未注册 API 路径与方法不匹配一律 404 not_found 封套。
func TestUnregisteredAPIPathsReturn404Envelope(t *testing.T) {
	r := httpapi.NewRouter(httpapi.RouterDeps{Logger: slog.New(slog.DiscardHandler), DB: fakePinger{}})
	// 已注册路径 + 不匹配方法（POST 有注册、GET 走 NoRoute）
	r.POST("/api/v1/probe", func(c *gin.Context) {})

	cases := []struct{ method, path string }{
		{http.MethodGet, "/api/v1/no-such"},     // 未注册路径
		{http.MethodGet, "/api/v1/probe"},       // 方法不匹配
		{http.MethodDelete, "/api/v1/probe/xx"}, // 未注册子路径
		{http.MethodGet, "/api/v1/me/"},         // Gin 尾斜杠不应自动 301 绕过封套
		{http.MethodGet, "/api"},                // API 前缀本身也不落入 SPA fallback
		{http.MethodGet, "//api/v1/me"},         // 双斜杠 API 路径不落入 SPA fallback
	}
	for _, tc := range cases {
		rec := doRequest(t, r, tc.method, tc.path)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s %s: want 404, got %d", tc.method, tc.path, rec.Code)
		}
		if env := decodeEnvelope(t, rec); env.Error.Code != "not_found" {
			t.Fatalf("%s %s: want not_found envelope, got %+v", tc.method, tc.path, env)
		}
	}
}

func TestDataExportRequiresAuthentication(t *testing.T) {
	rec := doRequest(t, newAuthRouter(t), http.MethodGet, "/api/v1/export")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET /api/v1/export without token: want 401, got %d body=%s", rec.Code, rec.Body.String())
	}
	if env := decodeEnvelope(t, rec); env.Error.Code != "unauthorized" {
		t.Fatalf("GET /api/v1/export without token: want unauthorized, got %+v", env)
	}
}

// 非 API fallback 只承接浏览器 GET/HEAD 导航；POST 等非导航请求不返回 SPA。
func TestNonAPINonNavigationRequestDoesNotServeSPA(t *testing.T) {
	rec := doRequest(t, newTestRouter(t, fakePinger{}), http.MethodPost, "/whatever")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("non-API POST should not serve SPA fallback, got %d", rec.Code)
	}
	if strings.Contains(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("non-API POST should not serve SPA HTML, got %s", rec.Header().Get("Content-Type"))
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err == nil && env.Error.Code != "" {
		t.Fatalf("non-API POST must not return a JSON envelope, got %s", rec.Body.String())
	}
}

// 非 API 路径不适用封套（S9 起由 go:embed SPA fallback 承接）：
// 产物已同步 → 200 text/html；产物未同步（空 dist）→ 纯 404，两者都不是 JSON 封套。
func TestNonAPIPathNotEnveloped(t *testing.T) {
	rec := doRequest(t, newTestRouter(t, fakePinger{}), http.MethodGet, "/no-such-page")
	if rec.Code != http.StatusOK && rec.Code != http.StatusNotFound {
		t.Fatalf("want 200 (SPA fallback) or 404 (dist not synced), got %d", rec.Code)
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err == nil && env.Error.Code != "" {
		t.Fatalf("non-API path must not return a JSON envelope, got %s", rec.Body.String())
	}
	if rec.Code == http.StatusOK && !strings.Contains(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("SPA fallback should serve text/html, got %s", rec.Header().Get("Content-Type"))
	}
}

// A8：handler panic → 500 internal 封套，进程存活（后续请求照常服务）。
func TestPanicBecomesInternalEnvelope(t *testing.T) {
	r := httpapi.NewRouter(httpapi.RouterDeps{Logger: slog.New(slog.DiscardHandler), DB: fakePinger{}})
	r.GET("/api/v1/boom", func(c *gin.Context) { panic("boom") })

	rec := doRequest(t, r, http.MethodGet, "/api/v1/boom")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("panic should map to 500, got %d", rec.Code)
	}
	if env := decodeEnvelope(t, rec); env.Error.Code != "internal" {
		t.Fatalf("want internal envelope, got %+v", env)
	}

	// 进程存活：同一 engine 继续正常服务
	rec = doRequest(t, r, http.MethodGet, "/healthz")
	if rec.Code != http.StatusOK {
		t.Fatalf("engine should keep serving after panic, got %d", rec.Code)
	}
}
