package creativecontent

import (
	"encoding/json"
	"math"
	"slices"

	"github.com/samson/customer-manage-platform/backend/internal/creativeglb"
)

// ManifestFacts is what a model's manifest tells the assistant: write_model
// reports exactly this, and ModelFacts carries it, so both say the same.
type ManifestFacts struct {
	SizeM     [3]float64           `json:"size_m"`
	Triangles int                  `json:"triangles"`
	Slots     []Slot               `json:"slots"`
	Parts     []string             `json:"parts"`
	Anchors   []creativeglb.Anchor `json:"anchors"`
}

// Slot is a material slot as the assistant sees it (data: 模型内容「有效槽位」):
// one of the GLB's (source model) or one a face assignment brings (source
// faces), with how many triangles the assignment gives it.
type Slot struct {
	ID            string `json:"id"`
	Hint          string `json:"hint,omitempty"`
	Source        string `json:"source"`
	FaceTriangles int    `json:"face_triangles,omitempty"`
}

// faceAssignment is the part of a face assignment that says which slots it
// brings; creativescene owns and checks the whole.
type faceAssignment struct {
	Meshes []struct {
		Assign map[string][][]int `json:"assign"`
	} `json:"meshes"`
	Hints map[string]string `json:"hints"`
}

// EffectiveSlots lists the slots an object shows: the GLB's, then those the
// face assignment (raw, null when there is none) brings, sorted, each once.
func EffectiveSlots(glb []creativeglb.Slot, faces json.RawMessage) []Slot {
	var f faceAssignment
	if len(faces) > 0 {
		_ = json.Unmarshal(faces, &f)
	}
	counts := map[string]int{}
	for _, m := range f.Meshes {
		for slot, ranges := range m.Assign {
			for _, r := range ranges {
				if len(r) == 2 {
					counts[slot] += r[1] - r[0]
				}
			}
		}
	}
	out := make([]Slot, 0, len(glb)+len(counts))
	for _, s := range glb {
		out = append(out, Slot{ID: s.ID, Hint: s.Hint, Source: "model", FaceTriangles: counts[s.ID]})
		delete(counts, s.ID)
	}
	extra := make([]string, 0, len(counts))
	for slot := range counts {
		extra = append(extra, slot)
	}
	slices.Sort(extra)
	for _, slot := range extra {
		out = append(out, Slot{ID: slot, Hint: f.Hints[slot], Source: "faces", FaceTriangles: counts[slot]})
	}
	return out
}

// ManifestFactsOf reads a manifest: its size in metres (rounded to
// millimetres), triangles, material slots (with those the face assignment
// brings), parts and anchors.
func ManifestFactsOf(m creativeglb.Manifest, faces json.RawMessage) ManifestFacts {
	f := ManifestFacts{Triangles: m.Triangles, Slots: EffectiveSlots(m.Slots, faces), Parts: m.Parts, Anchors: m.Anchors}
	for i := range f.SizeM {
		f.SizeM[i] = math.Round((m.Bounds.Max[i]-m.Bounds.Min[i])*1000) / 1000
	}
	return f
}

// ModelFacts is what the assistant is told about a complete model; the code
// is never part of it. Materials is a library asset's material assignment,
// what placing it writes into material_slots; other models have none.
type ModelFacts struct {
	Name  string `json:"name,omitempty"`
	Mount string `json:"mount"`
	ManifestFacts
	Materials    map[string]json.RawMessage `json:"materials"`
	ParamsSchema map[string]json.RawMessage `json:"params_schema"`
	Params       map[string]any             `json:"params"`
	// Omitted names the fields a result too large to carry whole left out.
	Omitted []string `json:"omitted,omitempty"`
}

// ModelBrief is the part of ModelFacts a search result carries. Parts and
// anchors are left out: up to 200 of each would crowd the page's bytes. The
// materials stay, so the assistant can place the asset without reading it.
type ModelBrief struct {
	Name      string                     `json:"name,omitempty"`
	SizeM     [3]float64                 `json:"size_m"`
	Mount     string                     `json:"mount"`
	Triangles int                        `json:"triangles"`
	Slots     []Slot                     `json:"slots"`
	Materials map[string]json.RawMessage `json:"materials"`
	Params    []string                   `json:"params"`
}

// ModelFactsOf decodes a model payload in its complete form; the staged form,
// which has no generator, reports false.
func ModelFactsOf(p Payload) (ModelFacts, bool) {
	var m struct {
		Format    string                     `json:"format"`
		NameHint  string                     `json:"name_hint"`
		Manifest  creativeglb.Manifest       `json:"manifest"`
		Faces     json.RawMessage            `json:"faces"`
		Materials map[string]json.RawMessage `json:"materials"`
		Generator *struct {
			Mount        string                     `json:"mount"`
			ParamsSchema map[string]json.RawMessage `json:"params_schema"`
			Params       map[string]any             `json:"params"`
		} `json:"generator"`
	}
	if len(p.Model) == 0 || json.Unmarshal(p.Model, &m) != nil || m.Format != "glb" || m.Generator == nil {
		return ModelFacts{}, false
	}
	if m.Materials == nil {
		m.Materials = map[string]json.RawMessage{}
	}
	return ModelFacts{Name: m.NameHint, Mount: m.Generator.Mount, ManifestFacts: ManifestFactsOf(m.Manifest, m.Faces),
		Materials: m.Materials, ParamsSchema: m.Generator.ParamsSchema, Params: m.Generator.Params}, true
}

// Brief keeps the name, size, mount, triangles, slots, materials and
// parameter names.
func (f ModelFacts) Brief() ModelBrief {
	params := make([]string, 0, len(f.ParamsSchema))
	for k := range f.ParamsSchema {
		params = append(params, k)
	}
	slices.Sort(params)
	return ModelBrief{Name: f.Name, SizeM: f.SizeM, Mount: f.Mount, Triangles: f.Triangles, Slots: f.Slots, Materials: f.Materials, Params: params}
}
