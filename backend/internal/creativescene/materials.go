package creativescene

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
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

// MaterialCatalog lists the materials for the assistant's tool description:
// ID, label and parameter names, in catalog order.
func MaterialCatalog() string {
	parts := make([]string, len(materials.list))
	for i, m := range materials.list {
		parts[i] = fmt.Sprintf("%s %s(%s)", m.ID, m.Label, strings.Join(paramNames(m), ","))
	}
	return strings.Join(parts, "；")
}
