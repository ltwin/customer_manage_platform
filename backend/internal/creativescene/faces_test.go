package creativescene_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
)

// Face assignments on models (scene-face-materials 3.2): commit_model carries
// them over a re-bake, derive_model changes only them over the same GLB.

func brickFaces() map[string]any {
	return map[string]any{
		"meshes": []any{map[string]any{"path": []any{[]any{"", 0}}, "primitive": 0, "topology": "t1-9f86d081884c7d65", "assign": map[string]any{"brick": []any{[]any{0, 2}}}}},
		"hints":  map[string]any{"brick": "brick"},
	}
}

func derive(ref, from string, faces any, extra map[string]any) op {
	o := op{"op": "derive_model", "ref": ref, "from_revision_id": from, "faces": faces}
	for k, v := range extra {
		o[k] = v
	}
	return o
}

func facesJSON(t *testing.T, f *creativescene.Faces) string {
	t.Helper()
	raw, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestCommitModelKeepsFaces(t *testing.T) {
	f := setup(t)
	doc := createScene(t, f.a)
	id := objectID(1)
	res := mustSend(t, f.a, batch{DocumentID: doc, Expected: readScene(t, f.a, doc).expect("objects"), Operations: []any{
		commit(t, "pillar", stagePair(t, f, f.a), pillarGenerator(2), basicManifest(t), map[string]any{"faces": brickFaces()}),
		op{"op": "add_objects", "objects": []any{instance(id, 1, map[string]any{"ref": "pillar"})}},
	}})
	got := receiptModels(t, res)["pillar"]
	_, models := readModels(t, f.a, doc)
	want := `{"meshes":[{"path":[["",0]],"primitive":0,"topology":"t1-9f86d081884c7d65","assign":{"brick":[[0,2]]}}],"hints":{"brick":"brick"}}`
	if s := facesJSON(t, models[got.ContentRevisionID].Faces); s != want {
		t.Fatalf("read faces %s", s)
	}
	// A model without an assignment reads faces as null.
	plain := newPillar(t, f, doc, objectID(2), 2)
	if _, models := readModels(t, f.a, doc); models[plain.ContentRevisionID].Faces != nil {
		t.Fatalf("faces of a plain model %+v", models[plain.ContentRevisionID].Faces)
	}
}

func TestDeriveModelReusesTheGLB(t *testing.T) {
	f := setup(t)
	doc := createScene(t, f.a)
	a, b := objectID(1), objectID(2)
	placed := newPillar(t, f, doc, a, 1)
	src, srcPayload := readRevision(t, f.a, placed.ContentRevisionID)

	// Not shared: a new revision of the same content, one scene revision, one unit.
	before := readScene(t, f.a, doc)
	res := mustSend(t, f.a, batch{DocumentID: doc, Expected: before.expect(content(a)), Operations: []any{
		derive("faces", placed.ContentRevisionID, brickFaces(), map[string]any{"base_content_id": placed.ContentID}),
		op{"op": "set_instance_model", "object_id": a, "model": map[string]any{"ref": "faces"}},
	}})
	derived := receiptModels(t, res)["faces"]
	if res.Revision != before.Revision+1 || len(res.ChangedUnits) != 1 || res.ChangedUnits[0] != content(a) {
		t.Fatalf("one assignment, one revision of one unit: %+v", res)
	}
	if derived.ContentID != placed.ContentID || derived.ContentRevisionID == placed.ContentRevisionID {
		t.Fatalf("an unshared model gets a new revision of its content: %+v", derived)
	}
	r, payload := readRevision(t, f.a, derived.ContentRevisionID)
	for _, role := range []string{"original", "thumbnail"} {
		if blobOf(t, r, role) != blobOf(t, src, role) {
			t.Errorf("%s is not the source's blob", role)
		}
	}
	if payload.Generator == nil || payload.Generator.Code != pillarCode || !reflect.DeepEqual(payload.Generator, srcPayload.Generator) || !reflect.DeepEqual(payload.Manifest, srcPayload.Manifest) || payload.NameHint != srcPayload.NameHint {
		t.Fatalf("derived payload %+v", payload)
	}
	if payload.Faces == nil || payload.Faces.Meshes[0].Assign["brick"][0][1] != 2 {
		t.Fatalf("derived faces %+v", payload.Faces)
	}
	if _, models := readModels(t, f.a, doc); models[derived.ContentRevisionID].Faces == nil {
		t.Fatal("the read carries the derived faces")
	}

	// Shared: without a base it forks, the other instance keeps its model.
	addObjects(t, f.a, doc, instance(b, 2, map[string]any{"content_revision_id": derived.ContentRevisionID}))
	res = mustSend(t, f.a, batch{DocumentID: doc, Expected: readScene(t, f.a, doc).expect(content(a)), Operations: []any{
		derive("clear", derived.ContentRevisionID, nil, nil),
		op{"op": "set_instance_model", "object_id": a, "model": map[string]any{"ref": "clear"}, "scope": "this"},
	}})
	cleared := receiptModels(t, res)["clear"]
	v, models := readModels(t, f.a, doc)
	if cleared.ContentID == placed.ContentID || v.object(t, b).Model.ContentRevisionID != derived.ContentRevisionID {
		t.Fatalf("fork: %+v %+v", cleared, v.object(t, b))
	}
	if models[cleared.ContentRevisionID].Faces != nil {
		t.Fatal("faces null clears the assignment")
	}

	// Undo brings the earlier revision back.
	mustRestore(t, f.a, doc, v.Revision-1, []string{content(a)})
	if v := readScene(t, f.a, doc); v.object(t, a).Model.ContentRevisionID != derived.ContentRevisionID {
		t.Fatalf("undo: %+v", v.object(t, a))
	}
}

func TestDeriveModelFromALibraryCopy(t *testing.T) {
	f := setup(t)
	doc := createScene(t, f.a)
	id, placedFromLibrary := objectID(1), objectID(2)
	newPillar(t, f, doc, id, 1)
	_, saved, err := saveModel(t, f.a, command(t, creativescene.SaveModelInput{DocumentID: doc, ObjectID: id, ExpectedObjectRev: contentRev(t, readScene(t, f.a, doc), id),
		ThumbnailUploadID: stage(t, f, f.a, "image"), Target: creativelibrary.ImportTarget{Title: "柱"}}))
	if err != nil {
		t.Fatal(err)
	}
	copied, _ := readRevision(t, f.a, saved.ContentRevisionID)
	addObjects(t, f.a, doc, instance(placedFromLibrary, 2, map[string]any{"content_revision_id": copied.ID}))
	res := mustSend(t, f.a, batch{DocumentID: doc, Expected: readScene(t, f.a, doc).expect(content(placedFromLibrary)), Operations: []any{
		derive("mine", copied.ID, brickFaces(), map[string]any{"base_content_id": copied.ContentID}),
		op{"op": "set_instance_model", "object_id": placedFromLibrary, "model": map[string]any{"ref": "mine"}},
	}})
	mine := receiptModels(t, res)["mine"]
	if mine.ContentID == copied.ContentID {
		t.Fatal("a library copy is never continued")
	}
	if _, payload := readRevision(t, f.a, mine.ContentRevisionID); payload.Materials != nil || payload.SourceRevisionID != "" || payload.Faces == nil {
		t.Fatalf("the library's materials and source stay with the library: %+v", payload)
	}
}

func TestDeriveModelRejections(t *testing.T) {
	f := setup(t)
	doc := createScene(t, f.a)
	id := objectID(1)
	placed := newPillar(t, f, doc, id, 1)
	other := createScene(t, f.b)
	foreign := receiptModels(t, mustSend(t, f.b, batch{DocumentID: other, Expected: readScene(t, f.b, other).expect("objects"), Operations: []any{
		commit(t, "pillar", stagePair(t, f, f.b), pillarGenerator(2), basicManifest(t), nil),
		op{"op": "add_objects", "objects": []any{instance(objectID(1), 1, map[string]any{"ref": "pillar"})}},
	}}))["pillar"]
	place := op{"op": "set_instance_model", "object_id": id, "model": map[string]any{"ref": "d"}}
	overlap := brickFaces()
	overlap["meshes"].([]any)[0].(map[string]any)["assign"] = map[string]any{"brick": []any{[]any{0, 4}}, "trim": []any{[]any{3, 6}}}
	seventeen := []any{}
	for i := range 17 {
		ref := "d" + string(rune('a'+i))
		seventeen = append(seventeen, derive(ref, placed.ContentRevisionID, brickFaces(), nil))
	}
	tests := []struct {
		name string
		ops  []any
		want error
	}{
		{"another account's model", []any{derive("d", foreign.ContentRevisionID, brickFaces(), nil), place}, creativescene.ErrModelUnavailable},
		{"missing revision", []any{derive("d", "ccrv_missing", brickFaces(), nil), place}, creativescene.ErrModelUnavailable},
		{"overlapping ranges", []any{derive("d", placed.ContentRevisionID, overlap, nil), place}, creativeops.ErrValidation},
		{"faces in commit_model checked too", []any{commit(t, "d", stagePair(t, f, f.a), pillarGenerator(2), basicManifest(t), map[string]any{"faces": overlap}), place}, creativeops.ErrValidation},
		{"more than sixteen with commits", seventeen, creativeops.ErrValidation},
		{"ref never used", []any{derive("d", placed.ContentRevisionID, brickFaces(), nil)}, creativeops.ErrValidation},
		{"bad ref", []any{derive("D!", placed.ContentRevisionID, brickFaces(), nil), place}, creativeops.ErrValidation},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := readScene(t, f.a, doc)
			_, err := send(t, f.a, batch{DocumentID: doc, Expected: v.expect(content(id)), Operations: tt.ops})
			wantErr(t, err, tt.want)
			if after := readScene(t, f.a, doc); after.Revision != v.Revision {
				t.Fatalf("rejected command moved the scene to %d", after.Revision)
			}
		})
	}
}
