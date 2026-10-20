package creativescene

import (
	"encoding/json"
	"fmt"
)

// reparent_objects from the assistant keeps world positions, as the editor's
// grouping does: resolveReparent fills in each move's local transform
// (local = inverse(new parent world) × object world), the orders the
// assistant left out and the groups' defaults, before the batch reaches the
// scene command.

// reparentFields is what resolveReparent reads from one move or new group;
// the item itself stays raw so everything else reaches strict decoding as
// sent.
type reparentFields struct {
	ID        string     `json:"id"`
	ObjectID  string     `json:"object_id"`
	ParentID  *string    `json:"parent_id"`
	Order     *int       `json:"order"`
	Transform *Transform `json:"transform"`
}

type reparentItem struct {
	raw map[string]json.RawMessage
	reparentFields
}

func decodeReparentItems(raws []json.RawMessage) ([]reparentItem, bool) {
	out := make([]reparentItem, len(raws))
	for i, raw := range raws {
		if json.Unmarshal(raw, &out[i].raw) != nil || json.Unmarshal(raw, &out[i].reparentFields) != nil {
			return nil, false
		}
	}
	return out, true
}

// reparentPlan is one reparent_objects being resolved.
type reparentPlan struct {
	scene   *lookAtScene
	groups  map[string]*reparentItem
	moves   []reparentItem
	removed map[string]bool
	// world caches the new groups' world matrices; nil marks one in progress.
	world map[string]*mat4
}

// resolveReparent fills in the reparent_objects of ops; other operations
// pass through byte for byte. moved is what update_objects in the batch
// moves: an object whose ancestor moves too would land somewhere the
// assistant did not see, so that is refused, as with look_at.
func resolveReparent(ops []json.RawMessage, cur Scene) ([]json.RawMessage, error) {
	type decoded struct {
		Op           string            `json:"op"`
		Moves        []json.RawMessage `json:"moves"`
		CreateGroups []json.RawMessage `json:"create_groups"`
		RemoveGroups []string          `json:"remove_groups"`
		Updates      []struct {
			ObjectID  string          `json:"object_id"`
			Transform json.RawMessage `json:"transform"`
			LookAt    json.RawMessage `json:"look_at"`
		} `json:"updates"`
	}
	heads := make([]decoded, len(ops))
	found := false
	moved := map[string]bool{}
	for i, raw := range ops {
		if json.Unmarshal(raw, &heads[i]) != nil {
			return ops, nil // the scene command reports the shape
		}
		switch heads[i].Op {
		case "reparent_objects":
			found = true
		case "update_objects":
			for _, u := range heads[i].Updates {
				if u.Transform != nil || u.LookAt != nil {
					moved[u.ObjectID] = true
				}
			}
		}
	}
	if !found {
		return ops, nil
	}
	s := &lookAtScene{objects: make(map[string]Object, len(cur.Objects)), added: map[string]lookAtItem{}, world: map[string]mat4{}}
	for _, o := range cur.Objects {
		s.objects[o.ID] = o
	}
	out := make([]json.RawMessage, len(ops))
	for i, h := range heads {
		if h.Op != "reparent_objects" {
			out[i] = ops[i]
			continue
		}
		moves, ok := decodeReparentItems(h.Moves)
		groups, ok2 := decodeReparentItems(h.CreateGroups)
		if !ok || !ok2 {
			out[i] = ops[i]
			continue
		}
		p := &reparentPlan{scene: s, groups: map[string]*reparentItem{}, moves: moves, removed: map[string]bool{}, world: map[string]*mat4{}}
		for j := range groups {
			p.groups[groups[j].ID] = &groups[j]
		}
		for _, id := range h.RemoveGroups {
			p.removed[id] = true
		}
		if err := p.resolve(groups, moved); err != nil {
			return nil, err
		}
		var whole map[string]json.RawMessage
		if err := json.Unmarshal(ops[i], &whole); err != nil {
			return nil, err
		}
		var err error
		if whole["moves"], err = marshalItems(moves); err != nil {
			return nil, err
		}
		if len(groups) > 0 {
			if whole["create_groups"], err = marshalItems(groups); err != nil {
				return nil, err
			}
		}
		if out[i], err = json.Marshal(whole); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func marshalItems(items []reparentItem) (json.RawMessage, error) {
	list := make([]map[string]json.RawMessage, len(items))
	for i, it := range items {
		list[i] = it.raw
	}
	return json.Marshal(list)
}

func setField(raw map[string]json.RawMessage, key string, v any) error {
	b, err := json.Marshal(v)
	raw[key] = b
	return err
}

func (p *reparentPlan) resolve(groups []reparentItem, moved map[string]bool) error {
	for _, m := range p.moves {
		if m.Transform != nil {
			return invalid("物件 %s：reparent_objects 的移动不要写 transform，改挂会自动保持世界位置", m.ObjectID)
		}
		if _, ok := p.scene.objects[m.ObjectID]; !ok {
			return nil // the scene command names the missing object
		}
		if moved[m.ObjectID] {
			return invalid("物件 %s 在同一次修改里也被移动了，请分两次提交", m.ObjectID)
		}
		chain, ok := p.scene.ancestors(m.ObjectID, p.scene.objects[m.ObjectID].ParentID)
		if !ok {
			return invalid("物件 %s 的父级链成环", m.ObjectID)
		}
		for id := m.ParentID; id != nil; id = p.parentOf(*id) {
			if len(chain) > maxObjects {
				return invalid("物件 %s 的新父级链成环", m.ObjectID)
			}
			chain = append(chain, *id)
		}
		for _, a := range chain {
			if moved[a] {
				return invalid("物件 %s 的父级 %s 在同一次修改里也被移动了，请分两次提交", m.ObjectID, a)
			}
		}
	}
	for j := range groups {
		if err := p.fillGroup(&groups[j]); err != nil {
			return err
		}
	}
	next := p.nextOrders()
	for j := range groups {
		if groups[j].Order == nil {
			if err := setField(groups[j].raw, "order", next(groups[j].ParentID)); err != nil {
				return err
			}
		}
	}
	for j := range p.moves {
		m := &p.moves[j]
		if m.Order == nil {
			if err := setField(m.raw, "order", next(m.ParentID)); err != nil {
				return err
			}
		}
		t, err := p.keepWorld(m.ObjectID, m.ParentID)
		if err != nil {
			return err
		}
		if err := setField(m.raw, "transform", t); err != nil {
			return err
		}
	}
	return nil
}

// parentOf is the parent an object will have when the operation's moves are
// done: new groups and moved objects included.
func (p *reparentPlan) parentOf(id string) *string {
	for _, m := range p.moves {
		if m.ObjectID == id {
			return m.ParentID
		}
	}
	if g, ok := p.groups[id]; ok {
		return g.ParentID
	}
	return p.scene.parentOf(id)
}

// fillGroup completes a new group the way add_objects needs it: kind,
// visibility, lock and name default as in the editor, and a missing
// transform is the middle of the objects moved into it, unrotated.
func (p *reparentPlan) fillGroup(g *reparentItem) error {
	defaults := map[string]any{"kind": KindGroup, "name": "分组", "visible": true, "locked": false, "parent_id": nil}
	for key, v := range defaults {
		if _, ok := g.raw[key]; !ok {
			if err := setField(g.raw, key, v); err != nil {
				return err
			}
		}
	}
	if g.Transform != nil {
		return nil
	}
	var sum vec3
	n := 0
	for _, m := range p.moves {
		if m.ParentID == nil || *m.ParentID != g.ID {
			continue
		}
		w, ok := p.scene.worldMatrix(m.ObjectID)
		if !ok {
			continue
		}
		for k := range 3 {
			sum[k] += w[12+k]
		}
		n++
	}
	position := vec3{}
	if n > 0 {
		position = vec3{sum[0] / float64(n), sum[1] / float64(n), sum[2] / float64(n)}
	}
	if g.ParentID != nil {
		parent, ok := p.worldOf(*g.ParentID)
		if !ok {
			return nil // the scene command names the missing parent
		}
		position = parent.invert().apply(position)
	}
	t := Transform{Position: position[:], Rotation: []float64{0, 0, 0, 1}, Scale: []float64{1, 1, 1}}
	g.Transform = &t
	return setField(g.raw, "transform", t)
}

// worldOf is the world matrix of an existing object or a new group, as the
// operation leaves it: moves keep world matrices, so existing objects are
// where they are now.
func (p *reparentPlan) worldOf(id string) (mat4, bool) {
	g, ok := p.groups[id]
	if !ok {
		return p.scene.worldMatrix(id)
	}
	if w, done := p.world[id]; done {
		if w == nil {
			return mat4{}, false // a loop among new groups; the scene refuses it
		}
		return *w, true
	}
	p.world[id] = nil
	if g.Transform == nil || len(g.Transform.Position) != 3 || len(g.Transform.Rotation) != 4 || len(g.Transform.Scale) != 3 {
		return mat4{}, false
	}
	t := g.Transform
	m := compose(vec3(t.Position), quat(t.Rotation), vec3(t.Scale))
	if g.ParentID != nil {
		parent, ok := p.worldOf(*g.ParentID)
		if !ok {
			return mat4{}, false
		}
		m = parent.mul(m)
	}
	p.world[id] = &m
	return m, true
}

// nextOrders hands out orders after the last child each parent keeps, in the
// order asked for.
func (p *reparentPlan) nextOrders() func(parent *string) int {
	leaving := make(map[string]bool, len(p.moves))
	for _, m := range p.moves {
		leaving[m.ObjectID] = true
	}
	key := func(parent *string) string {
		if parent == nil {
			return ""
		}
		return *parent
	}
	last := map[string]int{}
	take := func(parent *string, order int) {
		k := key(parent)
		if cur, ok := last[k]; !ok || order > cur {
			last[k] = order
		}
	}
	for id, o := range p.scene.objects {
		if !leaving[id] && !p.removed[id] {
			take(o.ParentID, o.Order)
		}
	}
	for _, g := range p.groups {
		if g.Order != nil {
			take(g.ParentID, *g.Order)
		}
	}
	for _, m := range p.moves {
		if m.Order != nil {
			take(m.ParentID, *m.Order)
		}
	}
	return func(parent *string) int {
		k := key(parent)
		next := 0
		if cur, ok := last[k]; ok {
			next = cur + 1
		}
		last[k] = next
		return next
	}
}

// keepWorld is the local transform that leaves object id where it is under
// parent.
func (p *reparentPlan) keepWorld(id string, parent *string) (Transform, error) {
	world, ok := p.scene.worldMatrix(id)
	if !ok {
		return Transform{}, invalid("物件 %s 的变换不完整", id)
	}
	local := world
	where := "根"
	if parent != nil {
		pw, ok := p.worldOf(*parent)
		if !ok {
			return Transform{}, invalid("父物件 %s 不存在", *parent)
		}
		if pw.determinant() == 0 {
			return Transform{}, invalid("物件 %s 无法在保持位置的前提下改挂到 %s：父级的缩放为 0", id, *parent)
		}
		local, where = pw.invert().mul(world), *parent
	}
	position, rotation, scale := local.decompose()
	for k := range 3 {
		if !in(position[k], -maxPositionM, maxPositionM) || !in(scale[k], minScale, maxScale) {
			return Transform{}, invalid("物件 %s 无法在保持位置的前提下改挂到 %s：%s", id, where,
				fmt.Sprintf("算出的局部位置 %v、缩放 %v 超出范围（位置 ±%d m，缩放 %g…%g）", position, scale, maxPositionM, minScale, float64(maxScale)))
		}
	}
	return Transform{Position: position[:], Rotation: rotation[:], Scale: scale[:]}, nil
}
