package creativeagent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
)

func decodeToolTask(t *testing.T, receipt creativeops.Receipt) ToolTask {
	t.Helper()
	var task ToolTask
	if err := json.Unmarshal(receipt.Outcome.Response, &task); err != nil {
		t.Fatal(err)
	}
	return task
}

func (f *fixture) detach(handle string) (ToolTask, creativeops.Command, error) {
	f.t.Helper()
	cmd := command(f.t, map[string]any{})
	receipt, err := f.service.DetachToolTask(f.t.Context(), f.alice, handle, cmd)
	if err != nil {
		return ToolTask{}, cmd, err
	}
	return decodeToolTask(f.t, receipt), cmd, nil
}

// Moving a parked generation to the background answers the call with the
// handle and continues the run; the task's result comes later and does not
// continue the run again. The same command replays its receipt, a second
// command keeps the result, and another account does not find the task.
func TestDetachContinuesTheRun(t *testing.T) {
	f := setup(t)
	run, generator, answers := startParkedGeneration(t, f)
	handle := f.sceneTasks(run.ID)[0].ID
	if _, err := f.service.DetachToolTask(t.Context(), f.bob, handle, command(t, map[string]any{})); !errors.Is(err, ErrNotFound) {
		t.Fatalf("another account detached the task: %v", err)
	}
	task, cmd, err := f.detach(handle)
	if err != nil || task.Mode != modeBackground || task.State != taskRunning || task.ToolKey != generateNodeTool || task.RunID != run.ID {
		t.Fatalf("detach %+v %v", task, err)
	}
	if again, err := f.service.DetachToolTask(t.Context(), f.alice, handle, cmd); err != nil || decodeToolTask(t, again).Revision != task.Revision {
		t.Fatalf("replay %v", err)
	}
	if second, _, err := f.detach(handle); err != nil || second.Mode != modeBackground || second.Revision != task.Revision {
		t.Fatalf("second detach %+v %v", second, err)
	}
	if got, _ := f.service.ReadRun(t.Context(), f.alice, run.ID); got.State != RunQueued {
		t.Fatalf("run not continued: %s", got.State)
	}
	if err = f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.service.ReadRun(t.Context(), f.alice, run.ID); got.State != RunSucceeded || len(*answers) != 1 ||
		!strings.Contains((*answers)[0], `"detached":true`) || !strings.Contains((*answers)[0], handle) {
		t.Fatalf("run=%s answers=%v", got.State, *answers)
	}
	generator.mu.Lock()
	generator.pending = false
	generator.mu.Unlock()
	f.markDue(handle)
	if _, err = f.service.FinishDueToolTasks(t.Context(), f.st, 10); err != nil {
		t.Fatal(err)
	}
	if got := f.sceneTasks(run.ID)[0]; got.State != taskSucceeded || got.Mode != modeBackground {
		t.Fatalf("task=%+v", got)
	}
	if got, _ := f.service.ReadRun(t.Context(), f.alice, run.ID); got.State != RunSucceeded || len(*answers) != 1 {
		t.Fatalf("background result continued the run: %s %v", got.State, *answers)
	}
}

// A source that finished before the detach commits answers the call with
// its real result: the task stays in the foreground.
func TestDetachAfterTheSourceFinished(t *testing.T) {
	f := setup(t)
	run, generator, answers := startParkedGeneration(t, f)
	generator.mu.Lock()
	generator.pending = false
	generator.mu.Unlock()
	task, _, err := f.detach(f.sceneTasks(run.ID)[0].ID)
	if err != nil || task.Mode != modeForeground || task.State != taskSucceeded || !strings.Contains(string(task.Result), "ccrv_out") {
		t.Fatalf("detach %+v %v", task, err)
	}
	if err = f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.service.ReadRun(t.Context(), f.alice, run.ID); got.State != RunSucceeded || len(*answers) != 1 || !strings.Contains((*answers)[0], "ccrv_out") {
		t.Fatalf("run=%s answers=%v", got.State, *answers)
	}
}

// A stop freezes the foreground: a later detach is refused. A task already
// in the background keeps running past the stop and is not withdrawn.
func TestStopAndDetach(t *testing.T) {
	f := setup(t)
	f.queue()
	generator := &stubGenerator{pending: true}
	f.service.SetGenerator(generator)
	c := f.conversation(f.alice, f.canvasID)
	consent := contextConsent(t, f, c.ID)
	f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, n int) (llmgateway.Result, error) {
		if n == 1 {
			return scriptedResult(r,
				call("bg", 0, generateNodeTool, `{"node_id":"ccnd_img","draft_revision":1,"run_in_background":true}`),
				call("fg", 1, generateNodeTool, `{"node_id":"ccnd_other","draft_revision":1}`)), nil
		}
		return scriptedResult(r), nil
	}
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "出两张图"})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	modes := map[string]string{}
	for _, task := range f.sceneTasks(run.ID) {
		modes[task.Mode] = task.ID
	}
	if _, err = f.service.CancelRun(t.Context(), f.alice, command(t, map[string]any{"run_id": run.ID})); err != nil {
		t.Fatal(err)
	}
	if _, _, err = f.detach(modes[modeForeground]); !errors.Is(err, ErrRunState) {
		t.Fatalf("detach after stop: %v", err)
	}
	if len(generator.stopped) != 1 || !strings.HasSuffix(generator.stopped[0], f.taskStep(modes[modeForeground])) {
		t.Fatalf("stop withdrew %v", generator.stopped)
	}
	generator.mu.Lock()
	generator.pending = false
	generator.mu.Unlock()
	f.markDue(modes[modeBackground])
	if _, err = f.service.FinishDueToolTasks(t.Context(), f.st, 10); err != nil {
		t.Fatal(err)
	}
	if got := f.taskRow(modes[modeBackground]); got.State != taskSucceeded {
		t.Fatalf("background task after the stop: %+v", got)
	}
}

func (f *fixture) taskStep(handle string) string {
	f.t.Helper()
	var step string
	if err := f.db.QueryRow(`SELECT tool_step_id FROM creative_agent_tool_tasks WHERE id=$1`, handle).Scan(&step); err != nil {
		f.t.Fatal(err)
	}
	return step
}

// Only a background task can be cancelled from its card. A cancelled
// generation is withdrawn (or charged and discarded) and reads cancelled; a
// second cancel keeps the result.
func TestCancelBackgroundGeneration(t *testing.T) {
	f := setup(t)
	run, generator, _ := startParkedGeneration(t, f)
	handle := f.sceneTasks(run.ID)[0].ID
	if _, err := f.service.CancelToolTask(t.Context(), f.alice, handle, command(t, map[string]any{})); !errors.Is(err, ErrRunState) {
		t.Fatalf("foreground task cancelled: %v", err)
	}
	if _, _, err := f.detach(handle); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.CancelToolTask(t.Context(), f.bob, handle, command(t, map[string]any{})); !errors.Is(err, ErrNotFound) {
		t.Fatalf("another account cancelled the task: %v", err)
	}
	receipt, err := f.service.CancelToolTask(t.Context(), f.alice, handle, command(t, map[string]any{}))
	if err != nil {
		t.Fatal(err)
	}
	task := decodeToolTask(t, receipt)
	if task.State != taskCancelled || !strings.Contains(string(task.Result), `"cancelled"`) || len(generator.cancelled) != 1 {
		t.Fatalf("cancel %+v cancelled=%v", task, generator.cancelled)
	}
	again, err := f.service.CancelToolTask(t.Context(), f.alice, handle, command(t, map[string]any{}))
	if err != nil || decodeToolTask(t, again).Revision != task.Revision || len(generator.cancelled) != 1 {
		t.Fatalf("second cancel %v", err)
	}
	if n := f.count("creative_agent_tool_tasks", "id=$1 AND stop_requested_at IS NOT NULL", handle); n != 1 {
		t.Fatal("cancel not recorded")
	}
}

// A cancelled background compress discards its candidate.
func TestCancelBackgroundCompress(t *testing.T) {
	f := setup(t)
	run, compressor, _ := startParkedCompress(t, f)
	handle := f.sceneTasks(run.ID)[0].ID
	if _, _, err := f.detach(handle); err != nil {
		t.Fatal(err)
	}
	receipt, err := f.service.CancelToolTask(t.Context(), f.alice, handle, command(t, map[string]any{}))
	if err != nil {
		t.Fatal(err)
	}
	if task := decodeToolTask(t, receipt); task.State != taskCancelled || len(compressor.discarded) != 1 || len(compressor.adopts) != 0 {
		t.Fatalf("cancel %+v discarded=%v adopts=%v", task, compressor.discarded, compressor.adopts)
	}
}

// A cancelled background bake cancels its job; the page cannot complete it.
func TestCancelBackgroundBake(t *testing.T) {
	f, c, consent, doc := visualScene(t, false)
	s := &script{turns: []func(llmgateway.ProviderRequest) llmgateway.Result{
		func(r llmgateway.ProviderRequest) llmgateway.Result {
			return scriptedResult(r, sceneCall("a", readSceneTool, map[string]any{"document_id": doc}))
		},
		func(r llmgateway.ProviderRequest) llmgateway.Result {
			return scriptedResult(r, sceneCall("b", writeModelTool, map[string]any{"document_id": doc, "based_on_revision": lastTool(r)["revision"],
				"target": map[string]any{"add": map[string]any{"name": "石柱", "transform": identityAt(0)}}, "code": testPillarCode, "run_in_background": true}))
		},
	}}
	f.vendor.script = s.answer
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "搭一根石柱"})
	if err != nil {
		t.Fatal(err)
	}
	if got := f.resume(run.ID); got.State != RunSucceeded {
		t.Fatalf("run %s %s", got.State, got.ErrorCode)
	}
	task := f.sceneTasks(run.ID)[0]
	receipt, err := f.service.CancelToolTask(t.Context(), f.alice, task.ID, command(t, map[string]any{}))
	if err != nil {
		t.Fatal(err)
	}
	if got := decodeToolTask(t, receipt); got.State != taskCancelled {
		t.Fatalf("cancel %+v", got)
	}
	var state string
	if err := f.db.QueryRow(`SELECT state FROM creative_scene_jobs WHERE id=$1`, f.runJobs(run.ID)[task.Step]).Scan(&state); err != nil || state != creativescene.JobCancelled {
		t.Fatalf("job %s %v", state, err)
	}
}
