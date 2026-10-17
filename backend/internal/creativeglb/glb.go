// Package creativeglb inspects untrusted GLB files produced by scene geometry
// modules. It accepts only a static subset of glTF 2.0 (data: 模型内容) and
// derives the model manifest from the actual vertex data; nothing the file
// declares about itself is trusted. The editor host runs the same algorithm in
// frontend/src/creative-scene/sandbox/inspect.ts against the shared testdata.
package creativeglb

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"
)

// Reason classifies a rejection. The editor host reports the same reason for
// the same file.
type Reason string

const (
	ReasonContainer          Reason = "container"
	ReasonJSON               Reason = "json"
	ReasonName               Reason = "name"
	ReasonBuffer             Reason = "buffer"
	ReasonSceneGraph         Reason = "scene_graph"
	ReasonAccessor           Reason = "accessor"
	ReasonIndexRange         Reason = "index_range"
	ReasonBoundsMismatch     Reason = "bounds_mismatch"
	ReasonUnsupportedFeature Reason = "unsupported_feature"
	ReasonLimit              Reason = "limit"
)

// Error is a rejected GLB. Detail is English and meant for logs and the
// editor's error area; callers branch on Reason only.
type Error struct {
	Reason Reason
	Detail string
}

func (e *Error) Error() string { return "creativeglb: " + string(e.Reason) + ": " + e.Detail }

func failf(r Reason, format string, args ...any) error {
	return &Error{Reason: r, Detail: fmt.Sprintf(format, args...)}
}

// Limits are the traversal and size budgets checked while inspecting.
type Limits struct {
	MaxBytes              int
	MaxTriangles          int
	MaxNodes              int
	MaxPrimitiveInstances int
	MaxSlots              int
	MaxParts              int
	MaxAnchors            int
}

// DefaultLimits are the contract budgets for model content.
func DefaultLimits() Limits {
	return Limits{
		MaxBytes:              16 << 20,
		MaxTriangles:          300000,
		MaxNodes:              10000,
		MaxPrimitiveInstances: 10000,
		MaxSlots:              32,
		MaxParts:              200,
		MaxAnchors:            200,
	}
}

// Manifest is what the scene, the editor and the assistant know about a model
// without loading its geometry. Arrays are sorted by id.
type Manifest struct {
	Bounds    Bounds   `json:"bounds"`
	Triangles int      `json:"triangles"`
	Slots     []Slot   `json:"slots"`
	Parts     []string `json:"parts"`
	Anchors   []Anchor `json:"anchors"`
}

// Bounds is the world-space axis-aligned box of the whole model.
type Bounds struct {
	Min [3]float64 `json:"min"`
	Max [3]float64 `json:"max"`
}

// Slot is a material slot (`slot:<id>` material) with its optional hint.
type Slot struct {
	ID   string `json:"id"`
	Hint string `json:"hint,omitempty"`
}

// Anchor is an `anchor:<id>` node and its world position.
type Anchor struct {
	ID       string     `json:"id"`
	Position [3]float64 `json:"position"`
}

const (
	glbMagic     = 0x46546C67 // "glTF"
	chunkJSON    = 0x4E4F534A // "JSON"
	chunkBIN     = 0x004E4942 // "BIN\0"
	compFloat    = 5126
	compUByte    = 5121
	compUShort   = 5123
	compUInt     = 5125
	modeTriangle = 4
	maxHintRunes = 64
	// emissiveStrength is the only extension GLTFExporter writes for
	// MeshStandardMaterial (emissiveIntensity != 1).
	emissiveStrength = "KHR_materials_emissive_strength"
)

var (
	namePattern    = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	componentSizes = map[int]int{5120: 1, compUByte: 1, 5122: 2, compUShort: 2, compUInt: 4, compFloat: 4}
	typeComponents = map[string]int{"SCALAR": 1, "VEC2": 2, "VEC3": 3, "VEC4": 4}
	// Top-level and per-object members that are valid glTF but outside the
	// static subset. Each maps to the feature named in the rejection.
	topUnsupported  = map[string]string{"images": "texture", "textures": "texture", "samplers": "texture", "skins": "skin", "animations": "animation", "cameras": "camera"}
	nodeUnsupported = map[string]string{"camera": "camera", "skin": "skin", "weights": "morph target"}
)

// Inspect validates glb against the static subset and computes its manifest.
// Errors are always *Error.
func Inspect(glb []byte, limits Limits) (Manifest, error) {
	if len(glb) > limits.MaxBytes {
		return Manifest{}, failf(ReasonLimit, "file is %d bytes, limit %d", len(glb), limits.MaxBytes)
	}
	jsonChunk, bin, err := splitContainer(glb)
	if err != nil {
		return Manifest{}, err
	}
	doc, err := parseDocument(jsonChunk, bin, limits)
	if err != nil {
		return Manifest{}, err
	}
	return doc.manifest(limits)
}

func splitContainer(glb []byte) (jsonChunk, bin []byte, err error) {
	if len(glb) < 20 {
		return nil, nil, failf(ReasonContainer, "file shorter than a GLB header")
	}
	le := binary.LittleEndian
	if le.Uint32(glb[0:]) != glbMagic {
		return nil, nil, failf(ReasonContainer, "magic is not glTF")
	}
	if v := le.Uint32(glb[4:]); v != 2 {
		return nil, nil, failf(ReasonContainer, "container version %d", v)
	}
	if n := le.Uint32(glb[8:]); uint64(n) != uint64(len(glb)) {
		return nil, nil, failf(ReasonContainer, "declared length %d, actual %d", n, len(glb))
	}
	jsonLen := uint64(le.Uint32(glb[12:]))
	if le.Uint32(glb[16:]) != chunkJSON {
		return nil, nil, failf(ReasonContainer, "first chunk is not JSON")
	}
	jsonEnd := 20 + jsonLen
	if jsonLen == 0 || jsonLen%4 != 0 || jsonEnd > uint64(len(glb)) {
		return nil, nil, failf(ReasonContainer, "JSON chunk length %d", jsonLen)
	}
	jsonChunk = glb[20:jsonEnd]
	if jsonEnd == uint64(len(glb)) {
		return jsonChunk, nil, nil
	}
	if jsonEnd+8 > uint64(len(glb)) {
		return nil, nil, failf(ReasonContainer, "truncated chunk header")
	}
	binLen := uint64(le.Uint32(glb[jsonEnd:]))
	if le.Uint32(glb[jsonEnd+4:]) != chunkBIN {
		return nil, nil, failf(ReasonContainer, "second chunk is not BIN")
	}
	binStart := jsonEnd + 8
	if binLen%4 != 0 || binStart+binLen != uint64(len(glb)) {
		return nil, nil, failf(ReasonContainer, "BIN chunk length %d does not end the file", binLen)
	}
	return jsonChunk, glb[binStart:], nil
}

// object is a JSON object with exact member names. Duplicate names keep the
// last value, exactly like JSON.parse in the browser that renders the file;
// struct decoding would match names case-insensitively and could see a
// different scene than three.js does.
type object map[string]json.RawMessage

func isNull(raw json.RawMessage) bool { return bytes.Equal(bytes.TrimSpace(raw), []byte("null")) }

func asObject(raw json.RawMessage, what string) (object, error) {
	var o object
	if isNull(raw) || json.Unmarshal(raw, &o) != nil || o == nil {
		return nil, failf(ReasonJSON, "%s is not an object", what)
	}
	return o, nil
}

func asObjects(raw json.RawMessage, what string) ([]object, error) {
	var items []json.RawMessage
	if isNull(raw) || json.Unmarshal(raw, &items) != nil || items == nil {
		return nil, failf(ReasonJSON, "%s is not an array", what)
	}
	out := make([]object, len(items))
	for i, item := range items {
		o, err := asObject(item, fmt.Sprintf("%s[%d]", what, i))
		if err != nil {
			return nil, err
		}
		out[i] = o
	}
	return out, nil
}

// checkMembers rejects members outside allowed. `extras` is always allowed;
// `extensions` only with the listed extension names; unsupported maps valid
// glTF members outside the subset to the feature reported.
func (o object) checkMembers(what string, allowed []string, extensions []string, unsupported map[string]string) error {
	names := make([]string, 0, len(o))
	for k := range o {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		switch {
		case k == "extras" || slices.Contains(allowed, k):
		case k == "extensions":
			ext, err := asObject(o[k], what+".extensions")
			if err != nil {
				return err
			}
			extNames := make([]string, 0, len(ext))
			for name := range ext {
				extNames = append(extNames, name)
			}
			sort.Strings(extNames)
			for _, name := range extNames {
				if !slices.Contains(extensions, name) {
					return failf(ReasonUnsupportedFeature, "extension %s on %s", name, what)
				}
			}
		case unsupported[k] != "":
			return failf(ReasonUnsupportedFeature, "%s (%s.%s)", unsupported[k], what, k)
		default:
			return failf(ReasonJSON, "unknown member %s.%s", what, k)
		}
	}
	return nil
}

func (o object) number(k, what string) (float64, bool, error) {
	raw, ok := o[k]
	if !ok {
		return 0, false, nil
	}
	var f float64
	if isNull(raw) || json.Unmarshal(raw, &f) != nil || math.IsInf(f, 0) || math.IsNaN(f) {
		return 0, true, failf(ReasonJSON, "%s.%s is not a finite number", what, k)
	}
	return f, true, nil
}

// integer reads a non-negative integral number (JSON has no integer type; the
// browser accepts 1.0 and 1e3 as integers, so this does too).
func (o object) integer(k, what string) (int, bool, error) {
	f, ok, err := o.number(k, what)
	if err != nil || !ok {
		return 0, ok, err
	}
	if f < 0 || f != math.Trunc(f) || f > math.MaxInt32 {
		return 0, true, failf(ReasonJSON, "%s.%s is not a non-negative integer", what, k)
	}
	return int(f), true, nil
}

// index reads an optional index into an array of length n; -1 when absent.
func (o object) index(k, what string, n int) (int, error) {
	i, ok, err := o.integer(k, what)
	if err != nil {
		return 0, err
	}
	if !ok {
		return -1, nil
	}
	if i >= n {
		return 0, failf(ReasonJSON, "%s.%s = %d out of range (%d)", what, k, i, n)
	}
	return i, nil
}

func (o object) numbers(k, what string, n int) ([]float64, bool, error) {
	raw, ok := o[k]
	if !ok {
		return nil, false, nil
	}
	var items []json.RawMessage
	if isNull(raw) || json.Unmarshal(raw, &items) != nil || len(items) != n {
		return nil, true, failf(ReasonJSON, "%s.%s is not an array of %d numbers", what, k, n)
	}
	out := make([]float64, n)
	for i, item := range items {
		f, _, err := object{"v": item}.number("v", fmt.Sprintf("%s.%s[%d]", what, k, i))
		if err != nil {
			return nil, true, err
		}
		out[i] = f
	}
	return out, true, nil
}

func (o object) indices(k, what string, n int) ([]int, error) {
	raw, ok := o[k]
	if !ok {
		return nil, nil
	}
	var items []json.RawMessage
	if isNull(raw) || json.Unmarshal(raw, &items) != nil || items == nil {
		return nil, failf(ReasonJSON, "%s.%s is not an array", what, k)
	}
	out := make([]int, len(items))
	for i, item := range items {
		v, err := object{"v": item}.index("v", fmt.Sprintf("%s.%s[%d]", what, k, i), n)
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}

func (o object) str(k, what string) (string, bool, error) {
	raw, ok := o[k]
	if !ok {
		return "", false, nil
	}
	var s string
	if isNull(raw) || json.Unmarshal(raw, &s) != nil {
		return "", true, failf(ReasonJSON, "%s.%s is not a string", what, k)
	}
	return s, true, nil
}

func (o object) boolean(k, what string) (bool, error) {
	raw, ok := o[k]
	if !ok {
		return false, nil
	}
	var b bool
	if isNull(raw) || json.Unmarshal(raw, &b) != nil {
		return false, failf(ReasonJSON, "%s.%s is not a boolean", what, k)
	}
	return b, nil
}

func (o object) array(k, what string) ([]object, bool, error) {
	raw, ok := o[k]
	if !ok {
		return nil, false, nil
	}
	items, err := asObjects(raw, what)
	return items, true, err
}

type document struct {
	bin       []byte
	views     []bufferView
	accessors []accessor
	materials []material
	meshes    [][]primitive
	nodes     []node
	roots     []int
}

type bufferView struct {
	offset, length, stride int
}

type accessor struct {
	view, offset, component, count, comps int
	normalized                            bool
	min, max                              []float64
}

type material struct {
	slot, hint string
	isSlot     bool
}

type primitive struct {
	position, indices, material int
}

type node struct {
	name     string
	children []int
	mesh     int
	local    [16]float64
}

func parseDocument(jsonChunk, bin []byte, limits Limits) (*document, error) {
	if bytes.HasPrefix(jsonChunk, []byte("\xEF\xBB\xBF")) {
		return nil, failf(ReasonJSON, "JSON chunk starts with a byte order mark")
	}
	if !utf8.Valid(jsonChunk) {
		return nil, failf(ReasonJSON, "JSON chunk is not UTF-8")
	}
	top, err := asObject(jsonChunk, "gltf")
	if err != nil {
		return nil, err
	}
	allowed := []string{"asset", "scene", "scenes", "nodes", "meshes", "materials", "accessors", "bufferViews", "buffers", "extensionsUsed", "extensionsRequired"}
	if err := top.checkMembers("gltf", allowed, nil, topUnsupported); err != nil {
		return nil, err
	}
	if err := checkAsset(top); err != nil {
		return nil, err
	}
	if err := checkExtensionLists(top); err != nil {
		return nil, err
	}
	d := &document{}
	bufferLen, err := parseBuffers(top, bin)
	if err != nil {
		return nil, err
	}
	d.bin = bin
	if d.views, err = parseViews(top, bufferLen); err != nil {
		return nil, err
	}
	if d.accessors, err = parseAccessors(top, d.views); err != nil {
		return nil, err
	}
	if d.materials, err = parseMaterials(top); err != nil {
		return nil, err
	}
	if d.meshes, err = d.parseMeshes(top); err != nil {
		return nil, err
	}
	if d.nodes, err = parseNodes(top, len(d.meshes), limits); err != nil {
		return nil, err
	}
	if d.roots, err = parseScene(top, len(d.nodes)); err != nil {
		return nil, err
	}
	if err := d.checkGraph(); err != nil {
		return nil, err
	}
	return d, nil
}

func checkAsset(top object) error {
	raw, ok := top["asset"]
	if !ok {
		return failf(ReasonJSON, "missing asset")
	}
	asset, err := asObject(raw, "asset")
	if err != nil {
		return err
	}
	if err := asset.checkMembers("asset", []string{"version", "generator", "copyright", "minVersion"}, nil, nil); err != nil {
		return err
	}
	if v, _, err := asset.str("version", "asset"); err != nil || v != "2.0" {
		return failf(ReasonJSON, "asset.version must be 2.0")
	}
	for _, k := range []string{"generator", "copyright", "minVersion"} {
		if _, _, err := asset.str(k, "asset"); err != nil {
			return err
		}
	}
	return nil
}

func checkExtensionLists(top object) error {
	for _, k := range []string{"extensionsUsed", "extensionsRequired"} {
		raw, ok := top[k]
		if !ok {
			continue
		}
		var names []string
		if isNull(raw) || json.Unmarshal(raw, &names) != nil {
			return failf(ReasonJSON, "%s is not an array of strings", k)
		}
		for _, name := range names {
			if k == "extensionsRequired" || name != emissiveStrength {
				return failf(ReasonUnsupportedFeature, "extension %s (%s)", name, k)
			}
		}
	}
	return nil
}

// parseBuffers returns the usable buffer length, or -1 when there is no
// buffer. The only buffer is the BIN chunk; any uri is rejected.
func parseBuffers(top object, bin []byte) (int, error) {
	buffers, ok, err := top.array("buffers", "buffers")
	if err != nil {
		return 0, err
	}
	if !ok || len(buffers) == 0 {
		if bin != nil {
			return 0, failf(ReasonBuffer, "BIN chunk without a buffer")
		}
		return -1, nil
	}
	if len(buffers) > 1 {
		return 0, failf(ReasonBuffer, "%d buffers", len(buffers))
	}
	b := buffers[0]
	if err := b.checkMembers("buffers[0]", []string{"byteLength", "uri", "name"}, nil, nil); err != nil {
		return 0, err
	}
	if _, has := b["uri"]; has {
		return 0, failf(ReasonBuffer, "buffer has a uri")
	}
	n, ok, err := b.integer("byteLength", "buffers[0]")
	if err != nil {
		return 0, err
	}
	if !ok || n < 1 {
		return 0, failf(ReasonBuffer, "buffer byteLength missing")
	}
	if bin == nil || n > len(bin) || len(bin)-n > 3 {
		return 0, failf(ReasonBuffer, "buffer byteLength %d does not match BIN chunk", n)
	}
	if _, _, err := b.str("name", "buffers[0]"); err != nil {
		return 0, err
	}
	return n, nil
}

func parseViews(top object, bufferLen int) ([]bufferView, error) {
	items, _, err := top.array("bufferViews", "bufferViews")
	if err != nil {
		return nil, err
	}
	views := make([]bufferView, len(items))
	for i, v := range items {
		what := fmt.Sprintf("bufferViews[%d]", i)
		if err := v.checkMembers(what, []string{"buffer", "byteOffset", "byteLength", "byteStride", "target", "name"}, nil, nil); err != nil {
			return nil, err
		}
		buf, ok, err := v.integer("buffer", what)
		if err != nil {
			return nil, err
		}
		if !ok || buf != 0 || bufferLen < 0 {
			return nil, failf(ReasonBuffer, "%s does not point at the BIN buffer", what)
		}
		offset, _, err := v.integer("byteOffset", what)
		if err != nil {
			return nil, err
		}
		length, ok, err := v.integer("byteLength", what)
		if err != nil {
			return nil, err
		}
		if !ok || length < 1 || offset+length > bufferLen {
			return nil, failf(ReasonBuffer, "%s lies outside the buffer", what)
		}
		stride, hasStride, err := v.integer("byteStride", what)
		if err != nil {
			return nil, err
		}
		if hasStride && (stride < 4 || stride > 252 || stride%4 != 0) {
			return nil, failf(ReasonAccessor, "%s.byteStride %d", what, stride)
		}
		if target, ok, err := v.integer("target", what); err != nil {
			return nil, err
		} else if ok && target != 34962 && target != 34963 {
			return nil, failf(ReasonBuffer, "%s.target %d", what, target)
		}
		if _, _, err := v.str("name", what); err != nil {
			return nil, err
		}
		views[i] = bufferView{offset: offset, length: length, stride: stride}
	}
	return views, nil
}

func parseAccessors(top object, views []bufferView) ([]accessor, error) {
	items, _, err := top.array("accessors", "accessors")
	if err != nil {
		return nil, err
	}
	out := make([]accessor, len(items))
	for i, a := range items {
		what := fmt.Sprintf("accessors[%d]", i)
		allowed := []string{"bufferView", "byteOffset", "componentType", "normalized", "count", "type", "max", "min", "name"}
		if err := a.checkMembers(what, allowed, nil, map[string]string{"sparse": "sparse accessor"}); err != nil {
			return nil, err
		}
		view, err := a.index("bufferView", what, len(views))
		if err != nil {
			return nil, err
		}
		if view < 0 {
			return nil, failf(ReasonAccessor, "%s has no bufferView", what)
		}
		component, _, err := a.integer("componentType", what)
		if err != nil {
			return nil, err
		}
		size := componentSizes[component]
		if size == 0 {
			return nil, failf(ReasonAccessor, "%s.componentType %d", what, component)
		}
		typ, _, err := a.str("type", what)
		if err != nil {
			return nil, err
		}
		comps := typeComponents[typ]
		if comps == 0 {
			return nil, failf(ReasonAccessor, "%s.type %q", what, typ)
		}
		normalized, err := a.boolean("normalized", what)
		if err != nil {
			return nil, err
		}
		if normalized && component == compFloat {
			return nil, failf(ReasonAccessor, "%s normalizes floats", what)
		}
		count, ok, err := a.integer("count", what)
		if err != nil {
			return nil, err
		}
		offset, _, err := a.integer("byteOffset", what)
		if err != nil {
			return nil, err
		}
		v := views[view]
		elem := size * comps
		stride := v.stride
		if stride == 0 {
			stride = elem
		}
		switch {
		case !ok || count < 1 || count > v.length:
			return nil, failf(ReasonAccessor, "%s.count", what)
		case offset%size != 0 || (v.offset+offset)%size != 0:
			return nil, failf(ReasonAccessor, "%s is not aligned", what)
		case stride < elem:
			return nil, failf(ReasonAccessor, "%s stride %d below element size %d", what, stride, elem)
		case offset+stride*(count-1)+elem > v.length:
			return nil, failf(ReasonAccessor, "%s exceeds its bufferView", what)
		}
		acc := accessor{view: view, offset: offset, component: component, count: count, comps: comps, normalized: normalized}
		if acc.min, _, err = a.numbers("min", what, comps); err != nil {
			return nil, err
		}
		if acc.max, _, err = a.numbers("max", what, comps); err != nil {
			return nil, err
		}
		if _, _, err := a.str("name", what); err != nil {
			return nil, err
		}
		out[i] = acc
	}
	return out, nil
}

func parseMaterials(top object) ([]material, error) {
	items, _, err := top.array("materials", "materials")
	if err != nil {
		return nil, err
	}
	textures := map[string]string{"normalTexture": "texture", "occlusionTexture": "texture", "emissiveTexture": "texture"}
	out := make([]material, len(items))
	for i, m := range items {
		what := fmt.Sprintf("materials[%d]", i)
		allowed := []string{"name", "pbrMetallicRoughness", "emissiveFactor", "alphaMode", "alphaCutoff", "doubleSided"}
		if err := m.checkMembers(what, allowed, []string{emissiveStrength}, textures); err != nil {
			return nil, err
		}
		if err := checkMaterialValues(m, what); err != nil {
			return nil, err
		}
		name, _, err := m.str("name", what)
		if err != nil {
			return nil, err
		}
		id, isSlot := strings.CutPrefix(name, "slot:")
		if !isSlot {
			continue
		}
		if !namePattern.MatchString(id) {
			return nil, failf(ReasonName, "material name %q is not slot:<id>", name)
		}
		out[i] = material{slot: id, isSlot: true}
		if raw, ok := m["extras"]; ok {
			// extras carries three.js userData; only a string hint is read.
			extras, err := asObject(raw, what+".extras")
			if err != nil {
				return nil, err
			}
			hint, ok, err := extras.str("hint", what+".extras")
			if err != nil {
				return nil, err
			}
			if ok && utf8.RuneCountInString(hint) > maxHintRunes {
				return nil, failf(ReasonName, "%s hint longer than %d characters", what, maxHintRunes)
			}
			out[i].hint = hint
		}
	}
	return out, nil
}

func checkMaterialValues(m object, what string) error {
	if raw, ok := m["pbrMetallicRoughness"]; ok {
		pbr, err := asObject(raw, what+".pbrMetallicRoughness")
		if err != nil {
			return err
		}
		pwhat := what + ".pbrMetallicRoughness"
		textures := map[string]string{"baseColorTexture": "texture", "metallicRoughnessTexture": "texture"}
		if err := pbr.checkMembers(pwhat, []string{"baseColorFactor", "metallicFactor", "roughnessFactor"}, nil, textures); err != nil {
			return err
		}
		if _, _, err := pbr.numbers("baseColorFactor", pwhat, 4); err != nil {
			return err
		}
		for _, k := range []string{"metallicFactor", "roughnessFactor"} {
			if _, _, err := pbr.number(k, pwhat); err != nil {
				return err
			}
		}
	}
	if _, _, err := m.numbers("emissiveFactor", what, 3); err != nil {
		return err
	}
	if _, _, err := m.number("alphaCutoff", what); err != nil {
		return err
	}
	if _, err := m.boolean("doubleSided", what); err != nil {
		return err
	}
	if mode, ok, err := m.str("alphaMode", what); err != nil {
		return err
	} else if ok && mode != "OPAQUE" && mode != "MASK" && mode != "BLEND" {
		return failf(ReasonJSON, "%s.alphaMode %q", what, mode)
	}
	if raw, ok := m["extensions"]; ok {
		ext, err := asObject(raw, what+".extensions")
		if err != nil {
			return err
		}
		strength, err := asObject(ext[emissiveStrength], what+"."+emissiveStrength)
		if err != nil {
			return err
		}
		ewhat := what + "." + emissiveStrength
		if err := strength.checkMembers(ewhat, []string{"emissiveStrength"}, nil, nil); err != nil {
			return err
		}
		if v, _, err := strength.number("emissiveStrength", ewhat); err != nil {
			return err
		} else if v < 0 {
			return failf(ReasonJSON, "%s.emissiveStrength is negative", ewhat)
		}
	}
	return nil
}

func (d *document) parseMeshes(top object) ([][]primitive, error) {
	items, _, err := top.array("meshes", "meshes")
	if err != nil {
		return nil, err
	}
	out := make([][]primitive, len(items))
	for i, m := range items {
		what := fmt.Sprintf("meshes[%d]", i)
		if err := m.checkMembers(what, []string{"primitives", "name"}, nil, map[string]string{"weights": "morph target"}); err != nil {
			return nil, err
		}
		if _, _, err := m.str("name", what); err != nil {
			return nil, err
		}
		prims, ok, err := m.array("primitives", what+".primitives")
		if err != nil {
			return nil, err
		}
		if !ok || len(prims) == 0 {
			return nil, failf(ReasonJSON, "%s has no primitives", what)
		}
		out[i] = make([]primitive, len(prims))
		for j, p := range prims {
			if out[i][j], err = d.parsePrimitive(p, fmt.Sprintf("%s.primitives[%d]", what, j)); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

// attributeFormats lists the accepted vertex attributes: component types
// (normalized integers only for COLOR_0) and component counts.
var attributeFormats = map[string]struct {
	components []int
	counts     []int
}{
	"POSITION":   {[]int{compFloat}, []int{3}},
	"NORMAL":     {[]int{compFloat}, []int{3}},
	"TANGENT":    {[]int{compFloat}, []int{4}},
	"TEXCOORD_0": {[]int{compFloat}, []int{2}},
	"TEXCOORD_1": {[]int{compFloat}, []int{2}},
	"COLOR_0":    {[]int{compFloat, compUByte, compUShort}, []int{3, 4}},
}

func (d *document) parsePrimitive(p object, what string) (primitive, error) {
	allowed := []string{"attributes", "indices", "material", "mode"}
	if err := p.checkMembers(what, allowed, nil, map[string]string{"targets": "morph target"}); err != nil {
		return primitive{}, err
	}
	if mode, ok, err := p.integer("mode", what); err != nil {
		return primitive{}, err
	} else if ok && mode != modeTriangle {
		return primitive{}, failf(ReasonUnsupportedFeature, "primitive mode %d (%s)", mode, what)
	}
	attrsRaw, ok := p["attributes"]
	if !ok {
		return primitive{}, failf(ReasonJSON, "%s has no attributes", what)
	}
	attrs, err := asObject(attrsRaw, what+".attributes")
	if err != nil {
		return primitive{}, err
	}
	names := make([]string, 0, len(attrs))
	for k := range attrs {
		names = append(names, k)
	}
	sort.Strings(names)
	prim := primitive{position: -1}
	count := -1
	for _, name := range names {
		format, known := attributeFormats[name]
		if !known {
			if strings.HasPrefix(name, "JOINTS_") || strings.HasPrefix(name, "WEIGHTS_") {
				return primitive{}, failf(ReasonUnsupportedFeature, "skin attribute %s (%s)", name, what)
			}
			return primitive{}, failf(ReasonUnsupportedFeature, "attribute %s (%s)", name, what)
		}
		idx, err := attrs.index(name, what+".attributes", len(d.accessors))
		if err != nil {
			return primitive{}, err
		}
		a := d.accessors[idx]
		okFormat := slices.Contains(format.components, a.component) && slices.Contains(format.counts, a.comps)
		if !okFormat || (a.component != compFloat && !a.normalized) {
			return primitive{}, failf(ReasonAccessor, "%s attribute %s has an unsupported format", what, name)
		}
		if count >= 0 && a.count != count {
			return primitive{}, failf(ReasonAccessor, "%s attributes differ in count", what)
		}
		count = a.count
		if name == "POSITION" {
			prim.position = idx
		}
	}
	if prim.position < 0 {
		return primitive{}, failf(ReasonAccessor, "%s has no POSITION", what)
	}
	if prim.indices, err = p.index("indices", what, len(d.accessors)); err != nil {
		return primitive{}, err
	}
	if prim.indices >= 0 {
		a := d.accessors[prim.indices]
		if a.comps != 1 || a.normalized || (a.component != compUByte && a.component != compUShort && a.component != compUInt) {
			return primitive{}, failf(ReasonAccessor, "%s indices are not unsigned integer scalars", what)
		}
		if d.views[a.view].stride != 0 {
			return primitive{}, failf(ReasonAccessor, "%s index bufferView has a stride", what)
		}
	}
	if prim.material, err = p.index("material", what, len(d.materials)); err != nil {
		return primitive{}, err
	}
	return prim, nil
}

func parseNodes(top object, meshes int, limits Limits) ([]node, error) {
	items, _, err := top.array("nodes", "nodes")
	if err != nil {
		return nil, err
	}
	if len(items) > limits.MaxNodes {
		return nil, failf(ReasonLimit, "%d nodes, limit %d", len(items), limits.MaxNodes)
	}
	out := make([]node, len(items))
	for i, n := range items {
		what := fmt.Sprintf("nodes[%d]", i)
		allowed := []string{"name", "children", "mesh", "matrix", "translation", "rotation", "scale"}
		if err := n.checkMembers(what, allowed, nil, nodeUnsupported); err != nil {
			return nil, err
		}
		name, _, err := n.str("name", what)
		if err != nil {
			return nil, err
		}
		children, err := n.indices("children", what, len(items))
		if err != nil {
			return nil, err
		}
		mesh, err := n.index("mesh", what, meshes)
		if err != nil {
			return nil, err
		}
		local, err := localMatrix(n, what)
		if err != nil {
			return nil, err
		}
		out[i] = node{name: name, children: children, mesh: mesh, local: local}
	}
	return out, nil
}

// localMatrix returns the column-major local transform. TRS is composed the
// way three.js Matrix4.compose does; a matrix must be decomposable (no shear),
// because three.js decomposes it into position, quaternion and scale.
func localMatrix(n object, what string) ([16]float64, error) {
	m, hasMatrix, err := n.numbers("matrix", what, 16)
	if err != nil {
		return [16]float64{}, err
	}
	t, hasT, err := n.numbers("translation", what, 3)
	if err != nil {
		return [16]float64{}, err
	}
	r, hasR, err := n.numbers("rotation", what, 4)
	if err != nil {
		return [16]float64{}, err
	}
	s, hasS, err := n.numbers("scale", what, 3)
	if err != nil {
		return [16]float64{}, err
	}
	if hasMatrix {
		if hasT || hasR || hasS {
			return [16]float64{}, failf(ReasonSceneGraph, "%s has both matrix and TRS", what)
		}
		var out [16]float64
		copy(out[:], m)
		if out[3] != 0 || out[7] != 0 || out[11] != 0 || out[15] != 1 {
			return [16]float64{}, failf(ReasonSceneGraph, "%s matrix is not affine", what)
		}
		cols := [3][3]float64{{out[0], out[1], out[2]}, {out[4], out[5], out[6]}, {out[8], out[9], out[10]}}
		for _, pair := range [3][2]int{{0, 1}, {0, 2}, {1, 2}} {
			a, b := cols[pair[0]], cols[pair[1]]
			dot := a[0]*b[0] + a[1]*b[1] + a[2]*b[2]
			if math.Abs(dot) > 1e-4*norm(a)*norm(b) {
				return [16]float64{}, failf(ReasonSceneGraph, "%s matrix has shear", what)
			}
		}
		return out, nil
	}
	if !hasT {
		t = []float64{0, 0, 0}
	}
	if !hasR {
		r = []float64{0, 0, 0, 1}
	}
	if !hasS {
		s = []float64{1, 1, 1}
	}
	return compose(t, r, s), nil
}

func norm(v [3]float64) float64 { return math.Sqrt(v[0]*v[0] + v[1]*v[1] + v[2]*v[2]) }

func compose(t, q, s []float64) [16]float64 {
	x, y, z, w := q[0], q[1], q[2], q[3]
	x2, y2, z2 := x+x, y+y, z+z
	xx, xy, xz := x*x2, x*y2, x*z2
	yy, yz, zz := y*y2, y*z2, z*z2
	wx, wy, wz := w*x2, w*y2, w*z2
	return [16]float64{
		(1 - (yy + zz)) * s[0], (xy + wz) * s[0], (xz - wy) * s[0], 0,
		(xy - wz) * s[1], (1 - (xx + zz)) * s[1], (yz + wx) * s[1], 0,
		(xz + wy) * s[2], (yz - wx) * s[2], (1 - (xx + yy)) * s[2], 0,
		t[0], t[1], t[2], 1,
	}
}

func multiply(a, b [16]float64) [16]float64 {
	var out [16]float64
	for col := range 4 {
		for row := range 4 {
			out[col*4+row] = a[row]*b[col*4] + a[4+row]*b[col*4+1] + a[8+row]*b[col*4+2] + a[12+row]*b[col*4+3]
		}
	}
	return out
}

func parseScene(top object, nodes int) ([]int, error) {
	scenes, _, err := top.array("scenes", "scenes")
	if err != nil {
		return nil, err
	}
	if len(scenes) != 1 {
		return nil, failf(ReasonSceneGraph, "%d scenes, exactly one required", len(scenes))
	}
	if s, ok, err := top.integer("scene", "gltf"); err != nil {
		return nil, err
	} else if ok && s != 0 {
		return nil, failf(ReasonSceneGraph, "default scene %d", s)
	}
	if err := scenes[0].checkMembers("scenes[0]", []string{"nodes", "name"}, nil, nil); err != nil {
		return nil, err
	}
	if _, _, err := scenes[0].str("name", "scenes[0]"); err != nil {
		return nil, err
	}
	return scenes[0].indices("nodes", "scenes[0]", nodes)
}

// checkGraph requires a forest: every node has at most one parent, scene roots
// have none, and there is no cycle anywhere in the node array.
func (d *document) checkGraph() error {
	parents := make([]int, len(d.nodes))
	for i := range parents {
		parents[i] = -1
	}
	for i, n := range d.nodes {
		for _, c := range n.children {
			if parents[c] >= 0 {
				return failf(ReasonSceneGraph, "nodes[%d] has more than one parent", c)
			}
			parents[c] = i
		}
	}
	seen := make([]bool, len(d.nodes))
	for _, r := range d.roots {
		if parents[r] >= 0 || seen[r] {
			return failf(ReasonSceneGraph, "scene root nodes[%d] is not a root", r)
		}
		seen[r] = true
	}
	// With single parents, a cycle is a parent chain that returns to itself.
	state := make([]int8, len(d.nodes)) // 0 new, 1 on the current chain, 2 done
	for i := range d.nodes {
		var chain []int
		for j := i; j >= 0 && state[j] != 2; j = parents[j] {
			if state[j] == 1 {
				return failf(ReasonSceneGraph, "nodes[%d] is part of a cycle", j)
			}
			state[j] = 1
			chain = append(chain, j)
		}
		for _, j := range chain {
			state[j] = 2
		}
	}
	return nil
}

type localBounds struct{ min, max [3]float32 }

// manifest walks the scene depth-first (roots, then children, in array order)
// and derives everything from vertex data.
func (d *document) manifest(limits Limits) (Manifest, error) {
	positions := map[int]localBounds{}
	maxIndex := map[int]uint32{}
	instances, triangles := 0, 0
	bounds := Bounds{Min: [3]float64{math.Inf(1), math.Inf(1), math.Inf(1)}, Max: [3]float64{math.Inf(-1), math.Inf(-1), math.Inf(-1)}}
	slots := map[string]bool{}
	manifest := Manifest{Slots: []Slot{}, Parts: []string{}, Anchors: []Anchor{}}
	parts, anchors := map[string]bool{}, map[string]bool{}

	type frame struct {
		node  int
		world [16]float64
	}
	identity := compose([]float64{0, 0, 0}, []float64{0, 0, 0, 1}, []float64{1, 1, 1})
	stack := make([]frame, 0, len(d.roots))
	for i := len(d.roots) - 1; i >= 0; i-- {
		stack = append(stack, frame{node: d.roots[i], world: identity})
	}
	for len(stack) > 0 {
		f := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		n := d.nodes[f.node]
		world := multiply(f.world, n.local)
		if err := collectName(n.name, world, &manifest, parts, anchors); err != nil {
			return Manifest{}, err
		}
		if n.mesh >= 0 {
			for _, p := range d.meshes[n.mesh] {
				instances++
				if instances > limits.MaxPrimitiveInstances {
					return Manifest{}, failf(ReasonLimit, "more than %d primitive instances", limits.MaxPrimitiveInstances)
				}
				lb, err := d.positionBounds(p.position, positions)
				if err != nil {
					return Manifest{}, err
				}
				t, err := d.triangles(p, maxIndex)
				if err != nil {
					return Manifest{}, err
				}
				triangles += t
				if triangles > limits.MaxTriangles {
					return Manifest{}, failf(ReasonLimit, "more than %d triangles", limits.MaxTriangles)
				}
				extend(&bounds, lb, world)
				if p.material >= 0 && d.materials[p.material].isSlot && !slots[d.materials[p.material].slot] {
					m := d.materials[p.material]
					slots[m.slot] = true
					manifest.Slots = append(manifest.Slots, Slot{ID: m.slot, Hint: m.hint})
				}
			}
		}
		for i := len(n.children) - 1; i >= 0; i-- {
			stack = append(stack, frame{node: n.children[i], world: world})
		}
	}
	if triangles == 0 {
		return Manifest{}, failf(ReasonSceneGraph, "model has no triangles")
	}
	switch {
	case len(manifest.Slots) > limits.MaxSlots:
		return Manifest{}, failf(ReasonLimit, "%d material slots, limit %d", len(manifest.Slots), limits.MaxSlots)
	case len(manifest.Parts) > limits.MaxParts:
		return Manifest{}, failf(ReasonLimit, "%d parts, limit %d", len(manifest.Parts), limits.MaxParts)
	case len(manifest.Anchors) > limits.MaxAnchors:
		return Manifest{}, failf(ReasonLimit, "%d anchors, limit %d", len(manifest.Anchors), limits.MaxAnchors)
	}
	manifest.Bounds = bounds
	manifest.Triangles = triangles
	manifest.Normalize()
	return manifest, nil
}

// collectName records part and anchor nodes. Repeated ids are common (lib.array
// clones named objects): parts are a set, and an anchor keeps its first
// occurrence in traversal order.
func collectName(name string, world [16]float64, m *Manifest, parts, anchors map[string]bool) error {
	if id, ok := strings.CutPrefix(name, "part:"); ok {
		if !namePattern.MatchString(id) {
			return failf(ReasonName, "node name %q is not part:<id>", name)
		}
		if !parts[id] {
			parts[id] = true
			m.Parts = append(m.Parts, id)
		}
	}
	if id, ok := strings.CutPrefix(name, "anchor:"); ok {
		if !namePattern.MatchString(id) {
			return failf(ReasonName, "node name %q is not anchor:<id>", name)
		}
		if !anchors[id] {
			anchors[id] = true
			m.Anchors = append(m.Anchors, Anchor{ID: id, Position: [3]float64{world[12], world[13], world[14]}})
		}
	}
	return nil
}

// positionBounds reads every POSITION value and requires the declared min and
// max to equal the computed range exactly (both are float32 values).
func (d *document) positionBounds(idx int, cache map[int]localBounds) (localBounds, error) {
	if lb, ok := cache[idx]; ok {
		return lb, nil
	}
	a := d.accessors[idx]
	lb := localBounds{
		min: [3]float32{float32(math.Inf(1)), float32(math.Inf(1)), float32(math.Inf(1))},
		max: [3]float32{float32(math.Inf(-1)), float32(math.Inf(-1)), float32(math.Inf(-1))},
	}
	base, stride := d.elementLayout(a)
	for i := range a.count {
		at := base + i*stride
		for c := range 3 {
			v := math.Float32frombits(binary.LittleEndian.Uint32(d.bin[at+4*c:]))
			if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
				return localBounds{}, failf(ReasonAccessor, "accessors[%d] has a non-finite position", idx)
			}
			lb.min[c] = min(lb.min[c], v)
			lb.max[c] = max(lb.max[c], v)
		}
	}
	if a.min == nil || a.max == nil {
		return localBounds{}, failf(ReasonAccessor, "accessors[%d] POSITION has no min/max", idx)
	}
	for c := range 3 {
		if float32(a.min[c]) != lb.min[c] || float32(a.max[c]) != lb.max[c] {
			return localBounds{}, failf(ReasonBoundsMismatch, "accessors[%d] declares [%v, %v] but positions span [%v, %v] on axis %d", idx, a.min[c], a.max[c], lb.min[c], lb.max[c], c)
		}
	}
	cache[idx] = lb
	return lb, nil
}

func (d *document) elementLayout(a accessor) (base, stride int) {
	v := d.views[a.view]
	stride = v.stride
	if stride == 0 {
		stride = componentSizes[a.component] * a.comps
	}
	return v.offset + a.offset, stride
}

// triangles counts one primitive and checks its index range.
func (d *document) triangles(p primitive, cache map[int]uint32) (int, error) {
	vertices := d.accessors[p.position].count
	if p.indices < 0 {
		if vertices%3 != 0 {
			return 0, failf(ReasonIndexRange, "%d vertices is not a triangle list", vertices)
		}
		return vertices / 3, nil
	}
	a := d.accessors[p.indices]
	if a.count%3 != 0 {
		return 0, failf(ReasonIndexRange, "%d indices is not a triangle list", a.count)
	}
	highest, ok := cache[p.indices]
	if !ok {
		base, stride := d.elementLayout(a)
		for i := range a.count {
			at := base + i*stride
			var v uint32
			switch a.component {
			case compUByte:
				v = uint32(d.bin[at])
			case compUShort:
				v = uint32(binary.LittleEndian.Uint16(d.bin[at:]))
			default:
				v = binary.LittleEndian.Uint32(d.bin[at:])
			}
			highest = max(highest, v)
		}
		cache[p.indices] = highest
	}
	if uint64(highest) >= uint64(vertices) {
		return 0, failf(ReasonIndexRange, "index %d with %d vertices", highest, vertices)
	}
	return a.count / 3, nil
}

// extend merges the 8 transformed corners of a local box, matching three.js
// Box3.applyMatrix4 (and so Box3.setFromObject without precise).
func extend(b *Bounds, lb localBounds, m [16]float64) {
	for corner := range 8 {
		var p [3]float64
		for c := range 3 {
			if corner&(1<<c) == 0 {
				p[c] = float64(lb.min[c])
			} else {
				p[c] = float64(lb.max[c])
			}
		}
		w := [3]float64{
			m[0]*p[0] + m[4]*p[1] + m[8]*p[2] + m[12],
			m[1]*p[0] + m[5]*p[1] + m[9]*p[2] + m[13],
			m[2]*p[0] + m[6]*p[1] + m[10]*p[2] + m[14],
		}
		for c := range 3 {
			b.Min[c] = math.Min(b.Min[c], w[c])
			b.Max[c] = math.Max(b.Max[c], w[c])
		}
	}
}

// Normalize sorts slots, parts and anchors by id so manifests compare and
// serialize the same way regardless of who computed them.
func (m *Manifest) Normalize() {
	sort.Slice(m.Slots, func(i, j int) bool { return m.Slots[i].ID < m.Slots[j].ID })
	sort.Strings(m.Parts)
	sort.Slice(m.Anchors, func(i, j int) bool { return m.Anchors[i].ID < m.Anchors[j].ID })
}

// Tolerance is the coordinate tolerance (metres) when comparing manifests
// computed by different implementations.
const Tolerance = 1e-4

// Matches compares two manifests by the contract rules: after sorting by id,
// ids, hints and the triangle count are exact; bounds and anchor positions
// agree within Tolerance.
func (m Manifest) Matches(other Manifest) bool {
	a, b := m.clone(), other.clone()
	a.Normalize()
	b.Normalize()
	if a.Triangles != b.Triangles || len(a.Slots) != len(b.Slots) || len(a.Parts) != len(b.Parts) || len(a.Anchors) != len(b.Anchors) {
		return false
	}
	if !near(a.Bounds.Min, b.Bounds.Min) || !near(a.Bounds.Max, b.Bounds.Max) {
		return false
	}
	for i := range a.Slots {
		if a.Slots[i] != b.Slots[i] {
			return false
		}
	}
	for i := range a.Parts {
		if a.Parts[i] != b.Parts[i] {
			return false
		}
	}
	for i := range a.Anchors {
		if a.Anchors[i].ID != b.Anchors[i].ID || !near(a.Anchors[i].Position, b.Anchors[i].Position) {
			return false
		}
	}
	return true
}

func (m Manifest) clone() Manifest {
	m.Slots = append([]Slot(nil), m.Slots...)
	m.Parts = append([]string(nil), m.Parts...)
	m.Anchors = append([]Anchor(nil), m.Anchors...)
	return m
}

func near(a, b [3]float64) bool {
	for i := range a {
		if math.Abs(a[i]-b[i]) > Tolerance {
			return false
		}
	}
	return true
}
