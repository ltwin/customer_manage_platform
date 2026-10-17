package creativescene

import (
	"slices"
	"sort"

	"github.com/samson/customer-manage-platform/backend/internal/creativedocument"
)

// restoreRequest is restore_units' unit list grouped by what it names.
type restoreRequest struct {
	environment, render bool
	objects             map[string]*listedUnits
	ids                 []string // sorted, for deterministic errors and output
}

type listedUnits struct{ placement, content bool }

func parseRestoreUnits(units []string) (restoreRequest, error) {
	r := restoreRequest{objects: map[string]*listedUnits{}}
	for _, u := range units {
		switch u {
		case unitObjects:
			return r, invalid("层级结构不能单独恢复，请恢复具体物件")
		case unitEnvironment:
			r.environment = true
			continue
		case unitRender:
			r.render = true
			continue
		}
		id, content, ok := objectOfUnit(u)
		if !ok {
			return r, invalid("场景没有单元 %q", u)
		}
		l := r.objects[id]
		if l == nil {
			l = &listedUnits{}
			r.objects[id] = l
			r.ids = append(r.ids, id)
		}
		if content {
			l.content = true
		} else {
			l.placement = true
		}
	}
	sort.Strings(r.ids)
	return r, nil
}

// restoreTouched lists the units restore_units checks. An object whose
// existence changes must be listed with both units and also touches objects.
func restoreTouched(old, cur Scene, units []string) ([]string, error) {
	r, err := parseRestoreUnits(units)
	if err != nil {
		return nil, err
	}
	var out []string
	if r.environment {
		out = append(out, unitEnvironment)
	}
	if r.render {
		out = append(out, unitRender)
	}
	oldIdx, curIdx := indexObjects(old.Objects), indexObjects(cur.Objects)
	existence := false
	for _, id := range r.ids {
		l := r.objects[id]
		_, inOld := oldIdx[id]
		_, inCur := curIdx[id]
		if inOld != inCur {
			if !l.placement || !l.content {
				return nil, invalid("物件 %s 会被加回或删除，摆放和内容两个单元都须列出", id)
			}
			existence = true
		}
		if l.placement {
			out = append(out, placementUnit(id))
		}
		if l.content {
			out = append(out, contentUnit(id))
		}
	}
	if existence {
		out = append(out, unitObjects)
	}
	return out, nil
}

// restoreUnits puts the listed units of cur back to their state in old. When
// objects come back or go away, the hierarchy is resolved over the final set
// (see resolveTree), so the result never depends on the order of units.
func restoreUnits(old, cur Scene, units []string) (Scene, creativedocument.Changes, error) {
	r, err := parseRestoreUnits(units)
	if err != nil {
		return Scene{}, creativedocument.Changes{}, err
	}
	next, c := cur, newChangeSet()
	next.Objects = slices.Clone(cur.Objects)
	if r.environment {
		next.Environment = old.Environment
		c.change(unitEnvironment)
	}
	if r.render {
		next.Render = old.Render
		c.change(unitRender)
	}
	oldIdx, curIdx := indexObjects(old.Objects), indexObjects(cur.Objects)
	added, deleted := map[string]bool{}, map[string]bool{}
	for _, id := range r.ids {
		l := r.objects[id]
		oi, inOld := oldIdx[id]
		ci, inCur := curIdx[id]
		switch {
		case inOld && inCur:
			o, src := &next.Objects[ci], old.Objects[oi]
			if l.placement {
				o.Name, o.Transform, o.Visible, o.Locked = src.Name, src.Transform, src.Visible, src.Locked
				c.change(placementUnit(id))
			}
			if l.content {
				o.Primitive, o.MaterialSlots, o.Light, o.Camera = src.Primitive, src.MaterialSlots, src.Light, src.Camera
				c.change(contentUnit(id))
			}
		case inOld:
			added[id] = true
			c.change(placementUnit(id), contentUnit(id))
		case inCur:
			deleted[id] = true
			c.remove(placementUnit(id), contentUnit(id))
		}
	}
	if len(added) > 0 || len(deleted) > 0 {
		next.Objects = resolveTree(old, next, added, deleted)
		c.change(unitObjects)
	}
	if err := Validate(next); err != nil {
		return Scene{}, creativedocument.Changes{}, err
	}
	return next, c.result(), nil
}

// resolveTree builds the final object set F (cur minus deleted, plus added
// taken from old) and gives each object its parent and order:
//   - parent: walk the ancestors in the object's source snapshot (old for
//     added objects, cur for kept ones) and take the first one that is a
//     group in F; otherwise the root. Added objects whose parent is added too
//     keep their hierarchy; children of a deleted group move up.
//   - order: kept objects under an unchanged parent keep theirs. The rest are
//     placed by (source depth, source parent, source order, id), keeping
//     their source order when it is free under the new parent and going last
//     otherwise.
//
// No cycle can arise: a kept object never had an added one as an ancestor,
// and added objects nest as they did in old, which had none.
func resolveTree(old, cur Scene, added, deleted map[string]bool) []Object {
	final := make([]Object, 0, len(cur.Objects)+len(added))
	for _, o := range cur.Objects {
		if !deleted[o.ID] {
			final = append(final, o)
		}
	}
	for _, o := range old.Objects {
		if added[o.ID] {
			final = append(final, o)
		}
	}
	inF := indexObjects(final)
	oldIdx, curIdx := indexObjects(old.Objects), indexObjects(cur.Objects)
	source := func(id string) (Scene, map[string]int) {
		if added[id] {
			return old, oldIdx
		}
		return cur, curIdx
	}
	newParent := func(id string) *string {
		s, idx := source(id)
		for p := s.Objects[idx[id]].ParentID; p != nil; {
			if i, ok := inF[*p]; ok && final[i].Kind == KindGroup {
				return p
			}
			pi, ok := idx[*p]
			if !ok {
				return nil
			}
			p = s.Objects[pi].ParentID
		}
		return nil
	}
	depth := func(id string) int {
		s, idx := source(id)
		n := 0
		for p := s.Objects[idx[id]].ParentID; p != nil; n++ {
			pi, ok := idx[*p]
			if !ok {
				break
			}
			p = s.Objects[pi].ParentID
		}
		return n
	}
	key := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}

	type slot struct {
		parent string
		order  int
	}
	taken := map[slot]bool{}
	last := map[string]int{}
	occupy := func(parent string, order int) {
		taken[slot{parent, order}] = true
		if prev, ok := last[parent]; !ok || order > prev {
			last[parent] = order
		}
	}
	parents := make([]*string, len(final))
	var moved []int
	for i := range final {
		parents[i] = newParent(final[i].ID)
		if !added[final[i].ID] && key(parents[i]) == key(final[i].ParentID) {
			occupy(key(parents[i]), final[i].Order)
			continue
		}
		moved = append(moved, i)
	}
	type sortKey struct {
		depth  int
		parent string
		order  int
		id     string
	}
	keys := make(map[int]sortKey, len(moved))
	for _, i := range moved {
		o := final[i]
		keys[i] = sortKey{depth(o.ID), key(o.ParentID), o.Order, o.ID}
	}
	sort.Slice(moved, func(a, b int) bool {
		x, y := keys[moved[a]], keys[moved[b]]
		if x.depth != y.depth {
			return x.depth < y.depth
		}
		if x.parent != y.parent {
			return x.parent < y.parent
		}
		if x.order != y.order {
			return x.order < y.order
		}
		return x.id < y.id
	})
	for _, i := range moved {
		parent, order := key(parents[i]), final[i].Order
		if taken[slot{parent, order}] {
			order = last[parent] + 1
		}
		final[i].ParentID, final[i].Order = parents[i], order
		occupy(parent, order)
	}
	return final
}
