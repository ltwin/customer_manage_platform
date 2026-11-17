package creativeagent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativegeneration"
	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
)

type sceneTaskRow struct {
	ID, Tool, Mode, State, Step, Run string
	Result                           json.RawMessage
}

func (f *fixture) sceneTasks(runID string) []sceneTaskRow {
	f.t.Helper()
	rows, err := f.db.Query(`SELECT id,tool_key,mode,state,tool_step_id,source_run_id,result FROM creative_agent_tool_tasks WHERE source_run_id=$1 ORDER BY created_at,id`, runID)
	if err != nil {
		f.t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []sceneTaskRow
	for rows.Next() {
		var r sceneTaskRow
		var result []byte
		if err := rows.Scan(&r.ID, &r.Tool, &r.Mode, &r.State, &r.Step, &r.Run, &result); err != nil {
			f.t.Fatal(err)
		}
		r.Result = result
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		f.t.Fatal(err)
	}
	return out
}

// A scene tool that parks registers its task with the job, and the job's
// completion ends the task in the same transaction that continues the run.
func TestSceneToolTaskEndsWithItsJob(t *testing.T) {
	f, c, consent, doc := visualScene(t, true)
	s := &script{turns: []func(llmgateway.ProviderRequest) llmgateway.Result{
		func(r llmgateway.ProviderRequest) llmgateway.Result {
			return scriptedResult(r, sceneCall("a", renderViewTool, map[string]any{"document_id": doc, "preset": "overview"}))
		},
	}}
	f.vendor.script = s.answer
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "看一眼"})
	if err != nil {
		t.Fatal(err)
	}
	if got := f.resume(run.ID); got.State != RunWaitingClient {
		t.Fatalf("after render_view: %s %s", got.State, got.ErrorCode)
	}
	jobs := f.runJobs(run.ID)
	tasks := f.sceneTasks(run.ID)
	if len(tasks) != 1 || len(jobs) != 1 {
		t.Fatalf("tasks=%+v jobs=%v", tasks, jobs)
	}
	task := tasks[0]
	if task.Tool != "render_view" || task.Mode != modeForeground || task.State != taskRunning || jobs[task.Step] == "" || task.ID != toolTaskID(task.Step) {
		t.Fatalf("task=%+v jobs=%v", task, jobs)
	}

	if resp := f.hostCompletes(); resp.State != creativescene.JobSucceeded {
		t.Fatalf("render %+v", resp)
	}
	task = f.sceneTasks(run.ID)[0]
	var published []byte
	if err := f.db.QueryRow(`SELECT output FROM creative_agent_steps WHERE id=$1`, task.Step).Scan(&published); err != nil {
		t.Fatal(err)
	}
	var shot map[string]any
	if task.State != taskSucceeded || json.Unmarshal(task.Result, &shot) != nil || shot["image_revision_id"] == nil {
		t.Fatalf("task after completion=%+v", task)
	}
	if got, _ := f.service.ReadRun(t.Context(), f.alice, run.ID); got.State != RunQueued {
		t.Fatalf("run not continued: %s", got.State)
	}
	// Nothing is left for the finisher.
	if n := f.count("creative_agent_tool_tasks", "next_check_at IS NOT NULL AND state='running'"); n != 0 {
		t.Fatalf("%d tasks left due", n)
	}
	if got := f.resume(run.ID); got.State != RunSucceeded {
		t.Fatalf("finished %s %s", got.State, got.ErrorCode)
	}
}

// A job cancelled with its stopped run never reaches the scene waiter; the
// finisher ends its task without continuing anything.
func TestStoppedRunsSceneTaskEndsCancelled(t *testing.T) {
	f, c, consent, doc := visualScene(t, true)
	s := &script{turns: []func(llmgateway.ProviderRequest) llmgateway.Result{
		func(r llmgateway.ProviderRequest) llmgateway.Result {
			return scriptedResult(r, sceneCall("a", renderViewTool, map[string]any{"document_id": doc, "preset": "overview"}))
		},
	}}
	f.vendor.script = s.answer
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "看一眼"})
	if err != nil {
		t.Fatal(err)
	}
	if got := f.resume(run.ID); got.State != RunWaitingClient {
		t.Fatalf("after render_view: %s %s", got.State, got.ErrorCode)
	}
	if _, err = f.service.CancelRun(t.Context(), f.alice, command(t, map[string]any{"run_id": run.ID})); err != nil {
		t.Fatal(err)
	}
	for _, job := range f.runJobs(run.ID) {
		var state string
		if err := f.db.QueryRow(`SELECT state FROM creative_scene_jobs WHERE id=$1`, job).Scan(&state); err != nil || state != creativescene.JobCancelled {
			t.Fatalf("job %s: %s %v", job, state, err)
		}
	}
	if _, err = f.service.FinishDueToolTasks(t.Context(), f.st, 10); err != nil {
		t.Fatal(err)
	}
	task := f.sceneTasks(run.ID)[0]
	if task.State != taskCancelled || len(task.Result) == 0 {
		t.Fatalf("task=%+v", task)
	}
	if got, _ := f.service.ReadRun(t.Context(), f.alice, run.ID); got.State != RunCancelled {
		t.Fatalf("stopped run moved: %s", got.State)
	}
}

// startParkedGeneration runs one generate_node call on a generation that has
// not finished, so the call parks on its task.
func startParkedGeneration(t *testing.T, f *fixture) (Run, *stubGenerator, *[]string) {
	t.Helper()
	f.queue()
	generator := &stubGenerator{pending: true}
	f.service.SetGenerator(generator)
	c := f.conversation(f.alice, f.canvasID)
	consent := contextConsent(t, f, c.ID)
	var answers []string
	f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, n int) (llmgateway.Result, error) {
		if n == 1 {
			return scriptedResult(r, call("generate", 0, generateNodeTool, `{"node_id":"ccnd_img","draft_revision":1}`)), nil
		}
		answers = append(answers, toolResult(r, 0))
		return scriptedResult(r), nil
	}
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "出一张图"})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	parked, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
	if err != nil || parked.State != RunWaitingClient || parked.WaitReason != waitToolTask {
		t.Fatalf("not parked on its task: %+v %v", parked, err)
	}
	if f.slotHolder() != "" {
		t.Fatal("a parked generation kept the slot")
	}
	return run, generator, &answers
}

// The generation outlives any old wait limit: the call parks without a slot,
// and the finisher hands the answer back when the execution concludes.
func TestForegroundGenerationParksUntilItsExecutionConcludes(t *testing.T) {
	f := setup(t)
	run, generator, answers := startParkedGeneration(t, f)
	task := f.sceneTasks(run.ID)
	if len(task) != 1 || task[0].Tool != generateNodeTool || task[0].Mode != modeForeground || task[0].State != taskRunning {
		t.Fatalf("task=%+v", task)
	}
	// Nothing concluded yet: a pass leaves the run parked.
	f.markDue(task[0].ID)
	if _, err := f.service.FinishDueToolTasks(t.Context(), f.st, 10); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.service.ReadRun(t.Context(), f.alice, run.ID); got.State != RunWaitingClient {
		t.Fatalf("run moved before the generation concluded: %s", got.State)
	}

	generator.mu.Lock()
	generator.pending = false
	generator.mu.Unlock()
	f.markDue(task[0].ID)
	if _, err := f.service.FinishDueToolTasks(t.Context(), f.st, 10); err != nil {
		t.Fatal(err)
	}
	if got := f.sceneTasks(run.ID)[0]; got.State != taskSucceeded || !strings.Contains(string(got.Result), "ccrv_out") {
		t.Fatalf("task=%+v", got)
	}
	if got, _ := f.service.ReadRun(t.Context(), f.alice, run.ID); got.State != RunQueued {
		t.Fatalf("run not continued: %s", got.State)
	}
	if err := f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.service.ReadRun(t.Context(), f.alice, run.ID); got.State != RunSucceeded || len(*answers) != 1 || !strings.Contains((*answers)[0], `"content_revision_ids":["ccrv_out"]`) {
		t.Fatalf("run=%s answers=%v", got.State, *answers)
	}
	if generator.submitted != 1 {
		t.Fatalf("submitted %d times", generator.submitted)
	}
}

// A stopped run withdraws what was not sent; what was sent still finishes,
// its task records the answer, and nothing continues the stopped run.
func TestStoppedRunsGenerationTaskStillRecordsItsAnswer(t *testing.T) {
	f := setup(t)
	run, generator, answers := startParkedGeneration(t, f)
	if _, err := f.service.CancelRun(t.Context(), f.alice, command(t, map[string]any{"run_id": run.ID})); err != nil {
		t.Fatal(err)
	}
	if len(generator.stopped) != 1 {
		t.Fatalf("queued generation not withdrawn: %v", generator.stopped)
	}
	generator.mu.Lock()
	generator.pending = false
	generator.mu.Unlock()
	task := f.sceneTasks(run.ID)[0]
	f.markDue(task.ID)
	if _, err := f.service.FinishDueToolTasks(t.Context(), f.st, 10); err != nil {
		t.Fatal(err)
	}
	if got := f.sceneTasks(run.ID)[0]; got.State != taskSucceeded {
		t.Fatalf("task=%+v", got)
	}
	// The stop closed the call; the finisher does not write the answer into it.
	var stepState string
	var output []byte
	if err := f.db.QueryRow(`SELECT state,output FROM creative_agent_steps WHERE id=$1`, task.Step).Scan(&stepState, &output); err != nil || stepState == "succeeded" || strings.Contains(string(output), "ccrv_out") {
		t.Fatalf("stopped run's step written: %s %s %v", stepState, output, err)
	}
	if got, _ := f.service.ReadRun(t.Context(), f.alice, run.ID); got.State != RunCancelled || len(*answers) != 0 {
		t.Fatalf("stopped run moved: %s %v", got.State, *answers)
	}
}

// A cancelled generation reads cancelled in the foreground too (review R6).
func TestCancelledGenerationIsCancelledForTheModel(t *testing.T) {
	outcome, err := generationOutcome(creativegeneration.AgentGenerationResult{NodeID: "n", ExecutionID: "e", State: "cancelled", ApplyState: "discarded"})
	if err != nil || outcome.State != taskCancelled || !strings.Contains(outcome.Result, `"state":"cancelled"`) || !strings.Contains(outcome.Result, `"success":false`) {
		t.Fatalf("outcome=%+v %v", outcome, err)
	}
}

func (f *fixture) markDue(taskID string) {
	f.t.Helper()
	if _, err := f.db.Exec(`UPDATE creative_agent_tool_tasks SET next_check_at=now()-interval '1 second' WHERE id=$1`, taskID); err != nil {
		f.t.Fatal(err)
	}
}
