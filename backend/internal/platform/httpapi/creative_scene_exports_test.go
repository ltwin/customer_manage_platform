package httpapi_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/creativedocument"
	"github.com/samson/customer-manage-platform/backend/internal/creativeglb"
	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
)

func creativeCommand(t *testing.T, payload any) creativeops.Command {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return creativeops.Command{OperationID: uuid.NewString(), CreatedAt: time.Now(), Payload: raw}
}

// The export endpoint's error codes follow the canvas and staging ones
// (api: 导出分镜).
func TestCreativeSceneExportHTTP(t *testing.T) {
	ctx := context.Background()
	// cmd/server registers the freshness check at start-up; the test router does not.
	creativecanvas.SetExportFreshness(creativescene.Freshness{})
	t.Cleanup(func() { creativecanvas.SetExportFreshness(nil) })
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

	r, err := creativecanvas.CreateProject(ctx, scope, creativeCommand(t, creativecanvas.CreateProjectInput{Name: "导出"}))
	if err != nil {
		t.Fatal(err)
	}
	var p creativecanvas.ProjectResult
	if err := json.Unmarshal(r.Outcome.Response, &p); err != nil {
		t.Fatal(err)
	}
	canvas, err := creativecanvas.GetCanvas(ctx, scope, p.CanvasID)
	if err != nil {
		t.Fatal(err)
	}
	node := "cwnode_" + uuid.NewString()
	if _, err := creativecanvas.ApplyCommands(ctx, scope, creativeCommand(t, creativecanvas.BatchInput{CanvasID: p.CanvasID, ExpectedTopologyRevision: canvas.TopologyRevision,
		ReadSet: creativecanvas.ReadSetFor(canvas), Actions: []creativecanvas.Action{{Type: "add_node", NodeID: node, TypeKey: creativescene.NodeTypeKey}}})); err != nil {
		t.Fatal(err)
	}
	canvas, err = creativecanvas.GetCanvas(ctx, scope, p.CanvasID)
	if err != nil {
		t.Fatal(err)
	}
	var doc string
	for _, n := range canvas.Nodes {
		if n.ID == node {
			doc = *n.DocumentID
		}
	}
	cam := "sobj_C00000000000000000001"
	if _, err := creativedocument.Command(ctx, scope, creativeCommand(t, map[string]any{"document_id": doc, "expected": map[string]any{"objects": "1"},
		"operations": []any{map[string]any{"op": "add_objects", "objects": []any{map[string]any{"id": cam, "kind": "camera", "name": "", "parent_id": nil, "order": 1,
			"transform": map[string]any{"position": []float64{0, 1, 5}, "rotation": []float64{0, 0, 0, 1}, "scale": []float64{1, 1, 1}},
			"visible":   true, "locked": false, "camera": map[string]any{}}}}}})); err != nil {
		t.Fatal(err)
	}

	post := func(payload any) *httptest.ResponseRecorder {
		t.Helper()
		key := uuid.NewString()
		raw, err := json.Marshal(map[string]any{"operation_id": key, "client_created_at": time.Now().UTC(), "payload": payload})
		if err != nil {
			t.Fatal(err)
		}
		return shootPlanningRequest(t, router, "POST", "/api/v1/creative/documents/"+doc+"/scene-exports", token, key, string(raw))
	}
	want := func(name string, w *httptest.ResponseRecorder, status int, code string) {
		t.Helper()
		if w.Code != status || code != "" && documentErrorCode(t, w) != code {
			t.Fatalf("%s: %d %s, want %d %s", name, w.Code, w.Body.String(), status, code)
		}
	}
	payload := func(upload string, shots ...any) map[string]any {
		if len(shots) == 0 {
			shots = []any{map[string]any{"camera_object_id": cam, "images": []any{map[string]any{"pass": "clay", "upload_id": upload}}}}
		}
		return map[string]any{"canvas_id": p.CanvasID, "scene_node_id": node, "scene_revision": "2", "shots": shots}
	}
	image := stageForHTTP(t, db, scope, "image", creativeglb.Manifest{})
	w := post(payload(image))
	want("export", w, 200, "")
	var res struct {
		ChangeID      string `json:"change_id"`
		SceneRevision string `json:"scene_revision"`
		Nodes         []struct {
			NodeID string `json:"node_id"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil || res.ChangeID == "" || res.SceneRevision != "2" || len(res.Nodes) != 1 {
		t.Fatalf("export result %s", w.Body.String())
	}

	read := shootPlanningRequest(t, router, "GET", "/api/v1/creative/canvases/"+p.CanvasID, token, "", "")
	want("canvas read", read, 200, "")
	var snap struct {
		Nodes []struct {
			ID           string `json:"id"`
			BasedOnState string `json:"based_on_state"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(read.Body.Bytes(), &snap); err != nil {
		t.Fatal(err)
	}
	for _, n := range snap.Nodes {
		if n.ID == res.Nodes[0].NodeID && n.BasedOnState != "current" || n.ID == node && n.BasedOnState != "" {
			t.Fatalf("node %s based_on_state %q", n.ID, n.BasedOnState)
		}
	}

	want("consumed upload", post(payload(image)), 409, "creative_upload_unavailable")
	many := []any{}
	for i := range 17 {
		many = append(many, map[string]any{"camera_object_id": "sobj_C0000000000000000010" + string(rune('a'+i)), "images": []any{
			map[string]any{"pass": "clay", "upload_id": "a"}, map[string]any{"pass": "line", "upload_id": "b"}, map[string]any{"pass": "lit", "upload_id": "c"}}})
	}
	want("49 images", post(payload("", many...)), 413, "creative_size_limit")
	want("camera missing", post(payload("u", map[string]any{"camera_object_id": "sobj_C00000000000000000009", "images": []any{map[string]any{"pass": "clay", "upload_id": "u"}}})), 400, "validation_failed")
	if _, err := db.Exec(`UPDATE creative_projects SET archived_at=clock_timestamp() WHERE id=$1`, p.ProjectID); err != nil {
		t.Fatal(err)
	}
	want("archived", post(payload(stageForHTTP(t, db, scope, "image", creativeglb.Manifest{}))), 409, "archived_read_only")
}
