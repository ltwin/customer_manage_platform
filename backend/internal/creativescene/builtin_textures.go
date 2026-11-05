package creativescene

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"path"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/samson/customer-manage-platform/backend/internal/platform/config"
	"github.com/samson/customer-manage-platform/backend/internal/platform/immutablefs"
)

// Built-in textured materials (data: 内置贴图库): CC0 PBR textures the
// catalog lists as kind textured. Their files are not account content: they
// sit under scene-textures/ in the object store, imported from the catalog's
// list, and are read through GET /creative/scene-textures/{file}.

// builtinFileName is a built-in texture file's name: its content's sha256.
var builtinFileName = regexp.MustCompile(`^[0-9a-f]{64}\.jpg$`)

const maxBuiltinFileBytes = 32 << 20

var (
	builtinSites  = []string{"polyhaven", "ambientcg"}
	builtinMapIDs = []string{"base_color", "normal", "roughness", "metalness", "ao"}
)

// builtinMap is one texture of a textured entry, as a slot's maps has it
// but naming a file of the entry.
type builtinMap struct {
	File    string `json:"file"`
	Channel string `json:"channel,omitempty"`
}

// BuiltinTextureFile is a file of the built-in textures: where the import
// fetches it, and how large it is. Material is the first entry naming it.
type BuiltinTextureFile struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	Bytes    int64  `json:"bytes"`
	Material string `json:"-"`
}

type builtinSource struct {
	Site    string `json:"site"`
	Asset   string `json:"asset"`
	Author  string `json:"author"`
	License string `json:"license"`
}

type materialCategory struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// hasTexturedFields says whether an entry sets any field only a textured
// entry may have.
func (m materialSpec) hasTexturedFields() bool {
	return m.Category != "" || m.SizeM != 0 || m.Maps != nil || m.Defaults != nil || m.Files != nil || m.Source != nil
}

// completeTextured checks a textured entry and gives it its parameter
// table: principled's, with the entry's defaults.
func completeTextured(m materialSpec, principled materialSpec, categories []materialCategory) (materialSpec, error) {
	if m.Params != nil {
		return m, errors.New("textured 条目不写 params，参数表取原理化")
	}
	if !slices.ContainsFunc(categories, func(c materialCategory) bool { return c.ID == m.Category }) {
		return m, fmt.Errorf("未知品类 %q", m.Category)
	}
	if !in(m.SizeM, 0.01, 100) {
		return m, errors.New("size_m 须在 0.01…100 米之间")
	}
	if len(m.Files) == 0 {
		return m, errors.New("没有 files")
	}
	listed := map[string]bool{}
	for _, f := range m.Files {
		if !builtinFileName.MatchString(f.Name) || listed[f.Name] {
			return m, fmt.Errorf("文件名 %q 须为内容的 sha256 且不重复", f.Name)
		}
		if !strings.HasPrefix(f.URL, "https://") || f.Bytes < 1 || f.Bytes > maxBuiltinFileBytes {
			return m, fmt.Errorf("文件 %s 的地址或大小不对", f.Name)
		}
		listed[f.Name] = false
	}
	if _, ok := m.Maps["base_color"]; !ok {
		return m, errors.New("maps 须有 base_color")
	}
	for _, kind := range slices.Sorted(maps.Keys(m.Maps)) {
		t := m.Maps[kind]
		switch {
		case !slices.Contains(builtinMapIDs, kind):
			return m, fmt.Errorf("未知贴图 %q", kind)
		case t.Channel != "" && !channelled(kind):
			return m, fmt.Errorf("%s 不能选通道", kind)
		case t.Channel != "" && !slices.Contains([]string{"r", "g", "b", "a"}, t.Channel):
			return m, fmt.Errorf("%s 的通道须为 r、g、b 或 a", kind)
		}
		if _, ok := listed[t.File]; !ok {
			return m, fmt.Errorf("%s 引用的文件 %q 不在 files 里", kind, t.File)
		}
		listed[t.File] = true
	}
	for name, used := range listed {
		if !used {
			return m, fmt.Errorf("文件 %s 没有被引用", name)
		}
	}
	if s := m.Source; s == nil || !slices.Contains(builtinSites, s.Site) || s.Asset == "" || s.Author == "" || s.License != "CC0" {
		return m, errors.New("source 须写明站点、资产、作者，许可为 CC0")
	}
	m.Params = maps.Clone(principled.Params)
	for _, name := range slices.Sorted(maps.Keys(m.Defaults)) {
		p, ok := m.Params[name]
		if !ok {
			return m, fmt.Errorf("defaults 的 %s 不是原理化的参数", name)
		}
		v, err := p.value(m.Defaults[name])
		if err != nil {
			return m, fmt.Errorf("defaults 的 %s %w", name, err)
		}
		p.Default = v
		m.Params[name] = p
	}
	return m, nil
}

// texturedMaterial is the catalog's textured entry with this ID, if any.
func texturedMaterial(id string) (materialSpec, bool) {
	m, ok := materials.byID[id]
	return m, ok && m.Kind == MaterialTextured
}

// BuiltinTextureFiles lists every built-in texture file once, in catalog
// order.
func BuiltinTextureFiles() []BuiltinTextureFile {
	return slices.Clone(materials.files)
}

var (
	// ErrBuiltinTextureUnknown is a file name the catalog does not list.
	ErrBuiltinTextureUnknown = errors.New("not a built-in texture")
	// ErrBuiltinTextureMissing is a listed file the object store does not have: not imported.
	ErrBuiltinTextureMissing = errors.New("built-in texture not imported")
)

const (
	builtinTexturePrefix = "scene-textures/"
	importWorkers        = 4
)

func builtinTextureKey(name string) string { return builtinTexturePrefix + name }

// BuiltinTextures reads and imports the built-in texture files in the
// object store.
type BuiltinTextures struct {
	store immutablefs.ObjectStore
}

func NewBuiltinTextures(store immutablefs.ObjectStore) *BuiltinTextures {
	return &BuiltinTextures{store: store}
}

// ComposeBuiltinTextures opens the store the files live in, by the same
// driver as the other object stores: the shared bucket, or its own local
// root.
func ComposeBuiltinTextures(cfg config.Config) (*BuiltinTextures, error) {
	switch cfg.AvatarStorageDriver {
	case config.StorageDriverOSS:
		store, err := immutablefs.NewOSS(immutablefs.OSSConfig{Region: cfg.OSSRegion, Endpoint: cfg.OSSEndpoint, Bucket: cfg.OSSBucket, UseCName: cfg.OSSUseCName})
		if err != nil {
			return nil, err
		}
		return NewBuiltinTextures(store), nil
	case config.StorageDriverLocal:
		store, err := immutablefs.NewLocal(cfg.SceneTextureLocalRoot)
		if err != nil {
			return nil, err
		}
		return NewBuiltinTextures(store), nil
	}
	return nil, config.ErrAvatarStorageDriverInvalid
}

// Open reads a built-in texture file with its size: ErrBuiltinTextureUnknown
// when the catalog does not list the name, ErrBuiltinTextureMissing when it
// was not imported.
func (b *BuiltinTextures) Open(ctx context.Context, name string) (io.ReadCloser, int64, error) {
	return b.open(ctx, materials.files, name)
}

func (b *BuiltinTextures) open(ctx context.Context, files []BuiltinTextureFile, name string) (io.ReadCloser, int64, error) {
	if !slices.ContainsFunc(files, func(f BuiltinTextureFile) bool { return f.Name == name }) {
		return nil, 0, ErrBuiltinTextureUnknown
	}
	meta, err := b.store.Stat(ctx, builtinTextureKey(name))
	if errors.Is(err, immutablefs.ErrNotFound) {
		return nil, 0, ErrBuiltinTextureMissing
	}
	if err != nil {
		return nil, 0, err
	}
	rc, err := b.store.Open(ctx, builtinTextureKey(name))
	if errors.Is(err, immutablefs.ErrNotFound) {
		return nil, 0, ErrBuiltinTextureMissing
	}
	return rc, meta.Size, err
}

// Missing lists the catalog's files the store does not have.
func (b *BuiltinTextures) Missing(ctx context.Context) ([]BuiltinTextureFile, error) {
	return b.missing(ctx, materials.files)
}

func (b *BuiltinTextures) missing(ctx context.Context, files []BuiltinTextureFile) ([]BuiltinTextureFile, error) {
	var out []BuiltinTextureFile
	for _, f := range files {
		_, err := b.store.Stat(ctx, builtinTextureKey(f.Name))
		switch {
		case errors.Is(err, immutablefs.ErrNotFound):
			out = append(out, f)
		case err != nil:
			return nil, err
		}
	}
	return out, nil
}

// FetchBuiltinTexture downloads a file from its source.
type FetchBuiltinTexture func(context.Context, BuiltinTextureFile) ([]byte, error)

// BuiltinImport is what an import did: files written, files already there,
// and the ones that failed, with why.
type BuiltinImport struct {
	Imported int                    `json:"imported"`
	Skipped  int                    `json:"skipped"`
	Failed   []BuiltinImportFailure `json:"failed"`
}

type BuiltinImportFailure struct {
	File     string `json:"file"`
	Material string `json:"material"`
	Reason   string `json:"reason"`
}

// Import writes every catalog file the store lacks, fetched and checked
// against its size and sha256 first: a file that does not match is not
// written. Files already there are not fetched, so running it again does
// nothing more; one file failing leaves the rest to go on.
func (b *BuiltinTextures) Import(ctx context.Context, fetch FetchBuiltinTexture) BuiltinImport {
	return b.importFiles(ctx, materials.files, fetch)
}

func (b *BuiltinTextures) importFiles(ctx context.Context, files []BuiltinTextureFile, fetch FetchBuiltinTexture) BuiltinImport {
	// Fetched a few at a time: the source sites are slow per connection.
	outcomes := make([]string, len(files)) // "imported", "skipped" or why it failed
	next := make(chan int)
	var wg sync.WaitGroup
	for range importWorkers {
		wg.Go(func() {
			for i := range next {
				outcomes[i] = b.importFile(ctx, files[i], fetch)
			}
		})
	}
	for i := range files {
		next <- i
	}
	close(next)
	wg.Wait()

	r := BuiltinImport{Failed: []BuiltinImportFailure{}}
	for i, f := range files {
		switch outcomes[i] {
		case "imported":
			r.Imported++
		case "skipped":
			r.Skipped++
		default:
			r.Failed = append(r.Failed, BuiltinImportFailure{File: f.Name, Material: f.Material, Reason: outcomes[i]})
		}
	}
	return r
}

// importFile writes one file unless it is there: "imported", "skipped", or
// why it failed.
func (b *BuiltinTextures) importFile(ctx context.Context, f BuiltinTextureFile, fetch FetchBuiltinTexture) string {
	key := builtinTextureKey(f.Name)
	if _, err := b.store.Stat(ctx, key); err == nil {
		return "skipped"
	} else if !errors.Is(err, immutablefs.ErrNotFound) {
		return err.Error()
	}
	body, err := fetch(ctx, f)
	if err != nil {
		return err.Error()
	}
	if int64(len(body)) != f.Bytes {
		return fmt.Sprintf("大小不符：下载到 %d 字节，清单为 %d", len(body), f.Bytes)
	}
	sum := sha256.Sum256(body)
	if hex.EncodeToString(sum[:])+".jpg" != f.Name {
		return "sha256 与文件名不符"
	}
	_, created, err := b.store.PutImmutable(ctx, key, body, immutablefs.Metadata{MediaType: "image/jpeg", Size: f.Bytes, Checksum: "sha256-" + hex.EncodeToString(sum[:])})
	switch {
	case err != nil:
		return err.Error()
	case created:
		return "imported"
	}
	return "skipped" // written meanwhile by another import
}

// builtinThumbnailFS holds each textured entry's 256 × 256 WebP material
// ball, rendered offline (frontend/scripts/render-texture-thumbnails.mjs) and
// named by its ID with @ as _.
//
//go:embed builtin_thumbnails/*.webp
var builtinThumbnailFS embed.FS

var builtinThumbnails = loadThumbnails(builtinThumbnailFS, materials.list)

// loadThumbnails panics when a textured entry has no thumbnail or a
// thumbnail no entry: both are build mistakes, like a bad catalog entry.
func loadThumbnails(fsys fs.FS, list []materialSpec) map[string][]byte {
	out := map[string][]byte{}
	byName := map[string]string{}
	for _, m := range list {
		if m.Kind != MaterialTextured {
			continue
		}
		name := strings.ReplaceAll(m.ID, "@", "_") + ".webp"
		b, err := fs.ReadFile(fsys, "builtin_thumbnails/"+name)
		if err != nil {
			panic(fmt.Sprintf("creativescene: material %q has no thumbnail: %v", m.ID, err))
		}
		out[m.ID], byName[name] = b, m.ID
	}
	names, err := fs.Glob(fsys, "builtin_thumbnails/*.webp")
	if err != nil {
		panic(err)
	}
	for _, n := range names {
		if _, ok := byName[path.Base(n)]; !ok {
			panic(fmt.Sprintf("creativescene: thumbnail %s belongs to no material", n))
		}
	}
	return out
}

// BuiltinMaterial is a textured entry as the assistant's search lists it:
// Slot goes into material_slots as it is.
type BuiltinMaterial struct {
	ID            string       `json:"builtin_id"`
	Title         string       `json:"title"`
	Category      string       `json:"category"`
	CategoryLabel string       `json:"category_label"`
	SizeM         float64      `json:"size_m"`
	Slot          MaterialSlot `json:"slot"`
}

// BuiltinMaterialFacts is a textured entry as read_asset gives it: what the
// search lists, its parameters' defaults and where its textures come from.
type BuiltinMaterialFacts struct {
	BuiltinMaterial
	Defaults map[string]any `json:"defaults"`
	Source   builtinSource  `json:"source"`
}

func builtinMaterial(m materialSpec) BuiltinMaterial {
	label := ""
	if i := slices.IndexFunc(materials.categories, func(c materialCategory) bool { return c.ID == m.Category }); i >= 0 {
		label = materials.categories[i].Label
	}
	return BuiltinMaterial{ID: m.ID, Title: m.Label, Category: m.Category, CategoryLabel: label, SizeM: m.SizeM, Slot: MaterialSlot{Preset: m.ID, Params: map[string]any{}}}
}

// BuiltinMaterials lists the textured entries matching query, in catalog
// order: each of its words, case aside, is part of the label, the category
// (ID or label) or the source asset's name. An empty query lists them all.
func BuiltinMaterials(query string) []BuiltinMaterial {
	words := strings.Fields(strings.ToLower(query))
	out := []BuiltinMaterial{}
	for _, m := range materials.list {
		if m.Kind != MaterialTextured {
			continue
		}
		b := builtinMaterial(m)
		text := strings.ToLower(strings.Join([]string{b.Title, b.Category, b.CategoryLabel, m.Source.Asset}, "\n"))
		if !slices.ContainsFunc(words, func(w string) bool { return !strings.Contains(text, w) }) {
			out = append(out, b)
		}
	}
	return out
}

// BuiltinMaterialByID is the textured entry with this ID, if any.
func BuiltinMaterialByID(id string) (BuiltinMaterialFacts, bool) {
	m, ok := texturedMaterial(id)
	if !ok {
		return BuiltinMaterialFacts{}, false
	}
	defaults := make(map[string]any, len(m.Params))
	for name, p := range m.Params {
		defaults[name] = p.Default
	}
	return BuiltinMaterialFacts{BuiltinMaterial: builtinMaterial(m), Defaults: defaults, Source: *m.Source}, true
}

// BuiltinMaterialIDs lists the textured entries' IDs in catalog order.
func BuiltinMaterialIDs() []string {
	var ids []string
	for _, m := range materials.list {
		if m.Kind == MaterialTextured {
			ids = append(ids, m.ID)
		}
	}
	return ids
}

// BuiltinThumbnail is a textured entry's WebP thumbnail.
func BuiltinThumbnail(id string) ([]byte, bool) {
	b, ok := builtinThumbnails[id]
	return b, ok
}
