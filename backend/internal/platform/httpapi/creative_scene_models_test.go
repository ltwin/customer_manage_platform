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

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativedocument"
	"github.com/samson/customer-manage-platform/backend/internal/creativeglb"
	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// stageForHTTP leaves a staged upload as the media worker would: a model in
// its staged form (server manifest of basic.glb) or a PNG thumbnail.
func stageForHTTP(t *testing.T, db *sql.DB, scope store.AccountScope, kind string, manifest creativeglb.Manifest) string {
	t.Helper()
	blob, upload := "ccbl_"+uuid.NewString(), "ccup_"+uuid.NewString()
	mime := map[string]string{"model": "model/gltf-binary", "image": "image/png"}[kind]
	if _, err := db.Exec(`INSERT INTO creative_blobs(id,account_id,storage_driver,object_key,object_version,sha256,byte_size,mime,state,verified_at)
 VALUES($1,$2,'local','creative-v2/'||$2||'/blobs/'||$1,'v1','sha256-'||repeat('1',64),100,$3,'ready',clock_timestamp())`, blob, scope.AccountID(), mime); err != nil {
		t.Fatal(err)
	}
	draft := creativecontent.Draft{Kind: kind}
	if kind == "model" {
		raw, err := json.Marshal(map[string]any{"format": "glb", "manifest": manifest})
		if err != nil {
			t.Fatal(err)
		}
		draft.Payload.Model = raw
	}
	err := scope.WithTxScope(context.Background(), func(tx store.TxAccountScope) error {
		_, err := creativecontent.WriteMediaAndRetain(context.Background(), tx, draft, []creativecontent.ObjectBinding{{Role: "original", BlobID: blob}}, func(r creativecontent.Revision) error {
			return tx.Insert(context.Background(), "creative_upload_candidates", []string{"id", "upload_id", "content_id", "content_revision_id", "state", "reason", "target_snapshot", "original_name", "declared_kind", "expires_at"},
				"ccuc_"+uuid.NewString(), upload, r.ContentID, r.ID, "pending", "staged", json.RawMessage(`{"kind":"staged"}`), "m", kind, "2100-01-01T00:00:00Z")
		})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return upload
}

func TestCreativeSceneModelHTTP(t *testing.T) {
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
	base := "/api/v1/creative/documents/" + doc.ID
	post := func(payload any) *httptest.ResponseRecorder {
		t.Helper()
		key := uuid.NewString()
		raw, err := json.Marshal(map[string]any{"operation_id": key, "client_created_at": time.Now().UTC(), "payload": payload})
		if err != nil {
			t.Fatal(err)
		}
		return shootPlanningRequest(t, router, "POST", base+"/commands", token, key, string(raw))
	}
	want := func(name string, w *httptest.ResponseRecorder, status int, code string) {
		t.Helper()
		if w.Code != status || code != "" && documentErrorCode(t, w) != code {
			t.Fatalf("%s: %d %s, want %d %s", name, w.Code, w.Body.String(), status, code)
		}
	}
	code := "export default function build({ THREE }) { return new THREE.Group() }"
	sum := sha256.Sum256([]byte(code))
	generator := map[string]any{"language": "three-module", "contract_version": 1, "lib_version": 1, "code": code,
		"code_sha256": "sha256-" + hex.EncodeToString(sum[:]), "params_schema": map[string]any{}, "params": map[string]any{}, "mount": "floor"}
	glbUpload, thumbUpload := stageForHTTP(t, db, scope, "model", manifest), stageForHTTP(t, db, scope, "image", manifest)
	id := "sobj_M00000000000000000001"
	instance := func(model map[string]any) map[string]any {
		return map[string]any{"id": id, "kind": "model_instance", "name": "立柱", "parent_id": nil, "order": 1,
			"transform": map[string]any{"position": []float64{0, 0, 0}, "rotation": []float64{0, 0, 0, 1}, "scale": []float64{1, 1, 1}},
			"visible":   true, "locked": false, "model": model, "material_slots": map[string]any{}}
	}
	commit := func(m any) map[string]any {
		return map[string]any{"op": "commit_model", "ref": "pillar", "glb_upload_id": glbUpload, "thumbnail_upload_id": thumbUpload, "generator": generator, "manifest": m, "name_hint": "立柱"}
	}
	place := map[string]any{"op": "add_objects", "objects": []any{instance(map[string]any{"ref": "pillar"})}}

	wrong := manifest
	wrong.Triangles++
	want("manifest mismatch", post(map[string]any{"expected": map[string]any{"objects": "1"}, "operations": []any{commit(wrong), place}}), 400, "validation_failed")

	w := post(map[string]any{"expected": map[string]any{"objects": "1"}, "operations": []any{commit(manifest), place}})
	want("commit and place", w, 200, "")
	var receipt struct {
		Revision string `json:"revision"`
		Models   map[string]struct {
			ContentID         string `json:"content_id"`
			ContentRevisionID string `json:"content_revision_id"`
		} `json:"models"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &receipt); err != nil || receipt.Models["pillar"].ContentRevisionID == "" || receipt.Revision != "2" {
		t.Fatalf("receipt %s", w.Body.String())
	}
	rev := receipt.Models["pillar"].ContentRevisionID

	read := shootPlanningRequest(t, router, "GET", base, token, "", "")
	want("read", read, 200, "")
	var v struct {
		Models map[string]map[string]any `json:"models"`
	}
	if err := json.Unmarshal(read.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	m := v.Models[rev]
	generatorRead, _ := m["generator"].(map[string]any)
	if m["usable"] != true || m["name_hint"] != "立柱" || m["manifest"] == nil || generatorRead == nil || generatorRead["code"] != nil || generatorRead["code_sha256"] == nil {
		t.Fatalf("models entry %s", read.Body.String())
	}
	if strings.Contains(read.Body.String(), "glb_url") || strings.Contains(read.Body.String(), "thumbnail_url") {
		t.Fatalf("read carries model addresses: %s", read.Body.String())
	}
	content := shootPlanningRequest(t, router, "GET", "/api/v1/creative/content-revisions/"+rev, token, "", "")
	if want("read model code", content, 200, ""); !strings.Contains(content.Body.String(), "THREE.Group") {
		t.Fatalf("model content %s", content.Body.String())
	}

	// The staged uploads were consumed: reusing them is an upload problem, never
	// a missing or trashed document.
	want("consumed upload", post(map[string]any{"expected": map[string]any{"object:" + id + ":content": "2"}, "operations": []any{
		commit(manifest), map[string]any{"op": "set_instance_model", "object_id": id, "model": map[string]any{"ref": "pillar"}},
	}}), 409, "creative_upload_unavailable")
	want("missing model", post(map[string]any{"expected": map[string]any{"object:" + id + ":content": "2"}, "operations": []any{
		map[string]any{"op": "set_instance_model", "object_id": id, "model": map[string]any{"content_revision_id": "ccrv_missing"}},
	}}), 400, "creative_model_unavailable")
}
