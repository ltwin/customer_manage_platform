package creativeglb

import (
	"encoding/binary"
	"math"
)

// The skin fixtures share testdata/cases.json with the editor host. The rig:
// an Armature root turned 180° about X (a turned export root, like Mixamo's
// Michelle; 180° keeps the expected bounds exact) holding the skinned body
// (with a translation of its own, which glTF ignores) and two joints, hips and
// spine. The unit box's bottom four vertices follow hips, the top four spine.

// skinRig keeps the builder and the parts of the rig cases bend.
type skinRig struct {
	b                *builder
	prim             map[string]any
	skins            []any
	body, hips, root int
}

func newSkinRig() *skinRig {
	b := &builder{}
	mat := b.material(map[string]any{"name": "slot:skin"})
	joints := make([]byte, 0, 32)
	weights := make([]float32, 0, 32)
	for i := range 8 {
		j := byte(0)
		if boxPositions[3*i+1] > 0.5 {
			j = 1
		}
		joints = append(joints, j, 0, 0, 0)
		weights = append(weights, 1, 0, 0, 0)
	}
	prim := map[string]any{
		"attributes": map[string]any{
			"POSITION":  b.positions(boxPositions),
			"JOINTS_0":  b.accessor(map[string]any{"bufferView": b.view(joints, 0), "componentType": compUByte, "count": 8, "type": "VEC4"}),
			"WEIGHTS_0": b.accessor(map[string]any{"bufferView": b.view(floatBytes(weights), 0), "componentType": compFloat, "count": 8, "type": "VEC4"}),
		},
		"indices": b.indices16(boxIndices), "material": mat, "mode": modeTriangle,
	}
	mesh := b.mesh(prim)
	// Inverse bind matrices in armature space: hips at the origin, spine 0.5 up.
	ibm := floatBytes([]float32{
		1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1,
		1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, -0.5, 0, 1,
	})
	ibmAcc := b.accessor(map[string]any{"bufferView": b.view(ibm, 0), "componentType": compFloat, "count": 2, "type": "MAT4"})
	r := &skinRig{b: b, prim: prim}
	r.root = b.node(map[string]any{"name": "Armature", "rotation": []float64{1, 0, 0, 0}})
	r.body = b.node(map[string]any{"name": "body", "mesh": mesh, "skin": 0, "translation": []float64{5, 0, 0}})
	r.hips = b.node(map[string]any{"name": "hips"})
	spine := b.node(map[string]any{"name": "spine", "translation": []float64{0, 0.5, 0}})
	b.nodes[r.root]["children"] = []int{r.body, r.hips}
	b.nodes[r.hips]["children"] = []int{spine}
	b.roots = []int{r.root}
	r.skins = []any{map[string]any{"joints": []int{r.hips, spine}, "inverseBindMatrices": ibmAcc}}
	return r
}

func (r *skinRig) glb() []byte {
	doc := r.b.doc()
	if r.skins != nil {
		doc["skins"] = r.skins
	}
	return encodeGLB(doc, r.b.bin)
}

func skinWith(mutate func(r *skinRig)) func() []byte {
	return func() []byte {
		r := newSkinRig()
		mutate(r)
		return r.glb()
	}
}

func skinFixtureCases() []fixtureCase {
	no := false
	hips := 2
	return []fixtureCase{
		{
			// Rest bounds are the box turned by the Armature: y ← −y, z ← −z;
			// the body's own translation does not move it.
			Name: "skinned-turned-root",
			Manifest: &Manifest{
				Bounds: Bounds{Min: [3]float64{-0.5, -1, -0.5}, Max: [3]float64{0.5, 0, 0.5}}, Triangles: 12,
				Slots: []Slot{{ID: "skin"}}, Parts: []string{}, Anchors: []Anchor{}, UV: &no,
				Skeleton: &Skeleton{
					Joints:      []Joint{{Node: 2, Parent: nil, Name: "hips"}, {Node: 3, Parent: &hips, Name: "spine"}},
					Fingerprint: "s1-ab6dfbf4f5b06874",
				},
			},
			build: skinWith(func(*skinRig) {}),
		},
		{
			Name: "bad-skin-joint-range", Reason: ReasonSkin,
			build: skinWith(func(r *skinRig) {
				r.b.bin[r.b.views[1]["byteOffset"].(int)] = 2
			}),
		},
		{
			Name: "bad-skin-weights-sum", Reason: ReasonSkin,
			build: skinWith(func(r *skinRig) {
				at := r.b.views[2]["byteOffset"].(int)
				binary.LittleEndian.PutUint32(r.b.bin[at:], math.Float32bits(0.5))
			}),
		},
		{
			Name: "bad-skin-weight-negative", Reason: ReasonSkin,
			build: skinWith(func(r *skinRig) {
				at := r.b.views[2]["byteOffset"].(int)
				binary.LittleEndian.PutUint32(r.b.bin[at+4:], math.Float32bits(-0.25))
				binary.LittleEndian.PutUint32(r.b.bin[at:], math.Float32bits(1.25))
			}),
		},
		{
			Name: "bad-skin-joint-outside-scene", Reason: ReasonSkin,
			build: skinWith(func(r *skinRig) {
				stray := r.b.node(map[string]any{"name": "stray"})
				r.skins[0].(map[string]any)["joints"] = []int{r.hips, stray}
			}),
		},
		{
			Name: "bad-skin-without-mesh", Reason: ReasonSkin,
			build: skinWith(func(r *skinRig) {
				r.b.nodes[r.hips]["skin"] = 0
			}),
		},
		{
			Name: "bad-skin-mesh-also-static", Reason: ReasonSkin,
			build: skinWith(func(r *skinRig) {
				static := r.b.node(map[string]any{"name": "copy", "mesh": 0})
				r.b.nodes[r.root]["children"] = []int{r.body, r.hips, static}
			}),
		},
		{
			Name: "bad-skin-attributes-without-skin", Reason: ReasonSkin,
			build: skinWith(func(r *skinRig) {
				delete(r.b.nodes[r.body], "skin")
				r.skins = nil
			}),
		},
		{
			Name: "bad-skin-eight-influences", Reason: ReasonSkin,
			build: skinWith(func(r *skinRig) {
				attrs := r.prim["attributes"].(map[string]any)
				attrs["JOINTS_1"] = attrs["JOINTS_0"]
				attrs["WEIGHTS_1"] = attrs["WEIGHTS_0"]
			}),
		},
		{
			Name: "bad-skin-inverse-bind-count", Reason: ReasonSkin,
			build: skinWith(func(r *skinRig) {
				r.b.accessors[r.skins[0].(map[string]any)["inverseBindMatrices"].(int)]["count"] = 1
			}),
		},
		{
			Name: "bad-skin-normalized-joints", Reason: ReasonAccessor,
			build: skinWith(func(r *skinRig) {
				r.b.accessors[r.prim["attributes"].(map[string]any)["JOINTS_0"].(int)]["normalized"] = true
			}),
		},
		{
			Name: "bad-skin-too-many-joints", Reason: ReasonLimit,
			build: skinWith(func(r *skinRig) {
				joints := make([]int, 0, 257)
				children := []int{}
				for range 257 {
					j := r.b.node(map[string]any{})
					joints = append(joints, j)
					children = append(children, j)
				}
				r.b.nodes[r.hips]["children"] = append(r.b.nodes[r.hips]["children"].([]int), children...)
				r.skins[0].(map[string]any)["joints"] = joints
			}),
		},
	}
}
