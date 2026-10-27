package creativescene_test

import (
	"math"
	"reflect"
	"testing"
)

// data: 面光源: an area light left at type alone is a 0.25 m square of
// 10000 cd/m², and holds exactly its type's fields.
func TestAreaLightDefaults(t *testing.T) {
	f := setup(t)
	doc := createScene(t, f.a)
	a, e := objectID(1), objectID(2)
	addObjects(t, f.a, doc,
		withContent(a, "light", 1, map[string]any{"type": "area"}),
		withContent(e, "light", 2, map[string]any{"type": "area", "shape": "ellipse", "width": 0.6, "height": 0.9, "intensity": 2000, "cast_shadow": true}))
	want := map[string]map[string]any{
		a: {"type": "area", "color": "#ffffff", "intensity": 10000.0, "cast_shadow": false, "shape": "rectangle", "width": 0.25, "height": 0.25},
		e: {"type": "area", "color": "#ffffff", "intensity": 2000.0, "cast_shadow": true, "shape": "ellipse", "width": 0.6, "height": 0.9},
	}
	for id, w := range want {
		if got := storedContent(t, f.a, doc, id, "light"); !reflect.DeepEqual(got, w) {
			t.Fatalf("light %s = %v, want %v", id, got, w)
		}
	}
}

// data: 面光源: the ranges, the intensity's by type, and the fields an area
// light lacks or only it has, each refused by name.
func TestAreaLightRanges(t *testing.T) {
	f := setup(t)
	doc := createScene(t, f.a)
	add := func(n int, fields map[string]any) error {
		v := readScene(t, f.a, doc)
		_, err := send(t, f.a, batch{DocumentID: doc, Expected: v.expect("objects"), Operations: []any{
			op{"op": "add_objects", "objects": []any{withContent(objectID(n), "light", n, fields)}}}})
		return err
	}
	for n, fields := range []map[string]any{
		{"type": "area", "width": 0.01, "height": 20},
		{"type": "area", "intensity": 100000, "shape": "ellipse"},
		{"type": "area", "intensity": 0},
	} {
		if err := add(n+1, fields); err != nil {
			t.Fatalf("%v: %v", fields, err)
		}
	}
	for n, tc := range []struct {
		fields map[string]any
		want   string
	}{
		{map[string]any{"type": "area", "width": 0.005}, "width"},
		{map[string]any{"type": "area", "height": 21}, "height"},
		{map[string]any{"type": "area", "shape": "disk"}, "shape"},
		{map[string]any{"type": "area", "intensity": 100001}, "面光源须有 #rrggbb 颜色和 0…100000 的强度"},
		{map[string]any{"type": "point", "intensity": 1001}, "点光须有 #rrggbb 颜色和 0…1000 的强度"},
		{map[string]any{"type": "area", "radius_m": 0.1}, "面光源没有字段 radius_m，可用字段：type、color、intensity、cast_shadow、shape、width、height"},
		{map[string]any{"type": "area", "distance": 5}, "面光源没有字段 distance"},
		{map[string]any{"type": "point", "width": 1}, "点光没有字段 width"},
		{map[string]any{"type": "directional", "shape": "ellipse"}, "平行光没有字段 shape"},
	} {
		wantMessage(t, add(n+10, tc.fields), tc.want)
	}
}

// data: 面光源「换类型」(Epic I6): a type change to or from an area light that
// leaves out intensity keeps the equivalent luminous intensity, the area at
// the object's world scale (its parents' included), the old area before the
// update and the new after; capped at the new type's top. An intensity
// written wins; to and from a directional light the number stays, capped.
func TestAreaLightTypeChangeKeepsLuminousIntensity(t *testing.T) {
	f := setup(t)
	doc := createScene(t, f.a)
	scaled := func(sx, sy float64) map[string]any {
		tr := identity()
		tr["scale"] = []float64{sx, sy, 1}
		return tr
	}
	parent, flat, slanted := objectID(1), objectID(2), objectID(3)
	g := group(parent, nil, 1)
	g["transform"] = scaled(2, 3)
	// A parent stretched 2 × along X over a child turned 45° about Z: the face
	// is a parallelogram of twice the area.
	h := group(slanted, nil, 2)
	h["transform"] = scaled(2, 1)
	addObjects(t, f.a, doc, g, h, group(flat, nil, 3))
	light := func(n int, parentID string, fields map[string]any) string {
		id := objectID(n)
		o := withContent(id, "light", n, fields)
		o["parent_id"] = parentID
		if parentID == slanted {
			tr := identity()
			tr["rotation"] = []float64{0, 0, math.Sin(math.Pi / 8), math.Cos(math.Pi / 8)}
			o["transform"] = tr
		}
		addObjects(t, f.a, doc, o)
		return id
	}
	update := func(id string, u map[string]any) {
		t.Helper()
		u["object_id"] = id
		v := readScene(t, f.a, doc)
		if _, err := send(t, f.a, batch{DocumentID: doc, Expected: v.expect(placement(id), content(id)), Operations: []any{
			op{"op": "update_objects", "updates": []any{u}}}}); err != nil {
			t.Fatal(err)
		}
	}
	for n, tc := range []struct {
		name          string
		parent        string
		light, change map[string]any
		transform     map[string]any
		wantType      string
		want          float64
	}{
		{"point to area", flat, map[string]any{"type": "point", "intensity": 400}, map[string]any{"type": "area"}, nil, "area", 6400},
		{"area back to spot", flat, map[string]any{"type": "area", "intensity": 6400}, map[string]any{"type": "spot"}, nil, "spot", 400},
		{"to an area of the size written", flat, map[string]any{"type": "point", "intensity": 400}, map[string]any{"type": "area", "width": 0.5, "height": 0.5}, nil, "area", 1600},
		{"ellipse to point", flat, map[string]any{"type": "area", "shape": "ellipse", "width": 1, "height": 1, "intensity": 100}, map[string]any{"type": "point"}, nil, "point", 25 * math.Pi},
		{"under a scaled parent", parent, map[string]any{"type": "point", "intensity": 400}, map[string]any{"type": "area"}, nil, "area", 400 / (0.0625 * 6)},
		{"a parallelogram", slanted, map[string]any{"type": "point", "intensity": 400}, map[string]any{"type": "area"}, nil, "area", 400 / (0.0625 * 2)},
		{"scale and type at once, from area", flat, map[string]any{"type": "area", "intensity": 6400}, map[string]any{"type": "point"}, scaled(2, 2), "point", 400},
		{"scale and type at once, to area", flat, map[string]any{"type": "point", "intensity": 400}, map[string]any{"type": "area"}, scaled(2, 2), "area", 1600},
		{"capped to area", flat, map[string]any{"type": "point", "intensity": 1000}, map[string]any{"type": "area", "width": 0.01, "height": 0.01}, nil, "area", 100000},
		{"capped from area", flat, map[string]any{"type": "area", "width": 2, "height": 2, "intensity": 100000}, map[string]any{"type": "point"}, nil, "point", 1000},
		{"intensity written wins", flat, map[string]any{"type": "point", "intensity": 400}, map[string]any{"type": "area", "intensity": 500}, nil, "area", 500},
		{"directional to area keeps the number", flat, map[string]any{"type": "directional", "intensity": 3}, map[string]any{"type": "area"}, nil, "area", 3},
		{"area to directional capped", flat, map[string]any{"type": "area", "intensity": 5000}, map[string]any{"type": "directional"}, nil, "directional", 1000},
	} {
		id := light(10+n, tc.parent, tc.light)
		u := map[string]any{"light": tc.change}
		if tc.transform != nil {
			u["transform"] = tc.transform
		}
		update(id, u)
		got := storedContent(t, f.a, doc, id, "light")
		if got["type"] != tc.wantType || math.Abs(got["intensity"].(float64)-tc.want) > 1e-9*tc.want {
			t.Fatalf("%s: light = %v, want %s at %v", tc.name, got, tc.wantType, tc.want)
		}
	}
}
