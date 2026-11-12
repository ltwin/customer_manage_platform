package creativescene_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativedocument"
	"github.com/samson/customer-manage-platform/backend/internal/creativeglb"
	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
)

// rigVector is a skeleton of the shared vectors (testdata/humanoid_rig.json)
// and the rig of one of its cases.
func rigVector(t *testing.T, caseName string) (creativeglb.Skeleton, creativescene.Rig) {
	t.Helper()
	var doc struct {
		Skeletons map[string]creativeglb.Skeleton `json:"skeletons"`
		Cases     []struct {
			Name     string            `json:"name"`
			Skeleton string            `json:"skeleton"`
			Rig      creativescene.Rig `json:"rig"`
		} `json:"cases"`
	}
	raw, err := os.ReadFile(filepath.Join("testdata", "humanoid_rig.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	for _, c := range doc.Cases {
		if c.Name == caseName {
			return doc.Skeletons[c.Skeleton], c.Rig
		}
	}
	t.Fatalf("no vector %q", caseName)
	return creativeglb.Skeleton{}, creativescene.Rig{}
}

// rigged is basic.glb's manifest with a skeleton, as the worker would stage a
// skinned GLB with that structure.
func rigged(t *testing.T, sk creativeglb.Skeleton) creativeglb.Manifest {
	t.Helper()
	m := basicManifest(t)
	m.Skeleton = &sk
	return m
}

func wantRigField(t *testing.T, err error, field, reason string) {
	t.Helper()
	var fe *creativeops.FieldError
	if !errors.As(err, &fe) || !errors.Is(err, creativeops.ErrValidation) || fe.Field() != field || fe.Reason != reason {
		t.Fatalf("got %v, want %s %s", err, field, reason)
	}
}

// The import takes the rig the browser found and checks it against the
// skeleton the server computed; a rigged model keeps it through a library
// copy, and is no source for a face assignment (AC-4, AC-10).
func TestImportedRig(t *testing.T) {
	f := setup(t)

	// AC-4: the browser's check bypassed, RobotExpressive's feet under its
	// IK root still fail.
	robot, unembedded := rigVector(t, "robot_expressive: Body 作 hips，脚挂在控制骨下")
	u := stageImport(t, f)
	u.glb = stageWith(t, f, f.a, "model", rigged(t, robot))
	in := u.input(t, f)
	in.Rig = &unembedded
	_, _, err := importModel(t, f.a, command(t, in))
	wantRigField(t, err, "rig.humanoid.leftFoot", "hierarchy")

	// A rig on a GLB without bones.
	plain := stageImport(t, f)
	in = plain.input(t, f)
	in.Rig = &creativescene.Rig{}
	_, _, err = importModel(t, f.a, command(t, in))
	wantRigField(t, err, "rig", "no_skeleton")

	// Both rejected imports left their uploads usable: the same ones import.
	in = u.input(t, f)
	in.Rig = nil
	if _, _, err := importModel(t, f.a, command(t, in)); err != nil {
		t.Fatalf("a robot without a humanoid mapping: %v", err)
	}

	xbot, rig := rigVector(t, "xbot: 穿刺①/素体检查的映射")
	rig.Constraints = []creativescene.Constraint{{Node: rig.Humanoid["leftLowerArm"], Kind: "rotation", Source: rig.Humanoid["leftHand"], Weight: 0.5}}
	u = stageImport(t, f)
	u.glb = stageWith(t, f, f.a, "model", rigged(t, xbot))
	in = u.input(t, f)
	in.Rig = &rig
	_, asset, err := importModel(t, f.a, command(t, in))
	if err != nil {
		t.Fatal(err)
	}
	imported, p := readRevision(t, f.a, asset.ContentRevisionID)
	if p.Rig == nil || !reflect.DeepEqual(*p.Rig, rig) || p.Manifest.Skeleton == nil || p.Manifest.Skeleton.Fingerprint != xbot.Fingerprint {
		t.Fatalf("imported rig %+v skeleton %+v", p.Rig, p.Manifest.Skeleton)
	}

	doc := createScene(t, f.a)
	id := objectID(1)
	addObjects(t, f.a, doc, instance(id, 1, map[string]any{"content_revision_id": imported.ID}))

	// AC-10: no face assignment on a rigged model.
	v := readScene(t, f.a, doc)
	_, err = send(t, f.a, batch{DocumentID: doc, Expected: v.expect(content(id)), Operations: []any{
		derive("faces", imported.ID, brickFaces(), nil),
		op{"op": "set_instance_model", "object_id": id, "model": map[string]any{"ref": "faces"}},
	}})
	wantErr(t, err, creativeops.ErrValidation)
	if after := readScene(t, f.a, doc); after.Revision != v.Revision {
		t.Fatalf("rejected derive moved the scene to %d", after.Revision)
	}

	// A library copy carries the rig as stored.
	c := command(t, creativescene.SaveModelInput{DocumentID: doc, ObjectID: id, ExpectedObjectRev: contentRev(t, v, id), ThumbnailUploadID: stage(t, f, f.a, "image"),
		Target: creativelibrary.ImportTarget{Title: "Xbot"}})
	_, saved, err := saveModel(t, f.a, c)
	if err != nil {
		t.Fatal(err)
	}
	if _, copied := readRevision(t, f.a, saved.ContentRevisionID); copied.Rig == nil || !reflect.DeepEqual(*copied.Rig, rig) {
		t.Fatalf("library copy rig %+v", copied.Rig)
	}
}

// AC-10: code models carry no bones.
func TestCommitModelRefusesASkinnedGLB(t *testing.T) {
	f := setup(t)
	raw, err := os.ReadFile(filepath.Join("..", "creativeglb", "testdata", "skinned-turned-root.glb"))
	if err != nil {
		t.Fatal(err)
	}
	skinned, err := creativeglb.Inspect(raw, creativeglb.DefaultLimits())
	if err != nil || skinned.Skeleton == nil {
		t.Fatalf("skinned fixture %+v %v", skinned.Skeleton, err)
	}
	doc := createScene(t, f.a)
	v := readScene(t, f.a, doc)
	u := uploads{glb: stageWith(t, f, f.a, "model", skinned), thumb: stage(t, f, f.a, "image")}
	_, err = send(t, f.a, batch{DocumentID: doc, Expected: v.expect("objects"), Operations: []any{
		commit(t, "body", u, pillarGenerator(2), skinned, nil),
		op{"op": "add_objects", "objects": []any{instance(objectID(1), 1, map[string]any{"ref": "body"})}},
	}})
	wantErr(t, err, creativeops.ErrValidation)
	if after := readScene(t, f.a, doc); after.Revision != v.Revision {
		t.Fatalf("rejected commit moved the scene to %d", after.Revision)
	}
}

// set_instance_model's material_slots replaces the instances' slots whole in
// the same content unit; without it the slots stay (Epic 修订 v7).
func TestSetInstanceModelMaterialSlots(t *testing.T) {
	f := setup(t)
	doc := createScene(t, f.a)
	id, twin := objectID(1), objectID(2)
	wood, paint := material("wood@1"), material("paint@1")
	res := mustSend(t, f.a, batch{DocumentID: doc, Expected: readScene(t, f.a, doc).expect("objects"), Operations: []any{
		commit(t, "first", stagePair(t, f, f.a), pillarGenerator(2), basicManifest(t), nil),
		op{"op": "add_objects", "objects": []any{
			slotted(instance(id, 1, map[string]any{"ref": "first"}), map[string]any{"stone": wood, "trim": paint}),
			slotted(instance(twin, 2, map[string]any{"ref": "first"}), map[string]any{"stone": wood}),
		}},
	}})
	first := receiptModels(t, res)["first"].ContentRevisionID
	second := newPillar(t, f, doc, objectID(3), 3).ContentRevisionID

	slotsOf := func(v sceneView, id string) map[string]string {
		out := map[string]string{}
		for name, s := range v.object(t, id).MaterialSlots {
			out[name] = s.Preset
		}
		return out
	}
	swap := func(model string, scope string, slots any) creativedocument.CommandResult {
		t.Helper()
		o := op{"op": "set_instance_model", "object_id": id, "model": map[string]any{"content_revision_id": model}}
		units := []string{content(id)}
		if scope == "all_instances" {
			o["scope"], o["object_ids"] = scope, []string{id, twin}
			units = append(units, content(twin))
		}
		if slots != nil {
			o["material_slots"] = slots
		}
		return mustSend(t, f.a, batch{DocumentID: doc, Expected: readScene(t, f.a, doc).expect(units...), Operations: []any{o}})
	}

	res = swap(second, "all_instances", map[string]any{"stone": paint})
	v := readScene(t, f.a, doc)
	if !reflect.DeepEqual(res.ChangedUnits, []string{content(id), content(twin)}) {
		t.Fatalf("changed %v", res.ChangedUnits)
	}
	for _, o := range []string{id, twin} {
		if got := slotsOf(v, o); !reflect.DeepEqual(got, map[string]string{"stone": "paint@1"}) || v.object(t, o).Model.ContentRevisionID != second {
			t.Fatalf("%s after the swap: %v on %s", o, got, v.object(t, o).Model.ContentRevisionID)
		}
	}
	swap(first, "this", nil)
	if got := slotsOf(readScene(t, f.a, doc), id); !reflect.DeepEqual(got, map[string]string{"stone": "paint@1"}) {
		t.Fatalf("a swap without material_slots changed them: %v", got)
	}
	swap(second, "this", map[string]any{})
	if got := slotsOf(readScene(t, f.a, doc), id); len(got) != 0 {
		t.Fatalf("empty material_slots kept %v", got)
	}

	v = readScene(t, f.a, doc)
	for _, tt := range []struct {
		slots any
		want  error
		field string
	}{
		{map[string]any{"stone": principled(map[string]any{"maps": map[string]any{"glitter": texture("ccrv_missing")}})}, creativeops.ErrValidation, "operations[0].material_slots.stone.maps.glitter"},
		{map[string]any{"stone": principled(map[string]any{"maps": map[string]any{"base_color": texture("ccrv_missing")}})}, creativescene.ErrImageUnavailable, "operations[0].material_slots.stone.maps.base_color.content_revision_id"},
	} {
		_, err := send(t, f.a, batch{DocumentID: doc, Expected: v.expect(content(id)), Operations: []any{
			op{"op": "set_instance_model", "object_id": id, "model": map[string]any{"content_revision_id": first}, "material_slots": tt.slots},
		}})
		wantField(t, err, tt.want, tt.field)
	}
	if after := readScene(t, f.a, doc); after.Revision != v.Revision {
		t.Fatalf("rejected swaps moved the scene to %d", after.Revision)
	}
}
