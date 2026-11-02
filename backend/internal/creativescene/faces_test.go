package creativescene_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativedocument"
	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
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

// Face assignments on builtins (scene-face-materials 3.3): kept on the
// object, cleared when a parameter of the builtin's topology declaration
// changes and faces are not written.

func builtinFaces(ranges ...[]any) map[string]any {
	list := []any{}
	for _, r := range ranges {
		list = append(list, r)
	}
	if len(list) == 0 {
		list = []any{[]any{0, 2}}
	}
	return map[string]any{"meshes": []any{map[string]any{"path": []any{[]any{"", 0}}, "primitive": 0, "topology": "t1-9f86d081884c7d65", "assign": map[string]any{"brick": list}}}}
}

func primitiveWith(id, builtin string, order int, faces any) map[string]any {
	o := object(id, "primitive", nil, order)
	p := map[string]any{"builtin": builtin}
	if faces != nil {
		p["faces"] = faces
	}
	o["primitive"] = p
	return o
}

func updatePrimitive(t *testing.T, f fixture, doc, id string, patch map[string]any) creativedocument.CommandResult {
	t.Helper()
	return mustSend(t, f.a, batch{DocumentID: doc, Expected: readScene(t, f.a, doc).expect(content(id)), Operations: []any{
		op{"op": "update_objects", "updates": []any{op{"object_id": id, "primitive": patch}}},
	}})
}

func TestBuiltinFacesFollowTheTopologyDeclaration(t *testing.T) {
	f := setup(t)
	doc := createScene(t, f.a)
	arch, cyl := objectID(1), objectID(2)
	addObjects(t, f.a, doc, primitiveWith(arch, "arch@1", 1, builtinFaces()), primitiveWith(cyl, "cylinder@1", 2, builtinFaces()))
	has := func(id string) bool { return readScene(t, f.a, doc).object(t, id).Primitive.Faces != nil }
	if !has(arch) || !has(cyl) {
		t.Fatal("faces written with add_objects are stored")
	}

	cases := []struct {
		id    string
		patch map[string]any
		keeps bool
	}{
		{arch, map[string]any{"params": map[string]any{"depth": 1.0}}, true},
		{cyl, map[string]any{"params": map[string]any{"radius_top": 0.3, "height": 3.0}}, true},
		{cyl, map[string]any{"params": map[string]any{"radius_top": 0.0}}, false},
		{arch, map[string]any{"params": map[string]any{"opening_width": 0.4}}, false},
	}
	for _, c := range cases {
		res := updatePrimitive(t, f, doc, c.id, c.patch)
		if has(c.id) != c.keeps {
			t.Fatalf("%s %v: faces kept = %v, want %v", c.id, c.patch, !c.keeps, c.keeps)
		}
		if len(res.ChangedUnits) != 1 || res.ChangedUnits[0] != content(c.id) {
			t.Fatalf("changed units %v", res.ChangedUnits)
		}
	}

	// Written faces win over the rule; null clears; faces alone need no params.
	updatePrimitive(t, f, doc, arch, map[string]any{"params": map[string]any{"width": 4.0}, "faces": builtinFaces()})
	if !has(arch) {
		t.Fatal("faces written with a topology change are kept")
	}
	updatePrimitive(t, f, doc, arch, map[string]any{"faces": nil})
	if has(arch) {
		t.Fatal("faces null clears")
	}
	updatePrimitive(t, f, doc, arch, map[string]any{"faces": builtinFaces([]any{0, 1}, []any{4, 6})})
	if got := readScene(t, f.a, doc).object(t, arch).Primitive.Faces; got == nil || len(got.Meshes[0].Assign["brick"]) != 2 {
		t.Fatalf("faces alone: %+v", got)
	}
}

func TestBuiltinFacesRejections(t *testing.T) {
	f := setup(t)
	doc := createScene(t, f.a)
	id := objectID(1)
	addObjects(t, f.a, doc, primitiveWith(id, "box@1", 1, nil))
	var tooMany [][]any
	for i := range 257 {
		tooMany = append(tooMany, []any{2 * i, 2*i + 1})
	}
	other := builtinFaces()
	other["meshes"].([]any)[0].(map[string]any)["path"] = []any{[]any{"box", 0}}
	unknown := builtinFaces()
	unknown["extra"] = true
	for name, faces := range map[string]any{
		"another path":    other,
		"too many ranges": builtinFaces(tooMany...),
		"unknown field":   unknown,
	} {
		t.Run(name, func(t *testing.T) {
			v := readScene(t, f.a, doc)
			_, err := send(t, f.a, batch{DocumentID: doc, Expected: v.expect(content(id)), Operations: []any{
				op{"op": "update_objects", "updates": []any{op{"object_id": id, "primitive": map[string]any{"faces": faces}}}},
			}})
			wantErr(t, err, creativeops.ErrValidation)
			_, err = send(t, f.a, batch{DocumentID: doc, Expected: v.expect("objects"), Operations: []any{
				op{"op": "add_objects", "objects": []any{primitiveWith(objectID(2), "box@1", 2, faces)}},
			}})
			wantErr(t, err, creativeops.ErrValidation)
		})
	}
}

// What the assistant is told (scene-face-materials 3.4): effective slots in
// read_scene, builtins an edit cleared, and what a rebake kept.

func slotsOf(t *testing.T, slots []creativecontent.Slot) string {
	t.Helper()
	raw, err := json.Marshal(slots)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestTheAssistantSeesFaceAssignments(t *testing.T) {
	f := setup(t)
	creativescene.SetJobWaiterForTest(t, &fakeWaiter{})
	doc := createScene(t, f.a)
	model, arch := objectID(1), objectID(2)
	mustSend(t, f.a, batch{DocumentID: doc, Expected: readScene(t, f.a, doc).expect("objects"), Operations: []any{
		commit(t, "pillar", stagePair(t, f, f.a), pillarGenerator(2), basicManifest(t), map[string]any{"faces": brickFaces()}),
		op{"op": "add_objects", "objects": []any{instance(model, 1, map[string]any{"ref": "pillar"}), primitiveWith(arch, "arch@1", 2, builtinFaces())}},
	}})

	read, err := agentRead(t, f.a, creativescene.ReadInput{DocumentID: doc})
	if err != nil {
		t.Fatal(err)
	}
	views := map[string]creativescene.ObjectView{}
	for _, o := range read.Objects {
		views[o.ID] = o
	}
	glb := basicManifest(t).Slots
	wantModel := `[{"id":"` + glb[0].ID + `","hint":"` + glb[0].Hint + `","source":"model"},{"id":"brick","hint":"brick","source":"faces","face_triangles":2}]`
	if got := slotsOf(t, views[model].Model.Slots); len(glb) != 1 || got != wantModel {
		t.Errorf("model slots %s, want %s", got, wantModel)
	}
	if got := slotsOf(t, views[arch].Slots); got != `[{"id":"main","source":"model"},{"id":"brick","source":"faces","face_triangles":2}]` {
		t.Errorf("builtin slots %s", got)
	}

	// edit_scene says which builtins a topology change cleared; a written null
	// is the assistant's own doing and is not reported.
	edit := func(patch map[string]any) creativescene.EditResult {
		t.Helper()
		out, err := agentEdit(t, f.a, creativescene.EditInput{DocumentID: doc, RunID: "ccrn_agent", BasedOn: int64(readScene(t, f.a, doc).Revision),
			Operations: raws(t, op{"op": "update_objects", "updates": []any{map[string]any{"object_id": arch, "primitive": patch}}})})
		if err != nil {
			t.Fatal(err)
		}
		return out.(creativescene.EditResult)
	}
	if got := edit(map[string]any{"params": map[string]any{"depth": 1.0}}); len(got.FacesCleared) != 0 {
		t.Errorf("depth keeps the faces: %+v", got)
	}
	if got := edit(map[string]any{"params": map[string]any{"opening_width": 0.4}}); len(got.FacesCleared) != 1 || got.FacesCleared[0] != arch {
		t.Errorf("opening_width clears the faces: %+v", got)
	}
	edit(map[string]any{"faces": builtinFaces()})
	if got := edit(map[string]any{"faces": nil}); len(got.FacesCleared) != 0 {
		t.Errorf("a written null is not reported: %+v", got)
	}

	// A rebake that carried nothing over: the brick faces are reported dropped.
	rebake := func(faces any) *creativescene.FacesChange {
		t.Helper()
		job, _, err := bakeJobInTx(t, f.a, creativescene.BakeInput{DocumentID: doc, RunID: "ccrn_agent", ToolCallID: uuid.NewString(), BasedOn: int64(readScene(t, f.a, doc).Revision),
			Code: pillarCode, Params: map[string]any{"height": 3}, Replace: &creativescene.ReplaceTarget{ObjectID: model}})
		if err != nil {
			t.Fatal(err)
		}
		claimed := claimOne(t, f.a)
		out := bakeOutput(stagePair(t, f, f.a), basicManifest(t))
		if faces != nil {
			out["faces"] = faces
		}
		resp, err := completeJob(t, f.a, uuid.NewString(), map[string]any{"job_id": job.ID, "claim_token": claimed.ClaimToken, "outcome": "succeeded", "bake": out})
		if err != nil || resp.Result.Bake == nil {
			t.Fatalf("bake %+v %v", resp, err)
		}
		var change *creativescene.FacesChange
		if err := f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
			var err error
			change, err = creativescene.BakeFacesInTx(t.Context(), tx, doc, *resp.Result.Bake)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return change
	}
	if got := rebake(brickFaces()); got == nil || len(got.Kept) != 1 || got.Kept[0] != (creativescene.SlotTriangles{Slot: "brick", Triangles: 2}) || len(got.Dropped) != 0 {
		t.Errorf("carried over: %+v", got)
	}
	if got := rebake(nil); got == nil || len(got.Kept) != 0 || len(got.Dropped) != 1 || got.Dropped[0].Triangles != 2 {
		t.Errorf("dropped: %+v", got)
	}
	if got := rebake(nil); got != nil {
		t.Errorf("nothing to report once the model has no faces: %+v", got)
	}
}

func TestSavingAModelKeepsItsFaceSlots(t *testing.T) {
	f := setup(t)
	doc := createScene(t, f.a)
	id := objectID(1)
	mustSend(t, f.a, batch{DocumentID: doc, Expected: readScene(t, f.a, doc).expect("objects"), Operations: []any{
		commit(t, "pillar", stagePair(t, f, f.a), pillarGenerator(2), basicManifest(t), map[string]any{"faces": brickFaces()}),
		op{"op": "add_objects", "objects": []any{slotted(instance(id, 1, map[string]any{"ref": "pillar"}), map[string]any{"brick": material("stone_brick@1"), "gone": material("wood@1")})}},
	}})
	_, saved, err := saveModel(t, f.a, command(t, creativescene.SaveModelInput{DocumentID: doc, ObjectID: id, ExpectedObjectRev: contentRev(t, readScene(t, f.a, doc), id),
		ThumbnailUploadID: stage(t, f, f.a, "image"), Target: creativelibrary.ImportTarget{Title: "砖柱"}}))
	if err != nil {
		t.Fatal(err)
	}
	_, payload := readRevision(t, f.a, saved.ContentRevisionID)
	if payload.Faces == nil {
		t.Fatal("the library copy carries the face assignment")
	}
	if _, ok := payload.Materials["brick"]; !ok || len(payload.Materials) != 1 {
		t.Fatalf("materials of effective slots only: %+v", payload.Materials)
	}
}

// The assistant gives a slot a face assignment brought a material as it
// would the model's own (data: 模型内容「有效槽位」): read and write go by
// the same slots, faces written in the same batch included.
func TestTheAssistantPaintsFaceSlots(t *testing.T) {
	f := setup(t)
	creativescene.SetJobWaiterForTest(t, &fakeWaiter{})
	doc := createScene(t, f.a)
	model, arch, box := objectID(1), objectID(2), objectID(3)
	mustSend(t, f.a, batch{DocumentID: doc, Expected: readScene(t, f.a, doc).expect("objects"), Operations: []any{
		commit(t, "pillar", stagePair(t, f, f.a), pillarGenerator(2), basicManifest(t), map[string]any{"faces": brickFaces()}),
		op{"op": "add_objects", "objects": []any{instance(model, 1, map[string]any{"ref": "pillar"}), primitiveWith(arch, "arch@1", 2, builtinFaces()), primitiveWith(box, "box@1", 3, nil)}},
	}})
	paint := map[string]any{"preset": "paint@1", "params": map[string]any{}}
	edit := func(ops ...any) error {
		t.Helper()
		_, err := agentEdit(t, f.a, creativescene.EditInput{DocumentID: doc, RunID: "ccrn_agent", BasedOn: int64(readScene(t, f.a, doc).Revision), Operations: raws(t, ops...)})
		return err
	}
	slots := func(id string, primitive map[string]any, s map[string]any) op {
		u := map[string]any{"object_id": id, "material_slots": s}
		if primitive != nil {
			u["primitive"] = primitive
		}
		return op{"op": "update_objects", "updates": []any{u}}
	}
	if err := edit(slots(model, nil, map[string]any{"brick": paint}), slots(arch, nil, map[string]any{"brick": paint})); err != nil {
		t.Fatalf("face slots of a model and a builtin take materials: %v", err)
	}
	if err := edit(slots(box, map[string]any{"faces": builtinFaces()}, map[string]any{"brick": paint})); err != nil {
		t.Fatalf("faces written in the same update bring their slot: %v", err)
	}
	if err := edit(slots(arch, nil, map[string]any{"door": paint})); !errors.Is(err, creativeops.ErrValidation) || !strings.Contains(err.Error(), "可用：main、brick") {
		t.Fatalf("a slot no assignment has is refused, naming the effective ones: %v", err)
	}
	for _, id := range []string{model, arch, box} {
		if got := readScene(t, f.a, doc).object(t, id).MaterialSlots["brick"]; got.Preset != "paint@1" {
			t.Errorf("%s brick = %+v", id, got)
		}
	}
}
