package httpapi_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/samson/customer-manage-platform/backend/internal/creativeglb"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
)

// POST /creative/assets/from-glb through routing and error mapping; the rules
// behind it are covered in creativescene.
func TestCreativeAssetFromGLBHTTP(t *testing.T) {
	router, st, tokens, url := newCustomerAPIRouterWithURL(t)
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(`UPDATE accounts SET status='active' WHERE id='` + testAcctID + `'`); err != nil {
		t.Fatal(err)
	}
	token := issueToken(t, tokens, testAcctID)
	scope := st.ScopeFor(auth.AccountContext{AccountID: testAcctID})
	glb, err := os.ReadFile(filepath.Join("..", "..", "creativeglb", "testdata", "basic.glb"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := creativeglb.Inspect(glb, creativeglb.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	slot := manifest.Slots[0].ID
	image := stageForHTTP(t, db, scope, "image", manifest)
	var revision string
	if err := db.QueryRow(`SELECT content_revision_id FROM creative_upload_candidates WHERE upload_id=$1`, image).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	payload := map[string]any{"glb_upload_id": stageForHTTP(t, db, scope, "model", manifest), "thumbnail_upload_id": stageForHTTP(t, db, scope, "image", manifest),
		"texture_upload_ids": []string{image}, "mount": "free",
		"import":    map[string]any{"file_name": "lantern.glb", "attribution": "Lantern, CC0", "source_url": "https://example.com/lantern"},
		"materials": map[string]any{slot: map[string]any{"preset": "principled@1", "params": map[string]any{}, "maps": map[string]any{"emissive": map[string]any{"content_revision_id": revision}}}},
		"target":    map[string]any{"title": "灯笼"}}
	post := func(change func(map[string]any)) *httptest.ResponseRecorder {
		t.Helper()
		p := map[string]any{}
		for k, v := range payload {
			p[k] = v
		}
		change(p)
		key := uuid.NewString()
		raw, err := json.Marshal(map[string]any{"operation_id": key, "client_created_at": time.Now().UTC(), "payload": p})
		if err != nil {
			t.Fatal(err)
		}
		return shootPlanningRequest(t, router, "POST", "/api/v1/creative/assets/from-glb", token, key, string(raw))
	}
	want := func(name string, w *httptest.ResponseRecorder, status int, code string) {
		t.Helper()
		if w.Code != status || code != "" && documentErrorCode(t, w) != code {
			t.Fatalf("%s: %d %s, want %d %s", name, w.Code, w.Body.String(), status, code)
		}
	}

	bad := post(func(p map[string]any) {
		p["materials"] = map[string]any{"nope": map[string]any{"preset": "principled@1", "params": map[string]any{}}}
	})
	want("not a slot", bad, 400, "validation_failed")
	if !strings.Contains(bad.Body.String(), `"field":"materials.nope"`) {
		t.Fatalf("not a slot names no field: %s", bad.Body.String())
	}
	want("image unavailable", post(func(p map[string]any) {
		p["materials"] = map[string]any{slot: map[string]any{"preset": "principled@1", "params": map[string]any{}, "maps": map[string]any{"base_color": map[string]any{"content_revision_id": "ccrv_missing"}}}}
		p["texture_upload_ids"] = []string{}
	}), 400, "creative_image_unavailable")
	want("missing group", post(func(p map[string]any) {
		p["target"] = map[string]any{"title": "灯笼", "group_ids": []string{"cagr_missing"}}
	}), 404, "not_found")
	gone := post(func(p map[string]any) { p["thumbnail_upload_id"] = "ccup_gone" })
	want("upload unavailable", gone, 409, "creative_upload_unavailable")
	if !strings.Contains(gone.Body.String(), `"upload_id":"ccup_gone"`) {
		t.Fatalf("unavailable upload not named: %s", gone.Body.String())
	}
	want("too many uploads", post(func(p map[string]any) {
		ids := make([]string, 65)
		for i := range ids {
			ids[i] = fmt.Sprintf("ccup_%02d", i)
		}
		p["texture_upload_ids"] = ids
	}), 413, "creative_size_limit")

	created := post(func(map[string]any) {})
	want("import", created, 201, "")
	var asset struct {
		ID     string `json:"asset_id"`
		Reused bool   `json:"reused"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &asset); err != nil || asset.Reused {
		t.Fatalf("created %s", created.Body.String())
	}
	got := shootPlanningRequest(t, router, "GET", "/api/v1/creative/assets/"+asset.ID, token, "", "")
	want("read asset", got, 200, "")
	if body := got.Body.String(); !strings.Contains(body, `"generator":{"mount":"free","language":"imported"}`) || !strings.Contains(body, `"file_name":"lantern.glb"`) ||
		!strings.Contains(body, `"emissive":{"content_revision_id":"`+revision+`"}`) {
		t.Fatalf("asset read %s", body)
	}
}
