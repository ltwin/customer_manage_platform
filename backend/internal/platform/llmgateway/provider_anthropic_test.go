package llmgateway_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
)

// anthropicThinkingBody is an official-shape answer: hidden thinking with a
// signature, words, then a call; cache reads and writes reported apart.
const anthropicThinkingBody = `{"id":"msg_1","model":"vendor-model","stop_reason":"tool_use","content":[` +
	`{"type":"thinking","thinking":"","signature":"sig-1"},` +
	`{"type":"text","text":"好的"},` +
	`{"type":"tool_use","id":"call_1","name":"search_assets","input":{"query":"逆光"}}` +
	`],"usage":{"input_tokens":20,"cache_read_input_tokens":100,"cache_creation_input_tokens":30,"output_tokens":40,"output_tokens_details":{"thinking_tokens":25}}}`

const anthropicThinkingStream = `event: message_start
data: {"type":"message_start","message":{"id":"msg_2","model":"vendor-model","usage":{"input_tokens":20,"cache_read_input_tokens":100,"cache_creation_input_tokens":0,"output_tokens":1}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"先看"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig-2"}}

event: content_block_start
data: {"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"好的"}}

event: content_block_start
data: {"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"call_1","name":"search_assets","input":{}}}

event: content_block_delta
data: {"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"query\":\"逆光\"}"}}

event: ping
data: {"type":"ping"}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"input_tokens":20,"output_tokens":40}}

event: message_stop
data: {"type":"message_stop"}
`

func anthropicModel(f *protocolFixture, t *testing.T) llmgateway.ModelConfig {
	model := f.model(t, llmgateway.ProviderAnthropicMessage)
	model.ReasoningEffort = "medium"
	return model
}

func invokeAnthropic(t *testing.T, model llmgateway.ModelConfig, chat llmgateway.ChatRequest, stream bool) llmgateway.Result {
	t.Helper()
	result, err := llmgateway.NewAnthropicMessages(llmgateway.NewHTTPClient(5*time.Second), "").Invoke(context.Background(), llmgateway.ProviderRequest{
		Model: model, Snapshot: snapshotFor(model), Chat: chat, Credential: "secret-value", Stream: stream,
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

type sentAnthropic struct {
	Thinking     map[string]any `json:"thinking"`
	OutputConfig map[string]any `json:"output_config"`
	CacheControl map[string]any `json:"cache_control"`
	System       string         `json:"system"`
	Messages     []struct {
		Role    string           `json:"role"`
		Content []map[string]any `json:"content"`
	} `json:"messages"`
}

func lastAnthropicBody(t *testing.T, f *protocolFixture) (sentAnthropic, string) {
	t.Helper()
	raw := f.body.Load().(string)
	var sent sentAnthropic
	if err := json.Unmarshal([]byte(raw), &sent); err != nil {
		t.Fatal(err)
	}
	return sent, raw
}

func TestAnthropicAnswerRecordsThinkingAndCacheUsage(t *testing.T) {
	fixture := newProtocolFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" || r.Header.Get("x-api-key") != "secret-value" {
			t.Errorf("request %s %q", r.URL.Path, r.Header.Get("x-api-key"))
		}
		_, _ = io.WriteString(w, anthropicThinkingBody)
	})
	result := invokeAnthropic(t, anthropicModel(fixture, t), textRequest("chat"), false)
	if result.Text != "好的" || len(result.ToolCalls) != 1 || result.FinishReason != llmgateway.FinishToolCalls {
		t.Fatalf("result %+v", result)
	}
	if !strings.Contains(result.Reasoning, `"signature":"sig-1"`) {
		t.Fatalf("thinking not recorded: %s", result.Reasoning)
	}
	u := result.Usage
	// input_tokens excludes cache reads and writes: 20 + 100 + 30.
	if !u.Complete() || *u.InputTotalTokens != 150 || *u.InputCachedTokens != 100 || *u.OutputTokens != 40 || *u.ReasoningTokens != 25 {
		t.Fatalf("usage %+v", u)
	}
	sent, _ := lastAnthropicBody(t, fixture)
	if sent.Thinking["type"] != "adaptive" || sent.OutputConfig["effort"] != "medium" || sent.CacheControl["type"] != "ephemeral" {
		t.Fatalf("request controls: thinking %v output %v cache %v", sent.Thinking, sent.OutputConfig, sent.CacheControl)
	}
}

func TestAnthropicThinkingReturnsOnlyWithinItsToolLoop(t *testing.T) {
	fixture := newProtocolFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, anthropicThinkingBody)
	})
	model := anthropicModel(fixture, t)
	recorded := invokeAnthropic(t, model, textRequest("chat"), false).Reasoning
	call := llmgateway.ToolCall{ID: "call_1", Name: "search_assets", Arguments: json.RawMessage(`{"query":"逆光"}`)}
	words := []llmgateway.Block{{Kind: llmgateway.BlockText, Text: "好的"}}
	text := func(s string) []llmgateway.Block { return []llmgateway.Block{{Kind: llmgateway.BlockText, Text: s}} }

	chat := textRequest("chat")
	chat.Messages = []llmgateway.Message{
		{Role: llmgateway.RoleSystem, Blocks: text("你是创作助手")},
		{Role: llmgateway.RoleUser, Blocks: text("找参考")},
		// An earlier loop: its thinking stays behind.
		{Role: llmgateway.RoleAssistant, Blocks: words, ToolCalls: []llmgateway.ToolCall{call}, Reasoning: recorded},
		{Role: llmgateway.RoleTool, ToolCallID: "call_1", Blocks: text("无结果")},
		{Role: llmgateway.RoleUser, Blocks: text("再试试")},
		// The current loop: replayed verbatim.
		{Role: llmgateway.RoleAssistant, Blocks: words, ToolCalls: []llmgateway.ToolCall{call}, Reasoning: recorded},
		{Role: llmgateway.RoleTool, ToolCallID: "call_1", Blocks: text("无结果")},
		// Parallel calls whose arguments must keep their key order.
		{Role: llmgateway.RoleAssistant, ToolCalls: []llmgateway.ToolCall{
			{ID: "call_2", Name: "search_assets", Arguments: json.RawMessage(`{"query":"侧光","limit":3}`)},
			{ID: "call_3", Name: "search_assets", Arguments: json.RawMessage(`{"query":"顶光"}`)},
		}},
		{Role: llmgateway.RoleTool, ToolCallID: "call_2", Blocks: text("一张")},
		{Role: llmgateway.RoleTool, ToolCallID: "call_3", Blocks: text("两张")},
		// The words were edited after the fact: the signature cannot follow.
		{Role: llmgateway.RoleAssistant, Blocks: text("改过的话"), ToolCalls: []llmgateway.ToolCall{call}, Reasoning: recorded},
		{Role: llmgateway.RoleTool, ToolCallID: "call_1", Blocks: text("无结果")},
	}
	invokeAnthropic(t, model, chat, false)
	sent, raw := lastAnthropicBody(t, fixture)
	if sent.System != "你是创作助手" {
		t.Fatalf("system %q", sent.System)
	}
	var thinkingAt []int
	for i, m := range sent.Messages {
		for _, c := range m.Content {
			if c["type"] == "thinking" {
				thinkingAt = append(thinkingAt, i)
			}
		}
	}
	// user, assistant, user(tool), user, assistant*, user(tool), assistant,
	// user(two tools), assistant, user(tool).
	if len(sent.Messages) != 10 || len(thinkingAt) != 1 || thinkingAt[0] != 4 {
		t.Fatalf("thinking sent in messages %v of %d: %s", thinkingAt, len(sent.Messages), raw)
	}
	if len(sent.Messages[7].Content) != 2 || sent.Messages[7].Content[1]["tool_use_id"] != "call_3" {
		t.Fatalf("parallel results were split: %+v", sent.Messages[7])
	}
	if !strings.Contains(raw, `{"query":"侧光","limit":3}`) {
		t.Fatalf("tool arguments were re-encoded: %s", raw)
	}

	// Another deployment of the same model gets no thinking either.
	other := model
	other.DeploymentKey = "dep-2"
	invokeAnthropic(t, other, chat, false)
	if _, raw := lastAnthropicBody(t, fixture); strings.Contains(raw, `"thinking","thinking"`) || strings.Contains(raw, "sig-1") {
		t.Fatalf("another deployment received the signature: %s", raw)
	}
}

func TestAnthropicStreamRecordsReplayableThinking(t *testing.T) {
	fixture := newProtocolFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, anthropicThinkingStream)
	})
	model := anthropicModel(fixture, t)
	result := invokeAnthropic(t, model, toolRequestWithoutChoice(), true)
	if result.Text != "好的" || len(result.ToolCalls) != 1 || string(result.ToolCalls[0].Arguments) != `{"query":"逆光"}` {
		t.Fatalf("result %+v", result)
	}
	// The closing usage omits the cache counts the opening one carried.
	u := result.Usage
	if !u.Complete() || *u.InputTotalTokens != 120 || *u.InputCachedTokens != 100 || *u.OutputTokens != 40 {
		t.Fatalf("usage %+v", u)
	}

	chat := toolRequestWithoutChoice()
	chat.Messages = append(chat.Messages,
		llmgateway.Message{Role: llmgateway.RoleAssistant, Blocks: []llmgateway.Block{{Kind: llmgateway.BlockText, Text: result.Text}}, ToolCalls: result.ToolCalls, Reasoning: result.Reasoning},
		llmgateway.Message{Role: llmgateway.RoleTool, ToolCallID: "call_1", Blocks: []llmgateway.Block{{Kind: llmgateway.BlockText, Text: "无结果"}}},
	)
	invokeAnthropic(t, model, chat, true)
	sent, raw := lastAnthropicBody(t, fixture)
	first := sent.Messages[1].Content[0]
	if first["type"] != "thinking" || first["thinking"] != "先看" || first["signature"] != "sig-2" {
		t.Fatalf("streamed thinking not replayed: %s", raw)
	}
}

func TestAnthropicConvertedChannelUsageStaysBillable(t *testing.T) {
	fixture := newProtocolFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"id":"msg_3","model":"vendor-model","stop_reason":"end_turn","content":[{"type":"text","text":"好"}],"usage":{"input_tokens":42,"input_tokens_details":{"cached_tokens":8},"output_tokens":5}}`)
	})
	u := invokeAnthropic(t, anthropicModel(fixture, t), textRequest("chat"), false).Usage
	if !u.Complete() || *u.InputTotalTokens != 50 || *u.InputCachedTokens != 8 {
		t.Fatalf("usage %+v", u)
	}
}

func TestClaudeReasoningNeverReachesAnotherProtocol(t *testing.T) {
	claude := newProtocolFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, anthropicThinkingBody)
	})
	recorded := invokeAnthropic(t, anthropicModel(claude, t), textRequest("chat"), false).Reasoning

	deepseek := newProtocolFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, openAIThinkingStream)
	})
	model := deepseek.model(t, llmgateway.ProviderOpenAICompatible)
	model.Thinking = "enabled"
	chat := toolRequestWithoutChoice()
	chat.Messages = append(chat.Messages,
		llmgateway.Message{Role: llmgateway.RoleAssistant, Reasoning: recorded, ToolCalls: []llmgateway.ToolCall{{ID: "call_1", Name: "search_assets", Arguments: json.RawMessage(`{"query":"逆光"}`)}}},
		llmgateway.Message{Role: llmgateway.RoleTool, ToolCallID: "call_1", Blocks: []llmgateway.Block{{Kind: llmgateway.BlockText, Text: "无结果"}}},
	)
	if _, err := llmgateway.NewOpenAICompatible(llmgateway.NewHTTPClient(5*time.Second)).Invoke(context.Background(), llmgateway.ProviderRequest{
		Model: model, Snapshot: snapshotFor(model), Chat: chat, Credential: "k", Stream: true,
	}); err != nil {
		t.Fatal(err)
	}
	if raw := deepseek.body.Load().(string); strings.Contains(raw, "sig-1") || strings.Contains(raw, "anthropic_messages") {
		t.Fatalf("a sealed Claude record reached DeepSeek: %s", raw)
	}
}

func TestClaudeModelsAreListedWithTheirLimits(t *testing.T) {
	catalog, err := llmgateway.DefaultCatalog(func(env string) (string, bool) {
		return "test-key", env == "AICODEWITH_API_KEY"
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"claude-opus-5-5", "claude-sonnet-5-5"} {
		model, err := catalog.Model(key)
		if err != nil {
			t.Fatal(err)
		}
		if model.Provider != llmgateway.ProviderAnthropicMessage || model.BaseURL != "https://api.aicodewith.ai" ||
			model.RequestModelID != key || model.VendorKey != "aicodewith" || !model.HandsBackReasoning() {
			t.Fatalf("%s: %+v", key, model.Deployment)
		}
		// The relay answers 400 to a forced tool choice, and the official API
		// rejects temperature; neither is sent.
		for name, chat := range map[string]func(*llmgateway.ChatRequest){
			"tool choice": func(c *llmgateway.ChatRequest) { c.ToolChoice = "required" },
			"temperature": func(c *llmgateway.ChatRequest) { v := 0.3; c.Temperature = &v },
			"json":        func(c *llmgateway.ChatRequest) { c.ResponseFormat = "json_object" },
		} {
			request := textRequest(key)
			chat(&request)
			if err := llmgateway.CheckCapability(request, model); !errors.Is(err, llmgateway.ErrCapability) {
				t.Fatalf("%s: %s accepted: %v", key, name, err)
			}
		}
		// Cache writes are booked as uncached input, so that rate covers them.
		rate := model.Price.UpperBound()
		if rate.InputUncached*4 != rate.Output || rate.InputCached != 200_000 {
			t.Fatalf("%s price %+v", key, rate)
		}
	}
}

func toolRequestWithoutChoice() llmgateway.ChatRequest {
	r := toolRequest()
	r.ToolChoice = ""
	return r
}
