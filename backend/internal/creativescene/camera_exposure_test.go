package creativescene_test

import (
	"reflect"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativedocument"
	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
)

// Epic revision v5 (data: 摄像机曝光): a camera written without an exposure
// mode is exposed automatically; one stored before the mode existed was
// exposed by hand (W2) and reads as manual, so its pictures do not change.
// Merging works on the completed camera: changing only its ISO keeps it
// manual.
func TestCameraExposureModeDefaults(t *testing.T) {
	f := setup(t)
	doc := createScene(t, f.a)
	fresh, old := objectID(1), objectID(2)
	addObjects(t, f.a, doc, withContent(fresh, "camera", 1, map[string]any{}), withContent(old, "camera", 2, map[string]any{"iso": 400}))
	if got := storedContent(t, f.a, doc, fresh, "camera"); got["exposure_mode"] != "auto" || got["exposure_compensation"] != 0.0 {
		t.Fatalf("a camera written without exposure: %v", got)
	}
	// As W2 stored it: no mode, no compensation.
	if _, err := f.db.Exec(`
		UPDATE creative_content_revisions r SET payload = jsonb_set(r.payload, '{scene,objects}', (
			SELECT jsonb_agg(CASE WHEN o->>'id' = $1
				THEN jsonb_set(o, '{camera}', (o->'camera') - 'exposure_mode' - 'exposure_compensation')
				ELSE o END ORDER BY n)
			FROM jsonb_array_elements(r.payload->'scene'->'objects') WITH ORDINALITY AS t(o, n)))
		WHERE r.account_id = 'scene-a' AND r.payload ? 'scene'`, old); err != nil {
		t.Fatal(err)
	}
	if got := storedContent(t, f.a, doc, old, "camera"); got["exposure_mode"] != "manual" || got["exposure_compensation"] != 0.0 || got["iso"] != 400.0 {
		t.Fatalf("a camera stored before the exposure mode: %v", got)
	}
	update := func(fields map[string]any) error {
		v := readScene(t, f.a, doc)
		_, err := send(t, f.a, batch{DocumentID: doc, Expected: v.expect(content(old)), Operations: []any{
			op{"op": "update_objects", "updates": []any{map[string]any{"object_id": old, "camera": fields}}}}})
		return err
	}
	if err := update(map[string]any{"iso": 800}); err != nil {
		t.Fatal(err)
	}
	if got := storedContent(t, f.a, doc, old, "camera"); got["exposure_mode"] != "manual" || got["iso"] != 800.0 {
		t.Fatalf("an old camera after changing its ISO: %v", got)
	}
	if err := update(map[string]any{"exposure_mode": "auto", "exposure_compensation": -1.5}); err != nil {
		t.Fatal(err)
	}
	if got := storedContent(t, f.a, doc, old, "camera"); got["exposure_mode"] != "auto" || got["exposure_compensation"] != -1.5 || got["iso"] != 800.0 {
		t.Fatalf("switched to auto: %v", got)
	}

	before := readScene(t, f.a, doc).Revision
	for name, tc := range map[string]struct {
		patch map[string]any
		field string
	}{
		"unknown mode":            {map[string]any{"exposure_mode": "program"}, "exposure_mode"},
		"empty mode":              {map[string]any{"exposure_mode": ""}, "exposure_mode"},
		"compensation too bright": {map[string]any{"exposure_compensation": 5.5}, "exposure_compensation"},
		"compensation too dark":   {map[string]any{"exposure_compensation": -6}, "exposure_compensation"},
	} {
		t.Run(name, func(t *testing.T) {
			err := update(tc.patch)
			wantErr(t, err, creativeops.ErrValidation)
			wantMessage(t, err, tc.field)
		})
	}
	if after := readScene(t, f.a, doc).Revision; after != before {
		t.Fatalf("refused updates wrote revision %d", after)
	}
}

// The default camera's exposure mode reads as auto, also for a render stored
// before it existed, and set_render takes the mode or the compensation alone.
func TestDefaultCameraExposureMode(t *testing.T) {
	f := setup(t)
	doc := createScene(t, f.a)
	if _, err := f.db.Exec(`UPDATE creative_content_revisions SET payload = jsonb_set(payload, '{scene,render}', '{"mode":"lit","exposure_ev100":-3}') WHERE account_id = 'scene-a' AND payload ? 'scene'`); err != nil {
		t.Fatal(err)
	}
	raw, err := creativedocument.Read(t.Context(), f.a, doc)
	if err != nil {
		t.Fatal(err)
	}
	if got := renderOf(t, raw); got["exposure_mode"] != "auto" || got["exposure_compensation"] != 0.0 || got["exposure_ev100"] != -3.0 {
		t.Fatalf("an old render reads %v", got)
	}
	set := func(render map[string]any) error {
		v := readScene(t, f.a, doc)
		_, err := send(t, f.a, batch{DocumentID: doc, Expected: v.expect("render"), Operations: []any{op{"op": "set_render", "render": render}}})
		return err
	}
	if err := set(map[string]any{"exposure_mode": "manual"}); err != nil {
		t.Fatal(err)
	}
	if err := set(map[string]any{"exposure_compensation": 2}); err != nil {
		t.Fatal(err)
	}
	want := creativescene.Render{Mode: "lit", Engine: creativescene.EngineWorkbench, Resolution: "2k", ExposureEV100: -3, ExposureMode: creativescene.ExposureManual, ExposureCompensation: 2}
	if r := readScene(t, f.a, doc).Scene.Render; !reflect.DeepEqual(r, want) {
		t.Fatalf("render %+v, want %+v", r, want)
	}
	for name, render := range map[string]map[string]any{
		"unknown mode":      {"exposure_mode": "program"},
		"too bright":        {"exposure_compensation": 5.1},
		"too dark":          {"exposure_compensation": -5.1},
		"null compensation": {"exposure_compensation": nil},
	} {
		t.Run(name, func(t *testing.T) { wantErr(t, set(render), creativeops.ErrValidation) })
	}
}
