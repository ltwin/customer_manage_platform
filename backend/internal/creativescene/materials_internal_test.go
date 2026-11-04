package creativescene

import (
	"fmt"
	"maps"
	"slices"
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

// texturedLook is what a textured entry looks like as a line: its size, the
// files and channels of its maps, and its defaults.
func texturedLook(m materialSpec) string {
	parts := []string{fmt.Sprintf("%gm", m.SizeM)}
	for _, kind := range builtinMapIDs {
		if t, ok := m.Maps[kind]; ok {
			parts = append(parts, kind+"="+t.File[:12]+t.Channel)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(m.Defaults)) {
		parts = append(parts, fmt.Sprintf("%s=%v", name, m.Params[name].Default))
	}
	return strings.Join(parts, " ")
}

// publishedTextured locks every built-in textured material's look (size,
// files with channels, defaults): a different look is a new @2.
var publishedTextured = map[string]string{
	"wood_floor@1":              "1.7m base_color=7c3ff1164c8d normal=eff2e4db46ba roughness=dc024a5c535cg metalness=dc024a5c535cb ao=dc024a5c535cr color=#ffffff metalness=1 roughness=1",
	"laminate_floor_02@1":       "1.7m base_color=2a1e6519309b normal=49aa58c34c67 roughness=d646acf8dedeg metalness=d646acf8dedeb ao=d646acf8deder color=#ffffff metalness=1 roughness=1",
	"oak_veneer_01@1":           "1.83m base_color=0718c2e1a209 normal=e74a9d3ddcd8 roughness=c1a3453f517fg metalness=c1a3453f517fb ao=c1a3453f517fr color=#ffffff metalness=1 roughness=1",
	"weathered_brown_planks@1":  "1.8m base_color=070ebc4c56a6 normal=1c4882afd62d roughness=61fbdcbd05b7g metalness=61fbdcbd05b7b ao=61fbdcbd05b7r color=#ffffff metalness=1 roughness=1",
	"rock_face_03@1":            "2.7m base_color=644ee79df7ff normal=a498cef4ba61 roughness=1921303981d1g metalness=1921303981d1b ao=1921303981d1r color=#ffffff metalness=1 roughness=1",
	"marble_01@1":               "1.5m base_color=d40378617171 normal=d5e17ccb2913 roughness=28a022c868fag metalness=28a022c868fab ao=28a022c868far color=#ffffff metalness=1 roughness=1",
	"red_brick@1":               "1.4m base_color=a1ae5ba7e630 normal=dd2ce5b5c42f roughness=2e732048bffdg metalness=2e732048bffdb ao=2e732048bffdr color=#ffffff metalness=1 roughness=1",
	"brick_wall_001@1":          "3m base_color=aefa7f374328 normal=62cc36d391b2 roughness=0d6c6a14399cg metalness=0d6c6a14399cb ao=0d6c6a14399cr color=#ffffff metalness=1 roughness=1",
	"medieval_blocks_03@1":      "2m base_color=63cc2220ed9e normal=16c8a4e48287 roughness=50b7486187a3g metalness=50b7486187a3b ao=50b7486187a3r color=#ffffff metalness=1 roughness=1",
	"concrete_floor_worn_001@1": "3m base_color=e29b45cdcd6d normal=3b782137e34d roughness=f239a75b8b81g metalness=f239a75b8b81b ao=f239a75b8b81r color=#ffffff metalness=1 roughness=1",
	"painted_plaster_wall@1":    "2m base_color=6fd812ed8dd5 normal=2d601a46ed5c roughness=8d6a2e391e87g metalness=8d6a2e391e87b ao=8d6a2e391e87r color=#ffffff metalness=1 roughness=1",
	"beige_wall_001@1":          "3m base_color=222d4d2e7d5c normal=7f5cbecd6766 roughness=a86cfe132a80g metalness=a86cfe132a80b ao=a86cfe132a80r color=#ffffff metalness=1 roughness=1",
	"metal_plate@1":             "0.5m base_color=864c9a653acd normal=146f8c00874a roughness=3b944444e128g metalness=3b944444e128b ao=3b944444e128r color=#ffffff metalness=1 roughness=1",
	"rusty_metal_02@1":          "1m base_color=4c887dd400ed normal=a7aa9c3e4b68 roughness=500189e41e84g metalness=500189e41e84b ao=500189e41e84r color=#ffffff metalness=1 roughness=1",
	"fabric_pattern_07@1":       "0.4m base_color=1f4941638b89 normal=84f15ee5f6b6 roughness=68b0e04fbee9g metalness=68b0e04fbee9b ao=68b0e04fbee9r color=#ffffff metalness=1 roughness=1",
	"denim_fabric@1":            "0.1m base_color=164fdc3adbae normal=832028890fa5 roughness=23b5b0d54a67g metalness=23b5b0d54a67b ao=23b5b0d54a67r color=#ffffff metalness=1 roughness=1",
	"forrest_ground_01@1":       "2m base_color=e4d557ff61a0 normal=aa23fa930c63 roughness=abc91717bc60g metalness=abc91717bc60b ao=abc91717bc60r color=#ffffff metalness=1 roughness=1",
	"leafy_grass@1":             "2m base_color=8e1c6d21365d normal=df0cf0ce96e6 roughness=70d61c669fe9g metalness=70d61c669fe9b ao=70d61c669fe9r color=#ffffff metalness=1 roughness=1",
	"sandy_gravel_02@1":         "2.53m base_color=db0d563089e4 normal=ad1e336887cb roughness=0279eea78d3fg metalness=0279eea78d3fb ao=0279eea78d3fr color=#ffffff metalness=1 roughness=1",
	"floor_tiles_06@1":          "3m base_color=fe326c219d09 normal=930ee26e4018 roughness=f3dd52d2363bg metalness=f3dd52d2363bb ao=f3dd52d2363br color=#ffffff metalness=1 roughness=1",
	"terrazzo_tiles@1":          "2m base_color=26cc390c19e4 normal=48f79a943f62 roughness=4480ba6a92d3g metalness=4480ba6a92d3b ao=4480ba6a92d3r color=#ffffff metalness=1 roughness=1",
	"asphalt_02@1":              "3m base_color=28f5ba869055 normal=ffe49db71a0f roughness=ed70e88640e4g metalness=ed70e88640e4b ao=ed70e88640e4r color=#ffffff metalness=1 roughness=1",
	"roof_slates_03@1":          "3m base_color=955eb467fc60 normal=f797ad9756e6 roughness=d3bae518a1ebg metalness=d3bae518a1ebb ao=d3bae518a1ebr color=#ffffff metalness=1 roughness=1",
	"brown_leather@1":           "0.4m base_color=b0de2403a31e normal=3386277290af roughness=3c043ed2e459g metalness=3c043ed2e459b ao=3c043ed2e459r color=#ffffff metalness=1 roughness=1",
}

func TestMaterialCatalog(t *testing.T) {
	if len(materials.list) != len(publishedMaterials)+len(publishedTextured) {
		t.Fatalf("%d materials, %d published: a new one needs its table here", len(materials.list), len(publishedMaterials)+len(publishedTextured))
	}
	for id, want := range publishedTextured {
		m, ok := materials.byID[id]
		if !ok {
			t.Fatalf("published material %s is gone", id)
		}
		if got := texturedLook(m); got != want {
			t.Fatalf("%s changed:\n got %s\nwant %s", id, got, want)
		}
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
