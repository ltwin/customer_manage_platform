package creativecontent_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativeglb"
)

func TestModelFactsOfCompleteAndStagedForms(t *testing.T) {
	complete := creativecontent.Payload{Model: json.RawMessage(`{"format":"glb","name_hint":"哥特束柱",
 "manifest":{"bounds":{"min":[-0.25,0,-0.25],"max":[0.25,2.4004,0.25]},"triangles":1200,
  "slots":[{"id":"stone","hint":"stone"}],"parts":["base","shaft"],"anchors":[{"id":"top","position":[0,2.4,0]}]},
 "materials":{"stone":{"preset":"marble@1","params":{}}},"source_revision_id":"ccrv_scene",
 "generator":{"code":"export default 1","code_sha256":"sha256-x","mount":"floor",
  "params_schema":{"height":{"type":"number","default":2},"flutes":{"type":"integer","default":8}},"params":{"height":2.4,"flutes":8}}}`)}
	f, ok := creativecontent.ModelFactsOf(complete)
	if !ok {
		t.Fatal("complete form not read")
	}
	if f.Name != "哥特束柱" || f.Mount != "floor" || f.Triangles != 1200 || f.SizeM != [3]float64{0.5, 2.4, 0.5} {
		t.Fatalf("facts %+v", f)
	}
	if len(f.Slots) != 1 || f.Slots[0].Hint != "stone" || len(f.Parts) != 2 || len(f.Anchors) != 1 || len(f.ParamsSchema) != 2 || f.Params["height"] != 2.4 ||
		string(f.Materials["stone"]) != `{"preset":"marble@1","params":{}}` {
		t.Fatalf("manifest and parameters %+v", f)
	}
	raw, err := json.Marshal(f)
	if err != nil || strings.Contains(string(raw), "export default") || strings.Contains(string(raw), "code") {
		t.Fatalf("facts carry the code: %s", raw)
	}

	brief, err := json.Marshal(f.Brief())
	if err != nil {
		t.Fatal(err)
	}
	var b map[string]any
	if err := json.Unmarshal(brief, &b); err != nil {
		t.Fatal(err)
	}
	if b["parts"] != nil || b["anchors"] != nil || b["params_schema"] != nil {
		t.Fatalf("brief carries parts, anchors or definitions: %s", brief)
	}
	if !strings.Contains(string(brief), `"params":["flutes","height"]`) || !strings.Contains(string(brief), `"size_m":[0.5,2.4,0.5]`) || b["mount"] != "floor" ||
		!strings.Contains(string(brief), `"materials":{"stone":{"preset":"marble@1","params":{}}}`) {
		t.Fatalf("brief %s", brief)
	}

	// A model the scene baked has no materials of its own.
	baked := creativecontent.Payload{Model: json.RawMessage(`{"format":"glb","manifest":{"triangles":12},"generator":{"mount":"free","params_schema":{},"params":{}}}`)}
	plain, ok := creativecontent.ModelFactsOf(baked)
	if !ok || plain.Materials == nil || len(plain.Materials) != 0 {
		t.Fatalf("baked model %+v", plain)
	}
	if brief, err := json.Marshal(plain.Brief()); err != nil || !strings.Contains(string(brief), `"materials":{}`) {
		t.Fatalf("baked brief %s %v", brief, err)
	}

	staged := creativecontent.Payload{Model: json.RawMessage(`{"format":"glb","manifest":{"triangles":12}}`)}
	if _, ok := creativecontent.ModelFactsOf(staged); ok {
		t.Fatal("staged form read as complete")
	}
	if _, ok := creativecontent.ModelFactsOf(creativecontent.Payload{}); ok {
		t.Fatal("empty payload read as a model")
	}
}

// data: 模型内容「有效槽位」: the GLB's slots, then those a face assignment
// brings, with the triangles assigned to each (scene-face-materials 3.4).
func TestEffectiveSlots(t *testing.T) {
	glb := []creativeglb.Slot{{ID: "stone", Hint: "stone"}, {ID: "trim"}}
	faces := json.RawMessage(`{"meshes":[{"assign":{"stone":[[0,2]],"face_2":[[2,3]]}},{"assign":{"face_1":[[0,4],[6,8]]}}],"hints":{"face_1":"brick"}}`)
	got, err := json.Marshal(creativecontent.EffectiveSlots(glb, faces))
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"id":"stone","hint":"stone","source":"model","face_triangles":2},{"id":"trim","source":"model"},{"id":"face_1","hint":"brick","source":"faces","face_triangles":6},{"id":"face_2","source":"faces","face_triangles":1}]`
	if string(got) != want {
		t.Errorf("effective slots\n got %s\nwant %s", got, want)
	}
	for _, none := range []json.RawMessage{nil, json.RawMessage(`null`)} {
		if got := creativecontent.EffectiveSlots(glb, none); len(got) != 2 || got[1] != (creativecontent.Slot{ID: "trim", Source: "model"}) {
			t.Errorf("without faces: %+v", got)
		}
	}
	facts, ok := creativecontent.ModelFactsOf(creativecontent.Payload{Model: json.RawMessage(`{"format":"glb","manifest":{"bounds":{"min":[0,0,0],"max":[1,1,1]},"triangles":12,"slots":[{"id":"stone"}],"parts":[],"anchors":[]},"faces":` + string(faces) + `,"generator":{"mount":"floor","params_schema":{},"params":{}}}`)})
	if !ok || len(facts.Slots) != 3 || facts.Brief().Slots[2].ID != "face_2" {
		t.Errorf("model facts carry the effective slots: %+v", facts.Slots)
	}
}
