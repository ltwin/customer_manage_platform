package httpapi_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/samson/customer-manage-platform/backend/internal/creativedocument"
	"github.com/samson/customer-manage-platform/backend/internal/creativeglb"
	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func TestCreativeSceneModelSaveToLibraryHTTP(t *testing.T) {
	ctx := context.Background()
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
	typ, _ := creativedocument.Lookup(creativescene.TypeKey)
	var doc creativedocument.Document
	if err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		var err error
		doc, err = creativedocument.CreateInTx(ctx, tx, typ, "柱廊", nil, creativedocument.Photographer)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	glb, err := os.ReadFile(filepath.Join("..", "..", "creativeglb", "testdata", "basic.glb"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := creativeglb.Inspect(glb, creativeglb.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	post := func(path string, payload any) *httptest.ResponseRecorder {
		t.Helper()
		key := uuid.NewString()
		raw, err := json.Marshal(map[string]any{"operation_id": key, "client_created_at": time.Now().UTC(), "payload": payload})
		if err != nil {
			t.Fatal(err)
		}
		return shootPlanningRequest(t, router, "POST", path, token, key, string(raw))
	}
	want := func(name string, w *httptest.ResponseRecorder, status int, code string) {
		t.Helper()
		if w.Code != status || code != "" && documentErrorCode(t, w) != code {
			t.Fatalf("%s: %d %s, want %d %s", name, w.Code, w.Body.String(), status, code)
		}
	}

	// A model instance and a box in one scene.
	code := "export default function build({ THREE }) { return new THREE.Group() }"
	sum := sha256.Sum256([]byte(code))
	generator := map[string]any{"language": "three-module", "contract_version": 1, "lib_version": 1, "code": code,
		"code_sha256": "sha256-" + hex.EncodeToString(sum[:]), "params_schema": map[string]any{}, "params": map[string]any{}, "mount": "floor"}
	id, boxID := "sobj_M00000000000000000001", "sobj_B00000000000000000001"
	object := func(id, kind string, order int, extra map[string]any) map[string]any {
		o := map[string]any{"id": id, "kind": kind, "name": "物件", "parent_id": nil, "order": order,
			"transform": map[string]any{"position": []float64{0, 0, 0}, "rotation": []float64{0, 0, 0, 1}, "scale": []float64{1, 1, 1}},
			"visible":   true, "locked": false, "material_slots": map[string]any{}}
		for k, v := range extra {
			o[k] = v
		}
		return o
	}
	w := post("/api/v1/creative/documents/"+doc.ID+"/commands", map[string]any{"expected": map[string]any{"objects": "1"}, "operations": []any{
		map[string]any{"op": "commit_model", "ref": "pillar", "glb_upload_id": stageForHTTP(t, db, scope, "model", manifest), "thumbnail_upload_id": stageForHTTP(t, db, scope, "image", manifest),
			"generator": generator, "manifest": manifest, "name_hint": "立柱"},
		map[string]any{"op": "add_objects", "objects": []any{
			object(id, "model_instance", 1, map[string]any{"model": map[string]any{"ref": "pillar"}}),
			object(boxID, "primitive", 2, map[string]any{"primitive": map[string]any{"builtin": "box@1"}}),
		}},
	}})
	want("commit and place", w, 200, "")
	var receipt struct {
		Models map[string]struct {
			ContentRevisionID string `json:"content_revision_id"`
		} `json:"models"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &receipt); err != nil {
		t.Fatal(err)
	}
	rev := receipt.Models["pillar"].ContentRevisionID
	read := shootPlanningRequest(t, router, "GET", "/api/v1/creative/documents/"+doc.ID, token, "", "")
	var scene struct {
		UnitRevs map[string]string `json:"unit_revs"`
	}
	if err := json.Unmarshal(read.Body.Bytes(), &scene); err != nil {
		t.Fatal(err)
	}
	unitRev := scene.UnitRevs["object:"+id+":content"]

	save := func(change func(map[string]any)) *httptest.ResponseRecorder {
		t.Helper()
		p := map[string]any{"document_id": doc.ID, "object_id": id, "expected_object_rev": unitRev, "target": map[string]any{"title": "哥特束柱"}}
		change(p)
		return post("/api/v1/creative/assets/from-scene-model", p)
	}
	keep := func(map[string]any) {}

	stale := save(func(p map[string]any) { p["expected_object_rev"] = "99" })
	want("stale rev", stale, 409, "creative_document_conflict")
	if !strings.Contains(stale.Body.String(), `"unit":"object:`+id+`:content"`) {
		t.Fatalf("conflict details %s", stale.Body.String())
	}
	want("not a model instance", save(func(p map[string]any) {
		p["object_id"], p["expected_object_rev"] = boxID, scene.UnitRevs["object:"+boxID+":content"]
	}), 400, "validation_failed")
	want("missing object", save(func(p map[string]any) { p["object_id"] = "sobj_X00000000000000000001" }), 404, "not_found")
	want("missing group", save(func(p map[string]any) {
		p["target"] = map[string]any{"title": "柱", "group_ids": []string{"cagr_missing"}}
	}), 404, "not_found")

	created := save(keep)
	want("save", created, 201, "")
	var asset struct {
		ID                string `json:"asset_id"`
		ContentRevisionID string `json:"content_revision_id"`
		Reused            bool   `json:"reused"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &asset); err != nil || asset.ContentRevisionID != rev || asset.Reused {
		t.Fatalf("created %s", created.Body.String())
	}
	again := save(keep)
	want("save again", again, 200, "")
	if !strings.Contains(again.Body.String(), `"reused":true`) || !strings.Contains(again.Body.String(), asset.ID) {
		t.Fatalf("reused %s", again.Body.String())
	}

	// Reading the asset gives the model without its code.
	got := shootPlanningRequest(t, router, "GET", "/api/v1/creative/assets/"+asset.ID, token, "", "")
	want("read asset", got, 200, "")
	if !strings.Contains(got.Body.String(), `"kind":"model"`) || !strings.Contains(got.Body.String(), "code_sha256") || strings.Contains(got.Body.String(), "THREE.Group") {
		t.Fatalf("asset read %s", got.Body.String())
	}
	list := shootPlanningRequest(t, router, "GET", "/api/v1/creative/assets?kind=model", token, "", "")
	want("list models", list, 200, "")
	if !strings.Contains(list.Body.String(), asset.ID) || strings.Contains(list.Body.String(), "THREE.Group") {
		t.Fatalf("asset list %s", list.Body.String())
	}

	if _, err := db.Exec(`UPDATE creative_content_revisions SET payload = payload #- '{model,generator}' WHERE id=$1`, rev); err != nil {
		t.Fatal(err)
	}
	want("staged form", save(keep), 400, "creative_model_unavailable")
	raw, _ := json.Marshal(map[string]string{"document_id": doc.ID})
	if _, err := creativedocument.Trash(ctx, scope, creativeops.Command{OperationID: uuid.NewString(), CreatedAt: time.Now(), Payload: raw}); err != nil {
		t.Fatal(err)
	}
	want("trashed scene", save(keep), 409, "creative_state_conflict")
}
