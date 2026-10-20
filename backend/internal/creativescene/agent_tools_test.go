package creativescene_test

import (
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativedocument"
	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func raws(t *testing.T, ops ...any) []json.RawMessage {
	t.Helper()
	out := make([]json.RawMessage, len(ops))
	for i, o := range ops {
		raw, err := json.Marshal(o)
		if err != nil {
			t.Fatal(err)
		}
		out[i] = raw
	}
	return out
}

func agentEdit(t *testing.T, scope store.AccountScope, in creativescene.EditInput) (any, error) {
	t.Helper()
	var out any
	err := scope.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		var err error
		out, err = creativescene.AgentEditInTx(t.Context(), tx, in)
		return err
	})
	return out, err
}

func moveOp(id string, x float64) op {
	return op{"op": "update_objects", "updates": []any{map[string]any{"object_id": id, "transform": at(x, 0, 0)}}}
}

// B1: the baseline moves only to where the model was told the scene is. A
// write that succeeded in between is not a new baseline, or it would carry
// over the photographer's move it never saw.
func TestAgentEditChecksTheBaseline(t *testing.T) {
	f := setup(t)
	const run = "ccrn_agent"
	p, q := objectID(1), objectID(2)
	me := author{t: t, scope: f.a, doc: createScene(t, f.a), actor: creativedocument.Photographer}
	base := me.add(named(box(p, nil, 10), "柱 P"))
	moved := me.move(p, 3)

	added, err := agentEdit(t, f.a, creativescene.EditInput{DocumentID: me.doc, RunID: run, BasedOn: base, Operations: raws(t, op{"op": "add_objects", "objects": []any{named(box(q, nil, 11), "柱 Q")}})})
	if err != nil {
		t.Fatal(err)
	}
	written, ok := added.(creativescene.EditResult)
	if !ok || int64(written.WrittenRevision) != moved+1 || !slices.Equal(written.ChangedObjects, []string{q}) {
		t.Fatalf("add result %#v", added)
	}

	// Still at the baseline it read, the assistant moves P.
	got, err := agentEdit(t, f.a, creativescene.EditInput{DocumentID: me.doc, RunID: run, BasedOn: base, Operations: raws(t, moveOp(p, 9))})
	if err != nil {
		t.Fatal(err)
	}
	conflict, ok := got.(*creativescene.WriteConflict)
	if !ok || conflict.Status != "conflict" || int64(conflict.Revision) != moved+1 || len(conflict.Conflicts) != 1 {
		t.Fatalf("expected a conflict, got %#v", got)
	}
	c := conflict.Conflicts[0]
	if c.ObjectID != p || c.Name != "柱 P" || c.Part != "摆放" || c.ChangedBy != "创作者" {
		t.Fatalf("conflict entry %+v", c)
	}
	if view, ok := c.Current.(creativescene.ObjectView); !ok || view.Transform.Position[0] != 3 {
		t.Fatalf("current state %#v", c.Current)
	}
	if len(conflict.ChangesSince.Lines) != 1 || !strings.Contains(conflict.ChangesSince.Lines[0], "创作者改了「柱 P」") {
		t.Fatalf("changes since %+v", conflict.ChangesSince)
	}
	if positionX(t, f.a, me.doc, p) != 3 {
		t.Fatal("the conflicting move was written")
	}

	// Its own writes never conflict, however old the baseline.
	if got, err = agentEdit(t, f.a, creativescene.EditInput{DocumentID: me.doc, RunID: run, BasedOn: base, Operations: raws(t, moveOp(q, 5))}); err != nil {
		t.Fatal(err)
	} else if _, ok := got.(creativescene.EditResult); !ok {
		t.Fatalf("moving its own object: %#v", got)
	}
	// At the baseline the conflict gave, the move goes through.
	if got, err = agentEdit(t, f.a, creativescene.EditInput{DocumentID: me.doc, RunID: run, BasedOn: int64(conflict.Revision), Operations: raws(t, moveOp(p, 9))}); err != nil {
		t.Fatal(err)
	} else if _, ok := got.(creativescene.EditResult); !ok || positionX(t, f.a, me.doc, p) != 9 {
		t.Fatalf("move at the new baseline: %#v", got)
	}

	// A whole restore by someone else touches everything.
	restored := me.restoreWhole(base)
	got, err = agentEdit(t, f.a, creativescene.EditInput{DocumentID: me.doc, RunID: run, BasedOn: restored - 1, Operations: raws(t, op{"op": "set_render", "render": map[string]any{"mode": "lit"}})})
	if err != nil {
		t.Fatal(err)
	}
	if conflict, ok := got.(*creativescene.WriteConflict); !ok || conflict.Conflicts[0].Unit != "render" || conflict.Conflicts[0].ChangedBy != "创作者" {
		t.Fatalf("after a whole restore: %#v", got)
	}
}

func TestAgentEditRefusals(t *testing.T) {
	f := setup(t)
	me := author{t: t, scope: f.a, doc: createScene(t, f.a), actor: creativedocument.Photographer}
	base := me.add(box(objectID(1), nil, 10))
	for _, tc := range []struct {
		name string
		base int64
		ops  []any
	}{
		{"commit_model", base, []any{op{"op": "commit_model"}}},
		{"reparent without moves", base, []any{op{"op": "reparent_objects", "moves": []any{}}}},
		{"whole restore", base, []any{op{"op": "restore", "revision": "1"}}},
		{"restore_units", base, []any{op{"op": "restore_units", "from_revision": "1", "units": []string{"render"}}}},
		{"revert_run with others", base, []any{op{"op": "revert_run", "run_id": "previous"}, moveOp(objectID(1), 1)}},
		{"baseline ahead of the scene", base + 1, []any{moveOp(objectID(1), 1)}},
		{"no baseline", 0, []any{moveOp(objectID(1), 1)}},
		{"unknown object", base, []any{moveOp(objectID(7), 1)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := agentEdit(t, f.a, creativescene.EditInput{DocumentID: me.doc, RunID: "ccrn_agent", BasedOn: tc.base, Operations: raws(t, tc.ops...)})
			wantErr(t, err, creativeops.ErrValidation)
		})
	}
	if _, err := agentEdit(t, f.b, creativescene.EditInput{DocumentID: me.doc, RunID: "ccrn_agent", BasedOn: base, Operations: raws(t, moveOp(objectID(1), 1))}); !errors.Is(err, creativedocument.ErrNotFound) {
		t.Fatalf("another account: %v", err)
	}
}

// TestAgentReparentKeepsWorldPositions: the assistant names only the new
// parent; the local transform comes out as three.js computes it
// (parent.invert().multiply(world).decompose()), and orders and group
// defaults are filled in.
func TestAgentReparentKeepsWorldPositions(t *testing.T) {
	f := setup(t)
	const run = "ccrn_agent"
	p, x, a, b, g := objectID(1), objectID(2), objectID(3), objectID(4), objectID(5)
	me := author{t: t, scope: f.a, doc: createScene(t, f.a), actor: creativedocument.Photographer}
	parent := group(p, nil, 10)
	parent["transform"] = map[string]any{"position": []float64{2, 1, -3}, "rotation": []float64{0, 0.3894183423086505, 0, 0.9210609940028851}, "scale": []float64{2, 2, 2}}
	child := box(x, nil, 11)
	child["transform"] = map[string]any{"position": []float64{5, 1, 0}, "rotation": []float64{0.06407134770607115, 0.09115754934299071, 0.15343930202422257, 0.9818561728660807}, "scale": []float64{1, 1, 1}}
	pa, pb := box(a, nil, 12), box(b, nil, 13)
	pa["transform"], pb["transform"] = at(1, 0, 2), at(3, 0, 2)
	base := me.add(parent, child, pa, pb)
	edit := func(t *testing.T, base int64, ops ...any) (any, error) {
		return agentEdit(t, f.a, creativescene.EditInput{DocumentID: me.doc, RunID: run, BasedOn: base, Operations: raws(t, ops...)})
	}

	got, err := edit(t, base, op{"op": "reparent_objects", "moves": []any{map[string]any{"object_id": x, "parent_id": p}}})
	if err != nil {
		t.Fatal(err)
	}
	res, ok := got.(creativescene.EditResult)
	if !ok {
		t.Fatalf("reparent result %#v", got)
	}
	o := readScene(t, f.a, me.doc).object(t, x)
	if o.ParentID == nil || *o.ParentID != p || o.Order != 0 ||
		!closeTo(o.Transform.Position, []float64{-0.0309740723285361, 0, 2.12109420037003}) ||
		!closeTo(o.Transform.Rotation, []float64{-0.000738459434010749, -0.298391140214303, 0.166277514054719, 0.939847844301968}) ||
		!closeTo(o.Transform.Scale, []float64{0.5, 0.5, 0.5}) {
		t.Fatalf("reparented %+v", o)
	}

	// A new group without a transform sits in the middle of what goes in.
	got, err = edit(t, res.WrittenRevision, op{"op": "reparent_objects",
		"create_groups": []any{map[string]any{"id": g, "name": "石台组", "parent_id": nil}},
		"moves":         []any{map[string]any{"object_id": a, "parent_id": g}, map[string]any{"object_id": b, "parent_id": g}}})
	if err != nil {
		t.Fatal(err)
	}
	v := readScene(t, f.a, me.doc)
	grp, oa, ob := v.object(t, g), v.object(t, a), v.object(t, b)
	if grp.Kind != "group" || !grp.Visible || grp.Order != 11 || !closeTo(grp.Transform.Position, []float64{2, 0, 2}) ||
		oa.Order != 0 || ob.Order != 1 || !closeTo(oa.Transform.Position, []float64{-1, 0, 0}) || !closeTo(ob.Transform.Position, []float64{1, 0, 0}) {
		t.Fatalf("group %+v, a %+v, b %+v", grp, oa, ob)
	}
	if r, ok := got.(creativescene.EditResult); !ok || r.ChangedCount != 3 {
		t.Fatalf("group result %#v", got)
	}

	tiny := group(objectID(6), nil, 20)
	tiny["transform"] = map[string]any{"position": []float64{0, 0, 0}, "rotation": []float64{0, 0, 0, 1}, "scale": []float64{0.001, 0.001, 0.001}}
	base = me.add(tiny)
	for name, ops := range map[string][]any{
		"transform written": {op{"op": "reparent_objects", "moves": []any{map[string]any{"object_id": x, "parent_id": nil, "transform": at(0, 0, 0)}}}},
		"parent moved too":  {moveOp(p, 4), op{"op": "reparent_objects", "moves": []any{map[string]any{"object_id": a, "parent_id": p}}}},
		"object moved too": {op{"op": "update_objects", "updates": []any{map[string]any{"object_id": a, "look_at": []float64{0, 0, 9}}}},
			op{"op": "reparent_objects", "moves": []any{map[string]any{"object_id": a, "parent_id": nil}}}},
		"scale out of range": {op{"op": "reparent_objects", "moves": []any{map[string]any{"object_id": p, "parent_id": objectID(6)}}}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := edit(t, base, ops...)
			wantErr(t, err, creativeops.ErrValidation)
		})
	}
	// Each changes the object list, which a revision changes once: refused up
	// front with the way to write it, not as a parent that is not there.
	_, err = edit(t, base,
		op{"op": "reparent_objects", "create_groups": []any{map[string]any{"id": objectID(7)}}, "moves": []any{map[string]any{"object_id": a, "parent_id": objectID(7)}}},
		op{"op": "reparent_objects", "moves": []any{map[string]any{"object_id": b, "parent_id": objectID(7)}}})
	if !errors.Is(err, creativeops.ErrValidation) || !strings.Contains(err.Error(), "写在同一个 reparent_objects 里") {
		t.Fatalf("two reparent_objects: %v", err)
	}
}

// revert_run puts back what a run changed and skips what someone else changed
// since, as 撤销本轮 does.
func TestAgentRevertTakesBackARun(t *testing.T) {
	f := setup(t)
	const earlier, current = "ccrn_earlier", "ccrn_current"
	a, b, c := objectID(1), objectID(2), objectID(3)
	me := author{t: t, scope: f.a, doc: createScene(t, f.a), actor: creativedocument.Photographer}
	me.add(named(box(c, nil, 10), "门"))
	bot := me.as(agentRun(earlier))
	bot.add(named(box(a, nil, 11), "柱 A"), named(box(b, nil, 12), "柱 B"))
	bot.move(c, 4)
	me.move(b, 7)
	revert := func(t *testing.T, ops ...any) creativescene.RevertResult {
		t.Helper()
		got, err := agentEdit(t, f.a, creativescene.EditInput{DocumentID: me.doc, RunID: current, Operations: raws(t, ops...), ConversationRuns: []string{earlier, current}})
		if err != nil {
			t.Fatal(err)
		}
		r, ok := got.(creativescene.RevertResult)
		if !ok {
			t.Fatalf("revert result %#v", got)
		}
		return r
	}

	only := revert(t, op{"op": "revert_run", "run_id": "previous", "object_ids": []string{a, objectID(9)}})
	if only.RunID != earlier || len(only.Restored) != 1 || only.Restored[0].ObjectID != a || only.Restored[0].Change != creativescene.ChangeAdded ||
		len(only.Skipped) != 1 || only.Skipped[0].Reason != "这次运行没有改过它" {
		t.Fatalf("partial revert %+v", only)
	}
	if readScene(t, f.a, me.doc).has(a) || !readScene(t, f.a, me.doc).has(b) {
		t.Fatal("partial revert touched more than A")
	}

	rest := revert(t, op{"op": "revert_run", "run_id": earlier})
	if len(rest.Restored) != 1 || rest.Restored[0].ObjectID != c || len(rest.Skipped) != 1 || rest.Skipped[0].ObjectID != b || rest.Skipped[0].Reason != "之后被创作者改过" || rest.WrittenRevision == nil {
		t.Fatalf("revert %+v", rest)
	}
	if positionX(t, f.a, me.doc, c) != 0 || positionX(t, f.a, me.doc, b) != 7 {
		t.Fatal("revert did not put C back or overwrote the photographer's move of B")
	}
	var actor, runID string
	if err := f.db.QueryRow(`SELECT actor_kind, run_id FROM creative_document_revisions WHERE document_id=$1 AND revision=$2`, me.doc, int64(*rest.WrittenRevision)).Scan(&actor, &runID); err != nil || actor != "agent" || runID != current {
		t.Fatalf("revert written by %s %s %v", actor, runID, err)
	}

	for _, runID := range []string{"ccrn_stranger", ""} {
		if _, err := agentEdit(t, f.a, creativescene.EditInput{DocumentID: me.doc, RunID: current, Operations: raws(t, op{"op": "revert_run", "run_id": runID}), ConversationRuns: []string{earlier, current}}); !errors.Is(err, creativeops.ErrValidation) {
			t.Fatalf("revert of %q: %v", runID, err)
		}
	}
}

func agentRead(t *testing.T, scope store.AccountScope, in creativescene.ReadInput) (creativescene.SceneRead, error) {
	t.Helper()
	var out creativescene.SceneRead
	err := scope.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		var err error
		out, err = creativescene.AgentReadInTx(t.Context(), tx, in)
		return err
	})
	return out, err
}

func TestAgentRead(t *testing.T) {
	f := setup(t)
	doc := createScene(t, f.a)
	pillar := newPillar(t, f, doc, objectID(1), 10)
	got, err := agentRead(t, f.a, creativescene.ReadInput{DocumentID: doc, IncludeCode: []string{pillar.ContentRevisionID}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Brief || got.ObjectCount != 2 || len(got.Objects) != 2 || got.Code[pillar.ContentRevisionID] != pillarCode || got.ChangesSinceLastWrite != nil {
		t.Fatalf("read %+v", got)
	}
	var placed creativescene.ObjectView
	for _, o := range got.Objects {
		if o.ID == objectID(1) {
			placed = o
		}
	}
	if m := placed.Model; m == nil || !m.Usable || m.ContentRevisionID != pillar.ContentRevisionID || m.Triangles == 0 || m.BoundsM == nil || m.Params["height"] != 2.0 || m.ParamsSchema["height"].Label == "" {
		t.Fatalf("placed model %+v", placed.Model)
	}
	if _, err := agentRead(t, f.a, creativescene.ReadInput{DocumentID: doc, IncludeCode: []string{"ccrv_elsewhere"}}); !errors.Is(err, creativeops.ErrValidation) {
		t.Fatalf("code of a model not in the scene: %v", err)
	}

	many := make([]map[string]any, 300)
	for i := range many {
		many[i] = box(objectID(100+i), nil, 100+i)
	}
	addObjects(t, f.a, doc, many...)
	got, err = agentRead(t, f.a, creativescene.ReadInput{DocumentID: doc})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Brief || len(got.Objects) != 302 || got.Objects[5].Transform != nil || got.Objects[5].Position == nil {
		t.Fatalf("brief read: brief=%v objects=%d %+v", got.Brief, len(got.Objects), got.Objects[5])
	}
	got, err = agentRead(t, f.a, creativescene.ReadInput{DocumentID: doc, ObjectIDs: []string{objectID(101), objectID(9999)}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Brief || len(got.Objects) != 1 || got.Objects[0].Transform == nil || !slices.Equal(got.Missing, []string{objectID(9999)}) {
		t.Fatalf("narrowed read %+v", got)
	}

	// Within a byte budget: requested code first, then objects in brief, then
	// as many as fit, and the result says what it left out.
	for _, budget := range []int{4000, 20000} {
		got, err = agentRead(t, f.a, creativescene.ReadInput{DocumentID: doc, IncludeCode: []string{pillar.ContentRevisionID}, MaxBytes: budget})
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(got)
		if len(raw) > budget || !got.Brief || got.OmittedObjects == 0 || len(got.Objects)+got.OmittedObjects != 302 || got.Code[pillar.ContentRevisionID] != pillarCode || got.Note == "" {
			t.Fatalf("budget %d: %d bytes, brief=%v listed=%d omitted=%d code=%d", budget, len(raw), got.Brief, len(got.Objects), got.OmittedObjects, len(got.Code))
		}
	}
	got, err = agentRead(t, f.a, creativescene.ReadInput{DocumentID: doc, IncludeCode: []string{pillar.ContentRevisionID}, MaxBytes: 600})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Code) != 0 || !slices.Equal(got.CodeOmitted, []string{pillar.ContentRevisionID}) {
		t.Fatalf("code past the budget: %+v", got)
	}
}

func bakeJobInTx(t *testing.T, scope store.AccountScope, in creativescene.BakeInput) (creativescene.Job, *creativescene.WriteConflict, error) {
	t.Helper()
	var job creativescene.Job
	var conflict *creativescene.WriteConflict
	err := scope.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		var err error
		job, conflict, err = creativescene.CreateBakeJobInTx(t.Context(), tx, in)
		return err
	})
	return job, conflict, err
}

func bakeRequest(t *testing.T, job creativescene.Job) creativescene.BakeRequest {
	t.Helper()
	var req creativescene.BakeRequest
	if err := json.Unmarshal(job.Request, &req); err != nil {
		t.Fatal(err)
	}
	return req
}

func TestCreateBakeJob(t *testing.T) {
	f := setup(t)
	const run = "ccrn_agent"
	doc := createScene(t, f.a)
	lone := newPillar(t, f, doc, objectID(1), 10)
	shared := newPillar(t, f, doc, objectID(2), 11)
	addObjects(t, f.a, doc, instance(objectID(3), 12, map[string]any{"content_revision_id": shared.ContentRevisionID}), group(objectID(4), nil, 13))
	base := int64(readScene(t, f.a, doc).Revision)
	in := func(step string) creativescene.BakeInput {
		return creativescene.BakeInput{DocumentID: doc, RunID: run, ToolCallID: step, BasedOn: base, Code: pillarCode, Params: map[string]any{"height": 3}}
	}

	add := in("step-add")
	add.Add = &creativescene.AddTarget{Name: "拱门", Transform: creativescene.Transform{Position: []float64{1, 0, 0}, Rotation: []float64{0, 0, 0, 1}, Scale: []float64{1, 1, 1}}, ParentID: ptr(objectID(4))}
	job, _, err := bakeJobInTx(t, f.a, add)
	if err != nil {
		t.Fatal(err)
	}
	req := bakeRequest(t, job)
	var object struct {
		ID       string  `json:"id"`
		Name     string  `json:"name"`
		ParentID *string `json:"parent_id"`
	}
	if err := json.Unmarshal(req.Target.Object, &object); err != nil || req.Target.Op != "add" || !strings.HasPrefix(object.ID, "sobj_") || object.Name != "拱门" || *object.ParentID != objectID(4) || len(req.Expected) != 0 || req.Code != pillarCode {
		t.Fatalf("add job %+v %+v %v", req, object, err)
	}
	if again, _, err := bakeJobInTx(t, f.a, add); err != nil || again.ID != job.ID {
		t.Fatalf("the same call made another job: %v", err)
	}

	for _, tc := range []struct {
		name, object, scope string
		base                string
		ids                 []string
	}{
		{"unshared model gets a new revision", objectID(1), "this", lone.ContentID, nil},
		{"one of shared instances forks", objectID(2), "this", "", nil},
		{"all shared instances", objectID(2), "all_instances", shared.ContentID, []string{objectID(2), objectID(3)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			replace := in("step-" + tc.name)
			replace.Replace = &creativescene.ReplaceTarget{ObjectID: tc.object, Scope: tc.scope}
			job, conflict, err := bakeJobInTx(t, f.a, replace)
			if err != nil || conflict != nil {
				t.Fatalf("%v %+v", err, conflict)
			}
			req := bakeRequest(t, job)
			if req.Target.Op != "replace" || req.Target.BaseContentID != tc.base || !slices.Equal(req.Target.ObjectIDs, tc.ids) {
				t.Fatalf("target %+v", req.Target)
			}
			units := []string{content(tc.object)}
			for _, id := range tc.ids[min(1, len(tc.ids)):] {
				units = append(units, content(id))
			}
			v := readScene(t, f.a, doc)
			for _, u := range units {
				if req.Expected[u] == nil || jsonRevision(*req.Expected[u]) != v.UnitRevs[u] {
					t.Fatalf("expected %s = %v, scene has %s", u, req.Expected[u], v.UnitRevs[u])
				}
			}
			if len(req.Expected) != len(units) {
				t.Fatalf("expected %v", req.Expected)
			}
		})
	}

	// Re-baked with other params, the code is the base revision's.
	params := in("step-params")
	params.Code, params.BaseContentRevisionID = "", lone.ContentRevisionID
	params.Replace = &creativescene.ReplaceTarget{ObjectID: objectID(1)}
	if job, _, err := bakeJobInTx(t, f.a, params); err != nil || bakeRequest(t, job).Code != pillarCode {
		t.Fatalf("params only: %v", err)
	}

	// The photographer swaps the shared model after the baseline: replacing it
	// conflicts and makes no job.
	mustSend(t, f.a, batch{DocumentID: doc, Expected: readScene(t, f.a, doc).expect(content(objectID(3))), Operations: []any{
		op{"op": "set_instance_model", "object_id": objectID(3), "model": map[string]any{"content_revision_id": lone.ContentRevisionID}},
	}})
	late := in("step-late")
	late.Replace = &creativescene.ReplaceTarget{ObjectID: objectID(3)}
	if job, conflict, err := bakeJobInTx(t, f.a, late); err != nil || conflict == nil || job.ID != "" || conflict.Conflicts[0].ObjectID != objectID(3) || conflict.Conflicts[0].Part != "内容" {
		t.Fatalf("late replace %+v %+v %v", job, conflict, err)
	}

	for _, tc := range []struct {
		name string
		edit func(*creativescene.BakeInput)
	}{
		{"code and base", func(b *creativescene.BakeInput) { b.BaseContentRevisionID = lone.ContentRevisionID; b.Add = add.Add }},
		{"no target", func(*creativescene.BakeInput) {}},
		{"two targets", func(b *creativescene.BakeInput) {
			b.Add, b.Replace = add.Add, &creativescene.ReplaceTarget{ObjectID: objectID(1)}
		}},
		{"replace a group", func(b *creativescene.BakeInput) { b.Replace = &creativescene.ReplaceTarget{ObjectID: objectID(4)} }},
		{"parent not a group", func(b *creativescene.BakeInput) {
			target := *add.Add
			target.ParentID = ptr(objectID(1))
			b.Add = &target
		}},
		{"bad transform", func(b *creativescene.BakeInput) {
			b.Add = &creativescene.AddTarget{Transform: creativescene.Transform{Position: []float64{0, 0, 0}, Rotation: []float64{0, 0, 0, 2}, Scale: []float64{1, 1, 1}}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := in("step-refused-" + tc.name)
			tc.edit(&b)
			_, _, err := bakeJobInTx(t, f.a, b)
			wantErr(t, err, creativeops.ErrValidation)
		})
	}
}

func retryInTx(t *testing.T, scope store.AccountScope, in creativescene.RetryInput) (*creativescene.BakeResult, *creativescene.WriteConflict, error) {
	t.Helper()
	var res *creativescene.BakeResult
	var conflict *creativescene.WriteConflict
	err := scope.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		var err error
		res, conflict, err = creativescene.RetryBakeInTx(t.Context(), tx, in)
		return err
	})
	return res, conflict, err
}

// A bake that conflicted is written again at the new baseline from what it
// baked; a second retry of it finds the bake used up.
func TestRetryBakeWritesTheConflictedBake(t *testing.T) {
	f := setup(t)
	creativescene.SetJobWaiterForTest(t, &fakeWaiter{})
	const run = "ccrn_agent"
	doc := createScene(t, f.a)
	placed := newPillar(t, f, doc, objectID(1), 10)
	base := int64(readScene(t, f.a, doc).Revision)
	job, _, err := bakeJobInTx(t, f.a, creativescene.BakeInput{DocumentID: doc, RunID: run, ToolCallID: "step-1", BasedOn: base, Code: pillarCode,
		Params: map[string]any{"height": 3}, Replace: &creativescene.ReplaceTarget{ObjectID: objectID(1)}})
	if err != nil {
		t.Fatal(err)
	}
	claimed := claimOne(t, f.a)
	other := newPillar(t, f, doc, objectID(2), 11)
	mustSend(t, f.a, batch{DocumentID: doc, Expected: readScene(t, f.a, doc).expect(content(objectID(1))), Operations: []any{
		op{"op": "set_instance_model", "object_id": objectID(1), "model": map[string]any{"content_revision_id": other.ContentRevisionID}},
	}})
	resp, err := completeJob(t, f.a, uuid.NewString(), map[string]any{"job_id": job.ID, "claim_token": claimed.ClaimToken, "outcome": "succeeded", "bake": bakeOutput(stagePair(t, f, f.a), basicManifest(t))})
	if err != nil || resp.Result.Failure == nil || resp.Result.Failure.Code != "conflict" {
		t.Fatalf("bake %+v %v", resp, err)
	}

	var described creativescene.WriteConflict
	if err := f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		var err error
		failed, err := creativescene.ReadRunJob(t.Context(), tx, run, "step-1")
		if err != nil {
			return err
		}
		described, err = creativescene.JobConflictInTx(t.Context(), tx, failed, *resp.Result.Failure, base)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	head := int64(readScene(t, f.a, doc).Revision)
	if described.RetryJobID != job.ID || int64(described.Revision) != head || len(described.Conflicts) != 1 || described.Conflicts[0].ChangedBy != "创作者" || described.Conflicts[0].ObjectID != objectID(1) {
		t.Fatalf("described %+v", described)
	}

	retry := creativescene.RetryInput{DocumentID: doc, RunID: "ccrn_next", ConversationRuns: []string{run, "ccrn_next"}, RetryJobID: job.ID, BasedOn: head,
		Replace: &creativescene.ReplaceTarget{ObjectID: objectID(1)}}
	res, conflict, err := retryInTx(t, f.a, retry)
	if err != nil || conflict != nil || res == nil || res.ContentRevisionID == "" || !slices.Equal(res.ObjectIDs, []string{objectID(1)}) {
		t.Fatalf("retry %+v %+v %v", res, conflict, err)
	}
	if got := readScene(t, f.a, doc).object(t, objectID(1)).Model.ContentRevisionID; got != res.ContentRevisionID || got == placed.ContentRevisionID {
		t.Fatalf("instance shows %s", got)
	}
	if _, _, err := retryInTx(t, f.a, retry); !errors.Is(err, creativeops.ErrValidation) || !strings.Contains(err.Error(), "已被使用") {
		t.Fatalf("second retry: %v", err)
	}
	stranger := retry
	stranger.ConversationRuns, stranger.RunID = []string{"ccrn_other"}, "ccrn_other"
	if _, _, err := retryInTx(t, f.a, stranger); !errors.Is(err, creativeops.ErrValidation) || !strings.Contains(err.Error(), "本会话") {
		t.Fatalf("retry from another conversation: %v", err)
	}

	var baked creativescene.BakedModel
	if err := f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		var err error
		baked, err = creativescene.BakedModelInTx(t.Context(), tx, res.ContentRevisionID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	m := basicManifest(t)
	if baked.Triangles != m.Triangles || baked.SizeM[1] <= 0 || len(baked.Slots) != len(m.Slots) {
		t.Fatalf("baked %+v", baked)
	}
}

func TestCreateRenderJob(t *testing.T) {
	f := setup(t)
	doc := createScene(t, f.a)
	cam := object(objectID(1), "camera", nil, 10)
	cam["camera"] = map[string]any{"focal_mm": 35, "sensor_mm": []float64{36, 24}, "aspect": "3:2", "near": 0.1, "far": 100, "label": "A"}
	addObjects(t, f.a, doc, cam, box(objectID(2), nil, 11))
	render := func(in creativescene.RenderInput) (creativescene.Job, error) {
		in.DocumentID, in.RunID = doc, "ccrn_agent"
		if in.ToolCallID == "" {
			in.ToolCallID = "step-" + uuid.NewString()
		}
		var job creativescene.Job
		err := f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
			var err error
			job, err = creativescene.CreateRenderJobInTx(t.Context(), tx, in)
			return err
		})
		return job, err
	}
	for _, ok := range []creativescene.RenderInput{
		{Preset: "overview"},
		{CameraObjectID: objectID(1)},
		{Viewpoint: &creativescene.Viewpoint{Position: [3]float64{5, 2, 5}, Target: [3]float64{0, 1, 0}, FocalMM: 35}},
	} {
		job, err := render(ok)
		if err != nil || job.Kind != creativescene.KindRenderView {
			t.Fatalf("%+v: %v", ok, err)
		}
		var req creativescene.RenderRequest
		if err := json.Unmarshal(job.Request, &req); err != nil || req.Mode != "clay" {
			t.Fatalf("request %s", job.Request)
		}
	}
	for _, bad := range []creativescene.RenderInput{
		{},
		{Preset: "overview", CameraObjectID: objectID(1)},
		{Preset: "fisheye"},
		{CameraObjectID: objectID(2)},
		{Viewpoint: &creativescene.Viewpoint{Position: [3]float64{1, 1, 1}, Target: [3]float64{1, 1, 1}, FocalMM: 35}},
		{Viewpoint: &creativescene.Viewpoint{Position: [3]float64{5, 2, 5}, FocalMM: 2}},
	} {
		if _, err := render(bad); !errors.Is(err, creativeops.ErrValidation) {
			t.Fatalf("%+v: %v", bad, err)
		}
	}
}

// What the assistant reads (agent.go) names the person neutrally, like the
// assistant's own text: the creator may be a photographer today and anything
// else tomorrow (owner 2026-10-14).
func TestAgentTextNamesNoProfession(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "agent.go", nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	ast.Inspect(file, func(n ast.Node) bool {
		if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING && strings.Contains(lit.Value, "摄影师") {
			t.Errorf("%s names a profession", fset.Position(lit.Pos()))
		}
		return true
	})
}

// The assistant's material slots merge by slot, null clearing one, and are
// checked against the object and the catalog; the sky cannot be an HDRI
// (design「助手：材质、灯光与环境」).
func TestAgentMaterialSlots(t *testing.T) {
	f := setup(t)
	const run = "ccrn_agent"
	b, m, l := objectID(1), objectID(2), objectID(3)
	me := author{t: t, scope: f.a, doc: createScene(t, f.a), actor: creativedocument.Photographer}
	slot := func(preset string, params map[string]any) map[string]any {
		if params == nil {
			params = map[string]any{}
		}
		return map[string]any{"preset": preset, "params": params}
	}
	pillar := instance(m, 11, map[string]any{"ref": "pillar"})
	// The command checks slots by shape only: a slot the model lost and a preset this build lacks.
	pillar["material_slots"] = map[string]any{"stone": slot("marble@1", nil), "legacy": slot("old@1", nil)}
	lamp := object(l, "light", nil, 12)
	lamp["light"] = map[string]any{"type": "point"}
	me.send([]string{"objects"}, commit(t, "pillar", stagePair(t, f, f.a), pillarGenerator(2), basicManifest(t), nil), op{"op": "add_objects", "objects": []any{box(b, nil, 10), pillar, lamp}})
	edit := func(ops ...any) error {
		_, err := agentEdit(t, f.a, creativescene.EditInput{DocumentID: me.doc, RunID: run, BasedOn: int64(readScene(t, f.a, me.doc).Revision), Operations: raws(t, ops...)})
		return err
	}
	slots := func(id string, s map[string]any) op {
		return op{"op": "update_objects", "updates": []any{map[string]any{"object_id": id, "material_slots": s}}}
	}
	stored := func(id string) map[string]creativescene.MaterialSlot {
		return readScene(t, f.a, me.doc).object(t, id).MaterialSlots
	}

	if err := edit(slots(m, map[string]any{"stone": slot("wood@1", map[string]any{"scale": 1.0})})); err != nil {
		t.Fatal(err)
	}
	if got := stored(m); len(got) != 2 || got["stone"].Preset != "wood@1" || got["stone"].Params["scale"] != 1.0 || got["legacy"].Preset != "old@1" {
		t.Fatalf("one slot written, the other kept: %+v", got)
	}
	if err := edit(slots(m, map[string]any{"legacy": nil}), slots(b, map[string]any{"main": slot("stone_brick@1", map[string]any{"brick_size": 0.5, "mortar_color": "#a0a0a0"})})); err != nil {
		t.Fatal(err)
	}
	if got := stored(m); len(got) != 1 || got["stone"].Preset != "wood@1" {
		t.Fatalf("null clears a slot: %+v", got)
	}
	if got := stored(b)["main"]; got.Preset != "stone_brick@1" || got.Params["brick_size"] != 0.5 {
		t.Fatalf("box main %+v", got)
	}

	for name, tc := range map[string]struct {
		op   any
		says string
	}{
		"slot the model lacks":   {slots(m, map[string]any{"trim": slot("paint@1", nil)}), "可用：stone"},
		"slot on a light":        {slots(l, map[string]any{"main": slot("paint@1", nil)}), "没有材质槽"},
		"unknown material":       {slots(b, map[string]any{"main": slot("glass@1", nil)}), "可用：principled@1"},
		"parameter out of range": {slots(b, map[string]any{"main": slot("stone_brick@1", map[string]any{"brick_size": 9.0})}), "brick_size"},
		"unknown parameter":      {slots(b, map[string]any{"main": slot("paint@1", map[string]any{"shine": 1.0})}), "可用参数"},
		"bad colour":             {slots(b, map[string]any{"main": slot("paint@1", map[string]any{"color": "red"})}), "color"},
		"added with a bad slot": {op{"op": "add_objects", "objects": []any{func() map[string]any {
			o := box(objectID(4), nil, 13)
			o["material_slots"] = map[string]any{"top": slot("paint@1", nil)}
			return o
		}()}}, "可用：main"},
		"hdri sky": {op{"op": "set_environment", "environment": map[string]any{"sky": map[string]any{"kind": "hdri", "hdri": "studio@1"}, "ambient": map[string]any{"color": "#ffffff", "intensity": 0.4}, "fog": nil}}, "暂无内置 HDRI"},
	} {
		err := edit(tc.op)
		if !errors.Is(err, creativeops.ErrValidation) || !strings.Contains(err.Error(), tc.says) {
			t.Errorf("%s: %v, want it to say %q", name, err, tc.says)
		}
	}
	if got := stored(b)["main"]; got.Preset != "stone_brick@1" {
		t.Fatalf("a refused batch writes nothing: %+v", got)
	}
	// Swapping the model and writing its slots in one batch would change the
	// content twice: refused, either way round, before the old model's slots
	// are offered.
	swap := op{"op": "set_instance_model", "object_id": m, "model": map[string]any{"content_revision_id": readScene(t, f.a, me.doc).object(t, m).Model.ContentRevisionID}}
	write := slots(m, map[string]any{"stone": slot("paint@1", nil)})
	for _, ops := range [][]any{{swap, write}, {write, swap}} {
		if err := edit(ops...); !errors.Is(err, creativeops.ErrValidation) || !strings.Contains(err.Error(), "既换模型又改材质槽") {
			t.Errorf("%v: %v", ops[0], err)
		}
	}
}
