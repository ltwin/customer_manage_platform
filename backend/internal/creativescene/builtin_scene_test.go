package creativescene_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
)

const (
	testFemale = "body_test_female@1"
	testMale   = "body_test_male@1"
)

// useTestBodies puts two test bodies in the built-in model catalog: a female
// one with slots body and eyes, a male one with body.
func useTestBodies(t *testing.T) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "builtin_models.json"))
	if err != nil {
		t.Fatal(err)
	}
	creativescene.UseBuiltinModelsForTest(t, raw)
}

func body(id string, order int, builtin string) map[string]any {
	o := instance(id, order, map[string]any{"builtin": builtin})
	o["material_slots"] = map[string]any{"body": map[string]any{"preset": "principled@1", "params": map[string]any{}}}
	return o
}

// A built-in model is placed by its catalog ID: no reference row, swapped
// like any model, and refused where it has no revision to work from.
func TestBuiltinModelInstances(t *testing.T) {
	useTestBodies(t)
	f := setup(t)
	doc := createScene(t, f.a)
	her, twin, pillar := objectID(1), objectID(2), objectID(3)
	addObjects(t, f.a, doc, body(her, 1, testFemale), body(twin, 2, testFemale))
	placed := newPillar(t, f, doc, pillar, 3)

	v, models := readModels(t, f.a, doc)
	if m := v.object(t, her).Model; m.Builtin != testFemale || m.ContentRevisionID != "" {
		t.Fatalf("stored %+v", m)
	}
	// Only the pillar's revision is the document's model: the body is no account content.
	if len(models) != 1 || models[placed.ContentRevisionID].ContentID == "" {
		t.Fatalf("models %v", models)
	}
	var refs int
	if err := f.db.QueryRow(`SELECT count(*) FROM creative_document_content_refs WHERE document_id = $1 AND revision = $2`, doc, v.Revision).Scan(&refs); err != nil {
		t.Fatal(err)
	}
	if refs != 1 {
		t.Fatalf("%d reference rows", refs)
	}

	// Both of hers become him, with his materials.
	mustSend(t, f.a, batch{DocumentID: doc, Expected: v.expect(content(her), content(twin)), Operations: []any{
		op{"op": "set_instance_model", "object_id": her, "model": map[string]any{"builtin": testMale}, "scope": "all_instances", "object_ids": []string{her, twin},
			"material_slots": map[string]any{"body": map[string]any{"preset": "principled@1", "params": map[string]any{"roughness": 0.4}}}},
	}})
	v = readScene(t, f.a, doc)
	for _, id := range []string{her, twin} {
		if o := v.object(t, id); o.Model.Builtin != testMale || o.MaterialSlots["body"].Params["roughness"] != 0.4 {
			t.Fatalf("%s after the swap: %+v %+v", id, o.Model, o.MaterialSlots)
		}
	}

	for name, tc := range map[string]struct {
		ops  []any
		want error
		says string
	}{
		"unknown body": {[]any{op{"op": "add_objects", "objects": []any{body(objectID(9), 9, "body_elf@1")}}}, creativescene.ErrModelUnavailable, "body_test_female@1"},
		"both refs": {[]any{op{"op": "add_objects", "objects": []any{instance(objectID(9), 9, map[string]any{"builtin": testMale, "content_revision_id": placed.ContentRevisionID})}}},
			creativeops.ErrValidation, "其中之一"},
		"swap to an unknown body": {[]any{op{"op": "set_instance_model", "object_id": her, "model": map[string]any{"builtin": "body_elf@1"}}}, creativescene.ErrModelUnavailable, "没有内置模型"},
		"not the same model": {[]any{op{"op": "set_instance_model", "object_id": her, "model": map[string]any{"builtin": testFemale}, "scope": "all_instances", "object_ids": []string{her, pillar}}},
			creativeops.ErrValidation, "不是同一个模型"},
		"derived from a body": {[]any{derive("d", testMale, brickFaces(), nil)}, creativescene.ErrModelUnavailable, "是内置模型"},
	} {
		units := []string{"objects", content(her), content(pillar)}
		_, err := send(t, f.a, batch{DocumentID: doc, Expected: v.expect(units...), Operations: tc.ops})
		if !errors.Is(err, tc.want) || !strings.Contains(err.Error(), tc.says) {
			t.Errorf("%s: %v, want %v saying %q", name, err, tc.want, tc.says)
		}
	}
	if after := readScene(t, f.a, doc); after.Revision != v.Revision {
		t.Fatalf("refused commands moved the scene to %d", after.Revision)
	}

	// Saving it to the library or baking code over it: refused, saying why.
	thumb := stage(t, f, f.a, "image")
	_, _, err := saveModel(t, f.a, command(t, creativescene.SaveModelInput{DocumentID: doc, ObjectID: her, ExpectedObjectRev: contentRev(t, v, her), ThumbnailUploadID: thumb, Target: creativelibrary.ImportTarget{Title: "人"}}))
	if !errors.Is(err, creativeops.ErrValidation) || !strings.Contains(err.Error(), "不用存入资产库") {
		t.Fatalf("saved: %v", err)
	}
	_, _, err = bakeJobInTx(t, f.a, creativescene.BakeInput{DocumentID: doc, RunID: "ccrn_agent", ToolCallID: "step-replace", BasedOn: int64(v.Revision), Code: pillarCode,
		Params: map[string]any{"height": 3}, Replace: &creativescene.ReplaceTarget{ObjectID: her}})
	if !errors.Is(err, creativeops.ErrValidation) || !strings.Contains(err.Error(), "不能用代码替换") {
		t.Fatalf("baked over: %v", err)
	}
}

// The render fingerprint counts a built-in model by its ID: swapping bodies
// makes exports stale, the same body does not.
func TestBuiltinModelFingerprint(t *testing.T) {
	useTestBodies(t)
	cam := objectID(1)
	scene := func(builtin string) creativescene.Scene {
		return creativescene.Scene{Objects: []creativescene.Object{
			{ID: cam, Kind: creativescene.KindCamera, Camera: &creativescene.Camera{}},
			{ID: objectID(2), Kind: creativescene.KindModelInstance, Model: &creativescene.ModelRef{Builtin: builtin}},
		}}
	}
	her, _ := creativescene.Fingerprint(scene(testFemale), cam, creativescene.PassClay)
	again, _ := creativescene.Fingerprint(scene(testFemale), cam, creativescene.PassClay)
	him, _ := creativescene.Fingerprint(scene(testMale), cam, creativescene.PassClay)
	if her == "" || her != again || her == him {
		t.Fatalf("fingerprints %s %s %s", her, again, him)
	}
}

// AC-9: the assistant places a body without writing its materials, and
// swaps to the other taking the other's; slots it writes name the body's.
func TestAgentPlacesBuiltinModels(t *testing.T) {
	useTestBodies(t)
	f := setup(t)
	doc := createScene(t, f.a)
	her := objectID(1)
	edit := func(ops ...any) error {
		_, err := agentEdit(t, f.a, creativescene.EditInput{DocumentID: doc, RunID: "ccrn_agent", BasedOn: int64(readScene(t, f.a, doc).Revision), Operations: raws(t, ops...)})
		return err
	}
	placed := instance(her, 1, map[string]any{"builtin": testFemale})
	delete(placed, "material_slots")
	if err := edit(op{"op": "add_objects", "objects": []any{placed}}); err != nil {
		t.Fatal(err)
	}
	slots := readScene(t, f.a, doc).object(t, her).MaterialSlots
	if len(slots) != 2 || slots["body"].Params["roughness"] != 0.6 || slots["eyes"].Params["color"] != "#222222" {
		t.Fatalf("placed with %+v", slots)
	}

	if err := edit(op{"op": "set_instance_model", "object_id": her, "model": map[string]any{"builtin": testMale}}); err != nil {
		t.Fatal(err)
	}
	o := readScene(t, f.a, doc).object(t, her)
	if o.Model.Builtin != testMale || len(o.MaterialSlots) != 1 || o.MaterialSlots["body"].Params["roughness"] != 0.4 {
		t.Fatalf("swapped to %+v with %+v", o.Model, o.MaterialSlots)
	}

	got, err := agentRead(t, f.a, creativescene.ReadInput{DocumentID: doc})
	if err != nil {
		t.Fatal(err)
	}
	var view *creativescene.ModelView
	for _, o := range got.Objects {
		if o.ID == her {
			view = o.Model
		}
	}
	if view == nil || view.Builtin != testMale || view.ContentRevisionID != "" || !view.Usable || view.NameHint != "测试男素体" || view.BoundsM == nil || len(view.Slots) != 1 {
		t.Fatalf("read %+v", view)
	}

	paint := map[string]any{"preset": "paint@1", "params": map[string]any{}}
	for name, tc := range map[string]struct {
		op   any
		says string
	}{
		"slot he lacks": {op{"op": "update_objects", "updates": []any{map[string]any{"object_id": her, "material_slots": map[string]any{"eyes": paint}}}}, "可用：body"},
		"swap with her slots wrong": {op{"op": "set_instance_model", "object_id": her, "model": map[string]any{"builtin": testFemale},
			"material_slots": map[string]any{"hair": paint}}, "可用：body、eyes"},
		"swap with an unknown material": {op{"op": "set_instance_model", "object_id": her, "model": map[string]any{"builtin": testFemale},
			"material_slots": map[string]any{"body": map[string]any{"preset": "glass@1", "params": map[string]any{}}}}, "未知材质"},
	} {
		if err := edit(tc.op); !errors.Is(err, creativeops.ErrValidation) || !strings.Contains(err.Error(), tc.says) {
			t.Errorf("%s: %v, want it to say %q", name, err, tc.says)
		}
	}
	// Written slots are kept as written.
	if err := edit(op{"op": "set_instance_model", "object_id": her, "model": map[string]any{"builtin": testFemale}, "material_slots": map[string]any{"body": paint}}); err != nil {
		t.Fatal(err)
	}
	if slots := readScene(t, f.a, doc).object(t, her).MaterialSlots; len(slots) != 1 || slots["body"].Preset != "paint@1" {
		t.Fatalf("written slots %+v", slots)
	}
}

func TestFindBuiltinModels(t *testing.T) {
	useTestBodies(t)
	ids := func(q string) string {
		var out []string
		for _, m := range creativescene.FindBuiltinModels(q) {
			out = append(out, m.ID)
		}
		return strings.Join(out, " ")
	}
	for q, want := range map[string]string{"女": testFemale, "男": testMale, "素体": testFemale + " " + testMale, "人形": testFemale + " " + testMale, "HUMANOID": testFemale + " " + testMale, "猫": "", "": testFemale + " " + testMale} {
		if got := ids(q); got != want {
			t.Errorf("%q: %q, want %q", q, got, want)
		}
	}
	m := creativescene.FindBuiltinModels("女")[0]
	if m.Title != "测试女素体" || m.Mount != "floor" || !m.Humanoid || m.CategoryLabel != "人形" || m.SizeM[1] != 1.68 {
		t.Fatalf("%+v", m)
	}
	facts, ok := creativescene.BuiltinModelByID(testFemale)
	if !ok || len(facts.Slots) != 2 || facts.Materials["eyes"].Preset != "principled@1" || facts.Source.License != "CC0" {
		t.Fatalf("%+v", facts)
	}
	if thumb, ok := creativescene.BuiltinModelThumbnail(testFemale); !ok || len(thumb) == 0 {
		t.Fatal("no thumbnail")
	}
	if _, ok := creativescene.BuiltinModelByID("wood_floor@1"); ok {
		t.Fatal("a material is no built-in model")
	}
}
