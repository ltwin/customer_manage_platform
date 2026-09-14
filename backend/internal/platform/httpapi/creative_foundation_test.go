package httpapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCreativeFoundationRoutesRequireAuthentication(t *testing.T) {
	r := NewRouter(RouterDeps{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	for _, path := range []string{"/api/v1/creative/capabilities", "/api/v1/creative/operations/8a523818-8704-483a-9fe5-46b36be83ca8"} {
		t.Run(path, func(t *testing.T) {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
			if w.Code != 401 {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

// TestEveryCreativeRouteRequiresABearerToken walks the real route table instead
// of a hand-kept list, because the failure it guards against is exactly the one
// a hand-kept list cannot see: a new route registered in the wrong group.
func TestEveryCreativeRouteRequiresABearerToken(t *testing.T) {
	r := NewRouter(RouterDeps{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	// Byte delivery is authorised by a signed ticket in the URL, not a bearer
	// token, so those routes legitimately answer without one.
	// This list is exactly registerCreativeMediaBytes; anything else answering
	// without a token is the bug this test exists to catch. Keyed by method as
	// well as path, so adding an unauthenticated verb to an already-exempt path
	// cannot slip through under the existing entry.
	ticketAuthorised := map[string]bool{
		"PUT /api/v1/creative/media-parts":               true,
		"GET /api/v1/creative/media/:revision_id/:role":  true,
		"HEAD /api/v1/creative/media/:revision_id/:role": true,
	}
	checked := 0
	for _, route := range r.Routes() {
		if !strings.HasPrefix(route.Path, "/api/v1/creative/") || ticketAuthorised[route.Method+" "+route.Path] {
			continue
		}
		checked++
		t.Run(route.Method+" "+route.Path, func(t *testing.T) {
			// Any syntactically valid value satisfies the path parameters; the
			// request must be refused before anything looks at them.
			path := placeholderPattern.ReplaceAllString(route.Path, "8a523818-8704-483a-9fe5-46b36be83ca8")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(route.Method, path, nil))
			if w.Code != 401 {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
	if checked == 0 {
		t.Fatal("the walk found no creative routes; the prefix or the router changed")
	}
}

var placeholderPattern = regexp.MustCompile(`:[a-zA-Z_0-9]+`)

func TestCreativeFoundationDoesNotAdvertiseUnimplementedActions(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	(&handlers{}).GetCreativeCapabilities(c)
	var result CreativeFoundationCapabilities
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Available || len(result.NodeTypes) != 0 || len(result.Tools) != 0 || result.Reason != "foundation_only" {
		t.Fatalf("premature capability: %+v", result)
	}
}
