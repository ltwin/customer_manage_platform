package creativescene

// Shadow is whether a light casts shadows on surfaces, as read_scene gives it
// (design「表面阴影上限」).
const (
	ShadowOn         = "on"
	ShadowOff        = "off"
	ShadowOverBudget = "over_budget"
)

// shadowMaps is how many shadow maps surfaces may use; a directional light
// takes four (cascades), any other light one.
const shadowMaps = 8

// shadowStates says which lights cast shadows, by the order the editor picks
// them in: the sky's sun first, then the lights with cast_shadow on in
// object-list order, each taken while it fits. The order ignores the camera,
// so moving the view never changes it; a light left out still lights the
// scene, without shadows. A hidden light, or one in a hidden group, is not
// drawn: it casts nothing and takes no map.
func shadowStates(s Scene) map[string]string {
	shown := shownIn(s.Objects)
	used := 0
	if s.Environment.Sky.Kind == "procedural" || s.Environment.Sky.Kind == SkyPhysical {
		used = 4
	}
	out := map[string]string{}
	for _, o := range s.Objects {
		l := o.Light
		switch {
		case l == nil:
			continue
		case l.CastShadow == nil || !*l.CastShadow || !shown(o.ID):
			out[o.ID] = ShadowOff
			continue
		}
		cost := 1
		if l.Type == "directional" {
			cost = 4
		}
		if used+cost > shadowMaps {
			out[o.ID] = ShadowOverBudget
			continue
		}
		used += cost
		out[o.ID] = ShadowOn
	}
	return out
}

// shownIn says whether an object of objects is drawn: it and every group
// above it visible.
func shownIn(objects []Object) func(id string) bool {
	byID := make(map[string]*Object, len(objects))
	for i := range objects {
		byID[objects[i].ID] = &objects[i]
	}
	memo := make(map[string]bool, len(objects))
	var shown func(id string, depth int) bool
	shown = func(id string, depth int) bool {
		if v, ok := memo[id]; ok {
			return v
		}
		o := byID[id]
		// A parent chain longer than the list is a loop, which validation rules out; not followed.
		v := o != nil && o.Visible && (o.ParentID == nil || (depth < len(objects) && shown(*o.ParentID, depth+1)))
		memo[id] = v
		return v
	}
	return func(id string) bool { return shown(id, 0) }
}
