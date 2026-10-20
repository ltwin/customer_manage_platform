package creativecontent_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
)

func TestModelFactsOfCompleteAndStagedForms(t *testing.T) {
	complete := creativecontent.Payload{Model: json.RawMessage(`{"format":"glb","name_hint":"哥特束柱",
 "manifest":{"bounds":{"min":[-0.25,0,-0.25],"max":[0.25,2.4004,0.25]},"triangles":1200,
  "slots":[{"id":"stone","hint":"stone"}],"parts":["base","shaft"],"anchors":[{"id":"top","position":[0,2.4,0]}]},
 "generator":{"code":"export default 1","code_sha256":"sha256-x","mount":"floor",
  "params_schema":{"height":{"type":"number","default":2},"flutes":{"type":"integer","default":8}},"params":{"height":2.4,"flutes":8}}}`)}
	f, ok := creativecontent.ModelFactsOf(complete)
	if !ok {
		t.Fatal("complete form not read")
	}
	if f.Name != "哥特束柱" || f.Mount != "floor" || f.Triangles != 1200 || f.SizeM != [3]float64{0.5, 2.4, 0.5} {
		t.Fatalf("facts %+v", f)
	}
	if len(f.Slots) != 1 || f.Slots[0].Hint != "stone" || len(f.Parts) != 2 || len(f.Anchors) != 1 || len(f.ParamsSchema) != 2 || f.Params["height"] != 2.4 {
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
	if !strings.Contains(string(brief), `"params":["flutes","height"]`) || !strings.Contains(string(brief), `"size_m":[0.5,2.4,0.5]`) || b["mount"] != "floor" {
		t.Fatalf("brief %s", brief)
	}

	staged := creativecontent.Payload{Model: json.RawMessage(`{"format":"glb","manifest":{"triangles":12}}`)}
	if _, ok := creativecontent.ModelFactsOf(staged); ok {
		t.Fatal("staged form read as complete")
	}
	if _, ok := creativecontent.ModelFactsOf(creativecontent.Payload{}); ok {
		t.Fatal("empty payload read as a model")
	}
}
