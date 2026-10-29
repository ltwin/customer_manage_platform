package creativescene

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

// data: 场景内置基础体「topology」: the one rule the editor and the server
// both use to decide whether a builtin's face assignment survives a parameter
// change (scene-face-materials 2.2).

func TestBuiltinTopologyChanged(t *testing.T) {
	with := func(id string, patch map[string]any) (builtinSpec, map[string]any) {
		b, err := lookupBuiltin(id)
		if err != nil {
			t.Fatal(err)
		}
		next, err := b.mergeParams(b.defaults(), patch)
		if err != nil {
			t.Fatal(err)
		}
		return b, next
	}
	cases := []struct {
		id      string
		patch   map[string]any
		changed bool
	}{
		{"box@1", map[string]any{"width": 5.0, "height": 2.0}, false},
		{"ground@1", map[string]any{"size": 100.0}, false},
		{"arch@1", map[string]any{"depth": 1.0}, false},
		{"arch@1", map[string]any{"opening_width": 0.4}, true},
		{"arch@1", map[string]any{"width": 3.0}, true},
		{"cylinder@1", map[string]any{"height": 4.0, "radius_bottom": 2.0}, false},
		{"cylinder@1", map[string]any{"radius_top": 0.3}, false},
		{"cylinder@1", map[string]any{"radius_top": 0.0}, true},
		{"cylinder@1", map[string]any{"radial_segments": 12.0}, true},
		{"sphere@1", map[string]any{"radius": 3.0}, false},
		{"sphere@1", map[string]any{"segments": 16.0}, true},
		{"stairs@1", map[string]any{"step_height": 0.3, "width": 3.0}, false},
		{"stairs@1", map[string]any{"step_count": 4.0}, true},
	}
	for _, c := range cases {
		b, next := with(c.id, c.patch)
		if got := b.topologyChanged(b.defaults(), next); got != c.changed {
			t.Errorf("%s %v: changed = %v, want %v", c.id, c.patch, got, c.changed)
		}
	}

	// Parameters read back from a stored snapshot may be json.Number.
	b, next := with("cylinder@1", map[string]any{"radius_top": 0.0})
	stored := map[string]any{}
	for k, v := range b.defaults() {
		stored[k] = json.Number(strconv.FormatFloat(v.(float64), 'g', -1, 64))
	}
	if !b.topologyChanged(stored, next) {
		t.Error("json.Number parameters compare by value")
	}
	if b.topologyChanged(stored, b.defaults()) {
		t.Error("the same values as json.Number and float64 are no change")
	}
}

func TestBuiltinTopologyDeclarationIsChecked(t *testing.T) {
	base := `{"builtin":"x@1","label":"x","mount":"floor",%s"params":{"n":{"type":"integer","default":1,"min":1,"max":9},"on":{"type":"boolean","default":true}}}`
	for name, topology := range map[string]string{
		"missing":                ``,
		"unknown parameter":      `"topology":["m"],`,
		"zero_split non-numeric": `"topology":[{"param":"on","zero_split":true}],`,
		"unknown field":          `"topology":[{"param":"n","zero":true}],`,
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("a bad declaration must stop the catalog from loading")
				}
			}()
			loadBuiltins([]byte("[" + strings.Replace(base, "%s", topology, 1) + "]"))
		})
	}
	ok := loadBuiltins([]byte("[" + strings.Replace(base, "%s", `"topology":["n",{"param":"n","zero_split":true}],`, 1) + "]"))
	if rules := *ok["x@1"].Topology; len(rules) != 2 || rules[0] != (topologyRule{Param: "n"}) || rules[1] != (topologyRule{Param: "n", ZeroSplit: true}) {
		t.Errorf("rules = %+v", rules)
	}
	for id, b := range builtins {
		if b.Topology == nil {
			t.Errorf("%s declares no topology", id)
		}
	}
}
