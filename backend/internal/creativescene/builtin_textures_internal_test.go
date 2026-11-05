package creativescene

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"

	"github.com/samson/customer-manage-platform/backend/internal/platform/immutablefs"
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

func builtinFile(body string) BuiltinTextureFile {
	sum := sha256.Sum256([]byte(body))
	return BuiltinTextureFile{Name: hex.EncodeToString(sum[:]) + ".jpg", URL: "https://example.test/" + body, Bytes: int64(len(body)), Material: "m@1"}
}

func TestImportBuiltinTextures(t *testing.T) {
	store, err := immutablefs.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	b := &BuiltinTextures{store: store}
	good, other, short, wrong, gone := builtinFile("good"), builtinFile("other"), builtinFile("short"), builtinFile("wrong"), builtinFile("gone")
	files := []BuiltinTextureFile{good, other, short, wrong, gone}
	served := map[string]string{good.URL: "good", other.URL: "other", short.URL: "sho", wrong.URL: "wronG"}
	var fetches atomic.Int32
	fetch := func(_ context.Context, f BuiltinTextureFile) ([]byte, error) {
		fetches.Add(1)
		body, ok := served[f.URL]
		if !ok {
			return nil, errors.New("HTTP 404")
		}
		return []byte(body), nil
	}

	missing, err := b.missing(t.Context(), files)
	if err != nil || len(missing) != 5 {
		t.Fatalf("missing %v %v", missing, err)
	}
	r := b.importFiles(t.Context(), files, fetch)
	if r.Imported != 2 || r.Skipped != 0 || len(r.Failed) != 3 {
		t.Fatalf("first import %+v", r)
	}
	reasons := map[string]string{}
	for _, f := range r.Failed {
		reasons[f.File] = f.Reason
		if f.Material != "m@1" {
			t.Fatalf("failure names no material: %+v", f)
		}
	}
	if !strings.Contains(reasons[short.Name], "大小") || !strings.Contains(reasons[wrong.Name], "sha256") || !strings.Contains(reasons[gone.Name], "404") {
		t.Fatalf("reasons %v", reasons)
	}
	// Nothing that failed was written; what was is read back as it came.
	if _, err := store.Stat(t.Context(), builtinTextureKey(wrong.Name)); !errors.Is(err, immutablefs.ErrNotFound) {
		t.Fatalf("a file that did not match was written: %v", err)
	}
	rc, size, err := b.open(t.Context(), files, good.Name)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(rc)
	_ = rc.Close()
	if string(body) != "good" || size != 4 {
		t.Fatalf("read %q %d", body, size)
	}

	// Again: what is there is skipped without fetching it.
	fetches.Store(0)
	served[short.URL], served[wrong.URL] = "short", "wrong"
	r = b.importFiles(t.Context(), files, fetch)
	if r.Imported != 2 || r.Skipped != 2 || len(r.Failed) != 1 || fetches.Load() != 3 {
		t.Fatalf("second import %+v after %d fetches", r, fetches.Load())
	}
	if missing, _ := b.missing(t.Context(), files); len(missing) != 1 || missing[0].Name != gone.Name {
		t.Fatalf("missing %v", missing)
	}

	// A name the catalog does not list is unknown; one not imported is missing.
	if _, _, err := b.open(t.Context(), files, builtinFile("elsewhere").Name); !errors.Is(err, ErrBuiltinTextureUnknown) {
		t.Fatalf("unlisted: %v", err)
	}
	if _, _, err := b.open(t.Context(), files, gone.Name); !errors.Is(err, ErrBuiltinTextureMissing) {
		t.Fatalf("not imported: %v", err)
	}
}

func TestBuiltinMaterials(t *testing.T) {
	ids := func(q string) []string {
		var out []string
		for _, m := range BuiltinMaterials(q) {
			out = append(out, m.ID)
		}
		return out
	}
	for q, want := range map[string]string{
		// Category, source asset name and label, case aside, in catalog order.
		"BRICK":      "red_brick@1 brick_wall_001@1 medieval_blocks_03@1",
		"砖墙":         "red_brick@1 brick_wall_001@1 medieval_blocks_03@1",
		"木地板":        "wood_floor@1",
		"metal rust": "rusty_metal_02@1",
		"玻璃":         "",
	} {
		if got := strings.Join(ids(q), " "); got != want {
			t.Errorf("%q: %q, want %q", q, got, want)
		}
	}
	if n := len(BuiltinMaterials("")); n != 24 {
		t.Fatalf("%d with no query", n)
	}
	m := BuiltinMaterials("木地板")[0]
	if m.Title != "木地板" || m.Category != "wood" || m.CategoryLabel != "木材" || m.SizeM != materials.byID["wood_floor@1"].SizeM || m.Slot.Preset != "wood_floor@1" || m.Slot.Params == nil || len(m.Slot.Params) != 0 {
		t.Fatalf("%+v", m)
	}
}

func TestBuiltinMaterialFacts(t *testing.T) {
	f, ok := BuiltinMaterialByID("wood_floor@1")
	if !ok || f.Slot.Preset != "wood_floor@1" || f.Source.License != "CC0" || f.Defaults["color"] != "#ffffff" || len(f.Defaults) != len(materials.byID[principledPreset].Params) {
		t.Fatalf("%+v", f)
	}
	for _, id := range []string{"principled@1", "nope@1", ""} {
		if _, ok := BuiltinMaterialByID(id); ok {
			t.Errorf("%q is not a built-in material", id)
		}
	}
	if ids := BuiltinMaterialIDs(); len(ids) != 24 || ids[0] != "wood_floor@1" {
		t.Fatalf("%v", ids)
	}
}

func TestBuiltinThumbnails(t *testing.T) {
	// Every built-in material has its own WebP thumbnail.
	seen := map[string]bool{}
	for _, id := range BuiltinMaterialIDs() {
		b, ok := BuiltinThumbnail(id)
		if !ok || len(b) < 12 || string(b[:4]) != "RIFF" || string(b[8:12]) != "WEBP" {
			t.Fatalf("%s: no WebP thumbnail", id)
		}
		sum := sha256.Sum256(b)
		if seen[string(sum[:])] {
			t.Fatalf("%s shares a thumbnail", id)
		}
		seen[string(sum[:])] = true
	}
	if _, ok := BuiltinThumbnail("principled@1"); ok {
		t.Fatal("a preset has no built-in thumbnail")
	}
	thumbs := fstest.MapFS{"builtin_thumbnails/wood_floor_1.webp": {Data: []byte("x")}}
	for name, fsys := range map[string]fstest.MapFS{
		"missing": thumbs,
		"stray":   {"builtin_thumbnails/wood_floor_1.webp": {Data: []byte("x")}, "builtin_thumbnails/gone_1.webp": {Data: []byte("x")}},
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil || !strings.Contains(fmt.Sprint(r), map[string]string{"missing": "no thumbnail", "stray": "no material"}[name]) {
					t.Fatalf("panic %v", r)
				}
			}()
			list := []materialSpec{{ID: "wood_floor@1", Kind: MaterialTextured}}
			if name == "missing" {
				list = append(list, materialSpec{ID: "red_brick@1", Kind: MaterialTextured})
			}
			loadThumbnails(fsys, list)
		})
	}
}
