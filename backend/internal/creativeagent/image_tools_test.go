package creativeagent

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativeimageedit"
	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// stubCompressor stands in for image editing; its own database tests cover
// the candidate and the adoption.
type stubCompressor struct {
	mu    sync.Mutex
	calls []creativeimageedit.AgentCompress
}

func (s *stubCompressor) CompressForAgent(_ context.Context, _ store.AccountScope, in creativeimageedit.AgentCompress) (creativeimageedit.AgentCompressResult, error) {
	s.mu.Lock()
	s.calls = append(s.calls, in)
	s.mu.Unlock()
	if in.NodeID == "cwnode_busy" {
		return creativeimageedit.AgentCompressResult{}, &creativeimageedit.Refusal{Message: "这个节点正在生成，完成后再试"}
	}
	return creativeimageedit.AgentCompressResult{NodeID: in.NodeID, State: creativeimageedit.AgentApplied, Quality: in.Quality, SourceSize: 1000, OutputSize: 600, ReductionPercent: 40}, nil
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
