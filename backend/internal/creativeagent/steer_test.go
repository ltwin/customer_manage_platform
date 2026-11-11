package creativeagent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"

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
