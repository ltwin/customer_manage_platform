package creativescene_test

import (
	"reflect"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
)

// render_view tells the assistant how a lit screenshot under a physical sky
// was exposed (owner 2026-10-23): a camera's own exposure, the default
// camera's for a viewpoint or preset, nothing for clay or another sky.
func TestRenderExposure(t *testing.T) {
	ptr := func(v float64) *float64 { return &v }
	mode := func(m string) *string { return &m }
	camera := func(id string, c creativescene.Camera) creativescene.Object {
		return creativescene.Object{ID: id, Kind: "camera", Camera: &c}
	}
	scene := creativescene.Scene{
		Environment: creativescene.Environment{Sky: creativescene.Sky{Kind: creativescene.SkyPhysical}},
		Render:      creativescene.Render{Mode: "clay", ExposureMode: creativescene.ExposureAmbient, ExposureCompensation: 1.5, ExposureEV100: 12},
		Objects: []creativescene.Object{
			camera("ambient", creativescene.Camera{ExposureMode: mode(creativescene.ExposureAmbient), ExposureCompensation: ptr(-1), FStop: ptr(2.8), ShutterS: ptr(1.0 / 4000), ISO: ptr(100)}),
			// f/8, 1/125 s, ISO 100: log2(64 × 125) ≈ 12.97.
			camera("manual", creativescene.Camera{ExposureMode: mode(creativescene.ExposureManual), ExposureCompensation: ptr(0), FStop: ptr(8), ShutterS: ptr(1.0 / 125), ISO: ptr(100)}),
		},
	}
	manualDefault := scene
	manualDefault.Render.ExposureMode = creativescene.ExposureManual
	procedural := scene
	procedural.Environment.Sky = creativescene.Sky{Kind: "procedural"}

	for name, tc := range map[string]struct {
		scene creativescene.Scene
		req   creativescene.RenderRequest
		want  *creativescene.ShotExposure
	}{
		"ambient-metered camera":    {scene, creativescene.RenderRequest{Mode: "lit", CameraObjectID: "ambient"}, &creativescene.ShotExposure{By: "camera", Mode: "ambient", Compensation: ptr(-1)}},
		"manual camera":             {scene, creativescene.RenderRequest{Mode: "lit", CameraObjectID: "manual"}, &creativescene.ShotExposure{By: "camera", Mode: "manual", EV100: ptr(12.97)}},
		"preset, default ambient":   {scene, creativescene.RenderRequest{Mode: "lit", Preset: "overview"}, &creativescene.ShotExposure{By: "default_camera", Mode: "ambient", Compensation: ptr(1.5)}},
		"viewpoint, default manual": {manualDefault, creativescene.RenderRequest{Mode: "lit", Viewpoint: &creativescene.Viewpoint{}}, &creativescene.ShotExposure{By: "default_camera", Mode: "manual", EV100: ptr(12)}},
		"clay":                      {scene, creativescene.RenderRequest{Mode: "clay", CameraObjectID: "manual"}, nil},
		"another sky":               {procedural, creativescene.RenderRequest{Mode: "lit", CameraObjectID: "manual"}, nil},
		"camera gone":               {scene, creativescene.RenderRequest{Mode: "lit", CameraObjectID: "deleted"}, nil},
	} {
		t.Run(name, func(t *testing.T) {
			if got := creativescene.RenderExposure(tc.scene, tc.req); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}
