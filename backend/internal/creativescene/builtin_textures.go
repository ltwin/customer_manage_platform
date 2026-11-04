package creativescene

import (
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
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
