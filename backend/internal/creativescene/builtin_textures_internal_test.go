package creativescene

import (
	"strings"
	"testing"
)

// texturedEntry is a well-formed textured entry with one field swapped in.
func texturedEntry(swap string) string {
	f := strings.Repeat("a", 64) + ".jpg"
	fields := map[string]string{
		"category": `"category":"wood"`,
		"size_m":   `"size_m":1.7`,
		"maps":     `"maps":{"base_color":{"file":"` + f + `"},"roughness":{"file":"` + f + `","channel":"g"}}`,
		"defaults": `"defaults":{"color":"#ffffff","roughness":1}`,
		"files":    `"files":[{"name":"` + f + `","url":"https://dl.polyhaven.org/a.jpg","bytes":100}]`,
		"source":   `"source":{"site":"polyhaven","asset":"a","author":"甲","license":"CC0"}`,
	}
	if k, v, ok := strings.Cut(swap, "="); ok {
		if v == "" {
			delete(fields, k)
		} else {
			fields[k] = v
		}
	}
	parts := []string{`"id":"t@1"`, `"kind":"textured"`, `"label":"木"`}
	for _, k := range []string{"category", "size_m", "maps", "defaults", "files", "source", "params"} {
		if v, ok := fields[k]; ok {
			parts = append(parts, v)
		}
	}
	return `{"materials":[{"id":"principled@1","kind":"principled","label":"原理化","params":{"color":{"type":"color","default":"#cccccc","label":"色"},"roughness":{"type":"number","default":0.5,"min":0,"max":1,"label":"糙度"}}},{` +
		strings.Join(parts, ",") + `}],"categories":[{"id":"wood","label":"木材"}],"hints":{}}`
}

func TestTexturedEntry(t *testing.T) {
	c := loadMaterials([]byte(texturedEntry("")))
	m := c.byID["t@1"]
	// The parameter table is principled's, with the entry's defaults.
	if len(m.Params) != 2 || m.Params["color"].Default != "#ffffff" || m.Params["roughness"].Default != 1.0 || c.byID["principled@1"].Params["roughness"].Default != 0.5 {
		t.Fatalf("params %+v", m.Params)
	}
	if len(c.files) != 1 || c.files[0].Material != "t@1" {
		t.Fatalf("files %+v", c.files)
	}
}

func TestMalformedTexturedEntryPanics(t *testing.T) {
	other := strings.Repeat("b", 64) + ".jpg"
	for name, swap := range map[string]string{
		"own params":          `params="params":{"color":{"type":"color","default":"#ffffff","label":"色"}}`,
		"unknown category":    `category="category":"glass"`,
		"size out of range":   `size_m="size_m":200`,
		"no base colour":      `maps="maps":{"normal":{"file":"` + strings.Repeat("a", 64) + `.jpg"}}`,
		"unknown map":         `maps="maps":{"base_color":{"file":"` + strings.Repeat("a", 64) + `.jpg"},"height":{"file":"` + strings.Repeat("a", 64) + `.jpg"}}`,
		"channel on colour":   `maps="maps":{"base_color":{"file":"` + strings.Repeat("a", 64) + `.jpg","channel":"r"}}`,
		"file not listed":     `maps="maps":{"base_color":{"file":"` + other + `"}}`,
		"name not a sha256":   `files="files":[{"name":"wood.jpg","url":"https://x/a.jpg","bytes":100}]`,
		"plain http":          `files="files":[{"name":"` + strings.Repeat("a", 64) + `.jpg","url":"http://x/a.jpg","bytes":100}]`,
		"too large":           `files="files":[{"name":"` + strings.Repeat("a", 64) + `.jpg","url":"https://x/a.jpg","bytes":40000000}]`,
		"unused file":         `files="files":[{"name":"` + strings.Repeat("a", 64) + `.jpg","url":"https://x/a.jpg","bytes":100},{"name":"` + other + `","url":"https://x/b.jpg","bytes":100}]`,
		"default not a param": `defaults="defaults":{"gloss":1}`,
		"default outside":     `defaults="defaults":{"roughness":2}`,
		"not CC0":             `source="source":{"site":"polyhaven","asset":"a","author":"甲","license":"CC-BY"}`,
		"no source":           `source=`,
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("no panic")
				}
			}()
			loadMaterials([]byte(texturedEntry(swap)))
		})
	}
	// A preset with a textured field is as malformed.
	t.Run("pattern with a category", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatal("no panic")
			}
		}()
		loadMaterials([]byte(`{"materials":[{"id":"a@1","kind":"pattern","label":"甲","category":"wood","params":{"color":{"type":"color","default":"#ffffff","label":"色"}}}],"categories":[{"id":"wood","label":"木材"}],"hints":{}}`))
	})
}

func TestBuiltinCatalog(t *testing.T) {
	textured := 0
	for _, m := range materials.list {
		if m.Kind == MaterialTextured {
			textured++
		}
	}
	if textured != 24 {
		t.Fatalf("%d textured materials", textured)
	}
	// Each file once, every one of the 72 a different image.
	if files := BuiltinTextureFiles(); len(files) != 72 {
		t.Fatalf("%d files", len(files))
	}
	if err := checkMaterial(MaterialSlot{Preset: "wood_floor@1", Params: map[string]any{"color": "#eeeeee", "ior": 1.4}}); err != nil {
		t.Fatal(err)
	}
	if err := checkMaterial(MaterialSlot{Preset: "wood_floor@1", Params: map[string]any{"mortar_color": "#eeeeee"}}); err == nil {
		t.Fatal("a textured material takes principled's parameters only")
	}
	if strings.Contains(MaterialCatalog(), "wood_floor@1") {
		t.Fatal("the catalog text lists textured materials one by one")
	}
}
