package creativescene

import (
	"encoding/json"
	"testing"

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
