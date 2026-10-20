package creativescene_test

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/creativedocument"
	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func exportJob(t *testing.T, scope store.AccountScope, in creativescene.ExportJobInput) (creativescene.Job, error) {
	t.Helper()
	if in.RunID == "" {
		in.RunID = "ccrn_test"
	}
	if in.ToolCallID == "" {
		in.ToolCallID = "step-" + uuid.NewString()
	}
	var job creativescene.Job
	err := scope.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		var err error
		job, err = creativescene.CreateExportJobInTx(t.Context(), tx, in)
		return err
	})
	return job, err
}

func exportRequest(t *testing.T, job creativescene.Job) creativescene.ExportRequest {
	t.Helper()
	var req creativescene.ExportRequest
	if err := json.Unmarshal(job.Request, &req); err != nil {
		t.Fatal(err)
	}
	return req
}

func canvasNodeCount(t *testing.T, scope store.AccountScope, canvas string) int {
	t.Helper()
	c, err := creativecanvas.GetCanvas(t.Context(), scope, canvas)
	if err != nil {
		t.Fatal(err)
	}
	return len(c.Nodes)
}

// export_shots fixes the cameras, passes and the scene node when the tool
// is called; a scene that is not on the run's canvas makes no job.
func TestCreateExportJob(t *testing.T) {
	f := setup(t)
	sc := newSceneCanvas(t, f.a)
	b, a := objectID(2), objectID(1)
	addObjects(t, f.a, sc.doc, withContent(b, "camera", 1, map[string]any{}), withContent(a, "camera", 2, map[string]any{}), box(objectID(3), nil, 3))

	job, err := exportJob(t, f.a, creativescene.ExportJobInput{DocumentID: sc.doc, CanvasID: sc.canvas, ToolCallID: "step-all"})
	if err != nil || job.Kind != creativescene.KindExportShots {
		t.Fatalf("job %+v %v", job, err)
	}
	req := exportRequest(t, job)
	if !slices.Equal(req.CameraObjectIDs, []string{b, a}) || !slices.Equal(req.Passes, []string{"clay", "line"}) || req.CanvasID != sc.canvas || req.SceneNodeID != sc.node {
		t.Fatalf("request %+v", req)
	}
	again, err := exportJob(t, f.a, creativescene.ExportJobInput{DocumentID: sc.doc, CanvasID: sc.canvas, ToolCallID: "step-all"})
	if err != nil || again.ID != job.ID {
		t.Fatalf("same tool call made %s, %v", again.ID, err)
	}
	picked, err := exportJob(t, f.a, creativescene.ExportJobInput{DocumentID: sc.doc, CanvasID: sc.canvas, CameraObjectIDs: []string{a}, Passes: []string{"line", "clay"}})
	if err != nil {
		t.Fatal(err)
	}
	if req := exportRequest(t, picked); !slices.Equal(req.CameraObjectIDs, []string{a}) || !slices.Equal(req.Passes, []string{"clay", "line"}) {
		t.Fatalf("picked request %+v", req)
	}
	lit, err := exportJob(t, f.a, creativescene.ExportJobInput{DocumentID: sc.doc, CanvasID: sc.canvas, CameraObjectIDs: []string{a}, Passes: []string{"lit", "clay"}})
	if err != nil {
		t.Fatal(err)
	}
	if req := exportRequest(t, lit); !slices.Equal(req.Passes, []string{"clay", "lit"}) {
		t.Fatalf("lit request %+v", req)
	}

	empty := newSceneCanvas(t, f.a)
	elsewhere := createScene(t, f.a)
	addObjects(t, f.a, elsewhere, withContent(objectID(9), "camera", 1, map[string]any{}))
	for name, in := range map[string]creativescene.ExportJobInput{
		"not on the canvas": {DocumentID: elsewhere, CanvasID: sc.canvas},
		"no camera":         {DocumentID: empty.doc, CanvasID: empty.canvas},
		"not a camera":      {DocumentID: sc.doc, CanvasID: sc.canvas, CameraObjectIDs: []string{objectID(3)}},
		"repeated camera":   {DocumentID: sc.doc, CanvasID: sc.canvas, CameraObjectIDs: []string{a, a}},
		"unknown pass":      {DocumentID: sc.doc, CanvasID: sc.canvas, Passes: []string{"depth"}},
		"repeated pass":     {DocumentID: sc.doc, CanvasID: sc.canvas, Passes: []string{"lit", "lit"}},
		"no canvas":         {DocumentID: sc.doc},
	} {
		if _, err := exportJob(t, f.a, in); !errors.Is(err, creativeops.ErrValidation) {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

// AC-14: render_view looks through the camera's depth of field only when
// asked, only with a camera, and only when that camera has it on.
func TestRenderViewDepthOfField(t *testing.T) {
	f := setup(t)
	doc := createScene(t, f.a)
	on, off := objectID(1), objectID(2)
	addObjects(t, f.a, doc, withContent(on, "camera", 1, map[string]any{"depth_of_field": true, "f_stop": 1.4}), withContent(off, "camera", 2, map[string]any{}))
	render := func(in creativescene.RenderInput) (creativescene.Job, error) {
		in.DocumentID, in.RunID, in.ToolCallID = doc, "ccrn_agent", "step-"+uuid.NewString()
		var job creativescene.Job
		err := f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
			var err error
			job, err = creativescene.CreateRenderJobInTx(t.Context(), tx, in)
			return err
		})
		return job, err
	}
	plain, err := render(creativescene.RenderInput{CameraObjectID: on})
	if err != nil || string(plain.Request) != `{"camera_object_id":"`+on+`","mode":"clay"}` {
		t.Fatalf("plain request %s %v", plain.Request, err)
	}
	blurred, err := render(creativescene.RenderInput{CameraObjectID: on, DepthOfField: true})
	if err != nil {
		t.Fatal(err)
	}
	var req creativescene.RenderRequest
	if err := json.Unmarshal(blurred.Request, &req); err != nil || !req.DepthOfField {
		t.Fatalf("depth of field request %s", blurred.Request)
	}
	lit, err := render(creativescene.RenderInput{CameraObjectID: on, Mode: "lit"})
	if err != nil || string(lit.Request) != `{"camera_object_id":"`+on+`","mode":"lit"}` {
		t.Fatalf("lit request %s %v", lit.Request, err)
	}
	for name, in := range map[string]creativescene.RenderInput{
		"preset":     {Preset: "overview", DepthOfField: true},
		"viewpoint":  {Viewpoint: &creativescene.Viewpoint{Position: [3]float64{5, 2, 5}, Target: [3]float64{0, 1, 0}, FocalMM: 35}, DepthOfField: true},
		"camera off": {CameraObjectID: off, DepthOfField: true},
		"bad mode":   {CameraObjectID: on, Mode: "line"},
	} {
		if _, err := render(in); !errors.Is(err, creativeops.ErrValidation) {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

// exportFixture is a claimed export_shots job of one camera, clay and line.
type exportFixture struct {
	sc      sceneCanvas
	cam     string
	job     creativescene.Job
	claimed *creativescene.ClaimedJob
	waiter  *fakeWaiter
}

func newExportFixture(t *testing.T, f fixture) exportFixture {
	t.Helper()
	waiter := &fakeWaiter{}
	creativescene.SetJobWaiterForTest(t, waiter)
	sc := newSceneCanvas(t, f.a)
	cam := objectID(1)
	addObjects(t, f.a, sc.doc, named(withContent(cam, "camera", 1, map[string]any{}), "机位 A"))
	job, err := exportJob(t, f.a, creativescene.ExportJobInput{DocumentID: sc.doc, CanvasID: sc.canvas})
	if err != nil {
		t.Fatal(err)
	}
	return exportFixture{sc: sc, cam: cam, job: job, claimed: claimOne(t, f.a), waiter: waiter}
}

func (x exportFixture) complete(t *testing.T, f fixture, token string, shots ...any) (creativescene.CompleteResponse, error) {
	t.Helper()
	if len(shots) == 0 {
		shots = []any{shot(x.cam, img("line", stage(t, f, f.a, "image")), img("clay", stage(t, f, f.a, "image")))}
	}
	rev := readScene(t, f.a, x.sc.doc).Revision
	return completeExportJob(t, f.a, map[string]any{"job_id": x.job.ID, "claim_token": token, "outcome": "succeeded",
		"export": map[string]any{"scene_revision": jsonRevision(rev), "shots": shots}})
}

func completeExportJob(t *testing.T, scope store.AccountScope, in map[string]any) (creativescene.CompleteResponse, error) {
	t.Helper()
	receipt, err := creativescene.Complete(t.Context(), scope, command(t, in))
	if err != nil {
		return creativescene.CompleteResponse{}, err
	}
	var out struct {
		JobID  string          `json:"job_id"`
		State  string          `json:"state"`
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(receipt.Outcome.Response, &out); err != nil {
		t.Fatal(err)
	}
	resp := creativescene.CompleteResponse{JobID: out.JobID, State: out.State}
	if out.State == creativescene.JobSucceeded {
		resp.Result.Export = &creativescene.ExportResult{}
		_ = json.Unmarshal(out.Result, resp.Result.Export)
	} else {
		resp.Result.Failure = &creativescene.JobFailure{}
		_ = json.Unmarshal(out.Result, resp.Result.Failure)
	}
	return resp, nil
}

// AC-7, E4: the nodes are placed in the completion's transaction, as the run,
// with the job's success and the run going on.
func TestCompleteExportPlacesTheNodes(t *testing.T) {
	f := setup(t)
	x := newExportFixture(t, f)
	before := canvasNodeCount(t, f.a, x.sc.canvas)

	// Shots that are not what the job asked for leave the job as it was.
	if _, err := x.complete(t, f, x.claimed.ClaimToken, shot(x.cam, img("clay", stage(t, f, f.a, "image")))); !errors.Is(err, creativeops.ErrValidation) {
		t.Fatalf("missing pass: %v", err)
	}
	if _, err := x.complete(t, f, x.claimed.ClaimToken, shot(objectID(7), img("clay", "a"), img("line", "b"))); !errors.Is(err, creativeops.ErrValidation) {
		t.Fatalf("other camera: %v", err)
	}
	// A staged image that expired rolls everything back.
	used := stage(t, f, f.a, "image")
	if _, err := f.db.Exec(`UPDATE creative_upload_candidates SET expires_at=clock_timestamp()-interval '1 second' WHERE upload_id=$1`, used); err != nil {
		t.Fatal(err)
	}
	if _, err := x.complete(t, f, x.claimed.ClaimToken, shot(x.cam, img("clay", used), img("line", stage(t, f, f.a, "image")))); !errors.Is(err, creativescene.ErrUploadUnavailable) {
		t.Fatalf("expired upload: %v", err)
	}
	if state, _, _, _ := jobRow(t, f, x.job.ID); state != creativescene.JobClaimed || canvasNodeCount(t, f.a, x.sc.canvas) != before {
		t.Fatalf("a refused completion changed the job (%s) or the canvas", state)
	}

	resp, err := x.complete(t, f, x.claimed.ClaimToken)
	if err != nil || resp.State != creativescene.JobSucceeded || resp.Result.Export == nil || len(resp.Result.Export.Nodes) != 2 {
		t.Fatalf("complete %+v %v", resp, err)
	}
	nodes := resp.Result.Export.Nodes
	if nodes[0].Pass != "clay" || nodes[1].Pass != "line" || canvasNodeCount(t, f.a, x.sc.canvas) != before+2 {
		t.Fatalf("nodes %+v", nodes)
	}
	n := canvasNode(t, f.a, x.sc.canvas, nodes[0].NodeID)
	if n.Title != "机位 A · clay" || basedOn(t, n)["camera_object_id"] != x.cam {
		t.Fatalf("node %+v", n)
	}
	if len(x.waiter.results) != 1 || x.waiter.results[0].Export == nil || x.waiter.results[0].Export.ChangeID != resp.Result.Export.ChangeID {
		t.Fatalf("settled %+v", x.waiter.results)
	}
}

func TestCompleteExportThatCannotBePlacedFailsTheJob(t *testing.T) {
	for name, break_ := range map[string]func(t *testing.T, f fixture, x exportFixture){
		"canvas archived": func(t *testing.T, f fixture, x exportFixture) {
			if _, err := f.db.Exec(`UPDATE creative_projects SET archived_at=clock_timestamp() WHERE id=$1`, x.sc.project); err != nil {
				t.Fatal(err)
			}
		},
		"scene node removed": func(t *testing.T, f fixture, x exportFixture) {
			c, err := creativecanvas.GetCanvas(t.Context(), f.a, x.sc.canvas)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := creativecanvas.ApplyCommands(t.Context(), f.a, command(t, creativecanvas.BatchInput{CanvasID: x.sc.canvas, ExpectedTopologyRevision: c.TopologyRevision,
				ReadSet: creativecanvas.ReadSetFor(c), Actions: []creativecanvas.Action{{Type: "remove_nodes", NodeIDs: []string{x.sc.node}}}})); err != nil {
				t.Fatal(err)
			}
		},
		"scene trashed": func(t *testing.T, f fixture, x exportFixture) {
			if _, err := creativedocument.Trash(t.Context(), f.a, command(t, map[string]any{"document_id": x.sc.doc})); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := setup(t)
			x := newExportFixture(t, f)
			break_(t, f, x)
			before, err := creativecanvas.GetCanvas(t.Context(), f.a, x.sc.canvas)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := x.complete(t, f, x.claimed.ClaimToken)
			if err != nil || resp.State != creativescene.JobFailed || resp.Result.Failure.Code != "invalid_result" || resp.Result.Failure.Message == "" {
				t.Fatalf("complete %+v %v", resp, err)
			}
			after, err := creativecanvas.GetCanvas(t.Context(), f.a, x.sc.canvas)
			if err != nil || after.Revision != before.Revision {
				t.Fatalf("the canvas changed: %v", err)
			}
			if len(x.waiter.results) != 1 || x.waiter.results[0].Failure == nil {
				t.Fatalf("the run did not go on: %+v", x.waiter.results)
			}
		})
	}
}

// E4: a run that may no longer write, or a lost claim, places nothing; a
// job handed to another tab ends with one set of nodes.
func TestCompleteExportGatesAndHandover(t *testing.T) {
	f := setup(t)
	x := newExportFixture(t, f)
	before := canvasNodeCount(t, f.a, x.sc.canvas)

	x.waiter.refuse = creativescene.ErrRunGate
	if _, err := x.complete(t, f, x.claimed.ClaimToken); !errors.Is(err, creativescene.ErrJobLeaseLost) {
		t.Fatalf("refused run: %v", err)
	}
	x.waiter.refuse = nil
	if canvasNodeCount(t, f.a, x.sc.canvas) != before {
		t.Fatal("a refused run placed nodes")
	}

	// The first tab's lease lapses; another tab takes the job over.
	if _, err := f.db.Exec(`UPDATE creative_scene_jobs SET lease_until=now()-interval '1 second' WHERE id=$1`, x.job.ID); err != nil {
		t.Fatal(err)
	}
	sweep(t, f.a)
	second := claimOne(t, f.a)
	if _, err := x.complete(t, f, x.claimed.ClaimToken); !errors.Is(err, creativescene.ErrJobLeaseLost) {
		t.Fatalf("the first tab completed after losing the lease: %v", err)
	}
	if resp, err := x.complete(t, f, second.ClaimToken); err != nil || resp.State != creativescene.JobSucceeded {
		t.Fatalf("second tab %+v %v", resp, err)
	}
	if _, err := x.complete(t, f, second.ClaimToken); !errors.Is(err, creativescene.ErrJobLeaseLost) {
		t.Fatalf("a second completion: %v", err)
	}
	if got := canvasNodeCount(t, f.a, x.sc.canvas); got != before+2 {
		t.Fatalf("canvas has %d nodes, want %d", got, before+2)
	}
}
