package creativescene

import (
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"sort"
	"unicode/utf8"

	"github.com/samson/customer-manage-platform/backend/internal/creativeglb"
)

// Faces is a face assignment (data: 模型内容「面级材质分配」): some of a
// mesh's triangles drawn with another slot. The GLB is never rewritten; the
// renderer finds each mesh by its path and applies the ranges while the
// topology fingerprint still matches. The server checks only the shape and the
// limits: it never reads the GLB, and an assignment only changes how this
// account's own model looks.
type Faces struct {
	Meshes []FaceMesh        `json:"meshes"`
	Hints  map[string]string `json:"hints,omitempty"`
}

// FaceMesh is the assignment on one primitive.
type FaceMesh struct {
	Path      []PathStep         `json:"path"`
	Primitive int                `json:"primitive"`
	Topology  string             `json:"topology"`
	Assign    map[string][][]int `json:"assign"`
}

// PathStep is one node on a mesh's path: its name as the GLB's JSON has it and
// its place among same-named siblings, written as [name, n].
type PathStep struct {
	Name string
	N    int
}

func (s PathStep) MarshalJSON() ([]byte, error) { return json.Marshal([]any{s.Name, s.N}) }

func (s *PathStep) UnmarshalJSON(raw []byte) error {
	var pair []json.RawMessage
	if err := json.Unmarshal(raw, &pair); err != nil || len(pair) != 2 {
		return errors.New("path 的每段须为 [节点名, 序号]")
	}
	if err := json.Unmarshal(pair[0], &s.Name); err != nil {
		return errors.New("path 的节点名须为字符串")
	}
	if err := json.Unmarshal(pair[1], &s.N); err != nil {
		return errors.New("path 的序号须为整数")
	}
	return nil
}

const (
	maxFaceMeshes       = 2000
	maxFaceRanges       = 8192
	maxFaceBytes        = 128 << 10
	maxFacePathSteps    = 64
	maxFaceNodeName     = 64
	maxFacePrimitive    = 255
	maxFaceHint         = 40
	maxEffectiveSlots   = 32
	maxBuiltinFaceRange = 256
)

var (
	faceSlotPattern     = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	faceTopologyPattern = regexp.MustCompile(`^t1-[0-9a-f]{16}$`)
	// builtinFacePath is the only mesh a builtin has.
	builtinFacePath = []PathStep{{Name: "", N: 0}}
)

// checkFaces validates an assignment against the data contract. slots are the
// GLB's own (a builtin's is main); builtin applies the builtin's tighter rule.
func checkFaces(f *Faces, slots []string, builtin bool) error {
	if f == nil {
		return nil
	}
	if len(f.Meshes) == 0 || len(f.Meshes) > maxFaceMeshes || builtin && len(f.Meshes) > 1 {
		return invalid("faces.meshes 须有 1 到 %d 项", maxFaceMeshes)
	}
	seen := map[string]bool{}
	used := map[string]bool{}
	ranges := 0
	for i, m := range f.Meshes {
		if err := checkFacePath(m, builtin); err != nil {
			return invalid("faces.meshes[%d]：%v", i, err)
		}
		key, _ := json.Marshal([]any{m.Path, m.Primitive})
		if seen[string(key)] {
			return invalid("faces.meshes[%d]：同一网格出现两次", i)
		}
		seen[string(key)] = true
		if !faceTopologyPattern.MatchString(m.Topology) {
			return invalid("faces.meshes[%d].topology 格式不对", i)
		}
		n, err := checkFaceAssign(m.Assign, used)
		if err != nil {
			return invalid("faces.meshes[%d].assign：%v", i, err)
		}
		ranges += n
	}
	limit := maxFaceRanges
	if builtin {
		limit = maxBuiltinFaceRange
	}
	if ranges > limit {
		return invalid("面分配的区间不能超过 %d 个，请在模型代码里划分槽位", limit)
	}
	for slot, hint := range f.Hints {
		if !used[slot] || utf8.RuneCountInString(hint) > maxFaceHint {
			return invalid("faces.hints 只能给分配里用到的槽位，每条不超过 %d 个字符", maxFaceHint)
		}
	}
	if raw, _ := json.Marshal(f); len(raw) > maxFaceBytes {
		return invalid("面分配不能超过 128 KiB，请在模型代码里划分槽位")
	}
	if len(effectiveSlotIDs(slots, f)) > maxEffectiveSlots {
		return invalid("槽位合计不能超过 %d 个", maxEffectiveSlots)
	}
	return nil
}

func checkFacePath(m FaceMesh, builtin bool) error {
	if builtin && (!slices.Equal(m.Path, builtinFacePath) || m.Primitive != 0) {
		return errors.New(`基础体的 path 须为 [["", 0]]，primitive 为 0`)
	}
	if len(m.Path) == 0 || len(m.Path) > maxFacePathSteps {
		return errors.New("path 须有 1 到 64 段")
	}
	for _, s := range m.Path {
		if s.N < 0 || utf8.RuneCountInString(s.Name) > maxFaceNodeName {
			return errors.New("path 的节点名不超过 64 个字符，序号不小于 0")
		}
	}
	if m.Primitive < 0 || m.Primitive > maxFacePrimitive {
		return errors.New("primitive 须在 0 到 255 之间")
	}
	return nil
}

// checkFaceAssign checks one mesh's ranges, adds its slots to used and returns
// how many ranges it has.
func checkFaceAssign(assign map[string][][]int, used map[string]bool) (int, error) {
	if len(assign) == 0 {
		return 0, errors.New("至少分配一个槽位")
	}
	var all [][]int
	for slot, ranges := range assign {
		if !faceSlotPattern.MatchString(slot) {
			return 0, errors.New("槽位 ID 须为 1 到 64 个字母、数字、下划线或连字符")
		}
		if len(ranges) == 0 {
			return 0, errors.New("槽位没有区间")
		}
		for i, r := range ranges {
			if len(r) != 2 || r[0] < 0 || r[0] >= r[1] || r[1] > creativeglb.DefaultLimits().MaxTriangles {
				return 0, errors.New("区间须为 [start, end)，0 ≤ start < end ≤ 300000")
			}
			if i > 0 && r[0] < ranges[i-1][0] {
				return 0, errors.New("同一槽位的区间须升序")
			}
		}
		used[slot] = true
		all = append(all, ranges...)
	}
	sort.Slice(all, func(i, j int) bool { return all[i][0] < all[j][0] })
	for i := 1; i < len(all); i++ {
		if all[i][0] < all[i-1][1] {
			return 0, errors.New("区间互相重叠")
		}
	}
	return len(all), nil
}

// effectiveSlotIDs lists the slots an object shows: the GLB's, then those the
// assignment brings, each once.
func effectiveSlotIDs(slots []string, f *Faces) []string {
	out := slices.Clone(slots)
	if f == nil {
		return out
	}
	var extra []string
	for _, m := range f.Meshes {
		for slot := range m.Assign {
			if !slices.Contains(out, slot) && !slices.Contains(extra, slot) {
				extra = append(extra, slot)
			}
		}
	}
	sort.Strings(extra)
	return append(out, extra...)
}
