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

const responsesReasoningItem = `{"type":"reasoning","id":"rs_1","summary":[],"encrypted_content":"gAAAA-sealed"}`

const responsesToolBody = `{"id":"resp_1","model":"vendor-model","status":"completed","output":[` +
	responsesReasoningItem + `,` +
	`{"type":"message","id":"msg_1","role":"assistant","content":[{"type":"output_text","text":"好的"}]},` +
	`{"type":"function_call","id":"fc_1","call_id":"call_1","name":"search_assets","arguments":"{\"query\":\"逆光\"}"}` +
	`],"usage":{"input_tokens":120,"input_tokens_details":{"cached_tokens":64},"output_tokens":30,"output_tokens_details":{"reasoning_tokens":12}}}`

var responsesToolStream = `event: response.created
data: {"type":"response.created","response":{"id":"resp_1","model":"vendor-model","status":"in_progress","output":[]}}

event: response.output_text.delta
data: {"type":"response.output_text.delta","delta":"好"}

event: response.output_text.delta
data: {"type":"response.output_text.delta","delta":"的"}

event: response.completed
data: {"type":"response.completed","response":` + responsesToolBody + `}
`

func invokeResponses(t *testing.T, model llmgateway.ModelConfig, chat llmgateway.ChatRequest, stream bool, sink llmgateway.Sink) (llmgateway.Result, error) {
	t.Helper()
	return llmgateway.NewOpenAIResponses(llmgateway.NewHTTPClient(5*time.Second)).Invoke(context.Background(), llmgateway.ProviderRequest{
		Model: model, Snapshot: snapshotFor(model), Chat: chat, Credential: "secret-value", Stream: stream, Sink: sink,
	})
}

func TestResponsesProtocolProducesTheSameCompleteResult(t *testing.T) {
	for _, stream := range []bool{false, true} {
		fixture := newProtocolFixture(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/responses" || r.Header.Get("Authorization") != "Bearer secret-value" {
				t.Errorf("request %s %q", r.URL.Path, r.Header.Get("Authorization"))
			}
			if stream {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, responsesToolStream)
				return
			}
			_, _ = io.WriteString(w, responsesToolBody)
		})
		model := fixture.model(t, llmgateway.ProviderOpenAIResponses)
		model.ReasoningEffort = "medium"
		sink := &recordingSink{}
		result, err := invokeResponses(t, model, toolRequest(), stream, sink)
		if err != nil {
			t.Fatal(err)
		}
		if result.Text != "好的" || result.FinishReason != llmgateway.FinishToolCalls || len(result.ToolCalls) != 1 ||
			result.ToolCalls[0].ID != "call_1" || string(result.ToolCalls[0].Arguments) != `{"query":"逆光"}` {
			t.Fatalf("stream=%v result %+v", stream, result)
		}
		if *result.Usage.InputTotalTokens != 120 || *result.Usage.InputCachedTokens != 64 ||
			*result.Usage.OutputTokens != 30 || *result.Usage.ReasoningTokens != 12 {
			t.Fatalf("usage %+v", result.Usage)
		}
		if stream && strings.Join(sink.deltas, "|") != "好|的" {
			t.Fatalf("deltas %q", sink.deltas)
		}
		if fixture.requests.Load() != 1 {
			t.Fatalf("%d attempts", fixture.requests.Load())
		}
		var sent map[string]any
		if err := json.Unmarshal([]byte(fixture.body.Load().(string)), &sent); err != nil {
			t.Fatal(err)
		}
		// Stateless, with the sealed reasoning asked for so it can come back.
		if sent["store"] != false || sent["max_output_tokens"] != float64(256) ||
			sent["include"].([]any)[0] != "reasoning.encrypted_content" {
			t.Fatalf("request %v", sent)
		}
		reasoning := sent["reasoning"].(map[string]any)
		if reasoning["effort"] != "medium" || reasoning["context"] != "all_turns" {
			t.Fatalf("reasoning %v", reasoning)
		}
		tool := sent["tools"].([]any)[0].(map[string]any)
		if tool["type"] != "function" || tool["name"] != "search_assets" || tool["parameters"] == nil {
			t.Fatalf("tool %v", tool)
		}
		input := sent["input"].([]any)[0].(map[string]any)
		part := input["content"].([]any)[0].(map[string]any)
		if input["role"] != "user" || part["type"] != "input_text" {
			t.Fatalf("input %v", input)
		}
		if strings.Contains(fixture.body.Load().(string), "secret-value") {
			t.Fatal("credential in the body")
		}
	}
}

// The sealed reasoning goes back to the model that wrote it, in the order it
// was produced, and nowhere else.
func TestResponsesReasoningReturnsOnlyToItsOwnModel(t *testing.T) {
	fixture := newProtocolFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, responsesToolBody)
	})
	model := fixture.model(t, llmgateway.ProviderOpenAIResponses)
	model.ReasoningEffort = "medium"
	first, err := invokeResponses(t, model, toolRequest(), false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(first.Reasoning, "gAAAA-sealed") {
		t.Fatalf("reasoning %q", first.Reasoning)
	}

	chat := toolRequest()
	chat.ToolChoice = ""
	chat.Messages = append(chat.Messages,
		llmgateway.Message{Role: llmgateway.RoleAssistant, Reasoning: first.Reasoning, Blocks: []llmgateway.Block{{Kind: llmgateway.BlockText, Text: "好的"}},
			ToolCalls: first.ToolCalls},
		llmgateway.Message{Role: llmgateway.RoleTool, ToolCallID: "call_1", Blocks: []llmgateway.Block{{Kind: llmgateway.BlockText, Text: "无结果"}}},
		llmgateway.Message{Role: llmgateway.RoleAssistant, Reasoning: "DeepSeek 的思考原文", Blocks: []llmgateway.Block{{Kind: llmgateway.BlockText, Text: "没找到"}}},
		llmgateway.Message{Role: llmgateway.RoleUser, Blocks: []llmgateway.Block{{Kind: llmgateway.BlockText, Text: "再试试"}}},
	)
	if err := chat.Validate(); err != nil {
		t.Fatal(err)
	}
	sentTypes := func() []string {
		t.Helper()
		var sent struct {
			Input []map[string]any `json:"input"`
		}
		if err := json.Unmarshal([]byte(fixture.body.Load().(string)), &sent); err != nil {
			t.Fatal(err)
		}
		var types []string
		for _, item := range sent.Input {
			kind, _ := item["type"].(string)
			if kind == "" {
				kind = item["role"].(string)
			}
			if kind == "function_call_output" && item["output"] != "无结果" {
				t.Fatalf("tool output %v", item["output"])
			}
			types = append(types, kind)
		}
		return types
	}
	if _, err := invokeResponses(t, model, chat, false, nil); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(sentTypes(), ","); got != "user,reasoning,message,function_call,function_call_output,message,user" {
		t.Fatalf("input order %s", got)
	}
	if strings.Contains(fixture.body.Load().(string), "DeepSeek 的思考原文") {
		t.Fatal("another vendor's reasoning reached this model")
	}

	// Neither another model nor the same model behind another deployment can
	// open these items.
	other := model
	other.RequestModelID, other.AcceptedModelIDs = "other-model", []string{"other-model", "vendor-model"}
	relay := model
	relay.DeploymentKey = "other-vendor/vendor-model"
	for name, elsewhere := range map[string]llmgateway.ModelConfig{"model": other, "deployment": relay} {
		if _, err := invokeResponses(t, elsewhere, chat, false, nil); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(fixture.body.Load().(string), "gAAAA-sealed") {
			t.Fatalf("sealed reasoning went to a different %s", name)
		}
	}

	// A thinking chat-completions deployment still gets the field, empty.
	deepseek := newProtocolFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"id":"r","model":"vendor-model","choices":[{"index":0,"finish_reason":"stop","message":{"content":"好"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`)
	})
	thinking := deepseek.model(t, llmgateway.ProviderOpenAICompatible)
	thinking.Thinking = "enabled"
	if _, err := llmgateway.NewOpenAICompatible(llmgateway.NewHTTPClient(5*time.Second)).Invoke(context.Background(), llmgateway.ProviderRequest{
		Model: thinking, Snapshot: snapshotFor(thinking), Chat: chat, Credential: "k",
	}); err != nil {
		t.Fatal(err)
	}
	body := deepseek.body.Load().(string)
	if strings.Contains(body, "gAAAA-sealed") || !strings.Contains(body, `"reasoning_content":""`) {
		t.Fatalf("chat completions got %s", body)
	}
}

func TestResponsesUnfinishedAnswersAreNeverSuccess(t *testing.T) {
	cases := []struct {
		name, body string
		stream     bool
		finish     llmgateway.FinishReason
		outcome    llmgateway.Outcome
	}{
		{name: "truncated", finish: llmgateway.FinishLength,
			body: `{"id":"r","model":"vendor-model","status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"output":[{"type":"function_call","call_id":"c","name":"search_assets","arguments":"{\"que"}]}`},
		{name: "filtered", finish: llmgateway.FinishRefused,
			body: `{"id":"r","model":"vendor-model","status":"incomplete","incomplete_details":{"reason":"content_filter"},"output":[]}`},
		{name: "failed", outcome: llmgateway.OutcomeUnknown,
			body: `{"id":"r","model":"vendor-model","status":"failed","error":{"code":"server_error","message":"boom"},"output":[]}`},
		{name: "unterminated stream", stream: true, outcome: llmgateway.OutcomeUnknown,
			body: "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"好\"}\n\n"},
		{name: "stream error", stream: true, outcome: llmgateway.OutcomeUnknown,
			body: "event: error\ndata: {\"type\":\"error\",\"message\":\"overloaded\"}\n\n"},
		{name: "rerouted", outcome: llmgateway.OutcomeProtocol,
			body: `{"id":"r","model":"gpt-4o","status":"completed","output":[]}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fixture := newProtocolFixture(t, func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, c.body)
			})
			result, err := invokeResponses(t, fixture.model(t, llmgateway.ProviderOpenAIResponses), toolRequest(), c.stream, nil)
			if c.outcome != "" {
				var provider *llmgateway.ProviderError
				if !errors.As(err, &provider) || provider.Outcome != c.outcome {
					t.Fatalf("err %v", err)
				}
				return
			}
			if err != nil || result.FinishReason != c.finish || result.Consumable() {
				t.Fatalf("result %+v err %v", result, err)
			}
		})
	}
}
