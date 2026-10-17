package creativescene_test

import (
	"math/rand/v2"
	"reflect"
	"strconv"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativedocument"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// restore sends restore_units the way undo does. objects is always expected;
// extra expected units are not checked.
func restore(t *testing.T, scope store.AccountScope, doc string, from creativeops.Revision, list []string) (creativedocument.CommandResult, error) {
	v := readScene(t, scope, doc)
	return send(t, scope, batch{DocumentID: doc, Expected: v.expect(append(append([]string(nil), list...), "objects")...),
		Operations: []any{op{"op": "restore_units", "from_revision": strconv.FormatInt(int64(from), 10), "units": list}}})
}

func mustRestore(t *testing.T, scope store.AccountScope, doc string, from creativeops.Revision, list []string) creativedocument.CommandResult {
	t.Helper()
	res, err := restore(t, scope, doc, from, list)
	if err != nil {
		t.Fatalf("restore %v from %d: %v", list, from, err)
	}
	return res
}

func remove(t *testing.T, scope store.AccountScope, doc string, removed []string, ids ...string) creativedocument.CommandResult {
	t.Helper()
	v := readScene(t, scope, doc)
	return mustSend(t, scope, batch{DocumentID: doc, Expected: v.expect(append(units(removed...), "objects")...), Operations: []any{op{"op": "remove_objects", "object_ids": ids}}})
}

func TestUndoAddAndDelete(t *testing.T) {
	f := setup(t)
	doc := createScene(t, f.a)
	a := objectID(1)
	o := box(a, nil, 1)
	o["transform"] = at(1, 0, -2)
	o["primitive"] = map[string]any{"builtin": "box@1", "params": map[string]any{"width": 2}}
	added := addObjects(t, f.a, doc, o).Revision
	before := readScene(t, f.a, doc).object(t, a)
	remove(t, f.a, doc, []string{a}, a)

	res := mustRestore(t, f.a, doc, added, units(a))
	if !reflect.DeepEqual(res.ChangedUnits, sorted(append(units(a), "objects"))) || len(res.RemovedUnits) != 0 {
		t.Fatalf("undo delete receipt %+v", res)
	}
	if got := readScene(t, f.a, doc).object(t, a); !reflect.DeepEqual(got, before) {
		t.Fatalf("restored %+v, want %+v", got, before)
	}

	res = mustRestore(t, f.a, doc, 1, units(a))
	if !reflect.DeepEqual(res.RemovedUnits, sorted(units(a))) || readScene(t, f.a, doc).has(a) {
		t.Fatalf("undo add receipt %+v", res)
	}
}

// nestedGroup is G at the root holding H and 74 boxes; H holds 74 more.
func nestedGroup(t *testing.T, scope store.AccountScope, doc string) (g string, all []string) {
	t.Helper()
	g, h := objectID(1), objectID(2)
	objects := []map[string]any{group(g, nil, 1), group(h, &g, 0)}
	all = []string{g, h}
	for i := range 74 {
		inG, inH := objectID(100+i), objectID(200+i)
		objects = append(objects, box(inG, &g, i+1), box(inH, &h, i))
		all = append(all, inG, inH)
	}
	addObjects(t, scope, doc, objects...)
	return g, all
}

func TestUndoDeleteOfLargeNestedGroup(t *testing.T) {
	f := setup(t)
	doc := createScene(t, f.a)
	g, all := nestedGroup(t, f.a, doc)
	if len(all) != 150 {
		t.Fatalf("group of %d objects", len(all))
	}
	before := readScene(t, f.a, doc)
	remove(t, f.a, doc, all, g)
	if v := readScene(t, f.a, doc); len(v.Scene.Objects) != 1 {
		t.Fatalf("delete left %d objects", len(v.Scene.Objects))
	}

	// One request brings back all 300 units with the same hierarchy.
	mustRestore(t, f.a, doc, before.Revision, units(all...))
	restored := readScene(t, f.a, doc)
	if !reflect.DeepEqual(byID(restored), byID(before)) {
		t.Fatalf("restored hierarchy %v, want %v", tree(restored), tree(before))
	}

	// The order of units does not matter.
	remove(t, f.a, doc, all, g)
	shuffled := units(all...)
	rand.New(rand.NewPCG(1, 2)).Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
	mustRestore(t, f.a, doc, restored.Revision, shuffled)
	if again := readScene(t, f.a, doc); !reflect.DeepEqual(byID(again), byID(before)) {
		t.Fatalf("shuffled restore %v, want %v", tree(again), tree(before))
	}
}

// TestUndoIsAllOrNothing: undoing a move of 150 objects fails as a whole
// when another writer has since moved one of them.
func TestUndoIsAllOrNothing(t *testing.T) {
	f := setup(t)
	doc := createScene(t, f.a)
	_, all := nestedGroup(t, f.a, doc)
	before := readScene(t, f.a, doc)
	updates := make([]any, len(all))
	for i, id := range all {
		updates[i] = map[string]any{"object_id": id, "transform": at(0, 1, 0)}
	}
	expected := map[string]any{}
	for _, id := range all {
		expected[placement(id)] = before.UnitRevs[placement(id)]
	}
	mustSend(t, f.a, batch{DocumentID: doc, Expected: expected, Operations: []any{op{"op": "update_objects", "updates": updates}}})
	moved := readScene(t, f.a, doc)
	other := all[70]
	mustSend(t, f.a, batch{DocumentID: doc, Expected: moved.expect(placement(other)), Operations: []any{op{"op": "update_objects", "updates": []any{map[string]any{"object_id": other, "transform": at(5, 0, 0)}}}}})
	after := readScene(t, f.a, doc)

	list := make([]string, len(all))
	stale := map[string]any{}
	for i, id := range all {
		list[i] = placement(id)
		stale[placement(id)] = moved.UnitRevs[placement(id)]
	}
	_, err := send(t, f.a, batch{DocumentID: doc, Expected: stale, Operations: []any{op{"op": "restore_units", "from_revision": strconv.FormatInt(int64(before.Revision), 10), "units": list}}})
	c, ok := creativedocument.IsPublicConflict(err)
	if !ok || len(c.Conflicts) != 1 || c.Conflicts[0].Unit != placement(other) {
		t.Fatalf("want one conflict on %s, got %v", other, err)
	}
	if v := readScene(t, f.a, doc); v.Revision != after.Revision || !reflect.DeepEqual(byID(v), byID(after)) {
		t.Fatal("a refused undo changed the scene")
	}
}

func TestRestoreResolvesHierarchy(t *testing.T) {
	f := setup(t)
	t.Run("lone object goes to the root and last", func(t *testing.T) {
		doc := createScene(t, f.a)
		g, h, a, b := objectID(1), objectID(2), objectID(3), objectID(4)
		rev := addObjects(t, f.a, doc, group(g, nil, 1), group(h, &g, 0), box(a, &h, 0), box(b, nil, 2)).Revision
		remove(t, f.a, doc, []string{g, h, a}, g)
		mustRestore(t, f.a, doc, rev, units(a))
		v := readScene(t, f.a, doc)
		// The ground holds order 0 at the root, and B holds 2.
		if o := v.object(t, a); o.ParentID != nil || o.Order != 3 {
			t.Fatalf("A at %v", tree(v)[a])
		}
		// H comes back to the root too; A, now kept, stays where it is.
		mustRestore(t, f.a, doc, rev, units(h))
		v = readScene(t, f.a, doc)
		if tree(v)[h] != "-#4" || tree(v)[a] != "-#3" {
			t.Fatalf("after restoring H: %v", tree(v))
		}
	})
	t.Run("nearest surviving group", func(t *testing.T) {
		doc := createScene(t, f.a)
		g, h, a, c := objectID(1), objectID(2), objectID(3), objectID(4)
		rev := addObjects(t, f.a, doc, group(g, nil, 1), group(h, &g, 0), box(a, &h, 0), box(c, &g, 1)).Revision
		remove(t, f.a, doc, []string{h, a}, h)
		mustRestore(t, f.a, doc, rev, units(a))
		if got := tree(readScene(t, f.a, doc))[a]; got != g+"#0" {
			t.Fatalf("A at %s, want under G at its old order", got)
		}
	})
	t.Run("children of a removed group move up", func(t *testing.T) {
		doc := createScene(t, f.a)
		g, b := objectID(1), objectID(2)
		addObjects(t, f.a, doc, group(g, nil, 1))
		addObjects(t, f.a, doc, box(b, &g, 0))
		res := mustRestore(t, f.a, doc, 1, units(g))
		if !reflect.DeepEqual(res.RemovedUnits, sorted(units(g))) || !reflect.DeepEqual(res.ChangedUnits, []string{"objects"}) {
			t.Fatalf("receipt %+v", res)
		}
		if got := tree(readScene(t, f.a, doc))[b]; got != "-#1" {
			t.Fatalf("B at %s, want at the root after the ground", got)
		}
	})
	t.Run("refused", func(t *testing.T) {
		doc := createScene(t, f.a)
		a := objectID(1)
		rev := addObjects(t, f.a, doc, box(a, nil, 1)).Revision
		remove(t, f.a, doc, []string{a}, a)
		for name, list := range map[string][]string{
			"objects alone":   {"objects"},
			"one unit of two": {placement(a)},
			"unknown unit":    {"lights"},
		} {
			t.Run(name, func(t *testing.T) {
				_, err := restore(t, f.a, doc, rev, list)
				wantErr(t, err, creativeops.ErrValidation)
			})
		}
	})
}

func TestRestoreEnvironmentAndPartialObject(t *testing.T) {
	f := setup(t)
	doc := createScene(t, f.a)
	a := objectID(1)
	rev := addObjects(t, f.a, doc, box(a, nil, 1)).Revision
	v := readScene(t, f.a, doc)
	mustSend(t, f.a, batch{DocumentID: doc, Expected: v.expect("environment", placement(a), content(a)), Operations: []any{
		op{"op": "set_environment", "environment": map[string]any{"sky": map[string]any{"kind": "color", "color": "#000000"}, "ambient": map[string]any{"color": "#ffffff", "intensity": 0}, "fog": nil}},
		op{"op": "update_objects", "updates": []any{map[string]any{"object_id": a, "transform": at(3, 0, 0), "primitive": map[string]any{"params": map[string]any{"depth": 4}}}}},
	}})
	// Only the placement comes back; the content edit stays.
	res := mustRestore(t, f.a, doc, rev, []string{"environment", placement(a)})
	if !reflect.DeepEqual(res.ChangedUnits, sorted([]string{"environment", placement(a)})) {
		t.Fatalf("receipt %+v", res)
	}
	v = readScene(t, f.a, doc)
	if o := v.object(t, a); o.Transform.Position[0] != 0 || o.Primitive.Params["depth"] != 4.0 || v.Scene.Environment.Sky.Kind != "procedural" {
		t.Fatalf("after partial restore %+v %+v", o, v.Scene.Environment)
	}
}
