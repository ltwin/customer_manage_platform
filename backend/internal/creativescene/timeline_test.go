package creativescene_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativedocument"
	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// data: 场景时间线 and api: 场景时间线命令 through the command path: markers,
// channels in their objects' units, P8 and restore.

func markerID(n int) string { return fmt.Sprintf("smk_%021d", n) }

func shotMarker(n, frame int, camera string) map[string]any {
	return map[string]any{"id": markerID(n), "name": fmt.Sprintf("镜 %d", n), "frame": frame, "camera_object_id": camera}
}

func setMarkers(markers ...map[string]any) op {
	list := make([]any, len(markers))
	for i, m := range markers {
		list[i] = m
	}
	return op{"op": "set_timeline", "timeline": op{"markers": list}}
}

// keyAt is one animation.set item.
func keyAt(path string, frame int, value any) op {
	return op{"path": path, "frame": frame, "value": value}
}

func animate(id string, patch op) op {
	return op{"op": "update_objects", "updates": []any{op{"object_id": id, "animation": patch}}}
}

func sets(keys ...op) op {
	list := make([]any, len(keys))
	for i, k := range keys {
		list[i] = k
	}
	return op{"set": list}
}

func camera(id string, order int) map[string]any {
	return withContent(id, "camera", order, map[string]any{})
}

// shotScene is a scene with camera cam, box subject and shots 1 and 2 at
// frames 0 and 10 through cam.
func shotScene(t *testing.T, scope store.AccountScope) (doc, cam, subject string) {
	t.Helper()
	doc, cam, subject = createScene(t, scope), objectID(1), objectID(2)
	addObjects(t, scope, doc, camera(cam, 1), box(subject, nil, 2))
	v := readScene(t, scope, doc)
	mustSend(t, scope, batch{DocumentID: doc, Expected: v.expect("timeline"), Operations: []any{setMarkers(shotMarker(2, 10, cam), shotMarker(1, 0, cam))}})
	return doc, cam, subject
}

func shotKeysError(t *testing.T, err error) *creativescene.ShotKeysError {
	t.Helper()
	var gaps *creativescene.ShotKeysError
	if !errors.As(err, &gaps) || !errors.Is(err, creativeops.ErrValidation) {
		t.Fatalf("want a P8 refusal, got %v", err)
	}
	return gaps
}

func TestTimelineMarkers(t *testing.T) {
	f := setup(t)
	doc, cam, subject := shotScene(t, f.a)
	v := readScene(t, f.a, doc)
	if got := v.Scene.Timeline; got == nil || got.FPS != 24 || len(got.Markers) != 2 || got.Markers[0].ID != markerID(1) || got.Markers[1].Frame != 10 {
		t.Fatalf("timeline = %+v, want shots 1 and 2 by frame", got)
	}

	cases := []struct {
		name string
		ops  []any
		want string
	}{
		{"marker bound to a box", []any{setMarkers(shotMarker(1, 0, subject))}, "不是摄像机"},
		{"marker bound to a missing camera", []any{setMarkers(shotMarker(1, 0, objectID(9)))}, "不存在"},
		{"two markers on one frame", []any{setMarkers(shotMarker(1, 0, cam), shotMarker(2, 0, cam))}, "重复"},
		{"empty set_timeline", []any{op{"op": "set_timeline", "timeline": op{}}}, "至少写一项"},
		{"path outside the whitelist", []any{animate(subject, sets(keyAt("name", 0, "x")))}, "不能打关键帧"},
		{"camera path on a box", []any{animate(subject, sets(keyAt("camera.focal_mm", 0, 50), keyAt("camera.focal_mm", 10, 50)))}, "只有摄像机"},
		{"empty animation", []any{animate(subject, op{})}, "至少写一项"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := readScene(t, f.a, doc)
			_, err := send(t, f.a, batch{DocumentID: doc, Expected: v.expect("timeline", placement(subject), content(subject)), Operations: c.ops})
			wantErr(t, err, creativeops.ErrValidation)
			if msg := publicMessage(err); !strings.Contains(msg, c.want) {
				t.Fatalf("message %q, want it to say %q", msg, c.want)
			}
			if after := readScene(t, f.a, doc); after.Revision != v.Revision {
				t.Fatal("a refused batch made a revision")
			}
		})
	}
}

func publicMessage(err error) string {
	var p creativeops.Public
	if errors.As(err, &p) {
		return p.PublicMessage()
	}
	return err.Error()
}

func TestShotKeysAreRequired(t *testing.T) {
	f := setup(t)
	doc, cam, subject := shotScene(t, f.a)
	v := readScene(t, f.a, doc)

	// A first key on shot 2 alone leaves shot 1 to extrapolation (P8).
	_, err := send(t, f.a, batch{DocumentID: doc, Expected: v.expect(placement(subject)), Operations: []any{animate(subject, sets(keyAt("transform.position", 10, []float64{1, 0, 0})))}})
	gaps := shotKeysError(t, err)
	want := []creativescene.ShotKeyGap{{ObjectID: subject, Path: "transform.position", MarkerID: markerID(1), Frame: 0}}
	if !reflect.DeepEqual(gaps.Missing, want) {
		t.Fatalf("missing = %+v, want %+v", gaps.Missing, want)
	}
	if msg := gaps.PublicMessage(); !strings.Contains(msg, "分镜「镜 1」（第 0 帧）") {
		t.Fatalf("message %q does not name the shot", msg)
	}

	// S1 written whole: shot 1 keeps the value before, shot 2 gets the new one;
	// a key off the shots (frame 5) is fine.
	res := mustSend(t, f.a, batch{DocumentID: doc, Expected: v.expect(placement(subject)), Operations: []any{animate(subject, sets(
		keyAt("transform.position", 0, []float64{0, 0, 0}), keyAt("transform.position", 10, []float64{1, 0, 0}), keyAt("transform.position", 5, []float64{2, 0, 0})))}})
	if !reflect.DeepEqual(res.ChangedUnits, []string{placement(subject)}) {
		t.Fatalf("changed %v, want only the placement unit", res.ChangedUnits)
	}
	v = readScene(t, f.a, doc)
	keys := v.object(t, subject).Animation["transform.position"].Keys
	if len(keys) != 3 || keys[1].Frame != 5 || keys[0].Interp != "linear" {
		t.Fatalf("keys = %+v, want frames 0, 5, 10 with interp completed", keys)
	}

	// A camera channel is in the content unit.
	res = mustSend(t, f.a, batch{DocumentID: doc, Expected: v.expect(content(cam)), Operations: []any{animate(cam, sets(keyAt("camera.focal_mm", 0, 35), keyAt("camera.focal_mm", 10, 85)))}})
	if !reflect.DeepEqual(res.ChangedUnits, []string{content(cam)}) {
		t.Fatalf("changed %v, want only the content unit", res.ChangedUnits)
	}

	// A new shot must bring keys for every channel (S2a), here only one of two.
	v = readScene(t, f.a, doc)
	expected := v.expect("timeline", placement(subject), content(cam))
	_, err = send(t, f.a, batch{DocumentID: doc, Expected: expected, Operations: []any{
		setMarkers(shotMarker(1, 0, cam), shotMarker(2, 10, cam), shotMarker(3, 20, cam)),
		animate(subject, sets(keyAt("transform.position", 20, []float64{0, 0, 0}))),
	}})
	if gaps := shotKeysError(t, err); len(gaps.Missing) != 1 || gaps.Missing[0].ObjectID != cam || gaps.Missing[0].Frame != 20 {
		t.Fatalf("missing = %+v, want the camera's focal length at frame 20", gaps.Missing)
	}
	mustSend(t, f.a, batch{DocumentID: doc, Expected: expected, Operations: []any{
		setMarkers(shotMarker(1, 0, cam), shotMarker(2, 10, cam), shotMarker(3, 20, cam)),
		animate(subject, sets(keyAt("transform.position", 20, []float64{0, 0, 0}))),
		animate(cam, sets(keyAt("camera.focal_mm", 20, 35))),
	}})

	// S4: removing shot 2 drops its keys; leaving one behind is fine.
	v = readScene(t, f.a, doc)
	mustSend(t, f.a, batch{DocumentID: doc, Expected: v.expect("timeline", placement(subject)), Operations: []any{
		setMarkers(shotMarker(1, 0, cam), shotMarker(3, 20, cam)),
		animate(subject, op{"delete": []any{op{"path": "transform.position", "frame": 10}}}),
	}})
	// S5: clear removes the channel and the base value takes the shown one.
	v = readScene(t, f.a, doc)
	mustSend(t, f.a, batch{DocumentID: doc, Expected: v.expect(placement(subject)), Operations: []any{
		op{"op": "update_objects", "updates": []any{op{"object_id": subject, "transform": at(2, 0, 0), "animation": op{"clear": []any{"transform.position"}}}}},
	}})
	if o := readScene(t, f.a, doc).object(t, subject); o.Animation != nil || o.Transform.Position[0] != 2 {
		t.Fatalf("after clear: animation %v, position %v", o.Animation, o.Transform.Position)
	}
}

func TestKeysShareTheirUnitsConcurrency(t *testing.T) {
	f := setup(t)
	doc, _, subject := shotScene(t, f.a)
	stale := readScene(t, f.a, doc).expect(placement(subject))
	mustSend(t, f.a, batch{DocumentID: doc, Expected: stale, Operations: []any{animate(subject, sets(keyAt("visible", 0, true), keyAt("visible", 10, false)))}})
	// Another client moving the base value on the old revision conflicts.
	_, err := send(t, f.a, batch{DocumentID: doc, Expected: stale, Operations: []any{op{"op": "update_objects", "updates": []any{op{"object_id": subject, "visible": false}}}}})
	var conflict *creativedocument.ConflictError
	if !errors.As(err, &conflict) || len(conflict.Conflicts) != 1 || conflict.Conflicts[0].Unit != placement(subject) {
		t.Fatalf("want a conflict on %s, got %v", placement(subject), err)
	}
}

func TestFirstTimelineWriteOnAnOldScene(t *testing.T) {
	f := setup(t)
	doc := createScene(t, f.a)
	// A scene made before timelines has no timeline unit.
	if _, err := f.db.Exec(`UPDATE creative_documents SET unit_revs = unit_revs - 'timeline' WHERE id = $1`, doc); err != nil {
		t.Fatal(err)
	}
	v := readScene(t, f.a, doc)
	if _, ok := v.UnitRevs["timeline"]; ok {
		t.Fatal("timeline unit still there")
	}
	if got := v.Scene.Timeline; got == nil || got.FPS != 24 || len(got.Markers) != 0 {
		t.Fatalf("old scene reads timeline %+v, want {24, []}", got)
	}
	res := mustSend(t, f.a, batch{DocumentID: doc, Expected: map[string]any{"timeline": nil}, Operations: []any{op{"op": "set_timeline", "timeline": op{"fps": 30}}}})
	if !slices.Contains(res.ChangedUnits, "timeline") {
		t.Fatalf("changed %v", res.ChangedUnits)
	}
}

func TestRestoreTimelineUnits(t *testing.T) {
	f := setup(t)
	doc, cam, _ := shotScene(t, f.a)
	before := readScene(t, f.a, doc)
	mustSend(t, f.a, batch{DocumentID: doc, Expected: before.expect(placement(cam), content(cam)), Operations: []any{
		animate(cam, sets(keyAt("transform.position", 0, []float64{0, 0, 0}), keyAt("transform.position", 10, []float64{0, 0, 3}),
			keyAt("camera.focal_mm", 0, 35), keyAt("camera.focal_mm", 10, 85))),
	}})
	keyed := readScene(t, f.a, doc)

	// A read of some units carries their channels and the timeline.
	raw, err := creativedocument.ReadRevision(t.Context(), f.a, doc, int64(keyed.Revision), []string{placement(cam), "timeline"})
	if err != nil {
		t.Fatal(err)
	}
	var unitRead struct {
		View struct {
			Scene struct {
				Timeline *creativescene.Timeline `json:"timeline"`
				Objects  []struct {
					Animation map[string]creativescene.Channel `json:"animation"`
				} `json:"objects"`
			} `json:"scene"`
		} `json:"view"`
	}
	if err := json.Unmarshal(raw, &unitRead); err != nil {
		t.Fatal(err)
	}
	if got := unitRead.View.Scene; got.Timeline == nil || len(got.Timeline.Markers) != 2 || len(got.Objects) != 1 ||
		len(got.Objects[0].Animation) != 1 || got.Objects[0].Animation["transform.position"].Keys == nil {
		t.Fatalf("unit read = %s, want the timeline and the position channel only", raw)
	}

	// Undoing the placement unit takes back the position channel only.
	mustRestore(t, f.a, doc, before.Revision, []string{placement(cam)})
	o := readScene(t, f.a, doc).object(t, cam)
	if _, ok := o.Animation["transform.position"]; ok {
		t.Fatal("position channel survived its unit's restore")
	}
	if len(o.Animation["camera.focal_mm"].Keys) != 2 {
		t.Fatalf("content channel touched: %+v", o.Animation)
	}
	mustRestore(t, f.a, doc, keyed.Revision, []string{placement(cam)})
	if len(readScene(t, f.a, doc).object(t, cam).Animation) != 2 {
		t.Fatal("redo did not bring the position channel back")
	}

	// Adding shot 3 with its keys, then undoing only the camera's placement,
	// leaves a gap at frame 20: restores are not checked.
	v := readScene(t, f.a, doc)
	mustSend(t, f.a, batch{DocumentID: doc, Expected: v.expect("timeline", placement(cam), content(cam)), Operations: []any{
		setMarkers(shotMarker(1, 0, cam), shotMarker(2, 10, cam), shotMarker(3, 20, cam)),
		animate(cam, sets(keyAt("transform.position", 20, []float64{0, 0, 6}), keyAt("camera.focal_mm", 20, 50))),
	}})
	mustRestore(t, f.a, doc, v.Revision, []string{placement(cam)})
	if keys := readScene(t, f.a, doc).object(t, cam).Animation["transform.position"].Keys; len(keys) != 2 {
		t.Fatalf("keys %+v, want the two from before shot 3", keys)
	}
	// Undoing the timeline unit takes shot 3 away again.
	mustRestore(t, f.a, doc, v.Revision, []string{"timeline"})
	if got := readScene(t, f.a, doc).Scene.Timeline.Markers; len(got) != 2 {
		t.Fatalf("markers %+v, want shots 1 and 2", got)
	}
}
