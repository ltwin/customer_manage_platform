package creativescene_test

import (
	"reflect"
	"testing"
)

// data: 光源尺寸: snapshots stored before radius_m and angular_diameter_deg
// existed read back with them at their defaults, and the next write stores
// them (no migration).
func TestOldLightIsCompletedOnRead(t *testing.T) {
	f := setup(t)
	doc := createScene(t, f.a)
	s, d := objectID(1), objectID(2)
	addObjects(t, f.a, doc,
		withContent(s, "light", 1, map[string]any{"type": "spot", "intensity": 400, "cast_shadow": true, "angle": 0.5}),
		withContent(d, "light", 2, map[string]any{"type": "directional"}))
	if _, err := f.db.Exec(`
		UPDATE creative_content_revisions r SET payload = jsonb_set(r.payload, '{scene,objects}', (
			SELECT jsonb_agg(CASE WHEN o ? 'light'
				THEN jsonb_set(o, '{light}', (o->'light') - 'radius_m' - 'angular_diameter_deg')
				ELSE o END ORDER BY n)
			FROM jsonb_array_elements(r.payload->'scene'->'objects') WITH ORDINALITY AS t(o, n)))
		WHERE r.account_id = 'scene-a' AND r.payload ? 'scene'`); err != nil {
		t.Fatal(err)
	}
	var stripped bool
	if err := f.db.QueryRow(`SELECT bool_and(NOT (payload->'scene'->'objects' @> '[{"light":{"radius_m":0.1}}]')) FROM creative_content_revisions WHERE account_id='scene-a' AND payload ? 'scene'`).Scan(&stripped); err != nil || !stripped {
		t.Fatalf("fixture did not strip the fields: %v %v", stripped, err)
	}
	want := map[string]map[string]any{
		s: {"type": "spot", "color": "#ffffff", "intensity": 400.0, "cast_shadow": true, "distance": 0.0, "decay": 2.0, "angle": 0.5, "penumbra": 0.0, "radius_m": 0.1},
		d: {"type": "directional", "color": "#ffffff", "intensity": 1.0, "cast_shadow": false, "angular_diameter_deg": 0.545},
	}
	for id, w := range want {
		if got := storedContent(t, f.a, doc, id, "light"); !reflect.DeepEqual(got, w) {
			t.Fatalf("read light %s = %v, want %v", id, got, w)
		}
	}
	// A write to another unit validates the whole snapshot, old lights included.
	addObjects(t, f.a, doc, box(objectID(3), nil, 3))
	for id, w := range want {
		if got := storedContent(t, f.a, doc, id, "light"); !reflect.DeepEqual(got, w) {
			t.Fatalf("light %s after write = %v, want %v", id, got, w)
		}
	}
}

// data: 光源尺寸: the sizes' ranges; 0 is a light of no size.
func TestLightSizeRanges(t *testing.T) {
	f := setup(t)
	doc := createScene(t, f.a)
	add := func(n int, fields map[string]any) error {
		v := readScene(t, f.a, doc)
		_, err := send(t, f.a, batch{DocumentID: doc, Expected: v.expect("objects"), Operations: []any{
			op{"op": "add_objects", "objects": []any{withContent(objectID(n), "light", n, fields)}}}})
		return err
	}
	for n, fields := range []map[string]any{
		{"type": "point", "radius_m": 0},
		{"type": "spot", "radius_m": 10},
		{"type": "directional", "angular_diameter_deg": 0},
		{"type": "directional", "angular_diameter_deg": 30},
	} {
		if err := add(n+1, fields); err != nil {
			t.Fatalf("%v: %v", fields, err)
		}
	}
	for n, tc := range []struct {
		fields map[string]any
		want   string
	}{
		{map[string]any{"type": "point", "radius_m": 10.5}, "radius_m"},
		{map[string]any{"type": "spot", "radius_m": -0.1}, "radius_m"},
		{map[string]any{"type": "directional", "angular_diameter_deg": 31}, "angular_diameter_deg"},
		{map[string]any{"type": "directional", "angular_diameter_deg": -1}, "angular_diameter_deg"},
	} {
		wantMessage(t, add(n+10, tc.fields), tc.want)
	}
}
