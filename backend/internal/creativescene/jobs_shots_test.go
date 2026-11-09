package creativescene_test

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// data: 场景时间线「客户端任务的分镜字段」: render_view and export_shots by shot.

func renderJob(t *testing.T, scope store.AccountScope, doc string, in creativescene.RenderInput) (creativescene.Job, error) {
	t.Helper()
	in.DocumentID, in.RunID, in.ToolCallID = doc, "ccrn_agent", "step-"+uuid.NewString()
	var job creativescene.Job
	err := scope.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		var err error
		job, err = creativescene.CreateRenderJobInTx(t.Context(), tx, in)
		return err
	})
	return job, err
}

func TestRenderViewByShot(t *testing.T) {
	f := setup(t)
	doc, cam, _ := shotScene(t, f.a)
	// The camera's depth of field is on in shot 2 only.
	v := readScene(t, f.a, doc)
	mustSend(t, f.a, batch{DocumentID: doc, Expected: v.expect(content(cam)), Operations: []any{
		animate(cam, sets(keyAt("camera.depth_of_field", 0, false), keyAt("camera.depth_of_field", 10, true)))}})

	job, err := renderJob(t, f.a, doc, creativescene.RenderInput{MarkerID: markerID(2), DepthOfField: true})
	if err != nil || string(job.Request) != `{"marker_id":"`+markerID(2)+`","mode":"clay","depth_of_field":true}` {
		t.Fatalf("shot request %s %v", job.Request, err)
	}
	framed, err := renderJob(t, f.a, doc, creativescene.RenderInput{Preset: "overview", Frame: 10})
	if err != nil || string(framed.Request) != `{"preset":"overview","frame":10,"mode":"clay"}` {
		t.Fatalf("framed request %s %v", framed.Request, err)
	}
	if _, err := renderJob(t, f.a, doc, creativescene.RenderInput{CameraObjectID: cam, Frame: 10, DepthOfField: true}); err != nil {
		t.Fatalf("camera at frame 10 with depth of field: %v", err)
	}

	v = readScene(t, f.a, doc)
	mustSend(t, f.a, batch{DocumentID: doc, Expected: v.expect("timeline", content(cam)), Operations: []any{
		setMarkers(shotMarker(1, 0, cam), shotMarker(2, 10, cam), shotMarker(3, 20, "")),
		animate(cam, sets(keyAt("camera.depth_of_field", 20, false)))}})
	for name, c := range map[string]struct {
		in   creativescene.RenderInput
		want string
	}{
		"depth of field off in shot 1":    {creativescene.RenderInput{MarkerID: markerID(1), DepthOfField: true}, "没有打开景深"},
		"depth of field off at frame 0":   {creativescene.RenderInput{CameraObjectID: cam, DepthOfField: true}, "没有打开景深"},
		"shot without a camera":           {creativescene.RenderInput{MarkerID: markerID(3)}, "未绑定摄像机"},
		"missing shot":                    {creativescene.RenderInput{MarkerID: markerID(9)}, "不存在"},
		"shot and camera":                 {creativescene.RenderInput{MarkerID: markerID(1), CameraObjectID: cam}, "其中一个"},
		"shot and frame":                  {creativescene.RenderInput{MarkerID: markerID(1), Frame: 5}, "不要再写 frame"},
		"frame out of range":              {creativescene.RenderInput{Preset: "top", Frame: 100001}, "frame 须在"},
		"depth of field through a preset": {creativescene.RenderInput{Preset: "top", DepthOfField: true}, "只能与分镜或 camera_object_id"},
	} {
		if _, err := renderJob(t, f.a, doc, c.in); !errors.Is(err, creativeops.ErrValidation) || !strings.Contains(publicMessage(err), c.want) {
			t.Errorf("%s: %v, want it to say %q", name, err, c.want)
		}
	}
}

func TestExportShotsByShot(t *testing.T) {
	f := setup(t)
	sc := newSceneCanvas(t, f.a)
	cam, other := objectID(1), objectID(2)
	addObjects(t, f.a, sc.doc, camera(cam, 1), camera(other, 2))
	v := readScene(t, f.a, sc.doc)
	mustSend(t, f.a, batch{DocumentID: sc.doc, Expected: v.expect("timeline"), Operations: []any{
		setMarkers(shotMarker(2, 10, cam), shotMarker(1, 0, other))}})

	// Left out, shots means every shot in frame order.
	job, err := exportJob(t, f.a, creativescene.ExportJobInput{DocumentID: sc.doc, CanvasID: sc.canvas})
	if err != nil {
		t.Fatal(err)
	}
	if req := exportRequest(t, job); !slices.Equal(req.MarkerIDs, []string{markerID(1), markerID(2)}) || req.CameraObjectIDs != nil {
		t.Fatalf("default request %+v", req)
	}
	if _, err := exportJob(t, f.a, creativescene.ExportJobInput{DocumentID: sc.doc, CanvasID: sc.canvas, CameraObjectIDs: []string{cam}}); err != nil {
		t.Fatalf("by camera on a scene with shots: %v", err)
	}

	for name, c := range map[string]struct {
		in   creativescene.ExportJobInput
		want string
	}{
		"shots and cameras": {creativescene.ExportJobInput{MarkerIDs: []string{markerID(1)}, CameraObjectIDs: []string{cam}}, "只能给一个"},
		"repeated shot":     {creativescene.ExportJobInput{MarkerIDs: []string{markerID(1), markerID(1)}}, "重复"},
		"missing shot":      {creativescene.ExportJobInput{MarkerIDs: []string{markerID(9)}}, "不存在"},
	} {
		c.in.DocumentID, c.in.CanvasID = sc.doc, sc.canvas
		if _, err := exportJob(t, f.a, c.in); !errors.Is(err, creativeops.ErrValidation) || !strings.Contains(publicMessage(err), c.want) {
			t.Errorf("%s: %v, want it to say %q", name, err, c.want)
		}
	}

	// Every shot without a camera is listed, the deleted camera's too.
	remove(t, f.a, sc.doc, []string{other}, other)
	v = readScene(t, f.a, sc.doc)
	mustSend(t, f.a, batch{DocumentID: sc.doc, Expected: v.expect("timeline"), Operations: []any{
		setMarkers(shotMarker(1, 0, ""), shotMarker(2, 10, cam), shotMarker(3, 20, ""))}})
	_, err = exportJob(t, f.a, creativescene.ExportJobInput{DocumentID: sc.doc, CanvasID: sc.canvas})
	if msg := publicMessage(err); !errors.Is(err, creativeops.ErrValidation) || !strings.Contains(msg, markerID(1)) || !strings.Contains(msg, markerID(3)) || strings.Contains(msg, markerID(2)) {
		t.Fatalf("unbound shots: %v", err)
	}
}

// The host's shots must be the job's shots; the nodes carry them.
func TestCompleteExportByShot(t *testing.T) {
	f := setup(t)
	waiter := &fakeWaiter{}
	creativescene.SetJobWaiterForTest(t, waiter)
	sc := newSceneCanvas(t, f.a)
	cam := objectID(1)
	addObjects(t, f.a, sc.doc, camera(cam, 1))
	v := readScene(t, f.a, sc.doc)
	mustSend(t, f.a, batch{DocumentID: sc.doc, Expected: v.expect("timeline"), Operations: []any{setMarkers(shotMarker(1, 0, cam), shotMarker(2, 10, cam))}})
	job, err := exportJob(t, f.a, creativescene.ExportJobInput{DocumentID: sc.doc, CanvasID: sc.canvas, Passes: []string{"clay"}})
	if err != nil {
		t.Fatal(err)
	}
	claimed := claimOne(t, f.a)
	complete := func(shots ...any) (creativescene.CompleteResponse, error) {
		rev := readScene(t, f.a, sc.doc).Revision
		return completeExportJob(t, f.a, map[string]any{"job_id": job.ID, "claim_token": claimed.ClaimToken, "outcome": "succeeded",
			"export": map[string]any{"scene_revision": jsonRevision(rev), "shots": shots}})
	}
	if _, err := complete(shot(cam, img("clay", stage(t, f, f.a, "image")))); !errors.Is(err, creativeops.ErrValidation) {
		t.Fatalf("cameras for a job of shots: %v", err)
	}
	resp, err := complete(shotExport(markerID(2), img("clay", stage(t, f, f.a, "image"))), shotExport(markerID(1), img("clay", stage(t, f, f.a, "image"))))
	if err != nil || resp.State != creativescene.JobSucceeded || len(resp.Result.Export.Nodes) != 2 {
		t.Fatalf("complete %+v %v", resp, err)
	}
	nodes := resp.Result.Export.Nodes
	if *nodes[0].MarkerID != markerID(1) || *nodes[1].MarkerID != markerID(2) {
		raw, _ := json.Marshal(nodes)
		t.Fatalf("nodes %s, want rows in the job's shot order", raw)
	}
}
