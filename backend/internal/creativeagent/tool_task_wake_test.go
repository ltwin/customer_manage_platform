package creativeagent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// insertNotice adds a finished background task with a pending notice,
// finished at the given time, in the given configuration.
func (f *fixture) insertNotice(conversationID, runID, model string, finished time.Time) string {
	f.t.Helper()
	id := "cctask_" + uuid.NewString()
	if _, err := f.db.Exec(`INSERT INTO creative_agent_tool_tasks(id,account_id,conversation_id,canvas_id,source_run_id,tool_step_id,tool_key,source_id,mode,state,result,finished_at,model_key,notification_state,notification_message_id,revision)
VALUES($1,'agent-a',$2,$3,$4,$5,'generate_node',$6,'background','succeeded','{}',$7,$8,'pending',$9,2)`,
		id, conversationID, f.canvasID, runID, "ccst_"+uuid.NewString(), "src_"+id, finished, model, "ccms_"+uuid.NewString()); err != nil {
		f.t.Fatal(err)
	}
	return id
}

func (f *fixture) wakeAt(id string) time.Time {
	f.t.Helper()
	var at time.Time
	if err := f.db.QueryRow(`SELECT wake_after FROM creative_agent_tool_tasks WHERE id=$1`, id).Scan(&at); err != nil {
		f.t.Fatal(err)
	}
	return at
}

func (f *fixture) schedule(id string) {
	f.t.Helper()
	if err := f.alice.WithTxScope(f.t.Context(), func(tx store.TxAccountScope) error { return scheduleWakeInTx(f.t.Context(), tx, id) }); err != nil {
		f.t.Fatal(err)
	}
}

// One configuration's notices wake together: 2 seconds after the last one,
// at most 10 seconds after the first; another configuration keeps its own
// time (review R7, R5).
func TestWakeTimeOfAGroup(t *testing.T) {
	f := setup(t)
	c := f.conversation(f.alice, f.canvasID)
	base := time.Now().UTC().Truncate(time.Millisecond)
	burst := []string{}
	for _, d := range []time.Duration{0, time.Second, 1500 * time.Millisecond} {
		id := f.insertNotice(c.ID, "ccrn_x", "model", base.Add(d))
		f.schedule(id)
		burst = append(burst, id)
	}
	for _, id := range burst {
		if got := f.wakeAt(id); !got.Equal(base.Add(3500 * time.Millisecond)) {
			t.Fatalf("burst wakes at %v, want %v", got.Sub(base), 3500*time.Millisecond)
		}
	}
	other := f.insertNotice(c.ID, "ccrn_x", "other-model", base.Add(1500*time.Millisecond))
	f.schedule(other)
	if got := f.wakeAt(other); !got.Equal(base.Add(3500 * time.Millisecond)) {
		t.Fatalf("other configuration %v", got.Sub(base))
	}
	// One a second for ten seconds: the group opens at first + 10s.
	steady := conversationWith(t, f)
	var last string
	for i := range 10 {
		last = f.insertNotice(steady, "ccrn_x", "model", base.Add(time.Duration(i)*time.Second))
		f.schedule(last)
	}
	if got := f.wakeAt(last); !got.Equal(base.Add(10 * time.Second)) {
		t.Fatalf("steady stream wakes at %v", got.Sub(base))
	}
}

func conversationWith(t *testing.T, f *fixture) string {
	t.Helper()
	return f.conversation(f.alice, f.canvasID).ID
}

func (f *fixture) dueNow(handles ...string) {
	f.t.Helper()
	for _, h := range handles {
		if _, err := f.db.Exec(`UPDATE creative_agent_tool_tasks SET wake_after=now()-interval '1 second' WHERE id=$1`, h); err != nil {
			f.t.Fatal(err)
		}
	}
}

func (f *fixture) skipReason(handle string) string {
	f.t.Helper()
	var reason *string
	if err := f.db.QueryRow(`SELECT wake_skip_reason FROM creative_agent_tool_tasks WHERE id=$1`, handle).Scan(&reason); err != nil {
		f.t.Fatal(err)
	}
	if reason == nil {
		return ""
	}
	return *reason
}

func (f *fixture) newestRun(conversationID string) Run {
	f.t.Helper()
	var id string
	if err := f.db.QueryRow(`SELECT id FROM creative_agent_runs WHERE conversation_id=$1 ORDER BY created_at DESC, id DESC LIMIT 1`, conversationID).Scan(&id); err != nil {
		f.t.Fatal(err)
	}
	run, err := f.service.ReadRun(f.t.Context(), f.alice, id)
	if err != nil {
		f.t.Fatal(err)
	}
	return run
}

// An idle conversation's due notices open one automatic turn with the
// source's model, triggered by the notice; the turn reads it and answers.
func TestDueNoticeOpensAnAutomaticTurn(t *testing.T) {
	f := setup(t)
	run, handle := finishedInBackground(t, f)
	var seen []llmgateway.ProviderRequest
	f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, _ int) (llmgateway.Result, error) {
		seen = append(seen, r)
		return scriptedResult(r), nil
	}
	f.dueNow(handle)
	if _, err := f.service.WakeDueToolTasks(t.Context(), f.st, 10); err != nil {
		t.Fatal(err)
	}
	auto := f.newestRun(run.ConversationID)
	var trigger string
	if err := f.db.QueryRow(`SELECT notification_message_id FROM creative_agent_tool_tasks WHERE id=$1`, handle).Scan(&trigger); err != nil {
		t.Fatal(err)
	}
	if auto.ID == run.ID || auto.TriggerKind != triggerBackgroundTool || auto.TriggerMessageID != trigger || auto.ModelKey != testModelKey {
		t.Fatalf("auto run %+v", auto)
	}
	// A second pass opens nothing more.
	if _, err := f.service.WakeDueToolTasks(t.Context(), f.st, 10); err != nil {
		t.Fatal(err)
	}
	if n := f.count("creative_agent_runs", "conversation_id=$1 AND trigger_kind='background_tool'", run.ConversationID); n != 1 {
		t.Fatalf("%d automatic runs", n)
	}
	if err := f.work(f.alice, auto.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.service.ReadRun(t.Context(), f.alice, auto.ID); got.State != RunSucceeded || len(seen) != 1 || !strings.Contains(requestText(seen[0]), noticePrefix) {
		t.Fatalf("auto run %s requests=%d", got.State, len(seen))
	}
	if state, _ := f.delivery(handle); state != "delivered" {
		t.Fatalf("notice %s", state)
	}
}

// Five automatic turns in a row stop the chain; a photographer message
// resets it.
func TestAutomaticTurnsStopAfterFiveInARow(t *testing.T) {
	f := setup(t)
	run, handle := finishedInBackground(t, f)
	consent := contextConsent(t, f, run.ConversationID)
	f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, _ int) (llmgateway.Result, error) {
		return scriptedResult(r), nil
	}
	var runs []string
	for range wakeChainLimit {
		next, err := f.startRun(f.alice, run.ConversationID, consent.ID, InstructionSegment{Type: "text", Text: "继续"})
		if err != nil {
			t.Fatal(err)
		}
		if err = f.work(f.alice, next.ID); err != nil {
			t.Fatal(err)
		}
		runs = append(runs, next.ID)
	}
	// The notice went with the first of them; make another one pending and
	// count those runs as automatic.
	if _, err := f.db.Exec(`UPDATE creative_agent_runs SET trigger_kind='background_tool' WHERE id = ANY($1)`, runs); err != nil {
		t.Fatal(err)
	}
	again := f.insertNotice(run.ConversationID, run.ID, testModelKey, time.Now())
	f.dueNow(again)
	if _, err := f.service.WakeDueToolTasks(t.Context(), f.st, 10); err != nil {
		t.Fatal(err)
	}
	if f.skipReason(again) != skipChainLimit || f.newestRun(run.ConversationID).ID != runs[len(runs)-1] {
		t.Fatalf("chain not stopped: %q", f.skipReason(again))
	}
	_ = handle
	// The photographer speaks: the count starts again.
	if _, err := f.startRun(f.alice, run.ConversationID, consent.ID, InstructionSegment{Type: "text", Text: "好"}); err != nil {
		t.Fatal(err)
	}
	if err := f.work(f.alice, f.newestRun(run.ConversationID).ID); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := f.alice.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		var err error
		n, err = automaticRunsInARowInTx(t.Context(), tx, run.ConversationID)
		return err
	}); err != nil || n != 0 {
		t.Fatalf("chain after a photographer message: %d %v", n, err)
	}
}

// A stopped source run's notices wait for the photographer; so do those of a
// conversation whose consent was withdrawn. A busy conversation looks again.
func TestNoticesThatDoNotWake(t *testing.T) {
	f := setup(t)
	run, handle := finishedInBackground(t, f)

	// Busy: a run of the conversation is still open.
	if _, err := f.db.Exec(`UPDATE creative_agent_runs SET state='queued',lease_until=NULL,finished_at=NULL WHERE id=$1`, run.ID); err != nil {
		t.Fatal(err)
	}
	f.dueNow(handle)
	before := time.Now()
	if _, err := f.service.WakeDueToolTasks(t.Context(), f.st, 10); err != nil {
		t.Fatal(err)
	}
	if f.skipReason(handle) != "" || !f.wakeAt(handle).After(before) || f.newestRun(run.ConversationID).ID != run.ID {
		t.Fatal("a busy conversation was woken")
	}

	// Stopped.
	if _, err := f.db.Exec(`UPDATE creative_agent_runs SET state='cancelled',finished_at=now() WHERE id=$1`, run.ID); err != nil {
		t.Fatal(err)
	}
	f.dueNow(handle)
	if _, err := f.service.WakeDueToolTasks(t.Context(), f.st, 10); err != nil {
		t.Fatal(err)
	}
	if f.skipReason(handle) != skipSourceStopped || f.newestRun(run.ConversationID).ID != run.ID {
		t.Fatalf("stopped source woke: %q", f.skipReason(handle))
	}

	// Consent withdrawn.
	if _, err := f.db.Exec(`UPDATE creative_agent_runs SET state='succeeded',finished_at=now() WHERE id=$1`, run.ID); err != nil {
		t.Fatal(err)
	}
	fresh := f.insertNotice(run.ConversationID, run.ID, testModelKey, time.Now())
	f.withdraw(f.alice)
	f.dueNow(fresh)
	if _, err := f.service.WakeDueToolTasks(t.Context(), f.st, 10); err != nil {
		t.Fatal(err)
	}
	if f.skipReason(fresh) != skipEgress || f.newestRun(run.ConversationID).ID != run.ID {
		t.Fatalf("withdrawn consent woke: %q", f.skipReason(fresh))
	}
	if n := f.count("creative_agent_slots", "conversation_id=$1 AND run_id IS NOT NULL", run.ConversationID); n != 0 {
		t.Fatal("a refused wake kept the slot")
	}
}
