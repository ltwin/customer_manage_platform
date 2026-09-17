package creativeagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"

	"github.com/samson/customer-manage-platform/backend/internal/creativeskill"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/platform/versionedfs"
)

func TestB3ExistingJournalDoesNotRestartWithoutCheckpoint(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	consent := f.consent(f.alice, c.ID)
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "整理"})
	if err != nil {
		t.Fatal(err)
	}
	held, err := f.service.claimRun(t.Context(), f.alice, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	chat := llmgateway.ChatRequest{ContractVersion: llmgateway.ContractVersion, ModelKey: held.Model.ModelKey, Messages: []llmgateway.Message{{Role: llmgateway.RoleUser, Blocks: []llmgateway.Block{{Kind: llmgateway.BlockText, Text: "整理"}}}}, OutputLimit: 100}
	if _, _, err = f.service.prepareModelStep(t.Context(), f.alice, held, "", chat); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.executeTurn(t.Context(), f.alice, held); !errors.Is(err, errCheckpointMissing) || f.vendor.calls() != 0 {
		t.Fatalf("restarted journal without checkpoint: err=%v vendor_calls=%d", err, f.vendor.calls())
	}
}

type checkpointPause struct {
	*adk.BaseChatModelAgentMiddleware
	after bool
}

func (p *checkpointPause) WrapInvokableToolCall(_ context.Context, next adk.InvokableToolCallEndpoint, _ *adk.ToolContext) (adk.InvokableToolCallEndpoint, error) {
	return func(ctx context.Context, args string, opts ...tool.Option) (string, error) {
		resumed, _, _ := tool.GetInterruptState[string](ctx)
		if resumed {
			return next(ctx, args, opts...)
		}
		if p.after {
			if _, err := next(ctx, args, opts...); err != nil {
				return "", err
			}
		}
		return "", tool.StatefulInterrupt(ctx, "checkpoint boundary", args)
	}, nil
}
func pauseCheckpointRun(t *testing.T, f *fixture, after bool, missingUsage ...bool) (Run, claim, *runExecution) {
	t.Helper()
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	revision := f.revision(f.alice, "检查点引用材料")
	consent := f.consent(f.alice, c.ID, revision)
	f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, n int) (llmgateway.Result, error) {
		if n == 1 {
			result := scriptedResult(r, llmgateway.ToolCall{ID: "reused-id", Name: readResultTool, Arguments: json.RawMessage(`{"item_id":"missing","offset":0,"limit":100}`)})
			if len(missingUsage) > 0 && missingUsage[0] {
				result.Usage = llmgateway.UsageEvidence{}
			}
			return result, nil
		}
		return scriptedResult(r), nil
	}
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "整理"}, InstructionSegment{Type: "content_ref", ContentRevisionID: revision})
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
	execution.config.Handlers = append([]adk.ChatModelAgentMiddleware{&checkpointPause{BaseChatModelAgentMiddleware: &adk.BaseChatModelAgentMiddleware{}, after: after}}, execution.config.Handlers...)
	if _, err = execution.run(t.Context()); !errors.Is(err, errRunInterrupted) {
		t.Fatalf("interrupt: %v", err)
	}
	if f.vendor.calls() != 1 {
		t.Fatalf("pause called model %d times", f.vendor.calls())
	}
	return run, held, execution
}
func TestB3CheckpointResumesCommittedToolsAndModelResults(t *testing.T) {
	for _, after := range []bool{false, true} {
		t.Run(fmt.Sprintf("after-tool-%t", after), func(t *testing.T) {
			f := setup(t)
			run, held, _ := pauseCheckpointRun(t, f, after)
			f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, n int) (llmgateway.Result, error) {
				if n == 2 {
					return scriptedResult(r, llmgateway.ToolCall{ID: "reused-id", Name: readResultTool, Arguments: json.RawMessage(`{"item_id":"missing","offset":0,"limit":100}`)}), nil
				}
				return scriptedResult(r), nil
			}
			resumed, err := f.service.newRunExecution(t.Context(), f.alice, held)
			if err != nil {
				t.Fatal(err)
			}
			out, err := resumed.run(t.Context())
			if err != nil || !out.Delivered {
				t.Fatalf("resume: %+v %v", out, err)
			}
			if f.vendor.calls() != 3 {
				t.Fatalf("provider calls=%d", f.vendor.calls())
			}
			// Leave the old checkpoint behind the committed final answer. A new runtime
			// must replay both the original tool output and the original paid model turn.
			replay, err := f.service.newRunExecution(t.Context(), f.alice, held)
			if err != nil {
				t.Fatal(err)
			}
			out, err = replay.run(t.Context())
			if err != nil || !out.Delivered {
				t.Fatalf("replay: %+v %v", out, err)
			}
			if f.vendor.calls() != 3 {
				t.Fatalf("paid again: %d", f.vendor.calls())
			}
			if n := f.count("creative_agent_context_items", "run_id=$1", run.ID); n != 2 {
				t.Fatalf("tool repeated: %d", n)
			}
			if n := f.count("creative_agent_messages", "run_id=$1 AND role='assistant'", run.ID); n != 1 {
				t.Fatalf("answer repeated: %d", n)
			}
			if err = f.service.finishRun(t.Context(), f.alice, held, out, nil); err != nil {
				t.Fatal(err)
			}
			assertRunReleased(t, f, run.ID)
		})
	}
}
func TestB3SavedGatewayResultIsConsumedAfterResumeWithoutRedispatch(t *testing.T) {
	f := setup(t)
	run, held, _ := pauseCheckpointRun(t, f, false)
	if _, err := f.db.Exec(`CREATE SEQUENCE consume_once;
 CREATE FUNCTION fail_consume_once() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.role='assistant' AND nextval('consume_once')=1 THEN RAISE EXCEPTION 'rollback consume' USING ERRCODE='40001'; END IF; RETURN NEW; END $$;
 CREATE TRIGGER fail_consume_once BEFORE INSERT ON creative_agent_messages FOR EACH ROW EXECUTE FUNCTION fail_consume_once()`); err != nil {
		t.Fatal(err)
	}
	execution, err := f.service.newRunExecution(t.Context(), f.alice, held)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = execution.run(t.Context()); err == nil {
		t.Fatal("fault did not roll back consumption")
	}
	if f.vendor.calls() != 2 {
		t.Fatalf("calls=%d", f.vendor.calls())
	}
	resumed, err := f.service.newRunExecution(t.Context(), f.alice, held)
	if err != nil {
		t.Fatal(err)
	}
	out, err := resumed.run(t.Context())
	if err != nil || !out.Delivered || f.vendor.calls() != 2 {
		t.Fatalf("resume %+v %v calls=%d", out, err, f.vendor.calls())
	}
	if err = f.service.finishRun(t.Context(), f.alice, held, out, nil); err != nil {
		t.Fatal(err)
	}
	assertRunReleased(t, f, run.ID)
}

func TestB3CheckpointFencesVersionsClaimsAndWrites(t *testing.T) {
	f := setup(t)
	run, held, first := pauseCheckpointRun(t, f, false)
	payload, ok, err := first.checkpoint.Get(t.Context(), run.ID)
	if err != nil || !ok {
		t.Fatalf("read: %t %v", ok, err)
	}
	stale, err := f.service.newRunExecution(t.Context(), f.alice, held)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = stale.checkpoint.Get(t.Context(), run.ID); err != nil {
		t.Fatal(err)
	}
	if err = first.checkpoint.Set(t.Context(), run.ID, payload); err != nil {
		t.Fatal(err)
	}
	if err = stale.checkpoint.Set(t.Context(), run.ID, payload); !errors.Is(err, errCheckpointConflict) {
		t.Fatalf("lost CAS: %v", err)
	}
	if err = first.checkpoint.Set(t.Context(), run.ID, make([]byte, checkpointMaxBytes+1)); !errors.Is(err, ErrLimit) {
		t.Fatalf("oversize: %v", err)
	}
	if _, _, err = first.checkpoint.Get(t.Context(), "another-run"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross run: %v", err)
	}
	first.checkpoint.runtime.scope = f.bob
	if _, _, err = first.checkpoint.Get(t.Context(), run.ID); err == nil {
		t.Fatal("cross account checkpoint was visible")
	}
	first.checkpoint.runtime.scope = f.alice
	for _, tc := range []struct{ column, value string }{{"runtime_version", "eino/other"}, {"serializer_version", "other"}, {"registry_digest", "other"}, {"skill_catalog_digest", "other"}} {
		t.Run(tc.column, func(t *testing.T) {
			var old string
			if err = f.db.QueryRow("SELECT "+tc.column+" FROM creative_agent_checkpoints WHERE run_id=$1", run.ID).Scan(&old); err != nil {
				t.Fatal(err)
			}
			if _, err = f.db.Exec("UPDATE creative_agent_checkpoints SET "+tc.column+"=$1 WHERE run_id=$2", tc.value, run.ID); err != nil {
				t.Fatal(err)
			}
			if _, _, err = first.checkpoint.Get(t.Context(), run.ID); !errors.Is(err, errCheckpointVersion) {
				t.Fatalf("incompatible: %v", err)
			}
			if _, err = f.db.Exec("UPDATE creative_agent_checkpoints SET "+tc.column+"=$1 WHERE run_id=$2", old, run.ID); err != nil {
				t.Fatal(err)
			}
		})
	}
	// A takeover changes both execution fences, independently of row revision.
	if _, err = f.db.Exec(`UPDATE creative_agent_runs SET execution_epoch=execution_epoch+1 WHERE id=$1`, run.ID); err != nil {
		t.Fatal(err)
	}
	if err = first.checkpoint.Set(t.Context(), run.ID, payload); !errors.Is(err, ErrRunState) {
		t.Fatalf("old epoch write: %v", err)
	}
}
func TestB3CheckpointRootsDeleteAtomically(t *testing.T) {
	f := setup(t)
	run, _, execution := pauseCheckpointRun(t, f, false)
	if n := f.count("creative_checkpoint_content_refs", "checkpoint_id IN (SELECT id FROM creative_agent_checkpoints WHERE run_id=$1)", run.ID); n == 0 {
		t.Fatal("checkpoint did not retain its input revision")
	}
	if _, err := f.db.Exec(`CREATE FUNCTION reject_checkpoint_delete() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'delete failed' USING ERRCODE='40001'; END $$;
 CREATE TRIGGER reject_checkpoint_delete BEFORE DELETE ON creative_agent_checkpoints FOR EACH ROW EXECUTE FUNCTION reject_checkpoint_delete()`); err != nil {
		t.Fatal(err)
	}
	if err := execution.checkpoint.Delete(t.Context(), run.ID); err == nil {
		t.Fatal("expected rollback")
	}
	if n := f.count("creative_checkpoint_content_refs", "true"); n == 0 {
		t.Fatal("refs were deleted outside payload transaction")
	}
	if _, err := f.db.Exec(`DROP TRIGGER reject_checkpoint_delete ON creative_agent_checkpoints`); err != nil {
		t.Fatal(err)
	}
	if err := execution.checkpoint.Delete(t.Context(), run.ID); err != nil {
		t.Fatal(err)
	}
	if n := f.count("creative_checkpoint_content_refs", "true"); n != 0 {
		t.Fatalf("orphan refs=%d", n)
	}
	if n := f.count("creative_agent_checkpoints", "run_id=$1", run.ID); n != 0 {
		t.Fatalf("checkpoints=%d", n)
	}
}
func TestB3CheckpointRejectsDivergedJournalAndUnknownDispatch(t *testing.T) {
	t.Run("changed input", func(t *testing.T) {
		f := setup(t)
		run, held, _ := pauseCheckpointRun(t, f, false)
		if _, err := f.db.Exec(`UPDATE creative_agent_steps SET input_hash='changed' WHERE run_id=$1 AND kind='model'`, run.ID); err != nil {
			t.Fatal(err)
		}
		e, err := f.service.newRunExecution(t.Context(), f.alice, held)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = e.run(t.Context()); !errors.Is(err, errCheckpointJournal) || f.vendor.calls() != 1 {
			t.Fatalf("diverged checkpoint: %v calls=%d", err, f.vendor.calls())
		}
	})
	t.Run("unknown request", func(t *testing.T) {
		f := setup(t)
		_, held, _ := pauseCheckpointRun(t, f, false)
		f.vendor.script = func(context.Context, llmgateway.ProviderRequest, int) (llmgateway.Result, error) {
			return llmgateway.Result{}, &llmgateway.ProviderError{Outcome: llmgateway.OutcomeUnknown, Class: "connection_lost", Detail: "accepted status unknown"}
		}
		e, err := f.service.newRunExecution(t.Context(), f.alice, held)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = e.run(t.Context()); err == nil {
			t.Fatal("expected transport failure")
		}
		if f.vendor.calls() != 2 {
			t.Fatalf("calls=%d", f.vendor.calls())
		}
		again, err := f.service.newRunExecution(t.Context(), f.alice, held)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = again.run(t.Context()); !errors.Is(err, llmgateway.ErrUnknown) || f.vendor.calls() != 2 {
			t.Fatalf("redispatched unknown result: %v calls=%d", err, f.vendor.calls())
		}
	})
}

func TestB3CheckpointResumesInAnotherProcess(t *testing.T) {
	f := setup(t)
	run, _, _ := pauseCheckpointRun(t, f, true)
	// C owns when a run is made queued. This fixture simulates that handoff and
	// exercises the real claimant + runner path with a fresh process and epoch.
	if _, err := f.db.Exec(`UPDATE creative_agent_runs SET state='queued',lease_until=NULL,execution_epoch=execution_epoch+1 WHERE id=$1`, run.ID); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestB3CheckpointChild$", "-test.v")
	cmd.Env = append(os.Environ(), "CREATIVE_B3_TEST_URL="+f.url, "CREATIVE_B3_TEST_RUN="+run.ID)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("child: %v\n%s", err, output)
	}
	got, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
	if err != nil || got.State != RunSucceeded {
		t.Fatalf("run=%+v err=%v", got, err)
	}
	if n := f.count("llm_requests", "caller_group_id=$1", run.ID); n != 2 {
		t.Fatalf("requests=%d", n)
	}
	if n := f.count("creative_agent_context_items", "run_id=$1", run.ID); n != 1 {
		t.Fatalf("tool executed again: %d", n)
	}
	assertRunReleased(t, f, run.ID)
}

func TestB3CheckpointChild(t *testing.T) {
	url, runID := os.Getenv("CREATIVE_B3_TEST_URL"), os.Getenv("CREATIVE_B3_TEST_RUN")
	if url == "" {
		t.Skip("subprocess helper")
	}
	st, err := store.Open(t.Context(), url)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	models, err := llmgateway.DefaultCatalog(func(string) (string, bool) { return "test-credential", true })
	if err != nil {
		t.Fatal(err)
	}
	vendor := &stubVendor{}
	gateway, err := llmgateway.New(llmgateway.Config{Catalog: models, Providers: map[llmgateway.ProviderKey]llmgateway.Provider{llmgateway.ProviderOpenAICompatible: vendor}, Credential: func(string) (string, bool) { return "test-credential", true }, DefaultBudget: llmgateway.BudgetPolicy{MonthlyLimitMicros: 20_000_000, MonthlyTokenLimit: 5_000_000}})
	if err != nil {
		t.Fatal(err)
	}
	objects, err := versionedfs.NewLocal(t.TempDir(), func(string, string, int, time.Time) versionedfs.PartAuthorization {
		return versionedfs.PartAuthorization{}
	})
	if err != nil {
		t.Fatal(err)
	}
	skills, err := creativeskill.NewService(objects, st.ScopeFor(auth.AccountContext{AccountID: platformPublisher}))
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(gateway, models, skills)
	if err != nil {
		t.Fatal(err)
	}
	scope := st.ScopeFor(auth.AccountContext{AccountID: "agent-a"})
	held, err := service.claimRun(t.Context(), scope, runID)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := service.executeTurn(t.Context(), scope, held)
	if err != nil || !outcome.Delivered {
		t.Fatalf("resume: %+v %v", outcome, err)
	}
	if vendor.calls() != 1 {
		t.Fatalf("child re-bought old model call: %d", vendor.calls())
	}
	if err = service.finishRun(t.Context(), scope, held, outcome, nil); err != nil {
		t.Fatal(err)
	}
}

func TestB3CheckpointRevalidatesMutableAuthority(t *testing.T) {
	for _, kind := range []string{"revoked", "cancelled", "terminal", "expired"} {
		t.Run(kind, func(t *testing.T) {
			f := setup(t)
			run, held, e := pauseCheckpointRun(t, f, false)
			switch kind {
			case "revoked":
				if _, err := f.service.RevokeConsent(t.Context(), f.alice, command(t, map[string]any{"consent_id": held.ConsentID, "expected_revision": "1"})); err != nil {
					t.Fatal(err)
				}
			case "cancelled":
				if _, err := f.db.Exec(`UPDATE creative_agent_runs SET cancel_requested_at=now() WHERE id=$1`, run.ID); err != nil {
					t.Fatal(err)
				}
			case "terminal":
				if err := f.service.finishRun(t.Context(), f.alice, held, turnOutcome{}, errRunInterrupted); err != nil {
					t.Fatal(err)
				}
			case "expired":
				e.runtime.held.Deadline = time.Now().Add(-time.Second)
			}
			if _, _, err := e.checkpoint.Get(t.Context(), run.ID); err == nil {
				t.Fatalf("%s checkpoint accepted", kind)
			}
			if f.vendor.calls() != 1 {
				t.Fatal("authorization check called provider")
			}
		})
	}
}
func TestB3UnknownCostSurvivesMissingCheckpoint(t *testing.T) {
	f := setup(t)
	run, held, paused := pauseCheckpointRun(t, f, false)
	f.vendor.script = func(context.Context, llmgateway.ProviderRequest, int) (llmgateway.Result, error) {
		return llmgateway.Result{}, &llmgateway.ProviderError{Outcome: llmgateway.OutcomeUnknown, Class: "lost", Detail: "unknown acceptance"}
	}
	e, err := f.service.newRunExecution(t.Context(), f.alice, held)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.run(t.Context()); err == nil {
		t.Fatal("missing injected failure")
	}
	if err = paused.checkpoint.Delete(t.Context(), run.ID); err != nil {
		t.Fatal(err)
	}
	if err = f.service.finishRun(t.Context(), f.alice, held, turnOutcome{}, errCheckpointMissing); err != nil {
		t.Fatal(err)
	}
	got, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
	if err != nil || got.SettlementState != "unknown" {
		t.Fatalf("run=%+v err=%v", got, err)
	}
	if n := f.count("llm_result_consumers", "state='pending'"); n != 1 {
		t.Fatalf("unknown consumer lost: %d", n)
	}
}
func TestB3CheckpointRejectsChangedLaterModelFrame(t *testing.T) {
	f := setup(t)
	run, held, _ := pauseCheckpointRun(t, f, false)
	e, err := f.service.newRunExecution(t.Context(), f.alice, held)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.run(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(`UPDATE creative_agent_steps SET input_hash='changed' WHERE run_id=$1 AND kind='model' AND ordinal>1`, run.ID); err != nil {
		t.Fatal(err)
	}
	next, err := f.service.newRunExecution(t.Context(), f.alice, held)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = next.run(t.Context()); !errors.Is(err, errCheckpointJournal) || f.vendor.calls() != 2 {
		t.Fatalf("new frame after mismatch: %v calls=%d", err, f.vendor.calls())
	}
}

// Upgrade decisions cannot silently retain the previous runtime's checkpoint
// label. Go test binaries omit module dependencies from ReadBuildInfo, so the
// build contract is checked against the pinned module manifest instead.
func TestB3CheckpointRuntimeMatchesPinnedDependency(t *testing.T) {
	raw, err := os.ReadFile("../../go.mod")
	if err != nil {
		t.Fatal(err)
	}
	pattern := regexp.MustCompile(`(?m)^\s*github\.com/cloudwego/eino\s+(v[^\s]+)\s*$`)
	match := pattern.FindStringSubmatch(string(raw))
	if len(match) != 2 || "eino/"+match[1] != checkpointRuntime {
		t.Fatalf("checkpoint runtime must match pinned Eino version")
	}
	if strings.Contains(string(raw), "replace github.com/cloudwego/eino") {
		t.Fatal("a replacement needs a separate checkpoint runtime identity")
	}
}

// A saved result belongs to the durable step, but the callback may still be held
// by a worker from before takeover. Its failed authority check must not consume it.
func TestB3OldConsumerCannotStealResultAfterTakeover(t *testing.T) {
	f := setup(t)
	run, held, _ := pauseCheckpointRun(t, f, false)
	if _, err := f.db.Exec(`CREATE FUNCTION fail_consume() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'rollback consume' USING ERRCODE='40001'; END $$;
 CREATE TRIGGER fail_consume BEFORE INSERT ON creative_agent_messages FOR EACH ROW EXECUTE FUNCTION fail_consume()`); err != nil {
		t.Fatal(err)
	}
	old, err := f.service.newRunExecution(t.Context(), f.alice, held)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = old.run(t.Context()); err == nil {
		t.Fatal("expected consumer rollback")
	}
	if _, err = f.db.Exec(`DROP TRIGGER fail_consume ON creative_agent_messages`); err != nil {
		t.Fatal(err)
	}
	_, binding := old.runtime.current.get()
	view, err := f.service.gateway.RequestForBinding(t.Context(), f.alice, callerService, binding)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(`UPDATE creative_agent_runs SET state='queued',lease_until=NULL WHERE id=$1`, run.ID); err != nil {
		t.Fatal(err)
	}
	next, err := f.service.claimRun(t.Context(), f.alice, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	err = f.alice.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		_, _, err := f.service.gateway.ConsumeInTx(t.Context(), tx, view.ID, callerService, "bind:"+binding, func(result llmgateway.Result) error { return old.consumeInTx(t.Context(), tx, result) })
		return err
	})
	if !errors.Is(err, ErrRunState) {
		t.Fatalf("stale consumer accepted: %v", err)
	}
	if n := f.count("llm_result_consumers", "request_id=$1 AND state='pending'", view.ID); n != 1 {
		t.Fatalf("pending consumer lost: %d", n)
	}
	out, err := f.service.executeTurn(t.Context(), f.alice, next)
	if err != nil || !out.Delivered || f.vendor.calls() != 2 {
		t.Fatalf("resume %+v %v calls=%d", out, err, f.vendor.calls())
	}
	if err = f.service.finishRun(t.Context(), f.alice, next, out, nil); err != nil {
		t.Fatal(err)
	}
	assertRunReleased(t, f, run.ID)
}

func TestB3OldFinisherCannotMutateTakenOverJournal(t *testing.T) {
	for _, after := range []bool{false, true} {
		t.Run(fmt.Sprintf("after-tool-%t", after), func(t *testing.T) {
			f := setup(t)
			run, held, paused := pauseCheckpointRun(t, f, after)
			if _, err := f.db.Exec(`UPDATE creative_agent_runs SET state='queued',lease_until=NULL WHERE id=$1`, run.ID); err != nil {
				t.Fatal(err)
			}
			next, err := f.service.claimRun(t.Context(), f.alice, run.ID)
			if err != nil {
				t.Fatal(err)
			}
			var before, afterRows string
			query := `SELECT jsonb_agg(to_jsonb(s) ORDER BY ordinal)::text FROM creative_agent_steps s WHERE run_id=$1`
			if err = f.db.QueryRow(query, run.ID).Scan(&before); err != nil {
				t.Fatal(err)
			}
			if err = f.service.finishRun(t.Context(), f.alice, held, paused.outcome, errRunInterrupted); err != nil {
				t.Fatal(err)
			}
			if err = f.db.QueryRow(query, run.ID).Scan(&afterRows); err != nil {
				t.Fatal(err)
			}
			if before != afterRows {
				t.Fatal("old finisher changed taken-over steps")
			}
			out, err := f.service.executeTurn(t.Context(), f.alice, next)
			if err != nil || !out.Delivered || f.vendor.calls() != 2 {
				t.Fatalf("resume %+v %v calls=%d", out, err, f.vendor.calls())
			}
			if err = f.service.finishRun(t.Context(), f.alice, next, out, nil); err != nil {
				t.Fatal(err)
			}
			assertRunReleased(t, f, run.ID)
		})
	}
}

func TestB3TerminalRetentionStartsAtFinish(t *testing.T) {
	f := setup(t)
	run, held, _ := pauseCheckpointRun(t, f, true)
	if err := f.service.finishRun(t.Context(), f.alice, held, turnOutcome{}, errRunInterrupted); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"creative_agent_checkpoints", "creative_agent_context_items"} {
		var early int
		query := `SELECT count(*) FROM ` + table + ` c JOIN creative_agent_runs r ON r.account_id=c.account_id AND r.id=c.run_id WHERE r.id=$1 AND c.retained_until<r.finished_at+interval '90 days'`
		if err := f.db.QueryRow(query, run.ID).Scan(&early); err != nil {
			t.Fatal(err)
		}
		if early != 0 {
			t.Fatalf("%s expires before terminal retention window", table)
		}
	}
}

func TestB3FinisherRequiresCurrentEpochStateAndSlot(t *testing.T) {
	for _, mutation := range []struct{ name, sql string }{
		{name: "epoch-only", sql: `UPDATE creative_agent_runs SET execution_epoch=execution_epoch+1 WHERE id=$1`},
		{name: "queued-before-claim", sql: `UPDATE creative_agent_runs SET state='queued',lease_until=NULL,execution_epoch=execution_epoch+1 WHERE id=$1`},
		{name: "reconciling-before-claim", sql: `UPDATE creative_agent_runs SET state='reconciling',lease_until=NULL,execution_epoch=execution_epoch+1 WHERE id=$1`},
		{name: "state-only", sql: `UPDATE creative_agent_runs SET state='queued',lease_until=NULL WHERE id=$1`},
		{name: "slot-token", sql: `UPDATE creative_agent_slots SET claim_token='another-holder' WHERE run_id=$1`},
		{name: "slot-released", sql: `UPDATE creative_agent_slots SET run_id=NULL,claim_token=NULL,acquired_at=NULL WHERE run_id=$1`},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			f := setup(t)
			run, held, paused := pauseCheckpointRun(t, f, false)
			if _, err := f.db.Exec(mutation.sql, run.ID); err != nil {
				t.Fatal(err)
			}
			snapshot := func() string {
				t.Helper()
				var result string
				err := f.db.QueryRow(`SELECT jsonb_build_object(
    'run',(SELECT to_jsonb(r) FROM creative_agent_runs r WHERE id=$1),
    'steps',(SELECT jsonb_agg(to_jsonb(s) ORDER BY ordinal) FROM creative_agent_steps s WHERE run_id=$1),
    'slot',(SELECT to_jsonb(s) FROM creative_agent_slots s WHERE account_id='agent-a'),
    'checkpoint',(SELECT to_jsonb(c) FROM creative_agent_checkpoints c WHERE run_id=$1),
    'budget',(SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM llm_usage_reservations r WHERE caller_group_id=$1)
   )::text`, run.ID).Scan(&result)
				if err != nil {
					t.Fatal(err)
				}
				return result
			}
			before := snapshot()
			if err := f.service.finishRun(t.Context(), f.alice, held, paused.outcome, errRunInterrupted); err != nil {
				t.Fatal(err)
			}
			if before != snapshot() {
				t.Fatal("finisher without current ownership mutated run, steps, slot, checkpoint or budget")
			}
		})
	}
}
