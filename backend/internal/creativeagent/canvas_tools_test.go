package creativeagent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativegeneration"
	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// stubGenerator stands in for node generation; the canvas writes before it are
// the real commands.
type stubGenerator struct {
	calls []creativegeneration.AgentGeneration
}

func (s *stubGenerator) Models() []creativegeneration.Model {
	return []creativegeneration.Model{{Key: "gpt-image-2", Label: "GPT Image 2", Kind: "image", Parameters: creativegeneration.ImageParameters()}}
}
func (s *stubGenerator) DraftParameters(model, nodeType string, given map[string]any) (json.RawMessage, error) {
	if model != "gpt-image-2" || nodeType != "core.image" {
		return nil, &creativegeneration.Refusal{Message: "模型 " + model + " 不能用于这个节点"}
	}
	return json.Marshal(given)
}
func (s *stubGenerator) GenerateNode(_ context.Context, _ store.AccountScope, in creativegeneration.AgentGeneration) (creativegeneration.AgentGenerationResult, error) {
	s.calls = append(s.calls, in)
	return creativegeneration.AgentGenerationResult{NodeID: in.NodeID, ExecutionID: "cwexec_1", State: "succeeded", ContentRevisionIDs: []string{"ccrv_out"}}, nil
}

// toolResult returns what the k-th call of the latest tool round returned.
// Provider call IDs are rewritten to journal identities, so calls are found
// by position.
func toolResult(r llmgateway.ProviderRequest, k int) string {
	var id string
	for _, m := range r.Chat.Messages {
		if len(m.ToolCalls) > k {
			id = m.ToolCalls[k].ID
		}
	}
	for _, m := range r.Chat.Messages {
		if id != "" && m.ToolCallID == id {
			var out strings.Builder
			for _, b := range m.Blocks {
				out.WriteString(b.Text)
			}
			return out.String()
		}
	}
	return ""
}

func call(id string, index int, name, args string) llmgateway.ToolCall {
	return llmgateway.ToolCall{ID: id, Index: index, Name: name, Arguments: json.RawMessage(args)}
}

// The assistant builds the graph, writes the generation settings and only then
// generates — three tools, each doing one thing, the same commands a
// photographer's hands send. A refusal is relayed as words, not a failed run.
func TestAssistantEditsTheCanvasThenGenerates(t *testing.T) {
	f := setup(t)
	f.queue()
	generator := &stubGenerator{}
	f.service.SetGenerator(generator)
	c := f.conversation(f.alice, f.canvasID)
	consent := contextConsent(t, f, c.ID)
	var created map[string]string
	var refused, canvas string
	f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, n int) (llmgateway.Result, error) {
		switch n {
		case 1:
			return scriptedResult(r, call("edit", 0, editCanvasTool, `{"actions":[
				{"type":"add_node","key":"ref","node_type":"text","title":"光线","text":"金色逆光"},
				{"type":"add_node","key":"img","node_type":"image","title":"日出人像"},
				{"type":"connect","source_node_id":"ref","target_node_id":"img"}]}`)), nil
		case 2:
			var out struct {
				Created map[string]string `json:"created"`
			}
			if err := json.Unmarshal([]byte(toolResult(r, 0)), &out); err != nil || len(out.Created) != 2 {
				t.Fatalf("edit result %q", toolResult(r, 0))
			}
			created = out.Created
			return scriptedResult(r,
				call("prompt", 0, setNodePromptTool, fmt.Sprintf(`{"node_id":%q,"prompt":"海边人像","model":"gpt-image-2","parameters":{"aspect_ratio":"2:3"}}`, created["img"])),
				call("wrong", 1, setNodePromptTool, fmt.Sprintf(`{"node_id":%q,"prompt":"x","model":"gpt-image-2","parameters":{}}`, created["ref"]))), nil
		case 3:
			refused = toolResult(r, 1)
			var out struct {
				Revision int64 `json:"draft_revision"`
			}
			if err := json.Unmarshal([]byte(toolResult(r, 0)), &out); err != nil || out.Revision != 1 {
				t.Fatalf("prompt result %q", toolResult(r, 0))
			}
			return scriptedResult(r, call("generate", 0, generateNodeTool, fmt.Sprintf(`{"node_id":%q,"draft_revision":1}`, created["img"])),
				call("read", 1, readCanvasTool, `{"offset":0,"limit":50}`)), nil
		}
		canvas = toolResult(r, 1)
		return scriptedResult(r), nil
	}
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "出一张图"})
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
	if !strings.Contains(refused, "不能用于这个节点") || f.count("creative_agent_steps", "tool_key=$1 AND error_code='creative_tool_refused'", setNodePromptTool) != 1 {
		t.Fatalf("refusal %q", refused)
	}
	if f.count("creative_nodes", "canvas_id=$1", f.canvasID) != 2 || f.count("creative_edges", "source_node_id=$1 AND target_node_id=$2", created["ref"], created["img"]) != 1 ||
		f.count("creative_node_prompt_drafts", "node_id=$1 AND model_key='gpt-image-2' AND text='海边人像' AND parameters='{\"aspect_ratio\":\"2:3\"}'", created["img"]) != 1 {
		t.Fatal("canvas was not edited as asked")
	}
	if len(generator.calls) != 1 || generator.calls[0].NodeID != created["img"] || generator.calls[0].DraftRevision != 1 || generator.calls[0].CanvasID != f.canvasID || !strings.HasPrefix(generator.calls[0].Seed, run.ID+"/ccst_") {
		t.Fatalf("generated %+v", generator.calls)
	}
	if !strings.Contains(canvas, `"model":"gpt-image-2"`) || !strings.Contains(canvas, `"text":"金色逆光"`) || !strings.Contains(canvas, `"draft_revision":1`) {
		t.Fatalf("read_canvas %s", canvas)
	}

	// A call recovered after its commit finds its receipt instead of adding again.
	h := &runtimeHandler{service: f.service, scope: f.alice}
	args := `{"actions":[{"type":"add_node","key":"n","node_type":"text","text":"一"}]}`
	for range 2 {
		if _, err = h.editCanvas(t.Context(), f.canvasID, "replay-seed", args); err != nil {
			t.Fatal(err)
		}
	}
	if f.count("creative_nodes", "canvas_id=$1", f.canvasID) != 3 {
		t.Fatal("a replayed edit applied twice")
	}
}
