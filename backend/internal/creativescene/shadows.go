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
// scene, without shadows.
func shadowStates(s Scene) map[string]string {
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
		case l.CastShadow == nil || !*l.CastShadow:
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
