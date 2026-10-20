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
	Slots     []creativeglb.Slot   `json:"slots"`
	Parts     []string             `json:"parts"`
	Anchors   []creativeglb.Anchor `json:"anchors"`
}

// ManifestFactsOf reads a manifest: its size in metres (rounded to
// millimetres), triangles, material slots, parts and anchors.
func ManifestFactsOf(m creativeglb.Manifest) ManifestFacts {
	f := ManifestFacts{Triangles: m.Triangles, Slots: m.Slots, Parts: m.Parts, Anchors: m.Anchors}
	for i := range f.SizeM {
		f.SizeM[i] = math.Round((m.Bounds.Max[i]-m.Bounds.Min[i])*1000) / 1000
	}
	return f
}

// ModelFacts is what the assistant is told about a complete model; the code
// is never part of it.
type ModelFacts struct {
	Name  string `json:"name,omitempty"`
	Mount string `json:"mount"`
	ManifestFacts
	ParamsSchema map[string]json.RawMessage `json:"params_schema"`
	Params       map[string]any             `json:"params"`
	// Omitted names the fields a result too large to carry whole left out.
	Omitted []string `json:"omitted,omitempty"`
}

// ModelBrief is the part of ModelFacts a search result carries. Parts and
// anchors are left out: up to 200 of each would crowd the page's bytes.
type ModelBrief struct {
	Name      string             `json:"name,omitempty"`
	SizeM     [3]float64         `json:"size_m"`
	Mount     string             `json:"mount"`
	Triangles int                `json:"triangles"`
	Slots     []creativeglb.Slot `json:"slots"`
	Params    []string           `json:"params"`
}

// ModelFactsOf decodes a model payload in its complete form; the staged form,
// which has no generator, reports false.
func ModelFactsOf(p Payload) (ModelFacts, bool) {
	var m struct {
		Format    string               `json:"format"`
		NameHint  string               `json:"name_hint"`
		Manifest  creativeglb.Manifest `json:"manifest"`
		Generator *struct {
			Mount        string                     `json:"mount"`
			ParamsSchema map[string]json.RawMessage `json:"params_schema"`
			Params       map[string]any             `json:"params"`
		} `json:"generator"`
	}
	if len(p.Model) == 0 || json.Unmarshal(p.Model, &m) != nil || m.Format != "glb" || m.Generator == nil {
		return ModelFacts{}, false
	}
	return ModelFacts{Name: m.NameHint, Mount: m.Generator.Mount, ManifestFacts: ManifestFactsOf(m.Manifest),
		ParamsSchema: m.Generator.ParamsSchema, Params: m.Generator.Params}, true
}

// Brief keeps the name, size, mount, triangles, slots and parameter names.
func (f ModelFacts) Brief() ModelBrief {
	params := make([]string, 0, len(f.ParamsSchema))
	for k := range f.ParamsSchema {
		params = append(params, k)
	}
	slices.Sort(params)
	return ModelBrief{Name: f.Name, SizeM: f.SizeM, Mount: f.Mount, Triangles: f.Triangles, Slots: f.Slots, Params: params}
}
