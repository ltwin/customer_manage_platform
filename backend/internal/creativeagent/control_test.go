package creativeagent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
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

func TestCUnknownWaitsForReconciliationAndCanBeClosed(t *testing.T) {
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
	if n := f.count("creative_agent_slots", "run_id=$1", run.ID); n != 1 {
		t.Fatal("unknown lost its slot")
	}
	if _, err = f.service.CancelRun(t.Context(), f.alice, command(t, map[string]any{"run_id": run.ID})); err != nil {
		t.Fatal(err)
	}
	got, _ = f.service.ReadRun(t.Context(), f.alice, run.ID)
	if got.State != RunReconciling {
		t.Fatal("cancel discarded unknown")
	}
	receipt, err := f.service.CloseReconciliation(t.Context(), f.alice, command(t, map[string]any{"run_id": run.ID}))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(receipt.Outcome.Response, &got); err != nil {
		t.Fatal(err)
	}
	if got.State != RunCancelled || got.SettlementState != "unknown" {
		t.Fatalf("closed=%+v", got)
	}
	if n := f.count("creative_agent_slots", "run_id=$1", run.ID); n != 0 {
		t.Fatal("close kept slot")
	}
	if n := f.count("llm_result_consumers", "state='pending'"); n != 1 {
		t.Fatal("unknown evidence discarded")
	}
	if _, err = f.service.SweepRuns(t.Context(), f.alice, 20); err != nil {
		t.Fatal(err)
	}
	if f.vendor.calls() != 1 {
		t.Fatal("unknown redispatched")
	}
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
	if err = f.work(f.alice, retry.ID); err != nil {
		t.Fatal(err)
	}
	got, err := f.service.ReadRun(t.Context(), f.alice, retry.ID)
	if err != nil || got.State != RunSucceeded || f.vendor.calls() != 1 {
		t.Fatalf("retry=%+v err=%v calls=%d", got, err, f.vendor.calls())
	}
	if _, err = f.service.RetryRun(t.Context(), f.alice, command(t, input)); !errors.Is(err, ErrRecoveryEvidence) {
		t.Fatalf("repeated completed recovery: %v", err)
	}
	assertRunReleased(t, f, retry.ID)
}

func TestCTextOnlyConsentDoesNotGrantLibraryAccess(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	receipt, err := f.service.GrantConsent(t.Context(), f.alice, command(t, map[string]any{
		"conversation_id": c.ID, "vendor_key": "deepseek", "purpose": "creative_assistance",
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
