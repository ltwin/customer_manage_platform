package creativescene_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// fakeWaiter stands in for the agent runtime: it records what settles and can
// refuse a run.
type fakeWaiter struct {
	mu      sync.Mutex
	refuse  error
	settled []creativescene.Job
	results []creativescene.JobResult
	aborted []string
}

func (w *fakeWaiter) Admit(_ context.Context, _ store.TxAccountScope, runID string) (creativescene.RunGate, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.refuse != nil {
		return nil, w.refuse
	}
	return runID, nil
}

func (w *fakeWaiter) Settle(_ context.Context, _ store.TxAccountScope, gate creativescene.RunGate, job creativescene.Job, res creativescene.JobResult) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if gate != *job.RunID {
		return errors.New("settled with another run's gate")
	}
	w.settled = append(w.settled, job)
	w.results = append(w.results, res)
	return nil
}

func (w *fakeWaiter) Abort(_ context.Context, _ store.AccountScope, runID string, _ error) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.aborted = append(w.aborted, runID)
	return nil
}

var pillarModule = json.RawMessage(`{"params_schema":{"height":{"type":"number","default":1,"min":0.5,"max":4,"label":"高度"}},"params":{"height":2},"mount":"floor"}`)

func claimOne(t *testing.T, scope store.AccountScope) *creativescene.ClaimedJob {
	t.Helper()
	got, err := creativescene.Claim(t.Context(), scope, creativescene.ClaimInput{EditorSessionID: tab})
	if err != nil || got == nil {
		t.Fatalf("claim %+v %v", got, err)
	}
	return got
}

func completeJob(t *testing.T, scope store.AccountScope, opID string, in map[string]any) (creativescene.CompleteResponse, error) {
	t.Helper()
	cmd := command(t, in)
	cmd.OperationID = opID
	return completeCommand(t, scope, cmd)
}

func completeCommand(t *testing.T, scope store.AccountScope, cmd creativeops.Command) (creativescene.CompleteResponse, error) {
	t.Helper()
	receipt, err := creativescene.Complete(t.Context(), scope, cmd)
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
	switch {
	case out.State != creativescene.JobSucceeded:
		resp.Result.Failure = &creativescene.JobFailure{}
		_ = json.Unmarshal(out.Result, resp.Result.Failure)
	case strings.Contains(string(out.Result), "image_upload_id"):
		resp.Result.Render = &creativescene.RenderResult{}
		_ = json.Unmarshal(out.Result, resp.Result.Render)
	default:
		resp.Result.Bake = &creativescene.BakeResult{}
		_ = json.Unmarshal(out.Result, resp.Result.Bake)
	}
	return resp, nil
}

func bakeOutput(u uploads, manifest any) map[string]any {
	return map[string]any{"glb_upload_id": u.glb, "thumbnail_upload_id": u.thumb, "manifest": manifest, "module": pillarModule}
}

func addJob(doc, id string) creativescene.NewJob {
	object, _ := json.Marshal(map[string]any{"id": id, "name": "柱", "parent_id": nil, "transform": at(1, 0, 2)})
	return creativescene.NewJob{DocumentID: doc, Kind: creativescene.KindBakeModel, RunID: "ccrn_test", ToolCallID: "step-" + id,
		Request: creativescene.BakeRequest{Code: pillarCode, Params: map[string]any{"height": 2.0}, LibVersion: 1, NameHint: "立柱",
			Target: creativescene.BakeTarget{Op: "add", Object: object}, Expected: map[string]*creativeops.Revision{}}}
}

func TestCompleteBakeAddsTheModelAsTheRun(t *testing.T) {
	f := setup(t)
	waiter := &fakeWaiter{}
	creativescene.SetJobWaiterForTest(t, waiter)
	doc := createScene(t, f.a)
	addObjects(t, f.a, doc, box(objectID(9), nil, 1))
	job := newJob(t, f.a, addJob(doc, objectID(1)))
	claimed := claimOne(t, f.a)
	u := stagePair(t, f, f.a)
	in := map[string]any{"job_id": job.ID, "claim_token": claimed.ClaimToken, "outcome": "succeeded", "bake": bakeOutput(u, basicManifest(t))}
	cmd := command(t, in)
	resp, err := completeCommand(t, f.a, cmd)
	if err != nil {
		t.Fatal(err)
	}
	if resp.State != creativescene.JobSucceeded || resp.Result.Bake == nil || resp.Result.Bake.ContentRevisionID == "" || len(resp.Result.Bake.ObjectIDs) != 1 || resp.Result.Bake.ObjectIDs[0] != objectID(1) {
		t.Fatalf("response %+v %+v", resp, resp.Result.Bake)
	}
	v, models := readModels(t, f.a, doc)
	placed := v.object(t, objectID(1))
	if placed.Model == nil || placed.Model.ContentRevisionID != resp.Result.Bake.ContentRevisionID || placed.Order != 2 || placed.Kind != creativescene.KindModelInstance {
		t.Fatalf("placed %+v", placed)
	}
	if m := models[placed.Model.ContentRevisionID]; !m.Usable || m.Generator == nil || m.Generator.Params["height"] != 2.0 || m.Generator.Mount != "floor" {
		t.Fatalf("model %+v", m)
	}
	var actor, runID string
	if err := f.db.QueryRow(`SELECT actor_kind, run_id FROM creative_document_revisions WHERE document_id=$1 AND revision=$2`, doc, int64(resp.Result.Bake.Revision)).Scan(&actor, &runID); err != nil || actor != "agent" || runID != "ccrn_test" {
		t.Fatalf("revision writer %s %s %v", actor, runID, err)
	}
	if len(waiter.settled) != 1 || waiter.settled[0].State != creativescene.JobSucceeded || waiter.results[0].Bake == nil {
		t.Fatalf("settled %+v", waiter.settled)
	}
	// A retried request replays the receipt and settles nothing twice.
	again, err := completeCommand(t, f.a, cmd)
	if err != nil || again.State != creativescene.JobSucceeded || len(waiter.settled) != 1 {
		t.Fatalf("replay %+v %v settled=%d", again, err, len(waiter.settled))
	}
}

func TestCompleteBakeReplaceConflictKeepsTheBake(t *testing.T) {
	f := setup(t)
	waiter := &fakeWaiter{}
	creativescene.SetJobWaiterForTest(t, waiter)
	doc := createScene(t, f.a)
	placed := newPillar(t, f, doc, objectID(1), 1)
	before := readScene(t, f.a, doc)
	n, err := strconv.ParseInt(before.UnitRevs[content(objectID(1))], 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	frozen := creativeops.Revision(n)
	job := newJob(t, f.a, creativescene.NewJob{DocumentID: doc, Kind: creativescene.KindBakeModel, RunID: "ccrn_test", ToolCallID: "step-1",
		Request: creativescene.BakeRequest{Code: pillarCode, Params: map[string]any{"height": 3.0}, LibVersion: 1,
			Target:   creativescene.BakeTarget{Op: "replace", ObjectID: objectID(1), Scope: "this", BaseContentID: placed.ContentID},
			Expected: map[string]*creativeops.Revision{content(objectID(1)): &frozen}}})
	claimed := claimOne(t, f.a)

	// While it bakes, the photographer swaps the model of the same instance.
	other := newPillar(t, f, doc, objectID(2), 2)
	mustSend(t, f.a, batch{DocumentID: doc, Expected: readScene(t, f.a, doc).expect(content(objectID(1))), Operations: []any{
		op{"op": "set_instance_model", "object_id": objectID(1), "model": map[string]any{"content_revision_id": other.ContentRevisionID}},
	}})
	head := readScene(t, f.a, doc).Revision

	u := stagePair(t, f, f.a)
	resp, err := completeJob(t, f.a, uuid.NewString(), map[string]any{"job_id": job.ID, "claim_token": claimed.ClaimToken, "outcome": "succeeded", "bake": bakeOutput(u, basicManifest(t))})
	if err != nil {
		t.Fatal(err)
	}
	fail := resp.Result.Failure
	if resp.State != creativescene.JobFailed || fail == nil || fail.Code != "conflict" || len(fail.Conflicts) != 1 || fail.Conflicts[0].Unit != content(objectID(1)) || fail.Bake == nil || fail.Bake.GLBUploadID != u.glb {
		t.Fatalf("conflict result %+v %+v", resp, fail)
	}
	after := readScene(t, f.a, doc)
	if after.Revision != head || after.object(t, objectID(1)).Model.ContentRevisionID != other.ContentRevisionID {
		t.Fatal("the conflicting bake was written")
	}
	// The staged uploads were not consumed: a retry can still use them.
	var pending int
	if err := f.db.QueryRow(`SELECT count(*) FROM creative_upload_candidates WHERE upload_id IN ($1,$2) AND state='pending'`, u.glb, u.thumb).Scan(&pending); err != nil || pending != 2 {
		t.Fatalf("pending uploads %d %v", pending, err)
	}
	if len(waiter.results) != 1 || waiter.results[0].Failure == nil || waiter.results[0].Failure.Code != "conflict" {
		t.Fatalf("settled %+v", waiter.results)
	}
}

func TestCompleteRefusals(t *testing.T) {
	f := setup(t)
	waiter := &fakeWaiter{}
	creativescene.SetJobWaiterForTest(t, waiter)
	doc := createScene(t, f.a)

	// A manifest the GLB does not have: the job fails, the scene does not change.
	job := newJob(t, f.a, addJob(doc, objectID(1)))
	claimed := claimOne(t, f.a)
	wrong := basicManifest(t)
	wrong.Triangles++
	u := stagePair(t, f, f.a)
	head := readScene(t, f.a, doc).Revision
	resp, err := completeJob(t, f.a, uuid.NewString(), map[string]any{"job_id": job.ID, "claim_token": claimed.ClaimToken, "outcome": "succeeded", "bake": bakeOutput(u, wrong)})
	if err != nil || resp.State != creativescene.JobFailed || resp.Result.Failure.Code != "invalid_result" || resp.Result.Failure.Message == "" {
		t.Fatalf("invalid result %+v %v", resp, err)
	}
	if readScene(t, f.a, doc).Revision != head {
		t.Fatal("an invalid bake was written")
	}

	// A staged upload that is gone rolls everything back; the job stays claimed.
	job = newJob(t, f.a, addJob(doc, objectID(2)))
	claimed = claimOne(t, f.a)
	_, err = completeJob(t, f.a, uuid.NewString(), map[string]any{"job_id": job.ID, "claim_token": claimed.ClaimToken, "outcome": "succeeded",
		"bake": bakeOutput(uploads{glb: "ccup_missing", thumb: u.thumb}, basicManifest(t))})
	wantErr(t, err, creativescene.ErrUploadUnavailable)
	if list, _ := creativescene.ListActive(t.Context(), f.a, doc); len(list) != 1 || list[0].State != creativescene.JobClaimed {
		t.Fatalf("job after an unavailable upload: %+v", list)
	}

	// Someone else's token, and malformed requests, change nothing.
	_, err = completeJob(t, f.a, uuid.NewString(), map[string]any{"job_id": job.ID, "claim_token": "clm_wrong", "outcome": "failed", "error": map[string]any{"code": "compile_error", "message": "x"}})
	wantErr(t, err, creativescene.ErrJobLeaseLost)
	for name, in := range map[string]map[string]any{
		"success without result": {"job_id": job.ID, "claim_token": claimed.ClaimToken, "outcome": "succeeded"},
		"unknown error code":     {"job_id": job.ID, "claim_token": claimed.ClaimToken, "outcome": "failed", "error": map[string]any{"code": "oops", "message": "x"}},
		"failure with a result":  {"job_id": job.ID, "claim_token": claimed.ClaimToken, "outcome": "failed", "error": map[string]any{"code": "host_error", "message": "x"}, "bake": bakeOutput(u, basicManifest(t))},
		"render for a bake job":  {"job_id": job.ID, "claim_token": claimed.ClaimToken, "outcome": "succeeded", "render": map[string]any{"image_upload_id": "ccup_x", "scene_revision": "1"}},
	} {
		if _, err := completeJob(t, f.a, uuid.NewString(), in); !errors.Is(err, creativeops.ErrValidation) {
			t.Errorf("%s: %v", name, err)
		}
	}

	// The host's own failure is recorded and goes to the run.
	resp, err = completeJob(t, f.a, uuid.NewString(), map[string]any{"job_id": job.ID, "claim_token": claimed.ClaimToken, "outcome": "failed",
		"error": map[string]any{"code": "compile_error", "message": "ReferenceError: x is not defined (line 3)"}})
	if err != nil || resp.State != creativescene.JobFailed || resp.Result.Failure.Code != "compile_error" || !strings.Contains(resp.Result.Failure.Message, "line 3") {
		t.Fatalf("host failure %+v %v", resp, err)
	}
	if n := len(waiter.results); n != 2 || waiter.results[n-1].Failure.Code != "compile_error" {
		t.Fatalf("settled %+v", waiter.results)
	}
}

// Each transition is one log line that names the tab holding the job and how
// long the job sat in the state it leaves.
func TestJobTransitionsAreLogged(t *testing.T) {
	f := setup(t)
	creativescene.SetJobWaiterForTest(t, &fakeWaiter{})
	var out bytes.Buffer
	creativescene.SetLoggerForTest(t, slog.New(slog.NewJSONHandler(&out, nil)))
	doc := createScene(t, f.a)
	job := newJob(t, f.a, addJob(doc, objectID(1)))
	claimed := claimOne(t, f.a)
	if _, err := completeJob(t, f.a, uuid.NewString(), map[string]any{"job_id": job.ID, "claim_token": claimed.ClaimToken, "outcome": "failed",
		"error": map[string]any{"code": "compile_error", "message": "x"}}); err != nil {
		t.Fatal(err)
	}
	type line struct {
		Msg     string `json:"msg"`
		JobID   string `json:"job_id"`
		From    string `json:"from"`
		To      string `json:"to"`
		RunID   string `json:"run_id"`
		Session string `json:"editor_session_id"`
		InState *int64 `json:"in_state"`
		Since   *int64 `json:"since_created"`
		Code    string `json:"code"`
	}
	var lines []line
	for _, raw := range bytes.Split(bytes.TrimSpace(out.Bytes()), []byte("\n")) {
		var l line
		if err := json.Unmarshal(raw, &l); err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
		if l.Msg == "creative scene job" && l.JobID == job.ID {
			lines = append(lines, l)
		}
	}
	if len(lines) != 3 {
		t.Fatalf("transitions %+v", lines)
	}
	created, claim, done := lines[0], lines[1], lines[2]
	if created.To != creativescene.JobQueued || created.Session != "" || created.RunID == "" {
		t.Fatalf("created %+v", created)
	}
	if claim.From != creativescene.JobQueued || claim.To != creativescene.JobClaimed || claim.Session != tab || claim.InState == nil || claim.Since == nil {
		t.Fatalf("claimed %+v", claim)
	}
	if done.From != creativescene.JobClaimed || done.To != creativescene.JobFailed || done.Session != tab || done.InState == nil || done.Code != "compile_error" {
		t.Fatalf("completed %+v", done)
	}
}

// A run that may no longer write (consent withdrawn, skill disabled, cancelled)
// gets nothing written; the run is ended and the host told to drop its work.
func TestCompleteForARunThatFailsAdmit(t *testing.T) {
	f := setup(t)
	waiter := &fakeWaiter{refuse: creativescene.ErrRunGate}
	creativescene.SetJobWaiterForTest(t, waiter)
	doc := createScene(t, f.a)
	job := newJob(t, f.a, addJob(doc, objectID(1)))
	claimed := claimOne(t, f.a)
	head := readScene(t, f.a, doc).Revision
	_, err := completeJob(t, f.a, uuid.NewString(), map[string]any{"job_id": job.ID, "claim_token": claimed.ClaimToken, "outcome": "succeeded", "bake": bakeOutput(stagePair(t, f, f.a), basicManifest(t))})
	wantErr(t, err, creativescene.ErrJobLeaseLost)
	if len(waiter.aborted) != 1 || waiter.aborted[0] != "ccrn_test" || len(waiter.settled) != 0 {
		t.Fatalf("aborted %v settled %v", waiter.aborted, waiter.settled)
	}
	if readScene(t, f.a, doc).Revision != head {
		t.Fatal("a refused run wrote to the scene")
	}
}

func TestCompleteRenderKeepsTheScreenshotStaged(t *testing.T) {
	f := setup(t)
	waiter := &fakeWaiter{}
	creativescene.SetJobWaiterForTest(t, waiter)
	doc := createScene(t, f.a)
	job := newJob(t, f.a, creativescene.NewJob{DocumentID: doc, Kind: creativescene.KindRenderView, RunID: "ccrn_test", ToolCallID: "step-r",
		Request: creativescene.RenderRequest{Preset: "overview", Mode: "clay"}})
	claimed := claimOne(t, f.a)
	image := stage(t, f, f.a, "image")
	head := readScene(t, f.a, doc).Revision

	_, err := completeJob(t, f.a, uuid.NewString(), map[string]any{"job_id": job.ID, "claim_token": claimed.ClaimToken, "outcome": "succeeded",
		"render": map[string]any{"image_upload_id": image, "scene_revision": jsonRevision(head + 1)}})
	wantErr(t, err, creativeops.ErrValidation)
	_, err = completeJob(t, f.a, uuid.NewString(), map[string]any{"job_id": job.ID, "claim_token": claimed.ClaimToken, "outcome": "succeeded",
		"render": map[string]any{"image_upload_id": stage(t, f, f.a, "model"), "scene_revision": jsonRevision(head)}})
	wantErr(t, err, creativeops.ErrValidation)

	resp, err := completeJob(t, f.a, uuid.NewString(), map[string]any{"job_id": job.ID, "claim_token": claimed.ClaimToken, "outcome": "succeeded",
		"render": map[string]any{"image_upload_id": image, "scene_revision": jsonRevision(head)}})
	if err != nil || resp.State != creativescene.JobSucceeded || resp.Result.Render == nil || resp.Result.Render.ContentRevisionID == "" || resp.Result.Render.SceneRevision != head {
		t.Fatalf("render %+v %v", resp, err)
	}
	var state string
	if err := f.db.QueryRow(`SELECT state FROM creative_upload_candidates WHERE upload_id=$1`, image).Scan(&state); err != nil || state != "pending" {
		t.Fatalf("screenshot candidate %s %v", state, err)
	}
	if readScene(t, f.a, doc).Revision != head || len(waiter.results) != 1 || waiter.results[0].Render == nil {
		t.Fatal("a screenshot changed the scene or did not settle")
	}
}

func jsonRevision(r creativeops.Revision) string {
	raw, _ := json.Marshal(r)
	return strings.Trim(string(raw), `"`)
}
