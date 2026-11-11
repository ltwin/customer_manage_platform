package creativescene

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// materialsJSON is the only copy of the material catalog: the principled
// BSDF and the procedural presets a material slot may name. make generate
// copies it to the frontend, which shades from it. The scene command checks
// slots by shape only (old or hand-written data may name presets this build
// does not know); the assistant's edits are checked against the catalog.
//
// A preset never changes once published: a different look is a new @2, so a
// scene keeps how it looked.
//
//go:embed materials.json
var materialsJSON []byte

const (
	MaterialPrincipled = "principled"
	MaterialPattern    = "pattern"
	MaterialTextured   = "textured"
)

// materialSpec is a catalog entry. A textured one (builtin_textures.go) has
// the fields after Params instead of its own parameters, and takes
// principled's table with its defaults once loaded.
type materialSpec struct {
	ID       string                `json:"id"`
	Kind     string                `json:"kind"`
	Label    string                `json:"label"`
	Params   map[string]paramSpec  `json:"params"`
	Category string                `json:"category"`
	SizeM    float64               `json:"size_m"`
	Maps     map[string]builtinMap `json:"maps"`
	Defaults map[string]any        `json:"defaults"`
	Files    []BuiltinTextureFile  `json:"files"`
	Source   *builtinSource        `json:"source"`
	Listed   *bool                 `json:"listed"`
}

type materialCatalog struct {
	list       []materialSpec
	byID       map[string]materialSpec
	hints      map[string]string
	categories []materialCategory
	// files are the textured entries' files, each once.
	files []BuiltinTextureFile
}

var materials = loadMaterials(materialsJSON)

// loadMaterials panics on a malformed catalog: it ships with the binary, so a
// bad entry is a build mistake rather than bad input.
func loadMaterials(raw []byte) materialCatalog {
	var file struct {
		Materials  []materialSpec     `json:"materials"`
		Categories []materialCategory `json:"categories"`
		Hints      map[string]string  `json:"hints"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	dec.UseNumber()
	if err := dec.Decode(&file); err != nil {
		panic(fmt.Sprintf("creativescene: materials.json: %v", err))
	}
	c := materialCatalog{list: file.Materials, byID: make(map[string]materialSpec, len(file.Materials)), hints: file.Hints, categories: file.Categories}
	for _, cat := range file.Categories {
		if !slotKeyPattern.MatchString(cat.ID) || cat.Label == "" {
			panic(fmt.Sprintf("creativescene: material category %q is malformed", cat.ID))
		}
	}
	for i, m := range file.Materials {
		if !versionedID.MatchString(m.ID) || m.Label == "" || !slices.Contains([]string{MaterialPrincipled, MaterialPattern, MaterialTextured}, m.Kind) {
			panic(fmt.Sprintf("creativescene: material %q is malformed", m.ID))
		}
		if _, dup := c.byID[m.ID]; dup {
			panic(fmt.Sprintf("creativescene: material %q listed twice", m.ID))
		}
		if m.Kind == MaterialTextured {
			c.byID[m.ID] = m // given its parameters below, once principled is read
			continue
		}
		if len(m.Params) == 0 || len(m.Params) > maxSlotParams || m.hasTexturedFields() {
			panic(fmt.Sprintf("creativescene: material %q is malformed", m.ID))
		}
		for name, p := range m.Params {
			if !slotKeyPattern.MatchString(name) || p.Label == "" || !slices.Contains([]string{"number", "color", "boolean"}, p.Type) || p.Type == "number" && (p.Min == nil || p.Max == nil || *p.Min > *p.Max) {
				panic(fmt.Sprintf("creativescene: %s.%s is malformed", m.ID, name))
			}
			v, err := p.value(p.Default)
			if err != nil {
				panic(fmt.Sprintf("creativescene: %s.%s default: %v", m.ID, name, err))
			}
			p.Default = v
			m.Params[name] = p
		}
		file.Materials[i] = m
		c.byID[m.ID] = m
	}
	seen := map[string]bool{}
	for i, m := range file.Materials {
		if m.Kind != MaterialTextured {
			continue
		}
		m, err := completeTextured(m, c.byID[principledPreset], c.categories)
		if err != nil {
			panic(fmt.Sprintf("creativescene: material %q: %v", m.ID, err))
		}
		file.Materials[i] = m
		c.byID[m.ID] = m
		for _, f := range m.Files {
			if !seen[f.Name] {
				seen[f.Name] = true
				f.Material = m.ID
				c.files = append(c.files, f)
			}
		}
	}
	for hint, id := range c.hints {
		if m, ok := c.byID[id]; !ok || m.Kind != MaterialPattern || hint != strings.ToLower(strings.TrimSpace(hint)) {
			panic(fmt.Sprintf("creativescene: material hint %q → %q is malformed", hint, id))
		}
	}
	return c
}

// hintPreset is the preset a model's slot hint stands for: the hint
// lowercased and trimmed, matched whole.
func hintPreset(hint string) (string, bool) {
	id, ok := materials.hints[strings.ToLower(strings.TrimSpace(hint))]
	return id, ok
}

// checkMaterial is the catalog check for the assistant's slots: a known
// material, parameters it has, in range, and textures as the data contract
// has them.
func checkMaterial(slot MaterialSlot) error {
	m, ok := materials.byID[slot.Preset]
	if !ok {
		return fmt.Errorf("未知材质 %q，可用：%s", slot.Preset, strings.Join(materialIDs(), "、"))
	}
	if _, err := checkTextures(slot); err != nil {
		return err
	}
	for k, v := range slot.Params {
		p, ok := m.Params[k]
		if !ok {
			return fmt.Errorf("%s 没有参数 %q，可用参数：%s", m.ID, k, strings.Join(paramNames(m), "、"))
		}
		if _, err := p.value(v); err != nil {
			return fmt.Errorf("%s 的参数 %s %w", m.ID, k, err)
		}
	}
	return nil
}

func materialIDs() []string {
	out := make([]string, len(materials.list))
	for i, m := range materials.list {
		out[i] = m.ID
	}
	return out
}

func paramNames(m materialSpec) []string {
	names := make([]string, 0, len(m.Params))
	for n := range m.Params {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// MaterialCatalog lists the materials for the assistant's tool description,
// one per entry in catalog order: ID, label and parameter names, with the
// range of a number that is not 0–1 (colours are #rrggbb). Textured ones are
// left out: the assistant finds them with search_assets.
func MaterialCatalog() string {
	var parts []string
	for _, m := range materials.list {
		if m.Kind == MaterialTextured {
			continue
		}
		names := paramNames(m)
		for j, n := range names {
			if p := m.Params[n]; p.Type == "number" && (*p.Min != 0 || *p.Max != 1) {
				names[j] = fmt.Sprintf("%s %s–%s%s", n, strconv.FormatFloat(*p.Min, 'g', -1, 64), strconv.FormatFloat(*p.Max, 'g', -1, 64), p.Unit)
			}
		}
		parts = append(parts, fmt.Sprintf("%s %s(%s)", m.ID, m.Label, strings.Join(names, ", ")))
	}
	return strings.Join(parts, "；")
}

// decodeSlot reads a slot an update merges as the scene command reads it:
// strictly. The merged slots go to the command whole, so a misspelt key in
// maps or mapping would otherwise be dropped instead of refused.
func decodeSlot(raw json.RawMessage, op int, path string) (MaterialSlot, error) {
	var slot MaterialSlot
	if err := creativeops.Decode(raw, &slot); err != nil {
		var field *creativeops.FieldError
		if err := shapeError(raw, reflect.TypeFor[MaterialSlot](), path); errors.As(err, &field) {
			field.Op = op
			return slot, err
		}
		return slot, &creativeops.FieldError{Err: creativeops.ErrValidation, Op: op, Path: path, Message: path + " 须为 {preset, params} 或 null"}
	}
	return slot, nil
}

// fillBuiltinMaterials writes a built-in model's materials into the
// assistant's add_objects items and set_instance_model operations that place
// it without material_slots, as the editor does (data: 内置模型库「放置」).
// Anything unreadable or unknown is left for the scene command to report.
func fillBuiltinMaterials(ops []json.RawMessage) ([]json.RawMessage, error) {
	fill := func(o map[string]json.RawMessage) (bool, error) {
		var m struct {
			Builtin string `json:"builtin"`
		}
		if _, written := o["material_slots"]; written || json.Unmarshal(o["model"], &m) != nil || m.Builtin == "" {
			return false, nil
		}
		slots, ok := builtinMaterials(m.Builtin)
		if !ok {
			return false, nil
		}
		raw, err := json.Marshal(slots)
		o["material_slots"] = raw
		return true, err
	}
	out := slices.Clone(ops)
	for i, raw := range ops {
		var op map[string]json.RawMessage
		var name string
		if json.Unmarshal(raw, &op) != nil || json.Unmarshal(op["op"], &name) != nil {
			continue
		}
		changed := false
		switch name {
		case "add_objects":
			var items []map[string]json.RawMessage
			if json.Unmarshal(op["objects"], &items) != nil {
				continue
			}
			for _, it := range items {
				filled, err := fill(it)
				if err != nil {
					return nil, err
				}
				changed = changed || filled
			}
			if changed {
				list, err := json.Marshal(items)
				if err != nil {
					return nil, err
				}
				op["objects"] = list
			}
		case "set_instance_model":
			var err error
			if changed, err = fill(op); err != nil {
				return nil, err
			}
		}
		if changed {
			var err error
			if out[i], err = json.Marshal(op); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

// resolveSlots is the assistant's material slots (design「助手：材质、灯光与
// 环境」): an update_objects entry's material_slots merges by slot into what
// the object has, null clearing a slot, and goes to the command whole, as the
// command replaces it. Every slot written names a slot the object has (main
// for a primitive, the manifest's for a model, the catalog's for a built-in
// one) and a material of the catalog with parameters in range. The model's
// slots are the snapshot's: an object whose model the batch also swaps is
// refused, as the scene command would refuse its content changing twice, with
// the way to do it. A set_instance_model's material_slots name the new
// model's slots.
func resolveSlots(ctx context.Context, tx store.TxAccountScope, ops []json.RawMessage, cur Scene) ([]json.RawMessage, error) {
	type item struct {
		ID            string                      `json:"id"`
		ObjectID      string                      `json:"object_id"`
		Kind          string                      `json:"kind"`
		Model         *ModelRef                   `json:"model"`
		Primitive     map[string]json.RawMessage  `json:"primitive"`
		MaterialSlots map[string]*json.RawMessage `json:"material_slots"`
	}
	type decoded struct {
		Op            string                      `json:"op"`
		Objects       []json.RawMessage           `json:"objects"`
		Updates       []json.RawMessage           `json:"updates"`
		ObjectID      string                      `json:"object_id"`
		ObjectIDs     []string                    `json:"object_ids"`
		Model         *ModelRef                   `json:"model"`
		MaterialSlots map[string]*json.RawMessage `json:"material_slots"`
	}
	heads := make([]decoded, len(ops))
	slotted := false
	for i, raw := range ops {
		if json.Unmarshal(raw, &heads[i]) != nil {
			return ops, nil // the scene command reports the shape
		}
		if heads[i].MaterialSlots != nil {
			slotted = true
		}
		for _, raws := range [][]json.RawMessage{heads[i].Objects, heads[i].Updates} {
			for _, r := range raws {
				var probe map[string]json.RawMessage
				if json.Unmarshal(r, &probe) == nil && probe["material_slots"] != nil {
					slotted = true
				}
			}
		}
	}
	if !slotted {
		return ops, nil
	}

	// What each object is and has, as the batch goes; faces is a builtin's
	// face assignment, whose slots take materials too (有效槽位).
	type state struct {
		kind    string
		model   string
		builtin string
		faces   *Faces
		slots   map[string]MaterialSlot
	}
	objects := make(map[string]*state, len(cur.Objects))
	for _, o := range cur.Objects {
		st := &state{kind: o.Kind, slots: o.MaterialSlots}
		if o.Model != nil {
			st.model, st.builtin = o.Model.ContentRevisionID, o.Model.Builtin
		}
		if o.Primitive != nil {
			st.faces = o.Primitive.Faces
		}
		objects[o.ID] = st
	}
	// A builtin's faces written in the batch; unreadable ones are the scene command's to report.
	writtenFaces := func(st *state, it item) {
		raw, ok := it.Primitive["faces"]
		if !ok {
			return
		}
		var f *Faces
		if json.Unmarshal(raw, &f) == nil {
			st.faces = f
		}
	}
	var revisions []string
	for _, h := range heads {
		if h.Model != nil && h.Model.ContentRevisionID != "" {
			revisions = append(revisions, h.Model.ContentRevisionID)
		}
		for _, r := range h.Objects {
			var it item
			if json.Unmarshal(r, &it) == nil && it.Model != nil && it.Model.ContentRevisionID != "" {
				revisions = append(revisions, it.Model.ContentRevisionID)
			}
		}
	}
	for _, st := range objects {
		if st.model != "" {
			revisions = append(revisions, st.model)
		}
	}
	slices.Sort(revisions)
	models, err := placedModelsInTx(ctx, tx, slices.Compact(revisions))
	if err != nil {
		return nil, err
	}
	slotNames := func(st *state) ([]string, bool) {
		switch st.kind {
		case KindPrimitive:
			return effectiveSlotIDs(builtinSlots, st.faces), true
		case KindModelInstance:
			if st.builtin != "" {
				b, ok := builtinModels.byID[st.builtin]
				if !ok {
					return nil, false // the scene command reports it
				}
				return slotIDs(b.Manifest), true
			}
			m, ok := models[st.model]
			if !ok || !m.usable {
				return nil, false // nothing to check against
			}
			return effectiveSlotIDs(slotIDs(m.payload.Manifest), m.payload.Faces), true
		}
		return []string{}, true
	}
	check := func(id string, st *state, name string, slot MaterialSlot) error {
		names, known := slotNames(st)
		if known && !slices.Contains(names, name) {
			if len(names) == 0 {
				return invalid("物件 %s 没有材质槽：只有基础体和模型实例有", id)
			}
			return invalid("物件 %s 没有材质槽 %q，可用：%s", id, name, strings.Join(names, "、"))
		}
		if err := checkMaterial(slot); err != nil {
			return invalid("物件 %s 的材质槽 %s：%v", id, name, err)
		}
		return nil
	}

	swapped, written := map[string]bool{}, map[string]bool{}
	bothChange := func(id string) error {
		return invalid("物件 %s 在同一次修改里既换模型又改材质槽，两者都改物件内容：请分两次提交，先换模型，再按新模型的槽位配材质", id)
	}
	out := make([]json.RawMessage, len(ops))
	for i, h := range heads {
		out[i] = ops[i]
		var items []json.RawMessage
		key := ""
		switch h.Op {
		case "add_objects":
			items, key = h.Objects, "objects"
		case "update_objects":
			items, key = h.Updates, "updates"
		case "set_instance_model":
			for _, id := range append([]string{h.ObjectID}, h.ObjectIDs...) {
				if written[id] {
					return nil, bothChange(id)
				}
				swapped[id] = true
			}
			if h.Model == nil || h.MaterialSlots == nil {
				continue
			}
			st := &state{kind: KindModelInstance, model: h.Model.ContentRevisionID, builtin: h.Model.Builtin}
			for _, name := range slices.Sorted(maps.Keys(h.MaterialSlots)) {
				v := h.MaterialSlots[name]
				if v == nil {
					continue // the scene command reports the shape
				}
				slot, err := decodeSlot(*v, i, "material_slots."+name)
				if err != nil {
					return nil, err
				}
				if err := check(h.ObjectID, st, name, slot); err != nil {
					return nil, err
				}
			}
			continue
		default:
			continue
		}
		next := make([]json.RawMessage, len(items))
		changed := false
		for j, raw := range items {
			next[j] = raw
			var it item
			if json.Unmarshal(raw, &it) != nil {
				continue // the scene command reports the shape
			}
			if h.Op == "add_objects" {
				st := &state{kind: it.Kind, slots: map[string]MaterialSlot{}}
				if it.Model != nil {
					st.model, st.builtin = it.Model.ContentRevisionID, it.Model.Builtin
				}
				writtenFaces(st, it)
				objects[it.ID] = st
				for name, v := range it.MaterialSlots {
					var slot MaterialSlot
					if v == nil || json.Unmarshal(*v, &slot) != nil {
						continue // the scene command reports the shape
					}
					if err := check(it.ID, st, name, slot); err != nil {
						return nil, err
					}
					st.slots[name] = slot
				}
				continue
			}
			st, ok := objects[it.ObjectID]
			if ok {
				writtenFaces(st, it)
			}
			if !ok || it.MaterialSlots == nil {
				continue // the scene command reports a missing object
			}
			if swapped[it.ObjectID] {
				return nil, bothChange(it.ObjectID)
			}
			written[it.ObjectID] = true
			merged := make(map[string]MaterialSlot, len(st.slots)+len(it.MaterialSlots))
			for name, slot := range st.slots {
				merged[name] = slot
			}
			for name, v := range it.MaterialSlots {
				if v == nil || string(*v) == "null" {
					delete(merged, name)
					continue
				}
				slot, err := decodeSlot(*v, i, fmt.Sprintf("updates[%d].material_slots.%s", j, name))
				if err != nil {
					return nil, err
				}
				if err := check(it.ObjectID, st, name, slot); err != nil {
					return nil, err
				}
				merged[name] = slot
			}
			st.slots = merged
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(raw, &fields); err != nil {
				return nil, err
			}
			whole, err := json.Marshal(merged)
			if err != nil {
				return nil, err
			}
			fields["material_slots"] = whole
			if next[j], err = json.Marshal(fields); err != nil {
				return nil, err
			}
			changed = true
		}
		if !changed {
			continue
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(ops[i], &fields); err != nil {
			return nil, err
		}
		list, err := json.Marshal(next)
		if err != nil {
			return nil, err
		}
		fields[key] = list
		if out[i], err = json.Marshal(fields); err != nil {
			return nil, err
		}
	}
	return out, nil
}
