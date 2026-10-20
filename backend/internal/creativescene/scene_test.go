package creativescene_test

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativedocument"
	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
	"github.com/samson/customer-manage-platform/backend/internal/planningmedia"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store/storetest"
)

func TestMain(m *testing.M) { storetest.Main(m, store.MigrateUp) }

type fixture struct {
	db   *sql.DB
	a, b store.AccountScope
}

func setup(t *testing.T) fixture {
	t.Helper()
	url := storetest.NewURL(t)
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`INSERT INTO accounts(id,password_hash,status) VALUES ('scene-a','test','active'),('scene-b','test','active')`); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.Context(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	return fixture{db: db, a: st.ScopeFor(auth.AccountContext{AccountID: "scene-a"}), b: st.ScopeFor(auth.AccountContext{AccountID: "scene-b"})}
}

func createScene(t *testing.T, scope store.AccountScope) string {
	t.Helper()
	typ, ok := creativedocument.Lookup(creativescene.TypeKey)
	if !ok {
		t.Fatal("scene type is not registered")
	}
	var d creativedocument.Document
	if err := scope.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		var err error
		d, err = creativedocument.CreateInTx(t.Context(), tx, typ, "场景", nil, creativedocument.Photographer)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return d.ID
}

// batch is the command payload; expected values are revision strings or nil
// for "does not exist".
type batch struct {
	DocumentID string         `json:"document_id"`
	Expected   map[string]any `json:"expected,omitempty"`
	Operations []any          `json:"operations"`
}

type op = map[string]any

func command(t *testing.T, payload any) creativeops.Command {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return creativeops.Command{OperationID: uuid.NewString(), CreatedAt: time.Now(), Payload: raw}
}

// send never calls t.Fatal so goroutines may use it.
func send(t *testing.T, scope store.AccountScope, b batch) (creativedocument.CommandResult, error) {
	receipt, err := creativedocument.CommandAs(t.Context(), scope, creativedocument.Photographer, command(t, b))
	if err != nil {
		return creativedocument.CommandResult{}, err
	}
	var out creativedocument.CommandResult
	err = json.Unmarshal(receipt.Outcome.Response, &out)
	return out, err
}

func mustSend(t *testing.T, scope store.AccountScope, b batch) creativedocument.CommandResult {
	t.Helper()
	out, err := send(t, scope, b)
	if err != nil {
		t.Fatalf("command %v: %v", b.Operations, err)
	}
	return out
}

type sceneView struct {
	TypeKey   string               `json:"type_key"`
	Revision  creativeops.Revision `json:"revision"`
	UnitRevs  map[string]string    `json:"unit_revs"`
	TrashedAt *time.Time           `json:"trashed_at"`
	Scene     creativescene.Scene  `json:"scene"`
	Models    map[string]any       `json:"models"`
}

func readScene(t *testing.T, scope store.AccountScope, id string) sceneView {
	t.Helper()
	raw, err := creativedocument.Read(t.Context(), scope, id)
	if err != nil {
		t.Fatal(err)
	}
	var v sceneView
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

// expect builds expected from the current unit revisions; a unit the scene
// does not hold expects null.
func (v sceneView) expect(units ...string) map[string]any {
	out := make(map[string]any, len(units))
	for _, u := range units {
		if r, ok := v.UnitRevs[u]; ok {
			out[u] = r
		} else {
			out[u] = nil
		}
	}
	return out
}

func (v sceneView) object(t *testing.T, id string) creativescene.Object {
	t.Helper()
	for _, o := range v.Scene.Objects {
		if o.ID == id {
			return o
		}
	}
	t.Fatalf("object %s missing", id)
	return creativescene.Object{}
}

func (v sceneView) has(id string) bool {
	for _, o := range v.Scene.Objects {
		if o.ID == id {
			return true
		}
	}
	return false
}

func objectID(n int) string { return fmt.Sprintf("sobj_%021d", n) }

func placement(id string) string { return "object:" + id }
func content(id string) string   { return "object:" + id + ":content" }

// units lists both units of each object.
func units(ids ...string) []string {
	out := make([]string, 0, 2*len(ids))
	for _, id := range ids {
		out = append(out, placement(id), content(id))
	}
	return out
}

func identity() map[string]any {
	return map[string]any{"position": []float64{0, 0, 0}, "rotation": []float64{0, 0, 0, 1}, "scale": []float64{1, 1, 1}}
}

func at(x, y, z float64) map[string]any {
	tr := identity()
	tr["position"] = []float64{x, y, z}
	return tr
}

func object(id, kind string, parent *string, order int) map[string]any {
	return map[string]any{"id": id, "kind": kind, "name": "", "parent_id": parent, "order": order, "transform": identity(), "visible": true, "locked": false}
}

func box(id string, parent *string, order int) map[string]any {
	o := object(id, "primitive", parent, order)
	o["primitive"] = map[string]any{"builtin": "box@1"}
	return o
}

func group(id string, parent *string, order int) map[string]any {
	return object(id, "group", parent, order)
}

func ptr(s string) *string { return &s }

func addObjects(t *testing.T, scope store.AccountScope, doc string, objects ...map[string]any) creativedocument.CommandResult {
	t.Helper()
	v := readScene(t, scope, doc)
	list := make([]any, len(objects))
	for i, o := range objects {
		list[i] = o
	}
	return mustSend(t, scope, batch{DocumentID: doc, Expected: v.expect("objects"), Operations: []any{op{"op": "add_objects", "objects": list}}})
}

func wantErr(t *testing.T, err, target error) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Fatalf("want %v, got %v", target, err)
	}
}

// tree is every object's parent and order, for comparing hierarchies.
func tree(v sceneView) map[string]string {
	out := make(map[string]string, len(v.Scene.Objects))
	for _, o := range v.Scene.Objects {
		parent := "-"
		if o.ParentID != nil {
			parent = *o.ParentID
		}
		out[o.ID] = parent + "#" + strconv.Itoa(o.Order)
	}
	return out
}

// byID makes snapshots comparable regardless of array order.
func byID(v sceneView) map[string]creativescene.Object {
	out := make(map[string]creativescene.Object, len(v.Scene.Objects))
	for _, o := range v.Scene.Objects {
		out[o.ID] = o
	}
	return out
}

func sorted(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

func TestNewSceneFromCanvas(t *testing.T) {
	f := setup(t)
	defs := map[string]creativecanvas.NodeDefinition{}
	for _, d := range creativecanvas.NodeDefinitions() {
		defs[d.TypeKey] = d
	}
	if d := defs[creativescene.NodeTypeKey]; !d.CreationSupported || !d.Maximize {
		t.Fatalf("core.scene definition %+v", d)
	}

	r, err := creativecanvas.CreateProject(t.Context(), f.a, command(t, creativecanvas.CreateProjectInput{Name: "场景项目"}))
	if err != nil {
		t.Fatal(err)
	}
	var p creativecanvas.ProjectResult
	if err := json.Unmarshal(r.Outcome.Response, &p); err != nil {
		t.Fatal(err)
	}
	canvas, err := creativecanvas.GetCanvas(t.Context(), f.a, p.CanvasID)
	if err != nil {
		t.Fatal(err)
	}
	node := "cwnode_" + uuid.NewString()
	add := creativecanvas.BatchInput{CanvasID: p.CanvasID, ExpectedTopologyRevision: canvas.TopologyRevision, ReadSet: creativecanvas.ReadSetFor(canvas),
		Actions: []creativecanvas.Action{{Type: "add_node", NodeID: node, TypeKey: creativescene.NodeTypeKey, Title: "未命名场景"}}}
	if _, err := creativecanvas.ApplyCommands(t.Context(), f.a, command(t, add)); err != nil {
		t.Fatal(err)
	}
	canvas, err = creativecanvas.GetCanvas(t.Context(), f.a, p.CanvasID)
	if err != nil {
		t.Fatal(err)
	}
	var n struct {
		Data struct {
			DocumentID string `json:"document_id"`
		} `json:"data"`
		DocumentSummary json.RawMessage `json:"document_summary"`
	}
	for _, c := range canvas.Nodes {
		if c.ID == node {
			raw, err := json.Marshal(c)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(raw, &n); err != nil {
				t.Fatal(err)
			}
		}
	}
	if n.Data.DocumentID == "" || string(n.DocumentSummary) != `{"cameras":0,"lights":0,"objects":1,"primitives":1}` {
		t.Fatalf("scene node %+v %s", n, n.DocumentSummary)
	}

	v := readScene(t, f.a, n.Data.DocumentID)
	if v.TypeKey != "scene" || v.Revision != 1 || len(v.Scene.Objects) != 1 || len(v.Models) != 0 || v.Models == nil {
		t.Fatalf("new scene %+v", v)
	}
	ground := v.Scene.Objects[0]
	if ground.Primitive == nil || ground.Primitive.Builtin != "ground@1" || ground.Primitive.Params["size"] != 40.0 || !ground.Locked || !ground.Visible || ground.ParentID != nil {
		t.Fatalf("ground %+v", ground)
	}
	if want := []string{"environment", "meta", "objects", "render", placement(ground.ID), content(ground.ID)}; !reflect.DeepEqual(sorted(keys(v.UnitRevs)), sorted(want)) {
		t.Fatalf("units %v", v.UnitRevs)
	}
	if sky := v.Scene.Environment.Sky; sky.Kind != "procedural" || *sky.SunElevationDeg != 35 || v.Scene.Render.Mode != "clay" || v.Scene.Environment.Fog != nil {
		t.Fatalf("environment %+v", v.Scene.Environment)
	}

	// A scene node's content is its document. Replacing it as plain node
	// content wrote an unchecked scene revision onto the node.
	contents := func() (n int) {
		if err := f.db.QueryRow(`SELECT count(*) FROM creative_content_revisions WHERE account_id='scene-a'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	before := contents()
	canvas, err = creativecanvas.GetCanvas(t.Context(), f.a, p.CanvasID)
	if err != nil {
		t.Fatal(err)
	}
	replace := creativecanvas.BatchInput{CanvasID: p.CanvasID, ExpectedTopologyRevision: canvas.TopologyRevision, ReadSet: creativecanvas.ReadSetFor(canvas),
		Actions: []creativecanvas.Action{{Type: "replace_content", NodeID: node, Payload: &creativecontent.Payload{Scene: json.RawMessage(`{"objects":"nonsense"}`)},
			Rights: &creativecontent.RightsDeclarationInput{SourceClass: planningmedia.SourcePhotographerOwned, RightsBasis: planningmedia.RightsOwnershipAttested}}}}
	_, err = creativecanvas.ApplyCommands(t.Context(), f.a, command(t, replace))
	wantErr(t, err, creativeops.ErrValidation)
	if after := contents(); after != before {
		t.Fatalf("content revisions %d -> %d", before, after)
	}
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestAddAndUpdateObjects(t *testing.T) {
	f := setup(t)
	doc := createScene(t, f.a)
	g, a, b := objectID(1), objectID(2), objectID(3)
	partial := box(b, &g, 1)
	partial["primitive"] = map[string]any{"builtin": "box@1", "params": map[string]any{"height": 2.5}}
	res := addObjects(t, f.a, doc, group(g, nil, 1), box(a, &g, 0), partial)
	if res.Revision != 2 || !reflect.DeepEqual(res.ChangedUnits, sorted(append(units(g, a, b), "objects"))) || len(res.RemovedUnits) != 0 {
		t.Fatalf("add receipt %+v", res)
	}
	v := readScene(t, f.a, doc)
	if p := v.object(t, b).Primitive.Params; p["height"] != 2.5 || p["width"] != 1.0 || p["depth"] != 1.0 {
		t.Fatalf("params not completed with defaults: %v", p)
	}

	// Placement and content are separate units; one item may touch both.
	cases := []struct {
		name   string
		update map[string]any
		units  []string
	}{
		{"placement", map[string]any{"object_id": a, "transform": at(1, 0, 2), "name": "石台"}, []string{placement(a)}},
		{"content", map[string]any{"object_id": a, "primitive": map[string]any{"params": map[string]any{"width": 3}}}, []string{content(a)}},
		{"both", map[string]any{"object_id": a, "visible": false, "material_slots": map[string]any{"main": map[string]any{"preset": "stone@1", "params": map[string]any{"roughness": 0.8}}}}, []string{placement(a), content(a)}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := readScene(t, f.a, doc)
			res := mustSend(t, f.a, batch{DocumentID: doc, Expected: v.expect(c.units...), Operations: []any{op{"op": "update_objects", "updates": []any{c.update}}}})
			if res.Revision != v.Revision+1 || !reflect.DeepEqual(res.ChangedUnits, c.units) {
				t.Fatalf("receipt %+v", res)
			}
		})
	}
	v = readScene(t, f.a, doc)
	o := v.object(t, a)
	if o.Name != "石台" || o.Transform.Position[0] != 1 || o.Visible || o.Primitive.Params["width"] != 3.0 || o.Primitive.Params["height"] != 1.0 || o.MaterialSlots["main"].Preset != "stone@1" {
		t.Fatalf("updated object %+v", o)
	}

	// The contract example: move one object and add another in one batch.
	c := objectID(4)
	res = mustSend(t, f.a, batch{DocumentID: doc, Expected: v.expect(placement(b), "objects"), Operations: []any{
		op{"op": "update_objects", "updates": []any{map[string]any{"object_id": b, "transform": at(0, 0, -3)}}},
		op{"op": "add_objects", "objects": []any{box(c, nil, 2)}},
	}})
	if !reflect.DeepEqual(res.ChangedUnits, sorted([]string{placement(b), "objects", placement(c), content(c)})) {
		t.Fatalf("mixed receipt %+v", res)
	}
}

// TestOneUpdateMovesTwoThousandObjects is select-all then G: one operation,
// one revision, however many objects.
func TestOneUpdateMovesTwoThousandObjects(t *testing.T) {
	f := setup(t)
	doc := createScene(t, f.a)
	ground := readScene(t, f.a, doc).Scene.Objects[0].ID
	ids := []string{ground}
	objects := make([]map[string]any, 0, 1999)
	for i := range 1999 {
		ids = append(ids, objectID(i+1))
		objects = append(objects, group(objectID(i+1), nil, i+1))
	}
	addObjects(t, f.a, doc, objects...)
	v := readScene(t, f.a, doc)
	updates := make([]any, len(ids))
	expected := map[string]any{}
	for i, id := range ids {
		updates[i] = map[string]any{"object_id": id, "transform": at(0, 2, 0)}
		expected[placement(id)] = v.UnitRevs[placement(id)]
	}
	res := mustSend(t, f.a, batch{DocumentID: doc, Expected: expected, Operations: []any{op{"op": "update_objects", "updates": updates}}})
	if res.Revision != v.Revision+1 || !res.ResetRequired {
		t.Fatalf("bulk update receipt %+v", res)
	}
	after := readScene(t, f.a, doc)
	for _, o := range after.Scene.Objects {
		if o.Transform.Position[1] != 2 || after.UnitRevs[placement(o.ID)] != strconv.FormatInt(int64(res.Revision), 10) {
			t.Fatalf("object %s not moved in revision %d", o.ID, res.Revision)
		}
	}
}

func TestOversizedSnapshotIsRefused(t *testing.T) {
	f := setup(t)
	doc := createScene(t, f.a)
	v := readScene(t, f.a, doc)
	// 1500 objects stay under the object cap; 80-character names push the
	// snapshot past 512 KiB.
	name := strings.Repeat("场", 80)
	objects := make([]any, 1500)
	for i := range objects {
		g := group(objectID(i+1), nil, i+1)
		g["name"] = name
		objects[i] = g
	}
	_, err := send(t, f.a, batch{DocumentID: doc, Expected: v.expect("objects"), Operations: []any{op{"op": "add_objects", "objects": objects}}})
	wantErr(t, err, creativedocument.ErrSizeLimit)
	if after := readScene(t, f.a, doc); after.Revision != v.Revision || len(after.Scene.Objects) != 1 {
		t.Fatalf("refused command left revision %d with %d objects", after.Revision, len(after.Scene.Objects))
	}
}

func TestRemoveAndReparent(t *testing.T) {
	f := setup(t)
	doc := createScene(t, f.a)
	g, h, a, b := objectID(1), objectID(2), objectID(3), objectID(4)
	addObjects(t, f.a, doc, group(g, nil, 1), group(h, &g, 0), box(a, &h, 0), box(b, nil, 2))

	v := readScene(t, f.a, doc)
	res := mustSend(t, f.a, batch{DocumentID: doc, Expected: v.expect("objects"), Operations: []any{op{"op": "reparent_objects", "moves": []any{map[string]any{"object_id": b, "parent_id": g, "order": 1}}}}})
	if !reflect.DeepEqual(res.ChangedUnits, []string{"objects"}) {
		t.Fatalf("reparent receipt %+v", res)
	}
	if o := readScene(t, f.a, doc).object(t, b); o.ParentID == nil || *o.ParentID != g || o.Order != 1 {
		t.Fatalf("reparented %+v", o)
	}

	// Removing a group takes its descendants, and every one of their units
	// must be expected.
	v = readScene(t, f.a, doc)
	remove := op{"op": "remove_objects", "object_ids": []string{g, a}}
	_, err := send(t, f.a, batch{DocumentID: doc, Expected: v.expect("objects", placement(g), content(g)), Operations: []any{remove}})
	wantErr(t, err, creativeops.ErrValidation)
	res = mustSend(t, f.a, batch{DocumentID: doc, Expected: v.expect(append(units(g, h, a, b), "objects")...), Operations: []any{remove}})
	if !reflect.DeepEqual(res.ChangedUnits, []string{"objects"}) || !reflect.DeepEqual(res.RemovedUnits, sorted(units(g, h, a, b))) {
		t.Fatalf("remove receipt %+v", res)
	}
	if v := readScene(t, f.a, doc); len(v.Scene.Objects) != 1 || v.has(a) {
		t.Fatalf("after remove %v", tree(v))
	}
}

func TestEnvironmentAndRender(t *testing.T) {
	f := setup(t)
	doc := createScene(t, f.a)
	v := readScene(t, f.a, doc)
	env := map[string]any{"sky": map[string]any{"kind": "color", "color": "#203040"}, "ambient": map[string]any{"color": "#ffffff", "intensity": 1}, "fog": map[string]any{"color": "#808080", "near": 5, "far": 50}}
	res := mustSend(t, f.a, batch{DocumentID: doc, Expected: v.expect("environment", "render"), Operations: []any{
		op{"op": "set_environment", "environment": env},
		op{"op": "set_render", "render": map[string]any{"mode": "lit"}},
	}})
	if !reflect.DeepEqual(res.ChangedUnits, []string{"environment", "render"}) {
		t.Fatalf("receipt %+v", res)
	}
	v = readScene(t, f.a, doc)
	if e := v.Scene.Environment; e.Sky.Kind != "color" || *e.Sky.Color != "#203040" || e.Fog == nil || e.Fog.Near != 5 || e.Fog.Far != 50 || v.Scene.Render.Mode != "lit" {
		t.Fatalf("environment %+v render %+v", e, v.Scene.Render)
	}
}

// TestInvalidCommandsLeaveNoRevision covers each operation's bad input; none
// may produce a revision.
func TestInvalidCommandsLeaveNoRevision(t *testing.T) {
	f := setup(t)
	doc := createScene(t, f.a)
	g, a, l := objectID(1), objectID(2), objectID(3)
	light := object(l, "light", nil, 3)
	light["light"] = map[string]any{"type": "point", "color": "#ffeedd", "intensity": 10, "cast_shadow": true}
	addObjects(t, f.a, doc, group(g, nil, 1), box(a, nil, 2), light)
	v := readScene(t, f.a, doc)
	ground := v.Scene.Objects[0].ID
	objects := v.expect("objects")
	add := func(o map[string]any) batch {
		return batch{DocumentID: doc, Expected: objects, Operations: []any{op{"op": "add_objects", "objects": []any{o}}}}
	}
	update := func(u map[string]any, units ...string) batch {
		return batch{DocumentID: doc, Expected: v.expect(units...), Operations: []any{op{"op": "update_objects", "updates": []any{u}}}}
	}
	reparent := func(id string, parent any, order int) batch {
		return batch{DocumentID: doc, Expected: objects, Operations: []any{op{"op": "reparent_objects", "moves": []any{map[string]any{"object_id": id, "parent_id": parent, "order": order}}}}}
	}
	without := func(o map[string]any, key string) map[string]any {
		delete(o, key)
		return o
	}
	with := func(o map[string]any, key string, value any) map[string]any {
		o[key] = value
		return o
	}
	n := objectID(9)
	cases := map[string]batch{
		"unknown op":               {DocumentID: doc, Expected: objects, Operations: []any{op{"op": "explode"}}},
		"unknown field":            {DocumentID: doc, Expected: objects, Operations: []any{op{"op": "add_objects", "objects": []any{box(n, nil, 5)}, "extra": 1}}},
		"empty add":                {DocumentID: doc, Expected: objects, Operations: []any{op{"op": "add_objects", "objects": []any{}}}},
		"missing visible":          add(without(box(n, nil, 5), "visible")),
		"missing parent_id":        add(without(box(n, nil, 5), "parent_id")),
		"short position":           add(with(box(n, nil, 5), "transform", map[string]any{"position": []float64{0, 0}, "rotation": []float64{0, 0, 0, 1}, "scale": []float64{1, 1, 1}})),
		"existing id":              add(box(ground, nil, 5)),
		"bad id":                   add(box("sobj_short", nil, 5)),
		"unknown builtin":          add(with(box(n, nil, 5), "primitive", map[string]any{"builtin": "torus@1"})),
		"param out of range":       add(with(box(n, nil, 5), "primitive", map[string]any{"builtin": "box@1", "params": map[string]any{"width": 0}})),
		"unknown param":            add(with(box(n, nil, 5), "primitive", map[string]any{"builtin": "box@1", "params": map[string]any{"radius": 1}})),
		"model instance":           add(object(n, "model_instance", nil, 5)),
		"order taken":              add(box(n, nil, 2)),
		"parent not a group":       add(box(n, &a, 0)),
		"parent missing":           add(box(n, ptr(objectID(99)), 0)),
		"name too long":            add(with(box(n, nil, 5), "name", string(make([]rune, 81)))),
		"scale zero":               add(with(box(n, nil, 5), "transform", map[string]any{"position": []float64{0, 0, 0}, "rotation": []float64{0, 0, 0, 1}, "scale": []float64{0, 1, 1}})),
		"not a unit quaternion":    add(with(box(n, nil, 5), "transform", map[string]any{"position": []float64{0, 0, 0}, "rotation": []float64{0, 0, 0, 2}, "scale": []float64{1, 1, 1}})),
		"group with content":       add(with(group(n, nil, 5), "primitive", map[string]any{"builtin": "box@1"})),
		"missing object":           update(map[string]any{"object_id": objectID(99), "visible": false}, placement(objectID(99))),
		"object twice":             {DocumentID: doc, Expected: v.expect(placement(a)), Operations: []any{op{"op": "update_objects", "updates": []any{map[string]any{"object_id": a, "visible": false}, map[string]any{"object_id": a, "locked": true}}}}},
		"no fields":                update(map[string]any{"object_id": a}, placement(a)),
		"light on primitive":       update(map[string]any{"object_id": a, "light": light["light"]}, content(a)),
		"params on light":          update(map[string]any{"object_id": l, "primitive": map[string]any{"params": map[string]any{"width": 2}}}, content(l)),
		"spot angle past π/2":      update(map[string]any{"object_id": l, "light": map[string]any{"type": "spot", "angle": 1.6}}, content(l)),
		"angle on point":           update(map[string]any{"object_id": l, "light": map[string]any{"angle": 0.5}}, content(l)),
		"decay out of range":       update(map[string]any{"object_id": l, "light": map[string]any{"decay": 11}}, content(l)),
		"negative distance":        add(with(object(n, "light", nil, 5), "light", map[string]any{"type": "point", "distance": -1})),
		"unknown light type":       add(with(object(n, "light", nil, 5), "light", map[string]any{"type": "area"})),
		"light without type":       add(with(object(n, "light", nil, 5), "light", map[string]any{"intensity": 5})),
		"old light name":           update(map[string]any{"object_id": l, "light": map[string]any{"range_m": 7}}, content(l)),
		"camera on light":          update(map[string]any{"object_id": l, "camera": map[string]any{"far": 50}}, content(l)),
		"camera far before near":   add(with(object(n, "camera", nil, 5), "camera", map[string]any{"far": 0.05})),
		"old camera name":          add(with(object(n, "camera", nil, 5), "camera", map[string]any{"near_m": 0.1})),
		"bad slot preset":          update(map[string]any{"object_id": a, "material_slots": map[string]any{"main": map[string]any{"preset": "stone", "params": map[string]any{}}}}, content(a)),
		"under itself":             reparent(g, g, 0),
		"reparent under primitive": reparent(g, a, 0),
		"remove missing":           {DocumentID: doc, Expected: objects, Operations: []any{op{"op": "remove_objects", "object_ids": []string{objectID(99)}}}},
		"objects touched twice":    {DocumentID: doc, Expected: v.expect(append(units(a), "objects")...), Operations: []any{op{"op": "add_objects", "objects": []any{box(n, nil, 5)}}, op{"op": "remove_objects", "object_ids": []string{a}}}},
		"bad render":               {DocumentID: doc, Expected: v.expect("render"), Operations: []any{op{"op": "set_render", "render": map[string]any{"mode": "path"}}}},
		"sky missing sun":          {DocumentID: doc, Expected: v.expect("environment"), Operations: []any{op{"op": "set_environment", "environment": map[string]any{"sky": map[string]any{"kind": "procedural"}, "ambient": map[string]any{"color": "#ffffff", "intensity": 0.4}, "fog": nil}}}},
		// null is accepted only where the contract says nullable; a null
		// number used to decode as 0 and save.
		"null in position":        update(map[string]any{"object_id": a, "transform": map[string]any{"position": []any{nil, 0, 0}, "rotation": []float64{0, 0, 0, 1}, "scale": []float64{1, 1, 1}}}, placement(a)),
		"null in rotation on add": add(with(box(n, nil, 5), "transform", map[string]any{"position": []float64{0, 0, 0}, "rotation": []any{0, 0, 0, nil}, "scale": []float64{1, 1, 1}})),
		"null visible":            add(with(box(n, nil, 5), "visible", nil)),
		"null name":               add(with(box(n, nil, 5), "name", nil)),
		"null order":              add(with(box(n, nil, 5), "order", nil)),
		"null optional on update": update(map[string]any{"object_id": a, "visible": nil, "locked": true}, placement(a)),
		"null params":             add(with(box(n, nil, 5), "primitive", map[string]any{"builtin": "box@1", "params": nil})),
		"null objects":            {DocumentID: doc, Expected: objects, Operations: []any{op{"op": "add_objects", "objects": nil}}},
		"null ambient":            {DocumentID: doc, Expected: v.expect("environment"), Operations: []any{op{"op": "set_environment", "environment": map[string]any{"sky": map[string]any{"kind": "color", "color": "#203040"}, "ambient": nil, "fog": nil}}}},
		"null in removed ids":     {DocumentID: doc, Expected: objects, Operations: []any{op{"op": "remove_objects", "object_ids": []any{nil}}}},
		"null param value":        add(with(box(n, nil, 5), "primitive", map[string]any{"builtin": "box@1", "params": map[string]any{"width": nil}})),
		"null light field":        update(map[string]any{"object_id": l, "light": map[string]any{"distance": nil}}, content(l)),
		"null camera field":       add(with(object(n, "camera", nil, 5), "camera", map[string]any{"label": nil})),
	}
	for name, b := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := send(t, f.a, b)
			wantErr(t, err, creativeops.ErrValidation)
			if errors.Is(err, creativedocument.ErrNotFound) {
				t.Fatal("a missing object must not read as a missing document")
			}
		})
	}
	if after := readScene(t, f.a, doc); after.Revision != v.Revision {
		t.Fatalf("invalid commands wrote revision %d", after.Revision)
	}
}

// A batch that does not decode says which field, where: the assistant writes
// these by hand and has only the message to go on.
func TestABadShapeNamesTheField(t *testing.T) {
	f := setup(t)
	doc := createScene(t, f.a)
	v := readScene(t, f.a, doc)
	light := func(fields map[string]any) batch {
		o := object(objectID(1), "light", nil, 1)
		o["light"] = fields
		return batch{DocumentID: doc, Expected: v.expect("objects"), Operations: []any{op{"op": "add_objects", "objects": []any{o}}}}
	}
	camera := func(fields map[string]any) batch {
		o := object(objectID(1), "camera", nil, 1)
		o["camera"] = fields
		return batch{DocumentID: doc, Expected: v.expect("objects"), Operations: []any{op{"op": "add_objects", "objects": []any{o}}}}
	}
	for name, tc := range map[string]struct {
		b    batch
		want string
	}{
		// Names from before v7 are rejected, not read as aliases.
		"old light field": {light(map[string]any{"type": "point", "color": "#ffaa55", "intensity": 35, "range_m": 7}),
			"objects[0].light 没有字段 range_m，可用字段：type、color、intensity、cast_shadow、distance、decay、angle、penumbra"},
		"field of another light type": {light(map[string]any{"type": "point", "angle": 0.5}),
			"物件 " + objectID(1) + "：点光没有字段 angle，可用字段：type、color、intensity、cast_shadow、distance、decay"},
		"old camera field": {camera(map[string]any{"focal_mm": 50, "near_m": 0.1}),
			"objects[0].camera 没有字段 near_m，可用字段：focal_mm、sensor_mm、aspect、near、far、label"},
		"null light field": {light(map[string]any{"type": "point", "distance": nil}), "字段 objects[0].light.distance 不能为 null"},
		"wrong type":       {light(map[string]any{"type": "point", "color": "#ffaa55", "intensity": "bright"}), "字段 objects[0].light.intensity 的类型不对"},
		"old fog fields": {batch{DocumentID: doc, Expected: v.expect("environment"), Operations: []any{op{"op": "set_environment", "environment": map[string]any{
			"sky": map[string]any{"kind": "color", "color": "#080f20"}, "ambient": map[string]any{"color": "#8ca6d5", "intensity": 0.25}, "fog": map[string]any{"color": "#101b30", "near_m": 17, "far_m": 55}}}}},
			"environment.fog 没有字段 far_m，可用字段：color、near、far"},
		"field of the operation": {batch{DocumentID: doc, Expected: v.expect("render"), Operations: []any{op{"op": "set_render", "render": map[string]any{"mode": "lit"}, "extra": 1}}}, "操作没有字段 extra"},
		// From a trial: the assistant pads ids by hand and lands one over.
		"hand-counted id": {batch{DocumentID: doc, Expected: v.expect("objects"), Operations: []any{op{"op": "add_objects", "objects": []any{object("sobj_houseL2aaaaaaaaaaaaaaa", "group", nil, 1)}}}},
			`物件 ID "sobj_houseL2aaaaaaaaaaaaaaa" 不合法：sobj_ 后须是 21 位字母、数字、_ 或 -，现在是 22 位`},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := send(t, f.a, tc.b)
			var public creativeops.Public
			if !errors.As(err, &public) || !errors.Is(err, creativeops.ErrValidation) || !strings.HasPrefix(public.PublicMessage(), tc.want) {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
		})
	}
}

// storedContent is one object's light or camera exactly as the snapshot
// holds it.
func storedContent(t *testing.T, scope store.AccountScope, doc, id, key string) map[string]any {
	t.Helper()
	raw, err := creativedocument.Read(t.Context(), scope, doc)
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		Scene struct {
			Objects []map[string]any `json:"objects"`
		} `json:"scene"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	for _, o := range v.Scene.Objects {
		if o["id"] == id {
			m, _ := o[key].(map[string]any)
			return m
		}
	}
	t.Fatalf("object %s missing", id)
	return nil
}

func withContent(id, kind string, order int, fields map[string]any) map[string]any {
	o := object(id, kind, nil, order)
	o[kind] = fields
	return o
}

// AC-1, AC-2: light and camera fields left out take the three.js defaults,
// and a stored light holds exactly the fields of its type.
func TestLightAndCameraDefaults(t *testing.T) {
	f := setup(t)
	doc := createScene(t, f.a)
	p, s, d, c := objectID(1), objectID(2), objectID(3), objectID(4)
	addObjects(t, f.a, doc,
		withContent(p, "light", 1, map[string]any{"type": "point", "color": "#ff963d", "intensity": 45, "distance": 7, "decay": 2}),
		withContent(s, "light", 2, map[string]any{"type": "spot"}),
		withContent(d, "light", 3, map[string]any{"type": "directional"}),
		withContent(c, "camera", 4, map[string]any{}))
	for id, want := range map[string]map[string]any{
		p: {"type": "point", "color": "#ff963d", "intensity": 45.0, "cast_shadow": false, "distance": 7.0, "decay": 2.0},
		s: {"type": "spot", "color": "#ffffff", "intensity": 1.0, "cast_shadow": false, "distance": 0.0, "decay": 2.0, "angle": math.Pi / 3, "penumbra": 0.0},
		d: {"type": "directional", "color": "#ffffff", "intensity": 1.0, "cast_shadow": false},
	} {
		if got := storedContent(t, f.a, doc, id, "light"); !reflect.DeepEqual(got, want) {
			t.Errorf("light %s = %v, want %v", id, got, want)
		}
	}
	want := map[string]any{"focal_mm": 35.0, "sensor_mm": []any{36.0, 24.0}, "aspect": "3:2", "near": 0.1, "far": 1000.0, "label": "",
		"depth_of_field": false, "f_stop": 2.8, "focus": 10.0, "focus_object_id": ""}
	if got := storedContent(t, f.a, doc, c, "camera"); !reflect.DeepEqual(got, want) {
		t.Errorf("camera = %v, want %v", got, want)
	}
}

// AC-4: update_objects changes only the light and camera fields it carries.
// A type change keeps the fields the new type shares, drops the rest and
// fills the ones it gains; the update itself must fit the type.
func TestLightAndCameraUpdatesMergeByKey(t *testing.T) {
	f := setup(t)
	doc := createScene(t, f.a)
	p, s, q, c := objectID(1), objectID(2), objectID(3), objectID(4)
	addObjects(t, f.a, doc,
		withContent(p, "light", 1, map[string]any{"type": "point", "color": "#ff963d", "intensity": 45, "distance": 7, "decay": 2}),
		withContent(s, "light", 2, map[string]any{"type": "spot", "color": "#ffeedd", "intensity": 30, "distance": 5, "decay": 1.5, "angle": 0.4, "penumbra": 0.3}),
		withContent(q, "light", 3, map[string]any{"type": "point", "color": "#336699", "intensity": 20}),
		withContent(c, "camera", 4, map[string]any{"focal_mm": 50, "label": "A"}))
	update := func(id, key string, fields map[string]any) error {
		v := readScene(t, f.a, doc)
		_, err := send(t, f.a, batch{DocumentID: doc, Expected: v.expect(content(id)), Operations: []any{
			op{"op": "update_objects", "updates": []any{map[string]any{"object_id": id, key: fields}}}}})
		return err
	}
	for _, step := range []struct {
		name, id     string
		patch, light map[string]any
	}{
		{"intensity only", p, map[string]any{"intensity": 80},
			map[string]any{"type": "point", "color": "#ff963d", "intensity": 80.0, "cast_shadow": false, "distance": 7.0, "decay": 2.0}},
		{"spot to point", s, map[string]any{"type": "point"},
			map[string]any{"type": "point", "color": "#ffeedd", "intensity": 30.0, "cast_shadow": false, "distance": 5.0, "decay": 1.5}},
		{"point to spot", p, map[string]any{"type": "spot"},
			map[string]any{"type": "spot", "color": "#ff963d", "intensity": 80.0, "cast_shadow": false, "distance": 7.0, "decay": 2.0, "angle": math.Pi / 3, "penumbra": 0.0}},
		{"point to spot with angle", q, map[string]any{"type": "spot", "angle": 0.5},
			map[string]any{"type": "spot", "color": "#336699", "intensity": 20.0, "cast_shadow": false, "distance": 0.0, "decay": 2.0, "angle": 0.5, "penumbra": 0.0}},
	} {
		if err := update(step.id, "light", step.patch); err != nil {
			t.Fatalf("%s: %v", step.name, err)
		}
		if got := storedContent(t, f.a, doc, step.id, "light"); !reflect.DeepEqual(got, step.light) {
			t.Fatalf("%s: light = %v, want %v", step.name, got, step.light)
		}
	}

	before := readScene(t, f.a, doc).Revision
	err := update(s, "light", map[string]any{"angle": 0.5})
	var public creativeops.Public
	if !errors.As(err, &public) || !strings.Contains(public.PublicMessage(), "点光没有字段 angle") {
		t.Fatalf("angle on a point light: %v", err)
	}
	if after := readScene(t, f.a, doc).Revision; after != before {
		t.Fatalf("a refused update wrote revision %d", after)
	}

	if err := update(c, "camera", map[string]any{"far": 200}); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"focal_mm": 50.0, "sensor_mm": []any{36.0, 24.0}, "aspect": "3:2", "near": 0.1, "far": 200.0, "label": "A",
		"depth_of_field": false, "f_stop": 2.8, "focus": 10.0, "focus_object_id": ""}
	if got := storedContent(t, f.a, doc, c, "camera"); !reflect.DeepEqual(got, want) {
		t.Fatalf("camera = %v, want %v", got, want)
	}
}
