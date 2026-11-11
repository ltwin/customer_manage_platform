package creativescene

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/samson/customer-manage-platform/backend/internal/creativeglb"
	"github.com/samson/customer-manage-platform/backend/internal/platform/config"
	"github.com/samson/customer-manage-platform/backend/internal/platform/immutablefs"
)

// Built-in models (data: 内置模型库): models every account may place in a
// scene and none may change, such as the two base bodies. The catalog ships
// with the binary; the GLBs sit under scene-models/ in the object store,
// imported from a directory, and are read through GET
// /creative/scene-models/{file}.

//go:embed models.json
var modelsJSON []byte

// builtinModelFileName is a built-in model's GLB name: its content's sha256.
var builtinModelFileName = regexp.MustCompile(`^[0-9a-f]{64}\.glb$`)

const (
	builtinModelPrefix  = "scene-models/"
	builtinModelLicense = "CC0"
)

var builtinModelCategories = []string{"humanoid"}

// BuiltinModelFile is a built-in model's GLB: its name and size. Model is the
// first entry naming it.
type BuiltinModelFile struct {
	Name  string `json:"name"`
	Bytes int64  `json:"bytes"`
	Model string `json:"-"`
}

type builtinModelSource struct {
	Site    string `json:"site"`
	Asset   string `json:"asset"`
	Author  string `json:"author"`
	License string `json:"license"`
	URL     string `json:"url"`
}

// builtinModelSpec is a catalog entry: once published, its file, manifest,
// rig and materials never change; a change is a new version.
type builtinModelSpec struct {
	ID       string               `json:"id"`
	Label    string               `json:"label"`
	Category string               `json:"category"`
	Mount    string               `json:"mount"`
	GLB      BuiltinModelFile     `json:"glb"`
	Manifest creativeglb.Manifest `json:"manifest"`
	Rig      *Rig                 `json:"rig,omitempty"`
	// Materials are what placing the model writes into the instance's
	// material_slots.
	Materials map[string]MaterialSlot `json:"materials"`
	Source    builtinModelSource      `json:"source"`
}

type builtinModelCatalog struct {
	list       []builtinModelSpec
	byID       map[string]builtinModelSpec
	thumbnails map[string][]byte
	// files are the entries' GLBs, each once.
	files []BuiltinModelFile
}

var builtinModels = mustLoadBuiltinModels(modelsJSON, builtinThumbnailFS)

// mustLoadBuiltinModels panics on a malformed catalog: it ships with the
// binary, so a bad entry is a build mistake rather than bad input.
func mustLoadBuiltinModels(raw []byte, thumbs fs.FS) builtinModelCatalog {
	c, err := loadBuiltinModels(raw, thumbs)
	if err != nil {
		panic(fmt.Sprintf("creativescene: models.json: %v", err))
	}
	return c
}

func loadBuiltinModels(raw []byte, thumbs fs.FS) (builtinModelCatalog, error) {
	var file struct {
		Models []builtinModelSpec `json:"models"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	dec.UseNumber()
	if err := dec.Decode(&file); err != nil {
		return builtinModelCatalog{}, err
	}
	c := builtinModelCatalog{list: file.Models, byID: map[string]builtinModelSpec{}, thumbnails: map[string][]byte{}}
	byFile := map[string]builtinModelSpec{}
	for _, m := range file.Models {
		if _, dup := c.byID[m.ID]; dup {
			return c, fmt.Errorf("模型 %q 重复", m.ID)
		}
		if err := checkBuiltinModel(m); err != nil {
			return c, fmt.Errorf("模型 %q：%w", m.ID, err)
		}
		c.byID[m.ID] = m
		// Entries sharing a file must say the same of it.
		if first, ok := byFile[m.GLB.Name]; ok {
			if m.GLB.Bytes != first.GLB.Bytes || !m.Manifest.Matches(first.Manifest) {
				return c, fmt.Errorf("模型 %q 与 %q 共用文件，大小或 manifest 不同", m.ID, first.ID)
			}
			continue
		}
		byFile[m.GLB.Name] = m
		f := m.GLB
		f.Model = m.ID
		c.files = append(c.files, f)
	}
	byName := map[string]bool{}
	for _, m := range file.Models {
		name := strings.ReplaceAll(m.ID, "@", "_") + ".webp"
		b, err := fs.ReadFile(thumbs, "builtin_thumbnails/models/"+name)
		if err != nil {
			return c, fmt.Errorf("模型 %q 没有缩略图：%w", m.ID, err)
		}
		c.thumbnails[m.ID], byName[name] = b, true
	}
	names, err := fs.Glob(thumbs, "builtin_thumbnails/models/*.webp")
	if err != nil {
		return c, err
	}
	for _, n := range names {
		if !byName[path.Base(n)] {
			return c, fmt.Errorf("缩略图 %s 不属于任何模型", n)
		}
	}
	return c, nil
}

func checkBuiltinModel(m builtinModelSpec) error {
	switch {
	case !versionedID.MatchString(m.ID):
		return errors.New("id 须为 名称@版本")
	case m.Label == "":
		return errors.New("没有 label")
	case !slices.Contains(builtinModelCategories, m.Category):
		return fmt.Errorf("未知品类 %q", m.Category)
	case !slices.Contains(mounts, m.Mount):
		return fmt.Errorf("未知 mount %q", m.Mount)
	case !builtinModelFileName.MatchString(m.GLB.Name):
		return fmt.Errorf("文件名 %q 须为内容的 sha256", m.GLB.Name)
	case m.GLB.Bytes < 1 || m.GLB.Bytes > int64(creativeglb.DefaultLimits().MaxBytes):
		return fmt.Errorf("文件大小 %d 超出范围", m.GLB.Bytes)
	case len(m.Materials) == 0:
		return errors.New("没有 materials")
	}
	for _, name := range slices.Sorted(maps.Keys(m.Materials)) {
		if !slices.ContainsFunc(m.Manifest.Slots, func(s creativeglb.Slot) bool { return s.ID == name }) {
			return fmt.Errorf("materials 的 %s 不是 manifest 的槽位", name)
		}
		if err := checkMaterial(m.Materials[name]); err != nil {
			return fmt.Errorf("materials 的 %s：%w", name, err)
		}
	}
	if m.Category == "humanoid" && (m.Rig == nil || len(m.Rig.Humanoid) == 0) {
		return errors.New("人形模型须有 rig.humanoid")
	}
	if m.Rig != nil {
		if err := m.Rig.Validate(m.Manifest.Skeleton); err != nil {
			return err
		}
	}
	if s := m.Source; s.Site == "" || s.Asset == "" || s.Author == "" || s.License != builtinModelLicense || !strings.HasPrefix(s.URL, "https://") {
		return errors.New("source 须写明站点、资产、作者和 https 地址，许可为 CC0")
	}
	return nil
}

var (
	// ErrBuiltinModelUnknown is a file name the catalog does not list.
	ErrBuiltinModelUnknown = errors.New("not a built-in model")
	// ErrBuiltinModelMissing is a listed file the object store does not have: not imported.
	ErrBuiltinModelMissing = errors.New("built-in model not imported")
)

func builtinModelKey(name string) string { return builtinModelPrefix + name }

// BuiltinModels reads and imports the built-in models' files in the object
// store.
type BuiltinModels struct {
	store immutablefs.ObjectStore
}

func NewBuiltinModels(store immutablefs.ObjectStore) *BuiltinModels {
	return &BuiltinModels{store: store}
}

// ComposeBuiltinModels opens the store the files live in, as
// ComposeBuiltinTextures does: the shared bucket, or its own local root.
func ComposeBuiltinModels(cfg config.Config) (*BuiltinModels, error) {
	store, err := composeBuiltinStore(cfg, cfg.SceneModelLocalRoot)
	if err != nil {
		return nil, err
	}
	return NewBuiltinModels(store), nil
}

// BuiltinModelFiles lists every built-in model file once, in catalog order.
func BuiltinModelFiles() []BuiltinModelFile {
	return slices.Clone(builtinModels.files)
}

// Open reads a built-in model file with its size: ErrBuiltinModelUnknown
// when the catalog does not list the name, ErrBuiltinModelMissing when it
// was not imported.
func (b *BuiltinModels) Open(ctx context.Context, name string) (io.ReadCloser, int64, error) {
	return b.open(ctx, builtinModels.files, name)
}

func (b *BuiltinModels) open(ctx context.Context, files []BuiltinModelFile, name string) (io.ReadCloser, int64, error) {
	if !slices.ContainsFunc(files, func(f BuiltinModelFile) bool { return f.Name == name }) {
		return nil, 0, ErrBuiltinModelUnknown
	}
	meta, err := b.store.Stat(ctx, builtinModelKey(name))
	if errors.Is(err, immutablefs.ErrNotFound) {
		return nil, 0, ErrBuiltinModelMissing
	}
	if err != nil {
		return nil, 0, err
	}
	rc, err := b.store.Open(ctx, builtinModelKey(name))
	if errors.Is(err, immutablefs.ErrNotFound) {
		return nil, 0, ErrBuiltinModelMissing
	}
	return rc, meta.Size, err
}

// Missing lists the catalog's files the store does not have.
func (b *BuiltinModels) Missing(ctx context.Context) ([]BuiltinModelFile, error) {
	return b.missing(ctx, builtinModels.files)
}

func (b *BuiltinModels) missing(ctx context.Context, files []BuiltinModelFile) ([]BuiltinModelFile, error) {
	var out []BuiltinModelFile
	for _, f := range files {
		_, err := b.store.Stat(ctx, builtinModelKey(f.Name))
		switch {
		case errors.Is(err, immutablefs.ErrNotFound):
			out = append(out, f)
		case err != nil:
			return nil, err
		}
	}
	return out, nil
}

// ReadBuiltinModel reads a file from wherever the import takes it: the
// sources have no direct links, so it is a directory the conversion script
// wrote.
type ReadBuiltinModel func(context.Context, BuiltinModelFile) ([]byte, error)

// BuiltinModelImport is what an import did, as BuiltinImport is for textures.
type BuiltinModelImport struct {
	Imported int                         `json:"imported"`
	Skipped  int                         `json:"skipped"`
	Failed   []BuiltinModelImportFailure `json:"failed"`
}

type BuiltinModelImportFailure struct {
	File   string `json:"file"`
	Model  string `json:"model"`
	Reason string `json:"reason"`
}

// Import writes every catalog file the store lacks once it matches the
// catalog: its size and sha256, the GLB check every imported model passes,
// and the manifest that check computes. A file that does not match is not
// written; files already there are not read, so running it again does
// nothing more.
func (b *BuiltinModels) Import(ctx context.Context, read ReadBuiltinModel) BuiltinModelImport {
	return b.importFiles(ctx, builtinModels.list, builtinModels.files, read)
}

func (b *BuiltinModels) importFiles(ctx context.Context, list []builtinModelSpec, files []BuiltinModelFile, read ReadBuiltinModel) BuiltinModelImport {
	r := BuiltinModelImport{Failed: []BuiltinModelImportFailure{}}
	for _, f := range files {
		switch outcome := b.importFile(ctx, list, f, read); outcome {
		case "imported":
			r.Imported++
		case "skipped":
			r.Skipped++
		default:
			r.Failed = append(r.Failed, BuiltinModelImportFailure{File: f.Name, Model: f.Model, Reason: outcome})
		}
	}
	return r
}

// importFile writes one file unless it is there: "imported", "skipped", or
// why it failed.
func (b *BuiltinModels) importFile(ctx context.Context, list []builtinModelSpec, f BuiltinModelFile, read ReadBuiltinModel) string {
	key := builtinModelKey(f.Name)
	if _, err := b.store.Stat(ctx, key); err == nil {
		return "skipped"
	} else if !errors.Is(err, immutablefs.ErrNotFound) {
		return err.Error()
	}
	body, err := read(ctx, f)
	if err != nil {
		return err.Error()
	}
	if int64(len(body)) != f.Bytes {
		return fmt.Sprintf("大小不符：读到 %d 字节，清单为 %d", len(body), f.Bytes)
	}
	sum := sha256.Sum256(body)
	if hex.EncodeToString(sum[:])+".glb" != f.Name {
		return "sha256 与文件名不符"
	}
	manifest, err := creativeglb.Inspect(body, creativeglb.DefaultLimits())
	if err != nil {
		return "GLB 校验不通过：" + err.Error()
	}
	for _, m := range list {
		if m.GLB.Name == f.Name && !manifest.Matches(m.Manifest) {
			return fmt.Sprintf("算出的 manifest 与清单里 %s 的不符", m.ID)
		}
	}
	_, created, err := b.store.PutImmutable(ctx, key, body, immutablefs.Metadata{MediaType: "model/gltf-binary", Size: f.Bytes, Checksum: "sha256-" + hex.EncodeToString(sum[:])})
	switch {
	case err != nil:
		return err.Error()
	case created:
		return "imported"
	}
	return "skipped" // written meanwhile by another import
}
