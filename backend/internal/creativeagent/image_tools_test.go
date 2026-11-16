package creativeagent

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/creativeimageedit"
	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// stubCompressor stands in for image editing; its own database tests cover
// the candidate and the adoption.
type stubCompressor struct {
	mu        sync.Mutex
	calls     []creativeimageedit.AgentCompress
	pending   bool
	adopts    []bool
	discarded []string
	conclude  creativeimageedit.AgentCompressResult
}

func (s *stubCompressor) AcceptAgentCompress(_ context.Context, _ store.AccountScope, in creativeimageedit.AgentCompress) (string, error) {
	s.mu.Lock()
	s.calls = append(s.calls, in)
	s.mu.Unlock()
	if in.NodeID == "cwnode_busy" {
		return "", &creativeimageedit.Refusal{Message: "这个节点正在生成，完成后再试"}
	}
	return "ccier_" + in.NodeID, nil
}

func (s *stubCompressor) AgentCompressPendingInTx(context.Context, store.TxAccountScope, string) (bool, time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pending, time.Now().Add(10 * time.Minute), nil
}

func (s *stubCompressor) ConcludeAgentCompress(_ context.Context, _ store.AccountScope, in creativeimageedit.AgentCompress, render string, adopt bool) (creativeimageedit.AgentCompressResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.adopts = append(s.adopts, adopt)
	if s.conclude.State != "" {
		return s.conclude, nil
	}
	if !adopt {
		return creativeimageedit.AgentCompressResult{NodeID: in.NodeID, RenderID: render, State: creativeimageedit.AgentNotApplied, Reason: "助手已停止"}, nil
	}
	return creativeimageedit.AgentCompressResult{NodeID: in.NodeID, RenderID: render, State: creativeimageedit.AgentApplied, Quality: in.Quality, SourceSize: 1000, OutputSize: 600, ReductionPercent: 40}, nil
}

func (s *stubCompressor) DiscardAgentCompressInTx(_ context.Context, _ store.TxAccountScope, in creativeimageedit.AgentCompress, render string) (creativeimageedit.AgentCompressResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.discarded = append(s.discarded, render)
	return creativeimageedit.AgentCompressResult{NodeID: in.NodeID, RenderID: render, State: creativeimageedit.AgentNotApplied, Reason: "压缩已取消"}, nil
}

func (s *stubCompressor) set(pending bool) {
	s.mu.Lock()
	s.pending = pending
	s.mu.Unlock()
}

func offered(r llmgateway.ProviderRequest, name string) bool {
	return slices.ContainsFunc(r.Chat.Tools, func(t llmgateway.ToolDefinition) bool { return t.Name == name })
}

// compress_image exists only where image editing is on. Its result reaches
// the model with the real sizes; a refusal is words, not a failed run.
func TestAssistantCompressesAnImage(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	consent := contextConsent(t, f, c.ID)
	var withoutEditing bool
	f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, _ int) (llmgateway.Result, error) {
		withoutEditing = offered(r, compressImageTool)
		return scriptedResult(r), nil
	}
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "你好"})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	if withoutEditing {
		t.Fatal("compress_image offered while image editing is off")
	}

	compressor := &stubCompressor{}
	f.service.SetImageCompressor(compressor)
	var applied, refused string
	f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, n int) (llmgateway.Result, error) {
		if n == 2 {
			if !offered(r, compressImageTool) {
				t.Error("compress_image not offered")
			}
			return scriptedResult(r,
				call("a", 0, compressImageTool, `{"node_id":"cwnode_photo","quality":75}`),
				call("b", 1, compressImageTool, `{"node_id":"cwnode_busy"}`)), nil
		}
		applied, refused = toolResult(r, 0), toolResult(r, 1)
		return scriptedResult(r), nil
	}
	run, err = f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "把这张图按质量 75 压缩"})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	got, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
	if err != nil || got.State != RunSucceeded {
		t.Fatalf("run=%+v %v", got, err)
	}
	var out struct {
		Success          bool    `json:"success"`
		State            string  `json:"state"`
		ReductionPercent float64 `json:"reduction_percent"`
	}
	if err = json.Unmarshal([]byte(applied), &out); err != nil || !out.Success || out.State != "applied" || out.ReductionPercent != 40 {
		t.Fatalf("result %q", applied)
	}
	if !strings.Contains(refused, "正在生成") || f.count("creative_agent_steps", "tool_key=$1 AND error_code='creative_tool_refused'", compressImageTool) != 1 {
		t.Fatalf("refusal %q", refused)
	}
	if len(compressor.calls) != 2 || compressor.calls[0].Quality != 75 || compressor.calls[0].CanvasID != f.canvasID ||
		compressor.calls[1].Quality != 0 || !strings.HasPrefix(compressor.calls[0].Seed, run.ID+"/ccst_") || compressor.calls[0].Seed == compressor.calls[1].Seed {
		t.Fatalf("calls %+v", compressor.calls)
	}
}

// startParkedCompress runs one compress_image call on a candidate that is
// still encoding, so the call parks on its task.
func startParkedCompress(t *testing.T, f *fixture) (Run, *stubCompressor, *[]string) {
	t.Helper()
	f.queue()
	compressor := &stubCompressor{pending: true}
	f.service.SetImageCompressor(compressor)
	c := f.conversation(f.alice, f.canvasID)
	consent := contextConsent(t, f, c.ID)
	var answers []string
	f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, n int) (llmgateway.Result, error) {
		if n == 1 {
			return scriptedResult(r, call("a", 0, compressImageTool, `{"node_id":"cwnode_photo","quality":70}`)), nil
		}
		answers = append(answers, toolResult(r, 0))
		return scriptedResult(r), nil
	}
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "压缩一下"})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	parked, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
	if err != nil || parked.State != RunWaitingClient || parked.WaitReason != waitToolTask {
		t.Fatalf("not parked on its task: %+v %v", parked, err)
	}
	if f.slotHolder() != "" {
		t.Fatal("a parked compress kept the slot")
	}
	return run, compressor, &answers
}

// An encode that takes longer than any old wait parks the call; the finisher
// adopts the candidate once and hands the real sizes back.
func TestCompressParksUntilTheCandidateIsEncoded(t *testing.T) {
	f := setup(t)
	run, compressor, answers := startParkedCompress(t, f)
	task := f.sceneTasks(run.ID)
	if len(task) != 1 || task[0].Tool != compressImageTool || task[0].Mode != modeForeground || task[0].State != taskRunning {
		t.Fatalf("task=%+v", task)
	}
	f.markDue(task[0].ID)
	if _, err := f.service.FinishDueToolTasks(t.Context(), f.st, 10); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.service.ReadRun(t.Context(), f.alice, run.ID); got.State != RunWaitingClient || len(compressor.adopts) != 0 {
		t.Fatalf("moved while encoding: %s adopts=%v", got.State, compressor.adopts)
	}

	compressor.set(false)
	f.markDue(task[0].ID)
	if _, err := f.service.FinishDueToolTasks(t.Context(), f.st, 10); err != nil {
		t.Fatal(err)
	}
	// A second pass over a finished task adopts nothing again.
	f.markDue(task[0].ID)
	if _, err := f.service.FinishDueToolTasks(t.Context(), f.st, 10); err != nil {
		t.Fatal(err)
	}
	var stored struct {
		State string `json:"state"`
	}
	if got := f.sceneTasks(run.ID)[0]; got.State != taskSucceeded || json.Unmarshal(got.Result, &stored) != nil || stored.State != "applied" {
		t.Fatalf("task=%+v", got)
	}
	if len(compressor.adopts) != 1 || !compressor.adopts[0] {
		t.Fatalf("adopts=%v", compressor.adopts)
	}
	if err := f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.service.ReadRun(t.Context(), f.alice, run.ID); got.State != RunSucceeded || len(*answers) != 1 || !strings.Contains((*answers)[0], `"reduction_percent":40`) {
		t.Fatalf("run=%s answers=%v", got.State, *answers)
	}
}

// A stopped call's candidate is discarded, never adopted for it.
func TestStoppedCompressDiscardsTheCandidate(t *testing.T) {
	f := setup(t)
	run, compressor, answers := startParkedCompress(t, f)
	if _, err := f.service.CancelRun(t.Context(), f.alice, command(t, map[string]any{"run_id": run.ID})); err != nil {
		t.Fatal(err)
	}
	compressor.set(false)
	task := f.sceneTasks(run.ID)[0]
	f.markDue(task.ID)
	if _, err := f.service.FinishDueToolTasks(t.Context(), f.st, 10); err != nil {
		t.Fatal(err)
	}
	if got := f.sceneTasks(run.ID)[0]; got.State != taskCancelled {
		t.Fatalf("task=%+v", got)
	}
	if len(compressor.adopts) != 1 || compressor.adopts[0] {
		t.Fatalf("adopts=%v", compressor.adopts)
	}
	if got, _ := f.service.ReadRun(t.Context(), f.alice, run.ID); got.State != RunCancelled || len(*answers) != 0 {
		t.Fatalf("stopped run moved: %s %v", got.State, *answers)
	}
}

// Each final candidate state reads the same way to the model and the task.
func TestCompressOutcomes(t *testing.T) {
	for state, want := range map[string]string{
		creativeimageedit.AgentApplied:     taskSucceeded,
		creativeimageedit.AgentNotReduced:  taskSucceeded,
		creativeimageedit.AgentUnconfirmed: taskSucceeded,
		creativeimageedit.AgentFailed:      taskFailed,
		creativeimageedit.AgentNotApplied:  taskFailed,
	} {
		c := &stubCompressor{conclude: creativeimageedit.AgentCompressResult{NodeID: "n", State: state}}
		outcome, err := concludeCompress(t.Context(), c, store.AccountScope{}, creativeimageedit.AgentCompress{NodeID: "n"}, "r", true)
		success := state == creativeimageedit.AgentApplied
		if err != nil || !outcome.Done || outcome.State != want || !strings.Contains(outcome.Result, `"state":"`+state+`"`) ||
			strings.Contains(outcome.Result, `"success":true`) != success {
			t.Fatalf("%s: %+v %v", state, outcome, err)
		}
	}
}
