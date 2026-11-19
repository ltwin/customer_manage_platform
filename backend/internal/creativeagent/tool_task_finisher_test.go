package creativeagent

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// fakeToolSource reports a scripted outcome and counts how often it settled.
type fakeToolSource struct {
	calls   atomic.Int32
	outcome func(task toolTask) (toolTaskOutcome, error)
}

func (f *fakeToolSource) settle(_ context.Context, _ store.TxAccountScope, task toolTask) (toolTaskOutcome, error) {
	f.calls.Add(1)
	return f.outcome(task)
}

func doneWith(result string) func(toolTask) (toolTaskOutcome, error) {
	return func(toolTask) (toolTaskOutcome, error) {
		return toolTaskOutcome{Done: true, State: taskSucceeded, Result: result}, nil
	}
}

// insertToolTask adds a running task on alice's canvas, due now when due is set.
func (f *fixture) insertToolTask(tool, mode, runID, stepID string, due bool) string {
	f.t.Helper()
	id := "cctask_" + uuid.NewString()
	var next any
	if due {
		next = time.Now().Add(-time.Second)
	}
	if _, err := f.db.Exec(`INSERT INTO creative_agent_tool_tasks(id,account_id,conversation_id,canvas_id,source_run_id,tool_step_id,tool_key,source_id,mode,model_key,next_check_at)
VALUES($1,'agent-a','ccco_x',$2,$3,$4,$5,$6,$7,'model',$8)`, id, f.canvasID, runID, stepID, tool, "src_"+id, mode, next); err != nil {
		f.t.Fatal(err)
	}
	return id
}

type taskRow struct {
	State    string
	Result   sql.NullString
	Revision int64
	Due      sql.NullTime
}

func (f *fixture) taskRow(id string) taskRow {
	f.t.Helper()
	var r taskRow
	if err := f.db.QueryRow(`SELECT state,result::text,revision,next_check_at FROM creative_agent_tool_tasks WHERE id=$1`, id).Scan(&r.State, &r.Result, &r.Revision, &r.Due); err != nil {
		f.t.Fatal(err)
	}
	return r
}

func TestToolTaskFinisherFinishesABackgroundTask(t *testing.T) {
	f := setup(t)
	source := &fakeToolSource{outcome: doneWith(`{"state":"succeeded"}`)}
	f.service.toolSources = map[string]toolTaskSource{"generate_node": source}
	id := f.insertToolTask("generate_node", modeBackground, "ccrn_x", "ccst_"+uuid.NewString(), true)
	idle := f.insertToolTask("generate_node", modeBackground, "ccrn_x", "ccst_"+uuid.NewString(), false)

	n, err := f.service.FinishDueToolTasks(t.Context(), f.st, 10)
	if err != nil || n != 1 {
		t.Fatalf("finished %d: %v", n, err)
	}
	got := f.taskRow(id)
	if got.State != taskSucceeded || got.Result.String != `{"state": "succeeded"}` || got.Revision != 2 || got.Due.Valid {
		t.Fatalf("task=%+v", got)
	}
	if f.taskRow(idle).State != taskRunning || source.calls.Load() != 1 {
		t.Fatal("a task that was not due was settled")
	}
	// Finished tasks are not picked up again.
	if n, err = f.service.FinishDueToolTasks(t.Context(), f.st, 10); err != nil || n != 0 {
		t.Fatalf("second pass %d: %v", n, err)
	}
}

func TestToolTaskFinisherWaitsWhenTheSourceIsNotDone(t *testing.T) {
	f := setup(t)
	recheck := time.Now().Add(10 * time.Minute).UTC().Truncate(time.Microsecond)
	f.service.toolSources = map[string]toolTaskSource{"generate_node": &fakeToolSource{outcome: func(toolTask) (toolTaskOutcome, error) {
		return toolTaskOutcome{RecheckAt: recheck}, nil
	}}}
	id := f.insertToolTask("generate_node", modeBackground, "ccrn_x", "ccst_"+uuid.NewString(), true)
	if _, err := f.service.FinishDueToolTasks(t.Context(), f.st, 10); err != nil {
		t.Fatal(err)
	}
	if got := f.taskRow(id); got.State != taskRunning || !got.Due.Time.Equal(recheck) || got.Revision != 1 {
		t.Fatalf("task=%+v, want running and due at %v", got, recheck)
	}
}

func TestToolTaskFinisherDefersAFailingTask(t *testing.T) {
	f := setup(t)
	f.service.toolSources = map[string]toolTaskSource{"generate_node": &fakeToolSource{outcome: func(toolTask) (toolTaskOutcome, error) {
		return toolTaskOutcome{}, errors.New("source unreadable")
	}}}
	id := f.insertToolTask("generate_node", modeBackground, "ccrn_x", "ccst_"+uuid.NewString(), true)
	before := time.Now()
	if _, err := f.service.FinishDueToolTasks(t.Context(), f.st, 10); err == nil {
		t.Fatal("failure not reported")
	}
	got := f.taskRow(id)
	if got.State != taskRunning || !got.Due.Valid || got.Due.Time.Before(before.Add(toolTaskRetryDelay-5*time.Second)) {
		t.Fatalf("task=%+v, want pushed back by %v", got, toolTaskRetryDelay)
	}
}

func TestToolTaskFinishersDoNotSettleOneTaskTwice(t *testing.T) {
	f := setup(t)
	source := &fakeToolSource{outcome: doneWith(`{}`)}
	f.service.toolSources = map[string]toolTaskSource{"generate_node": source}
	id := f.insertToolTask("generate_node", modeBackground, "ccrn_x", "ccst_"+uuid.NewString(), true)
	var wg sync.WaitGroup
	errs := make([]error, 4)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = f.service.FinishDueToolTasks(t.Context(), f.st, 10)
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := f.taskRow(id); got.State != taskSucceeded || got.Revision != 2 || source.calls.Load() != 1 {
		t.Fatalf("task=%+v settled %d times", got, source.calls.Load())
	}
}

func TestToolTaskFinisherWakesOnASourceChange(t *testing.T) {
	f := setup(t)
	f.service.toolSources = map[string]toolTaskSource{"write_model": &fakeToolSource{outcome: doneWith(`{}`)}}
	job := "csjob_" + uuid.NewString()
	if _, err := f.db.Exec(`INSERT INTO creative_scene_jobs(id,account_id,document_id,kind,request,deadline)
VALUES($1,'agent-a','cwdoc_x','bake_model','{}',now()+interval '30 minutes')`, job); err != nil {
		t.Fatal(err)
	}
	id := "cctask_" + uuid.NewString()
	if _, err := f.db.Exec(`INSERT INTO creative_agent_tool_tasks(id,account_id,conversation_id,canvas_id,source_run_id,tool_step_id,tool_key,source_id,mode,model_key)
VALUES($1,'agent-a','ccco_x',$2,'ccrn_x',$3,'write_model',$4,'background','model')`, id, f.canvasID, "ccst_"+uuid.NewString(), job); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); f.service.RunToolTaskFinisher(ctx, f.st) }()
	defer func() { cancel(); <-done }()
	// Let the listener connect: its first wake is the reconnect rescan.
	time.Sleep(300 * time.Millisecond)
	if _, err := f.db.Exec(`UPDATE creative_scene_jobs SET state='failed',result='{"code":"host_error","message":"x"}' WHERE id=$1`, job); err != nil {
		t.Fatal(err)
	}
	// Well inside the fallback scan: the NOTIFY is what woke the finisher.
	deadline := time.Now().Add(toolTaskFallbackScan - time.Second)
	for time.Now().Before(deadline) {
		if f.taskRow(id).State == taskSucceeded {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("the finisher did not react to the source change before its fallback scan")
}

func TestToolTaskFinisherContinuesAForegroundRun(t *testing.T) {
	f := setup(t)
	run := parkOnClient(t, f)
	var step string
	if err := f.db.QueryRow(`SELECT id FROM creative_agent_steps WHERE run_id=$1 AND kind='tool'`, run.ID).Scan(&step); err != nil {
		t.Fatal(err)
	}
	// parkOnClient reopened a step whose result was already published; a
	// call that really parked has none of it yet.
	for _, q := range []string{
		`DELETE FROM creative_agent_context_items WHERE source_step_id_snapshot=$1`,
		`DELETE FROM creative_agent_messages WHERE source_step_id=$1`,
	} {
		if _, err := f.db.Exec(q, step); err != nil {
			t.Fatal(err)
		}
	}
	f.service.toolSources = map[string]toolTaskSource{"generate_node": &fakeToolSource{outcome: doneWith(`{"node_id":"n","state":"succeeded","apply_state":"applied"}`)}}
	id := f.insertToolTask("generate_node", modeForeground, run.ID, step, true)

	if _, err := f.service.FinishDueToolTasks(t.Context(), f.st, 10); err != nil {
		t.Fatal(err)
	}
	if got := f.taskRow(id); got.State != taskSucceeded {
		t.Fatalf("task=%+v", got)
	}
	var stepState string
	if err := f.db.QueryRow(`SELECT state FROM creative_agent_steps WHERE id=$1`, step).Scan(&stepState); err != nil || stepState != "succeeded" {
		t.Fatalf("parked step=%s %v", stepState, err)
	}
	resumed, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
	if err != nil || resumed.State != RunQueued || f.slotHolder() != run.ID {
		t.Fatalf("resumed=%+v slot=%s %v", resumed, f.slotHolder(), err)
	}
}
