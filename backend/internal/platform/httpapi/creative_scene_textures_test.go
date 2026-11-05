package httpapi_test

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
	"github.com/samson/customer-manage-platform/backend/internal/platform/httpapi"
	"github.com/samson/customer-manage-platform/backend/internal/platform/immutablefs"
)

// The built-in textures are read by any account that may read the creative
// space, as the files are, with a year's caching; a file the catalog lists
// but the store lacks is a deployment problem, said as one code.
func TestSceneTextureRead(t *testing.T) {
	objects, err := immutablefs.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	files := creativescene.BuiltinTextureFiles()
	present, absent := files[0].Name, files[1].Name
	body := []byte("jpeg bytes")
	sum := sha256.Sum256(body)
	if _, _, err := objects.PutImmutable(t.Context(), "scene-textures/"+present, body, immutablefs.Metadata{MediaType: "image/jpeg", Size: int64(len(body)), Checksum: "sha256-" + hex.EncodeToString(sum[:])}); err != nil {
		t.Fatal(err)
	}
	h, _, issuer, url := newCustomerAPIRouterWithURL(t, func(d *httpapi.RouterDeps) {
		d.SceneTextures = creativescene.NewBuiltinTextures(objects)
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
		req := httptest.NewRequest(method, "/api/v1/creative/scene-textures/"+file, nil)
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

	if w := get("GET", present, "", ""); w.Code != 401 {
		t.Fatalf("no token: %d", w.Code)
	}
	for _, file := range []string{"wood.jpg", strings.Repeat("0", 64) + ".jpg"} {
		if w := get("GET", file, token, ""); w.Code != 404 {
			t.Fatalf("%s: %d %s", file, w.Code, w.Body)
		}
	}
	w := get("GET", absent, token, "")
	var env struct {
		Error struct {
			Code    string         `json:"code"`
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil || w.Code != 503 || env.Error.Code != "creative_texture_unavailable" || env.Error.Details["file"] != absent {
		t.Fatalf("not imported: %d %s", w.Code, w.Body)
	}

	w = get("GET", present, token, "")
	etag := `"` + strings.TrimSuffix(present, ".jpg") + `"`
	if w.Code != 200 || w.Body.String() != string(body) || w.Header().Get("Content-Type") != "image/jpeg" ||
		w.Header().Get("Content-Length") != "10" || w.Header().Get("ETag") != etag ||
		w.Header().Get("Cache-Control") != "private, max-age=31536000, immutable" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("read: %d %q %v", w.Code, w.Body, w.Header())
	}
	if w := get("HEAD", present, token, ""); w.Code != 200 || w.Body.Len() != 0 || w.Header().Get("Content-Length") != "10" {
		t.Fatalf("head: %d %v", w.Code, w.Header())
	}
	if w := get("GET", present, token, etag); w.Code != 304 || w.Body.Len() != 0 || w.Header().Get("ETag") != etag {
		t.Fatalf("not modified: %d", w.Code)
	}
}
