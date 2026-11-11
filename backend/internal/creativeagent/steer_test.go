package creativeagent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"

	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
)

// steerDuringAnswer stands for a steer accepted while the final answer was
// being written: the batch exists, the request that answered never saw it.
func steerDuringAnswer(t *testing.T, f *fixture, runID, text string) {
	t.Helper()
	if _, err := f.db.Exec(`INSERT INTO creative_run_inputs(account_id,run_id,batch_ordinal,ordinal,input_role,read_level,body)
		VALUES ('agent-a',$1,1,0,'instruction','text',$2)`, runID, text); err != nil {
		t.Fatal(err)
	}
}

// occurrences counts how many messages of one request carry text, by role.
func occurrences(chat llmgateway.ChatRequest, role llmgateway.Role, text string) int {
	n := 0
	for _, m := range chat.Messages {
		if m.Role != role {
			continue
		}
		for _, b := range m.Blocks {
			if strings.Contains(b.Text, text) {
				n++
				break
			}
		}
	}
	return n
}

func TestSteerAfterTheFinalAnswerIsAnsweredInTheSameRun(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	consent := f.consent(f.alice, c.ID)
	var runID string
	f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, n int) (llmgateway.Result, error) {
		if n == 1 {
			steerDuringAnswer(t, f, runID, "背景换成黄昏的海边")
		}
		return scriptedResult(r), nil
	}
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "整理"})
	if err != nil {
		t.Fatal(err)
	}
	runID = run.ID
	if err = f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	got, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
	if err != nil || got.State != RunSucceeded {
		t.Fatalf("run=%+v err=%v", got, err)
	}
	if f.vendor.calls() != 2 {
		t.Fatalf("calls=%d, want the answer and one continuation", f.vendor.calls())
	}
	second := f.vendor.sent[1]
	if n := occurrences(second, llmgateway.RoleUser, "背景换成黄昏的海边"); n != 1 {
		t.Fatalf("steer appears %d times in the continuation", n)
	}
	system := 0
	for _, m := range second.Messages {
		if m.Role == llmgateway.RoleSystem {
			system++
		}
	}
	if system != countSystem(f.vendor.sent[0]) {
		t.Fatalf("continuation has %d system messages, first request %d", system, countSystem(f.vendor.sent[0]))
	}
	if occurrences(second, llmgateway.RoleAssistant, "整理完成。") != 1 {
		t.Fatal("continuation lost the answer it follows")
	}
}

func countSystem(chat llmgateway.ChatRequest) int {
	n := 0
	for _, m := range chat.Messages {
		if m.Role == llmgateway.RoleSystem {
			n++
		}
	}
	return n
}

// A continuation that stops on a tool resumes from its own checkpoint, and the
// resumed run neither loses nor repeats the steer.
func TestSteerContinuationResumesFromItsCheckpoint(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	consent := f.consent(f.alice, c.ID)
	var runID string
	f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, n int) (llmgateway.Result, error) {
		switch n {
		case 1:
			steerDuringAnswer(t, f, runID, "背景换成黄昏的海边")
			return scriptedResult(r), nil
		case 2:
			return scriptedResult(r, llmgateway.ToolCall{ID: "after-steer", Name: readResultTool, Arguments: json.RawMessage(`{"item_id":"missing","offset":0,"limit":100}`)}), nil
		}
		return scriptedResult(r), nil
	}
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "整理"})
	if err != nil {
		t.Fatal(err)
	}
	runID = run.ID
	held, err := f.service.claimRun(t.Context(), f.alice, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	execution, err := f.service.newRunExecution(t.Context(), f.alice, held)
	if err != nil {
		t.Fatal(err)
	}
	execution.config.Handlers = append([]adk.ChatModelAgentMiddleware{&checkpointPause{BaseChatModelAgentMiddleware: &adk.BaseChatModelAgentMiddleware{}}}, execution.config.Handlers...)
	if _, err = execution.run(t.Context()); !errors.Is(err, errRunInterrupted) {
		t.Fatalf("continuation did not stop on its tool: %v", err)
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
		t.Fatalf("calls=%d", f.vendor.calls())
	}
	if n := occurrences(f.vendor.sent[2], llmgateway.RoleUser, "背景换成黄昏的海边"); n != 1 {
		t.Fatalf("steer appears %d times after resume", n)
	}
}

func (f *fixture) steer(runID string, segments ...InstructionSegment) error {
	f.t.Helper()
	_, err := f.service.SteerRun(f.t.Context(), f.alice, command(f.t, map[string]any{"run_id": runID, "instruction_segments": segments}))
	return err
}

// queuedRun is a run the worker has not picked up yet: no model step, still
// open to a steer.
func queuedRun(t *testing.T, f *fixture, consentID ...string) (Run, Conversation) {
	t.Helper()
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	id := ""
	if len(consentID) > 0 {
		id = consentID[0]
	} else {
		id = f.consent(f.alice, c.ID).ID
	}
	run, err := f.startRun(f.alice, c.ID, id, InstructionSegment{Type: "text", Text: "整理"})
	if err != nil {
		t.Fatal(err)
	}
	return run, c
}

// answeredRun is a run whose final answer is recorded but which is still
// running: the window after the answer and before the run is closed.
func answeredRun(t *testing.T, f *fixture) Run {
	t.Helper()
	run, _ := queuedRun(t, f)
	if err := f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`UPDATE creative_agent_runs SET state='running', finished_at=NULL, error_code=NULL WHERE id=$1`, run.ID); err != nil {
		t.Fatal(err)
	}
	return run
}

func TestSteerIsOneBatchOneMessageAndOneBlock(t *testing.T) {
	f := setup(t)
	run, _ := queuedRun(t, f)
	revision := f.revision(f.alice, "插队引用的素材")
	cmd := command(t, map[string]any{"run_id": run.ID, "instruction_segments": []InstructionSegment{
		{Type: "text", Text: "用这份素材"}, {Type: "content_ref", ContentRevisionID: revision},
	}})
	first, err := f.service.SteerRun(t.Context(), f.alice, cmd)
	if err != nil {
		t.Fatal(err)
	}
	again, err := f.service.SteerRun(t.Context(), f.alice, cmd)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	var one, two Run
	if json.Unmarshal(first.Outcome.Response, &one) != nil || json.Unmarshal(again.Outcome.Response, &two) != nil || one.LastEventSeq != two.LastEventSeq || one.Revision != two.Revision {
		t.Fatalf("replay answered differently: %+v / %+v", one, two)
	}
	if n := f.count("creative_run_inputs", "run_id=$1 AND batch_ordinal=1", run.ID); n != 2 {
		t.Fatalf("batch rows=%d", n)
	}
	if n := f.count("creative_run_inputs", "run_id=$1 AND batch_ordinal>1", run.ID); n != 0 {
		t.Fatal("replay wrote a second batch")
	}
	var messageID string
	if err := f.db.QueryRow(`SELECT id FROM creative_agent_messages WHERE run_id=$1 AND role='user' AND id<>(SELECT trigger_message_id FROM creative_agent_runs WHERE id=$1)`, run.ID).Scan(&messageID); err != nil {
		t.Fatalf("steer message: %v", err)
	}
	if n := f.count("creative_message_content_refs", "message_id=$1 AND content_revision_id=$2", messageID, revision); n != 1 {
		t.Fatal("reference is not held by the steer message")
	}
	var state, payload string
	if err := f.db.QueryRow(`SELECT state,payload::text FROM creative_agent_blocks WHERE run_id=$1 AND type='input'`, run.ID).Scan(&state, &payload); err != nil {
		t.Fatalf("input block: %v", err)
	}
	if state != "pending" || !strings.Contains(payload, messageID) {
		t.Fatalf("block state=%s payload=%s", state, payload)
	}
	// An older worker reads only body; a reference row must still give it text.
	rows, err := f.db.Query(`SELECT batch_ordinal,body FROM creative_run_inputs WHERE run_id=$1 AND batch_ordinal>0`, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var batch int
		var body string
		if err := rows.Scan(&batch, &body); err != nil {
			t.Fatalf("old reader: %v", err)
		}
	}
	// The receipt key is the operation, whatever the endpoint.
	reuse := cmd
	reuse.Payload = json.RawMessage(`{"run_id":"` + run.ID + `","expected_revision":1,"waiting_token":"x","text":"补充"}`)
	if _, err := f.service.SupplementRun(t.Context(), f.alice, reuse); !errors.Is(err, creativeops.ErrConflict) {
		t.Fatalf("operation reused across endpoints: %v", err)
	}
}

func TestSteerIsRefusedWhenTheRunCannotTakeIt(t *testing.T) {
	text := InstructionSegment{Type: "text", Text: "换个方向"}
	t.Run("waiting for input", func(t *testing.T) {
		f := setup(t)
		run, held, paused := pauseCheckpointRun(t, f, false)
		if err := f.service.completeAttempt(t.Context(), f.alice, held, paused.outcome, errRunInterrupted); err != nil {
			t.Fatal(err)
		}
		if err := f.steer(run.ID, text); !errors.Is(err, ErrRunState) {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("cancelled", func(t *testing.T) {
		f := setup(t)
		run, _ := queuedRun(t, f)
		if _, err := f.service.CancelRun(t.Context(), f.alice, command(t, map[string]any{"run_id": run.ID})); err != nil {
			t.Fatal(err)
		}
		if err := f.steer(run.ID, text); !errors.Is(err, ErrRunState) {
			t.Fatalf("err=%v", err)
		}
	})
	for _, tc := range []struct {
		name   string
		update string
		want   error
	}{
		{"final answer recorded", ``, ErrRunState},
		{"last model step failed", `UPDATE creative_agent_steps SET state='failed', output=NULL WHERE run_id=$1 AND kind='model'`, ErrRunState},
		{"last model step still prepared", `UPDATE creative_agent_steps SET state='prepared' WHERE run_id=$1 AND kind='model'`, nil},
		{"last model step called tools", `UPDATE creative_agent_steps SET output=jsonb_set(output,'{tool_calls}','[{"id":"x","name":"read_result"}]') WHERE run_id=$1 AND kind='model'`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setup(t)
			run := answeredRun(t, f)
			if tc.update != "" {
				if _, err := f.db.Exec(tc.update, run.ID); err != nil {
					t.Fatal(err)
				}
			}
			before := f.count("creative_run_inputs", "run_id=$1", run.ID)
			err := f.steer(run.ID, text)
			if tc.want == nil && err != nil || tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want %v", err, tc.want)
			}
			if tc.want != nil && f.count("creative_run_inputs", "run_id=$1", run.ID) != before {
				t.Fatal("a refused steer wrote input")
			}
		})
	}
	t.Run("twelve batches already", func(t *testing.T) {
		f := setup(t)
		run, _ := queuedRun(t, f)
		for i := 0; i < maxSupplements; i++ {
			if err := f.steer(run.ID, text); err != nil {
				t.Fatalf("steer %d: %v", i+1, err)
			}
		}
		if err := f.steer(run.ID, text); !errors.Is(err, ErrLimit) {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("skill or command segments", func(t *testing.T) {
		f := setup(t)
		run, _ := queuedRun(t, f)
		if err := f.steer(run.ID, InstructionSegment{Type: "command", Command: compactCommand}); !errors.Is(err, creativeops.ErrValidation) {
			t.Fatalf("err=%v", err)
		}
	})
}

// What the photographer attaches during a run is covered by the account's
// agreement (owner 2026-11-11), not by the run's list; the rest still holds.
func TestSteerReferencesAreCheckedForAccountAndRevocation(t *testing.T) {
	t.Run("own usable revision outside the run's list", func(t *testing.T) {
		f := setup(t)
		run, _ := queuedRun(t, f)
		if err := f.steer(run.ID, InstructionSegment{Type: "content_ref", ContentRevisionID: f.revision(f.alice, "不在清单里的素材")}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("another account's revision", func(t *testing.T) {
		f := setup(t)
		run, _ := queuedRun(t, f)
		if err := f.steer(run.ID, InstructionSegment{Type: "content_ref", ContentRevisionID: f.revision(f.bob, "别人的素材")}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("revoked authorisation", func(t *testing.T) {
		f := setup(t)
		run, _ := queuedRun(t, f)
		if _, err := f.db.Exec(`UPDATE creative_egress_consents SET revoked_at=now() WHERE id=$1`, run.EgressConsentID); err != nil {
			t.Fatal(err)
		}
		if err := f.steer(run.ID, InstructionSegment{Type: "text", Text: "继续"}); !errors.Is(err, ErrConsentRevoked) {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("image under an authorisation without image previews", func(t *testing.T) {
		f := setup(t)
		f.service.SetImageReader(testImageReader{})
		run, _ := queuedRun(t, f)
		image := f.mediaRevision(f.alice, "image", "image/png")
		if err := f.steer(run.ID, InstructionSegment{Type: "content_ref", ContentRevisionID: image}); !errors.Is(err, ErrEgressRequired) {
			t.Fatalf("err=%v", err)
		}
	})
}

func TestSupplementCarriesSegments(t *testing.T) {
	f := setup(t)
	run, held, paused := pauseCheckpointRun(t, f, false)
	if err := f.service.completeAttempt(t.Context(), f.alice, held, paused.outcome, errRunInterrupted); err != nil {
		t.Fatal(err)
	}
	waiting, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	revision := f.revision(f.alice, "补充引用的正文")
	both := map[string]any{"run_id": run.ID, "expected_revision": waiting.Revision, "waiting_token": waiting.WaitingToken, "text": "补充",
		"instruction_segments": []InstructionSegment{{Type: "text", Text: "补充"}}}
	if _, err = f.service.SupplementRun(t.Context(), f.alice, command(t, both)); !errors.Is(err, creativeops.ErrValidation) {
		t.Fatalf("text and segments together: %v", err)
	}
	delete(both, "text")
	both["instruction_segments"] = []InstructionSegment{{Type: "text", Text: "参考这份"}, {Type: "content_ref", ContentRevisionID: revision}}
	if _, err = f.service.SupplementRun(t.Context(), f.alice, command(t, both)); err != nil {
		t.Fatal(err)
	}
	if err = f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.vendor.prompt(), "补充引用的正文") {
		t.Fatal("the supplement's reference never reached the model")
	}
	if n := f.count("creative_agent_blocks", "run_id=$1 AND type='input' AND state='delivered'", run.ID); n != 1 {
		t.Fatalf("delivered input blocks=%d", n)
	}
}
