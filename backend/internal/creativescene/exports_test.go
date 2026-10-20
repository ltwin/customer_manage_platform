package creativescene_test

import (
	"encoding/json"
	"errors"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativedocument"
	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// sceneCanvas is a canvas holding one scene node.
type sceneCanvas struct {
	canvas, project, node, doc string
}

func newSceneCanvas(t *testing.T, scope store.AccountScope) sceneCanvas {
	t.Helper()
	creativecanvas.SetExportFreshness(creativescene.Freshness{})
	r, err := creativecanvas.CreateProject(t.Context(), scope, command(t, creativecanvas.CreateProjectInput{Name: "导出"}))
	if err != nil {
		t.Fatal(err)
	}
	var p creativecanvas.ProjectResult
	if err := json.Unmarshal(r.Outcome.Response, &p); err != nil {
		t.Fatal(err)
	}
	canvas, err := creativecanvas.GetCanvas(t.Context(), scope, p.CanvasID)
	if err != nil {
		t.Fatal(err)
	}
	node := "cwnode_" + uuid.NewString()
	add := creativecanvas.BatchInput{CanvasID: p.CanvasID, ExpectedTopologyRevision: canvas.TopologyRevision, ReadSet: creativecanvas.ReadSetFor(canvas),
		Actions: []creativecanvas.Action{{Type: "add_node", NodeID: node, TypeKey: creativescene.NodeTypeKey, X: 100, Y: 50}}}
	if _, err := creativecanvas.ApplyCommands(t.Context(), scope, command(t, add)); err != nil {
		t.Fatal(err)
	}
	n := canvasNode(t, scope, p.CanvasID, node)
	return sceneCanvas{canvas: p.CanvasID, project: p.ProjectID, node: node, doc: *n.DocumentID}
}

func canvasNode(t *testing.T, scope store.AccountScope, canvasID, id string) creativecanvas.Node {
	t.Helper()
	c, err := creativecanvas.GetCanvas(t.Context(), scope, canvasID)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range c.Nodes {
		if n.ID == id {
			return n
		}
	}
	t.Fatalf("node %s missing", id)
	return creativecanvas.Node{}
}

func shot(camera string, images ...any) map[string]any {
	return map[string]any{"camera_object_id": camera, "images": images}
}

func img(pass, upload string) map[string]any {
	return map[string]any{"pass": pass, "upload_id": upload}
}

func exportPayload(sc sceneCanvas, revision creativeops.Revision, shots ...any) map[string]any {
	return map[string]any{"document_id": sc.doc, "canvas_id": sc.canvas, "scene_node_id": sc.node, "scene_revision": jsonRevision(revision), "shots": shots}
}

func export(t *testing.T, scope store.AccountScope, payload map[string]any) (creativescene.ExportResult, error) {
	t.Helper()
	return exportCommand(t, scope, command(t, payload))
}

func exportCommand(t *testing.T, scope store.AccountScope, cmd creativeops.Command) (creativescene.ExportResult, error) {
	t.Helper()
	receipt, err := creativescene.Export(t.Context(), scope, cmd)
	if err != nil {
		return creativescene.ExportResult{}, err
	}
	var out creativescene.ExportResult
	if err := json.Unmarshal(receipt.Outcome.Response, &out); err != nil {
		t.Fatal(err)
	}
	return out, nil
}

func basedOn(t *testing.T, n creativecanvas.Node) map[string]any {
	t.Helper()
	var cfg struct {
		BasedOn map[string]any `json:"based_on"`
	}
	if err := json.Unmarshal(n.Config, &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg.BasedOn
}

// AC-3, AC-8: one export is one canvas change of image nodes beside the
// scene, a row per camera and a column per pass; the next export goes below.
func TestExportPlacesTheImagesInOneChange(t *testing.T) {
	f := setup(t)
	sc := newSceneCanvas(t, f.a)
	cams := []string{objectID(1), objectID(2), objectID(3)}
	addObjects(t, f.a, sc.doc,
		named(withContent(cams[0], "camera", 1, map[string]any{"label": "A 机位"}), "摄像机 1"),
		named(withContent(cams[1], "camera", 2, map[string]any{}), "摄像机 2"),
		named(withContent(cams[2], "camera", 3, map[string]any{"aspect": "2:3"}), "摄像机 3"))
	rev := readScene(t, f.a, sc.doc).Revision
	var shots []any
	for _, c := range cams {
		// Passes listed line first: columns still go clay, line.
		shots = append(shots, shot(c, img("line", stage(t, f, f.a, "image")), img("clay", stage(t, f, f.a, "image"))))
	}
	res, err := export(t, f.a, exportPayload(sc, rev, shots...))
	if err != nil {
		t.Fatal(err)
	}
	if res.ChangeID == "" || res.SceneRevision != rev || len(res.Nodes) != 6 {
		t.Fatalf("result %+v", res)
	}
	scene := canvasNode(t, f.a, sc.canvas, sc.node)
	x0 := scene.X + scene.Width + 80
	want := []struct {
		camera, pass, title string
		x, y                float64
	}{
		{cams[0], "clay", "A 机位 · clay", x0, scene.Y}, {cams[0], "line", "A 机位 · 线稿", x0 + 340, scene.Y},
		{cams[1], "clay", "摄像机 2 · clay", x0, scene.Y + 240}, {cams[1], "line", "摄像机 2 · 线稿", x0 + 340, scene.Y + 240},
		{cams[2], "clay", "摄像机 3 · clay", x0, scene.Y + 480}, {cams[2], "line", "摄像机 3 · 线稿", x0 + 340, scene.Y + 480},
	}
	for i, w := range want {
		got := res.Nodes[i]
		n := canvasNode(t, f.a, sc.canvas, got.NodeID)
		if got.CameraObjectID != w.camera || got.Pass != w.pass || n.Title != w.title || n.X != w.x || n.Y != w.y || n.ParentID != nil {
			t.Fatalf("node %d: %+v at (%v, %v) %q, want %+v", i, got, n.X, n.Y, n.Title, w)
		}
		b := basedOn(t, n)
		if b["scene_document_id"] != sc.doc || b["scene_revision"] != jsonRevision(rev) || b["camera_object_id"] != w.camera || b["pass"] != w.pass ||
			b["content_revision_id"] != *n.ContentRevisionID || got.ContentRevisionID != *n.ContentRevisionID || len(b["fingerprint"].(string)) != 67 {
			t.Fatalf("node %d based_on %v", i, b)
		}
		if n.BasedOnState != creativecanvas.BasedOnCurrent {
			t.Fatalf("node %d state %q", i, n.BasedOnState)
		}
	}

	// The second export lands below the first block instead of on it.
	again, err := export(t, f.a, exportPayload(sc, rev, shot(cams[0], img("clay", stage(t, f, f.a, "image")))))
	if err != nil {
		t.Fatal(err)
	}
	n := canvasNode(t, f.a, sc.canvas, again.Nodes[0].NodeID)
	if n.X != x0 || n.Y <= scene.Y+480 {
		t.Fatalf("second export at (%v, %v)", n.X, n.Y)
	}
}

// AC-4: retrying with the same operation id and client_created_at replays
// the receipt, so a lost response never doubles the nodes.
func TestExportRetryReplaysTheSameNodes(t *testing.T) {
	f := setup(t)
	sc := newSceneCanvas(t, f.a)
	cam := objectID(1)
	addObjects(t, f.a, sc.doc, withContent(cam, "camera", 1, map[string]any{}))
	cmd := command(t, exportPayload(sc, readScene(t, f.a, sc.doc).Revision, shot(cam, img("clay", stage(t, f, f.a, "image")))))
	first, err := exportCommand(t, f.a, cmd)
	if err != nil {
		t.Fatal(err)
	}
	second, err := exportCommand(t, f.a, cmd)
	if err != nil || second.ChangeID != first.ChangeID || second.Nodes[0].NodeID != first.Nodes[0].NodeID {
		t.Fatalf("replay %+v %v, first %+v", second, err, first)
	}
	c, err := creativecanvas.GetCanvas(t.Context(), f.a, sc.canvas)
	if err != nil || len(c.Nodes) != 2 {
		t.Fatalf("canvas has %d nodes: %v", len(c.Nodes), err)
	}
	moved := cmd
	moved.CreatedAt = cmd.CreatedAt.Add(time.Second)
	if _, err := exportCommand(t, f.a, moved); !errors.Is(err, creativeops.ErrConflict) {
		t.Fatalf("same id, another request: %v", err)
	}
}

func TestExportRefusals(t *testing.T) {
	f := setup(t)
	sc := newSceneCanvas(t, f.a)
	cam, box1 := objectID(1), objectID(2)
	addObjects(t, f.a, sc.doc, withContent(cam, "camera", 1, map[string]any{}), box(box1, nil, 2))
	rev := readScene(t, f.a, sc.doc).Revision
	one := func() any { return shot(cam, img("clay", stage(t, f, f.a, "image"))) }
	used := stage(t, f, f.a, "image")
	if _, err := export(t, f.a, exportPayload(sc, rev, shot(cam, img("clay", used)))); err != nil {
		t.Fatal(err)
	}
	many := make([]any, 0, 17)
	for i := range 17 {
		c := objectID(100 + i)
		addObjects(t, f.a, sc.doc, withContent(c, "camera", 100+i, map[string]any{}))
		many = append(many, shot(c, img("clay", "u1"), img("line", "u2"), img("lit", "u3")))
	}
	other := sceneCanvas{canvas: sc.canvas, node: sc.node, doc: createScene(t, f.a)}
	for _, tc := range []struct {
		name    string
		scope   store.AccountScope
		payload map[string]any
		want    error
	}{
		{"camera not in the revision", f.a, exportPayload(sc, rev, shot(objectID(9), img("clay", "u"))), creativeops.ErrValidation},
		{"not a camera", f.a, exportPayload(sc, rev, shot(box1, img("clay", "u"))), creativeops.ErrValidation},
		{"revision ahead", f.a, exportPayload(sc, rev+100, one()), creativeops.ErrValidation},
		{"repeated pass", f.a, exportPayload(sc, rev, shot(cam, img("clay", "u1"), img("clay", "u2"))), creativeops.ErrValidation},
		{"repeated camera", f.a, exportPayload(sc, rev, shot(cam, img("clay", "u1")), shot(cam, img("line", "u2"))), creativeops.ErrValidation},
		{"49 images", f.a, exportPayload(sc, rev, many...), creativedocument.ErrSizeLimit},
		{"consumed upload", f.a, exportPayload(sc, rev, shot(cam, img("clay", used))), creativescene.ErrUploadUnavailable},
		{"model upload", f.a, exportPayload(sc, rev, shot(cam, img("clay", stage(t, f, f.a, "model")))), creativescene.ErrUploadUnavailable},
		{"node of another scene", f.a, exportPayload(other, 1, shot(objectID(1), img("clay", "u"))), creativeops.ErrValidation},
		{"another account", f.b, exportPayload(sc, rev, shot(cam, img("clay", "u"))), creativedocument.ErrNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := export(t, tc.scope, tc.payload)
			wantErr(t, err, tc.want)
		})
	}
	// The other scene has a camera at revision 2 but no node on the canvas.
	cam2 := objectID(50)
	addObjects(t, f.a, other.doc, withContent(cam2, "camera", 1, map[string]any{}))
	if _, err := export(t, f.a, exportPayload(other, readScene(t, f.a, other.doc).Revision, shot(cam2, img("clay", stage(t, f, f.a, "image"))))); !errors.Is(err, creativecanvas.ErrNotFound) {
		t.Fatalf("scene node of another document: %v", err)
	}

	if _, err := creativedocument.Trash(t.Context(), f.a, command(t, map[string]any{"document_id": sc.doc})); err != nil {
		t.Fatal(err)
	}
	if _, err := export(t, f.a, exportPayload(sc, rev, one())); !errors.Is(err, creativedocument.ErrTrashed) {
		t.Fatalf("trashed scene: %v", err)
	}
	if _, err := creativedocument.Untrash(t.Context(), f.a, command(t, map[string]any{"document_id": sc.doc})); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`UPDATE creative_projects SET archived_at=clock_timestamp() WHERE id=$1`, sc.project); err != nil {
		t.Fatal(err)
	}
	if _, err := export(t, f.a, exportPayload(sc, rev, one())); !errors.Is(err, creativecanvas.ErrArchived) {
		t.Fatalf("archived canvas: %v", err)
	}
}

// E3: based_on only comes from export placement; a canvas command cannot
// write a node's config.
func TestCanvasCommandsCannotWriteBasedOn(t *testing.T) {
	f := setup(t)
	sc := newSceneCanvas(t, f.a)
	c, err := creativecanvas.GetCanvas(t.Context(), f.a, sc.canvas)
	if err != nil {
		t.Fatal(err)
	}
	payload := map[string]any{"canvas_id": sc.canvas, "expected_topology_revision": jsonRevision(c.TopologyRevision), "read_set": creativecanvas.ReadSetFor(c),
		"actions": []any{map[string]any{"type": "add_node", "node_id": "cwnode_" + uuid.NewString(), "type_key": "core.image",
			"config": map[string]any{"based_on": map[string]any{"scene_document_id": sc.doc}}}}}
	if _, err := creativecanvas.ApplyCommands(t.Context(), f.a, command(t, payload)); !errors.Is(err, creativeops.ErrValidation) {
		t.Fatalf("add_node with config: %v", err)
	}
}

// AC-5: the canvas read shows whether an export image still matches its
// scene; based_on stops counting once the node shows another image.
func TestBasedOnStateFollowsTheScene(t *testing.T) {
	f := setup(t)
	sc := newSceneCanvas(t, f.a)
	cam, pillar, lamp := objectID(1), objectID(2), objectID(3)
	addObjects(t, f.a, sc.doc, withContent(cam, "camera", 1, map[string]any{}), box(pillar, nil, 2),
		withContent(lamp, "light", 3, map[string]any{"type": "point"}))
	res, err := export(t, f.a, exportPayload(sc, readScene(t, f.a, sc.doc).Revision,
		shot(cam, img("clay", stage(t, f, f.a, "image")), img("line", stage(t, f, f.a, "image")))))
	if err != nil {
		t.Fatal(err)
	}
	clay, line := res.Nodes[0].NodeID, res.Nodes[1].NodeID
	states := func() (string, string) {
		return canvasNode(t, f.a, sc.canvas, clay).BasedOnState, canvasNode(t, f.a, sc.canvas, line).BasedOnState
	}
	move := func(x float64) {
		v := readScene(t, f.a, sc.doc)
		mustSend(t, f.a, batch{DocumentID: sc.doc, Expected: v.expect(placement(pillar)), Operations: []any{moveOp(pillar, x)}})
	}
	expect := func(step, clayWant, lineWant string) {
		t.Helper()
		if c, l := states(); c != clayWant || l != lineWant {
			t.Fatalf("%s: clay %q line %q, want %q %q", step, c, l, clayWant, lineWant)
		}
	}
	expect("after export", "current", "current")

	// A light only changes the lit image; another camera changes none.
	v := readScene(t, f.a, sc.doc)
	mustSend(t, f.a, batch{DocumentID: sc.doc, Expected: v.expect(content(lamp)), Operations: []any{
		op{"op": "update_objects", "updates": []any{map[string]any{"object_id": lamp, "light": map[string]any{"intensity": 80}}}}}})
	addObjects(t, f.a, sc.doc, withContent(objectID(4), "camera", 4, map[string]any{}))
	expect("light and another camera", "current", "current")

	move(3)
	expect("moved", "stale", "stale")
	move(0)
	expect("moved back", "current", "current")

	// The environment is not in line art.
	v = readScene(t, f.a, sc.doc)
	env := map[string]any{"sky": map[string]any{"kind": "color", "color": "#203040"}, "ambient": map[string]any{"color": "#ffffff", "intensity": 0.4}, "fog": nil}
	mustSend(t, f.a, batch{DocumentID: sc.doc, Expected: v.expect("environment"), Operations: []any{op{"op": "set_environment", "environment": env}}})
	expect("environment", "stale", "current")

	if _, err := creativedocument.Trash(t.Context(), f.a, command(t, map[string]any{"document_id": sc.doc})); err != nil {
		t.Fatal(err)
	}
	expect("trashed", "unavailable", "unavailable")
	if _, err := creativedocument.Untrash(t.Context(), f.a, command(t, map[string]any{"document_id": sc.doc})); err != nil {
		t.Fatal(err)
	}

	// Clearing the node's image ends based_on: no state at all.
	c, err := creativecanvas.GetCanvas(t.Context(), f.a, sc.canvas)
	if err != nil {
		t.Fatal(err)
	}
	clear := creativecanvas.BatchInput{CanvasID: sc.canvas, ExpectedTopologyRevision: c.TopologyRevision, ReadSet: creativecanvas.ReadSetFor(c),
		Actions: []creativecanvas.Action{{Type: "clear_content", NodeID: line}}}
	if _, err := creativecanvas.ApplyCommands(t.Context(), f.a, command(t, clear)); err != nil {
		t.Fatal(err)
	}
	expect("line cleared", "stale", "")

	// Removing the camera makes the image stale.
	remove(t, f.a, sc.doc, []string{cam}, cam)
	expect("camera removed", "stale", "")
}

// AC-5: the fingerprint counts exactly what a camera and pass would draw.
func TestFingerprintCountsWhatThePassDraws(t *testing.T) {
	cam := objectID(4)
	base := func() creativescene.Scene {
		s := validScene()
		s.Objects[3].Camera.DepthOfField = flag(true)
		return s
	}
	fp := func(s creativescene.Scene, pass string) string {
		v, ok := creativescene.Fingerprint(s, cam, pass)
		if !ok {
			t.Fatal("camera missing")
		}
		return v
	}
	obj := func(i int, m func(o *creativescene.Object)) func(*creativescene.Scene) {
		return func(s *creativescene.Scene) { m(&s.Objects[i]) }
	}
	otherCamera := func(s *creativescene.Scene) {
		c := s.Objects[3]
		c.ID, c.Order = objectID(9), 9
		s.Objects = append(s.Objects, c)
	}
	for _, tc := range []struct {
		name            string
		change          func(*creativescene.Scene)
		clay, line, lit bool // whether each pass goes stale
	}{
		{"move geometry", obj(1, func(o *creativescene.Object) { o.Transform.Position[0] = 2 }), true, true, true},
		{"reparent geometry", obj(1, func(o *creativescene.Object) { o.ParentID = nil; o.Order = 7 }), true, true, true},
		{"reparent and add a camera", func(s *creativescene.Scene) { s.Objects[1].ParentID = nil; s.Objects[1].Order = 7; otherCamera(s) }, true, true, true},
		{"hide geometry", obj(1, func(o *creativescene.Object) { o.Visible = false }), true, true, true},
		{"move this camera", obj(3, func(o *creativescene.Object) { o.Transform.Position[1] = 2 }), true, true, true},
		{"environment", func(s *creativescene.Scene) { s.Environment.Ambient.Intensity = 2 }, true, false, true},
		{"add another camera", otherCamera, false, false, false},
		{"light", obj(2, func(o *creativescene.Object) { o.Light.Intensity = f64(80) }), false, false, true},
		{"material slots", obj(1, func(o *creativescene.Object) { o.MaterialSlots = nil }), false, false, true},
		{"name, lock, order", obj(1, func(o *creativescene.Object) { o.Name, o.Locked, o.Order = "柱", true, 3 }), false, false, false},
		{"camera label", obj(3, func(o *creativescene.Object) { o.Camera.Label = ptr("A") }), false, false, false},
		{"render mode", func(s *creativescene.Scene) { s.Render.Mode = "lit" }, false, false, false},
		{"f_stop with depth of field", obj(3, func(o *creativescene.Object) { o.Camera.FStop = f64(1.4) }), true, false, true},
		{"focus with depth of field", obj(3, func(o *creativescene.Object) { o.Camera.Focus = f64(3) }), true, false, true},
		{"focus object with depth of field", obj(3, func(o *creativescene.Object) { o.Camera.FocusObjectID = ptr(objectID(2)) }), true, false, true},
		{"depth of field off", obj(3, func(o *creativescene.Object) { o.Camera.DepthOfField = flag(false) }), true, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := base()
			tc.change(&s)
			for pass, stale := range map[string]bool{"clay": tc.clay, "line": tc.line, "lit": tc.lit} {
				if got := fp(s, pass) != fp(base(), pass); got != stale {
					t.Errorf("%s: stale %v, want %v", pass, got, stale)
				}
			}
		})
	}
	// With depth of field off, aperture and focus change nothing.
	off := base()
	off.Objects[3].Camera.DepthOfField = flag(false)
	changed := off
	changed.Objects = slices.Clone(off.Objects)
	c := *changed.Objects[3].Camera
	c.FStop, c.Focus = f64(1.4), f64(2)
	changed.Objects[3].Camera = &c
	if fp(off, "clay") != fp(changed, "clay") {
		t.Fatal("aperture with depth of field off made clay stale")
	}
	// A deleted camera has no fingerprint.
	gone := base()
	gone.Objects = gone.Objects[:3]
	if _, ok := creativescene.Fingerprint(gone, cam, "clay"); ok {
		t.Fatal("fingerprint of a removed camera")
	}
}

// The freshness check runs on the snapshots the canvas read already loaded:
// it takes no transaction, so it cannot query.
func TestFreshnessReadsNothing(t *testing.T) {
	f := setup(t)
	sc := newSceneCanvas(t, f.a)
	cam := objectID(1)
	addObjects(t, f.a, sc.doc, withContent(cam, "camera", 1, map[string]any{}))
	res, err := export(t, f.a, exportPayload(sc, readScene(t, f.a, sc.doc).Revision, shot(cam, img("clay", stage(t, f, f.a, "image")))))
	if err != nil {
		t.Fatal(err)
	}
	n := canvasNode(t, f.a, sc.canvas, res.Nodes[0].NodeID)
	b := basedOn(t, n)
	var cur creativedocument.Current
	if err := f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		var err error
		cur, err = creativedocument.ReadCurrentInTx(t.Context(), tx, sc.doc, creativescene.TypeKey)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	src := creativecanvas.ExportSource{NodeID: n.ID, DocumentID: sc.doc, CameraObjectID: cam, Pass: "clay", Fingerprint: b["fingerprint"].(string)}
	old := src
	old.NodeID, old.Fingerprint = "old", "v0:abc"
	states := creativescene.Freshness{}.Check([]creativecanvas.ExportSource{src, old}, map[string]creativecontent.Revision{sc.doc: cur.Content})
	if states[n.ID] != "current" || states["old"] != "stale" {
		t.Fatalf("states %v", states)
	}
}

// Performance record (design「性能」): a canvas read with a 200-object scene
// and six export images, with and without the freshness check. Only logged.
func TestCanvasReadCostOfExports(t *testing.T) {
	f := setup(t)
	sc := newSceneCanvas(t, f.a)
	objects := []map[string]any{}
	for i := range 197 {
		o := box(objectID(100+i), nil, 10+i)
		o["transform"] = at(float64(i%20), 0, float64(i/20))
		objects = append(objects, o)
	}
	cams := []string{objectID(1), objectID(2), objectID(3)}
	for i, c := range cams {
		objects = append(objects, withContent(c, "camera", i+1, map[string]any{}))
	}
	addObjects(t, f.a, sc.doc, objects...)
	var shots []any
	for _, c := range cams {
		shots = append(shots, shot(c, img("clay", stage(t, f, f.a, "image")), img("line", stage(t, f, f.a, "image"))))
	}
	if _, err := export(t, f.a, exportPayload(sc, readScene(t, f.a, sc.doc).Revision, shots...)); err != nil {
		t.Fatal(err)
	}
	median := func() time.Duration {
		var reads []time.Duration
		for range 30 {
			start := time.Now()
			if _, err := creativecanvas.GetCanvas(t.Context(), f.a, sc.canvas); err != nil {
				t.Fatal(err)
			}
			reads = append(reads, time.Since(start))
		}
		slices.Sort(reads)
		return reads[len(reads)/2]
	}
	with := median()
	creativecanvas.SetExportFreshness(nil)
	defer creativecanvas.SetExportFreshness(creativescene.Freshness{})
	without := median()
	t.Logf("canvas read, 200-object scene, 6 export images: p50 %v with freshness, %v without (+%v)", with, without, with-without)
}

// The export records each camera's lens (contracts「导出图来源」lens) as the
// renderer used it: the focus object's depth along the view, through a
// scaled parent; a focus distance kept beyond near.
func TestExportRecordsTheLens(t *testing.T) {
	f := setup(t)
	sc := newSceneCanvas(t, f.a)
	aimed, plain, g, vase := objectID(1), objectID(2), objectID(3), objectID(4)
	turned := withContent(aimed, "camera", 1, map[string]any{"focal_mm": 85, "depth_of_field": true, "f_stop": 1.8, "focus_object_id": vase})
	// Standing at x = 5 and turned 90° about Y, it looks along −X.
	turned["transform"] = map[string]any{"position": []float64{5, 1, 0}, "rotation": []float64{0, math.Sqrt2 / 2, 0, math.Sqrt2 / 2}, "scale": []float64{1, 1, 1}}
	parent := group(g, nil, 3)
	parent["transform"] = map[string]any{"position": []float64{1, 0, 0}, "rotation": []float64{0, 0, 0, 1}, "scale": []float64{2, 2, 2}}
	child := named(box(vase, &g, 4), "花瓶")
	child["transform"] = at(-1, 0, 0) // world x = 1 + 2 × (−1) = −1, 6 m ahead
	addObjects(t, f.a, sc.doc, parent, child, turned, withContent(plain, "camera", 2, map[string]any{"focus": 0.01}))
	rev := readScene(t, f.a, sc.doc).Revision
	res, err := export(t, f.a, exportPayload(sc, rev,
		shot(aimed, img("clay", stage(t, f, f.a, "image")), img("line", stage(t, f, f.a, "image"))),
		shot(plain, img("clay", stage(t, f, f.a, "image")))))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]creativecanvas.SceneExportLens{
		aimed: {FocalMM: 85, DepthOfField: true, FStop: 1.8, FocusM: 6, FocusObjectName: "花瓶"},
		plain: {FocalMM: 35, DepthOfField: false, FStop: 2.8, FocusM: 0.1}, // near
	}
	for _, n := range res.Nodes {
		if n.Lens != want[n.CameraObjectID] {
			t.Fatalf("%s %s lens %+v, want %+v", n.CameraObjectID, n.Pass, n.Lens, want[n.CameraObjectID])
		}
		b, ok := creativecanvas.SceneExportOf(canvasNode(t, f.a, sc.canvas, n.NodeID))
		if !ok || b.Lens == nil || *b.Lens != n.Lens {
			t.Fatalf("based_on of %s: %+v", n.NodeID, b)
		}
	}
}
