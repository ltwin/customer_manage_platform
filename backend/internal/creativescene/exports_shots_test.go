package creativescene_test

import (
	"testing"

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
