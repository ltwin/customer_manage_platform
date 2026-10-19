package creativeagent

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"

	"github.com/samson/customer-manage-platform/backend/internal/creativedocument"
	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// parkingTool stands in for write_model and render_view. Appended after the
// runtime handler it is the tool itself: it hands the call to a page as a
// client job, keyed by the call's step, and parks. onPark runs once the job
// exists, before the call returns.
type parkingTool struct {
	*adk.BaseChatModelAgentMiddleware
	f      *fixture
	runID  string
	docID  string
	onPark func(job creativescene.Job, step string)
	mu     sync.Mutex
	calls  int
}

func (p *parkingTool) WrapInvokableToolCall(_ context.Context, _ adk.InvokableToolCallEndpoint, _ *adk.ToolContext) (adk.InvokableToolCallEndpoint, error) {
	return func(ctx context.Context, _ string, _ ...tool.Option) (string, error) {
		p.mu.Lock()
		p.calls++
		p.mu.Unlock()
		step := toolStepFrom(ctx)
		if step == "" {
			return "", errors.New("parking tool ran without its step")
		}
		var job creativescene.Job
		if err := p.f.alice.WithTxScope(ctx, func(tx store.TxAccountScope) error {
			var err error
			job, err = creativescene.CreateJob(ctx, tx, creativescene.NewJob{DocumentID: p.docID, Kind: creativescene.KindRenderView, RunID: p.runID, ToolCallID: step,
				Request: creativescene.RenderRequest{Preset: "overview", Mode: "clay"}})
			return err
		}); err != nil {
			return "", err
		}
		if p.onPark != nil {
			p.onPark(job, step)
		}
		return "", errParked{jobID: job.ID}
	}, nil
}

func (f *fixture) sceneDocument() string {
	f.t.Helper()
	typ, ok := creativedocument.Lookup(creativescene.TypeKey)
	if !ok {
		f.t.Fatal("scene type not registered")
	}
	var doc creativedocument.Document
	if err := f.alice.WithTxScope(f.t.Context(), func(tx store.TxAccountScope) error {
		var err error
		doc, err = creativedocument.CreateInTx(f.t.Context(), tx, typ, "中殿", nil, creativedocument.Photographer)
		return err
	}); err != nil {
		f.t.Fatal(err)
	}
	return doc.ID
}

func readResultCall(id, item string) llmgateway.ToolCall {
	return llmgateway.ToolCall{ID: id, Name: readResultTool, Arguments: json.RawMessage(`{"item_id":"` + item + `","offset":0,"limit":100}`)}
}

// startParkingRun starts a run whose first answer makes the given calls and
// whose second answer finishes, and executes the first attempt with a parking
// tool.
func startParkingRun(t *testing.T, f *fixture, onPark func(*runExecution) func(creativescene.Job, string), calls ...llmgateway.ToolCall) (Run, claim, *runExecution, *parkingTool, error) {
	t.Helper()
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	consent := f.consent(f.alice, c.ID)
	doc := f.sceneDocument()
	f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, n int) (llmgateway.Result, error) {
		if n == 1 {
			return scriptedResult(r, calls...), nil
		}
		return scriptedResult(r), nil
	}
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "搭一个中殿"})
	if err != nil {
		t.Fatal(err)
	}
	held, err := f.service.claimRun(t.Context(), f.alice, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	execution, err := f.service.newRunExecution(t.Context(), f.alice, held)
	if err != nil {
		t.Fatal(err)
	}
	parking := &parkingTool{BaseChatModelAgentMiddleware: &adk.BaseChatModelAgentMiddleware{}, f: f, runID: run.ID, docID: doc}
	if onPark != nil {
		parking.onPark = onPark(execution)
	}
	execution.config.Handlers = append(execution.config.Handlers, parking)
	_, err = execution.run(t.Context())
	return run, held, execution, parking, err
}

// settleParked is the job's completion as the runtime sees it: the job ends
// and the parked call's result is written exactly as callTool writes one.
func (f *fixture) settleParked(e *runExecution, jobID, step string) {
	f.t.Helper()
	if _, err := f.db.Exec(`UPDATE creative_scene_jobs SET state='failed', result='{"code":"host_error","message":"页面崩溃"}' WHERE id=$1`, jobID); err != nil {
		f.t.Fatal(err)
	}
	var parent string
	var index int
	var raw []byte
	if err := f.db.QueryRow(`SELECT parent_model_step_id, tool_call_index, input FROM creative_agent_steps WHERE id=$1`, step).Scan(&parent, &index, &raw); err != nil {
		f.t.Fatal(err)
	}
	var call llmgateway.ToolCall
	if err := json.Unmarshal(raw, &call); err != nil {
		f.t.Fatal(err)
	}
	if err := f.alice.WithTxScope(f.t.Context(), func(tx store.TxAccountScope) error {
		_, err := e.runtime.publishResultInTx(f.t.Context(), tx, publishedCall{StepID: step, CallID: runtimeCallID(parent, index), Name: call.Name, Args: string(call.Arguments),
			Result: `{"success":false,"code":"host_error","message":"页面崩溃"}`, Failed: true, Code: "creative_tool_failed"})
		return err
	}); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) runJobs(runID string) map[string]string {
	f.t.Helper()
	rows, err := f.db.Query(`SELECT id, tool_call_id FROM creative_scene_jobs WHERE run_id=$1`, runID)
	if err != nil {
		f.t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	jobs := map[string]string{}
	for rows.Next() {
		var id, step string
		if err := rows.Scan(&id, &step); err != nil {
			f.t.Fatal(err)
		}
		jobs[step] = id
	}
	if err := rows.Err(); err != nil {
		f.t.Fatal(err)
	}
	return jobs
}

// resume runs the queued run's next attempt to its end.
func (f *fixture) resume(runID string) Run {
	f.t.Helper()
	held, err := f.service.claimRun(f.t.Context(), f.alice, runID)
	if err != nil {
		f.t.Fatal(err)
	}
	execution, err := f.service.newRunExecution(f.t.Context(), f.alice, held)
	if err != nil {
		f.t.Fatal(err)
	}
	out, err := execution.run(f.t.Context())
	if err = f.service.completeAttempt(f.t.Context(), f.alice, held, out, err); err != nil {
		f.t.Fatal(err)
	}
	run, err := f.service.ReadRun(f.t.Context(), f.alice, runID)
	if err != nil {
		f.t.Fatal(err)
	}
	return run
}

func TestParallelParkedCallsWaitForEveryJob(t *testing.T) {
	f := setup(t)
	// The two calls write the same resource, so the second starts only after
	// the first parked: a park that stopped the batch would fail it.
	plan := func(id string) llmgateway.ToolCall {
		return llmgateway.ToolCall{ID: id, Name: updatePlanTool, Arguments: json.RawMessage(`{"steps":[{"key":"build","title":"搭中殿","state":"doing"}]}`)}
	}
	run, held, execution, parking, err := startParkingRun(t, f, nil, plan("a"), plan("b"))
	var parked *parkedRun
	if !errors.As(err, &parked) || len(parked.jobIDs) != 2 || !errors.Is(err, errRunInterrupted) {
		t.Fatalf("attempt ended with %v", err)
	}
	// A parked call does not stop its batch: both calls ran and parked.
	jobs := f.runJobs(run.ID)
	if parking.calls != 2 || len(jobs) != 2 {
		t.Fatalf("calls=%d jobs=%v", parking.calls, jobs)
	}
	for _, id := range jobs {
		if !slices.Contains(parked.jobIDs, id) {
			t.Fatalf("interrupt named %v, jobs are %v", parked.jobIDs, jobs)
		}
	}
	if n := f.count("creative_agent_steps", "run_id=$1 AND kind='tool' AND state='prepared'", run.ID); n != 2 {
		t.Fatalf("prepared tool steps=%d", n)
	}
	if err = f.service.completeAttempt(t.Context(), f.alice, held, execution.outcome, err); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.service.ReadRun(t.Context(), f.alice, run.ID); got.State != RunWaitingClient || got.WaitReason != waitClientJob {
		t.Fatalf("after parking: %+v", got)
	}
	if holder := f.slotHolder(); holder != "" {
		t.Fatalf("parked run kept the slot: %s", holder)
	}
	steps := make([]string, 0, len(jobs))
	for step := range jobs {
		steps = append(steps, step)
	}
	slices.Sort(steps)
	f.settleParked(execution, jobs[steps[0]], steps[0])
	f.sweepRuns()
	if got, _ := f.service.ReadRun(t.Context(), f.alice, run.ID); got.State != RunWaitingClient {
		t.Fatalf("resumed with a call still parked: %s", got.State)
	}
	f.settleParked(execution, jobs[steps[1]], steps[1])
	f.sweepRuns()
	if got, _ := f.service.ReadRun(t.Context(), f.alice, run.ID); got.State != RunQueued {
		t.Fatalf("not resumed after every job ended: %s", got.State)
	}
	done := f.resume(run.ID)
	if done.State != RunSucceeded || f.vendor.calls() != 2 || parking.calls != 2 {
		t.Fatalf("resumed run %s, model calls %d, tool calls %d", done.State, f.vendor.calls(), parking.calls)
	}
}

// A job can finish between the tool committing it and the attempt unwinding
// (design review B2): its step is already written when completeAttempt runs.
// The run goes straight back to the queue with its slot, not to waiting_input,
// and resumes once.
func TestJobFinishedBeforeTheAttemptUnwindsRequeuesOnce(t *testing.T) {
	f := setup(t)
	settle := func(e *runExecution) func(creativescene.Job, string) {
		return func(job creativescene.Job, step string) { f.settleParked(e, job.ID, step) }
	}
	run, held, execution, _, err := startParkingRun(t, f, settle, readResultCall("a", "a"))
	if n := f.count("creative_agent_steps", "run_id=$1 AND kind='tool' AND state='prepared'", run.ID); n != 0 {
		t.Fatalf("prepared tool steps=%d", n)
	}
	if err = f.service.completeAttempt(t.Context(), f.alice, held, execution.outcome, err); err != nil {
		t.Fatal(err)
	}
	var epoch int64
	if err = f.db.QueryRow(`SELECT execution_epoch FROM creative_agent_runs WHERE id=$1`, run.ID).Scan(&epoch); err != nil {
		t.Fatal(err)
	}
	got, _ := f.service.ReadRun(t.Context(), f.alice, run.ID)
	if got.State != RunQueued || got.WaitReason != "" || epoch != held.Epoch+1 {
		t.Fatalf("after a finished job: %+v epoch %d (held %d)", got, epoch, held.Epoch)
	}
	if holder := f.slotHolder(); holder != run.ID {
		t.Fatalf("slot held by %q", holder)
	}
	f.sweepRuns()
	if again, _ := f.service.ReadRun(t.Context(), f.alice, run.ID); again.Revision != got.Revision {
		t.Fatalf("the sweep moved a queued run: %+v", again)
	}
	if done := f.resume(run.ID); done.State != RunSucceeded || f.vendor.calls() != 2 {
		t.Fatalf("resumed run %s, model calls %d", done.State, f.vendor.calls())
	}
}

// A worker that dies after a tool committed its job reruns the answer from the
// checkpoint; the tool finds its job by the call's step and parks on it again.
func TestRerunAfterACrashParksOnTheSameJobs(t *testing.T) {
	f := setup(t)
	run, held, _, parking, err := startParkingRun(t, f, nil, readResultCall("a", "a"), readResultCall("b", "b"))
	var first *parkedRun
	if !errors.As(err, &first) {
		t.Fatalf("first attempt ended with %v", err)
	}
	again, err := f.service.newRunExecution(t.Context(), f.alice, held)
	if err != nil {
		t.Fatal(err)
	}
	again.config.Handlers = append(again.config.Handlers, parking)
	_, err = again.run(t.Context())
	var second *parkedRun
	if !errors.As(err, &second) {
		t.Fatalf("rerun ended with %v", err)
	}
	slices.Sort(first.jobIDs)
	slices.Sort(second.jobIDs)
	if !slices.Equal(first.jobIDs, second.jobIDs) || len(f.runJobs(run.ID)) != 2 || parking.calls != 4 {
		t.Fatalf("first %v second %v jobs %v calls %d", first.jobIDs, second.jobIDs, f.runJobs(run.ID), parking.calls)
	}
}

// An attempt can fail after a call parked: on 2026-10-19 the checkpoint taken
// at the interruption was over its cap. The worker ends the run itself, and its
// jobs must end with it, or a page claims them for a run that is gone and the
// sweep can never settle them.
func TestRunFailedAfterParkingCancelsItsJobs(t *testing.T) {
	f := setup(t)
	run, held, execution, _, err := startParkingRun(t, f, nil, readResultCall("a", "a"))
	var parked *parkedRun
	if !errors.As(err, &parked) {
		t.Fatalf("attempt ended with %v", err)
	}
	if err = f.service.completeAttempt(t.Context(), f.alice, held, execution.outcome, ErrLimit); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.service.ReadRun(t.Context(), f.alice, run.ID); got.State != RunFailed || got.ErrorCode != "creative_context_limit" {
		t.Fatalf("run %s %s", got.State, got.ErrorCode)
	}
	if n := f.count("creative_scene_jobs", "run_id=$1 AND state='cancelled'", run.ID); n != 1 {
		t.Fatalf("jobs %v not cancelled with the run", f.runJobs(run.ID))
	}
}
