package creativeglb

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"slices"
)

// The skin subset (scene-pose-system child 3, data: 模型内容「接受的 GLB 子集」):
// skins with inverse bind matrices, JOINTS_0 / WEIGHTS_0 on the meshes of
// skinned nodes, four influences per vertex. Skinned bounds and the skeleton
// summary are derived here.

// Skeleton is the joint summary of a skinned model: the union of every skin's
// joints by node index, each with its nearest joint ancestor. Fingerprint
// covers the structure only, not names or the rest pose.
type Skeleton struct {
	Joints      []Joint `json:"joints"`
	Fingerprint string  `json:"fingerprint"`
}

// Joint is one skeleton joint. Parent is nil for a joint without a joint
// ancestor.
type Joint struct {
	Node   int    `json:"node"`
	Parent *int   `json:"parent"`
	Name   string `json:"name"`
}

const (
	maxJointNameRunes = 64
	// weightTolerance is how far a vertex's weights may sum from 1.
	weightTolerance = 1e-3
)

type skin struct {
	joints []int
	// inverseBind holds one column-major matrix per joint.
	inverseBind [][16]float64
}

func (d *document) parseSkins(top object, nodes int, limits Limits) ([]skin, error) {
	items, _, err := top.array("skins", "skins")
	if err != nil {
		return nil, err
	}
	if len(items) > limits.MaxSkins {
		return nil, failf(ReasonLimit, "%d skins, limit %d", len(items), limits.MaxSkins)
	}
	out := make([]skin, len(items))
	for i, s := range items {
		what := fmt.Sprintf("skins[%d]", i)
		if err := s.checkMembers(what, []string{"joints", "inverseBindMatrices", "skeleton", "name"}, nil, nil); err != nil {
			return nil, err
		}
		if _, _, err := s.str("name", what); err != nil {
			return nil, err
		}
		if _, err := s.index("skeleton", what, nodes); err != nil {
			return nil, err
		}
		joints, err := s.indices("joints", what, nodes)
		if err != nil {
			return nil, err
		}
		if len(joints) == 0 {
			return nil, failf(ReasonSkin, "%s has no joints", what)
		}
		if len(joints) > limits.MaxJointsPerSkin {
			return nil, failf(ReasonLimit, "%s has %d joints, limit %d", what, len(joints), limits.MaxJointsPerSkin)
		}
		seen := map[int]bool{}
		for _, j := range joints {
			if seen[j] {
				return nil, failf(ReasonSkin, "%s lists nodes[%d] twice", what, j)
			}
			seen[j] = true
		}
		ibm, err := s.index("inverseBindMatrices", what, len(d.accessors))
		if err != nil {
			return nil, err
		}
		if ibm < 0 {
			return nil, failf(ReasonSkin, "%s has no inverseBindMatrices", what)
		}
		matrices, err := d.inverseBindMatrices(ibm, len(joints), what)
		if err != nil {
			return nil, err
		}
		out[i] = skin{joints: joints, inverseBind: matrices}
	}
	return out, nil
}

func (d *document) inverseBindMatrices(idx, joints int, what string) ([][16]float64, error) {
	a := d.accessors[idx]
	if a.component != compFloat || a.comps != 16 || a.count != joints {
		return nil, failf(ReasonSkin, "%s inverseBindMatrices is not %d float MAT4", what, joints)
	}
	base, stride := d.elementLayout(a)
	out := make([][16]float64, a.count)
	for i := range a.count {
		for c := range 16 {
			v := float64(math.Float32frombits(binary.LittleEndian.Uint32(d.bin[base+i*stride+4*c:])))
			if !finite(v) {
				return nil, failf(ReasonSkin, "%s inverseBindMatrices[%d] is not finite", what, i)
			}
			out[i][c] = v
		}
	}
	return out, nil
}

// checkSkins requires every skin's joints in the scene, a mesh on every
// skinned node, skin attributes exactly on the meshes of skinned nodes, joint
// indices within the skin and weights summing to 1.
func (d *document) checkSkins(limits Limits) error {
	reached := d.reachable()
	union := map[int]bool{}
	for i, s := range d.skins {
		for _, j := range s.joints {
			if !reached[j] {
				return failf(ReasonSkin, "skins[%d] joint nodes[%d] is not in the scene", i, j)
			}
			union[j] = true
		}
	}
	if len(union) > limits.MaxJoints {
		return failf(ReasonLimit, "%d joints, limit %d", len(union), limits.MaxJoints)
	}
	const static, skinned = 1, 2
	use := map[int]int{}
	maxJoint := map[int]uint32{}
	checkedWeights := map[int]bool{}
	// Like the POSITION budget: aliased accessors cannot make the scan longer
	// than the BIN chunk holds (four bytes is the smallest skin element).
	budget := len(d.bin) / 4
	for i, n := range d.nodes {
		if !reached[i] {
			continue
		}
		if n.skin >= 0 && n.mesh < 0 {
			return failf(ReasonSkin, "nodes[%d] has a skin but no mesh", i)
		}
		if n.mesh < 0 {
			continue
		}
		kind := static
		if n.skin >= 0 {
			kind = skinned
		}
		if use[n.mesh] != 0 && use[n.mesh] != kind {
			return failf(ReasonSkin, "meshes[%d] is used with and without a skin", n.mesh)
		}
		use[n.mesh] = kind
		for j, p := range d.meshes[n.mesh] {
			what := fmt.Sprintf("meshes[%d].primitives[%d]", n.mesh, j)
			if kind == static {
				if p.joints >= 0 || p.weights >= 0 {
					return failf(ReasonSkin, "%s has skin attributes but nodes[%d] has no skin", what, i)
				}
				continue
			}
			if p.joints < 0 || p.weights < 0 {
				return failf(ReasonSkin, "%s lacks JOINTS_0 or WEIGHTS_0 on skinned nodes[%d]", what, i)
			}
			highest, ok := maxJoint[p.joints]
			if !ok {
				var err error
				if highest, err = d.highestJoint(p.joints, &budget); err != nil {
					return err
				}
				maxJoint[p.joints] = highest
			}
			if int(highest) >= len(d.skins[n.skin].joints) {
				return failf(ReasonSkin, "%s joint index %d with %d joints in skins[%d]", what, highest, len(d.skins[n.skin].joints), n.skin)
			}
			if !checkedWeights[p.weights] {
				if err := d.checkWeights(p.weights, &budget); err != nil {
					return err
				}
				checkedWeights[p.weights] = true
			}
		}
	}
	return nil
}

func (d *document) reachable() []bool {
	reached := make([]bool, len(d.nodes))
	stack := append([]int(nil), d.roots...)
	for len(stack) > 0 {
		i := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		reached[i] = true
		stack = append(stack, d.nodes[i].children...)
	}
	return reached
}

func (d *document) highestJoint(idx int, budget *int) (uint32, error) {
	a := d.accessors[idx]
	if *budget -= a.count; *budget < 0 {
		return 0, failf(ReasonLimit, "skin accessors read more vertices than the BIN chunk holds")
	}
	base, stride := d.elementLayout(a)
	var highest uint32
	for i := range a.count {
		for c := range 4 {
			var v uint32
			if a.component == compUByte {
				v = uint32(d.bin[base+i*stride+c])
			} else {
				v = uint32(binary.LittleEndian.Uint16(d.bin[base+i*stride+2*c:]))
			}
			highest = max(highest, v)
		}
	}
	return highest, nil
}

func (d *document) checkWeights(idx int, budget *int) error {
	a := d.accessors[idx]
	if *budget -= a.count; *budget < 0 {
		return failf(ReasonLimit, "skin accessors read more vertices than the BIN chunk holds")
	}
	base, stride := d.elementLayout(a)
	for i := range a.count {
		sum := 0.0
		for c := range 4 {
			w := float64(math.Float32frombits(binary.LittleEndian.Uint32(d.bin[base+i*stride+4*c:])))
			if !(w >= 0 && w <= 1) {
				return failf(ReasonSkin, "accessors[%d] weight %v outside [0, 1]", idx, w)
			}
			sum += w
		}
		if math.Abs(sum-1) > weightTolerance {
			return failf(ReasonSkin, "accessors[%d] vertex %d weights sum to %v", idx, i, sum)
		}
	}
	return nil
}

// worlds returns every reachable node's world matrix (zero for the rest).
func (d *document) worlds() ([][16]float64, error) {
	out := make([][16]float64, len(d.nodes))
	type frame struct {
		node   int
		parent [16]float64
	}
	identity := compose([]float64{0, 0, 0}, []float64{0, 0, 0, 1}, []float64{1, 1, 1})
	stack := make([]frame, 0, len(d.roots))
	for _, r := range d.roots {
		stack = append(stack, frame{r, identity})
	}
	for len(stack) > 0 {
		f := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		w := multiply(f.parent, d.nodes[f.node].local)
		if !finite(w[:]...) {
			return nil, failf(ReasonSceneGraph, "nodes[%d] world transform overflows", f.node)
		}
		out[f.node] = w
		for _, c := range d.nodes[f.node].children {
			stack = append(stack, frame{c, w})
		}
	}
	return out, nil
}

// extendSkinned merges one skinned primitive at rest into b: each vertex is
// Σ wᵢ · (joint world · inverse bind)ᵢ · v. glTF ignores the skinned node's
// own transform, so it takes no part.
func (d *document) extendSkinned(b *Bounds, p primitive, s skin, worlds [][16]float64, budget *int) error {
	pos := d.accessors[p.position]
	if *budget -= pos.count; *budget < 0 {
		return failf(ReasonLimit, "POSITION accessors read more vertices than the BIN chunk holds (%d)", len(d.bin)/12)
	}
	joint := make([][16]float64, len(s.joints))
	for k, n := range s.joints {
		joint[k] = multiply(worlds[n], s.inverseBind[k])
	}
	jacc, wacc := d.accessors[p.joints], d.accessors[p.weights]
	pBase, pStride := d.elementLayout(pos)
	jBase, jStride := d.elementLayout(jacc)
	wBase, wStride := d.elementLayout(wacc)
	for i := range pos.count {
		var v [3]float64
		for c := range 3 {
			v[c] = float64(math.Float32frombits(binary.LittleEndian.Uint32(d.bin[pBase+i*pStride+4*c:])))
		}
		var out [3]float64
		for k := range 4 {
			w := float64(math.Float32frombits(binary.LittleEndian.Uint32(d.bin[wBase+i*wStride+4*k:])))
			if w == 0 {
				continue
			}
			var j int
			if jacc.component == compUByte {
				j = int(d.bin[jBase+i*jStride+k])
			} else {
				j = int(binary.LittleEndian.Uint16(d.bin[jBase+i*jStride+2*k:]))
			}
			m := joint[j]
			out[0] += w * (m[0]*v[0] + m[4]*v[1] + m[8]*v[2] + m[12])
			out[1] += w * (m[1]*v[0] + m[5]*v[1] + m[9]*v[2] + m[13])
			out[2] += w * (m[2]*v[0] + m[6]*v[1] + m[10]*v[2] + m[14])
		}
		for c := range 3 {
			b.Min[c] = math.Min(b.Min[c], out[c])
			b.Max[c] = math.Max(b.Max[c], out[c])
		}
	}
	return nil
}

// skeleton summarizes the union of every skin's joints.
func (d *document) skeleton() *Skeleton {
	if len(d.skins) == 0 {
		return nil
	}
	isJoint := map[int]bool{}
	for _, s := range d.skins {
		for _, j := range s.joints {
			isJoint[j] = true
		}
	}
	parents := make([]int, len(d.nodes))
	for i := range parents {
		parents[i] = -1
	}
	for i, n := range d.nodes {
		for _, c := range n.children {
			parents[c] = i
		}
	}
	nodes := make([]int, 0, len(isJoint))
	for j := range isJoint {
		nodes = append(nodes, j)
	}
	slices.Sort(nodes)
	out := &Skeleton{Joints: make([]Joint, len(nodes))}
	sum := sha256.New()
	_ = binary.Write(sum, binary.LittleEndian, uint32(len(nodes)))
	for i, n := range nodes {
		joint := Joint{Node: n, Name: truncateRunes(d.nodes[n].name, maxJointNameRunes)}
		parent := uint32(math.MaxUint32)
		for a := parents[n]; a >= 0; a = parents[a] {
			if isJoint[a] {
				joint.Parent = &a
				parent = uint32(a)
				break
			}
		}
		out.Joints[i] = joint
		_ = binary.Write(sum, binary.LittleEndian, [2]uint32{uint32(n), parent})
	}
	out.Fingerprint = "s1-" + hex.EncodeToString(sum.Sum(nil))[:16]
	return out
}

func truncateRunes(s string, n int) string {
	for i := range s {
		if n == 0 {
			return s[:i]
		}
		n--
	}
	return s
}

func (s *Skeleton) equal(o *Skeleton) bool {
	if s == nil || o == nil {
		return s == o
	}
	if s.Fingerprint != o.Fingerprint || len(s.Joints) != len(o.Joints) {
		return false
	}
	for i, j := range s.Joints {
		k := o.Joints[i]
		if j.Node != k.Node || j.Name != k.Name || (j.Parent == nil) != (k.Parent == nil) || (j.Parent != nil && *j.Parent != *k.Parent) {
			return false
		}
	}
	return true
}
