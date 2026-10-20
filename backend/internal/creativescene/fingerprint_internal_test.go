package creativescene

import (
	"encoding/json"
	"fmt"
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
