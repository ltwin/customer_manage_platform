package creativescene_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativedocument"
	"github.com/samson/customer-manage-platform/backend/internal/creativeglb"
	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// basicManifest is what the server computes for the shared basic.glb fixture.
func basicManifest(t *testing.T) creativeglb.Manifest {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "creativeglb", "testdata", "basic.glb"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := creativeglb.Inspect(raw, creativeglb.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// stage leaves a staged upload of kind model (the staged form with the
// server's manifest) or image (a PNG), as the media worker would, and returns
// its upload id.
func stage(t *testing.T, f fixture, scope store.AccountScope, kind string) string {
	t.Helper()
	account, blob, upload := scope.AccountID(), "ccbl_"+uuid.NewString(), "ccup_"+uuid.NewString()
	mime := map[string]string{"model": "model/gltf-binary", "image": "image/png"}[kind]
	if _, err := f.db.Exec(`INSERT INTO creative_blobs(id,account_id,storage_driver,object_key,object_version,sha256,byte_size,mime,state,verified_at)
 VALUES($1,$2,'local','creative-v2/'||$2||'/blobs/'||$1,'v1','sha256-'||repeat('1',64),100,$3,'ready',clock_timestamp())`, blob, account, mime); err != nil {
		t.Fatal(err)
	}
	draft := creativecontent.Draft{Kind: kind}
	if kind == "model" {
		payload, err := json.Marshal(map[string]any{"format": "glb", "manifest": basicManifest(t)})
		if err != nil {
			t.Fatal(err)
		}
		draft.Payload.Model = payload
	}
	err := scope.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		_, err := creativecontent.WriteMediaAndRetain(t.Context(), tx, draft, []creativecontent.ObjectBinding{{Role: "original", BlobID: blob}}, func(r creativecontent.Revision) error {
			return tx.Insert(t.Context(), "creative_upload_candidates", []string{"id", "upload_id", "content_id", "content_revision_id", "state", "reason", "target_snapshot", "original_name", "declared_kind", "expires_at"},
				"ccuc_"+uuid.NewString(), upload, r.ContentID, r.ID, "pending", "staged", json.RawMessage(`{"kind":"staged"}`), "m", kind, "2100-01-01T00:00:00Z")
		})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return upload
}

const pillarCode = `export const params = { height: { type: 'number', default: 1, min: 0.5, max: 4 } }
export default function build({ params, THREE, lib }) { return new THREE.Group() }`

func generator(code string, schema map[string]any, params map[string]any) map[string]any {
	sum := sha256.Sum256([]byte(code))
	return map[string]any{"language": "three-module", "contract_version": 1, "lib_version": 1, "code": code,
		"code_sha256": "sha256-" + hex.EncodeToString(sum[:]), "params_schema": schema, "params": params, "mount": "floor"}
}

func pillarGenerator(height float64) map[string]any {
	return generator(pillarCode, map[string]any{"height": map[string]any{"type": "number", "default": 1, "min": 0.5, "max": 4, "label": "高度"}}, map[string]any{"height": height})
}

// uploads is one staged GLB and thumbnail pair.
type uploads struct{ glb, thumb string }

func stagePair(t *testing.T, f fixture, scope store.AccountScope) uploads {
	t.Helper()
	return uploads{glb: stage(t, f, scope, "model"), thumb: stage(t, f, scope, "image")}
}

func commit(t *testing.T, ref string, u uploads, gen map[string]any, manifest any, extra map[string]any) op {
	t.Helper()
	o := op{"op": "commit_model", "ref": ref, "glb_upload_id": u.glb, "thumbnail_upload_id": u.thumb, "generator": gen, "manifest": manifest, "name_hint": "立柱"}
	for k, v := range extra {
		o[k] = v
	}
	return o
}

func instance(id string, order int, model map[string]any) map[string]any {
	o := object(id, "model_instance", nil, order)
	o["model"] = model
	o["material_slots"] = map[string]any{}
	return o
}

type committed struct {
	ContentID         string `json:"content_id"`
	ContentRevisionID string `json:"content_revision_id"`
}

func receiptModels(t *testing.T, res creativedocument.CommandResult) map[string]committed {
	t.Helper()
	var out map[string]committed
	if err := json.Unmarshal(res.Extra["models"], &out); err != nil {
		t.Fatalf("receipt models %s: %v", res.Extra["models"], err)
	}
	return out
}

type modelEntry struct {
	ContentID string                   `json:"content_id"`
	Usable    bool                     `json:"usable"`
	Manifest  *creativeglb.Manifest    `json:"manifest"`
	Generator *creativescene.Generator `json:"generator"`
	NameHint  *string                  `json:"name_hint"`
}

func readModels(t *testing.T, scope store.AccountScope, doc string) (sceneView, map[string]modelEntry) {
	t.Helper()
	raw, err := creativedocument.Read(t.Context(), scope, doc)
	if err != nil {
		t.Fatal(err)
	}
	var v sceneView
	var m struct {
		Models map[string]modelEntry `json:"models"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return v, m.Models
}

// newPillar commits a model and places one instance of it in one command.
func newPillar(t *testing.T, f fixture, doc, id string, order int) committed {
	t.Helper()
	v := readScene(t, f.a, doc)
	res := mustSend(t, f.a, batch{DocumentID: doc, Expected: v.expect("objects"), Operations: []any{
		commit(t, "pillar", stagePair(t, f, f.a), pillarGenerator(2), basicManifest(t), nil),
		op{"op": "add_objects", "objects": []any{instance(id, order, map[string]any{"ref": "pillar"})}},
	}})
	return receiptModels(t, res)["pillar"]
}

func TestCommitModelPlacesNewInstance(t *testing.T) {
	f := setup(t)
	doc := createScene(t, f.a)
	before := readScene(t, f.a, doc)
	id := objectID(1)
	u := stagePair(t, f, f.a)
	res := mustSend(t, f.a, batch{DocumentID: doc, Expected: before.expect("objects"), Operations: []any{
		commit(t, "pillar", u, pillarGenerator(2), basicManifest(t), nil),
		op{"op": "add_objects", "objects": []any{instance(id, 1, map[string]any{"ref": "pillar"})}},
	}})
	if res.Revision != before.Revision+1 {
		t.Fatalf("one command, one revision: %+v", res)
	}
	got := receiptModels(t, res)["pillar"]
	if got.ContentRevisionID == "" || got.ContentID == "" {
		t.Fatalf("receipt models %+v", res.Extra)
	}
	v, models := readModels(t, f.a, doc)
	if m := v.object(t, id).Model; m == nil || m.ContentRevisionID != got.ContentRevisionID || m.Ref != "" {
		t.Fatalf("instance model %+v", m)
	}
	entry := models[got.ContentRevisionID]
	if !entry.Usable || entry.ContentID != got.ContentID || entry.Manifest == nil || entry.Manifest.Triangles != 12 || entry.NameHint == nil || *entry.NameHint != "立柱" {
		t.Fatalf("models entry %+v", entry)
	}
	if g := entry.Generator; g == nil || g.Code != "" || g.CodeSHA256 == "" || g.Params["height"] != 2.0 || g.Mount != "floor" {
		t.Fatalf("generator in a read must not carry code: %+v", entry.Generator)
	}
	// The code is read through the content revision.
	r, err := creativecontent.Read(t.Context(), f.a, got.ContentRevisionID)
	if err != nil {
		t.Fatal(err)
	}
	var payload creativescene.ModelPayload
	if err := json.Unmarshal(r.Payload.Model, &payload); err != nil || payload.Generator == nil || payload.Generator.Code != pillarCode || len(r.Media) != 2 {
		t.Fatalf("model content %+v %v", r, err)
	}
	var consumed int
	if err := f.db.QueryRow(`SELECT count(*) FROM creative_upload_candidates WHERE upload_id IN ($1,$2) AND state='applied' AND adopted_target_kind='document' AND adopted_target_id=$3 AND adopted_target_revision=$4`, u.glb, u.thumb, doc, int64(res.Revision)).Scan(&consumed); err != nil || consumed != 2 {
		t.Fatalf("consumed candidates %d %v", consumed, err)
	}
}

// R4: an unshared model gets a new revision of the same content; a shared one
// forks unless all instances are changed together. Undo brings the old
// revision back.
func TestModelRevisionsForkAndUndo(t *testing.T) {
	f := setup(t)
	doc := createScene(t, f.a)
	a, b := objectID(1), objectID(2)
	first := newPillar(t, f, doc, a, 1)

	// Not shared: append to the same content; only the content unit moves.
	v := readScene(t, f.a, doc)
	res := mustSend(t, f.a, batch{DocumentID: doc, Expected: v.expect(content(a)), Operations: []any{
		commit(t, "taller", stagePair(t, f, f.a), pillarGenerator(3), basicManifest(t), map[string]any{"base_content_id": first.ContentID}),
		op{"op": "set_instance_model", "object_id": a, "model": map[string]any{"ref": "taller"}},
	}})
	second := receiptModels(t, res)["taller"]
	if second.ContentID != first.ContentID || second.ContentRevisionID == first.ContentRevisionID {
		t.Fatalf("unshared model should append: %+v vs %+v", second, first)
	}
	if len(res.ChangedUnits) != 1 || res.ChangedUnits[0] != content(a) {
		t.Fatalf("changed units %v", res.ChangedUnits)
	}

	// Duplicate: both instances now share the second revision.
	addObjects(t, f.a, doc, instance(b, 2, map[string]any{"content_revision_id": second.ContentRevisionID}))
	v = readScene(t, f.a, doc)
	res = mustSend(t, f.a, batch{DocumentID: doc, Expected: v.expect(content(a)), Operations: []any{
		commit(t, "fork", stagePair(t, f, f.a), pillarGenerator(4), basicManifest(t), nil),
		op{"op": "set_instance_model", "object_id": a, "model": map[string]any{"ref": "fork"}, "scope": "this"},
	}})
	fork := receiptModels(t, res)["fork"]
	v = readScene(t, f.a, doc)
	if fork.ContentID == first.ContentID || v.object(t, a).Model.ContentRevisionID != fork.ContentRevisionID || v.object(t, b).Model.ContentRevisionID != second.ContentRevisionID {
		t.Fatalf("fork changed the other instance: %+v", v.Scene.Objects)
	}

	// Both on the same revision again, then change all instances at once.
	mustSend(t, f.a, batch{DocumentID: doc, Expected: v.expect(content(a)), Operations: []any{
		op{"op": "set_instance_model", "object_id": a, "model": map[string]any{"content_revision_id": second.ContentRevisionID}},
	}})
	v = readScene(t, f.a, doc)
	res = mustSend(t, f.a, batch{DocumentID: doc, Expected: v.expect(content(a), content(b)), Operations: []any{
		commit(t, "both", stagePair(t, f, f.a), pillarGenerator(1), basicManifest(t), map[string]any{"base_content_id": first.ContentID}),
		op{"op": "set_instance_model", "object_id": a, "model": map[string]any{"ref": "both"}, "scope": "all_instances", "object_ids": []string{a, b}},
	}})
	both := receiptModels(t, res)["both"]
	after := readScene(t, f.a, doc)
	if after.Revision != v.Revision+1 || after.object(t, a).Model.ContentRevisionID != both.ContentRevisionID || after.object(t, b).Model.ContentRevisionID != both.ContentRevisionID {
		t.Fatalf("all instances: %+v", after.Scene.Objects)
	}

	// Undo the last step of a: its content unit goes back to the shared
	// second revision, which history still holds.
	restored := mustRestore(t, f.a, doc, v.Revision, []string{content(a)})
	final, models := readModels(t, f.a, doc)
	if final.object(t, a).Model.ContentRevisionID != second.ContentRevisionID || !models[second.ContentRevisionID].Usable || restored.Revision != after.Revision+1 {
		t.Fatalf("undo: %+v %+v", final.object(t, a), models)
	}
}

func TestCommitModelRejections(t *testing.T) {
	f := setup(t)
	doc := createScene(t, f.a)
	placed := objectID(1)
	existing := newPillar(t, f, doc, placed, 1)
	addObjects(t, f.a, doc, box(objectID(9), nil, 9))
	used := stagePair(t, f, f.a)
	mustSend(t, f.a, batch{DocumentID: doc, Expected: readScene(t, f.a, doc).expect("objects"), Operations: []any{
		commit(t, "once", used, pillarGenerator(2), basicManifest(t), nil),
		op{"op": "add_objects", "objects": []any{instance(objectID(2), 2, map[string]any{"ref": "once"})}},
	}})
	fresh := stagePair(t, f, f.a)
	foreign := stagePair(t, f, f.b)
	text, err := func() (creativecontent.Revision, error) {
		var r creativecontent.Revision
		body := "不是模型"
		err := f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
			var err error
			r, err = creativecontent.WriteAndRetain(t.Context(), tx, creativecontent.Draft{Kind: "text", Payload: creativecontent.Payload{Body: &body}}, "", nil, func(r creativecontent.Revision) error {
				return tx.Insert(t.Context(), "creative_document_content_refs", []string{"document_id", "revision", "content_revision_id"}, "cwdoc_other", int64(1), r.ID)
			})
			return err
		})
		return r, err
	}()
	if err != nil {
		t.Fatal(err)
	}
	manifest := basicManifest(t)
	wrongManifest := manifest
	wrongManifest.Triangles++
	badHash := pillarGenerator(2)
	badHash["code_sha256"] = "sha256-" + hex.EncodeToString(make([]byte, 32))
	place := func(ref string) op {
		return op{"op": "add_objects", "objects": []any{instance(objectID(3), 3, map[string]any{"ref": ref})}}
	}
	sixteen := make([]any, 0, 18)
	for i := range 17 {
		sixteen = append(sixteen, commit(t, "m"+string(rune('a'+i)), fresh, pillarGenerator(2), manifest, nil))
	}
	tests := []struct {
		name string
		ops  []any
		want error
	}{
		{"manifest differs from GLB", []any{commit(t, "p", fresh, pillarGenerator(2), wrongManifest, nil), place("p")}, creativeops.ErrValidation},
		{"code hash differs", []any{commit(t, "p", fresh, badHash, manifest, nil), place("p")}, creativeops.ErrValidation},
		{"param out of range", []any{commit(t, "p", fresh, pillarGenerator(9), manifest, nil), place("p")}, creativeops.ErrValidation},
		{"param without definition", []any{commit(t, "p", fresh, generator(pillarCode, map[string]any{}, map[string]any{"height": 2}), manifest, nil), place("p")}, creativeops.ErrValidation},
		{"integer without range", []any{commit(t, "p", fresh, generator(pillarCode, map[string]any{"n": map[string]any{"type": "integer", "default": 1}}, map[string]any{}), manifest, nil), place("p")}, creativeops.ErrValidation},
		{"ref undefined", []any{place("ghost")}, creativeops.ErrValidation},
		{"ref used before its commit", []any{place("p"), commit(t, "p", fresh, pillarGenerator(2), manifest, nil)}, creativeops.ErrValidation},
		{"ref repeated", []any{commit(t, "p", fresh, pillarGenerator(2), manifest, nil), commit(t, "p", fresh, pillarGenerator(2), manifest, nil), place("p")}, creativeops.ErrValidation},
		{"ref never used", []any{commit(t, "p", fresh, pillarGenerator(2), manifest, nil)}, creativeops.ErrValidation},
		{"too many commits", sixteen, creativeops.ErrValidation},
		{"upload kinds swapped", []any{commit(t, "p", uploads{glb: fresh.thumb, thumb: fresh.glb}, pillarGenerator(2), manifest, nil), place("p")}, creativeops.ErrValidation},
		{"upload already consumed", []any{commit(t, "p", used, pillarGenerator(2), manifest, nil), place("p")}, creativescene.ErrUploadUnavailable},
		{"upload of another account", []any{commit(t, "p", foreign, pillarGenerator(2), manifest, nil), place("p")}, creativescene.ErrUploadUnavailable},
		{"upload missing", []any{commit(t, "p", uploads{glb: "ccup_missing", thumb: fresh.thumb}, pillarGenerator(2), manifest, nil), place("p")}, creativescene.ErrUploadUnavailable},
		{"base content is not a model", []any{commit(t, "p", fresh, pillarGenerator(2), manifest, map[string]any{"base_content_id": text.ContentID}), place("p")}, creativeops.ErrValidation},
		{"referenced revision is not a model", []any{op{"op": "add_objects", "objects": []any{instance(objectID(3), 3, map[string]any{"content_revision_id": text.ID})}}}, creativescene.ErrModelUnavailable},
		{"referenced revision missing", []any{op{"op": "add_objects", "objects": []any{instance(objectID(3), 3, map[string]any{"content_revision_id": "ccrv_missing"})}}}, creativescene.ErrModelUnavailable},
		{"model through update_objects", []any{op{"op": "update_objects", "updates": []any{op{"object_id": placed, "model": map[string]any{"content_revision_id": existing.ContentRevisionID}}}}}, creativeops.ErrValidation},
		{"set_instance_model on a primitive", []any{op{"op": "set_instance_model", "object_id": objectID(9), "model": map[string]any{"content_revision_id": existing.ContentRevisionID}}}, creativeops.ErrValidation},
		{"all_instances listing an instance of another revision", []any{op{"op": "set_instance_model", "object_id": placed, "model": map[string]any{"content_revision_id": existing.ContentRevisionID}, "scope": "all_instances", "object_ids": []string{placed, objectID(2)}}}, creativeops.ErrValidation},
		{"all_instances without object_ids", []any{op{"op": "set_instance_model", "object_id": placed, "model": map[string]any{"content_revision_id": existing.ContentRevisionID}, "scope": "all_instances"}}, creativeops.ErrValidation},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := readScene(t, f.a, doc)
			expected := v.expect("objects", content(placed), content(objectID(9)), content(objectID(2)))
			_, err := send(t, f.a, batch{DocumentID: doc, Expected: expected, Operations: tt.ops})
			wantErr(t, err, tt.want)
			t.Logf("rejected: %v", err)
			if after := readScene(t, f.a, doc); after.Revision != v.Revision {
				t.Fatalf("rejected command moved the scene to %d", after.Revision)
			}
		})
	}
	// The staged uploads of rejected commands are still usable.
	newPillarFrom(t, f, doc, objectID(4), 4, fresh)
	// Another account cannot place this account's model.
	other := createScene(t, f.b)
	_, err = send(t, f.b, batch{DocumentID: other, Expected: readScene(t, f.b, other).expect("objects"), Operations: []any{
		op{"op": "add_objects", "objects": []any{instance(objectID(1), 1, map[string]any{"content_revision_id": existing.ContentRevisionID})}},
	}})
	wantErr(t, err, creativescene.ErrModelUnavailable)
}

func newPillarFrom(t *testing.T, f fixture, doc, id string, order int, u uploads) {
	t.Helper()
	mustSend(t, f.a, batch{DocumentID: doc, Expected: readScene(t, f.a, doc).expect("objects"), Operations: []any{
		commit(t, "pillar", u, pillarGenerator(2), basicManifest(t), nil),
		op{"op": "add_objects", "objects": []any{instance(id, order, map[string]any{"ref": "pillar"})}},
	}})
}

// Reading a scene reads all its models with the same number of queries
// whether it references one model or many.
func TestReadModelsQueriesDoNotGrowWithModels(t *testing.T) {
	f := setup(t)
	if _, err := f.db.Exec(`CREATE VIEW test_revision_scans AS SELECT 'scene-a'::text AS account_id, (coalesce(seq_scan,0)+coalesce(idx_scan,0))::bigint AS scans
 FROM pg_stat_xact_user_tables WHERE relname='creative_content_revisions'`); err != nil {
		t.Fatal(err)
	}
	doc := createScene(t, f.a)
	var refs []string
	for i := range 6 {
		refs = append(refs, newPillar(t, f, doc, objectID(i+1), i+1).ContentRevisionID)
	}
	typ, _ := creativedocument.Lookup(creativescene.TypeKey)
	scans := func(tx store.ReadTxAccountScope) (int64, error) {
		var v int64
		err := tx.QueryRow(t.Context(), "test_revision_scans", "scans", "").Scan(&v)
		return v, err
	}
	deltas := map[int]int64{}
	for _, n := range []int{1, 6} {
		var raw json.RawMessage
		err := f.a.WithReadSnapshot(t.Context(), func(tx store.ReadTxAccountScope) error {
			before, err := scans(tx)
			if err != nil {
				return err
			}
			cur := creativedocument.Current{Refs: refs[:n], Content: creativecontent.Revision{Payload: creativecontent.Payload{Scene: json.RawMessage(`{"objects":[]}`)}}}
			if raw, err = typ.View(t.Context(), tx, cur, nil); err != nil {
				return err
			}
			after, err := scans(tx)
			deltas[n] = after - before
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		var v struct {
			Models map[string]modelEntry `json:"models"`
		}
		if err := json.Unmarshal(raw, &v); err != nil || len(v.Models) != n {
			t.Fatalf("view of %d models: %s %v", n, raw, err)
		}
	}
	if deltas[1] != deltas[6] {
		t.Fatalf("revision scans grew with models: %v", deltas)
	}
}
