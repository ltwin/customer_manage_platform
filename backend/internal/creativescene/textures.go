package creativescene

import (
	"fmt"
	"maps"
	"slices"

	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
)

// Texture fields of a material slot (data: 场景文档「材质贴图」).
const (
	principledPreset = "principled@1"

	mappingTriplanar = "triplanar"
	mappingUV        = "uv"

	alphaOpaque = "opaque"
	alphaClip   = "clip"
	alphaBlend  = "blend"

	defaultAlphaCutoff = 0.5
	defaultChannel     = "r"
)

// textureMap is one written texture with its kind, in kind order.
type textureMap struct {
	kind string
	tex  *TextureMap
}

// entries lists the written textures in contract order.
func (m *TextureMaps) entries() []textureMap {
	if m == nil {
		return nil
	}
	var out []textureMap
	for _, e := range []textureMap{{"base_color", m.BaseColor}, {"normal", m.Normal}, {"roughness", m.Roughness}, {"metalness", m.Metalness}, {"ao", m.AO}, {"emissive", m.Emissive}} {
		if e.tex != nil {
			out = append(out, e)
		}
	}
	return out
}

// channelled kinds read one channel of their image; colour kinds read RGB.
func channelled(kind string) bool {
	return slices.Contains([]string{"roughness", "metalness", "ao"}, kind)
}

// checkTextures validates a slot's texture fields and says which one is
// wrong, as a path inside the slot.
func checkTextures(slot MaterialSlot) (string, error) {
	_, textured := texturedMaterial(slot.Preset)
	switch {
	case textured && slot.Maps != nil:
		return "maps", fmt.Errorf("%s 是内置贴图材质，自带贴图，不能写 maps；要用自己的图片，改用原理化材质 %s", slot.Preset, principledPreset)
	case !textured && slot.Preset != principledPreset:
		for _, f := range []struct {
			name string
			set  bool
		}{{"maps", slot.Maps != nil}, {"mapping", slot.Mapping != nil}, {"alpha_mode", slot.AlphaMode != ""}, {"alpha_cutoff", slot.AlphaCutoff != nil}} {
			if f.set {
				return f.name, fmt.Errorf("%s 只用于原理化材质 %s 和内置贴图材质，%s 不接贴图和透明方式", f.name, principledPreset, slot.Preset)
			}
		}
		return "", nil
	}
	if slot.Maps != nil {
		entries := slot.Maps.entries()
		if len(entries) == 0 {
			return "maps", fmt.Errorf("maps 至少写一种贴图：base_color、normal、roughness、metalness、ao、emissive")
		}
		for _, e := range entries {
			if !revisionIDPattern.MatchString(e.tex.ContentRevisionID) {
				return "maps." + e.kind + ".content_revision_id", fmt.Errorf("maps.%s 须为图片的 content_revision_id", e.kind)
			}
			switch {
			case e.tex.Channel == "":
			case !channelled(e.kind):
				return "maps." + e.kind + ".channel", fmt.Errorf("只有 roughness、metalness、ao 可以选通道，%s 不能", e.kind)
			case !slices.Contains([]string{"r", "g", "b", "a"}, e.tex.Channel):
				return "maps." + e.kind + ".channel", fmt.Errorf("maps.%s.channel 须为 r、g、b 或 a", e.kind)
			}
		}
	}
	if m := slot.Mapping; m != nil {
		if slot.Maps == nil && !textured {
			return "mapping", fmt.Errorf("mapping 只在有 maps 时允许")
		}
		kind := m.Kind
		switch kind {
		case "":
			kind = mappingTriplanar
		case mappingTriplanar, mappingUV:
		default:
			return "mapping.kind", fmt.Errorf("mapping.kind 须为 triplanar 或 uv")
		}
		if m.SizeM != nil {
			if kind != mappingTriplanar {
				return "mapping.size_m", fmt.Errorf("size_m 只用于 triplanar，uv 映射用 repeat")
			}
			if !in(*m.SizeM, 0.01, 100) {
				return "mapping.size_m", fmt.Errorf("size_m 须在 0.01…100 米之间")
			}
		}
		if m.Repeat != nil {
			if kind != mappingUV {
				return "mapping.repeat", fmt.Errorf("repeat 只用于 uv，triplanar 映射用 size_m")
			}
			if len(m.Repeat) != 2 || !in(m.Repeat[0], 0.01, 1000) || !in(m.Repeat[1], 0.01, 1000) {
				return "mapping.repeat", fmt.Errorf("repeat 须为 [u, v]，各在 0.01…1000 之间")
			}
		}
		if m.RotationDeg != nil && !in(*m.RotationDeg, 0, 360) {
			return "mapping.rotation_deg", fmt.Errorf("rotation_deg 须在 0…360 之间")
		}
		if m.NormalStrength != nil && !in(*m.NormalStrength, 0, 4) {
			return "mapping.normal_strength", fmt.Errorf("normal_strength 须在 0…4 之间")
		}
	}
	switch slot.AlphaMode {
	case "", alphaOpaque, alphaClip, alphaBlend:
	default:
		return "alpha_mode", fmt.Errorf("alpha_mode 须为 opaque、clip 或 blend")
	}
	if slot.AlphaCutoff != nil {
		if slot.AlphaMode != alphaClip {
			return "alpha_cutoff", fmt.Errorf("alpha_cutoff 只在 alpha_mode 为 clip 时允许")
		}
		if !in(*slot.AlphaCutoff, 0, 1) {
			return "alpha_cutoff", fmt.Errorf("alpha_cutoff 须在 0…1 之间")
		}
	}
	return "", nil
}

// completeTextures fills what a written slot leaves out. A slot without
// maps keeps its alpha_mode as written, so a scene without textures stores
// what it always did. A built-in textured material is completed as a slot
// with maps, its triplanar size the entry's.
func completeTextures(slot *MaterialSlot) {
	if slot.AlphaMode == alphaClip && slot.AlphaCutoff == nil {
		slot.AlphaCutoff = floatRef(defaultAlphaCutoff)
	}
	size := 1.0
	if m, ok := texturedMaterial(slot.Preset); ok {
		size = m.SizeM
	} else if slot.Maps == nil {
		return
	}
	for _, e := range slot.Maps.entries() {
		if channelled(e.kind) && e.tex.Channel == "" {
			e.tex.Channel = defaultChannel
		}
	}
	if slot.Mapping == nil {
		slot.Mapping = &TextureMapping{}
	}
	m := slot.Mapping
	if m.Kind == "" {
		m.Kind = mappingTriplanar
	}
	switch {
	case m.Kind == mappingTriplanar && m.SizeM == nil:
		m.SizeM = floatRef(size)
	case m.Kind == mappingUV && m.Repeat == nil:
		m.Repeat = []float64{1, 1}
	}
	if m.RotationDeg == nil {
		m.RotationDeg = floatRef(0)
	}
	if m.NormalStrength == nil {
		m.NormalStrength = floatRef(1)
	}
	if slot.AlphaMode == "" {
		slot.AlphaMode = alphaOpaque
	}
}

// writeSlots checks and completes the slots an operation writes; path is
// where they sit in the operation, such as objects[0].material_slots.
func writeSlots(slots map[string]MaterialSlot, path string) error {
	for _, name := range slices.Sorted(maps.Keys(slots)) {
		slot := slots[name]
		if field, err := checkTextures(slot); err != nil {
			return &creativeops.FieldError{Err: creativeops.ErrValidation, Path: path + "." + name + "." + field, Message: fmt.Sprintf("材质槽 %s：%v", name, err)}
		}
		completeTextures(&slot)
		slots[name] = slot
	}
	return nil
}

func floatRef(v float64) *float64 { return &v }
