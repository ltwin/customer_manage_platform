package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"
)

func sandboxServer(dist fstest.MapFS, base string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	serve := staticHandler(dist, base)
	r.NoRoute(serve)
	return r
}

// The sandbox page carries the CSP with the configured origin written out; it
// is never served without one, and nothing under its prefix falls back to the
// application shell.
func TestSceneSandboxPage(t *testing.T) {
	template := "default-src 'none'; script-src {origin}/scene-sandbox/ blob:; frame-ancestors {origin}\n"
	dist := fstest.MapFS{
		"index.html":                       {Data: []byte("<app>")},
		"scene-sandbox/sandbox.html":       {Data: []byte("<sandbox>")},
		"scene-sandbox/csp.txt":            {Data: []byte(template)},
		"scene-sandbox/sandbox.abc123.js":  {Data: []byte("void 0")},
		"scene-sandbox/nested/placeholder": {Data: []byte("x")},
	}
	get := func(r *gin.Engine, path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		return w
	}
	r := sandboxServer(dist, "https://crm.example.com/app/")
	w := get(r, "/scene-sandbox/sandbox.html")
	want := "default-src 'none'; script-src https://crm.example.com/scene-sandbox/ blob:; frame-ancestors https://crm.example.com"
	if w.Code != 200 || w.Body.String() != "<sandbox>" || w.Header().Get("Content-Security-Policy") != want || w.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("sandbox page %d %q csp %q", w.Code, w.Body.String(), w.Header().Get("Content-Security-Policy"))
	}
	if w := get(r, "/scene-sandbox/sandbox.abc123.js"); w.Code != 200 || w.Body.String() != "void 0" {
		t.Fatalf("sandbox script %d %q", w.Code, w.Body.String())
	}
	for _, path := range []string{"/scene-sandbox/", "/scene-sandbox", "/scene-sandbox/missing.js", "/scene-sandbox/nested/", "/scene-sandbox/index.html"} {
		w := get(r, path)
		if path == "/scene-sandbox" {
			// Without the trailing slash the path is not under the prefix: the
			// application shell, as for any other route.
			if w.Code != 200 || w.Body.String() != "<app>" {
				t.Fatalf("%s: %d %q", path, w.Code, w.Body.String())
			}
			continue
		}
		if w.Code != 404 || w.Body.String() == "<app>" {
			t.Fatalf("%s fell back to the application: %d %q", path, w.Code, w.Body.String())
		}
	}
	if w := get(r, "/projects/1"); w.Code != 200 || w.Body.String() != "<app>" {
		t.Fatalf("SPA fallback changed: %d", w.Code)
	}

	noCSP := fstest.MapFS{"index.html": dist["index.html"], "scene-sandbox/sandbox.html": dist["scene-sandbox/sandbox.html"]}
	for name, r := range map[string]*gin.Engine{
		"template missing":   sandboxServer(noCSP, "https://crm.example.com"),
		"origin missing":     sandboxServer(dist, ""),
		"origin not a URL":   sandboxServer(dist, "crm.example.com"),
		"origin not http(s)": sandboxServer(dist, "ftp://crm.example.com"),
	} {
		if w := get(r, "/scene-sandbox/sandbox.html"); w.Code != 404 || w.Header().Get("Content-Security-Policy") != "" {
			t.Fatalf("%s: served %d without a CSP", name, w.Code)
		}
	}
}
