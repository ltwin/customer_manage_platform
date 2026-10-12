package httpapi_test

import (
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// The library routes added for the asset page: rating, facets, group undo,
// emptying the recycle bin and usage, through routing and parameter parsing.
func TestCreativeLibraryHTTPRatingFacetsUndoAndTrash(t *testing.T) {
	router, _, tokens, url := newCustomerAPIRouterWithURL(t)
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec("UPDATE accounts SET status='active' WHERE id=$1", testAcctID); err != nil {
		t.Fatal(err)
	}
	token := issueToken(t, tokens, testAcctID)
	post := func(path string, payload any) *httptest.ResponseRecorder {
		t.Helper()
		key := uuid.NewString()
		body, err := json.Marshal(map[string]any{"operation_id": key, "client_created_at": time.Now().UTC(), "payload": payload})
		if err != nil {
			t.Fatal(err)
		}
		return shootPlanningRequest(t, router, "POST", path, token, key, string(body))
	}
	get := func(path string, into any) int {
		t.Helper()
		w := shootPlanningRequest(t, router, "GET", path, token, "", "")
		if w.Code == 200 && into != nil {
			if err := json.Unmarshal(w.Body.Bytes(), into); err != nil {
				t.Fatal(err)
			}
		}
		return w.Code
	}
	ids := []string{}
	for _, title := range []string{"one", "two"} {
		w := post("/api/v1/creative/assets", map[string]any{"title": title, "content": map[string]any{"kind": "text", "payload": map[string]any{"body": "正文"}, "rights": map[string]any{"source_class": "photographer_owned", "rights_basis": "ownership_attested"}}})
		if w.Code != 201 {
			t.Fatalf("create %d %s", w.Code, w.Body.String())
		}
		var created struct {
			ID string `json:"asset_id"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &created)
		ids = append(ids, created.ID)
	}

	type page struct {
		Items []struct {
			ID     string   `json:"id"`
			Rating *float64 `json:"rating"`
		} `json:"items"`
		NextCursor      string          `json:"next_cursor"`
		TotalCount      int             `json:"total_count"`
		LibraryRevision string          `json:"library_revision"`
		Facets          json.RawMessage `json:"facets"`
	}
	var p page
	if code := get("/api/v1/creative/assets?include_facets=true&limit=1", &p); code != 200 || !strings.Contains(string(p.Facets), `"text":2`) || p.Items[0].Rating != nil {
		t.Fatalf("first page %d %+v %s", code, p, p.Facets)
	}
	if code := get("/api/v1/creative/assets?include_facets=true&cursor="+p.NextCursor, nil); code != 400 {
		t.Fatal("facets with cursor", code)
	}
	// A present min_rating is always validated; 0 is not "absent".
	for _, query := range []string{"unrated=true&min_rating=1", "min_rating=0", "min_rating=0&unrated=true", "min_rating=0.3"} {
		if code := get("/api/v1/creative/assets?"+query, nil); code != 400 {
			t.Fatal(query, code)
		}
	}
	if code := get("/api/v1/creative/assets?unrated=true", &p); code != 200 || p.TotalCount != 2 {
		t.Fatal("unrated alone", code, p.TotalCount)
	}
	if w := post("/api/v1/creative/assets/batch-rate", map[string]any{"items": []map[string]any{{"asset_id": ids[0], "expected_revision": "1", "rating": 4.5}}}); w.Code != 200 || !strings.Contains(w.Body.String(), `"revision":"2"`) {
		t.Fatalf("rate %d %s", w.Code, w.Body.String())
	}
	if code := get("/api/v1/creative/assets?min_rating=4&sort=rating", &p); code != 200 || p.TotalCount != 1 || *p.Items[0].Rating != 4.5 {
		t.Fatalf("min_rating %d %+v", code, p)
	}

	w := post("/api/v1/creative/asset-groups", map[string]any{"name": "参考", "hierarchy_revision": "1"})
	var group struct {
		ID                string `json:"id"`
		HierarchyRevision string `json:"hierarchy_revision"`
		DeletionID        string `json:"deletion_id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &group); err != nil || w.Code != 200 {
		t.Fatalf("group %d %s", w.Code, w.Body.String())
	}
	var groups struct {
		AssetCounts map[string]int `json:"asset_counts"`
	}
	if code := get("/api/v1/creative/asset-groups", &groups); code != 200 || groups.AssetCounts[group.ID] != 0 || len(groups.AssetCounts) != 1 {
		t.Fatalf("group counts %d %+v", code, groups)
	}
	w = post("/api/v1/creative/asset-groups/"+group.ID+"/delete", map[string]any{"expected_revision": "1", "hierarchy_revision": group.HierarchyRevision})
	if err := json.Unmarshal(w.Body.Bytes(), &group); err != nil || w.Code != 200 || group.DeletionID == "" {
		t.Fatalf("delete %d %s", w.Code, w.Body.String())
	}
	if w = post("/api/v1/creative/asset-groups/deletions/"+group.DeletionID+"/restore", map[string]any{}); w.Code != 200 {
		t.Fatalf("restore %d %s", w.Code, w.Body.String())
	}
	if w = post("/api/v1/creative/asset-groups/deletions/"+group.DeletionID+"/restore", map[string]any{}); w.Code != 409 {
		t.Fatalf("second restore %d %s", w.Code, w.Body.String())
	}

	if w = post("/api/v1/creative/assets/"+ids[1]+"/trash", map[string]any{"expected_revision": "1"}); w.Code != 200 {
		t.Fatalf("trash %d %s", w.Code, w.Body.String())
	}
	if code := get("/api/v1/creative/assets?view=trash&limit=1", &p); code != 200 || p.TotalCount != 1 {
		t.Fatalf("trash page %d %+v", code, p)
	}
	if w = post("/api/v1/creative/library/empty-trash", map[string]any{"expected_library_revision": "1"}); w.Code != 409 {
		t.Fatalf("stale empty %d %s", w.Code, w.Body.String())
	}
	if w = post("/api/v1/creative/library/empty-trash", map[string]any{"expected_library_revision": p.LibraryRevision}); w.Code != 200 || !strings.Contains(w.Body.String(), `"purged_count":1`) {
		t.Fatalf("empty %d %s", w.Code, w.Body.String())
	}
	var usage struct {
		AssetCount int   `json:"asset_count"`
		LimitBytes int64 `json:"limit_bytes"`
	}
	if code := get("/api/v1/creative/library-usage", &usage); code != 200 || usage.AssetCount != 1 || usage.LimitBytes <= 0 {
		t.Fatalf("usage %d %+v", code, usage)
	}
}
