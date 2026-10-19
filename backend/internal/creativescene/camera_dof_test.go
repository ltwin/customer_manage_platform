package creativescene_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
)

func wantMessage(t *testing.T, err error, part string) {
	t.Helper()
	var public creativeops.Public
	if !errors.As(err, &public) || !strings.Contains(public.PublicMessage(), part) {
		t.Fatalf("want error containing %q, got %v", part, err)
	}
}

// AC-13: the depth of field fields merge by key like the others, and "" clears
// the focus object.
func TestCameraDepthOfFieldMergesByKey(t *testing.T) {
	f := setup(t)
	doc := createScene(t, f.a)
	c, target := objectID(1), objectID(2)
	addObjects(t, f.a, doc, box(target, nil, 1), withContent(c, "camera", 2, map[string]any{"focal_mm": 50}))
	update := func(fields map[string]any) error {
		v := readScene(t, f.a, doc)
		_, err := send(t, f.a, batch{DocumentID: doc, Expected: v.expect(content(c)), Operations: []any{
			op{"op": "update_objects", "updates": []any{map[string]any{"object_id": c, "camera": fields}}}}})
		return err
	}
	base := map[string]any{"focal_mm": 50.0, "sensor_mm": []any{36.0, 24.0}, "aspect": "3:2", "near": 0.1, "far": 1000.0, "label": ""}
	with := func(kv map[string]any) map[string]any {
		out := map[string]any{"depth_of_field": false, "f_stop": 2.8, "focus": 10.0, "focus_object_id": ""}
		for k, v := range base {
			out[k] = v
		}
		for k, v := range kv {
			out[k] = v
		}
		return out
	}
	for _, step := range []struct {
		name  string
		patch map[string]any
		want  map[string]any
	}{
		{"open", map[string]any{"depth_of_field": true, "f_stop": 1.4}, with(map[string]any{"depth_of_field": true, "f_stop": 1.4})},
		{"focus object", map[string]any{"focus_object_id": target}, with(map[string]any{"depth_of_field": true, "f_stop": 1.4, "focus_object_id": target})},
		{"focus distance only", map[string]any{"focus": 3}, with(map[string]any{"depth_of_field": true, "f_stop": 1.4, "focus": 3.0, "focus_object_id": target})},
		{"clear focus object", map[string]any{"focus_object_id": ""}, with(map[string]any{"depth_of_field": true, "f_stop": 1.4, "focus": 3.0})},
	} {
		if err := update(step.patch); err != nil {
			t.Fatalf("%s: %v", step.name, err)
		}
		if got := storedContent(t, f.a, doc, c, "camera"); !reflect.DeepEqual(got, step.want) {
			t.Fatalf("%s: camera = %v, want %v", step.name, got, step.want)
		}
	}

	before := readScene(t, f.a, doc).Revision
	for name, patch := range map[string]map[string]any{
		"f_stop low":  {"f_stop": 0.4},
		"f_stop high": {"f_stop": 65},
		"focus zero":  {"focus": 0},
	} {
		if err := update(patch); !errors.Is(err, creativeops.ErrValidation) {
			t.Fatalf("%s: want validation error, got %v", name, err)
		}
	}
	if after := readScene(t, f.a, doc).Revision; after != before {
		t.Fatalf("refused updates wrote revision %d", after)
	}
}

// AC-13: a focus object must exist after the write, be a primitive, model
// instance or group, and not be the camera itself.
func TestFocusObjectIsCheckedWhenWritten(t *testing.T) {
	f := setup(t)
	doc := createScene(t, f.a)
	c, other, light, empty := objectID(1), objectID(2), objectID(3), objectID(4)
	addObjects(t, f.a, doc,
		withContent(c, "camera", 1, map[string]any{}),
		withContent(other, "camera", 2, map[string]any{}),
		withContent(light, "light", 3, map[string]any{"type": "point"}),
		group(empty, nil, 4))
	update := func(target string) error {
		v := readScene(t, f.a, doc)
		_, err := send(t, f.a, batch{DocumentID: doc, Expected: v.expect(content(c)), Operations: []any{
			op{"op": "update_objects", "updates": []any{map[string]any{"object_id": c, "camera": map[string]any{"focus_object_id": target}}}}}})
		return err
	}
	for target, part := range map[string]string{
		objectID(9): "不存在",
		c:           "不能对焦到自己",
		other:       "只能对焦到",
		light:       "只能对焦到",
		"box":       "不合法",
	} {
		wantMessage(t, update(target), part)
	}
	if err := update(empty); err != nil {
		t.Fatalf("focus on an empty: %v", err)
	}

	// A target listed after the camera counts: the check runs on the scene
	// after the whole batch.
	c2, later := objectID(5), objectID(6)
	addObjects(t, f.a, doc, withContent(c2, "camera", 5, map[string]any{"focus_object_id": later}), box(later, nil, 6))
}

// E7: a focus object removed later never blocks a write, undo or restore.
func TestDanglingFocusObjectBlocksNothing(t *testing.T) {
	f := setup(t)
	doc := createScene(t, f.a)
	c, target := objectID(1), objectID(2)
	addObjects(t, f.a, doc, box(target, nil, 1))
	focused := addObjects(t, f.a, doc, withContent(c, "camera", 2, map[string]any{"depth_of_field": true, "focus_object_id": target})).Revision
	remove(t, f.a, doc, []string{target}, target)
	if got := storedContent(t, f.a, doc, c, "camera")["focus_object_id"]; got != target {
		t.Fatalf("focus_object_id after removal = %v", got)
	}

	v := readScene(t, f.a, doc)
	mustSend(t, f.a, batch{DocumentID: doc, Expected: v.expect(content(c)), Operations: []any{
		op{"op": "update_objects", "updates": []any{map[string]any{"object_id": c, "camera": map[string]any{"f_stop": 4}}}}}})
	// Undo of the f_stop change restores the camera with its dangling reference.
	mustRestore(t, f.a, doc, focused, []string{content(c)})
	// Undo of the removal brings the target back.
	mustRestore(t, f.a, doc, focused, units(target))
	if !readScene(t, f.a, doc).has(target) {
		t.Fatal("target not restored")
	}
}

// Snapshots stored before the depth of field fields existed read back with
// every camera field, and the next write stores them.
func TestOldCameraIsCompletedOnRead(t *testing.T) {
	f := setup(t)
	doc := createScene(t, f.a)
	c := objectID(1)
	addObjects(t, f.a, doc, withContent(c, "camera", 1, map[string]any{"focal_mm": 85}))
	if _, err := f.db.Exec(`
		UPDATE creative_content_revisions r SET payload = jsonb_set(r.payload, '{scene,objects}', (
			SELECT jsonb_agg(CASE WHEN o ? 'camera'
				THEN jsonb_set(o, '{camera}', (o->'camera') - 'depth_of_field' - 'f_stop' - 'focus' - 'focus_object_id')
				ELSE o END ORDER BY n)
			FROM jsonb_array_elements(r.payload->'scene'->'objects') WITH ORDINALITY AS t(o, n)))
		WHERE r.account_id = 'scene-a' AND r.payload ? 'scene'`); err != nil {
		t.Fatal(err)
	}
	var stripped bool
	if err := f.db.QueryRow(`SELECT bool_and(NOT (payload->'scene'->'objects' @> '[{"camera":{"f_stop":2.8}}]')) FROM creative_content_revisions WHERE account_id='scene-a' AND payload ? 'scene'`).Scan(&stripped); err != nil || !stripped {
		t.Fatalf("fixture did not strip the fields: %v %v", stripped, err)
	}
	want := map[string]any{"focal_mm": 85.0, "sensor_mm": []any{36.0, 24.0}, "aspect": "3:2", "near": 0.1, "far": 1000.0, "label": "",
		"depth_of_field": false, "f_stop": 2.8, "focus": 10.0, "focus_object_id": ""}
	if got := storedContent(t, f.a, doc, c, "camera"); !reflect.DeepEqual(got, want) {
		t.Fatalf("read camera = %v, want %v", got, want)
	}
	// A write to another unit validates the whole snapshot, old camera included.
	addObjects(t, f.a, doc, box(objectID(2), nil, 2))
	if got := storedContent(t, f.a, doc, c, "camera"); !reflect.DeepEqual(got, want) {
		t.Fatalf("camera after write = %v, want %v", got, want)
	}
}
