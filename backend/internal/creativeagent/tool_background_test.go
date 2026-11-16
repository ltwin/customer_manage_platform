package creativeagent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
)

// run_in_background is a boolean or absent; anything else is an argument
// error before any work, and a run on the old definitions never takes it.
func TestBackgroundArgument(t *testing.T) {
	for raw, want := range map[string]struct {
		background bool
		fails      bool
	}{
		`{"node_id":"n"}`:                           {},
		`{"node_id":"n","run_in_background":true}`:  {background: true},
		`{"node_id":"n","run_in_background":false}`: {},
		`{"node_id":"n","run_in_background":null}`:  {fails: true},
		`{"node_id":"n","run_in_background":"yes"}`: {fails: true},
		`{"node_id":"n","run_in_background":1}`:     {fails: true},
		`not json`:                                  {},
	} {
		args, background, err := splitBackground(raw)
		if (err != nil) != want.fails || background != want.background {
			t.Fatalf("%s: background=%v err=%v", raw, background, err)
		}
		if err == nil && strings.Contains(args, "run_in_background") {
			t.Fatalf("%s: field left in %s", raw, args)
		}
	}
	legacy := &runtimeHandler{legacyToolSchemas: true}
	if _, _, err := legacy.backgroundArgument(`{"run_in_background":false}`); !errors.Is(err, errToolArguments) {
		t.Fatalf("old definitions took run_in_background: %v", err)
	}
	if args, _, err := legacy.backgroundArgument(`{"node_id":"n"}`); err != nil || args != `{"node_id":"n"}` {
		t.Fatalf("old definitions: %s %v", args, err)
	}
}

func toolSchema(r llmgateway.ProviderRequest, name string) string {
	for _, d := range r.Chat.Tools {
		if d.Name == name {
			return string(d.InputSchema)
		}
	}
	return ""
}

// The four long-running tools offer run_in_background; render_view does not
// and refuses it like any unknown argument, before a job exists (review R9).
func TestRenderViewIsForegroundOnly(t *testing.T) {
	f, c, consent, doc := visualScene(t, true)
	s := &script{turns: []func(llmgateway.ProviderRequest) llmgateway.Result{
		func(r llmgateway.ProviderRequest) llmgateway.Result {
			return scriptedResult(r, sceneCall("a", renderViewTool, map[string]any{"document_id": doc, "preset": "overview", "run_in_background": true}))
		},
	}}
	f.vendor.script = s.answer
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "看一眼"})
	if err != nil {
		t.Fatal(err)
	}
	if got := f.resume(run.ID); got.State != RunSucceeded {
		t.Fatalf("run %s %s", got.State, got.ErrorCode)
	}
	first := s.request(1)
	for _, name := range []string{writeModelTool, exportShotsTool} {
		if !strings.Contains(toolSchema(first, name), "run_in_background") {
			t.Fatalf("%s does not offer run_in_background", name)
		}
	}
	for _, name := range []string{renderViewTool, readSceneTool, editSceneTool} {
		if schema := toolSchema(first, name); schema == "" || strings.Contains(schema, "run_in_background") {
			t.Fatalf("%s schema %q", name, schema)
		}
	}
	if jobs := f.runJobs(run.ID); len(jobs) != 0 || len(f.sceneTasks(run.ID)) != 0 {
		t.Fatalf("a refused call left work behind: %v", jobs)
	}
	if n := f.count("creative_agent_steps", "run_id=$1 AND tool_key=$2 AND state='failed'", run.ID, renderViewTool); n != 1 {
		t.Fatalf("render_view not refused: %d", n)
	}
}

// A background generation answers with its handle at once; the run does not
// park, a foreground call in the same answer still does, and the background
// task finishes after its run ended.
func TestGenerationInBackgroundOutlivesItsRun(t *testing.T) {
	f := setup(t)
	f.queue()
	generator := &stubGenerator{pending: true}
	f.service.SetGenerator(generator)
	c := f.conversation(f.alice, f.canvasID)
	consent := contextConsent(t, f, c.ID)
	var first llmgateway.ProviderRequest
	var answers []string
	f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, n int) (llmgateway.Result, error) {
		if n == 1 {
			first = r
			return scriptedResult(r,
				call("bg", 0, generateNodeTool, `{"node_id":"ccnd_img","draft_revision":1,"run_in_background":true}`),
				call("fg", 1, generateNodeTool, `{"node_id":"ccnd_other","draft_revision":1}`)), nil
		}
		answers = append(answers, toolResult(r, 0), toolResult(r, 1))
		return scriptedResult(r), nil
	}
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "出两张图"})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(toolSchema(first, generateNodeTool), "run_in_background") {
		t.Fatal("generate_node does not offer run_in_background")
	}
	parked, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
	if err != nil || parked.State != RunWaitingClient || parked.WaitReason != waitToolTask {
		t.Fatalf("not parked on the foreground call: %+v %v", parked, err)
	}
	tasks := f.sceneTasks(run.ID)
	modes := map[string]string{}
	for _, task := range tasks {
		modes[task.Mode] = task.ID
		if task.State != taskRunning {
			t.Fatalf("task=%+v", task)
		}
	}
	if len(tasks) != 2 || modes[modeBackground] == "" || modes[modeForeground] == "" || generator.submitted != 2 {
		t.Fatalf("tasks=%+v submitted=%d", tasks, generator.submitted)
	}

	// The foreground call concludes and the run ends; the background one is
	// still running.
	generator.mu.Lock()
	generator.pending = false
	generator.mu.Unlock()
	f.markDue(modes[modeForeground])
	if _, err := f.db.Exec(`UPDATE creative_agent_tool_tasks SET next_check_at=NULL WHERE id=$1`, modes[modeBackground]); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.FinishDueToolTasks(t.Context(), f.st, 10); err != nil {
		t.Fatal(err)
	}
	if err = f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.service.ReadRun(t.Context(), f.alice, run.ID); got.State != RunSucceeded {
		t.Fatalf("run %s", got.State)
	}
	var accepted struct {
		TaskHandle string `json:"task_handle"`
		State      string `json:"state"`
	}
	if len(answers) != 2 || json.Unmarshal([]byte(answers[0]), &accepted) != nil || accepted.TaskHandle != modes[modeBackground] || accepted.State != taskRunning ||
		!strings.Contains(answers[1], "ccrv_out") {
		t.Fatalf("answers %v", answers)
	}
	f.markDue(modes[modeBackground])
	if _, err = f.service.FinishDueToolTasks(t.Context(), f.st, 10); err != nil {
		t.Fatal(err)
	}
	for _, task := range f.sceneTasks(run.ID) {
		if task.State != taskSucceeded || !strings.Contains(string(task.Result), "ccrv_out") {
			t.Fatalf("task=%+v", task)
		}
	}
	if got, _ := f.service.ReadRun(t.Context(), f.alice, run.ID); got.State != RunSucceeded {
		t.Fatalf("finished run moved: %s", got.State)
	}
}

// A background bake is accepted, its run ends, and the page still writes
// it: the job is not cancelled with the run, and its completion does not
// need the run, its skills or the egress consent.
func TestBackgroundBakeFinishesAfterItsRun(t *testing.T) {
	f, c, consent, doc := visualScene(t, false)
	s := &script{turns: []func(llmgateway.ProviderRequest) llmgateway.Result{
		func(r llmgateway.ProviderRequest) llmgateway.Result {
			return scriptedResult(r, sceneCall("a", readSceneTool, map[string]any{"document_id": doc}))
		},
		func(r llmgateway.ProviderRequest) llmgateway.Result {
			return scriptedResult(r, sceneCall("b", writeModelTool, map[string]any{"document_id": doc, "based_on_revision": lastTool(r)["revision"],
				"target": map[string]any{"add": map[string]any{"name": "石柱", "transform": identityAt(0)}}, "code": testPillarCode, "params": map[string]any{"height": 3},
				"run_in_background": true}))
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
	accepted := lastTool(s.request(3))
	tasks := f.sceneTasks(run.ID)
	if len(tasks) != 1 || tasks[0].Mode != modeBackground || tasks[0].State != taskRunning || accepted["task_handle"] != tasks[0].ID || accepted["state"] != taskRunning {
		t.Fatalf("accepted=%v tasks=%+v", accepted, tasks)
	}
	jobs := f.runJobs(run.ID)
	var state string
	if err := f.db.QueryRow(`SELECT state FROM creative_scene_jobs WHERE id=$1`, jobs[tasks[0].Step]).Scan(&state); err != nil || state != creativescene.JobQueued {
		t.Fatalf("job ended with its run: %s %v", state, err)
	}

	f.revokeConsent(consent.ID)
	if resp := f.hostCompletes(); resp.State != creativescene.JobSucceeded {
		t.Fatalf("bake %+v", resp)
	}
	if _, err = f.service.FinishDueToolTasks(t.Context(), f.st, 10); err != nil {
		t.Fatal(err)
	}
	task := f.sceneTasks(run.ID)[0]
	var baked map[string]any
	if task.State != taskSucceeded || json.Unmarshal(task.Result, &baked) != nil || baked["status"] != "succeeded" || baked["object_ids"] == nil {
		t.Fatalf("task=%+v", task)
	}
	if got, _ := f.service.ReadRun(t.Context(), f.alice, run.ID); got.State != RunSucceeded {
		t.Fatalf("finished run moved: %s", got.State)
	}
}

// A background bake whose project was archived meanwhile finishes failed
// without writing (review R4), and nothing aborts its ended run.
func TestBackgroundBakeRefusedOnAnArchivedProject(t *testing.T) {
	f, c, consent, doc := visualScene(t, false)
	s := &script{turns: []func(llmgateway.ProviderRequest) llmgateway.Result{
		func(r llmgateway.ProviderRequest) llmgateway.Result {
			return scriptedResult(r, sceneCall("a", readSceneTool, map[string]any{"document_id": doc}))
		},
		func(r llmgateway.ProviderRequest) llmgateway.Result {
			return scriptedResult(r, sceneCall("b", writeModelTool, map[string]any{"document_id": doc, "based_on_revision": lastTool(r)["revision"],
				"target": map[string]any{"add": map[string]any{"name": "石柱", "transform": identityAt(0)}}, "code": testPillarCode,
				"run_in_background": true}))
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
	var before int64
	if err := f.db.QueryRow(`SELECT revision FROM creative_documents WHERE id=$1`, doc).Scan(&before); err != nil {
		t.Fatal(err)
	}
	f.setProjectArchived(true)
	if resp := f.hostCompletes(); resp.State != creativescene.JobFailed {
		t.Fatalf("archived write: %+v", resp)
	}
	var result string
	if err := f.db.QueryRow(`SELECT result::text FROM creative_scene_jobs WHERE run_id=$1`, run.ID).Scan(&result); err != nil || !strings.Contains(result, `"archived"`) {
		t.Fatalf("job result %s %v", result, err)
	}
	var after int64
	if err := f.db.QueryRow(`SELECT revision FROM creative_documents WHERE id=$1`, doc).Scan(&after); err != nil || after != before {
		t.Fatalf("archived scene written: %d -> %d %v", before, after, err)
	}
}

// A run parked under the definitions before run_in_background resumes with
// them: its checkpoint is accepted (the framework state it restores keeps
// the tool schemas its model steps were built with) and the run is held to
// those definitions, so run_in_background is an unknown argument there.
func TestCheckpointFromBeforeBackgroundResumes(t *testing.T) {
	f := setup(t)
	run, generator, _ := startParkedGeneration(t, f)
	var stored string
	if err := f.db.QueryRow(`SELECT registry_digest FROM creative_agent_checkpoints WHERE run_id=$1`, run.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	model, err := f.service.models.Model(testModelKey)
	if err != nil {
		t.Fatal(err)
	}
	var legacy string
	for _, library := range []bool{false, true} {
		h := &runtimeHandler{service: f.service, held: claim{Model: model}, libraryEnabled: library}
		current, err := registryDigest(h)
		if err != nil {
			t.Fatal(err)
		}
		if current != stored {
			continue
		}
		h.legacyToolSchemas = true
		if legacy, err = registryDigest(h); err != nil {
			t.Fatal(err)
		}
	}
	if legacy == "" || legacy == stored {
		t.Fatalf("no legacy digest for %s", stored)
	}
	if _, err := f.db.Exec(`UPDATE creative_agent_checkpoints SET registry_digest=$2 WHERE run_id=$1`, run.ID, legacy); err != nil {
		t.Fatal(err)
	}
	var refused string
	f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, n int) (llmgateway.Result, error) {
		switch n {
		case 2:
			return scriptedResult(r, call("again", 0, generateNodeTool, `{"node_id":"ccnd_img","draft_revision":1,"run_in_background":true}`)), nil
		case 3:
			refused = toolResult(r, 0)
		}
		return scriptedResult(r), nil
	}
	generator.mu.Lock()
	generator.pending = false
	generator.mu.Unlock()
	f.markDue(f.sceneTasks(run.ID)[0].ID)
	if _, err = f.service.FinishDueToolTasks(t.Context(), f.st, 10); err != nil {
		t.Fatal(err)
	}
	if err = f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := f.service.ReadRun(t.Context(), f.alice, run.ID)
	if got.State != RunSucceeded {
		t.Fatalf("old checkpoint not resumed: %s %s", got.State, got.ErrorCode)
	}
	if refused == "" || len(f.sceneTasks(run.ID)) != 1 || generator.submitted != 1 {
		t.Fatalf("old definitions took run_in_background: %q tasks=%d submitted=%d", refused, len(f.sceneTasks(run.ID)), generator.submitted)
	}
}
