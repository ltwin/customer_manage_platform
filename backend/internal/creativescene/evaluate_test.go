package creativescene

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"testing"
)

// patterns: 关键帧求值 — the vectors are shared with the frontend evaluator
// (make generate copies them to timeline/keyframe_eval.gen.json).

func TestKeyframeVectors(t *testing.T) {
	var doc struct {
		Tolerance float64 `json:"tolerance"`
		Cases     []struct {
			Name   string `json:"name"`
			Kind   string `json:"kind"`
			Keys   []Key  `json:"keys"`
			Expect []struct {
				Frame int             `json:"frame"`
				Value json.RawMessage `json:"value"`
			} `json:"expect"`
		} `json:"cases"`
	}
	readJSON(t, "keyframe_eval.json", &doc)
	kinds := map[string]sampleKind{
		"number": sampleNumber, "vector": sampleVector,
		"quaternion": sampleQuaternion, "discrete": sampleDiscrete,
	}
	for _, c := range doc.Cases {
		kind, ok := kinds[c.Kind]
		if !ok {
			t.Fatalf("%s: unknown kind %q", c.Name, c.Kind)
		}
		for _, e := range c.Expect {
			var got any
			switch kind {
			case sampleNumber:
				var f *float64
				mustSet(t, &f, kind, c.Keys, e.Frame)
				if f != nil {
					got = []float64{*f}
				}
			case sampleVector, sampleQuaternion:
				var v []float64
				mustSet(t, &v, kind, c.Keys, e.Frame)
				if v != nil {
					got = v
				}
			default:
				var v any
				mustSet(t, &v, kind, c.Keys, e.Frame)
				got = v
			}
			if !sameValue(got, e.Value, kind, doc.Tolerance) {
				t.Errorf("%s @%d: got %v, want %s", c.Name, e.Frame, got, e.Value)
			}
		}
	}
}

func mustSet(t *testing.T, field any, kind sampleKind, keys []Key, frame int) {
	t.Helper()
	if err := setSample(field, kind, keys, frame); err != nil {
		t.Fatal(err)
	}
}

func sameValue(got any, want json.RawMessage, kind sampleKind, tol float64) bool {
	if string(want) == "null" {
		return got == nil
	}
	if kind == sampleDiscrete {
		var w any
		_ = json.Unmarshal(want, &w)
		return reflect.DeepEqual(got, w)
	}
	g, ok := got.([]float64)
	w, err := decodeNumbers(want)
	if !ok || err != nil || len(g) != len(w) {
		return false
	}
	for i := range g {
		if math.Abs(g[i]-w[i]) > tol {
			return false
		}
	}
	return true
}

func TestEvaluateAt(t *testing.T) {
	focal := 35.0
	cam := Object{
		ID: "sobj_cam", Kind: KindCamera, Visible: true,
		Transform: Transform{Position: []float64{0, 1, 5}, Rotation: []float64{0, 0, 0, 1}, Scale: []float64{1, 1, 1}},
		Camera:    &Camera{FocalMM: &focal, SensorMM: []float64{36, 24}},
		Animation: map[string]Channel{
			"transform.position": {Keys: []Key{
				{Frame: 0, Value: json.RawMessage(`[0,1,5]`), Interp: "linear"},
				{Frame: 10, Value: json.RawMessage(`[10,1,5]`), Interp: "linear"},
			}},
			"camera.focal_mm": {Keys: []Key{
				{Frame: 0, Value: json.RawMessage(`35`), Interp: "constant"},
				{Frame: 10, Value: json.RawMessage(`85`), Interp: "linear"},
			}},
			"camera.sensor_mm": {Keys: []Key{{Frame: 10, Value: json.RawMessage(`[23.5,15.6]`), Interp: "constant"}}},
			"visible":          {Keys: []Key{{Frame: 0, Value: json.RawMessage(`true`)}, {Frame: 10, Value: json.RawMessage(`false`)}}},
		},
	}
	box := Object{ID: "sobj_box", Kind: KindPrimitive, Visible: true, Transform: cam.Transform}
	s := Scene{Timeline: &Timeline{FPS: 24}, Objects: []Object{cam, box}}
	before, _ := json.Marshal(s)

	at5 := EvaluateAt(s, 5)
	c := at5.Objects[0]
	if !reflect.DeepEqual(c.Transform.Position, []float64{5, 1, 5}) {
		t.Errorf("position @5 = %v", c.Transform.Position)
	}
	if *c.Camera.FocalMM != 35 || !c.Visible || !reflect.DeepEqual(c.Camera.SensorMM, []float64{23.5, 15.6}) {
		t.Errorf("camera @5 = focal %v visible %v sensor %v", *c.Camera.FocalMM, c.Visible, c.Camera.SensorMM)
	}
	if c.Animation != nil || at5.Timeline != nil {
		t.Error("evaluated scene keeps animation or timeline")
	}
	at10 := EvaluateAt(s, 10).Objects[0]
	if *at10.Camera.FocalMM != 85 || at10.Visible {
		t.Errorf("camera @10 = focal %v visible %v", *at10.Camera.FocalMM, at10.Visible)
	}
	if after, _ := json.Marshal(s); string(after) != string(before) {
		t.Errorf("EvaluateAt changed its input:\n%s\n%s", before, after)
	}

	// A8: without channels the objects come back as they were.
	plain := Scene{Timeline: &Timeline{FPS: 24}, Objects: []Object{box}}
	want, _ := json.Marshal(plain.Objects)
	got, _ := json.Marshal(EvaluateAt(plain, 3).Objects)
	if string(got) != string(want) {
		t.Errorf("objects without channels changed:\n%s\n%s", want, got)
	}
}

// timelineAtLimits is a scene at the object limit with 200 shots and 20000
// keys, as the timeline's limits allow.
func timelineAtLimits(b *testing.B) Scene {
	b.Helper()
	s := Scene{Timeline: &Timeline{FPS: defaultFPS}}
	for i := range maxMarkers {
		s.Timeline.Markers = append(s.Timeline.Markers, Marker{ID: fmt.Sprintf("smk_%021d", i), Frame: i * 10})
	}
	keyed := maxSceneKeys / maxMarkers / 2 // objects keyed on position and rotation at every shot
	for i := range maxObjects {
		o := Object{
			ID: fmt.Sprintf("sobj_%021d", i), Kind: KindPrimitive, Order: i, Visible: true,
			Transform: Transform{Position: []float64{float64(i), 0, 0}, Rotation: []float64{0, 0, 0, 1}, Scale: []float64{1, 1, 1}},
		}
		if i < keyed {
			pos, rot := make([]Key, 0, maxMarkers), make([]Key, 0, maxMarkers)
			for _, m := range s.Timeline.Markers {
				pos = append(pos, Key{Frame: m.Frame, Value: json.RawMessage(fmt.Sprintf("[%d,0,%d]", i, m.Frame)), Interp: interpLinear})
				rot = append(rot, Key{Frame: m.Frame, Value: json.RawMessage(`[0,0.7071067811865476,0,0.7071067811865476]`), Interp: interpLinear})
			}
			o.Animation = map[string]Channel{"transform.position": {Keys: pos}, "transform.rotation": {Keys: rot}}
		}
		s.Objects = append(s.Objects, o)
	}
	return s
}

// Evaluating a scene at the timeline's limits, between two shots so every
// channel interpolates; design 未决事项 records the result (budget 5 ms).
func BenchmarkEvaluateAtLimits(b *testing.B) {
	s := timelineAtLimits(b)
	b.ReportAllocs()
	for b.Loop() {
		EvaluateAt(s, 995)
	}
}
