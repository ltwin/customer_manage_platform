package creativescene_test

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativedocument"
	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// author writes one scene as one actor; every write expects the versions it
// read just before, so it never conflicts.
type author struct {
	t     *testing.T
	scope store.AccountScope
	doc   string
	actor creativedocument.Actor
}

func agentRun(run string) creativedocument.Actor {
	return creativedocument.Actor{Kind: "agent", RunID: &run}
}

func (a author) as(actor creativedocument.Actor) author { a.actor = actor; return a }

func (a author) send(expect []string, ops ...any) int64 {
	a.t.Helper()
	v := readScene(a.t, a.scope, a.doc)
	receipt, err := creativedocument.CommandAs(a.t.Context(), a.scope, a.actor, command(a.t, batch{DocumentID: a.doc, Expected: v.expect(expect...), Operations: ops}))
	if err != nil {
		a.t.Fatalf("%s %v: %v", a.actor.Kind, ops, err)
	}
	var out creativedocument.CommandResult
	if err := json.Unmarshal(receipt.Outcome.Response, &out); err != nil {
		a.t.Fatal(err)
	}
	return int64(out.Revision)
}

func named(o map[string]any, name string) map[string]any { o["name"] = name; return o }

func (a author) add(objects ...map[string]any) int64 {
	list := make([]any, len(objects))
	for i, o := range objects {
		list[i] = o
	}
	return a.send([]string{"objects"}, op{"op": "add_objects", "objects": list})
}

func (a author) move(id string, x float64) int64 {
	return a.send([]string{placement(id)}, op{"op": "update_objects", "updates": []any{map[string]any{"object_id": id, "transform": at(x, 0, 0)}}})
}

func (a author) resize(id string, width float64) int64 {
	return a.send([]string{content(id)}, op{"op": "update_objects", "updates": []any{map[string]any{"object_id": id, "primitive": map[string]any{"params": map[string]any{"width": width}}}}})
}

func (a author) remove(ids ...string) int64 {
	return a.send(append(units(ids...), "objects"), op{"op": "remove_objects", "object_ids": ids})
}

func (a author) restoreWhole(from int64) int64 {
	a.t.Helper()
	v := readScene(a.t, a.scope, a.doc)
	payload := map[string]any{"document_id": a.doc, "expected_revision": strconv.FormatInt(int64(v.Revision), 10), "operations": []any{op{"op": "restore", "revision": strconv.FormatInt(from, 10)}}}
	receipt, err := creativedocument.CommandAs(a.t.Context(), a.scope, a.actor, command(a.t, payload))
	if err != nil {
		a.t.Fatal(err)
	}
	var out creativedocument.CommandResult
	if err := json.Unmarshal(receipt.Outcome.Response, &out); err != nil {
		a.t.Fatal(err)
	}
	return int64(out.Revision)
}

// apply submits one restore batch exactly as the panel does.
func (a author) apply(b creativescene.RestoreBatch) error {
	a.t.Helper()
	payload := map[string]any{"document_id": a.doc, "expected": b.Expected,
		"operations": []any{op{"op": "restore_units", "from_revision": strconv.FormatInt(int64(b.FromRevision), 10), "units": b.Units}}}
	_, err := creativedocument.CommandAs(a.t.Context(), a.scope, creativedocument.Photographer, command(a.t, payload))
	return err
}

func runChanges(t *testing.T, scope store.AccountScope, doc, run string) creativescene.RunChangeSet {
	t.Helper()
	got, err := creativescene.RunChanges(t.Context(), scope, doc, run)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func positionX(t *testing.T, scope store.AccountScope, doc, id string) float64 {
	t.Helper()
	return readScene(t, scope, doc).object(t, id).Transform.Position[0]
}

func TestRunChanges(t *testing.T) {
	f := setup(t)
	const run = "ccrn_run"
	a, b := objectID(1), objectID(2)
	newScene := func(t *testing.T) (author, author) {
		me := author{t: t, scope: f.a, doc: createScene(t, f.a), actor: creativedocument.Photographer}
		return me, me.as(agentRun(run))
	}

	t.Run("nothing written", func(t *testing.T) {
		me, _ := newScene(t)
		got := runChanges(t, f.a, me.doc, run)
		if len(got.Revisions) != 0 || len(got.Entries) != 0 || len(got.UndoAll) != 0 {
			t.Fatalf("%+v", got)
		}
	})

	t.Run("added", func(t *testing.T) {
		me, agent := newScene(t)
		added := agent.add(named(box(a, nil, 10), "柱 A"))
		got := runChanges(t, f.a, me.doc, run)
		if len(got.Entries) != 1 || got.Revisions[0] != creativeops.Revision(added) {
			t.Fatalf("%+v", got)
		}
		e := got.Entries[0]
		if e.Change != creativescene.ChangeAdded || e.Name != "柱 A" || e.Blocked != nil || e.Restore == nil || e.Restore.FromRevision != 1 ||
			!slices.Equal(e.Restore.Units, units(a)) || e.Restore.Expected["objects"] == nil {
			t.Fatalf("entry %+v restore %+v", e, e.Restore)
		}
		if err := me.apply(got.UndoAll[0]); err != nil {
			t.Fatal(err)
		}
		if _, ok := byID(readScene(t, f.a, me.doc))[a]; ok {
			t.Fatal("undo kept the added object")
		}
	})

	t.Run("removed", func(t *testing.T) {
		me, agent := newScene(t)
		me.add(named(box(a, nil, 10), "长椅"))
		agent.remove(a)
		e := runChanges(t, f.a, me.doc, run).Entries[0]
		if e.Change != creativescene.ChangeRemoved || e.Name != "长椅" || e.Restore == nil {
			t.Fatalf("%+v", e)
		}
		if err := me.apply(*e.Restore); err != nil {
			t.Fatal(err)
		}
		if o, ok := byID(readScene(t, f.a, me.doc))[a]; !ok || o.Name != "长椅" {
			t.Fatal("restore did not bring the object back")
		}
	})

	t.Run("modified", func(t *testing.T) {
		me, agent := newScene(t)
		me.add(box(a, nil, 10))
		agent.move(a, 3)
		agent.move(a, 5)
		got := runChanges(t, f.a, me.doc, run)
		e := got.Entries[0]
		if len(got.Revisions) != 2 || e.Change != creativescene.ChangeModified || e.Restore == nil || e.Restore.FromRevision != 2 || !slices.Equal(e.Restore.Units, []string{placement(a)}) {
			t.Fatalf("%+v %+v", got, e.Restore)
		}
		if _, ok := e.Restore.Expected["objects"]; ok {
			t.Fatalf("a modification expects the object list: %+v", e.Restore.Expected)
		}
		if err := me.apply(*e.Restore); err != nil || positionX(t, f.a, me.doc, a) != 0 {
			t.Fatalf("restore %v, x=%v", err, positionX(t, f.a, me.doc, a))
		}
	})

	t.Run("changed by others since", func(t *testing.T) {
		me, agent := newScene(t)
		me.add(box(a, nil, 10))
		agent.move(a, 3)
		moved := me.move(a, 4)
		got := runChanges(t, f.a, me.doc, run)
		e := got.Entries[0]
		if e.Restore != nil || e.Blocked == nil || e.Blocked.Reason != creativescene.BlockedByOthers || e.Blocked.Revision != creativeops.Revision(moved) ||
			e.Blocked.ActorKind != "photographer" || len(got.UndoAll) != 0 {
			t.Fatalf("%+v %+v", e, e.Blocked)
		}
	})

	t.Run("restored whole since", func(t *testing.T) {
		me, agent := newScene(t)
		me.add(box(a, nil, 10))
		agent.move(a, 3)
		me.restoreWhole(2)
		e := runChanges(t, f.a, me.doc, run).Entries[0]
		if e.Restore != nil || e.Blocked == nil || e.Blocked.Reason != creativescene.BlockedByWholeRestore {
			t.Fatalf("%+v", e)
		}
	})

	t.Run("batches by source revision", func(t *testing.T) {
		me, agent := newScene(t)
		me.add(box(a, nil, 10), box(b, nil, 11)) // 2
		agent.move(a, 3)                         // 3
		me.move(b, 4)                            // 4
		agent.move(b, 5)                         // 5
		got := runChanges(t, f.a, me.doc, run)
		if len(got.Entries) != 2 || got.Entries[0].Restore == nil || got.Entries[1].Restore == nil || len(got.UndoAll) != 2 ||
			got.UndoAll[0].FromRevision != 2 || got.UndoAll[1].FromRevision != 4 {
			t.Fatalf("%+v", got)
		}
		for _, batch := range got.UndoAll {
			if err := me.apply(batch); err != nil {
				t.Fatal(err)
			}
		}
		// The run's moves are gone; the photographer's own move of b stays.
		if positionX(t, f.a, me.doc, a) != 0 || positionX(t, f.a, me.doc, b) != 4 {
			t.Fatalf("a at %v, b at %v", positionX(t, f.a, me.doc, a), positionX(t, f.a, me.doc, b))
		}
	})

	t.Run("one object written around another writer", func(t *testing.T) {
		me, agent := newScene(t)
		me.add(box(a, nil, 10)) // 2
		agent.resize(a, 2)      // 3
		moved := me.move(a, 4)  // 4
		agent.move(a, 5)        // 5
		e := runChanges(t, f.a, me.doc, run).Entries[0]
		if e.Restore != nil || e.Blocked == nil || e.Blocked.Revision != creativeops.Revision(moved) {
			t.Fatalf("%+v", e)
		}
	})

	t.Run("added and removed within the run", func(t *testing.T) {
		me, agent := newScene(t)
		agent.add(box(a, nil, 10))
		agent.remove(a)
		if got := runChanges(t, f.a, me.doc, run); len(got.Revisions) != 2 || len(got.Entries) != 0 || len(got.UndoAll) != 0 {
			t.Fatalf("%+v", got)
		}
	})

	t.Run("hierarchy only", func(t *testing.T) {
		me, agent := newScene(t)
		g := objectID(9)
		me.add(group(g, nil, 10), box(a, nil, 11))
		moved := agent.send([]string{"objects"}, op{"op": "reparent_objects", "moves": []any{map[string]any{"object_id": a, "parent_id": g, "order": 0}}})
		got := runChanges(t, f.a, me.doc, run)
		if len(got.Entries) != 1 || got.Entries[0].Target.Unit != "objects" || got.Entries[0].Blocked == nil ||
			got.Entries[0].Blocked.Reason != creativescene.BlockedHierarchy || got.Entries[0].Blocked.Revision != creativeops.Revision(moved) {
			t.Fatalf("%+v", got)
		}
	})

	t.Run("environment", func(t *testing.T) {
		me, agent := newScene(t)
		env := readScene(t, f.a, me.doc).Scene.Environment
		env.Ambient.Intensity = 0.2
		agent.send([]string{"environment"}, op{"op": "set_environment", "environment": env})
		e := runChanges(t, f.a, me.doc, run).Entries[0]
		if e.Target.Kind != "unit" || e.Target.Unit != "environment" || e.Name != "环境" || e.Restore == nil {
			t.Fatalf("%+v", e)
		}
	})

	t.Run("other accounts and types", func(t *testing.T) {
		me, agent := newScene(t)
		agent.add(box(a, nil, 10))
		if _, err := creativescene.RunChanges(t.Context(), f.b, me.doc, run); err == nil {
			t.Fatal("another account read the run's changes")
		}
		if _, err := creativescene.RunChanges(t.Context(), f.a, me.doc, ""); err == nil {
			t.Fatal("no run id accepted")
		}
	})
}

func changesSince(t *testing.T, scope store.AccountScope, doc string, runs ...string) *creativescene.ChangeSummary {
	t.Helper()
	var out *creativescene.ChangeSummary
	if err := scope.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		var err error
		out, err = creativescene.ChangesSinceInTx(t.Context(), tx, doc, runs)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestChangesSince(t *testing.T) {
	f := setup(t)
	a, b := objectID(1), objectID(2)
	newScene := func(t *testing.T) (author, author) {
		me := author{t: t, scope: f.a, doc: createScene(t, f.a), actor: creativedocument.Photographer}
		return me, me.as(agentRun("ccrn_1"))
	}
	for _, tc := range []struct {
		name  string
		write func(me, agent author)
		want  []string
	}{
		{"nothing since", func(me, agent author) { agent.add(named(box(a, nil, 10), "柱 A")) }, nil},
		{"manual edits", func(me, agent author) {
			agent.add(named(box(a, nil, 10), "柱 A"))
			me.move(a, 2)
			me.resize(a, 3)
			me.add(named(box(b, nil, 11), "灯笼"))
		}, []string{"摄影师改了「柱 A」(" + a + ")的摆放和内容", "摄影师新增了「灯笼」(" + b + ")"}},
		{"deleted", func(me, agent author) {
			agent.add(named(box(a, nil, 10), "柱 A"))
			me.remove(a)
		}, []string{"摄影师删除了「柱 A」(" + a + ")"}},
		{"whole restore", func(me, agent author) {
			agent.add(named(box(a, nil, 10), "柱 A"))
			me.restoreWhole(1)
		}, []string{"摄影师把整个场景恢复到第 1 版"}},
		{"unit restore", func(me, agent author) {
			me.add(named(box(a, nil, 10), "柱 A"))
			agent.move(a, 3)
			me.send([]string{placement(a)}, op{"op": "restore_units", "from_revision": "2", "units": []string{placement(a)}})
		}, []string{"摄影师把「柱 A」(" + a + ")恢复到第 2 版"}},
		{"the conversation's own runs are not news", func(me, agent author) {
			agent.add(named(box(a, nil, 10), "柱 A"))
			agent.as(agentRun("ccrn_2")).move(a, 4)
		}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			me, agent := newScene(t)
			tc.write(me, agent)
			got := changesSince(t, f.a, me.doc, "ccrn_1", "ccrn_2")
			if got == nil {
				t.Fatal("no summary for a scene the conversation wrote")
			}
			if lines := got.Lines; len(lines) > 0 || tc.want != nil {
				if !slices.Equal(lines, tc.want) {
					t.Fatalf("lines %q, want %q", lines, tc.want)
				}
			}
		})
	}

	t.Run("never written", func(t *testing.T) {
		me, _ := newScene(t)
		me.add(box(a, nil, 10))
		if got := changesSince(t, f.a, me.doc, "ccrn_1"); got != nil {
			t.Fatalf("%+v", got)
		}
	})

	t.Run("long lists are cut", func(t *testing.T) {
		me, agent := newScene(t)
		agent.add(box(a, nil, 10))
		for i := range 35 {
			me.add(named(box(objectID(100+i), nil, i+11), "道具"))
		}
		got := changesSince(t, f.a, me.doc, "ccrn_1")
		if len(got.Lines) != 30 || got.More != 5 || !strings.Contains(got.Lines[0], "新增") {
			t.Fatalf("%d lines, more %d", len(got.Lines), got.More)
		}
	})
}
