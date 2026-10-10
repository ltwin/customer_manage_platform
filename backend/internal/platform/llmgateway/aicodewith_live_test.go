package llmgateway_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
)

// aicodewithBudgetMicros caps these tests at 1 USD of official OpenAI price;
// the relay bills a fraction of that.
const aicodewithBudgetMicros = 1_000_000

// TestLiveAICodeWithModelsAnswer is the admission evidence for each GPT-6
// entry: the relay answers under the model id the catalog accepts, with usage.
func TestLiveAICodeWithModelsAnswer(t *testing.T) {
	gateway, scope, _ := liveFixtureFor(t, "AICODEWITH_API_KEY", aicodewithBudgetMicros)
	for _, key := range []string{"gpt-6.1-sol", "gpt-6-sol", "gpt-6-astra"} {
		t.Run(key, func(t *testing.T) {
			chat := liveChat(2000, "用一句话说明逆光人像的要点。")
			chat.ModelKey = key
			_, result := liveTurn(t, gateway, scope, "live-"+key, chat, false)
			if strings.TrimSpace(result.Text) == "" || result.FinishReason != llmgateway.FinishStop {
				t.Fatalf("no complete answer: %+v", result)
			}
			if result.Usage.InputTotalTokens == nil || result.Usage.OutputTokens == nil {
				t.Fatalf("usage missing: %+v", result.Usage)
			}
			// Without our own instructions the relay injects a ~4,300-token
			// prompt; a one-line question must stay far below that.
			if *result.Usage.InputTotalTokens > 1000 {
				t.Fatalf("input %d tokens: the relay's own prompt was added", *result.Usage.InputTotalTokens)
			}
			t.Logf("%s answered as %s: in %d out %d; %s", key, result.Model,
				*result.Usage.InputTotalTokens, *result.Usage.OutputTokens, result.Text)
		})
	}
}

// TestLiveAICodeWithReasoningToolLoop is the evidence that sealed reasoning
// survives the relay: the second, streamed turn hands the first turn's
// encrypted items back and the model accepts them. The relay may switch
// upstream channels between calls, so the loop runs three times. High effort
// makes the first turn reason for certain; at medium a simple step often
// carries no reasoning at all (measured 2026-10-09).
func TestLiveAICodeWithReasoningToolLoop(t *testing.T) {
	defaults, err := llmgateway.DefaultCatalog(nil)
	if err != nil {
		t.Fatal(err)
	}
	model, err := defaults.Deployment("gpt-6.1-sol", "aicodewith/api/gpt-6.1-sol")
	if err != nil {
		t.Skip("gpt-6.1-sol is not configured: ", err)
	}
	model.ReasoningEffort = "high"
	catalog, err := llmgateway.NewCatalog("live-high", "USD", []llmgateway.ModelConfig{model})
	if err != nil {
		t.Fatal(err)
	}
	gateway, scope, _ := liveFixtureWith(t, "AICODEWITH_API_KEY", aicodewithBudgetMicros, catalog)
	handedBack := 0
	for round := range 3 {
		chat := liveChat(6000, "先在心里算出 17×23 的结果，然后调用 read_canvas 看看画布，最后用一句话说出计算结果和画布上有几个节点。")
		chat.ModelKey = "gpt-6.1-sol"
		chat.Tools = []llmgateway.ToolDefinition{{
			Name: "read_canvas", Description: "读取画布上的节点", SchemaVersion: 1,
			InputSchema: json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{}}`),
		}}
		group := fmt.Sprintf("live-gpt-loop-%d", round)
		_, first := liveTurn(t, gateway, scope, group, chat, true)
		if len(first.ToolCalls) == 0 {
			t.Fatalf("round %d: expected a tool call: %+v", round, first)
		}
		// Even at high effort the model sometimes calls the tool without
		// reasoning first; that round proves nothing about the hand-back.
		if first.Reasoning != "" {
			handedBack++
		}
		answer := llmgateway.Message{Role: llmgateway.RoleAssistant, Reasoning: first.Reasoning, ToolCalls: first.ToolCalls}
		if first.Text != "" {
			answer.Blocks = []llmgateway.Block{{Kind: llmgateway.BlockText, Text: first.Text}}
		}
		chat.Messages = append(chat.Messages, answer)
		for _, call := range first.ToolCalls {
			chat.Messages = append(chat.Messages, llmgateway.Message{Role: llmgateway.RoleTool, ToolCallID: call.ID,
				Blocks: []llmgateway.Block{{Kind: llmgateway.BlockText, Text: "画布上有 3 个节点"}}})
		}
		_, second := liveTurn(t, gateway, scope, group, chat, true)
		if !strings.Contains(second.Text, "391") || (!strings.Contains(second.Text, "3") && !strings.Contains(second.Text, "三")) {
			t.Fatalf("round %d: the second turn lost the thread: %+v", round, second)
		}
		t.Logf("round %d: reasoning %d bytes, input %d; answer: %s", round, len(first.Reasoning), *second.Usage.InputTotalTokens, second.Text)
	}
	if handedBack == 0 {
		t.Fatal("no round produced reasoning to hand back")
	}
}

// TestLiveAICodeWithSeesAnImage is the evidence behind image input on GPT-6.
func TestLiveAICodeWithSeesAnImage(t *testing.T) {
	gateway, scope, _ := liveFixtureFor(t, "AICODEWITH_API_KEY", aicodewithBudgetMicros)
	raw := liveGradientPNG(400, 400)
	sum := sha256.Sum256(raw)
	chat := liveChat(2000, "这张图左半边和右半边分别是什么颜色？一句话。")
	chat.ModelKey = "gpt-6.1-sol"
	chat.Messages[1].Blocks = append(chat.Messages[1].Blocks, llmgateway.Block{Kind: llmgateway.BlockImageInput, Image: &llmgateway.ImageInput{
		RevisionID: "ccrv_live", Digest: "sha256-" + hex.EncodeToString(sum[:]), MIME: "image/png", ByteSize: int64(len(raw)),
		Open: func(context.Context) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(raw)), nil },
	}})
	_, result := liveTurn(t, gateway, scope, "live-gpt-vision", chat, true)
	if result.Usage.InputTotalTokens == nil || *result.Usage.InputTotalTokens > 4096+400 {
		t.Fatalf("image input tokens above the per-image bound: %+v", result.Usage)
	}
	if !strings.Contains(result.Text, "红") || !strings.Contains(result.Text, "蓝") {
		t.Fatalf("the answer does not describe the image: %s", result.Text)
	}
	t.Logf("input tokens %d; answer: %s", *result.Usage.InputTotalTokens, result.Text)
}
