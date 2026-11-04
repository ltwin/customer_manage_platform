package creativescene

import (
	"maps"
	"testing"
)

// Surfaces get eight shadow maps: the sky's sun takes four, then the lights
// casting shadows take theirs in object-list order, a directional four and
// any other one, each while it fits. A light that does not fit is passed
// over, so a later small one may still fit.
func TestShadowStatesFollowTheObjectList(t *testing.T) {
	cast, off := true, false
	light := func(id, typ string, shadows *bool) Object {
		return Object{ID: id, Kind: KindLight, Visible: true, Light: &Light{Type: typ, CastShadow: shadows}}
	}
	objects := []Object{
		light("spot", "spot", &cast),
		light("key", "directional", &cast),
		light("fill", "point", &off),
		light("rim", "area", &cast),
		light("bulb", "point", &cast),
		light("plain", "point", nil),
		{ID: "box", Kind: KindPrimitive, Visible: true},
	}
	for sky, want := range map[string]map[string]string{
		"procedural": {"spot": ShadowOn, "key": ShadowOverBudget, "fill": ShadowOff, "rim": ShadowOn, "bulb": ShadowOn, "plain": ShadowOff},
		SkyPhysical:  {"spot": ShadowOn, "key": ShadowOverBudget, "fill": ShadowOff, "rim": ShadowOn, "bulb": ShadowOn, "plain": ShadowOff},
		"color":      {"spot": ShadowOn, "key": ShadowOn, "fill": ShadowOff, "rim": ShadowOn, "bulb": ShadowOn, "plain": ShadowOff},
	} {
		got := shadowStates(Scene{Environment: Environment{Sky: Sky{Kind: sky}}, Objects: objects})
		if !maps.Equal(got, want) {
			t.Errorf("%s sky: %v, want %v", sky, got, want)
		}
	}
	many := []Object{light("a", "directional", &cast), light("b", "directional", &cast), light("c", "point", &cast)}
	if got := shadowStates(Scene{Environment: Environment{Sky: Sky{Kind: "color"}}, Objects: many}); got["a"] != ShadowOn || got["b"] != ShadowOn || got["c"] != ShadowOverBudget {
		t.Errorf("two directionals fill the maps: %v", got)
	}
}

// A light that is not drawn, hidden or in a hidden group, casts nothing and
// leaves its maps to the next.
func TestHiddenLightsTakeNoShadowMaps(t *testing.T) {
	cast := true
	group := func(id string, visible bool) Object { return Object{ID: id, Kind: KindGroup, Visible: visible} }
	light := func(id, typ string, visible bool, parent *string) Object {
		return Object{ID: id, Kind: KindLight, Visible: visible, ParentID: parent, Light: &Light{Type: typ, CastShadow: &cast}}
	}
	shut, open := "shut", "open"
	objects := []Object{
		light("hidden", "directional", false, nil),
		light("boxed", "directional", true, &shut),
		group(shut, false),
		group(open, true),
		light("key", "directional", true, &open),
	}
	got := shadowStates(Scene{Environment: Environment{Sky: Sky{Kind: "procedural"}}, Objects: objects})
	want := map[string]string{"hidden": ShadowOff, "boxed": ShadowOff, "key": ShadowOn}
	if !maps.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}
