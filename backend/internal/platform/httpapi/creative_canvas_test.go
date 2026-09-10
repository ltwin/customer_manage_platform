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

func TestCreativeCanvasHTTPPersistenceAndEnvelope(t *testing.T) {
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
	capabilities := shootPlanningRequest(t, router, "GET", "/api/v1/creative/capabilities", token, "", "")
	if capabilities.Code != 200 || !strings.Contains(capabilities.Body.String(), `"available":true`) {
		t.Fatalf("active account without enrollment: %d %s", capabilities.Code, capabilities.Body.String())
	}
	for _, path := range []string{"/api/v1/creative-workspaces", "/api/v1/creative-pilot"} {
		gone := shootPlanningRequest(t, router, "GET", path, token, "", "")
		if gone.Code != 404 {
			t.Fatalf("retired endpoint %s: %d", path, gone.Code)
		}
	}

	request := func(path string, payload any) (*httptest.ResponseRecorder, string, string) {
		t.Helper()
		key := uuid.NewString()
		body, err := json.Marshal(map[string]any{"operation_id": key, "client_created_at": time.Now().UTC(), "payload": payload})
		if err != nil {
			t.Fatal(err)
		}
		return shootPlanningRequest(t, router, "POST", path, token, key, string(body)), string(body), key
	}
	p, body, key := request("/api/v1/creative/projects", map[string]any{"name": "HTTP创作"})
	if p.Code != 201 {
		t.Fatalf("create %d %s", p.Code, p.Body.String())
	}
	var project struct {
		ID       string `json:"project_id"`
		CanvasID string `json:"default_canvas_id"`
	}
	if err := json.Unmarshal(p.Body.Bytes(), &project); err != nil {
		t.Fatal(err)
	}
	replay := shootPlanningRequest(t, router, "POST", "/api/v1/creative/projects", token, key, body)
	if replay.Code != 201 || !sameJSON(replay.Body.Bytes(), p.Body.Bytes()) {
		t.Fatalf("replay %d %s", replay.Code, replay.Body.String())
	}
	bad := shootPlanningRequest(t, router, "POST", "/api/v1/creative/projects", token, uuid.NewString(), body)
	if bad.Code != 400 {
		t.Fatalf("mismatch header %d", bad.Code)
	}

	for _, header := range []string{"", "not-a-uuid"} {
		malformed := shootPlanningRequest(t, router, "POST", "/api/v1/creative/projects", token, header, body)
		if malformed.Code != 400 {
			t.Fatalf("invalid header %q: %d", header, malformed.Code)
		}
	}
	nodeID := "cwnode_" + uuid.NewString()
	n, _, _ := request("/api/v1/creative/canvases/"+project.CanvasID+"/commands", map[string]any{"type": "add_node", "node_id": nodeID, "type_key": "core.text", "x": 10, "y": 20, "expected_topology_revision": "1", "content": map[string]any{"kind": "text", "payload": map[string]any{"body": "原始文字"}, "rights": map[string]any{"source_class": "photographer_owned", "rights_basis": "ownership_attested"}}})
	if n.Code != 200 {
		t.Fatalf("node %d %s", n.Code, n.Body.String())
	}
	snapshot := shootPlanningRequest(t, router, "GET", "/api/v1/creative/canvases/"+project.CanvasID, token, "", "")
	if snapshot.Code != 200 {
		t.Fatalf("snapshot %d %s", snapshot.Code, snapshot.Body.String())
	}

	// Invalid coordinates must not turn into an accidental move or new origin node.
	for _, kind := range []string{"move_node", "add_node"} {
		for _, field := range []string{"x", "y"} {
			for _, variant := range []string{"missing", "null"} {
				input := map[string]any{"type": kind, "node_id": nodeID, "x": 100, "y": 200}
				if kind == "move_node" {
					input["expected_placement_revision"] = "1"
				} else {
					input["node_id"] = "cwnode_" + uuid.NewString()
					input["type_key"] = "core.text"
					input["expected_topology_revision"] = "2"
				}
				if variant == "null" {
					input[field] = nil
				} else {
					delete(input, field)
				}
				invalid, _, operation := request("/api/v1/creative/canvases/"+project.CanvasID+"/commands", input)
				if invalid.Code != 400 {
					t.Fatalf("%s %s %s accepted: %d %s", kind, variant, field, invalid.Code, invalid.Body.String())
				}
				receipt := shootPlanningRequest(t, router, "GET", "/api/v1/creative/operations/"+operation, token, "", "")
				if receipt.Code != 404 {
					t.Fatalf("invalid command retained receipt: %d", receipt.Code)
				}
			}
		}
	}
	missingPointer, _, _ := request("/api/v1/creative/canvases/"+project.CanvasID+"/commands", map[string]any{"type": "replace_content", "node_id": nodeID, "expected_data_revision": "1", "payload": map[string]any{"body": "遗漏当前修订"}})
	if missingPointer.Code != 400 {
		t.Fatalf("missing nullable required pointer %d", missingPointer.Code)
	}
	var before struct {
		Nodes []struct {
			ContentRevisionID string  `json:"content_revision_id"`
			X                 float64 `json:"x"`
			DataRevision      string  `json:"data_revision"`
			PlacementRevision string  `json:"placement_revision"`
		} `json:"nodes"`
	}
	current := shootPlanningRequest(t, router, "GET", "/api/v1/creative/canvases/"+project.CanvasID, token, "", "")
	if err := json.Unmarshal(current.Body.Bytes(), &before); err != nil {
		t.Fatal(err)
	}
	if len(before.Nodes) != 1 || before.Nodes[0].X != 10 || before.Nodes[0].PlacementRevision != "1" {
		t.Fatal("invalid move mutated state")
	}
	oldRevision := before.Nodes[0].ContentRevisionID
	edited, _, _ := request("/api/v1/creative/canvases/"+project.CanvasID+"/commands", map[string]any{"type": "replace_content", "node_id": nodeID, "expected_data_revision": "1", "expected_content_revision_id": oldRevision, "payload": map[string]any{"body": "新正文"}})
	if edited.Code != 200 {
		t.Fatalf("edit %d %s", edited.Code, edited.Body.String())
	}
	stale := shootPlanningRequest(t, router, "GET", "/api/v1/creative/content-revisions/"+oldRevision, token, "", "")
	if stale.Code != 404 {
		t.Fatalf("stale revision %d %s", stale.Code, stale.Body.String())
	}
	for _, length := range []int{500, 501, 2000} {
		asset, _, _ := request("/api/v1/creative/assets", map[string]any{"title": "来源说明", "content": map[string]any{"kind": "text", "payload": map[string]any{"body": "正文"}, "rights": map[string]any{"source_class": "photographer_owned", "rights_basis": "ownership_attested", "evidence_summary": strings.Repeat("证", length)}}})
		want := 400
		if length == 500 {
			want = 201
		}
		if asset.Code != want {
			t.Fatalf("evidence %d: %d %s", length, asset.Code, asset.Body.String())
		}
	}
	extra, _, _ := request("/api/v1/creative/assets", map[string]any{"title": "多余授权字段", "content": map[string]any{"kind": "text", "payload": map[string]any{"body": "正文"}, "rights": map[string]any{"source_class": "photographer_owned", "rights_basis": "ownership_attested", "license_generation_reference_granted": true}}})
	if extra.Code != 400 {
		t.Fatalf("extra rights accepted %d %s", extra.Code, extra.Body.String())
	}
	archive, _, _ := request("/api/v1/creative/projects/"+project.ID+"/archive", map[string]any{"expected_revision": "1"})
	if archive.Code != 200 {
		t.Fatalf("archive %d %s", archive.Code, archive.Body.String())
	}
	move, _, _ := request("/api/v1/creative/canvases/"+project.CanvasID+"/commands", map[string]any{"type": "move_node", "node_id": nodeID, "x": 40, "y": 50, "expected_placement_revision": "1"})
	if move.Code != 409 {
		t.Fatalf("archived write %d %s", move.Code, move.Body.String())
	}
	poisoned, _, _ := request("/api/v1/creative/projects", map[string]any{"name": "bad", "account_id": "another-account"})
	if poisoned.Code != 400 {
		t.Fatalf("unknown field %d", poisoned.Code)
	}
	unauth := shootPlanningRequest(t, router, "GET", "/api/v1/creative/assets", "", "", "")
	if unauth.Code != 401 {
		t.Fatalf("unauth %d", unauth.Code)
	}
}

func sameJSON(a, b []byte) bool {
	var left, right any
	if json.Unmarshal(a, &left) != nil || json.Unmarshal(b, &right) != nil {
		return false
	}
	l, err := json.Marshal(left)
	if err != nil {
		return false
	}
	r, err := json.Marshal(right)
	return err == nil && string(l) == string(r)
}
