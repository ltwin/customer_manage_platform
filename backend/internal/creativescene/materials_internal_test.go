package creativescene

import (
	"fmt"
	"strings"
	"testing"
)

// paramTable is one material's parameters as a line: what a published preset
// may never change.
func paramTable(m materialSpec) string {
	names := paramNames(m)
	parts := make([]string, len(names))
	for i, n := range names {
		p := m.Params[n]
		if p.Type == "color" {
			parts[i] = fmt.Sprintf("%s=%v", n, p.Default)
		} else {
			parts[i] = fmt.Sprintf("%s=%v[%g,%g]", n, p.Default, *p.Min, *p.Max)
		}
	}
	return m.Kind + " " + strings.Join(parts, " ")
}

// publishedMaterials locks every material's parameter table. A published
// preset never changes: a different look is a new @2.
var publishedMaterials = map[string]string{
	"concrete@1":    "pattern color=#a19d96 metalness=0[0,1] roughness=0.9[0,1] stains=0.3[0,1]",
	"dirt@1":        "pattern color=#6b5440 metalness=0[0,1] roughness=0.95[0,1]",
	"emissive@1":    "pattern color=#ffd9a0 metalness=0[0,1] roughness=0.5[0,1] strength=5[0,100]",
	"fabric@1":      "pattern color=#6e2b2b metalness=0[0,1] roughness=0.95[0,1] scale=0.01[0.002,0.1]",
	"grass@1":       "pattern color=#5b7a3a metalness=0[0,1] roughness=0.9[0,1]",
	"marble@1":      "pattern color=#e8e4dc metalness=0[0,1] roughness=0.25[0,1] scale=0.8[0.05,10] vein_color=#8c8780",
	"metal@1":       "pattern color=#8c8f93 metalness=1[0,1] roughness=0.45[0,1] rust=0.3[0,1]",
	"paint@1":       "pattern color=#c9c4bb metalness=0[0,1] roughness=0.6[0,1]",
	"plaster@1":     "pattern color=#d8d0c2 metalness=0[0,1] mottle=0.4[0,1] roughness=0.9[0,1]",
	"principled@1":  "principled anisotropy=0[0,1] anisotropy_rotation=0[-3.141593,3.141593] clearcoat=0[0,1] clearcoat_roughness=0[0,1] color=#cccccc emissive=#000000 emissive_intensity=1[0,100] ior=1.5[1,2.333] iridescence=0[0,1] iridescence_thickness=400[0,1200] metalness=0[0,1] opacity=1[0,1] roughness=0.5[0,1] sheen=0[0,1] sheen_color=#ffffff sheen_roughness=1[0,1] specular_color=#ffffff specular_intensity=1[0,1] thickness=0[0,10] transmission=0[0,1]",
	"stone@1":       "pattern color=#7d776d metalness=0[0,1] roughness=0.9[0,1] scale=1[0.05,10]",
	"stone_brick@1": "pattern brick_size=0.3[0.05,2] color=#8f8676 metalness=0[0,1] mortar_color=#bdb5a6 roughness=0.85[0,1]",
	"tile@1":        "pattern color=#e6e2da grout_color=#a9a49b metalness=0[0,1] roughness=0.35[0,1] tile_size=0.3[0.05,2]",
	"wood@1":        "pattern color=#8a5a36 metalness=0[0,1] roughness=0.6[0,1] scale=0.5[0.05,5]",
	"wood_plank@1":  "pattern color=#9a6a42 metalness=0[0,1] plank_width=0.15[0.05,1] roughness=0.55[0,1]",
}

func TestMaterialCatalog(t *testing.T) {
	if len(materials.list) != len(publishedMaterials) {
		t.Fatalf("%d materials, %d published: a new one needs its table here", len(materials.list), len(publishedMaterials))
	}
	for id, want := range publishedMaterials {
		m, ok := materials.byID[id]
		if !ok {
			t.Fatalf("published material %s is gone", id)
		}
		if got := paramTable(m); got != want {
			t.Fatalf("%s changed:\n got %s\nwant %s", id, got, want)
		}
	}
	// Every preset has a tint, roughness and metalness; principled has the 20 three.js fields.
	for _, m := range materials.list {
		for _, k := range []string{"color", "roughness", "metalness"} {
			if _, ok := m.Params[k]; !ok {
				t.Fatalf("%s has no %s", m.ID, k)
			}
		}
	}
	if n := len(materials.byID["principled@1"].Params); n != 20 {
		t.Fatalf("principled has %d params", n)
	}
	for hint, want := range map[string]string{"stone": "stone@1", " Wood ": "wood@1", "BRICK": "stone_brick@1"} {
		if got, ok := hintPreset(hint); !ok || got != want {
			t.Fatalf("hint %q → %q", hint, got)
		}
	}
	if _, ok := hintPreset("old stone"); ok {
		t.Fatal("hints match whole")
	}
	text := MaterialCatalog()
	for _, want := range []string{"principled@1 原理化 BSDF(anisotropy, anisotropy_rotation -3.141593–3.141593rad,", "ior 1–2.333,", "stone_brick@1 石砖(brick_size 0.05–2m, color, metalness, mortar_color, roughness)", "；emissive@1 自发光("} {
		if !strings.Contains(text, want) {
			t.Fatalf("catalog text lacks %q: %s", want, text)
		}
	}
}

func TestCheckMaterial(t *testing.T) {
	ok := MaterialSlot{Preset: "stone_brick@1", Params: map[string]any{"brick_size": 0.5, "mortar_color": "#ffffff"}}
	if err := checkMaterial(ok); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		slot MaterialSlot
		says string
	}{
		"unknown preset": {MaterialSlot{Preset: "lava@1"}, "可用：principled@1、paint@1"},
		"unknown param":  {MaterialSlot{Preset: "paint@1", Params: map[string]any{"gloss": 1}}, "可用参数：color、metalness、roughness"},
		"out of range":   {MaterialSlot{Preset: "principled@1", Params: map[string]any{"ior": 3.0}}, "ior 须在 1 到 2.333 之间"},
		"bad colour":     {MaterialSlot{Preset: "principled@1", Params: map[string]any{"color": "red"}}, "#rrggbb"},
	} {
		err := checkMaterial(c.slot)
		if err == nil || !strings.Contains(err.Error(), c.says) {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func TestMalformedMaterialCatalogPanics(t *testing.T) {
	for name, raw := range map[string]string{
		"unknown kind":    `{"materials":[{"id":"a@1","kind":"pbr","label":"甲","params":{"color":{"type":"color","default":"#ffffff","label":"色"}}}],"hints":{}}`,
		"default outside": `{"materials":[{"id":"a@1","kind":"pattern","label":"甲","params":{"roughness":{"type":"number","default":2,"min":0,"max":1,"label":"糙度"}}}],"hints":{}}`,
		"hint to unknown": `{"materials":[{"id":"a@1","kind":"pattern","label":"甲","params":{"color":{"type":"color","default":"#ffffff","label":"色"}}}],"hints":{"x":"b@1"}}`,
		"listed twice":    `{"materials":[{"id":"a@1","kind":"pattern","label":"甲","params":{"color":{"type":"color","default":"#ffffff","label":"色"}}},{"id":"a@1","kind":"pattern","label":"乙","params":{"color":{"type":"color","default":"#ffffff","label":"色"}}}],"hints":{}}`,
		"unknown field":   `{"materials":[],"hints":{},"extra":1}`,
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("no panic")
				}
			}()
			loadMaterials([]byte(raw))
		})
	}
}
