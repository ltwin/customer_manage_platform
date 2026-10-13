package dbtest

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/creativeimageedit"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/httpapi"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func TestImageEditHTTPRoundtripAndBoundaries(t *testing.T) {
	f := setup(t)
	tokens := auth.NewTokenIssuer("image-edit-test-secret")
	token, err := tokens.Issue("edit-a")
	if err != nil {
		t.Fatal(err)
	}
	deps := httpapi.RouterDeps{Logger: slog.New(slog.DiscardHandler), DB: f.st, ScopeFactory: f.st, Auth: auth.NewService(f.st, tokens), CreativeMedia: f.media, CreativeImageEdit: f.svc}
	router := httpapi.NewRouter(deps)
	request := func(router http.Handler, method, path, key, body, token string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, "/api/v1/creative"+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if key != "" {
			req.Header.Set("Idempotency-Key", key)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	check := func(w *httptest.ResponseRecorder, status int) {
		t.Helper()
		if w.Code != status {
			t.Fatalf("status %d want %d: %s", w.Code, status, w.Body.String())
		}
	}
	check(request(router, "GET", "/image-edit-capabilities", "", "", ""), 401)
	caps := request(router, "GET", "/image-edit-capabilities", "", "", token)
	check(caps, 200)
	if !strings.Contains(caps.Body.String(), `"available":true`) {
		t.Fatal(caps.Body.String())
	}
	if err := f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		if err := tx.LockCreativeWrite(t.Context()); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		defer cancel()
		req := httptest.NewRequestWithContext(ctx, "GET", "/api/v1/creative/image-edit-capabilities", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != 200 {
			return fmt.Errorf("capabilities blocked by writer: %d %s", w.Code, w.Body.String())
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	deps.CreativeImageEdit = nil
	off := httpapi.NewRouter(deps)
	caps = request(off, "GET", "/image-edit-capabilities", "", "", token)
	check(caps, 200)
	if !strings.Contains(caps.Body.String(), `"available":false`) {
		t.Fatal(caps.Body.String())
	}
	check(request(off, "POST", "/image-edit-renders", uuid.NewString(), "{}", token), 503)
	node, source := f.imageNode(t, "http")
	id := uuid.NewString()
	body := fmt.Sprintf(`{"operation_id":%q,"canvas_id":%q,"node_id":%q,"source_revision_id":%q,"action":"compress","parameters":{"quality":60}}`, id, f.canvas, node, source)
	check(request(router, "POST", "/image-edit-renders", uuid.NewString(), body, token), 400)
	check(request(router, "POST", "/image-edit-renders", id, strings.TrimSuffix(body, "}")+`,"caller_kind":"agent"}`, token), 400)
	cropMissing := strings.Replace(body, `"compress","parameters":{"quality":60}`, `"crop","parameters":{"width":32,"height":32}`, 1)
	check(request(router, "POST", "/image-edit-renders", id, cropMissing, token), 400)
	huge := strings.Replace(body, `"compress","parameters":{"quality":60}`, `"crop","parameters":{"left":9223372036854775807,"top":0,"width":2,"height":2}`, 1)
	check(request(router, "POST", "/image-edit-renders", id, huge, token), 400)
	w := request(router, "POST", "/image-edit-renders", id, body, token)
	check(w, 202)
	var r creativeimageedit.Render
	if err = json.Unmarshal(w.Body.Bytes(), &r); err != nil {
		t.Fatal(err)
	}
	replay := request(router, "POST", "/image-edit-renders", id, body, token)
	check(replay, 202)
	var rr creativeimageedit.Render
	if err = json.Unmarshal(replay.Body.Bytes(), &rr); err != nil {
		t.Fatal(err)
	}
	if rr.ID != r.ID {
		t.Fatal("create retry created new render")
	}
	busyID := uuid.NewString()
	busy := request(router, "POST", "/image-edit-renders", busyID, strings.Replace(body, id, busyID, 1), token)
	check(busy, 409)
	if !strings.Contains(busy.Body.String(), `"render_id":"`+r.ID+`"`) {
		t.Fatal("missing busy render id", busy.Body.String())
	}
	cmd := f.applyCommand(t, r)
	var payload map[string]any
	if err = json.Unmarshal(cmd.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	delete(payload, "canvas_id")
	delete(payload, "node_id")
	envelope := map[string]any{"operation_id": cmd.OperationID, "client_created_at": cmd.CreatedAt.UTC().Format(time.RFC3339Nano), "payload": payload}
	encode := func() string {
		b, e := json.Marshal(envelope)
		if e != nil {
			t.Fatal(e)
		}
		return string(b)
	}
	path := "/canvases/" + f.canvas + "/nodes/" + node + "/image-edits/apply"
	check(request(router, "POST", path, cmd.OperationID, encode(), token), 409)
	if err = f.work(t); err != nil {
		t.Fatal(err)
	}
	check(request(router, "GET", "/image-edit-renders/"+r.ID, "", "", token), 200)
	canvas, e := creativecanvas.GetCanvas(t.Context(), f.a, f.canvas)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = creativecanvas.ApplyCommands(t.Context(), f.a, command(t, creativecanvas.BatchInput{CanvasID: f.canvas, ReadSet: creativecanvas.ReadSetFor(canvas), Actions: []creativecanvas.Action{{Type: "update_metadata", NodeID: node, Title: "更新标题"}}})); e != nil {
		t.Fatal(e)
	}
	conflict := request(router, "POST", path, cmd.OperationID, encode(), token)
	check(conflict, 409)
	if !strings.Contains(conflict.Body.String(), `"code":"creative_revision_conflict"`) {
		t.Fatal(conflict.Body.String())
	}
	var currentData int64
	if err = f.db.QueryRow(`SELECT data_revision FROM creative_nodes WHERE id=$1`, node).Scan(&currentData); err != nil {
		t.Fatal(err)
	}
	payload["expected_data_revision"] = fmt.Sprint(currentData)
	payload["node_id"] = node
	check(request(router, "POST", path, cmd.OperationID, encode(), token), 400)
	delete(payload, "node_id")
	execSQL(t, f, `INSERT INTO accounts(id,password_hash,status) VALUES ('edit-b','test','active')`)
	otherToken, err := tokens.Issue("edit-b")
	if err != nil {
		t.Fatal(err)
	}
	check(request(router, "GET", "/image-edit-renders/"+r.ID, "", "", otherToken), 404)
	check(request(router, "POST", "/image-edit-renders/"+r.ID+"/discard", "", "", otherToken), 404)
	check(request(router, "POST", path, cmd.OperationID, encode(), otherToken), 404)
	w = request(router, "POST", path, cmd.OperationID, encode(), token)
	check(w, 200)
	var result creativeimageedit.ApplyResult
	if err = json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	w = request(router, "POST", path, cmd.OperationID, encode(), token)
	check(w, 200)
	var repeated creativeimageedit.ApplyResult
	if err = json.Unmarshal(w.Body.Bytes(), &repeated); err != nil {
		t.Fatal(err)
	}
	if result.VersionID == "" || result.VersionID != repeated.VersionID {
		t.Fatal("apply not replayed")
	}
	payload["render_id"] = "another"
	check(request(router, "POST", path, cmd.OperationID, encode(), token), 409)
	w = request(router, "POST", "/image-edit-renders/"+r.ID+"/discard", "", "", token)
	check(w, 200)
	if !strings.Contains(w.Body.String(), `"state":"applied"`) {
		t.Fatal("discard undid applied edit")
	}
	// Discard is idempotent, and an abandoned candidate maps to HTTP 410.
	node2, source2 := f.imageNode(t, "discard-http")
	r2, err := f.create(t, node2, source2, 60)
	if err != nil {
		t.Fatal(err)
	}
	discardPath := "/image-edit-renders/" + r2.ID + "/discard"
	check(request(router, "POST", discardPath, "", "", token), 200)
	check(request(router, "POST", discardPath, "", "", token), 200)
	cmd = f.applyCommand(t, r2)
	envelope["operation_id"] = cmd.OperationID
	payload["render_id"] = r2.ID
	payload["expected_source_revision_id"] = source2
	payload["expected_data_revision"] = "1"
	check(request(router, "POST", "/canvases/"+f.canvas+"/nodes/"+node2+"/image-edits/apply", cmd.OperationID, encode(), token), 410)
}
