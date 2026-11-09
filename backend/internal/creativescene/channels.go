package creativescene

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"regexp"
	"slices"
	"unicode/utf8"

	"github.com/samson/customer-manage-platform/backend/internal/creativedocument"
)

// Keyframe channels and shot markers (data: 场景时间线). An object's animation
// maps a channel path to keyframes of one property; channels.json is the
// whitelist of paths, which unit each lives in, which objects may have it and
// how it is sampled (patterns: 关键帧求值). Bone channels join it in child 4.
//
// channelsJSON is the only copy of the list; make generate copies it to the
// frontend, which routes edits and draws key states from it.
//
//go:embed channels.json
var channelsJSON []byte

const (
	defaultFPS     = 24
	maxFPS         = 120
	maxMarkers     = 200
	maxFrame       = 100000
	maxChannelKeys = 1000
	maxSceneKeys   = 20000

	interpLinear   = "linear"
	interpConstant = "constant"
)

var markerIDPattern = regexp.MustCompile(`^smk_[A-Za-z0-9_-]{21}$`)

// sampleKind is how a channel's value goes between two keys.
type sampleKind int

const (
	sampleNumber sampleKind = iota
	sampleVector
	sampleQuaternion
	// sampleDiscrete values only step: booleans, enums, strings and arrays
	// read as a whole such as sensor_mm.
	sampleDiscrete
)

type channelSpec struct {
	path string
	// content puts the channel in object:<id>:content, else object:<id>,
	// next to the base field it keys.
	content bool
	// camera limits the channel to camera objects.
	camera bool
	sample sampleKind
	// field points at the base field the channel's value lands in. For a
	// camera channel o.Camera is set (channelOf checks the kind first).
	field func(o *Object) any
}

// channelFields are the base fields of channels.json, by path.
var channelFields = map[string]func(o *Object) any{
	"transform.position":           func(o *Object) any { return &o.Transform.Position },
	"transform.rotation":           func(o *Object) any { return &o.Transform.Rotation },
	"transform.scale":              func(o *Object) any { return &o.Transform.Scale },
	"visible":                      func(o *Object) any { return &o.Visible },
	"camera.focal_mm":              func(o *Object) any { return &o.Camera.FocalMM },
	"camera.f_stop":                func(o *Object) any { return &o.Camera.FStop },
	"camera.focus":                 func(o *Object) any { return &o.Camera.Focus },
	"camera.exposure_compensation": func(o *Object) any { return &o.Camera.ExposureCompensation },
	"camera.iso":                   func(o *Object) any { return &o.Camera.ISO },
	"camera.shutter_s":             func(o *Object) any { return &o.Camera.ShutterS },
	"camera.sensor_mm":             func(o *Object) any { return &o.Camera.SensorMM },
	"camera.aspect":                func(o *Object) any { return &o.Camera.Aspect },
	"camera.depth_of_field":        func(o *Object) any { return &o.Camera.DepthOfField },
	"camera.focus_object_id":       func(o *Object) any { return &o.Camera.FocusObjectID },
	"camera.exposure_mode":         func(o *Object) any { return &o.Camera.ExposureMode },
}

var channelByPath = loadChannels()

func loadChannels() map[string]*channelSpec {
	var list []struct {
		Path       string `json:"path"`
		Unit       string `json:"unit"`
		CameraOnly bool   `json:"camera_only"`
		Sample     string `json:"sample"`
	}
	if err := json.Unmarshal(channelsJSON, &list); err != nil {
		panic(fmt.Sprintf("creativescene: channels.json: %v", err))
	}
	samples := map[string]sampleKind{
		"number": sampleNumber, "vector": sampleVector,
		"quaternion": sampleQuaternion, "discrete": sampleDiscrete,
	}
	m := make(map[string]*channelSpec, len(list))
	for _, c := range list {
		sample, ok := samples[c.Sample]
		field := channelFields[c.Path]
		if !ok || field == nil || (c.Unit != "placement" && c.Unit != "content") {
			panic(fmt.Sprintf("creativescene: channels.json: bad entry %q", c.Path))
		}
		m[c.Path] = &channelSpec{path: c.Path, content: c.Unit == "content", camera: c.CameraOnly, sample: sample, field: field}
	}
	if len(m) != len(channelFields) {
		panic("creativescene: channels.json and channelFields differ")
	}
	return m
}

// channelOf is the spec of path on an object of kind, or nil when the path
// is not animatable there.
func channelOf(path, kind string) *channelSpec {
	c := channelByPath[path]
	if c == nil || (c.camera && kind != KindCamera) {
		return nil
	}
	return c
}

// completeTimeline gives a scene stored before timelines existed the empty
// one (data: 场景时间线).
func completeTimeline(s *Scene) {
	if s.Timeline == nil {
		s.Timeline = &Timeline{FPS: defaultFPS}
	}
	if s.Timeline.Markers == nil {
		s.Timeline.Markers = []Marker{}
	}
}

// completeAnimation fills the interp a key leaves out (linear, constant for
// a discrete channel) and stores quaternion keys normalized; one too far off
// unit length is left for Validate to refuse.
func completeAnimation(o *Object) {
	if len(o.Animation) == 0 {
		o.Animation = nil
		return
	}
	for path, ch := range o.Animation {
		spec := channelOf(path, o.Kind)
		if spec == nil {
			continue
		}
		for i := range ch.Keys {
			k := &ch.Keys[i]
			if k.Interp == "" {
				k.Interp = interpLinear
				if spec.sample == sampleDiscrete {
					k.Interp = interpConstant
				}
			}
			if spec.sample == sampleQuaternion {
				k.Value = normalizedQuaternion(k.Value)
			}
		}
	}
}

func normalizedQuaternion(raw json.RawMessage) json.RawMessage {
	var q []float64
	if json.Unmarshal(raw, &q) != nil || len(q) != 4 {
		return raw
	}
	n := math.Sqrt(q[0]*q[0] + q[1]*q[1] + q[2]*q[2] + q[3]*q[3])
	if !finite(n) || math.Abs(n-1) > quaternionSlack {
		return raw
	}
	out, err := json.Marshal([]float64{q[0] / n, q[1] / n, q[2] / n, q[3] / n})
	if err != nil {
		return raw
	}
	return out
}

// validateTimeline checks the markers' own shape; which camera a marker
// names is checked only when written (checkMarkerCameras).
func validateTimeline(t *Timeline) error {
	if t == nil {
		return nil
	}
	if t.FPS < 1 || t.FPS > maxFPS {
		return invalid("时间线帧率 fps 须在 1…%d 之间", maxFPS)
	}
	if len(t.Markers) > maxMarkers {
		return &creativedocument.LimitError{Limit: maxMarkers, Message: fmt.Sprintf("分镜不能超过 %d 个", maxMarkers)}
	}
	ids := make(map[string]bool, len(t.Markers))
	for i, m := range t.Markers {
		if !markerIDPattern.MatchString(m.ID) {
			return invalid("分镜 ID %q 须为 smk_ 加 21 位字母、数字、_ 或 -", m.ID)
		}
		if ids[m.ID] {
			return invalid("分镜 ID %s 重复", m.ID)
		}
		ids[m.ID] = true
		if utf8.RuneCountInString(m.Name) > maxNameRunes {
			return invalid("分镜 %s 的名称不能超过 %d 个字", m.ID, maxNameRunes)
		}
		if m.Frame < 0 || m.Frame > maxFrame {
			return invalid("分镜 %s 的帧号须在 0…%d 之间", m.ID, maxFrame)
		}
		if i > 0 && m.Frame == t.Markers[i-1].Frame {
			return invalid("分镜 %s 与 %s 的帧号 %d 重复，一个分镜占一帧", t.Markers[i-1].ID, m.ID, m.Frame)
		}
		if i > 0 && m.Frame < t.Markers[i-1].Frame {
			return invalid("分镜须按帧号升序存储")
		}
		if id := m.CameraObjectID; id != "" && !objectIDPattern.MatchString(id) {
			return objectIDProblem(id)
		}
	}
	return nil
}

// checkMarkerCameras is run when the markers are written: each bound camera
// must be a camera of the scene after the write. One deleted later leaves
// its marker unbound, which is not a violation.
func checkMarkerCameras(s *Scene) error {
	if s.Timeline == nil {
		return nil
	}
	kinds := make(map[string]string, len(s.Objects))
	for _, o := range s.Objects {
		kinds[o.ID] = o.Kind
	}
	for _, m := range s.Timeline.Markers {
		if m.CameraObjectID == "" {
			continue
		}
		switch kind, ok := kinds[m.CameraObjectID]; {
		case !ok:
			return invalid("分镜 %s 绑定的摄像机 %s 不存在", m.ID, m.CameraObjectID)
		case kind != KindCamera:
			return invalid("分镜 %s 绑定的 %s 是 %s，不是摄像机", m.ID, m.CameraObjectID, kind)
		}
	}
	return nil
}

// validateAnimation checks o's channels and returns how many keys they hold.
// A key's value is checked by putting it into a copy of the object and
// running the base field's own validation, so each range rule lives once.
func validateAnimation(o *Object) (int, error) {
	total := 0
	for _, path := range slices.Sorted(maps.Keys(o.Animation)) {
		keys := o.Animation[path].Keys
		spec := channelOf(path, o.Kind)
		switch {
		case spec == nil && channelByPath[path] != nil:
			return 0, invalid("物件 %s：%s 只有摄像机能打关键帧", o.ID, path)
		case spec == nil:
			return 0, invalid("物件 %s：%s 不能打关键帧", o.ID, path)
		case len(keys) == 0:
			return 0, invalid("物件 %s：通道 %s 没有关键帧，没有关键帧的通道应删除", o.ID, path)
		case len(keys) > maxChannelKeys:
			return 0, &creativedocument.LimitError{Limit: maxChannelKeys, Message: fmt.Sprintf("物件 %s 的通道 %s 有 %d 个关键帧，一条通道不能超过 %d 个", o.ID, path, len(keys), maxChannelKeys)}
		}
		for i, k := range keys {
			if err := checkKey(o, spec, k); err != nil {
				return 0, invalid("物件 %s：通道 %s 第 %d 帧：%v", o.ID, path, k.Frame, err)
			}
			if i > 0 && k.Frame <= keys[i-1].Frame {
				return 0, invalid("物件 %s：通道 %s 的关键帧须按帧号严格递增（第 %d 帧在第 %d 帧之后）", o.ID, path, k.Frame, keys[i-1].Frame)
			}
		}
		total += len(keys)
	}
	return total, nil
}

func checkKey(o *Object, spec *channelSpec, k Key) error {
	if k.Frame < 0 || k.Frame > maxFrame {
		return fmt.Errorf("帧号须在 0…%d 之间", maxFrame)
	}
	switch {
	case k.Interp != interpLinear && k.Interp != interpConstant:
		return errors.New("插值 interp 须为 linear 或 constant")
	case k.Interp == interpLinear && spec.sample == sampleDiscrete:
		return errors.New("这个属性不能插值，interp 只能用 constant")
	case len(k.Value) == 0 || string(k.Value) == "null":
		return errors.New("缺少值")
	case spec.camera && o.Camera == nil:
		return errors.New("缺少 camera")
	}
	c := *o
	c.Animation = nil
	if c.Camera != nil {
		cam := *c.Camera
		c.Camera = &cam
	}
	// A discrete set decodes the raw value as is, whatever the channel.
	if err := setSample(spec.field(&c), sampleDiscrete, []Key{k}, k.Frame); err != nil {
		return errors.New("值的类型与属性不符")
	}
	if spec.camera {
		return validateCamera(*c.Camera)
	}
	return validateTransform(c.Transform)
}
