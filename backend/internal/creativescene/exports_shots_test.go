package creativescene_test

import (
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
)

// api: 导出分镜 and data: 导出图来源 by shot (scene-timeline-shots AC-10,
// AC-11): a row per shot titled by it, and each image stale only when its
// own shot would draw differently.

func shotExport(marker string, images ...any) map[string]any {
	return map[string]any{"marker_id": marker, "images": images}
}

func TestExportByShot(t *testing.T) {
	f := setup(t)
	sc := newSceneCanvas(t, f.a)
	camA, camB, subject, wall := objectID(1), objectID(2), objectID(3), objectID(4)
	addObjects(t, f.a, sc.doc, camera(camA, 1), camera(camB, 2), box(subject, nil, 3), box(wall, nil, 4))
	named := func(n, frame int, name, cam string) map[string]any {
		m := shotMarker(n, frame, cam)
		m["name"] = name
		return m
	}
	v := readScene(t, f.a, sc.doc)
	mustSend(t, f.a, batch{DocumentID: sc.doc, Expected: v.expect("timeline", placement(subject)), Operations: []any{
		setMarkers(named(1, 0, "回头", camA), named(2, 10, "", camA), named(3, 20, "拔剑", camA)),
		animate(subject, sets(keyAt("transform.position", 0, []float64{0, 0, 0}), keyAt("transform.position", 10, []float64{1, 0, 0}), keyAt("transform.position", 20, []float64{0, 0, 0}))),
	}})

	var shots []any
	for n := 1; n <= 3; n++ {
		shots = append(shots, shotExport(markerID(n), img("clay", stage(t, f, f.a, "image")), img("line", stage(t, f, f.a, "image"))))
	}
	res, err := export(t, f.a, exportPayload(sc, readScene(t, f.a, sc.doc).Revision, shots...))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Nodes) != 6 {
		t.Fatalf("nodes %+v, want 3 rows of 2", res.Nodes)
	}
	titles := []string{"回头 · clay", "回头 · 线稿", "分镜 2 · clay", "分镜 2 · 线稿", "拔剑 · clay", "拔剑 · 线稿"}
	for i, n := range res.Nodes {
		row := i/2 + 1
		if n.MarkerID == nil || *n.MarkerID != markerID(row) || n.CameraObjectID != camA {
			t.Fatalf("node %d = %+v, want shot %d through camera A", i, n, row)
		}
		node := canvasNode(t, f.a, sc.canvas, n.NodeID)
		if node.Title != titles[i] {
			t.Errorf("node %d title %q, want %q", i, node.Title, titles[i])
		}
		// frame 0 is left out, as on every export by camera.
		var frame any
		if row > 1 {
			frame = float64((row - 1) * 10)
		}
		if b := basedOn(t, node); b["marker_id"] != markerID(row) || b["frame"] != frame {
			t.Errorf("node %d based_on %v", i, b)
		}
	}
	states := func() []string {
		out := make([]string, 0, 3)
		for i := 0; i < 6; i += 2 {
			out = append(out, canvasNode(t, f.a, sc.canvas, res.Nodes[i].NodeID).BasedOnState)
		}
		return out
	}
	expect := func(step string, want ...string) {
		t.Helper()
		got := states()
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%s: states %v, want %v", step, got, want)
			}
		}
	}
	send1 := func(units []string, ops ...any) {
		t.Helper()
		v := readScene(t, f.a, sc.doc)
		mustSend(t, f.a, batch{DocumentID: sc.doc, Expected: v.expect(units...), Operations: ops})
	}
	expect("after export", "current", "current", "current")

	// Only shot 2's key: only shot 2 is stale; back again, current.
	send1([]string{placement(subject)}, animate(subject, sets(keyAt("transform.position", 10, []float64{2, 0, 0}))))
	expect("shot 2 keyed", "current", "stale", "current")
	send1([]string{placement(subject)}, animate(subject, sets(keyAt("transform.position", 10, []float64{1, 0, 0}))))
	expect("shot 2 back", "current", "current", "current")

	// A set piece without channels is in every shot.
	send1([]string{placement(wall)}, moveOp(wall, 3))
	expect("wall moved", "stale", "stale", "stale")
	send1([]string{placement(wall)}, moveOp(wall, 0))
	expect("wall back", "current", "current", "current")

	// Moving shot 2 with its keys draws the same.
	send1([]string{"timeline", placement(subject)},
		setMarkers(named(1, 0, "回头", camA), named(2, 15, "", camA), named(3, 20, "拔剑", camA)),
		animate(subject, op{"delete": []any{op{"path": "transform.position", "frame": 10}}, "set": []any{keyAt("transform.position", 15, []float64{1, 0, 0})}}))
	expect("shot 2 moved", "current", "current", "current")

	// Another camera with the same settings in the same place is still another camera.
	send1([]string{"timeline"}, setMarkers(named(1, 0, "回头", camA), named(2, 15, "", camB), named(3, 20, "拔剑", camA)))
	expect("shot 2 rebound", "current", "stale", "current")

	// A deleted shot, or one left without a camera, is stale.
	send1([]string{"timeline", placement(subject)},
		setMarkers(named(1, 0, "回头", ""), named(2, 15, "", camB)),
		animate(subject, op{"delete": []any{op{"path": "transform.position", "frame": 20}}}))
	expect("shot 1 unbound, shot 3 deleted", "stale", "stale", "stale")

	rev := readScene(t, f.a, sc.doc).Revision
	for _, c := range []struct {
		name  string
		shots []any
	}{
		{"marker and camera mixed", []any{shotExport(markerID(2), img("clay", "u1")), shot(camA, img("clay", "u2"))}},
		{"both on one shot", []any{map[string]any{"marker_id": markerID(2), "camera_object_id": camB, "images": []any{img("clay", "u")}}}},
		{"repeated shot", []any{shotExport(markerID(2), img("clay", "u1")), shotExport(markerID(2), img("line", "u2"))}},
		{"missing shot", []any{shotExport(markerID(3), img("clay", "u"))}},
		{"shot without a camera", []any{shotExport(markerID(1), img("clay", "u"))}},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := export(t, f.a, exportPayload(sc, rev, c.shots...))
			wantErr(t, err, creativeops.ErrValidation)
		})
	}
}

func TestExportTransparentBackgroundStaleAndReportValidation(t *testing.T) {
	f := setup(t)
	sc := newSceneCanvas(t, f.a)
	cam := objectID(1)
	addObjects(t, f.a, sc.doc, camera(cam, 1))

	// 1. Opaque revision without effective_transparent_background (legacy caller) succeeds
	// and flips clay/lit stale when scene switch toggles, while line stays current.
	revOff := readScene(t, f.a, sc.doc).Revision
	resOpaque, err := export(t, f.a, exportPayload(sc, revOff, shot(cam,
		img("clay", stage(t, f, f.a, "image")),
		img("line", stage(t, f, f.a, "image")),
		img("lit", stage(t, f, f.a, "image")),
	)))
	if err != nil || len(resOpaque.Nodes) != 3 {
		t.Fatalf("legacy opaque export = %+v, err=%v", resOpaque, err)
	}
	v := readScene(t, f.a, sc.doc)
	mustSend(t, f.a, batch{DocumentID: sc.doc, Expected: v.expect("render"), Operations: []any{op{"op": "set_render", "render": map[string]any{"transparent_background": true}}}})
	revOn := readScene(t, f.a, sc.doc).Revision

	nodeState := func(id string) string { return canvasNode(t, f.a, sc.canvas, id).BasedOnState }
	if nodeState(resOpaque.Nodes[0].NodeID) != creativecanvas.BasedOnStale ||
		nodeState(resOpaque.Nodes[1].NodeID) != creativecanvas.BasedOnCurrent ||
		nodeState(resOpaque.Nodes[2].NodeID) != creativecanvas.BasedOnStale {
		t.Fatalf("after enable: clay=%s line=%s lit=%s", nodeState(resOpaque.Nodes[0].NodeID), nodeState(resOpaque.Nodes[1].NodeID), nodeState(resOpaque.Nodes[2].NodeID))
	}

	// Toggle off restores opaque clay/lit back to current.
	v = readScene(t, f.a, sc.doc)
	mustSend(t, f.a, batch{DocumentID: sc.doc, Expected: v.expect("render"), Operations: []any{op{"op": "set_render", "render": map[string]any{"transparent_background": false}}}})
	if nodeState(resOpaque.Nodes[0].NodeID) != creativecanvas.BasedOnCurrent || nodeState(resOpaque.Nodes[2].NodeID) != creativecanvas.BasedOnCurrent {
		t.Fatalf("after disable: clay=%s lit=%s", nodeState(resOpaque.Nodes[0].NodeID), nodeState(resOpaque.Nodes[2].NodeID))
	}

	// 2. Transparent scene revision requires effective_transparent_background=true for clay/lit,
	// rejects missing/false/null/non-bool BEFORE consuming staged uploads, and allows line-only without report.
	beforeCount := canvasNodeCount(t, f.a, sc.canvas)
	clayUpload := stage(t, f, f.a, "image")
	badPayloads := map[string]map[string]any{
		"missing": exportPayload(sc, revOn, shot(cam, img("clay", clayUpload))),
		"mismatch": func() map[string]any {
			p := exportPayload(sc, revOn, shot(cam, img("clay", clayUpload)))
			p["effective_transparent_background"] = false
			return p
		}(),
		"null": func() map[string]any {
			p := exportPayload(sc, revOn, shot(cam, img("clay", clayUpload)))
			p["effective_transparent_background"] = nil
			return p
		}(),
		"string": func() map[string]any {
			p := exportPayload(sc, revOn, shot(cam, img("clay", clayUpload)))
			p["effective_transparent_background"] = "true"
			return p
		}(),
	}
	for name, p := range badPayloads {
		_, err := export(t, f.a, p)
		wantErr(t, err, creativeops.ErrValidation)
		if (name == "missing" || name == "mismatch") && !strings.Contains(publicMessage(err), "导出背景设置不一致") {
			t.Fatalf("%s message = %q, want mismatch guidance", name, publicMessage(err))
		}
	}
	if got := canvasNodeCount(t, f.a, sc.canvas); got != beforeCount {
		t.Fatalf("rejected exports placed nodes: %d != %d", got, beforeCount)
	}
	// Staged upload clayUpload must still be unconsumed so retrying with true succeeds.
	okPayload := exportPayload(sc, revOn, shot(cam, img("clay", clayUpload)))
	okPayload["effective_transparent_background"] = true
	if res, err := export(t, f.a, okPayload); err != nil || len(res.Nodes) != 1 {
		t.Fatalf("retry with unconsumed upload = %+v, err=%v", res, err)
	}
	// Line-only export against revOn without effective_transparent_background succeeds.
	if res, err := export(t, f.a, exportPayload(sc, revOn, shot(cam, img("line", stage(t, f, f.a, "image"))))); err != nil || len(res.Nodes) != 1 {
		t.Fatalf("line-only omit report = %+v, err=%v", res, err)
	}
}
