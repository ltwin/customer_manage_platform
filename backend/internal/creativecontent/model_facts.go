package creativecontent

import (
	"context"
	"encoding/json"
	"math"
	"slices"

	"github.com/samson/customer-manage-platform/backend/internal/creativeglb"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// ManifestFacts is what a model's manifest tells the assistant: write_model
// reports exactly this, and ModelFacts carries it, so both say the same.
type ManifestFacts struct {
	SizeM     [3]float64           `json:"size_m"`
	Triangles int                  `json:"triangles"`
	Slots     []Slot               `json:"slots"`
	Parts     []string             `json:"parts"`
	Anchors   []creativeglb.Anchor `json:"anchors"`
	// UV is whether every mesh has UVs; absent for a model baked before
	// manifests recorded it.
	UV *bool `json:"uv,omitempty"`
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

// EffectiveSlots lists the slots an object shows: the GLB's, then those a
// face assignment brings (assigns holds each mesh's), sorted by ID, each once.
// The one rule for every caller, the editor's included (models/faces.ts).
func EffectiveSlots(glb []creativeglb.Slot, assigns []map[string][][]int, hints map[string]string) []Slot {
	counts := map[string]int{}
	for _, assign := range assigns {
		for slot, ranges := range assign {
			counts[slot] += 0
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
		out = append(out, Slot{ID: slot, Hint: hints[slot], Source: "faces", FaceTriangles: counts[slot]})
	}
	return out
}

// effectiveSlotsOf is EffectiveSlots on a stored assignment (null when there
// is none).
func effectiveSlotsOf(glb []creativeglb.Slot, faces json.RawMessage) []Slot {
	var f faceAssignment
	if len(faces) > 0 {
		_ = json.Unmarshal(faces, &f)
	}
	assigns := make([]map[string][][]int, len(f.Meshes))
	for i, m := range f.Meshes {
		assigns[i] = m.Assign
	}
	return EffectiveSlots(glb, assigns, f.Hints)
}

// ManifestFactsOf reads a manifest: its size in metres (rounded to
// millimetres), triangles, material slots (with those the face assignment
// brings), parts, anchors and whether it has UVs.
func ManifestFactsOf(m creativeglb.Manifest, faces json.RawMessage) ManifestFacts {
	f := ManifestFacts{Triangles: m.Triangles, Slots: effectiveSlotsOf(m.Slots, faces), Parts: m.Parts, Anchors: m.Anchors, UV: m.UV}
	for i := range f.SizeM {
		f.SizeM[i] = math.Round((m.Bounds.Max[i]-m.Bounds.Min[i])*1000) / 1000
	}
	return f
}

// ModelFacts is what the assistant is told about a complete model; the code
// is never part of it. Materials is a library asset's material assignment,
// what placing it writes into material_slots; other models have none. An
// imported model (data: 模型内容「导入的模型」) has no code and no parameters,
// and says where it came from in Import.
type ModelFacts struct {
	Name     string          `json:"name,omitempty"`
	Mount    string          `json:"mount"`
	Imported bool            `json:"imported,omitempty"`
	Import   json.RawMessage `json:"import,omitempty"`
	ManifestFacts
	Materials map[string]json.RawMessage `json:"materials"`
	// Images are the images the materials' textures use (read_asset fills
	// them in): sizes only, never pixels.
	Images       map[string]TextureImage    `json:"images,omitempty"`
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
	Imported  bool                       `json:"imported,omitempty"`
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
		Import    json.RawMessage            `json:"import"`
		Generator *struct {
			Language     string                     `json:"language"`
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
	f := ModelFacts{Name: m.NameHint, Mount: m.Generator.Mount, ManifestFacts: ManifestFactsOf(m.Manifest, m.Faces),
		Materials: m.Materials, ParamsSchema: m.Generator.ParamsSchema, Params: m.Generator.Params}
	if m.Generator.Language == "imported" {
		f.Imported, f.Import = true, m.Import
		f.ParamsSchema, f.Params = map[string]json.RawMessage{}, map[string]any{}
	}
	return f, true
}

// Brief keeps the name, size, mount, whether it is imported, triangles,
// slots, materials and parameter names.
func (f ModelFacts) Brief() ModelBrief {
	params := make([]string, 0, len(f.ParamsSchema))
	for k := range f.ParamsSchema {
		params = append(params, k)
	}
	slices.Sort(params)
	return ModelBrief{Name: f.Name, SizeM: f.SizeM, Mount: f.Mount, Imported: f.Imported, Triangles: f.Triangles, Slots: f.Slots, Materials: f.Materials, Params: params}
}

// MaterialBrief is what a search result says about a material asset (data:
// 材质资产「助手可读」): its preset, which image each texture uses, how they
// are mapped and how the slot is transparent. Images come as ids only.
type MaterialBrief struct {
	Preset    string            `json:"preset"`
	Maps      map[string]string `json:"maps"`
	Mapping   string            `json:"mapping,omitempty"`
	AlphaMode string            `json:"alpha_mode,omitempty"`
}

// MaterialBriefOf decodes a material payload; anything else reports false.
func MaterialBriefOf(p Payload) (MaterialBrief, bool) {
	var m struct {
		Slot *struct {
			Preset string `json:"preset"`
			Maps   map[string]struct {
				ContentRevisionID string `json:"content_revision_id"`
			} `json:"maps"`
			Mapping *struct {
				Kind string `json:"kind"`
			} `json:"mapping"`
			AlphaMode string `json:"alpha_mode"`
		} `json:"slot"`
	}
	if len(p.Material) == 0 || json.Unmarshal(p.Material, &m) != nil || m.Slot == nil {
		return MaterialBrief{}, false
	}
	b := MaterialBrief{Preset: m.Slot.Preset, Maps: map[string]string{}, AlphaMode: m.Slot.AlphaMode}
	for kind, t := range m.Slot.Maps {
		b.Maps[kind] = t.ContentRevisionID
	}
	if m.Slot.Mapping != nil {
		b.Mapping = m.Slot.Mapping.Kind
	}
	return b, true
}

// MaterialFacts is what read_asset gives of a material asset (data: 材质资产
// 「助手可读」): the slot whole, as edit_scene takes it, and the size of each
// image its textures use.
type MaterialFacts struct {
	Slot   json.RawMessage         `json:"slot"`
	Images map[string]TextureImage `json:"images"`
}

// MaterialFactsOf decodes a material payload, Images left for the caller;
// anything else reports false.
func MaterialFactsOf(p Payload) (MaterialFacts, bool) {
	var m struct {
		Slot json.RawMessage `json:"slot"`
	}
	if len(p.Material) == 0 || json.Unmarshal(p.Material, &m) != nil || len(m.Slot) == 0 {
		return MaterialFacts{}, false
	}
	return MaterialFacts{Slot: m.Slot, Images: map[string]TextureImage{}}, true
}

// TextureImage is an image a texture uses as the assistant reads it.
type TextureImage struct {
	Usable bool `json:"usable"`
	Width  *int `json:"width,omitempty"`
	Height *int `json:"height,omitempty"`
}

// TextureImageIDs are the distinct images the slots' textures use, sorted.
func TextureImageIDs(slots ...json.RawMessage) []string {
	var out []string
	for _, raw := range slots {
		var slot struct {
			Maps map[string]struct {
				ContentRevisionID string `json:"content_revision_id"`
			} `json:"maps"`
		}
		if json.Unmarshal(raw, &slot) != nil {
			continue
		}
		for _, t := range slot.Maps {
			out = append(out, t.ContentRevisionID)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// TextureImagesInTx reads the original size of each image in one batch; one
// that is gone or not an image is unusable.
func TextureImagesInTx(ctx context.Context, tx store.TxAccountScope, ids []string) (map[string]TextureImage, error) {
	out := make(map[string]TextureImage, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	batch, err := ReadDisplayBatchInTx(ctx, tx, ids)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		var img TextureImage
		if m, ok := batch[id].ImageOriginal(); ok {
			img = TextureImage{Usable: true, Width: m.Width, Height: m.Height}
		}
		out[id] = img
	}
	return out, nil
}
