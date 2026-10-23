package creativescene_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativedocument"
	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
)

func f64(v float64) *float64 { return &v }

func flag(v bool) *bool { return &v }

func validTransform() creativescene.Transform {
	return creativescene.Transform{Position: []float64{0, 0, 0}, Rotation: []float64{0, 0, 0, 1}, Scale: []float64{1, 1, 1}}
}

// validScene holds one object of each kind, nested: group > box, plus a
// light and a camera at the root.
func validScene() creativescene.Scene {
	g := objectID(1)
	return creativescene.Scene{
		Environment: creativescene.Environment{
			Sky:     creativescene.Sky{Kind: "procedural", SunElevationDeg: f64(35), SunAzimuthDeg: f64(135), Turbidity: f64(3)},
			Ambient: creativescene.Ambient{Color: "#ffffff", Intensity: 0.4},
		},
		Render: creativescene.Render{Mode: "clay", Engine: creativescene.EngineWorkbench, Resolution: "2k", ExposureMode: creativescene.ExposureAmbient},
		Objects: []creativescene.Object{
			{ID: g, Kind: "group", Transform: validTransform(), Visible: true},
			{ID: objectID(2), Kind: "primitive", ParentID: &g, Transform: validTransform(), Visible: true,
				Primitive:     &creativescene.Primitive{Builtin: "cylinder@1", Params: map[string]any{"radius_top": 0.0, "radius_bottom": 0.5, "height": 1.0, "radial_segments": 32.0}},
				MaterialSlots: map[string]creativescene.MaterialSlot{"main": {Preset: "stone@1", Params: map[string]any{"roughness": 0.8, "tint": "warm", "worn": true}}}},
			{ID: objectID(3), Kind: "light", Order: 1, Transform: validTransform(), Visible: true,
				Light: &creativescene.Light{Type: "spot", Color: ptr("#ffeedd"), Intensity: f64(50), CastShadow: flag(false), Distance: f64(20), Decay: f64(2), Angle: f64(0.26), Penumbra: f64(0.2)}},
			{ID: objectID(4), Kind: "camera", Order: 2, Transform: validTransform(), Visible: true,
				Camera: &creativescene.Camera{FocalMM: f64(35), SensorMM: []float64{36, 24}, Aspect: ptr("3:2"), Near: f64(0.1), Far: f64(1000), Label: ptr(""),
					DepthOfField: flag(false), FStop: f64(2.8), Focus: f64(10), FocusObjectID: ptr(""), ISO: f64(100), ShutterS: f64(1.0 / 4000), ExposureMode: ptr(creativescene.ExposureAmbient), ExposureCompensation: f64(0)}},
		},
	}
}

func TestValidate(t *testing.T) {
	if err := creativescene.Validate(validScene()); err != nil {
		t.Fatalf("valid scene rejected: %v", err)
	}
	type mutation func(s *creativescene.Scene)
	obj := func(i int, m func(o *creativescene.Object)) mutation {
		return func(s *creativescene.Scene) { m(&s.Objects[i]) }
	}
	cases := map[string]mutation{
		// render
		"path tracing":       func(s *creativescene.Scene) { s.Render.Engine = "path_tracing" },
		"no engine":          func(s *creativescene.Scene) { s.Render.Engine = "" },
		"unknown resolution": func(s *creativescene.Scene) { s.Render.Resolution = "16k" },
		"no resolution":      func(s *creativescene.Scene) { s.Render.Resolution = "" },
		// S1
		"bad id":       obj(1, func(o *creativescene.Object) { o.ID = "box" }),
		"duplicate id": obj(3, func(o *creativescene.Object) { o.ID = objectID(2) }),
		// S2
		"parent missing": obj(1, func(o *creativescene.Object) { o.ParentID = ptr(objectID(9)) }),
		"parent not group": func(s *creativescene.Scene) {
			s.Objects[2].ParentID = ptr(objectID(2))
		},
		"cycle": func(s *creativescene.Scene) {
			h := objectID(5)
			s.Objects = append(s.Objects, creativescene.Object{ID: h, Kind: "group", ParentID: ptr(objectID(1)), Order: 1, Transform: validTransform()})
			s.Objects[0].ParentID = &h
		},
		// S3
		"negative order": obj(2, func(o *creativescene.Object) { o.Order = -1 }),
		"siblings clash": obj(3, func(o *creativescene.Object) { o.Order = 1 }),
		// S4
		"primitive without primitive": obj(1, func(o *creativescene.Object) { o.Primitive = nil }),
		"camera with light":           obj(3, func(o *creativescene.Object) { o.Light = &creativescene.Light{Type: "point", Color: ptr("#ffffff")} }),
		"light with slots":            obj(2, func(o *creativescene.Object) { o.MaterialSlots = validScene().Objects[1].MaterialSlots }),
		"group with camera":           obj(0, func(o *creativescene.Object) { o.Camera = validScene().Objects[3].Camera }),
		"unknown kind":                obj(0, func(o *creativescene.Object) { o.Kind = "model_instance" }),
		// S5
		"unknown builtin": obj(1, func(o *creativescene.Object) { o.Primitive.Builtin = "cylinder@2" }),
		"missing param":   obj(1, func(o *creativescene.Object) { delete(o.Primitive.Params, "height") }),
		"fractional integer": obj(1, func(o *creativescene.Object) {
			o.Primitive.Params["radial_segments"] = 3.5
		}),
		"param wrong type": obj(1, func(o *creativescene.Object) { o.Primitive.Params["height"] = "tall" }),
		// S6
		"position far":      obj(1, func(o *creativescene.Object) { o.Transform.Position[2] = 100001 }),
		"scale huge":        obj(1, func(o *creativescene.Object) { o.Transform.Scale[0] = 1001 }),
		"rotation off unit": obj(1, func(o *creativescene.Object) { o.Transform.Rotation = []float64{0, 0, 0, 0.99} }),
		"light too bright":  obj(2, func(o *creativescene.Object) { o.Light.Intensity = f64(1001) }),
		"spot angle":        obj(2, func(o *creativescene.Object) { o.Light.Angle = f64(1.6) }),
		"negative distance": obj(2, func(o *creativescene.Object) { o.Light.Distance = f64(-1) }),
		"decay":             obj(2, func(o *creativescene.Object) { o.Light.Decay = f64(11) }),
		// L1: exactly the fields of the type.
		"point with angle":      obj(2, func(o *creativescene.Object) { o.Light.Type = "point" }),
		"spot without penumbra": obj(2, func(o *creativescene.Object) { o.Light.Penumbra = nil }),
		"focal":                 obj(3, func(o *creativescene.Object) { o.Camera.FocalMM = f64(5) }),
		"sensor":                obj(3, func(o *creativescene.Object) { o.Camera.SensorMM = []float64{36} }),
		"aspect":                obj(3, func(o *creativescene.Object) { o.Camera.Aspect = ptr("33:1") }),
		"far before near":       obj(3, func(o *creativescene.Object) { o.Camera.Far = f64(0.05) }),
		"camera without near":   obj(3, func(o *creativescene.Object) { o.Camera.Near = nil }),
		"camera without f_stop": obj(3, func(o *creativescene.Object) { o.Camera.FStop = nil }),
		"f_stop low":            obj(3, func(o *creativescene.Object) { o.Camera.FStop = f64(0.4) }),
		"f_stop high":           obj(3, func(o *creativescene.Object) { o.Camera.FStop = f64(65) }),
		"focus zero":            obj(3, func(o *creativescene.Object) { o.Camera.Focus = f64(0) }),
		"focus object id":       obj(3, func(o *creativescene.Object) { o.Camera.FocusObjectID = ptr("box") }),
		"sun":                   func(s *creativescene.Scene) { s.Environment.Sky.SunElevationDeg = f64(91) },
		"sky extra field":       func(s *creativescene.Scene) { s.Environment.Sky.HDRI = ptr("dusk@1") },
		"hdri id": func(s *creativescene.Scene) {
			s.Environment.Sky = creativescene.Sky{Kind: "hdri", HDRI: ptr("Dusk")}
		},
		"ambient": func(s *creativescene.Scene) { s.Environment.Ambient.Intensity = 11 },
		"fog order": func(s *creativescene.Scene) {
			s.Environment.Fog = &creativescene.Fog{Color: "#808080", Near: 10, Far: 5}
		},
		"render": func(s *creativescene.Scene) { s.Render.Mode = "path" },
		// S7
		"long name":  obj(1, func(o *creativescene.Object) { o.Name = strings.Repeat("字", 81) }),
		"long label": obj(3, func(o *creativescene.Object) { o.Camera.Label = ptr(strings.Repeat("镜", 41)) }),
		"slot params": obj(1, func(o *creativescene.Object) {
			params := map[string]any{}
			for i := range 33 {
				params[strings.Repeat("k", i+1)] = 1.0
			}
			o.MaterialSlots = map[string]creativescene.MaterialSlot{"main": {Preset: "stone@1", Params: params}}
		}),
		"slot text": obj(1, func(o *creativescene.Object) {
			o.MaterialSlots["main"].Params["tint"] = strings.Repeat("x", 81)
		}),
	}
	for name, m := range cases {
		t.Run(name, func(t *testing.T) {
			s := validScene()
			m(&s)
			err := creativescene.Validate(s)
			if !errors.Is(err, creativeops.ErrValidation) {
				t.Fatalf("want validation error, got %v", err)
			}
		})
	}

	// S8 is a size limit, not a validation error.
	for name, m := range map[string]mutation{
		"objects": func(s *creativescene.Scene) {
			for i := len(s.Objects); i <= 2000; i++ {
				s.Objects = append(s.Objects, creativescene.Object{ID: objectID(100 + i), Kind: "group", Order: 10 + i, Transform: validTransform()})
			}
		},
		"cameras": func(s *creativescene.Scene) {
			for i := range 32 {
				c := s.Objects[3]
				c.ID, c.Order = objectID(100+i), 10+i
				s.Objects = append(s.Objects, c)
			}
		},
	} {
		t.Run("too many "+name, func(t *testing.T) {
			s := validScene()
			m(&s)
			if err := creativescene.Validate(s); !errors.Is(err, creativedocument.ErrSizeLimit) {
				t.Fatalf("want size limit, got %v", err)
			}
		})
	}
}

// validVolume is a completed volume object at order 3.
func validVolume(n int) creativescene.Object {
	return creativescene.Object{ID: objectID(n), Kind: "volume", Order: 3 + n, Transform: validTransform(), Visible: true,
		Volume: &creativescene.Volume{Shape: ptr("box"), ScatterColor: ptr("#cccccc"), ScatterDensity: f64(0.1), Anisotropy: f64(0),
			AbsorptionColor: ptr("#cccccc"), AbsorptionDensity: f64(0), EdgeFalloff: f64(0.1), CastShadow: flag(false)}}
}

// W5 (data: 体积物件): ranges, content fields matching the kind, and at
// most 8 volumes, refused as a size limit.
func TestValidateVolumes(t *testing.T) {
	s := validScene()
	s.Objects = append(s.Objects, validVolume(10))
	if err := creativescene.Validate(s); err != nil {
		t.Fatalf("valid volume rejected: %v", err)
	}
	for name, c := range map[string]struct {
		m     func(o *creativescene.Object)
		field string
	}{
		"shape":             {func(o *creativescene.Object) { o.Volume.Shape = ptr("cone") }, "volume.shape"},
		"scatter colour":    {func(o *creativescene.Object) { o.Volume.ScatterColor = ptr("white") }, "volume.scatter_color"},
		"scatter density":   {func(o *creativescene.Object) { o.Volume.ScatterDensity = f64(101) }, "volume.scatter_density"},
		"negative density":  {func(o *creativescene.Object) { o.Volume.AbsorptionDensity = f64(-1) }, "volume.absorption_density"},
		"anisotropy":        {func(o *creativescene.Object) { o.Volume.Anisotropy = f64(0.96) }, "volume.anisotropy"},
		"absorption colour": {func(o *creativescene.Object) { o.Volume.AbsorptionColor = ptr("#ccc") }, "volume.absorption_color"},
		"edge falloff":      {func(o *creativescene.Object) { o.Volume.EdgeFalloff = f64(1.5) }, "volume.edge_falloff"},
		"missing field":     {func(o *creativescene.Object) { o.Volume.CastShadow = nil }, "volume.cast_shadow"},
		"no volume":         {func(o *creativescene.Object) { o.Volume = nil }, "volume"},
		"material slots": {func(o *creativescene.Object) {
			o.MaterialSlots = map[string]creativescene.MaterialSlot{"main": {Preset: "stone@1"}}
		}, "material_slots"},
		"volume on a light": {nil, "volume"},
	} {
		t.Run(name, func(t *testing.T) {
			s := validScene()
			v := validVolume(10)
			if c.m != nil {
				c.m(&v)
				s.Objects = append(s.Objects, v)
			} else {
				s.Objects[2].Volume = v.Volume
			}
			err := creativescene.Validate(s)
			if !errors.Is(err, creativeops.ErrValidation) || !strings.Contains(err.Error(), c.field) {
				t.Fatalf("want a validation error naming %s, got %v", c.field, err)
			}
		})
	}

	s = validScene()
	for i := range 9 {
		s.Objects = append(s.Objects, validVolume(10+i))
	}
	if err := creativescene.Validate(s); !errors.Is(err, creativedocument.ErrSizeLimit) || !strings.Contains(err.Error(), "8") {
		t.Fatalf("9 volumes: want a size limit naming 8, got %v", err)
	}
}
