package httpapi_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
	"github.com/samson/customer-manage-platform/backend/internal/platform/httpapi"
)

// sceneModelFiles stands in for the built-in models' store: the catalog
// ships without entries until the base bodies come, so the handler is driven
// by what the store says.
type sceneModelFiles map[string]error

func (f sceneModelFiles) Open(_ context.Context, name string) (io.ReadCloser, int64, error) {
	err, ok := f[name]
	switch {
	case !ok:
		return nil, 0, creativescene.ErrBuiltinModelUnknown
	case err != nil:
		return nil, 0, err
	}
	return io.NopCloser(strings.NewReader("glb bytes")), 9, nil
}

// AC-12: the built-in models are read by any account that may read the
// creative space, with a year's caching; a listed file the store lacks is a
// deployment problem, said as one code.
func TestSceneModelRead(t *testing.T) {
	present, absent := strings.Repeat("a", 64)+".glb", strings.Repeat("b", 64)+".glb"
	files := sceneModelFiles{present: nil, absent: creativescene.ErrBuiltinModelMissing}
	h, _, issuer, url := newCustomerAPIRouterWithURL(t, func(d *httpapi.RouterDeps) {
		d.SceneModels = files
	})
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(`UPDATE accounts SET status='active' WHERE id='` + testAcctID + `'`); err != nil {
		t.Fatal(err)
	}
	token := issueToken(t, issuer, testAcctID)
	get := func(method, file, tok, etag string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, "/api/v1/creative/scene-models/"+file, nil)
		if tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
		if etag != "" {
			req.Header.Set("If-None-Match", etag)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w
	}
	unavailable := func(w *httptest.ResponseRecorder, file string) {
		t.Helper()
		var env struct {
			Error struct {
				Code    string         `json:"code"`
				Details map[string]any `json:"details"`
			} `json:"error"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil || w.Code != 503 || env.Error.Code != "creative_model_file_unavailable" || env.Error.Details["file"] != file {
			t.Fatalf("unavailable: %d %s", w.Code, w.Body)
		}
	}

	if w := get("GET", present, "", ""); w.Code != 401 {
		t.Fatalf("no token: %d", w.Code)
	}
	for _, file := range []string{"body.glb", strings.Repeat("a", 64) + ".gltf", strings.Repeat("c", 64) + ".glb"} {
		if w := get("GET", file, token, ""); w.Code != 404 {
			t.Fatalf("%s: %d %s", file, w.Code, w.Body)
		}
	}
	unavailable(get("GET", absent, token, ""), absent)
	files[absent] = errors.New("oss: timeout")
	unavailable(get("GET", absent, token, ""), absent)

	w := get("GET", present, token, "")
	etag := `"` + strings.Repeat("a", 64) + `"`
	if w.Code != 200 || w.Body.String() != "glb bytes" || w.Header().Get("Content-Type") != "model/gltf-binary" ||
		w.Header().Get("Content-Length") != "9" || w.Header().Get("ETag") != etag ||
		w.Header().Get("Cache-Control") != "private, max-age=31536000, immutable" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("read: %d %q %v", w.Code, w.Body, w.Header())
	}
	if w := get("HEAD", present, token, ""); w.Code != 200 || w.Body.Len() != 0 || w.Header().Get("Content-Length") != "9" {
		t.Fatalf("head: %d %v", w.Code, w.Header())
	}
	if w := get("GET", present, token, etag); w.Code != 304 || w.Body.Len() != 0 || w.Header().Get("ETag") != etag {
		t.Fatalf("not modified: %d", w.Code)
	}
	// A matching If-None-Match does not pass over the checks: off the list 404, missing 503.
	unknown := strings.Repeat("c", 64) + ".glb"
	if w := get("GET", unknown, token, `"`+strings.Repeat("c", 64)+`"`); w.Code != 404 {
		t.Fatalf("unknown with etag: %d %s", w.Code, w.Body)
	}
	files[absent] = creativescene.ErrBuiltinModelMissing
	unavailable(get("GET", absent, token, `"`+strings.Repeat("b", 64)+`"`), absent)
}
