package creativescene

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/samson/customer-manage-platform/backend/internal/creativeglb"
	"github.com/samson/customer-manage-platform/backend/internal/platform/immutablefs"
)

// builtinModelEntry is a well-formed catalog entry: the xbot skeleton and its
// mapping from the shared rig vectors, and one slot.
func builtinModelEntry(t *testing.T) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "humanoid_rig.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Skeletons map[string]map[string]any `json:"skeletons"`
		Cases     []struct {
			Skeleton string         `json:"skeleton"`
			Rig      map[string]any `json:"rig"`
			Valid    bool           `json:"valid"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	var rig map[string]any
	for _, c := range doc.Cases {
		if c.Skeleton == "xbot" && c.Valid {
			rig = c.Rig
			break
		}
	}
	return map[string]any{
		"id": "body_test@1", "label": "测试素体", "category": "humanoid", "mount": "floor",
		"glb": map[string]any{"name": strings.Repeat("a", 64) + ".glb", "bytes": 1000},
		"manifest": map[string]any{
			"bounds": map[string]any{"min": []float64{-0.5, 0, -0.2}, "max": []float64{0.5, 1.8, 0.2}}, "triangles": 12,
			"slots": []any{map[string]any{"id": "body"}}, "parts": []any{}, "anchors": []any{}, "uv": true,
			"skeleton": doc.Skeletons["xbot"],
		},
		"rig":       rig,
		"materials": map[string]any{"body": map[string]any{"preset": "principled@1", "params": map[string]any{"roughness": 0.6}}},
		"source":    map[string]any{"site": "quaternius", "asset": "Universal Base Characters", "author": "Quaternius", "license": "CC0", "url": "https://quaternius.com/packs/universalbasecharacters.html"},
	}
}

func modelCatalog(t *testing.T, entries ...map[string]any) []byte {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"models": entries})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func modelThumbs(ids ...string) fstest.MapFS {
	fsys := fstest.MapFS{}
	for _, id := range ids {
		fsys["builtin_thumbnails/models/"+strings.ReplaceAll(id, "@", "_")+".webp"] = &fstest.MapFile{Data: []byte("RIFF0000WEBP")}
	}
	return fsys
}

func TestBuiltinModelCatalog(t *testing.T) {
	entry := builtinModelEntry(t)
	c, err := loadBuiltinModels(modelCatalog(t, entry), modelThumbs("body_test@1"))
	if err != nil {
		t.Fatal(err)
	}
	m := c.byID["body_test@1"]
	if m.Rig == nil || m.Rig.Humanoid["hips"] == 0 || m.Manifest.Skeleton == nil || len(c.files) != 1 || c.files[0].Model != "body_test@1" || string(c.thumbnails["body_test@1"]) != "RIFF0000WEBP" {
		t.Fatalf("%+v %+v", m, c.files)
	}
	// A new version may keep the file: listed once.
	again := builtinModelEntry(t)
	again["id"] = "body_test@2"
	c, err = loadBuiltinModels(modelCatalog(t, entry, again), modelThumbs("body_test@1", "body_test@2"))
	if err != nil || len(c.files) != 1 || len(c.list) != 2 {
		t.Fatalf("%v %+v", err, c.files)
	}
	// The catalog that ships loads.
	if _, err := loadBuiltinModels(modelsJSON, builtinThumbnailFS); err != nil {
		t.Fatal(err)
	}
}

func TestMalformedBuiltinModel(t *testing.T) {
	for name, tc := range map[string]struct {
		change func(m map[string]any)
		want   string
	}{
		"id without version": {func(m map[string]any) { m["id"] = "body" }, "id"},
		"no label":           {func(m map[string]any) { m["label"] = "" }, "label"},
		"unknown category":   {func(m map[string]any) { m["category"] = "prop" }, "品类"},
		"unknown mount":      {func(m map[string]any) { m["mount"] = "roof" }, "mount"},
		"name not a sha256":  {func(m map[string]any) { m["glb"] = map[string]any{"name": "body.glb", "bytes": 1000} }, "sha256"},
		"empty file": {func(m map[string]any) {
			m["glb"] = map[string]any{"name": strings.Repeat("a", 64) + ".glb", "bytes": 0}
		}, "大小"},
		"over the budget": {func(m map[string]any) {
			m["glb"] = map[string]any{"name": strings.Repeat("a", 64) + ".glb", "bytes": 201 << 20}
		}, "大小"},
		"no materials": {func(m map[string]any) { m["materials"] = map[string]any{} }, "materials"},
		"material not a slot": {func(m map[string]any) {
			m["materials"] = map[string]any{"skin": map[string]any{"preset": "principled@1", "params": map[string]any{}}}
		}, "skin"},
		"unknown preset": {func(m map[string]any) {
			m["materials"] = map[string]any{"body": map[string]any{"preset": "nope@1", "params": map[string]any{}}}
		}, "nope@1"},
		"parameter outside": {func(m map[string]any) {
			m["materials"] = map[string]any{"body": map[string]any{"preset": "principled@1", "params": map[string]any{"roughness": 3}}}
		}, "roughness"},
		"humanoid without rig": {func(m map[string]any) { delete(m, "rig") }, "rig.humanoid"},
		"rig without skeleton": {func(m map[string]any) { delete(m["manifest"].(map[string]any), "skeleton") }, "骨架"},
		"rig missing a bone": {func(m map[string]any) {
			delete(m["rig"].(map[string]any)["humanoid"].(map[string]any), "head")
		}, "head"},
		"not CC0":         {func(m map[string]any) { m["source"].(map[string]any)["license"] = "CC-BY" }, "CC0"},
		"plain http":      {func(m map[string]any) { m["source"].(map[string]any)["url"] = "http://quaternius.com" }, "https"},
		"unknown field":   {func(m map[string]any) { m["tags"] = []string{"人"} }, "tags"},
		"thumbnail stray": {func(m map[string]any) {}, "不属于"},
	} {
		t.Run(name, func(t *testing.T) {
			entry := builtinModelEntry(t)
			tc.change(entry)
			thumbs := modelThumbs("body_test@1")
			if name == "thumbnail stray" {
				thumbs = modelThumbs("body_test@1", "gone@1")
			}
			if _, err := loadBuiltinModels(modelCatalog(t, entry), thumbs); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
		})
	}
	t.Run("no thumbnail", func(t *testing.T) {
		if _, err := loadBuiltinModels(modelCatalog(t, builtinModelEntry(t)), fstest.MapFS{}); err == nil || !strings.Contains(err.Error(), "缩略图") {
			t.Fatal(err)
		}
	})
	t.Run("listed twice", func(t *testing.T) {
		if _, err := loadBuiltinModels(modelCatalog(t, builtinModelEntry(t), builtinModelEntry(t)), modelThumbs("body_test@1")); err == nil || !strings.Contains(err.Error(), "重复") {
			t.Fatal(err)
		}
	})
	t.Run("shared file said otherwise", func(t *testing.T) {
		other := builtinModelEntry(t)
		other["id"] = "body_test@2"
		other["manifest"].(map[string]any)["triangles"] = 13
		if _, err := loadBuiltinModels(modelCatalog(t, builtinModelEntry(t), other), modelThumbs("body_test@1", "body_test@2")); err == nil || !strings.Contains(err.Error(), "共用文件") {
			t.Fatal(err)
		}
	})
	t.Run("panics at load", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatal("no panic")
			}
		}()
		mustLoadBuiltinModels([]byte(`{"models":[{"id":"x"}]}`), fstest.MapFS{})
	})
}

func builtinModelFile(body []byte, model string) BuiltinModelFile {
	sum := sha256.Sum256(body)
	return BuiltinModelFile{Name: hex.EncodeToString(sum[:]) + ".glb", Bytes: int64(len(body)), Model: model}
}

func TestImportBuiltinModels(t *testing.T) {
	glb, err := os.ReadFile(filepath.Join("..", "creativeglb", "testdata", "skinned-turned-root.glb"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := creativeglb.Inspect(glb, creativeglb.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	store, err := immutablefs.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	b := &BuiltinModels{store: store}
	notGLB := []byte("not a glb")
	good, bad, gone := builtinModelFile(glb, "good@1"), builtinModelFile(notGLB, "bad@1"), builtinModelFile([]byte("gone"), "gone@1")
	short := builtinModelFile([]byte("short"), "short@1")
	files := []BuiltinModelFile{good, bad, gone, short}
	wrongManifest := manifest
	wrongManifest.Triangles++
	list := []builtinModelSpec{
		{ID: "good@1", GLB: good, Manifest: wrongManifest},
		{ID: "bad@1", GLB: bad, Manifest: manifest},
		{ID: "short@1", GLB: short, Manifest: manifest},
	}
	dir := map[string][]byte{good.Name: glb, bad.Name: notGLB, short.Name: []byte("shor")}
	reads := 0
	read := func(_ context.Context, f BuiltinModelFile) ([]byte, error) {
		reads++
		body, ok := dir[f.Name]
		if !ok {
			return nil, errors.New("open " + f.Name + ": no such file")
		}
		return body, nil
	}

	if missing, err := b.missing(t.Context(), files); err != nil || len(missing) != 4 {
		t.Fatalf("missing %v %v", missing, err)
	}
	// The manifest the catalog says is not what the file gives: nothing is
	// written, nor is a file that is no GLB, too short or not there.
	r := b.importFiles(t.Context(), list, files, read)
	if r.Imported != 0 || r.Skipped != 0 || len(r.Failed) != 4 {
		t.Fatalf("first import %+v", r)
	}
	reasons := map[string]string{}
	for _, f := range r.Failed {
		reasons[f.File] = f.Reason
	}
	if !strings.Contains(reasons[good.Name], "manifest") || !strings.Contains(reasons[bad.Name], "GLB") ||
		!strings.Contains(reasons[short.Name], "大小") || !strings.Contains(reasons[gone.Name], "no such file") {
		t.Fatalf("reasons %v", reasons)
	}
	if r.Failed[0].Model != "good@1" {
		t.Fatalf("failure names no model: %+v", r.Failed[0])
	}
	if missing, _ := b.missing(t.Context(), files); len(missing) != 4 {
		t.Fatalf("a file that did not match was written: %v", missing)
	}

	// A sha256 that is not the name's fails as well.
	renamed := good
	renamed.Name = strings.Repeat("b", 64) + ".glb"
	dir[renamed.Name] = glb
	if r := b.importFiles(t.Context(), list, []BuiltinModelFile{renamed}, read); len(r.Failed) != 1 || !strings.Contains(r.Failed[0].Reason, "sha256") {
		t.Fatalf("renamed %+v", r)
	}

	list[0].Manifest = manifest
	r = b.importFiles(t.Context(), list, files, read)
	if r.Imported != 1 || len(r.Failed) != 3 {
		t.Fatalf("second import %+v", r)
	}
	meta, err := store.Stat(t.Context(), builtinModelKey(good.Name))
	if err != nil || meta.MediaType != "model/gltf-binary" {
		t.Fatalf("stored %+v %v", meta, err)
	}
	rc, size, err := b.open(t.Context(), files, good.Name)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(rc)
	_ = rc.Close()
	if string(body) != string(glb) || size != int64(len(glb)) {
		t.Fatalf("read %d bytes, size %d", len(body), size)
	}

	// Again: what is there is skipped without reading it.
	reads = 0
	r = b.importFiles(t.Context(), list, []BuiltinModelFile{good}, read)
	if r.Skipped != 1 || reads != 0 {
		t.Fatalf("third import %+v after %d reads", r, reads)
	}

	// A name the catalog does not list is unknown; one not imported is missing.
	if _, _, err := b.open(t.Context(), files, renamed.Name); !errors.Is(err, ErrBuiltinModelUnknown) {
		t.Fatalf("unlisted: %v", err)
	}
	if _, _, err := b.open(t.Context(), files, gone.Name); !errors.Is(err, ErrBuiltinModelMissing) {
		t.Fatalf("not imported: %v", err)
	}
}
