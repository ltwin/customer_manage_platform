package httpapi

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestSensitiveRequestLabelRedactsShareTokenAndAssetRef(t *testing.T) {
	canary := "sp1.AbCdEfGhIjKlMnOp.0123456789abcdefghijklmnopqrstuvwxyzABCDEFG"
	assetRef := "ref_" + strings.Repeat("x", 24)

	cases := []struct {
		path string
		want string
	}{
		{path: "/healthz", want: "/healthz"},
		{path: "/api/v1/me", want: "/api/v1/me"},
		{path: "/api/v1/customers", want: "/api/v1/customers"},
		{path: "/dashboard", want: "/dashboard"},
		{path: "/shared/plans/" + canary, want: "/shared/plans/:token"},
		{path: "/api/v1/shared/plans/" + canary, want: "/api/v1/shared/plans/:token"},
		{path: "/api/v1/shared/plans/" + canary + "/feedback", want: "/api/v1/shared/plans/:token/feedback"},
		{
			path: "/api/v1/shared/plans/" + canary + "/shots/shot_1/feedback",
			want: "/api/v1/shared/plans/:token/shots/shot_1/feedback",
		},
		{
			path: "/api/v1/shared/plans/" + canary + "/assets/" + assetRef + "/content",
			want: "/api/v1/shared/plans/:token/assets/:ref/content",
		},
	}
	for _, tc := range cases {
		if got := sensitiveRequestLabel(tc.path); got != tc.want {
			t.Fatalf("sensitiveRequestLabel(%q)=%q want %q", tc.path, got, tc.want)
		}
	}
}

func TestMiddlewareLogsSanitizeShareTokenCanary(t *testing.T) {
	canary := "sp1.AbCdEfGhIjKlMnOp.0123456789abcdefghijklmnopqrstuvwxyzABCDEFG"
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))

	engine := gin.New()
	engine.Use(
		recoveryMiddleware(logger),
		requestLogMiddleware(logger),
		errorEnvelopeMiddleware(logger),
	)
	engine.GET("/api/v1/me", func(c *gin.Context) {
		c.Status(http.StatusUnauthorized)
	})
	engine.GET("/shared/plans/:token", func(c *gin.Context) {
		c.Status(http.StatusNotFound)
	})
	engine.GET("/api/v1/shared/plans/:token/assets/:ref/content", func(c *gin.Context) {
		_ = c.Error(errFixture{})
	})
	engine.GET("/api/v1/shared/plans/:token/panic", func(c *gin.Context) {
		panic("share-path-panic")
	})

	for _, path := range []string{
		"/api/v1/me",
		"/shared/plans/" + canary,
		"/api/v1/shared/plans/" + canary + "/assets/ref_canary/content",
		"/api/v1/shared/plans/" + canary + "/panic",
	} {
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	}

	body := logs.String()
	if strings.Contains(body, canary) {
		t.Fatalf("middleware logs retained raw share token canary: %s", body)
	}
	if !strings.Contains(body, `"/api/v1/me"`) {
		t.Fatalf("non-sensitive path must stay verbatim in logs: %s", body)
	}
	if !strings.Contains(body, `"/shared/plans/:token"`) {
		t.Fatalf("web share path must log route template: %s", body)
	}
	if !strings.Contains(body, `"/api/v1/shared/plans/:token/assets/:ref/content"`) {
		t.Fatalf("shared asset path must log route template: %s", body)
	}
	if !strings.Contains(body, `"panic recovered"`) {
		t.Fatalf("panic path should emit recovery log: %s", body)
	}
}

func TestMiddlewareKeepsNonSensitivePathAuthBehavior(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	engine := gin.New()
	engine.Use(
		recoveryMiddleware(logger),
		requestLogMiddleware(logger),
		errorEnvelopeMiddleware(logger),
	)
	engine.GET("/api/v1/customers", func(c *gin.Context) {
		abortError(c, http.StatusUnauthorized, CodeUnauthorized, "未认证")
	})

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/customers", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}
	var env ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil || env.Error.Code != "unauthorized" {
		t.Fatalf("want unauthorized envelope, got %s", rec.Body.String())
	}
	if !strings.Contains(logs.String(), `"/api/v1/customers"`) {
		t.Fatalf("non-sensitive auth failure must log raw path: %s", logs.String())
	}
}

type errFixture struct{}

func (errFixture) Error() string { return "fixture handler error" }
