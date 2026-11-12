package creativescene

import (
	"fmt"
	"slices"
	"sort"

	"github.com/samson/customer-manage-platform/backend/internal/creativeglb"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
)

// Rig is a skinned model's standard humanoid mapping and node constraints
// (data: 模型内容「骨架与人形映射」). The browser finds it at import; the
// server only checks it against the model's skeleton and never evaluates a
// pose (patterns: 标准人形骨骼).
type Rig struct {
	// Humanoid maps standard bone names to joint node indices; nil for a
	// skeleton that is not humanoid.
	Humanoid    map[string]int `json:"humanoid,omitempty"`
	Constraints []Constraint   `json:"constraints,omitempty"`
}

// Constraint is one node constraint, as VRMC_node_constraint 1.0.
type Constraint struct {
	Node     int     `json:"node"`
	Kind     string  `json:"kind"`
	Source   int     `json:"source"`
	Weight   float64 `json:"weight"`
	RollAxis string  `json:"roll_axis,omitempty"`
	AimAxis  string  `json:"aim_axis,omitempty"`
}

const maxRigConstraints = 256

// The reasons a rig is refused (details.reason); the frontend's
// rig/humanoid.ts gives the same ones for the shared vectors.
const (
	rigNoSkeleton     = "no_skeleton"
	rigUnknownBone    = "unknown_bone"
	rigMissing        = "missing_required"
	rigNotJoint       = "not_joint"
	rigDuplicateNode  = "duplicate_node"
	rigHierarchy      = "hierarchy"
	rigInvalid        = "invalid"
	rigLimit          = "limit"
	rigSourceIsNode   = "source_is_node"
	rigConstraintLoop = "cycle"
)

// humanoidBones lists the standard bones (VRM 1.0 humanoid) in check order:
// torso and head, legs, arms, fingers, left before right. humanoidParent is
// each bone's parent on the standard chain.
var humanoidBones, humanoidParent = func() ([]string, map[string]string) {
	bones := []string{"hips", "spine", "chest", "upperChest", "neck", "head", "leftEye", "rightEye", "jaw"}
	parent := map[string]string{"spine": "hips", "chest": "spine", "upperChest": "chest", "neck": "upperChest", "head": "neck", "leftEye": "head", "rightEye": "head", "jaw": "head"}
	chain := func(s, first string, links ...string) {
		up := first
		for _, l := range links {
			parent[s+l] = up
			bones = append(bones, s+l)
			up = s + l
		}
	}
	for _, s := range []string{"left", "right"} {
		chain(s, "hips", "UpperLeg", "LowerLeg", "Foot", "Toes")
	}
	for _, s := range []string{"left", "right"} {
		chain(s, "upperChest", "Shoulder", "UpperArm", "LowerArm", "Hand")
	}
	for _, s := range []string{"left", "right"} {
		chain(s, s+"Hand", "ThumbMetacarpal", "ThumbProximal", "ThumbDistal")
		for _, f := range []string{"Index", "Middle", "Ring", "Little"} {
			chain(s, s+"Hand", f+"Proximal", f+"Intermediate", f+"Distal")
		}
	}
	return bones, parent
}()

var requiredBones = []string{
	"hips", "spine", "head",
	"leftUpperArm", "leftLowerArm", "leftHand", "leftUpperLeg", "leftLowerLeg", "leftFoot",
	"rightUpperArm", "rightLowerArm", "rightHand", "rightUpperLeg", "rightLowerLeg", "rightFoot",
}

var (
	rollAxes = []string{"X", "Y", "Z"}
	aimAxes  = []string{"PositiveX", "NegativeX", "PositiveY", "NegativeY", "PositiveZ", "NegativeZ"}
)

// Validate checks r against the skeleton of the model it belongs to, in the
// order of design「服务端的骨架与 rig 校验顺序」, and reports the first
// problem as a field of "rig".
func (r *Rig) Validate(skeleton *creativeglb.Skeleton) error {
	if skeleton == nil {
		return rigError("rig", rigNoSkeleton, "模型没有骨架，不能带 rig")
	}
	parents := make(map[int]*int, len(skeleton.Joints))
	for _, j := range skeleton.Joints {
		parents[j.Node] = j.Parent
	}
	if r.Humanoid != nil {
		if err := checkHumanoid(r.Humanoid, parents); err != nil {
			return err
		}
	}
	return checkConstraints(r.Constraints, parents)
}

func checkHumanoid(h map[string]int, parents map[int]*int) error {
	names := make([]string, 0, len(h))
	for b := range h {
		names = append(names, b)
	}
	sort.Strings(names)
	for _, b := range names {
		if _, ok := humanoidParent[b]; !ok && b != "hips" {
			return rigError("rig.humanoid."+b, rigUnknownBone, "%s 不是标准人形骨骼名", b)
		}
	}
	for _, b := range requiredBones {
		if _, ok := h[b]; !ok {
			return rigError("rig.humanoid."+b, rigMissing, "人形映射缺少必需骨骼 %s", b)
		}
	}
	boneOf := map[int]string{}
	for _, b := range humanoidBones {
		n, ok := h[b]
		if !ok {
			continue
		}
		if _, joint := parents[n]; !joint {
			return rigError("rig.humanoid."+b, rigNotJoint, "%s 映射的节点 %d 不是关节", b, n)
		}
		if other, dup := boneOf[n]; dup {
			return rigError("rig.humanoid."+b, rigDuplicateNode, "%s 与 %s 映射到同一节点 %d", b, other, n)
		}
		boneOf[n] = b
	}
	// Embedding: the nearest mapped joint ancestor must map a standard
	// ancestor of b (not necessarily its parent).
	for _, b := range humanoidBones[1:] {
		n, ok := h[b]
		if !ok {
			continue
		}
		got := ""
		for a := parents[n]; a != nil; a = parents[*a] {
			if m, mapped := boneOf[*a]; mapped {
				got = m
				break
			}
		}
		if !standardAncestor(got, b) {
			if got == "" {
				return rigError("rig.humanoid."+b, rigHierarchy, "%s 上面没有映射过的骨骼，应在 %s 之下", b, humanoidParent[b])
			}
			return rigError("rig.humanoid."+b, rigHierarchy, "%s 的上级是 %s，应为 %s 或它的上级骨骼", b, got, humanoidParent[b])
		}
	}
	return nil
}

func standardAncestor(a, b string) bool {
	for p := humanoidParent[b]; p != ""; p = humanoidParent[p] {
		if p == a {
			return true
		}
	}
	return false
}

func checkConstraints(cs []Constraint, parents map[int]*int) error {
	if len(cs) > maxRigConstraints {
		return rigError("rig.constraints", rigLimit, "节点约束不能超过 %d 条", maxRigConstraints)
	}
	for i, c := range cs {
		f := fmt.Sprintf("rig.constraints[%d]", i)
		switch {
		case c.Kind != "roll" && c.Kind != "aim" && c.Kind != "rotation":
			return rigError(f+".kind", rigInvalid, "约束种类须为 roll、aim 或 rotation")
		case !(c.Weight >= 0 && c.Weight <= 1):
			return rigError(f+".weight", rigInvalid, "约束权重须在 0 到 1 之间")
		case (c.Kind == "roll") != (c.RollAxis != "") || c.RollAxis != "" && !slices.Contains(rollAxes, c.RollAxis):
			return rigError(f+".roll_axis", rigInvalid, "roll 约束须有 roll_axis（X、Y、Z），其他种类不能有")
		case (c.Kind == "aim") != (c.AimAxis != "") || c.AimAxis != "" && !slices.Contains(aimAxes, c.AimAxis):
			return rigError(f+".aim_axis", rigInvalid, "aim 约束须有 aim_axis，其他种类不能有")
		}
	}
	owner := map[int]int{}
	for i, c := range cs {
		f := fmt.Sprintf("rig.constraints[%d].node", i)
		if _, joint := parents[c.Node]; !joint {
			return rigError(f, rigNotJoint, "被约束的节点 %d 不是关节", c.Node)
		}
		if _, dup := owner[c.Node]; dup {
			return rigError(f, rigDuplicateNode, "节点 %d 有不止一条约束", c.Node)
		}
		owner[c.Node] = i
	}
	for i, c := range cs {
		f := fmt.Sprintf("rig.constraints[%d].source", i)
		if c.Source == c.Node {
			return rigError(f, rigSourceIsNode, "约束的源不能是被约束的节点自己")
		}
		if _, joint := parents[c.Source]; !joint {
			return rigError(f, rigNotJoint, "约束的源 %d 不是关节", c.Source)
		}
	}
	if constraintCycle(cs, owner, parents) {
		return rigError("rig.constraints", rigConstraintLoop, "节点约束的依赖成环")
	}
	return nil
}

// constraintCycle reports whether the dependencies of data: 模型内容 (Epic
// 修订 v7) loop: roll and rotation read their source's local rotation; aim
// reads its source's world position and the world transform of its node's
// parent, so it also depends on constraints on their joint ancestors.
func constraintCycle(cs []Constraint, owner map[int]int, parents map[int]*int) bool {
	deps := make([][]int, len(cs))
	for i, c := range cs {
		nodes := []int{c.Source}
		if c.Kind == "aim" {
			for _, from := range []*int{parents[c.Source], parents[c.Node]} {
				for a := from; a != nil; a = parents[*a] {
					nodes = append(nodes, *a)
				}
			}
		}
		for _, n := range nodes {
			if j, ok := owner[n]; ok {
				deps[i] = append(deps[i], j)
			}
		}
	}
	const visiting, done = 1, 2
	state := make([]int, len(cs))
	var visit func(i int) bool
	visit = func(i int) bool {
		switch state[i] {
		case visiting:
			return true
		case done:
			return false
		}
		state[i] = visiting
		for _, d := range deps[i] {
			if visit(d) {
				return true
			}
		}
		state[i] = done
		return false
	}
	for i := range cs {
		if visit(i) {
			return true
		}
	}
	return false
}

func rigError(path, reason, format string, args ...any) error {
	return &creativeops.FieldError{Err: creativeops.ErrValidation, Op: -1, Path: path, Reason: reason, Message: fmt.Sprintf(format, args...)}
}
