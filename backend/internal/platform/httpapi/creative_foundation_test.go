package httpapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
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
