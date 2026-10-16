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
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// forEachClaudeVendor runs one acceptance against every relay that serves the
// Claude line, each alone in its own catalog, so the evidence names who passed.
// A vendor without its credential is skipped.
func forEachClaudeVendor(t *testing.T, run func(*testing.T, *llmgateway.Service, store.AccountScope)) {
	for _, credential := range []string{"FOUR_ROUTER_API_KEY", "AICODEWITH_API_KEY"} {
		t.Run(credential, func(t *testing.T) {
			gateway, scope, _ := liveFixtureFor(t, credential, aicodewithBudgetMicros)
			run(t, gateway, scope)
		})
	}
}

// TestLiveClaudeModelsAnswer is the admission evidence for each
// Claude entry: the relay answers under the model id the catalog accepts, with
// complete usage, and our system prompt decides who the assistant says it is.
func TestLiveClaudeModelsAnswer(t *testing.T) {
	forEachClaudeVendor(t, func(t *testing.T, gateway *llmgateway.Service, scope store.AccountScope) {
		for _, key := range []string{"claude-opus-5-5", "claude-sonnet-5-5"} {
			t.Run(key, func(t *testing.T) {
				chat := liveChat(4000, "你是谁？一句话。")
				chat.ModelKey = key
				chat.Messages[0].Blocks[0].Text = "你是「拾光」工作室的创作助手，只自称「创作助手」。回答尽量简短。"
				_, result := liveTurn(t, gateway, scope, "live-"+key, chat, false)
				if strings.TrimSpace(result.Text) == "" || result.FinishReason != llmgateway.FinishStop {
					t.Fatalf("no complete answer: %+v", result)
				}
				if !result.Usage.Complete() {
					t.Fatalf("usage incomplete: %+v", result.Usage)
				}
				// A relay channel injects its own identity prompt; ours must win.
				if !strings.Contains(result.Text, "创作助手") {
					t.Fatalf("the assistant introduced itself as someone else: %s", result.Text)
				}
				t.Logf("%s answered as %s: in %d (cached %d) out %d; %s", key, result.Model,
					*result.Usage.InputTotalTokens, *result.Usage.InputCachedTokens, *result.Usage.OutputTokens, result.Text)
			})
		}
	})
}

// TestLiveClaudeThinkingToolLoop is the evidence that signed
// thinking survives the relay: the second, streamed turn hands the first
// turn's content back verbatim and the model accepts it. The relay may switch
// channels between calls, so the loop runs twice per model. Adaptive thinking
// skips an easy step on an official channel (17×23 drew none from Sonnet on
// 4Router, 2026-10-16), so the step is a product hard enough to think about,
// and the system prompt drops liveChat's "answer briefly", which alone kept
// Sonnet from thinking at all.
func TestLiveClaudeThinkingToolLoop(t *testing.T) {
	forEachClaudeVendor(t, func(t *testing.T, gateway *llmgateway.Service, scope store.AccountScope) {
		for _, key := range []string{"claude-sonnet-5-5", "claude-opus-5-5"} {
			handedBack := 0
			for round := range 2 {
				chat := liveChat(16000, "先在心里算出 4837×2963 的精确结果并验算一遍，然后调用 read_canvas 看看画布，最后用一句话说出计算结果和画布上有几个节点。")
				chat.ModelKey = key
				chat.Messages[0].Blocks[0].Text = "你是摄影创作助手。"
				chat.Tools = []llmgateway.ToolDefinition{{
					Name: "read_canvas", Description: "读取画布上的节点", SchemaVersion: 1,
					InputSchema: json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{}}`),
				}}
				group := fmt.Sprintf("live-%s-loop-%d", key, round)
				_, first := liveTurn(t, gateway, scope, group, chat, true)
				if len(first.ToolCalls) == 0 {
					t.Fatalf("%s round %d: expected a tool call: %+v", key, round, first)
				}
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
				if !strings.Contains(strings.NewReplacer(",", "", "，", "", " ", "").Replace(second.Text), "14332031") || (!strings.Contains(second.Text, "3") && !strings.Contains(second.Text, "三")) {
					t.Fatalf("%s round %d: the second turn lost the thread: %+v", key, round, second)
				}
				t.Logf("%s round %d: thinking record %d bytes, input %d; answer: %s", key, round, len(first.Reasoning), *second.Usage.InputTotalTokens, second.Text)
			}
			if handedBack == 0 {
				t.Fatalf("%s: no round produced thinking to hand back", key)
			}
		}
	})
}

// TestLiveClaudeReadsTheCache is the evidence behind sending
// automatic prompt caching: the same long prefix is read back on the next call.
func TestLiveClaudeReadsTheCache(t *testing.T) {
	forEachClaudeVendor(t, func(t *testing.T, gateway *llmgateway.Service, scope store.AccountScope) {
		rules := strings.Repeat("客户资料需完整记录，拍摄前确认场地与服装。", 300)
		var cached int64
		for i := range 2 {
			chat := liveChat(2000, "回复“好”。")
			chat.ModelKey = "claude-sonnet-5-5"
			chat.Messages[0].Blocks[0].Text += "\n工作室规范：" + rules
			_, result := liveTurn(t, gateway, scope, fmt.Sprintf("live-claude-cache-%d", i), chat, false)
			if !result.Usage.Complete() {
				t.Fatalf("usage incomplete: %+v", result.Usage)
			}
			cached = *result.Usage.InputCachedTokens
			t.Logf("call %d: input %d, cached %d", i, *result.Usage.InputTotalTokens, cached)
		}
		if cached == 0 {
			t.Fatal("the second call read nothing from the cache")
		}
	})
}

// TestLiveClaudeSeesAnImage is the evidence behind image input.
func TestLiveClaudeSeesAnImage(t *testing.T) {
	forEachClaudeVendor(t, func(t *testing.T, gateway *llmgateway.Service, scope store.AccountScope) {
		raw := liveGradientPNG(400, 400)
		sum := sha256.Sum256(raw)
		chat := liveChat(4000, "这张图左半边和右半边分别是什么颜色？一句话。")
		chat.ModelKey = "claude-sonnet-5-5"
		chat.Messages[1].Blocks = append(chat.Messages[1].Blocks, llmgateway.Block{Kind: llmgateway.BlockImageInput, Image: &llmgateway.ImageInput{
			RevisionID: "ccrv_live", Digest: "sha256-" + hex.EncodeToString(sum[:]), MIME: "image/png", ByteSize: int64(len(raw)),
			Open: func(context.Context) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(raw)), nil },
		}})
		_, result := liveTurn(t, gateway, scope, "live-claude-vision", chat, true)
		if result.Usage.InputTotalTokens == nil || *result.Usage.InputTotalTokens > 4096+400 {
			t.Fatalf("image input tokens above the per-image bound: %+v", result.Usage)
		}
		if !strings.Contains(result.Text, "红") || !strings.Contains(result.Text, "蓝") {
			t.Fatalf("the answer does not describe the image: %s", result.Text)
		}
		t.Logf("input tokens %d; answer: %s", *result.Usage.InputTotalTokens, result.Text)
	})
}
