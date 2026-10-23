package creativescene

import (
	"encoding/json"
	"sort"

	"github.com/samson/customer-manage-platform/backend/internal/creativedocument"
)

const (
	unitEnvironment = "environment"
	unitRender      = "render"
	unitObjects     = "objects"

	// maxBatchItems matches the object limit, so one confirmed edit of any
	// selection is always one operation and one revision.
	maxBatchItems = maxObjects
)

// sceneOp is one decoded scene operation. touched reads the scene before the
// batch; apply edits the working copy in batch order, with env for the
// operations that read or write content.
type sceneOp interface {
	touched(s Scene, byID map[string]int) ([]string, error)
	apply(env *opEnv, s *Scene, c *changeSet) error
}

type addObjectsOp struct {
	Op      string   `json:"op"`
	Objects []Object `json:"objects"`
}

type updateObjectsOp struct {
	Op      string         `json:"op"`
	Updates []objectUpdate `json:"updates"`
}

// objectUpdate changes only what it carries: placement fields are replaced,
// primitive.params, light and camera are merged by key (mergeLight,
// mergeCamera), and material_slots is replaced whole.
type objectUpdate struct {
	ObjectID      string                   `json:"object_id"`
	Name          *string                  `json:"name,omitempty"`
	Transform     *Transform               `json:"transform,omitempty"`
	Visible       *bool                    `json:"visible,omitempty"`
	Locked        *bool                    `json:"locked,omitempty"`
	Primitive     *primitivePatch          `json:"primitive,omitempty"`
	MaterialSlots *map[string]MaterialSlot `json:"material_slots,omitempty"`
	Light         *lightPatch              `json:"light,omitempty"`
	Camera        *Camera                  `json:"camera,omitempty"`
}

type primitivePatch struct {
	Params map[string]any `json:"params"`
}

type removeObjectsOp struct {
	Op        string   `json:"op"`
	ObjectIDs []string `json:"object_ids"`
}

// reparentObjectsOp runs in three steps: create_groups, then moves, then
// remove_groups. Grouping and ungrouping each touch objects, so they have to
// be one operation.
type reparentObjectsOp struct {
	Op           string   `json:"op"`
	Moves        []move   `json:"moves"`
	CreateGroups []Object `json:"create_groups,omitempty"`
	RemoveGroups []string `json:"remove_groups,omitempty"`
}

// move with a transform also replaces the local transform; the caller works
// it out to keep the world position.
type move struct {
	ObjectID  string     `json:"object_id"`
	ParentID  *string    `json:"parent_id"`
	Order     int        `json:"order"`
	Transform *Transform `json:"transform,omitempty"`
}

type setEnvironmentOp struct {
	Op          string      `json:"op"`
	Environment Environment `json:"environment"`
}

type setRenderOp struct {
	Op     string      `json:"op"`
	Render renderPatch `json:"render"`
}

func decodeOp(raw json.RawMessage) (sceneOp, error) {
	var head struct {
		Op string `json:"op"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return nil, invalid("操作格式不合法")
	}
	count := func(n int, what string) error {
		if n == 0 || n > maxBatchItems {
			return invalid("%s须为 1 到 %d 项", what, maxBatchItems)
		}
		return nil
	}
	switch head.Op {
	case "add_objects":
		var op addObjectsOp
		if err := decodeInput(raw, &op); err != nil {
			return nil, err
		}
		return &op, count(len(op.Objects), "objects ")
	case "update_objects":
		var op updateObjectsOp
		if err := decodeInput(raw, &op); err != nil {
			return nil, err
		}
		return &op, count(len(op.Updates), "updates ")
	case "remove_objects":
		var op removeObjectsOp
		if err := decodeInput(raw, &op); err != nil {
			return nil, err
		}
		return &op, count(len(op.ObjectIDs), "object_ids ")
	case "reparent_objects":
		var op reparentObjectsOp
		if err := decodeInput(raw, &op); err != nil {
			return nil, err
		}
		if err := count(len(op.Moves), "moves "); err != nil {
			return nil, err
		}
		if op.CreateGroups != nil {
			if err := count(len(op.CreateGroups), "create_groups "); err != nil {
				return nil, err
			}
		}
		if op.RemoveGroups != nil {
			if err := count(len(op.RemoveGroups), "remove_groups "); err != nil {
				return nil, err
			}
		}
		return &op, nil
	case "set_environment":
		var op setEnvironmentOp
		return &op, decodeInput(raw, &op)
	case "set_render":
		var op setRenderOp
		if err := decodeInput(raw, &op); err != nil {
			return nil, err
		}
		r := op.Render
		if r.Mode == nil && r.ExposureEV == nil && r.Engine == nil && r.Resolution == nil && r.ExposureEV100 == nil && r.ExposureMode == nil && r.ExposureCompensation == nil {
			return nil, invalid("render 至少写一项：mode、exposure_ev、engine、resolution、exposure_ev100、exposure_mode 或 exposure_compensation")
		}
		return &op, nil
	case "commit_model":
		var op commitModelOp
		return &op, decodeInput(raw, &op)
	case "set_instance_model":
		var op setInstanceModelOp
		if err := decodeInput(raw, &op); err != nil {
			return nil, err
		}
		return &op, op.checkScope()
	}
	return nil, invalid("场景不支持操作 %q", head.Op)
}

func decodeOps(raws []json.RawMessage) ([]sceneOp, error) {
	ops := make([]sceneOp, 0, len(raws))
	commits := 0
	for _, raw := range raws {
		op, err := decodeOp(raw)
		if err != nil {
			return nil, err
		}
		if _, ok := op.(*commitModelOp); ok {
			commits++
		}
		ops = append(ops, op)
	}
	if commits > maxCommitsPerBatch {
		return nil, invalid("一次最多提交 %d 个 commit_model", maxCommitsPerBatch)
	}
	return ops, nil
}

func indexObjects(objects []Object) map[string]int {
	out := make(map[string]int, len(objects))
	for i, o := range objects {
		out[o.ID] = i
	}
	return out
}

// changeSet collects Changes; both lists come out sorted so receipts are
// stable.
type changeSet struct {
	changed map[string]bool
	removed map[string]bool
	// focus lists the cameras whose focus_object_id the batch wrote; Apply
	// checks their targets against the scene after the whole batch.
	focus map[string]bool
}

func newChangeSet() *changeSet {
	return &changeSet{changed: map[string]bool{}, removed: map[string]bool{}, focus: map[string]bool{}}
}

func (c *changeSet) change(units ...string) {
	for _, u := range units {
		c.changed[u] = true
	}
}

func (c *changeSet) remove(units ...string) {
	for _, u := range units {
		c.removed[u] = true
	}
}

func (c *changeSet) result() creativedocument.Changes {
	sorted := func(set map[string]bool) []string {
		out := make([]string, 0, len(set))
		for u := range set {
			out = append(out, u)
		}
		sort.Strings(out)
		return out
	}
	return creativedocument.Changes{Changed: sorted(c.changed), Removed: sorted(c.removed)}
}

func (op *addObjectsOp) touched(Scene, map[string]int) ([]string, error) {
	return []string{unitObjects}, nil
}

func (op *addObjectsOp) apply(env *opEnv, s *Scene, c *changeSet) error {
	for _, o := range op.Objects {
		if o.Kind == KindModelInstance && o.Model != nil {
			if err := env.resolve(o.Model); err != nil {
				return err
			}
		}
		if o.Primitive != nil {
			b, err := lookupBuiltin(o.Primitive.Builtin)
			if err != nil {
				return invalid("物件 %s：%v", o.ID, err)
			}
			params, err := b.mergeParams(b.defaults(), o.Primitive.Params)
			if err != nil {
				return invalid("物件 %s：%v", o.ID, err)
			}
			o.Primitive = &Primitive{Builtin: b.Builtin, Params: params}
		}
		if o.Light != nil {
			if err := completeLight(o.Light); err != nil {
				return invalid("物件 %s：%v", o.ID, err)
			}
		}
		if o.Camera != nil {
			if o.Camera.FocusObjectID != nil {
				c.focus[o.ID] = true
			}
			completeCamera(o.Camera, ExposureAmbient)
		}
		canonicalNumbers(&o)
		s.Objects = append(s.Objects, o)
		c.change(unitObjects, placementUnit(o.ID), contentUnit(o.ID))
	}
	return nil
}

func (u objectUpdate) placement() bool {
	return u.Name != nil || u.Transform != nil || u.Visible != nil || u.Locked != nil
}

func (u objectUpdate) content() bool {
	return u.Primitive != nil || u.MaterialSlots != nil || u.Light != nil || u.Camera != nil
}

func (u objectUpdate) units() []string {
	var out []string
	if u.placement() {
		out = append(out, placementUnit(u.ObjectID))
	}
	if u.content() {
		out = append(out, contentUnit(u.ObjectID))
	}
	return out
}

func (op *updateObjectsOp) touched(_ Scene, byID map[string]int) ([]string, error) {
	seen := make(map[string]bool, len(op.Updates))
	var out []string
	for _, u := range op.Updates {
		if _, ok := byID[u.ObjectID]; !ok {
			return nil, invalid("物件 %s 不存在", u.ObjectID)
		}
		if seen[u.ObjectID] {
			return nil, invalid("物件 %s 在 updates 中出现了两次", u.ObjectID)
		}
		seen[u.ObjectID] = true
		units := u.units()
		if len(units) == 0 {
			return nil, invalid("物件 %s 的更新没有任何字段", u.ObjectID)
		}
		out = append(out, units...)
	}
	return out, nil
}

func (op *updateObjectsOp) apply(_ *opEnv, s *Scene, c *changeSet) error {
	byID := indexObjects(s.Objects)
	for _, u := range op.Updates {
		o := &s.Objects[byID[u.ObjectID]]
		if u.Name != nil {
			o.Name = *u.Name
		}
		if u.Transform != nil {
			o.Transform = *u.Transform
		}
		if u.Visible != nil {
			o.Visible = *u.Visible
		}
		if u.Locked != nil {
			o.Locked = *u.Locked
		}
		if u.Primitive != nil {
			if o.Primitive == nil {
				return invalid("物件 %s 不是基础体", o.ID)
			}
			b, err := lookupBuiltin(o.Primitive.Builtin)
			if err != nil {
				return invalid("物件 %s：%v", o.ID, err)
			}
			params, err := b.mergeParams(o.Primitive.Params, u.Primitive.Params)
			if err != nil {
				return invalid("物件 %s：%v", o.ID, err)
			}
			o.Primitive = &Primitive{Builtin: b.Builtin, Params: params}
		}
		if u.MaterialSlots != nil {
			o.MaterialSlots = *u.MaterialSlots
		}
		if u.Light != nil {
			if o.Light == nil {
				return invalid("物件 %s 不是灯光", o.ID)
			}
			l, err := mergeLight(*o.Light, *u.Light)
			if err != nil {
				return invalid("物件 %s：%v", o.ID, err)
			}
			o.Light = &l
		}
		if u.Camera != nil {
			if o.Camera == nil {
				return invalid("物件 %s 不是摄像机", o.ID)
			}
			if u.Camera.FocusObjectID != nil {
				c.focus[o.ID] = true
			}
			cam := mergeCamera(*o.Camera, *u.Camera)
			o.Camera = &cam
		}
		canonicalNumbers(o)
		c.change(u.units()...)
	}
	return nil
}

// subtree is ids plus every descendant in s, in snapshot order.
func subtree(s Scene, ids map[string]bool) map[string]bool {
	children := make(map[string][]string, len(s.Objects))
	for _, o := range s.Objects {
		if o.ParentID != nil {
			children[*o.ParentID] = append(children[*o.ParentID], o.ID)
		}
	}
	out := make(map[string]bool, len(ids))
	var walk func(id string)
	walk = func(id string) {
		if out[id] {
			return
		}
		out[id] = true
		for _, child := range children[id] {
			walk(child)
		}
	}
	for id := range ids {
		walk(id)
	}
	return out
}

// removed is the listed objects plus their descendants; listing an object
// whose ancestor is also listed is fine, listing it twice is not.
func (op *removeObjectsOp) removed(s Scene, byID map[string]int) (map[string]bool, error) {
	listed := make(map[string]bool, len(op.ObjectIDs))
	for _, id := range op.ObjectIDs {
		if _, ok := byID[id]; !ok {
			return nil, invalid("物件 %s 不存在", id)
		}
		if listed[id] {
			return nil, invalid("物件 %s 在 object_ids 中出现了两次", id)
		}
		listed[id] = true
	}
	return subtree(s, listed), nil
}

func (op *removeObjectsOp) touched(s Scene, byID map[string]int) ([]string, error) {
	gone, err := op.removed(s, byID)
	if err != nil {
		return nil, err
	}
	out := []string{unitObjects}
	for _, o := range s.Objects {
		if gone[o.ID] {
			out = append(out, placementUnit(o.ID), contentUnit(o.ID))
		}
	}
	return out, nil
}

func (op *removeObjectsOp) apply(_ *opEnv, s *Scene, c *changeSet) error {
	gone, err := op.removed(*s, indexObjects(s.Objects))
	if err != nil {
		return err
	}
	kept := s.Objects[:0:0]
	for _, o := range s.Objects {
		if gone[o.ID] {
			c.remove(placementUnit(o.ID), contentUnit(o.ID))
			continue
		}
		kept = append(kept, o)
	}
	s.Objects = kept
	c.change(unitObjects)
	return nil
}

// touched is objects, the placement of every move that carries a transform,
// and both units of every removed group. Created groups are new units, as in
// add_objects.
func (op *reparentObjectsOp) touched(s Scene, byID map[string]int) ([]string, error) {
	seen := make(map[string]bool, len(op.CreateGroups)+len(op.Moves)+len(op.RemoveGroups))
	once := func(id string) error {
		if seen[id] {
			return invalid("物件 %s 在 reparent_objects 中出现了两次", id)
		}
		seen[id] = true
		return nil
	}
	created := make(map[string]bool, len(op.CreateGroups))
	for _, g := range op.CreateGroups {
		if g.Kind != KindGroup {
			return nil, invalid("create_groups 里的物件 %s 不是分组", g.ID)
		}
		if err := once(g.ID); err != nil {
			return nil, err
		}
		created[g.ID] = true
	}
	out := []string{unitObjects}
	for _, m := range op.Moves {
		if _, ok := byID[m.ObjectID]; !ok {
			return nil, invalid("物件 %s 不存在", m.ObjectID)
		}
		if m.ParentID != nil && !created[*m.ParentID] {
			if _, ok := byID[*m.ParentID]; !ok {
				return nil, invalid("父物件 %s 不存在", *m.ParentID)
			}
		}
		if err := once(m.ObjectID); err != nil {
			return nil, err
		}
		if m.Transform != nil {
			out = append(out, placementUnit(m.ObjectID))
		}
	}
	for _, id := range op.RemoveGroups {
		i, ok := byID[id]
		if !ok {
			return nil, invalid("分组 %s 不存在", id)
		}
		if s.Objects[i].Kind != KindGroup {
			return nil, invalid("remove_groups 里的物件 %s 不是分组", id)
		}
		if err := once(id); err != nil {
			return nil, err
		}
		out = append(out, placementUnit(id), contentUnit(id))
	}
	return out, nil
}

// apply creates, moves, then removes; Validate then rejects a non-group
// parent, a cycle or a clashing order.
func (op *reparentObjectsOp) apply(_ *opEnv, s *Scene, c *changeSet) error {
	for _, g := range op.CreateGroups {
		canonicalNumbers(&g)
		s.Objects = append(s.Objects, g)
		c.change(placementUnit(g.ID), contentUnit(g.ID))
	}
	byID := indexObjects(s.Objects)
	for _, m := range op.Moves {
		o := &s.Objects[byID[m.ObjectID]]
		o.ParentID, o.Order = m.ParentID, m.Order
		if m.Transform != nil {
			o.Transform = *m.Transform
			canonicalNumbers(o)
			c.change(placementUnit(o.ID))
		}
	}
	if len(op.RemoveGroups) > 0 {
		gone := make(map[string]bool, len(op.RemoveGroups))
		for _, id := range op.RemoveGroups {
			gone[id] = true
		}
		// A child that is itself removed does not count: ungrouping nested
		// groups removes both.
		kept := s.Objects[:0:0]
		for _, o := range s.Objects {
			if o.ParentID != nil && gone[*o.ParentID] && !gone[o.ID] {
				return invalid("分组 %s 仍有子物件 %s，不能删除", *o.ParentID, o.ID)
			}
			if gone[o.ID] {
				c.remove(placementUnit(o.ID), contentUnit(o.ID))
				continue
			}
			kept = append(kept, o)
		}
		s.Objects = kept
	}
	c.change(unitObjects)
	return nil
}

func (op *setEnvironmentOp) touched(Scene, map[string]int) ([]string, error) {
	return []string{unitEnvironment}, nil
}

// apply replaces the environment whole; a physical sky is stored complete.
func (op *setEnvironmentOp) apply(_ *opEnv, s *Scene, c *changeSet) error {
	env := op.Environment
	if err := completeSky(&env.Sky); err != nil {
		return err
	}
	s.Environment = env
	c.change(unitEnvironment)
	return nil
}

func (op *setRenderOp) touched(Scene, map[string]int) ([]string, error) {
	return []string{unitRender}, nil
}

// apply merges by key: writing only the mode keeps the exposure.
func (op *setRenderOp) apply(_ *opEnv, s *Scene, c *changeSet) error {
	p := op.Render
	if p.Mode != nil {
		s.Render.Mode = *p.Mode
	}
	if p.ExposureEV != nil {
		s.Render.ExposureEV = *p.ExposureEV
	}
	if p.Engine != nil {
		s.Render.Engine = *p.Engine
	}
	if p.Resolution != nil {
		s.Render.Resolution = *p.Resolution
	}
	if p.ExposureEV100 != nil {
		s.Render.ExposureEV100 = *p.ExposureEV100
	}
	if p.ExposureMode != nil {
		s.Render.ExposureMode = *p.ExposureMode
	}
	if p.ExposureCompensation != nil {
		s.Render.ExposureCompensation = *p.ExposureCompensation
	}
	c.change(unitRender)
	return nil
}
