package creativescene

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

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
)

type materialSpec struct {
	ID     string               `json:"id"`
	Kind   string               `json:"kind"`
	Label  string               `json:"label"`
	Params map[string]paramSpec `json:"params"`
}

type materialCatalog struct {
	list  []materialSpec
	byID  map[string]materialSpec
	hints map[string]string
}

var materials = loadMaterials(materialsJSON)

// loadMaterials panics on a malformed catalog: it ships with the binary, so a
// bad entry is a build mistake rather than bad input.
func loadMaterials(raw []byte) materialCatalog {
	var file struct {
		Materials []materialSpec    `json:"materials"`
		Hints     map[string]string `json:"hints"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	dec.UseNumber()
	if err := dec.Decode(&file); err != nil {
		panic(fmt.Sprintf("creativescene: materials.json: %v", err))
	}
	c := materialCatalog{list: file.Materials, byID: make(map[string]materialSpec, len(file.Materials)), hints: file.Hints}
	for i, m := range file.Materials {
		if !versionedID.MatchString(m.ID) || m.Label == "" || len(m.Params) == 0 || len(m.Params) > maxSlotParams || !slices.Contains([]string{MaterialPrincipled, MaterialPattern}, m.Kind) {
			panic(fmt.Sprintf("creativescene: material %q is malformed", m.ID))
		}
		if _, dup := c.byID[m.ID]; dup {
			panic(fmt.Sprintf("creativescene: material %q listed twice", m.ID))
		}
		for name, p := range m.Params {
			if !slotKeyPattern.MatchString(name) || p.Label == "" || p.Type != "number" && p.Type != "color" || p.Type == "number" && (p.Min == nil || p.Max == nil || *p.Min > *p.Max) {
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
// material, and parameters it has, in range.
func checkMaterial(slot MaterialSlot) error {
	m, ok := materials.byID[slot.Preset]
	if !ok {
		return fmt.Errorf("未知材质 %q，可用：%s", slot.Preset, strings.Join(materialIDs(), "、"))
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
// range of a number that is not 0–1 (colours are #rrggbb).
func MaterialCatalog() string {
	parts := make([]string, len(materials.list))
	for i, m := range materials.list {
		names := paramNames(m)
		for j, n := range names {
			if p := m.Params[n]; p.Type == "number" && (*p.Min != 0 || *p.Max != 1) {
				names[j] = fmt.Sprintf("%s %s–%s%s", n, strconv.FormatFloat(*p.Min, 'g', -1, 64), strconv.FormatFloat(*p.Max, 'g', -1, 64), p.Unit)
			}
		}
		parts[i] = fmt.Sprintf("%s %s(%s)", m.ID, m.Label, strings.Join(names, ", "))
	}
	return strings.Join(parts, "；")
}

// resolveSlots is the assistant's material slots (design「助手：材质、灯光与
// 环境」): an update_objects entry's material_slots merges by slot into what
// the object has, null clearing a slot, and goes to the command whole, as the
// command replaces it. Every slot written names a slot the object has (main
// for a primitive, the manifest's for a model) and a material of the catalog
// with parameters in range. The sky cannot be an HDRI: none is built in.
func resolveSlots(ctx context.Context, tx store.TxAccountScope, ops []json.RawMessage, cur Scene) ([]json.RawMessage, error) {
	type item struct {
		ID            string                      `json:"id"`
		ObjectID      string                      `json:"object_id"`
		Kind          string                      `json:"kind"`
		Model         *ModelRef                   `json:"model"`
		MaterialSlots map[string]*json.RawMessage `json:"material_slots"`
	}
	type decoded struct {
		Op          string            `json:"op"`
		Objects     []json.RawMessage `json:"objects"`
		Updates     []json.RawMessage `json:"updates"`
		Environment *struct {
			Sky struct {
				Kind string `json:"kind"`
			} `json:"sky"`
		} `json:"environment"`
	}
	heads := make([]decoded, len(ops))
	slotted := false
	for i, raw := range ops {
		if json.Unmarshal(raw, &heads[i]) != nil {
			return ops, nil // the scene command reports the shape
		}
		if heads[i].Op == "set_environment" && heads[i].Environment != nil && heads[i].Environment.Sky.Kind == "hdri" {
			return nil, invalid("第 %d 个操作：暂无内置 HDRI，请用程序化天空（procedural）或纯色（color）", i+1)
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

	// What each object is and has, as the batch goes.
	type state struct {
		kind  string
		model string
		slots map[string]MaterialSlot
	}
	objects := make(map[string]*state, len(cur.Objects))
	for _, o := range cur.Objects {
		st := &state{kind: o.Kind, slots: o.MaterialSlots}
		if o.Model != nil {
			st.model = o.Model.ContentRevisionID
		}
		objects[o.ID] = st
	}
	var revisions []string
	for _, h := range heads {
		for _, r := range h.Objects {
			var it item
			if json.Unmarshal(r, &it) == nil && it.Model != nil {
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
			return []string{"main"}, true
		case KindModelInstance:
			m, ok := models[st.model]
			if !ok || !m.usable {
				return nil, false // nothing to check against
			}
			names := make([]string, len(m.payload.Manifest.Slots))
			for i, sl := range m.payload.Manifest.Slots {
				names[i] = sl.ID
			}
			return names, true
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
					st.model = it.Model.ContentRevisionID
				}
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
			if !ok || it.MaterialSlots == nil {
				continue // the scene command reports a missing object
			}
			merged := make(map[string]MaterialSlot, len(st.slots)+len(it.MaterialSlots))
			for name, slot := range st.slots {
				merged[name] = slot
			}
			for name, v := range it.MaterialSlots {
				if v == nil || string(*v) == "null" {
					delete(merged, name)
					continue
				}
				var slot MaterialSlot
				if err := json.Unmarshal(*v, &slot); err != nil {
					return nil, invalid("物件 %s 的材质槽 %s 须为 {preset, params} 或 null", it.ObjectID, name)
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
