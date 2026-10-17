package creativeglb

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite testdata fixtures")

// The fixtures are shared with frontend/scripts/creative-scene-model.test.ts:
// testdata/cases.json lists every file with its expected manifest or reason.
// Expected manifests are written by hand below, never derived from Inspect.

type fixtureCase struct {
	Name     string    `json:"name"`
	File     string    `json:"file"`
	Manifest *Manifest `json:"manifest,omitempty"`
	Reason   Reason    `json:"reason,omitempty"`
	build    func() []byte
}

type builder struct {
	bin       []byte
	views     []map[string]any
	accessors []map[string]any
	meshes    []map[string]any
	materials []map[string]any
	nodes     []map[string]any
	roots     []int
}

func (b *builder) view(data []byte, stride int) int {
	for len(b.bin)%4 != 0 {
		b.bin = append(b.bin, 0)
	}
	v := map[string]any{"buffer": 0, "byteOffset": len(b.bin), "byteLength": len(data)}
	if stride > 0 {
		v["byteStride"] = stride
	}
	b.bin = append(b.bin, data...)
	b.views = append(b.views, v)
	return len(b.views) - 1
}

func (b *builder) accessor(a map[string]any) int {
	b.accessors = append(b.accessors, a)
	return len(b.accessors) - 1
}

func floatBytes(vals []float32) []byte {
	out := make([]byte, 4*len(vals))
	for i, v := range vals {
		binary.LittleEndian.PutUint32(out[4*i:], math.Float32bits(v))
	}
	return out
}

func minMax(vals []float32, comps int) (lo, hi []float64) {
	lo, hi = make([]float64, comps), make([]float64, comps)
	for c := range comps {
		lo[c], hi[c] = math.Inf(1), math.Inf(-1)
	}
	for i, v := range vals {
		lo[i%comps] = math.Min(lo[i%comps], float64(v))
		hi[i%comps] = math.Max(hi[i%comps], float64(v))
	}
	return lo, hi
}

func (b *builder) positions(vals []float32) int {
	lo, hi := minMax(vals, 3)
	return b.accessor(map[string]any{"bufferView": b.view(floatBytes(vals), 0), "componentType": compFloat, "count": len(vals) / 3, "type": "VEC3", "min": lo, "max": hi})
}

func (b *builder) indices16(vals []uint16) int {
	data := make([]byte, 2*len(vals))
	for i, v := range vals {
		binary.LittleEndian.PutUint16(data[2*i:], v)
	}
	return b.accessor(map[string]any{"bufferView": b.view(data, 0), "componentType": compUShort, "count": len(vals), "type": "SCALAR"})
}

func (b *builder) mesh(prims ...map[string]any) int {
	list := make([]any, len(prims))
	for i, p := range prims {
		list[i] = p
	}
	b.meshes = append(b.meshes, map[string]any{"primitives": list})
	return len(b.meshes) - 1
}

func (b *builder) material(m map[string]any) int {
	b.materials = append(b.materials, m)
	return len(b.materials) - 1
}

func (b *builder) node(n map[string]any) int {
	b.nodes = append(b.nodes, n)
	return len(b.nodes) - 1
}

func (b *builder) doc() map[string]any {
	d := map[string]any{
		"asset":       map[string]any{"version": "2.0", "generator": "creativeglb fixtures"},
		"scene":       0,
		"scenes":      []any{map[string]any{"nodes": b.roots}},
		"nodes":       b.nodes,
		"meshes":      b.meshes,
		"accessors":   b.accessors,
		"bufferViews": b.views,
		"buffers":     []any{map[string]any{"byteLength": len(b.bin)}},
	}
	if len(b.materials) > 0 {
		d["materials"] = b.materials
	}
	return d
}

func encodeGLB(doc map[string]any, bin []byte) []byte {
	js, err := json.Marshal(doc)
	if err != nil {
		panic(err)
	}
	for len(js)%4 != 0 {
		js = append(js, ' ')
	}
	padded := append([]byte(nil), bin...)
	for len(padded)%4 != 0 {
		padded = append(padded, 0)
	}
	total := 12 + 8 + len(js)
	if len(padded) > 0 {
		total += 8 + len(padded)
	}
	out := make([]byte, 0, total)
	out = binary.LittleEndian.AppendUint32(out, glbMagic)
	out = binary.LittleEndian.AppendUint32(out, 2)
	out = binary.LittleEndian.AppendUint32(out, uint32(total))
	out = binary.LittleEndian.AppendUint32(out, uint32(len(js)))
	out = binary.LittleEndian.AppendUint32(out, chunkJSON)
	out = append(out, js...)
	if len(padded) > 0 {
		out = binary.LittleEndian.AppendUint32(out, uint32(len(padded)))
		out = binary.LittleEndian.AppendUint32(out, chunkBIN)
		out = append(out, padded...)
	}
	return out
}

// Unit box: x and z in [-0.5, 0.5], y in [0, 1].
var (
	boxPositions = []float32{
		-0.5, 0, -0.5, 0.5, 0, -0.5, 0.5, 1, -0.5, -0.5, 1, -0.5,
		-0.5, 0, 0.5, 0.5, 0, 0.5, 0.5, 1, 0.5, -0.5, 1, 0.5,
	}
	boxIndices = []uint16{
		0, 2, 1, 0, 3, 2, 4, 5, 6, 4, 6, 7, 0, 4, 7, 0, 7, 3,
		1, 2, 6, 1, 6, 5, 0, 1, 5, 0, 5, 4, 3, 7, 6, 3, 6, 2,
	}
)

func (b *builder) box(material int) map[string]any {
	p := map[string]any{"attributes": map[string]any{"POSITION": b.positions(boxPositions)}, "indices": b.indices16(boxIndices), "mode": modeTriangle}
	if material >= 0 {
		p["material"] = material
	}
	return p
}

// basic: one part with one slotted box.
func basicBuilder() *builder {
	b := &builder{}
	mat := b.material(map[string]any{"name": "slot:stone", "extras": map[string]any{"hint": "stone"}, "pbrMetallicRoughness": map[string]any{"baseColorFactor": []float64{0.8, 0.8, 0.8, 1}, "metallicFactor": 0, "roughnessFactor": 1}})
	mesh := b.mesh(b.box(mat))
	b.roots = []int{b.node(map[string]any{"name": "part:body", "mesh": mesh})}
	return b
}

func basicWith(mutate func(doc map[string]any, b *builder)) func() []byte {
	return func() []byte {
		b := basicBuilder()
		doc := b.doc()
		mutate(doc, b)
		return encodeGLB(doc, b.bin)
	}
}

func fixtureCases() []fixtureCase {
	empty := func() []Slot { return []Slot{} }
	cases := []fixtureCase{
		{
			Name: "basic",
			build: func() []byte {
				b := basicBuilder()
				return encodeGLB(b.doc(), b.bin)
			},
			Manifest: &Manifest{Bounds: Bounds{Min: [3]float64{-0.5, 0, -0.5}, Max: [3]float64{0.5, 1, 0.5}}, Triangles: 12, Slots: []Slot{{ID: "stone", Hint: "stone"}}, Parts: []string{"body"}, Anchors: []Anchor{}},
		},
		{
			Name: "hierarchy",
			build: func() []byte {
				b := &builder{}
				box := b.mesh(b.box(-1))
				panel := b.mesh(map[string]any{"attributes": map[string]any{"POSITION": b.positions([]float32{0, 0, 0, 1, 0, 0, 1, 1, 0, 0, 0, 0, 1, 1, 0, 0, 1, 0})}})
				b.nodes = make([]map[string]any, 5)
				b.nodes[0] = map[string]any{"name": "root", "translation": []float64{2, 0, 0}, "children": []int{1, 2, 4}}
				b.nodes[1] = map[string]any{"mesh": box, "scale": []float64{2, 1, 1}}
				b.nodes[2] = map[string]any{"mesh": panel, "matrix": []float64{0, 0, -1, 0, 0, 1, 0, 0, 1, 0, 0, 0, 0, 1, 0, 1}, "children": []int{3}}
				b.nodes[3] = map[string]any{"mesh": box, "rotation": []float64{0, 0, 1, 0}}
				b.nodes[4] = map[string]any{"name": "anchor:top", "translation": []float64{0, 2, 0}}
				b.roots = []int{0}
				return encodeGLB(b.doc(), b.bin)
			},
			Manifest: &Manifest{Bounds: Bounds{Min: [3]float64{1, 0, -1}, Max: [3]float64{3, 2, 0.5}}, Triangles: 26, Slots: empty(), Parts: []string{}, Anchors: []Anchor{{ID: "top", Position: [3]float64{2, 2, 0}}}},
		},
		{
			Name: "multi-material",
			build: func() []byte {
				b := &builder{}
				wood := b.material(map[string]any{"name": "slot:wood", "extras": map[string]any{"hint": "wood"}})
				metal := b.material(map[string]any{"name": "slot:metal", "doubleSided": true})
				plain := b.material(map[string]any{"name": "plain", "emissiveFactor": []float64{1, 0.5, 0}, "extensions": map[string]any{emissiveStrength: map[string]any{"emissiveStrength": 2}}})
				oak := b.material(map[string]any{"name": "slot:wood", "extras": map[string]any{"hint": "oak"}})
				pos := b.positions(boxPositions)
				first := b.indices16(boxIndices[:18])
				second := b.indices16(boxIndices[18:])
				split := b.mesh(
					map[string]any{"attributes": map[string]any{"POSITION": pos}, "indices": first, "material": wood},
					map[string]any{"attributes": map[string]any{"POSITION": pos}, "indices": second, "material": metal},
				)
				plainBox := b.mesh(b.box(plain))
				oakBox := b.mesh(b.box(oak))
				b.nodes = []map[string]any{
					{"name": "set", "mesh": split, "children": []int{1, 2}},
					{"translation": []float64{0, 0, 3}, "mesh": plainBox},
					{"translation": []float64{0, 0, -3}, "mesh": oakBox},
				}
				b.roots = []int{0}
				doc := b.doc()
				doc["extensionsUsed"] = []string{emissiveStrength}
				return encodeGLB(doc, b.bin)
			},
			Manifest: &Manifest{Bounds: Bounds{Min: [3]float64{-0.5, 0, -3.5}, Max: [3]float64{0.5, 1, 3.5}}, Triangles: 36, Slots: []Slot{{ID: "metal"}, {ID: "wood", Hint: "wood"}}, Parts: []string{}, Anchors: []Anchor{}},
		},
		{
			Name: "parts-anchors",
			build: func() []byte {
				b := &builder{}
				frame := b.material(map[string]any{"name": "slot:frame"})
				// POSITION and NORMAL interleaved with a 24-byte stride.
				inter := make([]float32, 0, 48)
				for i := range 8 {
					inter = append(inter, boxPositions[3*i:3*i+3]...)
					inter = append(inter, 0, 1, 0)
				}
				view := b.view(floatBytes(inter), 24)
				lo, hi := minMax(boxPositions, 3)
				pos := b.accessor(map[string]any{"bufferView": view, "componentType": compFloat, "count": 8, "type": "VEC3", "min": lo, "max": hi})
				normal := b.accessor(map[string]any{"bufferView": view, "byteOffset": 12, "componentType": compFloat, "count": 8, "type": "VEC3"})
				uv := b.accessor(map[string]any{"bufferView": b.view(floatBytes(make([]float32, 16)), 0), "componentType": compFloat, "count": 8, "type": "VEC2"})
				color := b.accessor(map[string]any{"bufferView": b.view(bytes.Repeat([]byte{255, 128, 0, 255}, 8), 0), "componentType": compUByte, "normalized": true, "count": 8, "type": "VEC4"})
				idx := make([]byte, len(boxIndices))
				for i, v := range boxIndices {
					idx[i] = byte(v)
				}
				indices := b.accessor(map[string]any{"bufferView": b.view(idx, 0), "componentType": compUByte, "count": len(idx), "type": "SCALAR"})
				mesh := b.mesh(map[string]any{"attributes": map[string]any{"POSITION": pos, "NORMAL": normal, "TEXCOORD_0": uv, "COLOR_0": color}, "indices": indices, "material": frame})
				b.nodes = []map[string]any{
					{"name": "part:door", "mesh": mesh, "children": []int{1, 2, 3, 4, 5}},
					{"name": "part:panel", "translation": []float64{1, 0, 0}, "mesh": mesh},
					{"name": "part:panel", "translation": []float64{-1, 0, 0}, "mesh": mesh},
					{"name": "anchor:hinge", "translation": []float64{0.5, 0.5, 0}},
					{"name": "anchor:hinge", "translation": []float64{-0.5, 0.5, 0}},
					{"name": "anchor:handle", "translation": []float64{0, 0.5, 0.5}},
				}
				b.roots = []int{0}
				return encodeGLB(b.doc(), b.bin)
			},
			Manifest: &Manifest{
				Bounds: Bounds{Min: [3]float64{-1.5, 0, -0.5}, Max: [3]float64{1.5, 1, 0.5}}, Triangles: 36,
				Slots: []Slot{{ID: "frame"}}, Parts: []string{"door", "panel"},
				Anchors: []Anchor{{ID: "handle", Position: [3]float64{0, 0.5, 0.5}}, {ID: "hinge", Position: [3]float64{0.5, 0.5, 0}}},
			},
		},
		{
			Name: "bad-bounds-mismatch", Reason: ReasonBoundsMismatch,
			build: basicWith(func(doc map[string]any, b *builder) {
				b.accessors[0]["max"] = []float64{0.5, 0.25, 0.5}
			}),
		},
		{
			Name: "bad-index-out-of-range", Reason: ReasonIndexRange,
			build: func() []byte {
				b := &builder{}
				idx := append([]uint16(nil), boxIndices...)
				idx[5] = 8
				mesh := b.mesh(map[string]any{"attributes": map[string]any{"POSITION": b.positions(boxPositions)}, "indices": b.indices16(idx)})
				b.roots = []int{b.node(map[string]any{"mesh": mesh})}
				return encodeGLB(b.doc(), b.bin)
			},
		},
		{
			Name: "bad-index-not-triangles", Reason: ReasonIndexRange,
			build: func() []byte {
				b := &builder{}
				mesh := b.mesh(map[string]any{"attributes": map[string]any{"POSITION": b.positions(boxPositions)}, "indices": b.indices16(boxIndices[:35])})
				b.roots = []int{b.node(map[string]any{"mesh": mesh})}
				return encodeGLB(b.doc(), b.bin)
			},
		},
		{
			Name: "bad-cycle", Reason: ReasonSceneGraph,
			build: basicWith(func(doc map[string]any, b *builder) {
				doc["nodes"] = append(b.nodes, map[string]any{"children": []int{2}}, map[string]any{"children": []int{1}})
			}),
		},
		{
			Name: "bad-shared-child", Reason: ReasonSceneGraph,
			build: basicWith(func(doc map[string]any, b *builder) {
				doc["nodes"] = append(b.nodes, map[string]any{"children": []int{0}}, map[string]any{"children": []int{0}})
				doc["scenes"] = []any{map[string]any{"nodes": []int{1, 2}}}
			}),
		},
		{
			Name: "bad-morph-target", Reason: ReasonUnsupportedFeature,
			build: basicWith(func(doc map[string]any, b *builder) {
				prim := b.meshes[0]["primitives"].([]any)[0].(map[string]any)
				prim["targets"] = []any{map[string]any{"POSITION": 0}}
			}),
		},
		{
			Name: "bad-skin", Reason: ReasonUnsupportedFeature,
			build: basicWith(func(doc map[string]any, b *builder) {
				doc["skins"] = []any{map[string]any{"joints": []int{0}}}
				b.nodes[0]["skin"] = 0
			}),
		},
		{
			Name: "bad-animation", Reason: ReasonUnsupportedFeature,
			build: basicWith(func(doc map[string]any, b *builder) {
				doc["animations"] = []any{map[string]any{"channels": []any{}, "samplers": []any{}}}
			}),
		},
		{
			// Also the host's "GLB with an external URI" sample: GLTFLoader
			// would fetch the image with the editor's cookies.
			Name: "bad-texture-external-uri", Reason: ReasonUnsupportedFeature,
			build: basicWith(func(doc map[string]any, b *builder) {
				doc["images"] = []any{map[string]any{"uri": "http://127.0.0.1:9/steal.png"}}
				doc["textures"] = []any{map[string]any{"source": 0}}
				b.materials[0]["pbrMetallicRoughness"].(map[string]any)["baseColorTexture"] = map[string]any{"index": 0}
			}),
		},
		{
			Name: "bad-buffer-external-uri", Reason: ReasonBuffer,
			build: func() []byte {
				b := basicBuilder()
				doc := b.doc()
				doc["buffers"] = []any{map[string]any{"byteLength": len(b.bin), "uri": "http://127.0.0.1:9/steal.bin"}}
				return encodeGLB(doc, nil)
			},
		},
		{
			Name: "bad-unknown-extension", Reason: ReasonUnsupportedFeature,
			build: basicWith(func(doc map[string]any, b *builder) {
				doc["extensionsUsed"] = []string{"KHR_lights_punctual"}
				b.nodes[0]["extensions"] = map[string]any{"KHR_lights_punctual": map[string]any{"light": 0}}
			}),
		},
		{
			Name: "bad-sparse-accessor", Reason: ReasonUnsupportedFeature,
			build: basicWith(func(doc map[string]any, b *builder) {
				b.accessors[0]["sparse"] = map[string]any{"count": 1, "indices": map[string]any{"bufferView": 1, "componentType": compUShort}, "values": map[string]any{"bufferView": 0}}
			}),
		},
		{
			Name: "bad-lines", Reason: ReasonUnsupportedFeature,
			build: basicWith(func(doc map[string]any, b *builder) {
				b.meshes[0]["primitives"].([]any)[0].(map[string]any)["mode"] = 1
			}),
		},
		{
			Name: "bad-shear-matrix", Reason: ReasonSceneGraph,
			build: basicWith(func(doc map[string]any, b *builder) {
				b.nodes[0]["matrix"] = []float64{1, 0, 0, 0, 0.5, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1}
			}),
		},
		{
			Name: "bad-part-name", Reason: ReasonName,
			build: basicWith(func(doc map[string]any, b *builder) {
				b.nodes[0]["name"] = "part:左门"
			}),
		},
		{
			Name: "bad-container-length", Reason: ReasonContainer,
			build: func() []byte {
				b := basicBuilder()
				out := encodeGLB(b.doc(), b.bin)
				binary.LittleEndian.PutUint32(out[8:], uint32(len(out)+4))
				return out
			},
		},
		{
			Name: "bad-too-many-triangles", Reason: ReasonLimit,
			build: func() []byte {
				b := &builder{}
				idx := make([]uint16, 0, 3000)
				for range 1000 {
					idx = append(idx, 0, 1, 2)
				}
				mesh := b.mesh(map[string]any{"attributes": map[string]any{"POSITION": b.positions([]float32{0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 1})}, "indices": b.indices16(idx)})
				for i := range 301 {
					b.roots = append(b.roots, b.node(map[string]any{"mesh": mesh, "translation": []float64{float64(i), 0, 0}}))
				}
				return encodeGLB(b.doc(), b.bin)
			},
		},
	}
	for i := range cases {
		cases[i].File = cases[i].Name + ".glb"
	}
	return cases
}

func TestFixtures(t *testing.T) {
	cases := fixtureCases()
	index, err := json.MarshalIndent(cases, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	index = append(index, '\n')
	indexPath := filepath.Join("testdata", "cases.json")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(indexPath, index, 0o644); err != nil {
			t.Fatal(err)
		}
	} else if got, err := os.ReadFile(indexPath); err != nil || !bytes.Equal(got, index) {
		t.Fatalf("testdata/cases.json is stale; run go test ./internal/creativeglb -run TestFixtures -update")
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			data := c.build()
			path := filepath.Join("testdata", c.File)
			if *update {
				if err := os.WriteFile(path, data, 0o644); err != nil {
					t.Fatal(err)
				}
			} else if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, data) {
				t.Fatalf("%s is stale; run go test ./internal/creativeglb -run TestFixtures -update", path)
			}
			got, err := Inspect(data, DefaultLimits())
			if c.Manifest != nil {
				if err != nil {
					t.Fatalf("Inspect: %v", err)
				}
				assertManifest(t, got, *c.Manifest)
				return
			}
			assertReason(t, err, c.Reason)
		})
	}
}

func assertManifest(t *testing.T, got, want Manifest) {
	t.Helper()
	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(want)
	if !bytes.Equal(gotJSON, wantJSON) {
		t.Fatalf("manifest\n got %s\nwant %s", gotJSON, wantJSON)
	}
}

func assertReason(t *testing.T, err error, want Reason) {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("got %v, want rejection %s", err, want)
	}
	if e.Reason != want {
		t.Fatalf("got %s (%s), want %s", e.Reason, e.Detail, want)
	}
	t.Logf("rejected: %s", e.Detail)
}

func TestLimits(t *testing.T) {
	small := DefaultLimits()
	small.MaxSlots, small.MaxParts, small.MaxAnchors = 0, 1, 1
	parts := fixtureCases()[3].build()
	basic := fixtureCases()[0].build()
	tests := []struct {
		name   string
		glb    []byte
		limits func(Limits) Limits
	}{
		{"bytes", basic, func(l Limits) Limits { l.MaxBytes = len(basic) - 1; return l }},
		{"nodes", parts, func(l Limits) Limits { l.MaxNodes = 5; return l }},
		{"instances", parts, func(l Limits) Limits { l.MaxPrimitiveInstances = 2; return l }},
		{"slots", basic, func(l Limits) Limits { l.MaxSlots = 0; return l }},
		{"parts", parts, func(l Limits) Limits { l.MaxParts = 1; return l }},
		{"anchors", parts, func(l Limits) Limits { l.MaxAnchors = 1; return l }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Inspect(tt.glb, tt.limits(DefaultLimits()))
			assertReason(t, err, ReasonLimit)
		})
	}
	if _, err := Inspect(parts, DefaultLimits()); err != nil {
		t.Fatalf("parts fixture within default limits: %v", err)
	}
}

// jsonGLB wraps raw JSON text (with the basic BIN chunk) so member-level
// parsing rules can be tested without the builder.
func jsonGLB(t *testing.T, edit func(string) string) []byte {
	t.Helper()
	b := basicBuilder()
	js, err := json.Marshal(b.doc())
	if err != nil {
		t.Fatal(err)
	}
	text := edit(string(js))
	raw := []byte(text)
	for len(raw)%4 != 0 {
		raw = append(raw, ' ')
	}
	total := 20 + len(raw) + 8 + len(b.bin)
	out := binary.LittleEndian.AppendUint32(nil, glbMagic)
	out = binary.LittleEndian.AppendUint32(out, 2)
	out = binary.LittleEndian.AppendUint32(out, uint32(total))
	out = binary.LittleEndian.AppendUint32(out, uint32(len(raw)))
	out = binary.LittleEndian.AppendUint32(out, chunkJSON)
	out = append(out, raw...)
	out = binary.LittleEndian.AppendUint32(out, uint32(len(b.bin)))
	out = binary.LittleEndian.AppendUint32(out, chunkBIN)
	return append(out, b.bin...)
}

func TestMemberParsing(t *testing.T) {
	tests := []struct {
		name string
		edit func(string) string
		want Reason // "" means accepted
	}{
		{"unchanged", func(s string) string { return s }, ""},
		// three.js reads exact names; a case variant must not be read as the
		// real member by the server.
		{"case variant member", func(s string) string { return strings.Replace(s, `"name":"part:body"`, `"Name":"part:body"`, 1) }, ReasonJSON},
		// Duplicate names keep the last value, like JSON.parse.
		{"duplicate member last wins", func(s string) string {
			return strings.Replace(s, `"name":"part:body"`, `"name":"part:左","name":"part:body"`, 1)
		}, ""},
		{"null number", func(s string) string { return strings.Replace(s, `"mesh":0`, `"mesh":null`, 1) }, ReasonJSON},
		{"fractional index", func(s string) string { return strings.Replace(s, `"mesh":0`, `"mesh":0.5`, 1) }, ReasonJSON},
		{"integral float index", func(s string) string { return strings.Replace(s, `"mesh":0`, `"mesh":0.0`, 1) }, ""},
		{"byte order mark", func(s string) string { return "\xEF\xBB\xBF" + s }, ReasonJSON},
		{"asset version", func(s string) string { return strings.Replace(s, `"version":"2.0"`, `"version":"1.0"`, 1) }, ReasonJSON},
		{"required extension", func(s string) string {
			return strings.Replace(s, `"asset":`, `"extensionsRequired":["KHR_materials_emissive_strength"],"asset":`, 1)
		}, ReasonUnsupportedFeature},
		{"second scene", func(s string) string {
			return strings.Replace(s, `"scenes":[{"nodes":[0]}]`, `"scenes":[{"nodes":[0]},{"nodes":[]}]`, 1)
		}, ReasonSceneGraph},
		{"matrix and TRS", func(s string) string {
			return strings.Replace(s, `"mesh":0`, `"mesh":0,"translation":[0,0,0],"matrix":[1,0,0,0,0,1,0,0,0,0,1,0,0,0,0,1]`, 1)
		}, ReasonSceneGraph},
		{"camera on node", func(s string) string { return strings.Replace(s, `"mesh":0`, `"mesh":0,"camera":0`, 1) }, ReasonUnsupportedFeature},
		{"joints attribute", func(s string) string { return strings.Replace(s, `"POSITION":0`, `"POSITION":0,"JOINTS_0":0`, 1) }, ReasonUnsupportedFeature},
		{"slot hint too long", func(s string) string {
			return strings.Replace(s, `"hint":"stone"`, `"hint":"`+strings.Repeat("石", 65)+`"`, 1)
		}, ReasonName},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := Inspect(jsonGLB(t, tt.edit), DefaultLimits())
			if tt.want == "" {
				if err != nil {
					t.Fatalf("Inspect: %v", err)
				}
				if m.Triangles != 12 || len(m.Parts) != 1 || m.Parts[0] != "body" {
					t.Fatalf("unexpected manifest %+v", m)
				}
				return
			}
			assertReason(t, err, tt.want)
		})
	}
}

func TestNoTriangles(t *testing.T) {
	b := basicBuilder()
	doc := b.doc()
	doc["scenes"] = []any{map[string]any{"nodes": []int{}}}
	_, err := Inspect(encodeGLB(doc, b.bin), DefaultLimits())
	assertReason(t, err, ReasonSceneGraph)
}

func TestMatches(t *testing.T) {
	base := Manifest{
		Bounds: Bounds{Min: [3]float64{0, 0, 0}, Max: [3]float64{1, 2, 3}}, Triangles: 10,
		Slots: []Slot{{ID: "b"}, {ID: "a", Hint: "stone"}}, Parts: []string{"y", "x"},
		Anchors: []Anchor{{ID: "top", Position: [3]float64{0, 2, 0}}},
	}
	tests := []struct {
		name   string
		mutate func(*Manifest)
		want   bool
	}{
		{"same", func(*Manifest) {}, true},
		{"order ignored", func(m *Manifest) {
			m.Slots = []Slot{{ID: "a", Hint: "stone"}, {ID: "b"}}
			m.Parts = []string{"x", "y"}
		}, true},
		{"bounds within tolerance", func(m *Manifest) { m.Bounds.Max[0] = 1 + 5e-5 }, true},
		{"bounds beyond tolerance", func(m *Manifest) { m.Bounds.Max[0] = 1 + 2e-4 }, false},
		{"anchor beyond tolerance", func(m *Manifest) { m.Anchors[0].Position[1] = 2.001 }, false},
		{"triangles differ", func(m *Manifest) { m.Triangles = 11 }, false},
		{"hint differs", func(m *Manifest) { m.Slots[1].Hint = "wood" }, false},
		{"part missing", func(m *Manifest) { m.Parts = []string{"x"} }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			other := base.clone()
			tt.mutate(&other)
			if got := base.Matches(other); got != tt.want {
				t.Fatalf("Matches = %v, want %v", got, tt.want)
			}
		})
	}
}
