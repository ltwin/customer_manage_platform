package creativescene

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"sort"
	"unicode/utf8"

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativedocument"
	"github.com/samson/customer-manage-platform/backend/internal/creativeglb"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
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

// effectiveSlots lists the slots an object with this assignment (nil when
// there is none) shows, by creativecontent's rule.
func (f *Faces) effectiveSlots(glb []creativeglb.Slot) []creativecontent.Slot {
	if f == nil {
		return creativecontent.EffectiveSlots(glb, nil, nil)
	}
	assigns := make([]map[string][][]int, len(f.Meshes))
	for i, m := range f.Meshes {
		assigns[i] = m.Assign
	}
	return creativecontent.EffectiveSlots(glb, assigns, f.Hints)
}

// effectiveSlotIDs is effectiveSlots by ID alone.
func effectiveSlotIDs(slots []string, f *Faces) []string {
	glb := make([]creativeglb.Slot, len(slots))
	for i, id := range slots {
		glb[i] = creativeglb.Slot{ID: id}
	}
	eff := f.effectiveSlots(glb)
	out := make([]string, len(eff))
	for i, s := range eff {
		out[i] = s.ID
	}
	return out
}

// builtinSlots is the one slot a builtin's own geometry has.
var builtinSlots = []string{"main"}

// nextPrimitiveFaces is a builtin's assignment after an update: written
// faces replace it (null clears it); otherwise it holds unless the update
// changed a parameter of the builtin's topology declaration, which clears it
// (data: 场景内置基础体「topology」).
func nextPrimitiveFaces(b builtinSpec, old *Primitive, params map[string]any, written json.RawMessage) (*Faces, error) {
	if len(written) > 0 {
		var f *Faces
		dec := json.NewDecoder(bytes.NewReader(written))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&f); err != nil {
			return nil, errors.New("faces 格式不合法")
		}
		return f, checkFaces(f, builtinSlots, true)
	}
	if old.Faces != nil && b.topologyChanged(old.Params, params) {
		return nil, nil
	}
	return old.Faces, nil
}

// rawFaces is an assignment as JSON, null when there is none.
func rawFaces(f *Faces) json.RawMessage {
	raw, err := json.Marshal(f)
	if err != nil {
		return nil
	}
	return raw
}

// SlotTriangles is how many triangles of a face assignment a slot had.
type SlotTriangles struct {
	Slot      string `json:"slot"`
	Triangles int    `json:"triangles"`
}

// FacesChange is what a rebake did to the replaced model's face assignment:
// the entries carried over and those dropped, by slot.
type FacesChange struct {
	Kept    []SlotTriangles `json:"kept"`
	Dropped []SlotTriangles `json:"dropped"`
}

func countFaceSlots(meshes []FaceMesh) []SlotTriangles {
	counts := map[string]int{}
	for _, m := range meshes {
		for slot, ranges := range m.Assign {
			for _, r := range ranges {
				counts[slot] += r[1] - r[0]
			}
		}
	}
	out := make([]SlotTriangles, 0, len(counts))
	for slot, n := range counts {
		out = append(out, SlotTriangles{Slot: slot, Triangles: n})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slot < out[j].Slot })
	return out
}

// BakeFacesInTx compares the face assignment of the model a bake replaced
// with the one it wrote, for write_model's answer. The job result keeps its
// shape: the replaced model is read from the revision before the write. Nil
// when the bake added an object or the replaced model had no assignment.
func BakeFacesInTx(ctx context.Context, tx store.TxAccountScope, documentID string, bake BakeResult) (*FacesChange, error) {
	if bake.Revision < 2 || len(bake.ObjectIDs) == 0 {
		return nil, nil
	}
	prev, err := creativedocument.ReadAtInTx(ctx, tx, documentID, TypeKey, int64(bake.Revision)-1)
	if err != nil {
		return nil, err
	}
	scene, err := decodeScene(prev.Content.Payload)
	if err != nil {
		return nil, err
	}
	i, ok := indexObjects(scene.Objects)[bake.ObjectIDs[0]]
	if !ok || scene.Objects[i].Model == nil {
		return nil, nil
	}
	old := scene.Objects[i].Model.ContentRevisionID
	models, err := placedModelsInTx(ctx, tx, []string{old, bake.ContentRevisionID})
	if err != nil {
		return nil, err
	}
	before := models[old].payload.Faces
	if before == nil {
		return nil, nil
	}
	var kept []FaceMesh
	if after := models[bake.ContentRevisionID].payload.Faces; after != nil {
		kept = after.Meshes
	}
	carried := map[string]bool{}
	for _, m := range kept {
		key, _ := json.Marshal([]any{m.Path, m.Primitive})
		carried[string(key)] = true
	}
	var dropped []FaceMesh
	for _, m := range before.Meshes {
		if key, _ := json.Marshal([]any{m.Path, m.Primitive}); !carried[string(key)] {
			dropped = append(dropped, m)
		}
	}
	return &FacesChange{Kept: countFaceSlots(kept), Dropped: countFaceSlots(dropped)}, nil
}

// clearedFaces lists the builtins an edit left without the face assignment
// they had before, when the edit did not write their faces itself: a change of
// a topology parameter cleared them.
func clearedFaces(ctx context.Context, tx store.TxAccountScope, documentID string, before Scene, ops []json.RawMessage) ([]string, error) {
	written := map[string]bool{}
	for _, raw := range ops {
		var op struct {
			Op      string `json:"op"`
			Updates []struct {
				ObjectID  string                     `json:"object_id"`
				Primitive map[string]json.RawMessage `json:"primitive"`
			} `json:"updates"`
		}
		if json.Unmarshal(raw, &op) != nil || op.Op != "update_objects" {
			continue
		}
		for _, u := range op.Updates {
			if _, ok := u.Primitive["faces"]; ok {
				written[u.ObjectID] = true
			}
		}
	}
	had := map[string]bool{}
	for _, o := range before.Objects {
		if o.Primitive != nil && o.Primitive.Faces != nil && !written[o.ID] {
			had[o.ID] = true
		}
	}
	if len(had) == 0 {
		return nil, nil
	}
	cur, err := creativedocument.ReadCurrentInTx(ctx, tx, documentID, TypeKey)
	if err != nil {
		return nil, err
	}
	after, err := decodeScene(cur.Content.Payload)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, o := range after.Objects {
		if had[o.ID] && o.Primitive != nil && o.Primitive.Faces == nil {
			out = append(out, o.ID)
		}
	}
	sort.Strings(out)
	return out[:min(len(out), maxListedEntries)], nil
}
