package creativescene

import (
	"encoding/json"
	"math"
	"reflect"
	"slices"
)

// EvaluateAt is the scene as it stands at frame (patterns: 关键帧求值): every
// channel's value written into its base field, with Animation and Timeline
// left out. The result has the shape of a scene without a timeline, so the
// render fingerprint, lensOf and the assistant's views take it unchanged; a
// scene without channels comes back as it was, less its Timeline (A8). s is
// not modified.
func EvaluateAt(s Scene, frame int) Scene {
	s.Timeline = nil
	s.Objects = slices.Clone(s.Objects)
	for i := range s.Objects {
		o := &s.Objects[i]
		if o.Animation == nil {
			continue
		}
		animation := o.Animation
		o.Animation = nil
		// Fields are replaced, never written through, so a copy of the camera
		// keeps the input as it was.
		if o.Camera != nil {
			c := *o.Camera
			o.Camera = &c
		}
		for path, ch := range animation {
			spec := channelOf(path, o.Kind)
			if spec == nil || (spec.camera && o.Camera == nil) {
				continue // Validate rejects it; nothing to show
			}
			// Stored keys are validated, so a value that fails to decode is
			// not expected; the base value then stands.
			_ = setSample(spec.field(o), spec.sample, ch.Keys, frame)
		}
	}
	return s
}

// setSample sets field, a pointer to the keyed base field, to the value of
// keys at frame: a new value, never one written through the old pointer or
// slice the field may share with another scene. An empty channel sets nothing.
func setSample(field any, kind sampleKind, keys []Key, frame int) error {
	if len(keys) == 0 {
		return nil
	}
	raw, nums, err := sampleKeys(kind, keys, frame)
	if err != nil {
		return err
	}
	if nums == nil {
		fresh := reflect.New(reflect.TypeOf(field).Elem())
		if err := json.Unmarshal(raw, fresh.Interface()); err != nil {
			return err
		}
		reflect.ValueOf(field).Elem().Set(fresh.Elem())
		return nil
	}
	switch p := field.(type) {
	case **float64:
		v := nums[0]
		*p = &v
	case *[]float64:
		*p = nums
	}
	return nil
}

// sampleKeys is the value of a non-empty channel at frame: the left key's raw
// value when it holds there unchanged, otherwise the interpolated numbers.
func sampleKeys(kind sampleKind, keys []Key, frame int) (json.RawMessage, []float64, error) {
	i, _ := slices.BinarySearchFunc(keys, frame, func(k Key, f int) int { return k.Frame - f })
	switch {
	case i < len(keys) && keys[i].Frame == frame:
		return keys[i].Value, nil, nil
	case i == 0:
		return keys[0].Value, nil, nil
	case i == len(keys):
		return keys[i-1].Value, nil, nil
	}
	a, b := keys[i-1], keys[i]
	if kind == sampleDiscrete || a.Interp == "constant" {
		return a.Value, nil, nil
	}
	va, err := decodeNumbers(a.Value)
	if err != nil {
		return nil, nil, err
	}
	vb, err := decodeNumbers(b.Value)
	if err != nil {
		return nil, nil, err
	}
	t := float64(frame-a.Frame) / float64(b.Frame-a.Frame)
	if kind == sampleQuaternion {
		return nil, slerp(va, vb, t), nil
	}
	return nil, lerp(va, vb, t), nil
}

// decodeNumbers reads a number as a one-element slice, an array as is.
func decodeNumbers(raw json.RawMessage) ([]float64, error) {
	var v float64
	if json.Unmarshal(raw, &v) == nil {
		return []float64{v}, nil
	}
	var vs []float64
	err := json.Unmarshal(raw, &vs)
	return vs, err
}

func lerp(a, b []float64, t float64) []float64 {
	out := make([]float64, len(a))
	for i := range a {
		out[i] = a[i] + (b[i]-a[i])*t
	}
	return out
}

// slerp takes the shorter way round and falls back to a normalized lerp when
// the two are less than 1e-6 rad apart, where sin θ loses precision.
func slerp(a, b []float64, t float64) []float64 {
	dot := a[0]*b[0] + a[1]*b[1] + a[2]*b[2] + a[3]*b[3]
	if dot < 0 {
		b = []float64{-b[0], -b[1], -b[2], -b[3]}
		dot = -dot
	}
	var q []float64
	if theta := math.Acos(math.Min(dot, 1)); theta < 1e-6 {
		q = lerp(a, b, t)
	} else {
		s := math.Sin(theta)
		wa, wb := math.Sin((1-t)*theta)/s, math.Sin(t*theta)/s
		q = []float64{wa*a[0] + wb*b[0], wa*a[1] + wb*b[1], wa*a[2] + wb*b[2], wa*a[3] + wb*b[3]}
	}
	n := quat(q).normalize()
	return n[:]
}
