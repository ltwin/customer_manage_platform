package creativescene_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// Epic design「助手写入规则」: the assistant names a shot and the tool layer
// writes the keys S1–S6 need (AC-13, AC-14).

const shotRun = "ccrn_shots"

// shotAgent edits and reads one scene as the assistant, always at the
// current revision.
type shotAgent struct {
	t     *testing.T
	scope store.AccountScope
	doc   string
}

func (a shotAgent) edit(shot string, ops ...any) (creativescene.EditResult, error) {
	a.t.Helper()
	got, err := agentEdit(a.t, a.scope, creativescene.EditInput{DocumentID: a.doc, RunID: shotRun, Shot: shot,
		BasedOn: int64(readScene(a.t, a.scope, a.doc).Revision), Operations: raws(a.t, ops...)})
	if err != nil {
		return creativescene.EditResult{}, err
	}
	res, ok := got.(creativescene.EditResult)
	if !ok {
		a.t.Fatalf("edit at %q: %#v", shot, got)
	}
	return res, nil
}

func (a shotAgent) mustEdit(shot string, ops ...any) creativescene.EditResult {
	a.t.Helper()
	res, err := a.edit(shot, ops...)
	if err != nil {
		a.t.Fatalf("edit at %q: %v", shot, err)
	}
	return res
}

func (a shotAgent) read(shot string) creativescene.SceneRead {
	a.t.Helper()
	r, err := agentRead(a.t, a.scope, creativescene.ReadInput{DocumentID: a.doc, Shot: shot})
	if err != nil {
		a.t.Fatal(err)
	}
	return r
}

func (a shotAgent) object(shot, id string) creativescene.ObjectView {
	a.t.Helper()
	for _, o := range a.read(shot).Objects {
		if o.ID == id {
			return o
		}
	}
	a.t.Fatalf("object %s missing at %q", id, shot)
	return creativescene.ObjectView{}
}

func addShot(name, camera string, more ...string) op {
	o := op{"op": "add_shot", "name": name, "camera_object_id": camera}
	if len(more) > 0 {
		o["copy_from"] = more[0]
	}
	return o
}

func TestAssistantBuildsShots(t *testing.T) {
	f := setup(t)
	doc := createScene(t, f.a)
	cam, subject, wall := objectID(1), objectID(2), objectID(3)
	addObjects(t, f.a, doc, camera(cam, 1), box(subject, nil, 2), box(wall, nil, 3))
	a := shotAgent{t: t, scope: f.a, doc: doc}

	// Three shots in one edit, ten frames apart.
	res := a.mustEdit("", addShot("回头", cam), addShot("前进", cam), addShot("拔剑", cam))
	if len(res.AddedShots) != 3 {
		t.Fatalf("added %v", res.AddedShots)
	}
	one, two, three := res.AddedShots[0], res.AddedShots[1], res.AddedShots[2]
	tl := a.read("").Timeline
	if tl == nil || len(tl.Shots) != 3 || tl.Shots[1].ID != two || tl.Shots[1].Frame != 10 || tl.Shots[2].Frame != 20 || tl.Shots[0].CameraName == "" {
		t.Fatalf("timeline %+v", tl)
	}

	// AC-13: in shot 2 the subject steps forward and the lens goes to 85 mm.
	a.mustEdit(two,
		op{"op": "update_objects", "updates": []any{
			map[string]any{"object_id": subject, "transform": at(0, 0, -2)},
			map[string]any{"object_id": cam, "camera": map[string]any{"focal_mm": 85}},
		}})
	for _, c := range []struct {
		shot  string
		z     float64
		focal float64
	}{{one, 0, 35}, {two, -2, 85}, {three, 0, 35}} {
		if z := a.object(c.shot, subject).Transform.Position[2]; z != c.z {
			t.Errorf("subject at %s: z %v, want %v", c.shot, z, c.z)
		}
		if focal := *a.object(c.shot, cam).Camera.FocalMM; focal != c.focal {
			t.Errorf("camera at %s: %v mm, want %v", c.shot, focal, c.focal)
		}
	}
	if got := a.object("", subject).Animated; !slices.Equal(got, []string{"transform.position"}) {
		t.Errorf("subject animated %v, want only the position", got)
	}
	if r := a.read(two); r.AtShot != two || len(r.ShotKeyGaps) != 0 {
		t.Errorf("read at shot 2: at %q, gaps %v", r.AtShot, r.ShotKeyGaps)
	}

	// AC-14: without a shot a keyed property is refused, the set is not.
	before := readScene(t, f.a, doc).Revision
	_, err := a.edit("", moveOp(subject, 5))
	if !errors.Is(err, creativeops.ErrValidation) || !strings.Contains(publicMessage(err), subject+" 的 transform.position") {
		t.Fatalf("unkeyed write to a keyed property: %v", err)
	}
	if readScene(t, f.a, doc).Revision != before {
		t.Fatal("a refused edit was written")
	}
	a.mustEdit("", moveOp(wall, 4))
	for _, s := range []string{one, two, three} {
		if x := a.object(s, wall).Transform.Position[0]; x != 4 {
			t.Errorf("wall at %s: x %v, want 4 in every shot", s, x)
		}
	}

	// S2b copies shot 2; S2a starts from the default state.
	res = a.mustEdit("", addShot("前进 B", cam, two), addShot("全景", cam))
	copied, fresh := res.AddedShots[0], res.AddedShots[1]
	if z := a.object(copied, subject).Transform.Position[2]; z != -2 || *a.object(copied, cam).Camera.FocalMM != 85 {
		t.Errorf("copy of shot 2: z %v", z)
	}
	if z := a.object(fresh, subject).Transform.Position[2]; z != 0 || *a.object(fresh, cam).Camera.FocalMM != 35 {
		t.Errorf("new shot: z %v", z)
	}

	// S3 and S4: moving shot 2 keeps its picture, removing shot 3 leaves no gap.
	a.mustEdit("", op{"op": "update_shot", "id": two, "frame": 15}, op{"op": "remove_shot", "id": three})
	if z := a.object(two, subject).Transform.Position[2]; z != -2 {
		t.Errorf("moved shot 2: z %v", z)
	}
	if r := a.read(""); len(r.Timeline.Shots) != 4 || len(r.ShotKeyGaps) != 0 {
		t.Errorf("after move and remove: %+v, gaps %v", r.Timeline, r.ShotKeyGaps)
	}

	// S6: a card only in shot 2, absent from a shot made after.
	card := objectID(4)
	a.mustEdit(two, op{"op": "add_objects", "objects": []any{func() map[string]any { o := box(card, nil, 4); o["only_in_shot"] = true; return o }()}})
	if !*a.object(two, card).Visible || *a.object(one, card).Visible || *a.object("", card).Visible {
		t.Error("the card is not only in shot 2")
	}
	res = a.mustEdit("", addShot("收尾", cam))
	if *a.object(res.AddedShots[0], card).Visible {
		t.Error("the card shows in a new shot")
	}

	// look_at at a shot keys the rotation there only.
	rotation := a.object(one, cam).Transform.Rotation
	a.mustEdit(two, op{"op": "update_objects", "updates": []any{map[string]any{"object_id": cam, "look_at": []float64{3, 0, -5}}}})
	if got := a.object(one, cam).Transform.Rotation; !slices.Equal(got, rotation) {
		t.Errorf("look_at at shot 2 turned the camera in shot 1: %v", got)
	}
	if slices.Equal(a.object(two, cam).Transform.Rotation, rotation) {
		t.Error("look_at at shot 2 did not turn the camera there")
	}

	for name, c := range map[string]struct {
		shot string
		ops  []any
		want string
	}{
		"shots with a shot":          {two, []any{addShot("x", cam)}, "单独提交"},
		"only_in_shot alone":         {"", []any{op{"op": "add_objects", "objects": []any{func() map[string]any { o := box(objectID(9), nil, 9); o["only_in_shot"] = true; return o }()}}}, "要和 shot 一起用"},
		"raw keys":                   {"", []any{op{"op": "update_objects", "updates": []any{map[string]any{"object_id": wall, "animation": map[string]any{"set": []any{map[string]any{"path": "visible", "frame": 0, "value": false}}}}}}}, "只能写 clear"},
		"clearing at a shot":         {two, []any{clearPosition(subject, 0)}, "单独提交"},
		"set_timeline":               {"", []any{op{"op": "set_timeline", "timeline": map[string]any{"fps": 30}}}, "add_shot"},
		"missing shot":               {"smk_000000000000000000009", []any{moveOp(subject, 1)}, "不存在"},
		"taken frame":                {"", []any{op{"op": "add_shot", "name": "x", "frame": 0, "camera_object_id": cam}}, "已经是分镜"},
		"reparenting a keyed object": {"", []any{op{"op": "reparent_objects", "create_groups": []any{group(objectID(8), nil, 8)}, "moves": []any{map[string]any{"object_id": subject, "parent_id": objectID(8)}}}}, "改挂会让"},
	} {
		if _, err := a.edit(c.shot, c.ops...); !errors.Is(err, creativeops.ErrValidation) || !strings.Contains(publicMessage(err), c.want) {
			t.Errorf("%s: %v, want it to say %q", name, err, c.want)
		}
	}

	// S5: cleared with its base value written, the subject stands where shot
	// 2 had it in every shot, and can be reparented now.
	a.mustEdit("", clearPosition(subject, -2))
	for _, s := range a.read("").Timeline.Shots {
		if z := a.object(s.ID, subject).Transform.Position[2]; z != -2 {
			t.Errorf("cleared subject at %s: z %v, want -2", s.ID, z)
		}
	}
	if got := a.object("", subject).Animated; len(got) != 0 {
		t.Errorf("subject still animated %v", got)
	}
	a.mustEdit("", op{"op": "reparent_objects", "create_groups": []any{group(objectID(8), nil, 8)}, "moves": []any{map[string]any{"object_id": subject, "parent_id": objectID(8)}}})
}

// clearPosition clears id's position channel and puts it at z everywhere.
func clearPosition(id string, z float64) op {
	return op{"op": "update_objects", "updates": []any{map[string]any{"object_id": id, "transform": at(0, 0, z), "animation": map[string]any{"clear": []any{"transform.position"}}}}}
}

// The baseline check covers what the expansion touches: a shot added on an
// old baseline conflicts with the photographer's change to a keyed camera.
func TestShotExpansionIsCheckedAgainstTheBaseline(t *testing.T) {
	f := setup(t)
	doc, cam, _ := shotScene(t, f.a)
	v := readScene(t, f.a, doc)
	mustSend(t, f.a, batch{DocumentID: doc, Expected: v.expect(content(cam)), Operations: []any{animate(cam, sets(keyAt("camera.focal_mm", 0, 35), keyAt("camera.focal_mm", 10, 50)))}})
	base := int64(readScene(t, f.a, doc).Revision)
	v = readScene(t, f.a, doc)
	mustSend(t, f.a, batch{DocumentID: doc, Expected: v.expect(content(cam)), Operations: []any{op{"op": "update_objects", "updates": []any{map[string]any{"object_id": cam, "camera": map[string]any{"label": "A 机"}}}}}})

	got, err := agentEdit(t, f.a, creativescene.EditInput{DocumentID: doc, RunID: shotRun, BasedOn: base, Operations: raws(t, addShot("新", cam))})
	if err != nil {
		t.Fatal(err)
	}
	conflict, ok := got.(*creativescene.WriteConflict)
	if !ok || !slices.ContainsFunc(conflict.Conflicts, func(c creativescene.ConflictEntry) bool { return c.ObjectID == cam && c.Part == "内容" }) {
		t.Fatalf("want a conflict on the camera's content, got %#v", got)
	}
}
