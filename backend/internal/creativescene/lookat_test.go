package creativescene_test

import (
	"math"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativedocument"
	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
)

// lookAtGolden comes from three.js 0.186 (frontend/node_modules/three):
// Object3D.lookAt on a PerspectiveCamera, PointLight or Object3D placed as
// described, its quaternion then normalized.
var lookAtGolden = []struct {
	name        string
	kind        string
	pos, target []float64
	group       map[string]any // transform of the parent group, nil at the root
	want        []float64
}{
	{"root camera", "camera", []float64{3, 2, 5}, []float64{0, 1, 0}, nil,
		[]float64{-0.08174336421824298, 0.26597160570637124, 0.02264160112939285, 0.9602418889334895}},
	{"root light straight below", "light", []float64{0, 5, 0}, []float64{0, 0, 0}, nil,
		[]float64{-0.7070714249637373, 0, 0, 0.7071421356415908}},
	{"root primitive straight above", "primitive", []float64{1, 0, 1}, []float64{1, 5, 1}, nil,
		[]float64{-0.7070714249637373, 0, 0, 0.7071421356415908}},
	{"root primitive", "primitive", []float64{-2, 0.5, 4}, []float64{1, 1, -3}, nil,
		[]float64{-0.006589671611517535, 0.9790514918452171, 0.03210438170536429, 0.2009578593114074}},
	{"camera in rotated scaled group", "camera", []float64{1, 1, 1}, []float64{0, 0, 0},
		map[string]any{"position": []float64{2, 0, -1}, "rotation": groupRotation, "scale": []float64{2, 1, 0.5}},
		[]float64{-0.142746905342013, 0.6218131964703021, 0.009561583640959872, 0.7699871725093484}},
	{"primitive in rotated uniform group", "primitive", []float64{0.5, 0, -1}, []float64{4, 2, 0},
		map[string]any{"position": []float64{-1, 1, 2}, "rotation": groupRotation, "scale": []float64{1.5, 1.5, 1.5}},
		[]float64{-0.129033173613238, 0.4933555904619983, -0.011868494409639118, 0.8601219915265726}},
}

// groupRotation is three.js Euler(0.2, 0.6, -0.1) as a quaternion.
var groupRotation = []float64{0.08055924577961499, 0.298443096891027, -0.018042467247517832, 0.9508503537429719}

func lookAtObject(id, kind string, parent *string, order int, pos []float64) map[string]any {
	var o map[string]any
	switch kind {
	case "camera":
		o = withContent(id, "camera", order, map[string]any{})
	case "light":
		o = withContent(id, "light", order, map[string]any{"type": "point"})
	default:
		o = box(id, nil, order)
	}
	o["parent_id"] = parent
	tr := identity()
	tr["position"] = pos
	o["transform"] = tr
	return o
}

func closeTo(got, want []float64) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if math.Abs(got[i]-want[i]) > 1e-9 {
			return false
		}
	}
	return true
}

// AC-6: look_at gives the rotation three.js Object3D.lookAt gives, through
// add_objects at the root and update_objects inside a group.
func TestLookAtMatchesThreeJS(t *testing.T) {
	f := setup(t)
	for i, tc := range lookAtGolden {
		t.Run(tc.name, func(t *testing.T) {
			me := author{t: t, scope: f.a, doc: createScene(t, f.a), actor: creativedocument.Photographer}
			id := objectID(100 + i)
			var res any
			var err error
			if tc.group == nil {
				item := lookAtObject(id, tc.kind, nil, 1, tc.pos)
				delete(item, "transform")
				item["transform"] = map[string]any{"position": tc.pos}
				item["look_at"] = tc.target
				res, err = agentEdit(t, f.a, creativescene.EditInput{DocumentID: me.doc, RunID: "ccrn_look", BasedOn: 1,
					Operations: raws(t, op{"op": "add_objects", "objects": []any{item}})})
			} else {
				g := objectID(1)
				parent := group(g, nil, 1)
				parent["transform"] = tc.group
				base := me.add(parent, lookAtObject(id, tc.kind, &g, 0, tc.pos))
				res, err = agentEdit(t, f.a, creativescene.EditInput{DocumentID: me.doc, RunID: "ccrn_look", BasedOn: base,
					Operations: raws(t, op{"op": "update_objects", "updates": []any{map[string]any{"object_id": id, "look_at": tc.target}}})})
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := res.(creativescene.EditResult); !ok {
				t.Fatalf("result %#v", res)
			}
			o := readScene(t, f.a, me.doc).object(t, id)
			if !closeTo(o.Transform.Rotation, tc.want) {
				t.Fatalf("rotation %v, want %v", o.Transform.Rotation, tc.want)
			}
			if !closeTo(o.Transform.Position, tc.pos) || !closeTo(o.Transform.Scale, []float64{1, 1, 1}) {
				t.Fatalf("position %v scale %v changed", o.Transform.Position, o.Transform.Scale)
			}
		})
	}
}

// AC-6: look_at is refused when it cannot be resolved unambiguously.
func TestLookAtRefusals(t *testing.T) {
	f := setup(t)
	me := author{t: t, scope: f.a, doc: createScene(t, f.a), actor: creativedocument.Photographer}
	g, c := objectID(1), objectID(2)
	base := me.add(group(g, nil, 1), lookAtObject(c, "camera", &g, 0, []float64{0, 1, 5}))
	update := func(fields map[string]any) map[string]any {
		fields["object_id"] = c
		return fields
	}
	for _, tc := range []struct {
		name, part string
		ops        []any
	}{
		{"rotation too", "不能同时写", []any{op{"op": "update_objects", "updates": []any{update(map[string]any{
			"look_at": []float64{0, 0, 0}, "transform": map[string]any{"rotation": []float64{0, 0, 0, 1}}})}}}},
		{"target at the object", "重合", []any{op{"op": "update_objects", "updates": []any{update(map[string]any{"look_at": []float64{0, 1, 5}})}}}},
		{"parent moved too", "父级在同一次修改里也被移动", []any{op{"op": "update_objects", "updates": []any{
			map[string]any{"object_id": g, "transform": at(1, 0, 0)},
			update(map[string]any{"look_at": []float64{0, 0, 0}})}}}},
		{"bad target", "look_at 须为", []any{op{"op": "update_objects", "updates": []any{update(map[string]any{"look_at": []float64{0, 0}})}}}},
		{"add without position", "transform.position", []any{op{"op": "add_objects", "objects": []any{
			map[string]any{"id": objectID(3), "kind": "group", "name": "", "parent_id": nil, "order": 2, "visible": true, "locked": false, "look_at": []float64{0, 0, 0}}}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := agentEdit(t, f.a, creativescene.EditInput{DocumentID: me.doc, RunID: "ccrn_look", BasedOn: base, Operations: raws(t, tc.ops...)})
			wantErr(t, err, creativeops.ErrValidation)
			wantMessage(t, err, tc.part)
		})
	}
	// look_at is an edit_scene convenience; the scene command refuses it.
	v := readScene(t, f.a, me.doc)
	_, err := send(t, f.a, batch{DocumentID: me.doc, Expected: v.expect(placement(c)), Operations: []any{
		op{"op": "update_objects", "updates": []any{update(map[string]any{"look_at": []float64{0, 0, 0}})}}}})
	wantErr(t, err, creativeops.ErrValidation)

	// Position only plus look_at keeps the scale and moves the camera.
	if _, err := agentEdit(t, f.a, creativescene.EditInput{DocumentID: me.doc, RunID: "ccrn_look", BasedOn: base, Operations: raws(t,
		op{"op": "update_objects", "updates": []any{update(map[string]any{"look_at": []float64{0, 0, 0}, "transform": map[string]any{"position": []float64{2, 1, 5}}})}})}); err != nil {
		t.Fatal(err)
	}
	if o := readScene(t, f.a, me.doc).object(t, c); !closeTo(o.Transform.Position, []float64{2, 1, 5}) {
		t.Fatalf("position %v", o.Transform.Position)
	}
}
