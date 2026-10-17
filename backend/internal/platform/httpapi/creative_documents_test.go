package httpapi_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativedocument"
	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
	"github.com/samson/customer-manage-platform/backend/internal/planningmedia"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

type docErr struct {
	Error struct {
		Code    string `json:"code"`
		Details struct {
			Conflicts []map[string]*string `json:"conflicts"`
		} `json:"details"`
	} `json:"error"`
}

func documentErrorCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var e docErr
	if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
		t.Fatalf("error body %s: %v", w.Body.String(), err)
	}
	return e.Error.Code
}

func sameBody(t *testing.T, a, b []byte) bool {
	t.Helper()
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		t.Fatalf("bodies %s / %s", a, b)
	}
	return reflect.DeepEqual(x, y)
}

// Every document route through routing, parameter parsing and error mapping;
// the rules behind them are covered in creativedocument.
func TestCreativeDocumentHTTP(t *testing.T) {
	ctx := context.Background()
	router, st, tokens, url := newCustomerAPIRouterWithURL(t)
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(`UPDATE accounts SET status='active' WHERE id='` + testAcctID + `'; INSERT INTO accounts(id,password_hash,status) VALUES ('acct-docs-b','test','active')`); err != nil {
		t.Fatal(err)
	}
	token, other := issueToken(t, tokens, testAcctID), issueToken(t, tokens, "acct-docs-b")
	scope := st.ScopeFor(auth.AccountContext{AccountID: testAcctID})
	body := "灯光以侧逆光为主。"
	var doc creativedocument.Document
	if err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		var err error
		doc, err = creativedocument.CreateInternalInTx(ctx, tx, "参考说明", creativecontent.Draft{Kind: "text", Payload: creativecontent.Payload{Body: &body},
			Rights: creativecontent.RightsDeclarationInput{SourceClass: planningmedia.SourcePhotographerOwned, RightsBasis: planningmedia.RightsOwnershipAttested}}, creativedocument.Photographer)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	base := "/api/v1/creative/documents/" + doc.ID

	// A replay resends the same envelope, creation time included.
	created := map[string]time.Time{}
	sendAs := func(tok, path, key string, payload any) *httptest.ResponseRecorder {
		t.Helper()
		if _, ok := created[key]; !ok {
			created[key] = time.Now().UTC()
		}
		raw, err := json.Marshal(map[string]any{"operation_id": key, "client_created_at": created[key], "payload": payload})
		if err != nil {
			t.Fatal(err)
		}
		return shootPlanningRequest(t, router, "POST", path, tok, key, string(raw))
	}
	post := func(tok, path string, payload any) *httptest.ResponseRecorder {
		t.Helper()
		return sendAs(tok, path, uuid.NewString(), payload)
	}
	get := func(tok, path string, into any) *httptest.ResponseRecorder {
		t.Helper()
		w := shootPlanningRequest(t, router, "GET", path, tok, "", "")
		if w.Code == 200 && into != nil {
			if err := json.Unmarshal(w.Body.Bytes(), into); err != nil {
				t.Fatal(err)
			}
		}
		return w
	}
	want := func(name string, w *httptest.ResponseRecorder, status int, code string) {
		t.Helper()
		if w.Code != status || code != "" && documentErrorCode(t, w) != code {
			t.Fatalf("%s: %d %s, want %d %s", name, w.Code, w.Body.String(), status, code)
		}
	}

	var read struct {
		TypeKey  string            `json:"type_key"`
		Revision string            `json:"revision"`
		UnitRevs map[string]string `json:"unit_revs"`
		Content  struct {
			Payload struct {
				Body string `json:"body"`
			} `json:"payload"`
		} `json:"content"`
	}
	want("read", get(token, base, &read), 200, "")
	if read.TypeKey != creativedocument.InternalTypeKey || read.Revision != "1" || !reflect.DeepEqual(read.UnitRevs, map[string]string{"meta": "1", "body": "1"}) || read.Content.Payload.Body != body {
		t.Fatalf("read %+v", read)
	}
	var fields map[string]json.RawMessage
	get(token, base, &fields)
	for _, key := range []string{"id", "title", "type_key", "content_revision_id", "revision", "content", "unit_revs", "trashed_at", "created_at", "updated_at"} {
		if _, ok := fields[key]; !ok {
			t.Fatalf("read lacks %q: %v", key, fields)
		}
	}
	want("read another account's", get(other, base, nil), 404, "not_found")

	// Commands, replay and every mapped failure.
	edit := map[string]any{"expected": map[string]any{"meta": "1", "body": "1"}, "operations": []any{map[string]any{"op": "rename", "title": "灯光参考说明"}, map[string]any{"op": "set_body", "text": "补一盏低位冷色。"}}}
	key := uuid.NewString()
	first := sendAs(token, base+"/commands", key, edit)
	want("command", first, 200, "")
	if !sameBody(t, first.Body.Bytes(), []byte(`{"revision":"2","changed_units":["body","meta"],"annotated_units":[],"removed_units":[],"reset_required":false}`)) {
		t.Fatalf("receipt %s", first.Body.String())
	}
	if replay := sendAs(token, base+"/commands", key, edit); replay.Code != 200 || !sameBody(t, replay.Body.Bytes(), first.Body.Bytes()) {
		t.Fatalf("replay %d %s", replay.Code, replay.Body.String())
	}
	want("same key, other payload", sendAs(token, base+"/commands", key, map[string]any{"expected": map[string]any{"body": "2"}, "operations": []any{map[string]any{"op": "set_body", "text": "x"}}}), 409, "idempotency_conflict")
	stale := post(token, base+"/commands", map[string]any{"expected": map[string]any{"body": "1"}, "operations": []any{map[string]any{"op": "set_body", "text": "x"}}})
	want("stale", stale, 409, "creative_document_conflict")
	var conflict docErr
	_ = json.Unmarshal(stale.Body.Bytes(), &conflict)
	if c := conflict.Error.Details.Conflicts; len(c) != 1 || *c[0]["unit"] != "body" || *c[0]["expected_rev"] != "1" || *c[0]["current_rev"] != "2" {
		t.Fatalf("conflict details %s", stale.Body.String())
	}
	want("restore with expected", post(token, base+"/commands", map[string]any{"expected": map[string]any{"body": "2"}, "operations": []any{map[string]any{"op": "restore", "revision": "1"}}}), 400, "validation_failed")
	want("document id in payload", post(token, base+"/commands", map[string]any{"document_id": doc.ID, "expected": map[string]any{"body": "2"}, "operations": []any{map[string]any{"op": "set_body", "text": "x"}}}), 400, "validation_failed")
	want("command on another account's", post(other, base+"/commands", map[string]any{"expected": map[string]any{"body": "2"}, "operations": []any{map[string]any{"op": "set_body", "text": "x"}}}), 404, "not_found")

	// History and old revisions.
	var revisions struct {
		Items []struct {
			Revision  string `json:"revision"`
			ActorKind string `json:"actor_kind"`
		} `json:"items"`
		NextCursor *string `json:"next_cursor"`
	}
	want("revisions", get(token, base+"/revisions", &revisions), 200, "")
	if len(revisions.Items) != 2 || revisions.Items[0].Revision != "2" || revisions.Items[0].ActorKind != "photographer" || revisions.NextCursor != nil {
		t.Fatalf("revisions %+v", revisions)
	}
	want("since 1", get(token, base+"/revisions?since_revision=1&limit=1", &revisions), 200, "")
	if len(revisions.Items) != 1 || revisions.Items[0].Revision != "2" {
		t.Fatalf("since 1 %+v", revisions)
	}
	want("other run", get(token, base+"/revisions?run_id=crun_x", &revisions), 200, "")
	if len(revisions.Items) != 0 {
		t.Fatalf("run filter %+v", revisions)
	}
	for _, q := range []string{"since_revision=0", "since_revision=01", "limit=0", "limit=101", "cursor=bad"} {
		want("revisions "+q, get(token, base+"/revisions?"+q, nil), 400, "validation_failed")
	}
	want("revisions of another account's", get(other, base+"/revisions", nil), 404, "not_found")
	var old struct {
		View     map[string]string `json:"view"`
		UnitRevs map[string]string `json:"unit_revs"`
	}
	want("old revision", get(token, base+"/revisions/1?units=body", &old), 200, "")
	if !reflect.DeepEqual(old.View, map[string]string{"body": body}) || old.UnitRevs["body"] != "1" {
		t.Fatalf("old revision %+v", old)
	}
	want("bad revision", get(token, base+"/revisions/x", nil), 400, "validation_failed")
	want("bad unit", get(token, base+"/revisions/1?units=body,", nil), 400, "validation_failed")
	missing := get(token, base+"/revisions/9", nil)
	want("missing revision", missing, 404, "not_found")
	if !strings.Contains(missing.Body.String(), "修订不存在") {
		t.Fatalf("missing revision reads as %s", missing.Body.String())
	}
	var listed struct {
		Items []map[string]json.RawMessage `json:"items"`
	}
	want("revision fields", get(token, base+"/revisions", &listed), 200, "")
	for _, key := range []string{"changed_units", "annotated_units", "removed_units"} {
		if _, ok := listed.Items[0][key]; !ok {
			t.Fatalf("revision item lacks %q: %v", key, listed.Items[0])
		}
	}

	// List.
	var list struct {
		Items []struct {
			ID      string            `json:"id"`
			Title   string            `json:"title"`
			Summary map[string]string `json:"summary"`
		} `json:"items"`
	}
	want("list", get(token, "/api/v1/creative/documents?type=internal.document", &list), 200, "")
	if len(list.Items) != 1 || list.Items[0].Title != "灯光参考说明" || list.Items[0].Summary["preview"] != "补一盏低位冷色。" {
		t.Fatalf("list %+v", list)
	}
	want("list limit", get(token, "/api/v1/creative/documents?limit=0", nil), 400, "validation_failed")
	want("list of another account", get(other, "/api/v1/creative/documents", &list), 200, "")
	if len(list.Items) != 0 {
		t.Fatalf("another account lists %+v", list)
	}

	// Conversation pointer.
	r, err := creativecanvas.CreateProject(ctx, scope, creativeops.Command{OperationID: uuid.NewString(), CreatedAt: time.Now(), Payload: json.RawMessage(`{"name":"项目"}`)})
	if err != nil {
		t.Fatal(err)
	}
	var project creativecanvas.ProjectResult
	if err := json.Unmarshal(r.Outcome.Response, &project); err != nil {
		t.Fatal(err)
	}
	var pointer struct {
		ConversationID *string `json:"conversation_id"`
	}
	want("conversation", get(token, base+"/conversation?canvas_id="+project.CanvasID, &pointer), 200, "")
	if pointer.ConversationID != nil {
		t.Fatalf("conversation %+v", pointer)
	}
	want("conversation on a missing canvas", get(token, base+"/conversation?canvas_id=missing", nil), 404, "not_found")
	if w := get(token, base+"/conversation", nil); w.Code != 400 {
		t.Fatalf("conversation without canvas %d", w.Code)
	}

	// Placing the document through the canvas command route.
	place := func(tok, canvas string) *httptest.ResponseRecorder {
		t.Helper()
		snapshot, err := creativecanvas.GetCanvas(ctx, st.ScopeFor(auth.AccountContext{AccountID: testAcctID}), project.CanvasID)
		if err != nil {
			t.Fatal(err)
		}
		return post(tok, "/api/v1/creative/canvases/"+canvas+"/commands", map[string]any{"type": "batch", "expected_topology_revision": snapshot.TopologyRevision, "read_set": []any{},
			"actions": []any{map[string]any{"type": "add_node", "node_id": "cwnode_" + uuid.NewString(), "type_key": creativedocument.InternalTypeKey, "document_id": doc.ID, "x": 0, "y": 0}}})
	}
	want("place", place(token, project.CanvasID), 200, "")

	// Trash, untrash and purge.
	want("purge before trash", post(token, base+"/purge", map[string]any{}), 409, "creative_state_conflict")
	want("trash another account's", post(other, base+"/trash", map[string]any{}), 404, "not_found")
	var trashed struct {
		TrashedAt *time.Time `json:"trashed_at"`
	}
	w := post(token, base+"/trash", map[string]any{})
	if err := json.Unmarshal(w.Body.Bytes(), &trashed); err != nil || w.Code != 200 || trashed.TrashedAt == nil {
		t.Fatalf("trash %d %s", w.Code, w.Body.String())
	}
	want("read in trash", get(token, base, nil), 200, "")
	want("command in trash", post(token, base+"/commands", map[string]any{"expected": map[string]any{"body": "2"}, "operations": []any{map[string]any{"op": "set_body", "text": "x"}}}), 409, "creative_state_conflict")
	want("place from trash", place(token, project.CanvasID), 409, "creative_state_conflict")
	w = post(token, base+"/untrash", map[string]any{})
	if err := json.Unmarshal(w.Body.Bytes(), &trashed); err != nil || w.Code != 200 || trashed.TrashedAt != nil {
		t.Fatalf("untrash %d %s", w.Code, w.Body.String())
	}
	want("trash again", post(token, base+"/trash", map[string]any{}), 200, "")
	purgeKey := uuid.NewString()
	purged := sendAs(token, base+"/purge", purgeKey, map[string]any{})
	if purged.Code != 200 || !sameBody(t, purged.Body.Bytes(), []byte(`{"id":"`+doc.ID+`","purged":true}`)) {
		t.Fatalf("purge %d %s", purged.Code, purged.Body.String())
	}
	// A retry after a lost response gets the first answer.
	if replay := sendAs(token, base+"/purge", purgeKey, map[string]any{}); replay.Code != 200 || !sameBody(t, replay.Body.Bytes(), purged.Body.Bytes()) {
		t.Fatalf("purge replay %d %s", replay.Code, replay.Body.String())
	}
	want("read purged", get(token, base, nil), 404, "not_found")
	want("purge again", post(token, base+"/purge", map[string]any{}), 404, "not_found")

	// Creative capabilities follow the account status: an inactive account
	// can neither read nor write documents.
	if _, err := db.Exec(`UPDATE accounts SET status='pending_verification' WHERE id='` + testAcctID + `'`); err != nil {
		t.Fatal(err)
	}
	want("list when inactive", get(token, "/api/v1/creative/documents", nil), 403, "forbidden")
	for _, path := range []string{"/commands", "/trash", "/untrash", "/purge"} {
		want(path+" when inactive", post(token, base+path, map[string]any{}), 403, "forbidden")
	}
}

// TestCreativeSceneDocumentHTTP walks the scene branch through the generic
// document routes: read, commands, conflict details, isolation, trash and the
// size limit. Scene rules themselves are covered in creativescene.
func TestCreativeSceneDocumentHTTP(t *testing.T) {
	ctx := context.Background()
	router, st, tokens, url := newCustomerAPIRouterWithURL(t)
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(`UPDATE accounts SET status='active' WHERE id='` + testAcctID + `'; INSERT INTO accounts(id,password_hash,status) VALUES ('acct-scene-b','test','active')`); err != nil {
		t.Fatal(err)
	}
	token, other := issueToken(t, tokens, testAcctID), issueToken(t, tokens, "acct-scene-b")
	scope := st.ScopeFor(auth.AccountContext{AccountID: testAcctID})
	typ, ok := creativedocument.Lookup(creativescene.TypeKey)
	if !ok {
		t.Fatal("scene type is not registered")
	}
	var doc creativedocument.Document
	if err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		var err error
		doc, err = creativedocument.CreateInTx(ctx, tx, typ, "黄昏教堂", nil, creativedocument.Photographer)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	base := "/api/v1/creative/documents/" + doc.ID
	post := func(tok, path string, payload any) *httptest.ResponseRecorder {
		t.Helper()
		key := uuid.NewString()
		raw, err := json.Marshal(map[string]any{"operation_id": key, "client_created_at": time.Now().UTC(), "payload": payload})
		if err != nil {
			t.Fatal(err)
		}
		return shootPlanningRequest(t, router, "POST", path, tok, key, string(raw))
	}
	want := func(name string, w *httptest.ResponseRecorder, status int, code string) {
		t.Helper()
		if w.Code != status || code != "" && documentErrorCode(t, w) != code {
			t.Fatalf("%s: %d %s, want %d %s", name, w.Code, w.Body.String(), status, code)
		}
	}
	type sceneRead struct {
		TypeKey   string            `json:"type_key"`
		Revision  string            `json:"revision"`
		UnitRevs  map[string]string `json:"unit_revs"`
		TrashedAt *time.Time        `json:"trashed_at"`
		Scene     struct {
			Objects []struct {
				ID        string `json:"id"`
				Primitive struct {
					Builtin string `json:"builtin"`
				} `json:"primitive"`
			} `json:"objects"`
		} `json:"scene"`
		Models map[string]any `json:"models"`
	}
	read := func(tok string) (sceneRead, *httptest.ResponseRecorder) {
		t.Helper()
		var v sceneRead
		w := shootPlanningRequest(t, router, "GET", base, tok, "", "")
		if w.Code == 200 {
			if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
				t.Fatal(err)
			}
		}
		return v, w
	}

	v, w := read(token)
	want("read", w, 200, "")
	if v.TypeKey != "scene" || v.Revision != "1" || len(v.Scene.Objects) != 1 || v.Scene.Objects[0].Primitive.Builtin != "ground@1" || v.Models == nil {
		t.Fatalf("read %s", w.Body.String())
	}
	_, w = read(other)
	want("read another account's", w, 404, "not_found")

	a := "sobj_A00000000000000000001"
	boxObject := func(id string, order int) map[string]any {
		return map[string]any{"id": id, "kind": "primitive", "name": "石台", "parent_id": nil, "order": order,
			"transform": map[string]any{"position": []float64{0, 0, -3}, "rotation": []float64{0, 0, 0, 1}, "scale": []float64{1, 1, 1}},
			"visible":   true, "locked": false, "primitive": map[string]any{"builtin": "box@1"}}
	}
	w = post(token, base+"/commands", map[string]any{"expected": map[string]any{"objects": "1"}, "operations": []any{map[string]any{"op": "add_objects", "objects": []any{boxObject(a, 1)}}}})
	want("add", w, 200, "")
	if !sameBody(t, w.Body.Bytes(), []byte(`{"revision":"2","changed_units":["object:`+a+`","object:`+a+`:content","objects"],"annotated_units":[],"removed_units":[],"reset_required":false}`)) {
		t.Fatalf("add receipt %s", w.Body.String())
	}
	move := func(expected string) map[string]any {
		return map[string]any{"expected": map[string]any{"object:" + a: expected}, "operations": []any{map[string]any{"op": "update_objects", "updates": []any{
			map[string]any{"object_id": a, "transform": map[string]any{"position": []float64{1, 0, -3}, "rotation": []float64{0, 0, 0, 1}, "scale": []float64{1, 1, 1}}}}}}}
	}
	want("move", post(token, base+"/commands", move("2")), 200, "")
	w = post(token, base+"/commands", move("2"))
	want("stale move", w, 409, "creative_document_conflict")
	var e docErr
	if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil || len(e.Error.Details.Conflicts) != 1 || *e.Error.Details.Conflicts[0]["unit"] != "object:"+a || *e.Error.Details.Conflicts[0]["current_rev"] != "3" {
		t.Fatalf("conflict details %s", w.Body.String())
	}
	missing := "sobj_Z00000000000000000009"
	want("missing object is not a missing document", post(token, base+"/commands", map[string]any{"expected": map[string]any{"object:" + missing: nil},
		"operations": []any{map[string]any{"op": "update_objects", "updates": []any{map[string]any{"object_id": missing, "visible": false}}}}}), 400, "validation_failed")
	want("command on another account's", post(other, base+"/commands", move("3")), 404, "not_found")

	cameras := make([]any, 33)
	for i := range cameras {
		c := boxObject(fmt.Sprintf("sobj_C%020d", i), 10+i)
		c["kind"] = "camera"
		delete(c, "primitive")
		c["camera"] = map[string]any{"focal_mm": 35, "sensor_mm": []float64{36, 24}, "aspect": "3:2", "near_m": 0.1, "far_m": 1000, "label": ""}
		cameras[i] = c
	}
	want("too many cameras", post(token, base+"/commands", map[string]any{"expected": map[string]any{"objects": "2"}, "operations": []any{map[string]any{"op": "add_objects", "objects": cameras}}}), 413, "creative_size_limit")

	want("trash", post(token, base+"/trash", map[string]any{}), 200, "")
	v, w = read(token)
	want("read in trash", w, 200, "")
	if v.TrashedAt == nil {
		t.Fatalf("trashed read %s", w.Body.String())
	}
	want("command in trash", post(token, base+"/commands", move("3")), 409, "creative_state_conflict")
	want("untrash", post(token, base+"/untrash", map[string]any{}), 200, "")
	want("command after untrash", post(token, base+"/commands", move("3")), 200, "")
}
