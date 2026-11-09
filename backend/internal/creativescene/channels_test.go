package creativescene

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativedocument"
)

// data: 场景时间线 — the shape of markers and channels. pinnedScene's camera
// is pinnedCamera, its light pinnedLight.

const pinnedLight = "sobj_000000000000000000002"

func key(frame int, value, interp string) Key {
	return Key{Frame: frame, Value: json.RawMessage(value), Interp: interp}
}

func marker(n, frame int, camera string) Marker {
	return Marker{ID: fmt.Sprintf("smk_%021d", n), Frame: frame, CameraObjectID: camera}
}

func groupAt(n int) Object {
	return Object{
		ID: fmt.Sprintf("sobj_%021d", 100+n), Kind: KindGroup, Order: 10 + n, Visible: true,
		Transform: Transform{Position: []float64{0, 0, 0}, Rotation: []float64{0, 0, 0, 1}, Scale: []float64{1, 1, 1}},
	}
}

func manyKeys(n int) []Key {
	keys := make([]Key, n)
	for i := range keys {
		keys[i] = key(i, fmt.Sprintf("[%d,0,0]", i%100), interpLinear)
	}
	return keys
}

func TestTimelineDecode(t *testing.T) {
	s := pinnedScene(t, `{"mode":"lit"}`)
	if s.Timeline == nil || s.Timeline.FPS != defaultFPS || s.Timeline.Markers == nil || len(s.Timeline.Markers) != 0 {
		t.Fatalf("old scene timeline = %+v, want {24, []}", s.Timeline)
	}
	if err := Validate(s); err != nil {
		t.Fatal(err)
	}

	o := &s.Objects[0]
	o.Animation = map[string]Channel{
		"transform.rotation": {Keys: []Key{key(0, `[0,0,0,1.0005]`, "")}},
		"visible":            {Keys: []Key{key(0, `true`, "")}},
	}
	completeAnimation(o)
	if got := o.Animation["visible"].Keys[0].Interp; got != interpConstant {
		t.Errorf("discrete interp = %q, want constant", got)
	}
	if got := o.Animation["transform.rotation"].Keys[0]; got.Interp != interpLinear || string(got.Value) != `[0,0,0,1]` {
		t.Errorf("rotation key = %s %q, want [0,0,0,1] linear", got.Value, got.Interp)
	}
	s.Timeline.Markers = []Marker{marker(1, 0, pinnedCamera), marker(2, 10, "")}
	if err := Validate(s); err != nil {
		t.Fatal(err)
	}
	if err := checkMarkerCameras(&s); err != nil {
		t.Fatal(err)
	}
}

func TestTimelineRejects(t *testing.T) {
	cases := []struct {
		name  string
		edit  func(s *Scene)
		check func(s *Scene) error
		want  string
		limit bool
	}{
		{name: "path outside the whitelist", want: "light.intensity 不能打关键帧", edit: func(s *Scene) {
			s.Objects[1].Animation = map[string]Channel{"light.intensity": {Keys: []Key{key(0, `5`, "")}}}
		}},
		{name: "camera channel on a light", want: "只有摄像机能打关键帧", edit: func(s *Scene) {
			s.Objects[1].Animation = map[string]Channel{"camera.focal_mm": {Keys: []Key{key(0, `50`, interpLinear)}}}
		}},
		{name: "empty channel", want: "没有关键帧", edit: func(s *Scene) {
			s.Objects[0].Animation = map[string]Channel{"visible": {}}
		}},
		{name: "frames not increasing", want: "严格递增", edit: func(s *Scene) {
			s.Objects[0].Animation = map[string]Channel{"transform.position": {Keys: []Key{key(10, `[0,0,0]`, interpLinear), key(10, `[1,0,0]`, interpLinear)}}}
		}},
		{name: "frame out of range", want: "帧号须在 0…100000", edit: func(s *Scene) {
			s.Objects[0].Animation = map[string]Channel{"transform.position": {Keys: []Key{key(100001, `[0,0,0]`, interpLinear)}}}
		}},
		{name: "quaternion off unit length", want: "单位四元数", edit: func(s *Scene) {
			s.Objects[0].Animation = map[string]Channel{"transform.rotation": {Keys: []Key{key(0, `[0,0,0,1.1]`, interpLinear)}}}
		}},
		{name: "value out of the field's range", want: "焦距须在 8…800", edit: func(s *Scene) {
			s.Objects[0].Animation = map[string]Channel{"camera.focal_mm": {Keys: []Key{key(0, `5`, interpLinear)}}}
		}},
		{name: "value of the wrong type", want: "类型与属性不符", edit: func(s *Scene) {
			s.Objects[0].Animation = map[string]Channel{"camera.focal_mm": {Keys: []Key{key(0, `"85"`, interpLinear)}}}
		}},
		{name: "null value", want: "缺少值", edit: func(s *Scene) {
			s.Objects[0].Animation = map[string]Channel{"visible": {Keys: []Key{key(0, `null`, interpConstant)}}}
		}},
		{name: "linear on a discrete property", want: "不能插值", edit: func(s *Scene) {
			s.Objects[0].Animation = map[string]Channel{"visible": {Keys: []Key{key(0, `true`, interpLinear)}}}
		}},
		{name: "unknown interp", want: "linear 或 constant", edit: func(s *Scene) {
			s.Objects[0].Animation = map[string]Channel{"camera.f_stop": {Keys: []Key{key(0, `2`, "bezier")}}}
		}},
		{name: "focus on a light", want: "只能对焦到基础体", edit: func(s *Scene) {
			s.Objects[0].Animation = map[string]Channel{"camera.focus_object_id": {Keys: []Key{key(0, `"`+pinnedLight+`"`, interpConstant)}}}
		}, check: func(s *Scene) error {
			return checkFocusTargets(s, map[string]bool{pinnedCamera: true})
		}},
		{name: "fps out of range", want: "fps", edit: func(s *Scene) { s.Timeline.FPS = 0 }},
		{name: "bad marker id", want: "smk_", edit: func(s *Scene) {
			s.Timeline.Markers = []Marker{{ID: "smk_1", Frame: 0}}
		}},
		{name: "duplicate marker id", want: "重复", edit: func(s *Scene) {
			s.Timeline.Markers = []Marker{marker(1, 0, ""), marker(1, 5, "")}
		}},
		{name: "duplicate marker frame", want: "帧号 5 重复", edit: func(s *Scene) {
			s.Timeline.Markers = []Marker{marker(1, 5, ""), marker(2, 5, "")}
		}},
		{name: "markers out of order", want: "升序", edit: func(s *Scene) {
			s.Timeline.Markers = []Marker{marker(1, 5, ""), marker(2, 0, "")}
		}},
		{name: "marker frame out of range", want: "帧号须在", edit: func(s *Scene) {
			s.Timeline.Markers = []Marker{marker(1, -1, "")}
		}},
		{name: "marker bound to a light", want: "不是摄像机", edit: func(s *Scene) {
			s.Timeline.Markers = []Marker{marker(1, 0, pinnedLight)}
		}, check: checkMarkerCameras},
		{name: "marker bound to a missing camera", want: "不存在", edit: func(s *Scene) {
			s.Timeline.Markers = []Marker{marker(1, 0, "sobj_000000000000000000009")}
		}, check: checkMarkerCameras},
		{name: "over 200 markers", limit: true, edit: func(s *Scene) {
			s.Timeline.Markers = nil
			for i := range maxMarkers + 1 {
				s.Timeline.Markers = append(s.Timeline.Markers, marker(i, i, ""))
			}
		}},
		{name: "over 1000 keys in a channel", limit: true, edit: func(s *Scene) {
			s.Objects[0].Animation = map[string]Channel{"transform.position": {Keys: manyKeys(maxChannelKeys + 1)}}
		}},
		{name: "over 20000 keys in a scene", limit: true, edit: func(s *Scene) {
			for g := range maxSceneKeys/(2*maxChannelKeys) + 1 {
				o := groupAt(g)
				o.Animation = map[string]Channel{
					"transform.position": {Keys: manyKeys(maxChannelKeys)},
					"transform.scale":    {Keys: manyKeys(maxChannelKeys)},
					"visible":            {Keys: []Key{key(0, `true`, interpConstant)}},
				}
				for i := range o.Animation["transform.scale"].Keys {
					o.Animation["transform.scale"].Keys[i].Value = json.RawMessage(`[1,1,1]`)
				}
				s.Objects = append(s.Objects, o)
			}
			s.Objects[0].Animation = map[string]Channel{"transform.position": {Keys: manyKeys(maxChannelKeys)}}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := pinnedScene(t, `{"mode":"lit"}`)
			c.edit(&s)
			check := c.check
			if check == nil {
				check = func(s *Scene) error { return Validate(*s) }
			}
			err := check(&s)
			switch {
			case err == nil:
				t.Fatal("accepted")
			case c.limit && !errors.Is(err, creativedocument.ErrSizeLimit):
				t.Fatalf("err = %v, want a size limit", err)
			case !c.limit && !strings.Contains(err.Error(), c.want):
				t.Fatalf("err = %v, want it to say %q", err, c.want)
			}
		})
	}
}
