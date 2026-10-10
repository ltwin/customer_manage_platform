package llmgateway_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
)

// Explicit opt-in keeps the normal gate offline even with a configured key.
// The isolated account's gateway budget caps the complete test at 1 USD.
func TestLiveKrillModelsAndToolHistory(t *testing.T) {
	if os.Getenv("KRILL_AI_LIVE") != "1" {
		t.Skip("set KRILL_AI_LIVE=1 to run the paid Krill acceptance")
	}
	gateway, scope, _ := liveFixtureFor(t, "KRILL_AI_API_KEY", 1_000_000)
	for _, key := range []string{"gpt-6.1-sol", "gpt-6-sol", "gpt-6-astra"} {
		t.Run(key, func(t *testing.T) {
			chat := liveChat(2000, "用一句话说明逆光人像的要点。")
			chat.ModelKey = key
			_, result := liveTurn(t, gateway, scope, "krill-"+key, chat, false)
			if result.Model != key || strings.TrimSpace(result.Text) == "" || result.Usage.InputTotalTokens == nil || result.Usage.OutputTokens == nil {
				t.Fatalf("incomplete answer: %+v", result)
			}
		})
	}
	raw := liveGradientPNG(400, 400)
	sum := sha256.Sum256(raw)
	chat := liveChat(4000, "先调用 read_canvas，然后用一句话回答画布节点数量、图片左半边和右半边的颜色。")
	chat.ModelKey = "gpt-6.1-sol"
	chat.Messages[1].Blocks = append(chat.Messages[1].Blocks, llmgateway.Block{Kind: llmgateway.BlockImageInput, Image: &llmgateway.ImageInput{
		RevisionID: "ccrv_live", Digest: "sha256-" + hex.EncodeToString(sum[:]), MIME: "image/png", ByteSize: int64(len(raw)),
		Open: func(context.Context) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(raw)), nil },
	}})
	chat.Tools = []llmgateway.ToolDefinition{{Name: "read_canvas", Description: "读取画布节点", SchemaVersion: 1,
		InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)}}
	chat.ToolChoice = "required"
	_, first := liveTurn(t, gateway, scope, "krill-tools", chat, true)
	if len(first.ToolCalls) == 0 {
		t.Fatal("missing tool call")
	}
	chat.Messages = append(chat.Messages, llmgateway.Message{Role: llmgateway.RoleAssistant, ToolCalls: first.ToolCalls, Reasoning: first.Reasoning})
	for _, call := range first.ToolCalls {
		if call.Name != "read_canvas" {
			t.Fatalf("unexpected tool %s", call.Name)
		}
		chat.Messages = append(chat.Messages, llmgateway.Message{Role: llmgateway.RoleTool, ToolCallID: call.ID,
			Blocks: []llmgateway.Block{{Kind: llmgateway.BlockText, Text: "画布上有 3 个节点"}}})
	}
	chat.ToolChoice = "none"
	_, second := liveTurn(t, gateway, scope, "krill-tools", chat, true)
	if !strings.Contains(second.Text, "红") || !strings.Contains(second.Text, "蓝") || (!strings.Contains(second.Text, "3") && !strings.Contains(second.Text, "三")) {
		t.Fatalf("image or tool history lost: %s", second.Text)
	}
}
