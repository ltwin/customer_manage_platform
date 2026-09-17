package creativeagent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/jobs"
)

func TestBlankAnswerIsNotSuccessfulDelivery(t *testing.T) {
	for _, tc := range []struct{ name, reply string }{
		{"ASCII whitespace", " \n\t "},
		{"Unicode whitespace", "\u3000\u00a0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setup(t)
			f.queue()
			f.vendor.reply = tc.reply
			c := f.conversation(f.alice, f.canvasID)
			run, err := f.startRun(f.alice, c.ID, f.consent(f.alice, c.ID).ID,
				InstructionSegment{Type: "text", Text: "整理参考"})
			if err != nil {
				t.Fatal(err)
			}
			if err := f.work(f.alice, run.ID); err != nil {
				t.Fatal(err)
			}
			after, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
			if err != nil {
				t.Fatal(err)
			}
			if after.State != RunFailed || after.ErrorCode != "creative_model_empty_result" || after.SettlementState != "settled" {
				t.Fatalf("blank answer must fail without erasing its cost: %+v", after)
			}
			if n := f.count("creative_agent_messages", "run_id=$1 AND role='assistant'", run.ID); n != 0 {
				t.Fatalf("blank answer created %d messages", n)
			}
			// The complete provider result was consumed once, even though it did
			// not contain a useful answer. Retrying the task must not buy another.
			if n := f.count("llm_result_consumers", "state='consumed'"); n != 1 {
				t.Fatalf("consumed results: %d", n)
			}
			if err := f.work(f.alice, run.ID); err != nil {
				t.Fatal(err)
			}
			if f.vendor.calls() != 1 {
				t.Fatalf("provider calls: %d", f.vendor.calls())
			}
			assertRunReleased(t, f, run.ID)
		})
	}
}

// Pause the actual finishing UPDATE while a second delivery attempts to claim
// the same run. Before the fix, finish owned run and the claimant owned slot;
// each then waited for the other. The trigger only controls that interleaving.
func TestRunFinishAndConcurrentRedeliveryDoNotDeadlock(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	run, err := f.startRun(f.alice, c.ID, f.consent(f.alice, c.ID).ID,
		InstructionSegment{Type: "text", Text: "整理参考"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	conn, err := f.db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock(76543210)`); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_, _ = conn.ExecContext(cleanup, `SELECT pg_advisory_unlock(76543210)`)
	}()
	if _, err := f.db.ExecContext(ctx, `
	 CREATE FUNCTION test_finish_barrier() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
	 IF NEW.state='succeeded' AND OLD.state<>'succeeded' THEN
	   PERFORM set_config('deadlock_timeout','50ms',true);
	   PERFORM pg_advisory_xact_lock(76543210);
	 END IF;
	 RETURN NEW; END $$;
	 CREATE TRIGGER test_finish_barrier BEFORE UPDATE ON creative_agent_runs
	 FOR EACH ROW EXECUTE FUNCTION test_finish_barrier();`); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(runTask{RunID: run.ID})
	if err != nil {
		t.Fatal(err)
	}
	work := func(done chan<- error) {
		done <- f.service.workRun(ctx, f.alice, jobs.Request{Payload: payload})
	}
	first, duplicate := make(chan error, 1), make(chan error, 1)
	go work(first)
	waitRunLock(t, ctx, f, "UPDATE creative_agent_runs%", "advisory")
	go work(duplicate)
	// The fixed path waits on slot; the old path waited on run. Accept either
	// barrier here so the assertion tests the outcome, not the implementation.
	waitRunLock(t, ctx, f, "SELECT % FROM creative_agent_%FOR UPDATE", "transactionid")
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_unlock(76543210)`); err != nil {
		t.Fatal(err)
	}
	for _, done := range []<-chan error{first, duplicate} {
		if err := <-done; err != nil {
			t.Errorf("concurrent delivery: %v", err)
		}
	}
	if err := f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	after, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
	if err != nil || after.State != RunSucceeded {
		t.Fatalf("run stranded after delivery: %+v err=%v", after, err)
	}
	assertRunReleased(t, f, run.ID)
	if f.vendor.calls() != 1 || f.count("creative_agent_messages", "run_id=$1 AND role='assistant'", run.ID) != 1 {
		t.Fatal("redelivery repeated the model or its answer")
	}
}

// A PostgreSQL serialization error rolls back the entire finishing transaction.
// A sequence survives rollback and lets the first attempt fail exactly once.
func TestRunFinishRetriesRolledBackTransactionWithoutCallingModelAgain(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	run, err := f.startRun(f.alice, c.ID, f.consent(f.alice, c.ID).ID,
		InstructionSegment{Type: "text", Text: "整理参考"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`
	 CREATE SEQUENCE test_finish_attempt;
	 CREATE FUNCTION test_finish_retry() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
	 IF NEW.state='succeeded' AND OLD.state<>'succeeded' THEN
	   IF nextval('test_finish_attempt')=1 THEN
	     RAISE EXCEPTION 'retry finishing' USING ERRCODE='40001';
	   END IF;
	 END IF;
	 RETURN NEW; END $$;
	 CREATE TRIGGER test_finish_retry BEFORE UPDATE ON creative_agent_runs
	 FOR EACH ROW EXECUTE FUNCTION test_finish_retry();`); err != nil {
		t.Fatal(err)
	}
	if err := f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	after, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
	if err != nil || after.State != RunSucceeded {
		t.Fatalf("run: %+v err=%v", after, err)
	}
	var attempts int
	if err := f.db.QueryRow(`SELECT last_value FROM test_finish_attempt`).Scan(&attempts); err != nil || attempts != 2 {
		t.Fatalf("finishing attempts=%d err=%v", attempts, err)
	}
	if n := f.count("creative_run_events", "run_id=$1 AND event_type='run.finished'", run.ID); n != 1 {
		t.Fatalf("finishing events: %d", n)
	}
	if f.vendor.calls() != 1 {
		t.Fatalf("provider calls: %d", f.vendor.calls())
	}
	assertRunReleased(t, f, run.ID)
}

func TestRunFinishSurvivesCancelledExecutionContext(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	run, err := f.startRun(f.alice, c.ID, f.consent(f.alice, c.ID).ID,
		InstructionSegment{Type: "text", Text: "整理参考"})
	if err != nil {
		t.Fatal(err)
	}
	held, err := f.service.claimRun(t.Context(), f.alice, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := f.service.finishRun(ctx, f.alice, held, turnOutcome{}, context.Canceled); err != nil {
		t.Fatal(err)
	}
	after, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
	if err != nil || after.State != RunFailed || after.FinishedAt == nil {
		t.Fatalf("run: %+v err=%v", after, err)
	}
	assertRunReleased(t, f, run.ID)
}

func assertRunReleased(t *testing.T, f *fixture, runID string) {
	t.Helper()
	if n := f.count("creative_agent_slots", "run_id=$1", runID); n != 0 {
		t.Fatalf("run still owns %d slots", n)
	}
	if _, tokens := f.holds(); tokens != 0 {
		t.Fatalf("run still holds %d tokens", tokens)
	}
}

func waitRunLock(t *testing.T, ctx context.Context, f *fixture, query, event string) {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var count int
		err := f.db.QueryRowContext(ctx, `SELECT count(*) FROM pg_stat_activity
		 WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE $1 AND wait_event=$2`, query, event).Scan(&count)
		if err != nil {
			t.Fatal(err)
		}
		if count > 0 {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("lock barrier not reached: %s / %s: %v", query, event, ctx.Err())
		case <-ticker.C:
		}
	}
}
