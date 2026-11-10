package creativeagent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"

	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
)

func TestCCancelQueuedIsAtomicAndIdempotent(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	consent := f.consent(f.alice, c.ID)
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "test"})
	if err != nil {
		t.Fatal(err)
	}
	op := command(t, map[string]any{"run_id": run.ID})
	receipt, err := f.service.CancelRun(t.Context(), f.alice, op)
	if err != nil {
		t.Fatal(err)
	}
	again, err := f.service.CancelRun(t.Context(), f.alice, op)
	if err != nil || *receipt.Outcome.ResultID != *again.Outcome.ResultID {
		t.Fatalf("replay: %v", err)
	}
	got, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
	if err != nil || got.State != RunCancelled {
		t.Fatalf("run=%+v err=%v", got, err)
	}
	assertRunReleased(t, f, run.ID)
	if err = f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	if f.vendor.calls() != 0 {
		t.Fatal("cancelled run dispatched")
	}
	if _, err = f.service.CancelRun(t.Context(), f.bob, command(t, map[string]any{"run_id": run.ID})); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross account: %v", err)
	}
}

// An unknown result no longer keeps a stopped run waiting: the stop ends it at
// once and frees the conversation, while the cost stays open for the gateway to
// verify. The unknown evidence is kept and nothing is sent again.
func TestCCancelEndsAnUnknownRunAtOnce(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	consent := f.consent(f.alice, c.ID)
	f.vendor.script = func(context.Context, llmgateway.ProviderRequest, int) (llmgateway.Result, error) {
		return llmgateway.Result{}, &llmgateway.ProviderError{Outcome: llmgateway.OutcomeUnknown, Class: "lost", Detail: "accepted unknown"}
	}
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	got, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
	if err != nil || got.State != RunReconciling {
		t.Fatalf("run=%+v err=%v", got, err)
	}
	receipt, err := f.service.CancelRun(t.Context(), f.alice, command(t, map[string]any{"run_id": run.ID}))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(receipt.Outcome.Response, &got); err != nil {
		t.Fatal(err)
	}
	if got.State != RunCancelled || got.SettlementState != "unknown" || got.FinishedAt == nil {
		t.Fatalf("cancelled=%+v", got)
	}
	if n := f.count("creative_agent_slots", "run_id=$1", run.ID); n != 0 {
		t.Fatal("cancel kept slot")
	}
	if n := f.count("llm_result_consumers", "state='pending'"); n != 1 {
		t.Fatal("unknown evidence discarded")
	}
	// A second stop under a new key finds the run already ended.
	again, err := f.service.CancelRun(t.Context(), f.alice, command(t, map[string]any{"run_id": run.ID}))
	if err != nil {
		t.Fatal(err)
	}
	var second Run
	if err = json.Unmarshal(again.Outcome.Response, &second); err != nil || second.State != RunCancelled {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	if _, err = f.service.CloseReconciliation(t.Context(), f.alice, command(t, map[string]any{"run_id": run.ID})); !errors.Is(err, ErrRunState) {
		t.Fatalf("close after stop: %v", err)
	}
	if _, err = f.service.SweepRuns(t.Context(), f.alice, 20); err != nil {
		t.Fatal(err)
	}
	if got, _ = f.service.ReadRun(t.Context(), f.alice, run.ID); got.State != RunCancelled {
		t.Fatalf("sweep reopened %+v", got)
	}
	if f.vendor.calls() != 1 {
		t.Fatal("unknown redispatched")
	}
}

// close-reconciliation remains for runs that wait on an unknown result without
// having been stopped.
func TestCCloseReconciliationEndsAnUnstoppedUnknownRun(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	consent := f.consent(f.alice, c.ID)
	f.vendor.script = func(context.Context, llmgateway.ProviderRequest, int) (llmgateway.Result, error) {
		return llmgateway.Result{}, &llmgateway.ProviderError{Outcome: llmgateway.OutcomeUnknown, Class: "lost", Detail: "accepted unknown"}
	}
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	if n := f.count("creative_agent_slots", "run_id=$1", run.ID); n != 1 {
		t.Fatal("unknown lost its slot")
	}
	receipt, err := f.service.CloseReconciliation(t.Context(), f.alice, command(t, map[string]any{"run_id": run.ID}))
	if err != nil {
		t.Fatal(err)
	}
	var got Run
	if err = json.Unmarshal(receipt.Outcome.Response, &got); err != nil {
		t.Fatal(err)
	}
	if got.State != RunFailed || got.SettlementState != "unknown" {
		t.Fatalf("closed=%+v", got)
	}
	if n := f.count("creative_agent_slots", "run_id=$1", run.ID); n != 0 {
		t.Fatal("close kept slot")
	}
}

// A run stopped before this release can still sit in reconciling. The sweep
// ends it rather than keep it waiting for its cost.
func TestCSweepEndsAStoppedRunLeftReconciling(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	consent := f.consent(f.alice, c.ID)
	f.vendor.script = func(context.Context, llmgateway.ProviderRequest, int) (llmgateway.Result, error) {
		return llmgateway.Result{}, &llmgateway.ProviderError{Outcome: llmgateway.OutcomeUnknown, Class: "lost", Detail: "accepted unknown"}
	}
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(`UPDATE creative_agent_runs SET cancel_requested_at=clock_timestamp() WHERE id=$1 AND state='reconciling'`, run.ID); err != nil {
		t.Fatal(err)
	}
	f.sweepRuns()
	got, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
	if err != nil || got.State != RunCancelled || got.SettlementState != "unknown" {
		t.Fatalf("run=%+v err=%v", got, err)
	}
	if n := f.count("creative_agent_slots", "run_id=$1", run.ID); n != 0 {
		t.Fatal("sweep kept slot")
	}
}

// A receipt is never rewritten: a stop recorded before this release replays as
// it was. The client then stops again under a new key, which ends the run.
func TestCOldCancelReceiptReplaysAndANewKeyEndsTheRun(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	consent := f.consent(f.alice, c.ID)
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "test"})
	if err != nil {
		t.Fatal(err)
	}
	op := command(t, map[string]any{"run_id": run.ID})
	if _, err = f.service.CancelRun(t.Context(), f.alice, op); err != nil {
		t.Fatal(err)
	}
	// What an older release answered while a model call was still out.
	if _, err = f.db.Exec(`UPDATE creative_operation_receipts SET response=jsonb_set(response::jsonb,'{state}','"reconciling"')::json WHERE operation_id=$1`, op.OperationID); err != nil {
		t.Fatal(err)
	}
	replay, err := f.service.CancelRun(t.Context(), f.alice, op)
	if err != nil {
		t.Fatal(err)
	}
	var got Run
	if err = json.Unmarshal(replay.Outcome.Response, &got); err != nil || got.State != RunReconciling {
		t.Fatalf("replay=%+v err=%v", got, err)
	}
	fresh, err := f.service.CancelRun(t.Context(), f.alice, command(t, map[string]any{"run_id": run.ID}))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(fresh.Outcome.Response, &got); err != nil || got.State != RunCancelled {
		t.Fatalf("fresh=%+v err=%v", got, err)
	}
}

// Stopping A frees the conversation while A's model call is still out. B runs
// in it at once, and A's late answer touches neither B nor the conversation.
func TestCStoppedRunsLateAnswerDoesNotReachTheNextRun(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	consent := f.consent(f.alice, c.ID)
	entered, release := make(chan struct{}), make(chan struct{})
	f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, n int) (llmgateway.Result, error) {
		result := scriptedResult(r)
		if n == 1 {
			close(entered)
			<-release
			result.Text = "A 的迟到回答"
			return result, nil
		}
		result.Text = "B 的回答"
		return result, nil
	}
	a, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "first"})
	if err != nil {
		t.Fatal(err)
	}
	workedA := make(chan error, 1)
	go func() { workedA <- f.work(f.alice, a.ID) }()
	// However the test ends, A is let go and its worker finishes before the
	// fixture's database closes.
	var unblock sync.Once
	t.Cleanup(func() {
		unblock.Do(func() { close(release) })
		<-workedA
	})
	<-entered
	receipt, err := f.service.CancelRun(t.Context(), f.alice, command(t, map[string]any{"run_id": a.ID}))
	if err != nil {
		t.Fatal(err)
	}
	var stopped Run
	if err = json.Unmarshal(receipt.Outcome.Response, &stopped); err != nil || stopped.State != RunCancelled {
		t.Fatalf("stopped=%+v err=%v", stopped, err)
	}
	b, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "second"})
	if err != nil {
		t.Fatalf("next run refused: %v", err)
	}
	if err = f.work(f.alice, b.ID); err != nil {
		t.Fatal(err)
	}
	unblock.Do(func() { close(release) })
	if err = <-workedA; err != nil {
		t.Fatal(err)
	}
	workedA <- nil // the cleanup reads once more
	f.sweepRuns()
	gotA, err := f.service.ReadRun(t.Context(), f.alice, a.ID)
	if err != nil || gotA.State != RunCancelled || gotA.SettlementState != "settled" {
		t.Fatalf("a=%+v err=%v", gotA, err)
	}
	gotB, err := f.service.ReadRun(t.Context(), f.alice, b.ID)
	if err != nil || gotB.State != RunSucceeded || gotB.SettlementState != "settled" {
		t.Fatalf("b=%+v err=%v", gotB, err)
	}
	if n := f.count("creative_agent_messages", "run_id=$1 AND role='assistant'", a.ID); n != 0 {
		t.Fatal("late answer published")
	}
	if n := f.count("creative_agent_messages", "conversation_id=$1 AND body::text LIKE '%A 的迟到回答%'", c.ID); n != 0 {
		t.Fatal("late answer reached the conversation")
	}
	if n := f.count("creative_agent_messages", "run_id=$1 AND role='assistant'", b.ID); n != 1 {
		t.Fatalf("b has %d answers", n)
	}
	assertRunReleased(t, f, a.ID)
	assertRunReleased(t, f, b.ID)
}

func TestCExpiredLeaseWithoutCheckpointStopsWithoutRedispatch(t *testing.T) {
	f := setup(t)
	run, held, _ := pauseCheckpointRun(t, f, false)
	if _, err := f.db.Exec(`DELETE FROM creative_checkpoint_content_refs WHERE checkpoint_id IN(SELECT id FROM creative_agent_checkpoints WHERE run_id=$1)`, run.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`DELETE FROM creative_agent_checkpoints WHERE run_id=$1`, run.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`UPDATE creative_agent_runs SET lease_until=now()-interval '1 minute' WHERE id=$1`, run.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.SweepRuns(t.Context(), f.alice, 20); err != nil {
		t.Fatal(err)
	}
	got, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
	if err != nil || got.State != RunFailed {
		t.Fatalf("run=%+v err=%v", got, err)
	}
	if err = f.service.finishRun(t.Context(), f.alice, held, turnOutcome{}, errRunInterrupted); err != nil {
		t.Fatal(err)
	}
	if f.vendor.calls() != 1 {
		t.Fatal("missing checkpoint redispatched")
	}
	assertRunReleased(t, f, run.ID)
}

func TestCWaitingSupplementKeepsOriginalRunAndClaim(t *testing.T) {
	f := setup(t)
	run, held, paused := pauseCheckpointRun(t, f, false)
	if err := f.service.completeAttempt(t.Context(), f.alice, held, paused.outcome, errRunInterrupted); err != nil {
		t.Fatal(err)
	}
	waiting, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
	if err != nil || waiting.State != RunWaitingInput || waiting.WaitingToken == "" {
		t.Fatalf("waiting=%+v %v", waiting, err)
	}
	if n := f.count("creative_agent_slots", "run_id=$1", run.ID); n != 0 {
		t.Fatal("waiting holds slot")
	}
	if _, err = f.service.SweepRuns(t.Context(), f.alice, 20); err != nil {
		t.Fatal(err)
	}
	f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, _ int) (llmgateway.Result, error) {
		raw, _ := json.Marshal(r.Chat)
		if !strings.Contains(string(raw), "补充要求") {
			t.Fatal("supplement absent from model input")
		}
		return scriptedResult(r), nil
	}
	input := map[string]any{"run_id": run.ID, "expected_revision": waiting.Revision, "waiting_token": waiting.WaitingToken, "text": "补充要求"}
	if _, err = f.service.SupplementRun(t.Context(), f.alice, command(t, input)); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.SupplementRun(t.Context(), f.alice, command(t, input)); !errors.Is(err, ErrRunState) {
		t.Fatalf("old waiting token reused: %v", err)
	}
	if err = f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	got, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
	if err != nil || got.State != RunSucceeded || !got.DeadlineAt.Equal(waiting.DeadlineAt) {
		t.Fatalf("run=%+v err=%v", got, err)
	}
	if f.vendor.calls() != 2 {
		t.Fatal("re-bought saved model turn")
	}
	assertRunReleased(t, f, run.ID)
}

func TestCCancelDuringDispatchDoesNotPublishLateAnswer(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	consent := f.consent(f.alice, c.ID)
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "test"})
	if err != nil {
		t.Fatal(err)
	}
	f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, _ int) (llmgateway.Result, error) {
		if _, err := f.service.CancelRun(t.Context(), f.alice, command(t, map[string]any{"run_id": run.ID})); err != nil {
			t.Fatal(err)
		}
		return scriptedResult(r), nil
	}
	if err = f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.SweepRuns(t.Context(), f.alice, 20); err != nil {
		t.Fatal(err)
	}
	got, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
	if err != nil || got.State != RunCancelled || got.SettlementState != "settled" {
		t.Fatalf("run=%+v err=%v", got, err)
	}
	if n := f.count("creative_agent_messages", "run_id=$1 AND role='assistant'", run.ID); n != 0 {
		t.Fatal("late answer published")
	}
	if n := f.count("llm_result_consumers", "state='pending'"); n != 0 {
		t.Fatal("known late result retained forever")
	}
	assertRunReleased(t, f, run.ID)
}

// A turn that delivered its answer but was never closed (the worker's close ran
// out of time) is finished by the sweep rather than queued again: each takeover
// would end in the same close and the run would spin until its deadline. A
// refused tool call earlier in the turn was answered, so it is not unfinished.
func TestCSweepClosesADeliveredRunWithARefusedTool(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	consent := contextConsent(t, f, c.ID)
	f.service.SetWebFetcher(&stubFetcher{})
	f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, n int) (llmgateway.Result, error) {
		if n == 1 {
			return scriptedResult(r, searchCall("s0", 0, " ")), nil
		}
		return scriptedResult(r), nil
	}
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "找找森系外景的资料"})
	if err != nil {
		t.Fatal(err)
	}
	held, err := f.service.claimRun(t.Context(), f.alice, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.executeWithCompaction(t.Context(), f.alice, held); err != nil {
		t.Fatal(err)
	}
	if n := f.count("creative_agent_steps", "run_id=$1 AND state='failed' AND error_code='creative_tool_refused'", run.ID); n != 1 {
		t.Fatalf("refused steps = %d", n)
	}
	if _, err = f.db.Exec(`UPDATE creative_agent_runs SET lease_until=now()-interval '1 minute' WHERE id=$1`, run.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.SweepRuns(t.Context(), f.alice, 20); err != nil {
		t.Fatal(err)
	}
	got, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
	if err != nil || got.State != RunSucceeded {
		t.Fatalf("run=%+v err=%v", got, err)
	}
	if f.vendor.calls() != 2 {
		t.Fatalf("model called %d times", f.vendor.calls())
	}
	assertRunReleased(t, f, run.ID)
}

func TestCExpiredLeaseRequeuesCompatibleCheckpoint(t *testing.T) {
	f := setup(t)
	run, held, _ := pauseCheckpointRun(t, f, false)
	if _, err := f.db.Exec(`UPDATE creative_agent_runs SET lease_until=now()-interval '1 minute' WHERE id=$1`, run.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.SweepRuns(t.Context(), f.alice, 20); err != nil {
		t.Fatal(err)
	}
	got, _ := f.service.ReadRun(t.Context(), f.alice, run.ID)
	if got.State != RunQueued {
		t.Fatalf("state=%s", got.State)
	}
	if err := f.service.finishRun(t.Context(), f.alice, held, turnOutcome{}, errRunInterrupted); err != nil {
		t.Fatal(err)
	}
	if err := f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	got, _ = f.service.ReadRun(t.Context(), f.alice, run.ID)
	if got.State != RunSucceeded || f.vendor.calls() != 2 {
		t.Fatalf("run=%+v calls=%d", got, f.vendor.calls())
	}
	assertRunReleased(t, f, run.ID)
}

func TestCTerminalRetryExecutesOnlyRecordedResourceRead(t *testing.T) {
	for _, interrupted := range []bool{false, true} {
		name := "normal"
		if interrupted {
			name = "interrupted delivery"
		}
		t.Run(name, func(t *testing.T) {
			f := setup(t)
			f.queue()
			req := skillRequest("platform", "retry-resource", "恢复测试")
			req.Activate = true
			req.Manifest.ToolAllowlist = []string{"read_skill_resource@1", "read_run_result@1"}
			version := f.runImport(f.platform, req)
			c := f.conversation(f.alice, f.canvasID)
			consent := f.consent(f.alice, c.ID)
			f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, _ int) (llmgateway.Result, error) {
				return scriptedResult(r, llmgateway.ToolCall{ID: "read", Name: readSkillTool, Arguments: resourceArgumentsFromRequest(t, r)}), nil
			}
			run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "读取参考"}, InstructionSegment{Type: "skill_ref", SkillID: version.SkillID, SkillVersionID: version.ID})
			if err != nil {
				t.Fatal(err)
			}
			held, err := f.service.claimRun(t.Context(), f.alice, run.ID)
			if err != nil {
				t.Fatal(err)
			}
			e, err := f.service.newRunExecution(t.Context(), f.alice, held)
			if err != nil {
				t.Fatal(err)
			}
			e.config.Handlers = append([]adk.ChatModelAgentMiddleware{&checkpointPause{BaseChatModelAgentMiddleware: &adk.BaseChatModelAgentMiddleware{}}}, e.config.Handlers...)
			if _, err = e.run(t.Context()); !errors.Is(err, errRunInterrupted) {
				t.Fatalf("pause %v", err)
			}
			if err = f.service.finishRun(t.Context(), f.alice, held, e.outcome, errRunInterrupted); err != nil {
				t.Fatal(err)
			}
			var step string
			if err = f.db.QueryRow(`SELECT id FROM creative_agent_steps WHERE run_id=$1 AND kind='tool'`, run.ID).Scan(&step); err != nil {
				t.Fatal(err)
			}
			input := map[string]any{"run_id": run.ID, "step_ids": []string{step}, "egress_consent_id": consent.ID}
			receipt, err := f.service.RetryRun(t.Context(), f.alice, command(t, input))
			if err != nil {
				t.Fatal(err)
			}
			var retry Run
			if err = json.Unmarshal(receipt.Outcome.Response, &retry); err != nil {
				t.Fatal(err)
			}
			if retry.ID == run.ID || retry.SourceRunID != run.ID || retry.TriggerMessageID == run.TriggerMessageID {
				t.Fatalf("retry=%+v", retry)
			}
			if interrupted {
				retryClaim, err := f.service.claimRun(t.Context(), f.alice, retry.ID)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = f.db.Exec(`CREATE FUNCTION fail_recovery_delivery() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'interrupt recovery delivery' USING ERRCODE='40001'; END $$;
CREATE TRIGGER fail_recovery_delivery BEFORE INSERT ON creative_agent_messages FOR EACH ROW WHEN (NEW.role='assistant') EXECUTE FUNCTION fail_recovery_delivery()`); err != nil {
					t.Fatal(err)
				}
				if _, err = f.service.executeRecordedRetry(t.Context(), f.alice, retryClaim); err == nil {
					t.Fatal("expected interrupted delivery")
				}
				if n := f.count("creative_agent_steps", "run_id=$1 AND kind='tool' AND state='succeeded'", retry.ID); n != 1 {
					t.Fatalf("committed recovery steps=%d", n)
				}
				if _, err = f.db.Exec(`DROP TRIGGER fail_recovery_delivery ON creative_agent_messages`); err != nil {
					t.Fatal(err)
				}
				for i := 0; i < 2; i++ {
					out, err := f.service.executeRecordedRetry(t.Context(), f.alice, retryClaim)
					if err != nil || !out.Delivered {
						t.Fatalf("resumed delivery=%+v err=%v", out, err)
					}
				}
				if err = f.service.finishRun(t.Context(), f.alice, retryClaim, turnOutcome{}, nil); err != nil {
					t.Fatal(err)
				}
			} else if err = f.work(f.alice, retry.ID); err != nil {
				t.Fatal(err)
			}
			got, err := f.service.ReadRun(t.Context(), f.alice, retry.ID)
			if err != nil || got.State != RunSucceeded || f.vendor.calls() != 1 {
				t.Fatalf("retry=%+v err=%v calls=%d", got, err, f.vendor.calls())
			}
			if _, err = f.service.RetryRun(t.Context(), f.alice, command(t, input)); !errors.Is(err, ErrRecoveryEvidence) {
				t.Fatalf("repeated completed recovery: %v", err)
			}
			if n := f.count("creative_agent_messages", "run_id=$1 AND role='assistant'", retry.ID); n != 1 {
				t.Fatalf("recovery assistant deliveries=%d, want 1", n)
			}
			if n := f.count("creative_agent_messages", "run_id=$1 AND role='tool'", retry.ID); n != 0 {
				t.Fatalf("recovery without model call produced %d orphan tool messages", n)
			}
			if err = f.work(f.alice, retry.ID); err != nil {
				t.Fatal(err)
			}
			if n := f.count("creative_agent_messages", "run_id=$1 AND role='assistant'", retry.ID); n != 1 {
				t.Fatalf("recovery replay duplicated delivery: %d", n)
			}
			// The delivery is one text block, beside the recovered step's own row.
			if n := f.count("creative_agent_blocks", "run_id=$1 AND type='text' AND state='complete'", retry.ID); n != 1 {
				t.Fatalf("recovery delivery blocks=%d, want 1", n)
			}
			if n := f.count("creative_agent_blocks", "run_id=$1 AND type='tool' AND state='succeeded'", retry.ID); n != 1 {
				t.Fatalf("recovered tool blocks=%d, want 1", n)
			}
			assertRunReleased(t, f, retry.ID)
			f.vendor.script = func(_ context.Context, request llmgateway.ProviderRequest, _ int) (llmgateway.Result, error) {
				found := false
				for _, message := range request.Chat.Messages {
					for _, block := range message.Blocks {
						found = found || strings.Contains(block.Text, "已恢复原步骤的资源读取")
					}
				}
				if !found {
					t.Error("next turn lost the completed recovery history")
				}
				return scriptedResult(request), nil
			}
			followup, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "继续整理恢复结果"})
			if err != nil {
				t.Fatal(err)
			}
			if err = f.work(f.alice, followup.ID); err != nil {
				t.Fatal(err)
			}
			got, err = f.service.ReadRun(t.Context(), f.alice, followup.ID)
			if err != nil || got.State != RunSucceeded || f.vendor.calls() != 2 {
				t.Fatalf("followup=%+v err=%v calls=%d", got, err, f.vendor.calls())
			}

		})
	}
}
func TestCTextOnlyConsentDoesNotGrantLibraryAccess(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	receipt, err := f.service.GrantConsent(t.Context(), f.alice, command(t, map[string]any{
		"conversation_id": c.ID, "purpose": "creative_assistance",
		"mode": "selected_revisions", "data_classes": []string{"text"}, "selected_revision_ids": []string{},
	}))
	if err != nil {
		t.Fatal(err)
	}
	var consent Consent
	if err = json.Unmarshal(receipt.Outcome.Response, &consent); err != nil {
		t.Fatal(err)
	}
	if consent.Scope.Mode != ScopeSelectedRevisions || consent.ApprovedRevisions != 0 {
		t.Fatalf("broad consent: %+v", consent)
	}
	revision := f.revision(f.alice, "未授权的资料")
	if _, err = f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "content_ref", ContentRevisionID: revision}); err == nil {
		t.Fatal("text-only consent granted content access")
	}
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "整理问题"})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
}

func TestCRecoveredQueueClosurePreservesUnknownUsage(t *testing.T) {
	for _, reason := range []string{"deadline", "model-disabled"} {
		t.Run(reason, func(t *testing.T) {
			f := setup(t)
			run, _, _ := pauseCheckpointRun(t, f, false, true)
			if _, err := f.db.Exec(`UPDATE creative_agent_runs SET lease_until=now()-interval '1 minute' WHERE id=$1`, run.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := f.service.SweepRuns(t.Context(), f.alice, 20); err != nil {
				t.Fatal(err)
			}
			queued, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
			if err != nil || queued.State != RunQueued || (queued.SettlementState != "unknown" && queued.SettlementState != "pending") {
				t.Fatalf("queued=%+v %v", queued, err)
			}
			if reason == "deadline" {
				if _, err = f.db.Exec(`UPDATE creative_agent_runs SET created_at=now()-interval '10 minutes',deadline_at=now()-interval '1 second' WHERE id=$1`, run.ID); err != nil {
					t.Fatal(err)
				}
			} else {
				f.service.models, err = llmgateway.DefaultCatalog(func(string) (string, bool) { return "", false })
				if err != nil {
					t.Fatal(err)
				}
			}
			if err = f.work(f.alice, run.ID); err != nil {
				t.Fatal(err)
			}
			got, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
			if err != nil || got.State != RunFailed || got.SettlementState != queued.SettlementState {
				t.Fatalf("closed=%+v %v", got, err)
			}
			if f.vendor.calls() != 1 {
				t.Fatal("closed recovered run redispatched")
			}
			if n := f.count("creative_agent_slots", "run_id=$1", run.ID); n != 0 {
				t.Fatal("slot retained")
			}
		})
	}
}

func TestCSweepDeadlineDoesNotSkipUnattemptedAccounts(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	consent := f.consent(f.alice, c.ID)
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(`UPDATE creative_agent_runs SET updated_at=now()-interval '1 minute' WHERE id=$1`, run.ID); err != nil {
		t.Fatal(err)
	}
	blocker, err := f.db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = blocker.Rollback() }()
	if _, err = blocker.Exec(`SELECT 1 FROM creative_agent_slots WHERE account_id='agent-a' FOR UPDATE`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	next, err := f.service.SweepAllAccounts(ctx, f.st, "", 10)
	if err == nil || next != "agent-a" {
		t.Fatalf("next=%q err=%v; unattempted suffix must remain reachable", next, err)
	}
}
