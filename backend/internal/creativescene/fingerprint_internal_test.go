package creativescene

import (
	"encoding/json"
	"fmt"
	"maps"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
)

// A camera stored before the depth of field fields existed fingerprints the
// same as once a write has stored its defaults: decodeScene completes it.
func TestOldCameraFingerprintsLikeACompletedOne(t *testing.T) {
	scene := func(camera string) creativecontent.Payload {
		raw := `{"environment":{"sky":{"kind":"color","color":"#000000"},"ambient":{"color":"#ffffff","intensity":0.4},"fog":null},
			"render":{"mode":"clay"},"objects":[{"id":"sobj_000000000000000000001","kind":"camera","name":"","parent_id":null,"order":0,
			"transform":{"position":[0,1,5],"rotation":[0,0,0,1],"scale":[1,1,1]},"visible":true,"locked":false,"camera":` + camera + `}]}`
		return creativecontent.Payload{Scene: json.RawMessage(raw)}
	}
	old, err := decodeScene(scene(`{"focal_mm":35,"sensor_mm":[36,24],"aspect":"3:2","near":0.1,"far":1000,"label":""}`))
	if err != nil {
		t.Fatal(err)
	}
	full, err := decodeScene(scene(`{"focal_mm":35,"sensor_mm":[36,24],"aspect":"3:2","near":0.1,"far":1000,"label":"",
		"depth_of_field":false,"f_stop":2.8,"focus":10,"focus_object_id":""}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, pass := range Passes {
		a, okA := Fingerprint(old, "sobj_000000000000000000001", pass)
		b, okB := Fingerprint(full, "sobj_000000000000000000001", pass)
		if !okA || !okB || a != b {
			t.Fatalf("%s: %s vs %s", pass, a, b)
		}
	}
}

// bigScene is a scene at the object limit with one camera, as a payload.
func bigScene(tb testing.TB) (creativecontent.Payload, string) {
	tb.Helper()
	camera := "sobj_000000000000000000001"
	objects := []string{`{"id":"` + camera + `","kind":"camera","name":"","parent_id":null,"order":0,
		"transform":{"position":[0,1,5],"rotation":[0,0,0,1],"scale":[1,1,1]},"visible":true,"locked":false,
		"camera":{"focal_mm":35,"sensor_mm":[36,24],"aspect":"3:2","near":0.1,"far":1000,"label":""}}`}
	for i := 1; i < 2000; i++ {
		id := fmt.Sprintf("sobj_%021d", i+1)
		objects = append(objects, fmt.Sprintf(`{"id":%q,"kind":"primitive","name":"","parent_id":null,"order":%d,
			"transform":{"position":[%d,0,0],"rotation":[0,0,0,1],"scale":[1,1,1]},"visible":true,"locked":false,
			"primitive":{"builtin":"box@1","params":{"width":1,"height":1,"depth":1}},"material_slots":{}}`, id, i, i))
	}
	raw := `{"environment":{"sky":{"kind":"color","color":"#000000"},"ambient":{"color":"#ffffff","intensity":0.4},"fog":null},
		"render":{"mode":"clay"},"objects":[` + strings.Join(objects, ",") + `]}`
	if _, err := decodeScene(creativecontent.Payload{Scene: json.RawMessage(raw)}); err != nil {
		tb.Fatal(err)
	}
	return creativecontent.Payload{Scene: json.RawMessage(raw)}, camera
}

// Every re-export adds nodes for the same (scene, camera, pass); the check
// fingerprints each of those once, so 48 such nodes cost about what one does.
func TestFreshnessFingerprintsEachShotOnce(t *testing.T) {
	payload, camera := bigScene(t)
	sc, err := decodeScene(payload)
	if err != nil {
		t.Fatal(err)
	}
	fp, _ := Fingerprint(sc, camera, "clay")
	current := map[string]creativecontent.Revision{"cwdoc_big": {Payload: payload}}
	sources := func(n int) []creativecanvas.ExportSource {
		out := make([]creativecanvas.ExportSource, n)
		for i := range out {
			out[i] = creativecanvas.ExportSource{NodeID: fmt.Sprintf("cwnode_%d", i), DocumentID: "cwdoc_big", CameraObjectID: camera, Pass: "clay", Fingerprint: fp}
		}
		return out
	}
	one, many := sources(1), sources(48)
	if states := (Freshness{}).Check(many, current); states["cwnode_47"] != creativecanvas.BasedOnCurrent {
		t.Fatalf("states %v", states)
	}
	a := testing.AllocsPerRun(5, func() { Freshness{}.Check(one, current) })
	b := testing.AllocsPerRun(5, func() { Freshness{}.Check(many, current) })
	if b > a*1.2 {
		t.Fatalf("48 nodes of one shot allocate %.0f times, one node %.0f: fingerprints are not shared", b, a)
	}
}

// A canvas read at the limits: a 2000-object scene and 48 export nodes.
func BenchmarkFreshnessCheck(b *testing.B) {
	payload, camera := bigScene(b)
	sc, _ := decodeScene(payload)
	current := map[string]creativecontent.Revision{"cwdoc_big": {Payload: payload}}
	var sources []creativecanvas.ExportSource
	for i := range 48 {
		pass := Passes[i%2]
		fp, _ := Fingerprint(sc, camera, pass)
		sources = append(sources, creativecanvas.ExportSource{NodeID: fmt.Sprintf("cwnode_%d", i), DocumentID: "cwdoc_big", CameraObjectID: camera, Pass: pass, Fingerprint: fp})
	}
	b.ReportAllocs()
	for b.Loop() {
		Freshness{}.Check(sources, current)
	}
}

// pinnedScene has one camera and one light; render is its render settings
// as JSON. The pins below are its fingerprints.
func pinnedScene(t *testing.T, render string) Scene {
	t.Helper()
	return sceneOf(t, proceduralSky, render, `{}`)
}

const (
	proceduralSky = `{"kind":"procedural","sun_elevation_deg":35,"sun_azimuth_deg":135,"turbidity":3}`
	physicalSky   = `{"kind":"physical","date":"2026-06-21"}`
)

// sceneOf is pinnedScene with its sky, render settings and camera fields as JSON.
func sceneOf(t *testing.T, sky, render, camera string) Scene {
	t.Helper()
	raw := `{"environment":{"sky":` + sky + `,"ambient":{"color":"#ffffff","intensity":0.4},"fog":null},
		"render":` + render + `,"objects":[{"id":"sobj_000000000000000000001","kind":"camera","name":"","parent_id":null,"order":0,
		"transform":{"position":[0,1,5],"rotation":[0,0,0,1],"scale":[1,1,1]},"visible":true,"locked":false,"camera":` + camera + `},
		{"id":"sobj_000000000000000000002","kind":"light","name":"","parent_id":null,"order":1,
		"transform":{"position":[0,3,0],"rotation":[0,0,0,1],"scale":[1,1,1]},"visible":true,"locked":false,"light":{"type":"point","intensity":50}}]}`
	s, err := decodeScene(creativecontent.Payload{Scene: json.RawMessage(raw)})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

const pinnedCamera = "sobj_000000000000000000001"

// The fingerprints of pinnedScene at exposure 0 on the workbench. beforeW1
// were taken before W1 (scene-display-look) gave clay the fixed studio light
// and clay and lit AgX; pinned are the ones since. Line art did not change.
var (
	beforeW1 = map[string]string{
		PassClay: "v1:97cf4a21ba8b318b9d4f7127a87cde66902afca4c4ff687c0658dbe14eb5e92e",
		PassLine: "v1:26a2cfb52279e30da0729c68d15b2acfdc0a362cbd546072dc2894f193844d72",
		PassLit:  "v1:2e0a60a7fa1fe800852915a413dcba499be7baf9c35b3ceeb504a2760c86f6e5",
	}
	pinned = map[string]string{
		PassClay: "v1:ec5e772a3a1246db4f4e3c3be75f06963297881456d8ab0d70f0b275415d0d9b",
		PassLine: beforeW1[PassLine],
		PassLit:  "v1:ae03e71833a7ac86908f92c525e29e0c1682df2b502e7b11d8992ad47e504208",
	}
)

// W1 changed how clay and lit images look without a version bump: their
// exports from before read as stale once, line art stays current.
func TestViewTransformStalesClayAndLitOnce(t *testing.T) {
	s := pinnedScene(t, `{"mode":"lit"}`)
	for _, pass := range Passes {
		got, _ := Fingerprint(s, pinnedCamera, pass)
		if got != pinned[pass] {
			t.Fatalf("%s: %s, want %s", pass, got, pinned[pass])
		}
		if (got == beforeW1[pass]) != (pass == PassLine) {
			t.Fatalf("%s: %s against the one before W1 %s", pass, got, beforeW1[pass])
		}
	}
}

// Clay draws with the fixed studio light: the sky, the ambient light and
// the fog only count for lit images.
func TestClayIgnoresTheEnvironment(t *testing.T) {
	s := pinnedScene(t, `{"mode":"lit"}`)
	blue := "#3355ff"
	s.Environment = Environment{
		Sky:     Sky{Kind: "color", Color: &blue},
		Ambient: Ambient{Color: "#ffd0a0", Intensity: 1.2},
		Fog:     &Fog{Color: "#c0c8d0", Near: 5, Far: 40},
	}
	for _, pass := range Passes {
		got, _ := Fingerprint(s, pinnedCamera, pass)
		if (got != pinned[pass]) != (pass == PassLit) {
			t.Fatalf("another environment, %s: %s", pass, got)
		}
	}
}

// Exposure only brightens lit images. At 0 it is left out of the hash, so
// fingerprints taken before the field existed still match (no version bump).
func TestExposureFingerprintsOnlyLit(t *testing.T) {
	for _, render := range []string{`{"mode":"lit"}`, `{"mode":"lit","exposure_ev":0}`} {
		for _, pass := range Passes {
			if got, _ := Fingerprint(pinnedScene(t, render), pinnedCamera, pass); got != pinned[pass] {
				t.Fatalf("%s %s: %s, want %s", render, pass, got, pinned[pass])
			}
		}
	}
	brighter := pinnedScene(t, `{"mode":"lit","exposure_ev":2}`)
	for _, pass := range Passes {
		got, _ := Fingerprint(brighter, pinnedCamera, pass)
		if (got != pinned[pass]) != (pass == PassLit) {
			t.Fatalf("exposure 2, %s: %s", pass, got)
		}
	}
}

// The renderer counts for the image it draws, lit; clay and line art are the
// workbench's whatever the scene says. The workbench is left out of the hash,
// so fingerprints from before the field existed still match; the output
// resolution never counts (R2 design R-3).
func TestEngineFingerprintsOnlyLit(t *testing.T) {
	for _, render := range []string{
		`{"mode":"lit","engine":"workbench"}`,
		`{"mode":"lit","engine":"workbench","resolution":"8k"}`,
		`{"mode":"lit","resolution":"1k"}`,
	} {
		for _, pass := range Passes {
			if got, _ := Fingerprint(pinnedScene(t, render), pinnedCamera, pass); got != pinned[pass] {
				t.Fatalf("%s %s: %s, want %s", render, pass, got, pinned[pass])
			}
		}
	}
	gi := pinnedScene(t, `{"mode":"lit","engine":"realtime_gi"}`)
	for _, pass := range Passes {
		got, _ := Fingerprint(gi, pinnedCamera, pass)
		if (got != pinned[pass]) != (pass == PassLit) {
			t.Fatalf("realtime_gi, %s: %s", pass, got)
		}
		if other, _ := Fingerprint(pinnedScene(t, `{"mode":"lit","engine":"realtime_gi","resolution":"4k"}`), pinnedCamera, pass); other != got {
			t.Fatalf("realtime_gi at 4k, %s: %s, want %s", pass, other, got)
		}
	}
}

// fingerprints are a scene's fingerprints of pinnedCamera in every pass.
func fingerprints(s Scene) map[string]string {
	out := map[string]string{}
	for _, pass := range Passes {
		out[pass], _ = Fingerprint(s, pinnedCamera, pass)
	}
	return out
}

// sameIn reports for each pass whether a and b fingerprint alike.
func sameIn(a, b Scene) map[string]bool {
	fa, fb := fingerprints(a), fingerprints(b)
	return map[string]bool{PassClay: fa[PassClay] == fb[PassClay], PassLine: fa[PassLine] == fb[PassLine], PassLit: fa[PassLit] == fb[PassLit]}
}

var onlyLitChanges = map[string]bool{PassClay: true, PassLine: true, PassLit: false}
var nothingChanges = map[string]bool{PassClay: true, PassLine: true, PassLit: true}

// W2: a camera's ISO and shutter only expose a lit image under a physical
// sky. Elsewhere they are cleared, the defaults a read fills in included, so
// fingerprints from before the fields existed still match the pins.
func TestCameraExposureFingerprintsOnlyPhysicalLit(t *testing.T) {
	for _, camera := range []string{`{"iso":1600}`, `{"shutter_s":0.5}`, `{"iso":25,"shutter_s":30}`} {
		for pass, got := range fingerprints(sceneOf(t, proceduralSky, `{"mode":"lit"}`, camera)) {
			if got != pinned[pass] {
				t.Fatalf("procedural sky, camera %s, %s: %s, want %s", camera, pass, got, pinned[pass])
			}
		}
		base := sceneOf(t, physicalSky, `{"mode":"lit"}`, `{}`)
		if got := sameIn(base, sceneOf(t, physicalSky, `{"mode":"lit"}`, camera)); !maps.Equal(got, onlyLitChanges) {
			t.Fatalf("physical sky, camera %s: alike %v", camera, got)
		}
	}
}

// Under a physical sky the aperture exposes a lit image even with depth of
// field off, and the exposure compensation does not count: the camera's
// exposure has replaced it. Focus still only counts with depth of field on.
func TestPhysicalLitCountsTheApertureNotTheCompensation(t *testing.T) {
	base := sceneOf(t, physicalSky, `{"mode":"lit"}`, `{}`)
	for _, tc := range []struct {
		name         string
		render, cam  string
		want         map[string]bool
		inProcedural map[string]bool
	}{
		{"aperture with depth of field off", `{"mode":"lit"}`, `{"f_stop":8}`, onlyLitChanges, nothingChanges},
		{"focus with depth of field off", `{"mode":"lit"}`, `{"focus":3}`, nothingChanges, nothingChanges},
		{"exposure compensation", `{"mode":"lit","exposure_ev":2}`, `{}`, nothingChanges, onlyLitChanges},
	} {
		if got := sameIn(base, sceneOf(t, physicalSky, tc.render, tc.cam)); !maps.Equal(got, tc.want) {
			t.Fatalf("%s under a physical sky: alike %v, want %v", tc.name, got, tc.want)
		}
		if got := sameIn(pinnedScene(t, `{"mode":"lit"}`), sceneOf(t, proceduralSky, tc.render, tc.cam)); !maps.Equal(got, tc.inProcedural) {
			t.Fatalf("%s under the procedural sky: alike %v, want %v", tc.name, got, tc.inProcedural)
		}
	}
}

// The default camera's exposure never counts: exports come from scene cameras.
func TestDefaultExposureNeverCounts(t *testing.T) {
	for _, sky := range []string{proceduralSky, physicalSky} {
		a := sceneOf(t, sky, `{"mode":"lit","exposure_ev100":15}`, `{}`)
		for _, render := range []string{`{"mode":"lit"}`, `{"mode":"lit","exposure_ev100":0}`, `{"mode":"lit","exposure_ev100":-10}`} {
			if got := sameIn(a, sceneOf(t, sky, render, `{}`)); !maps.Equal(got, nothingChanges) {
				t.Fatalf("%s, %s: alike %v", sky, render, got)
			}
		}
	}
}

// The physical sky's fields only count for lit images, like the rest of
// the environment.
func TestPhysicalSkyFingerprintsOnlyLit(t *testing.T) {
	base := sceneOf(t, physicalSky, `{"mode":"lit"}`, `{}`)
	for _, sky := range []string{
		`{"kind":"physical","date":"2026-06-21","local_time":"18:30"}`,
		`{"kind":"physical","date":"2026-06-22"}`,
		`{"kind":"physical","date":"2026-06-21","dust_density":12}`,
		`{"kind":"physical","date":"2026-06-21","sun_temperature_k":4000}`,
		`{"kind":"physical","date":"2026-06-21","latitude_deg":30.66,"longitude_deg":104.06,"timezone":"Asia/Shanghai"}`,
		// W3: each of the night sky's fields.
		`{"kind":"physical","date":"2026-06-21","night":{"moon":false}}`,
		`{"kind":"physical","date":"2026-06-21","night":{"stars":false}}`,
		`{"kind":"physical","date":"2026-06-21","night":{"milky_way":false}}`,
		`{"kind":"physical","date":"2026-06-21","night":{"moon_strength":2}}`,
		`{"kind":"physical","date":"2026-06-21","night":{"moon_temperature_k":4000}}`,
		`{"kind":"physical","date":"2026-06-21","night":{"stars_strength":0.5}}`,
		`{"kind":"physical","date":"2026-06-21","night":{"stars_temperature_k":9000}}`,
	} {
		if got := sameIn(base, sceneOf(t, sky, `{"mode":"lit"}`, `{}`)); !maps.Equal(got, onlyLitChanges) {
			t.Fatalf("%s: alike %v", sky, got)
		}
	}
}

// W3 (data: 夜空): a physical sky stored before the night sky had none and
// fingerprinted without it; read now, it is completed with one, so its lit
// exports read as stale once. Clay and line art leave the environment out.
func TestThePhysicalSkysNightStalesItsLitOnce(t *testing.T) {
	now := sceneOf(t, physicalSky, `{"mode":"lit"}`, `{}`)
	if now.Environment.Sky.Night == nil {
		t.Fatal("the night sky was not completed")
	}
	before := now
	before.Environment.Sky.Night = nil
	if got := sameIn(before, now); !maps.Equal(got, onlyLitChanges) {
		t.Fatalf("alike %v", got)
	}
}

// AC-W2-9: changing one camera's exposure stales that camera's lit image
// only: each camera's fingerprint leaves the other cameras out.
func TestOneCamerasExposureStalesOnlyItsLit(t *testing.T) {
	const other = "sobj_000000000000000000003"
	withSecond := func(camera string) Scene {
		s := sceneOf(t, physicalSky, `{"mode":"lit"}`, camera)
		second := s.Objects[0]
		second.ID = other
		second.Order = 2
		second.Transform.Position = []float64{2, 1, 5}
		s.Objects = append(s.Objects, second)
		return s
	}
	before := withSecond(`{}`)
	after := withSecond(`{"iso":400,"shutter_s":0.004}`)
	// The second camera is a copy of the first, so give it back its own exposure.
	after.Objects[2].Camera = before.Objects[2].Camera
	for _, pass := range Passes {
		a, _ := Fingerprint(before, pinnedCamera, pass)
		b, _ := Fingerprint(after, pinnedCamera, pass)
		if (a == b) != (pass != PassLit) {
			t.Errorf("the changed camera's %s: alike %v", pass, a == b)
		}
		a, _ = Fingerprint(before, other, pass)
		b, _ = Fingerprint(after, other, pass)
		if a != b {
			t.Errorf("the other camera's %s went stale", pass)
		}
	}
}
