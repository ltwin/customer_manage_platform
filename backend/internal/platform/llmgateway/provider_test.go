package llmgateway_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
)

// protocolFixture serves one fake deployment and counts real attempts, so a
// hidden SDK or transport retry would be visible as a second request.
type protocolFixture struct {
	server   *httptest.Server
	requests atomic.Int64
	body     atomic.Value
}

func newProtocolFixture(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) *protocolFixture {
	t.Helper()
	f := &protocolFixture{}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requests.Add(1)
		raw, _ := io.ReadAll(r.Body)
		f.body.Store(string(raw))
		handler(w, r)
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (f *protocolFixture) model(t *testing.T, provider llmgateway.ProviderKey) llmgateway.ModelConfig {
	t.Helper()
	return llmgateway.ModelConfig{
		ModelKey: "chat", Provider: provider, DeploymentKey: "dep-1",
		BaseURL: f.server.URL, CredentialEnv: "TEST_KEY", RequestModelID: "vendor-model",
		AcceptedModelIDs: []string{"vendor-model"},
		Capability:       llmgateway.Capability{ToolCalling: true, ContextTokens: 10000, MaxOutputTokens: 1000},
		Price:            usablePrice(), LimitKey: "dep-1", Concurrency: 2, RateLimitPerMin: 10,
		QueryCapability: "none", CancelCapability: "none", Enabled: true,
	}
}

func snapshotFor(model llmgateway.ModelConfig) llmgateway.ModelSnapshot {
	return llmgateway.ModelSnapshot{
		ModelKey: model.ModelKey, CatalogVersion: "cat-1", DeploymentKey: model.DeploymentKey,
		Provider: model.Provider, RequestModelID: model.RequestModelID,
		AcceptedModelIDs: model.AcceptedModelIDs, PriceVersion: model.Price.Version,
		Currency: "USD", MaxOutputTokens: model.Capability.MaxOutputTokens,
	}
}

func toolRequest() llmgateway.ChatRequest {
	r := textRequest("chat")
	r.Tools = []llmgateway.ToolDefinition{{
		Name: "search_assets", Description: "查参考", SchemaVersion: 1,
		InputSchema: json.RawMessage(okToolSchema),
	}}
	r.ToolChoice = "auto"
	return r
}

func invoke(t *testing.T, f *protocolFixture, provider llmgateway.Provider, key llmgateway.ProviderKey, stream bool) (llmgateway.Result, error) {
	t.Helper()
	model := f.model(t, key)
	return provider.Invoke(context.Background(), llmgateway.ProviderRequest{
		Model:      model,
		Snapshot:   snapshotFor(model),
		Chat:       toolRequest(),
		Credential: "secret-value",
		Stream:     stream,
	})
}

const openAIToolStream = `data: {"id":"req-1","model":"vendor-model","choices":[{"index":0,"delta":{"role":"assistant","content":"好的"}}]}

data: {"id":"req-1","model":"vendor-model","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"search_ass","arguments":"{\"que"}}]}}]}

data: {"id":"req-1","model":"vendor-model","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"name":"ets","arguments":"ry\":\"逆光\"}"}}]}}]}

data: {"id":"req-1","model":"vendor-model","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}

data: {"id":"req-1","model":"vendor-model","choices":[],"usage":{"prompt_tokens":120,"completion_tokens":30,"prompt_cache_hit_tokens":64,"prompt_cache_miss_tokens":56,"total_tokens":150}}

data: [DONE]
`

const anthropicToolStream = `data: {"type":"message_start","message":{"id":"req-1","model":"vendor-model","usage":{"input_tokens":56,"cache_read_input_tokens":64}}}

data: {"type":"content_block_start","index":0,"content_block":{"type":"text"}}

data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"好的"}}

data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"call_1","name":"search_assets"}}

data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"que"}}

data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"ry\":\"逆光\"}"}}

data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"input_tokens":56,"cache_read_input_tokens":64,"output_tokens":30}}

data: {"type":"message_stop"}
`

// TestTwoProtocolsProduceTheSameCompleteResult is the contract-independence
// proof required before a first vendor may be enabled.
func TestTwoProtocolsProduceTheSameCompleteResult(t *testing.T) {
	openAI := newProtocolFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, openAIToolStream)
	})
	anthropic := newProtocolFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, anthropicToolStream)
	})

	client := llmgateway.NewHTTPClient(5 * time.Second)
	first, err := invoke(t, openAI, llmgateway.NewOpenAICompatible(client), llmgateway.ProviderOpenAICompatible, true)
	if err != nil {
		t.Fatalf("openai compatible: %v", err)
	}
	second, err := invoke(t, anthropic, llmgateway.NewAnthropicMessages(client, ""), llmgateway.ProviderAnthropicMessage, true)
	if err != nil {
		t.Fatalf("anthropic: %v", err)
	}

	for name, got := range map[string]llmgateway.Result{"openai": first, "anthropic": second} {
		if got.Text != "好的" {
			t.Fatalf("%s: text %q", name, got.Text)
		}
		if got.FinishReason != llmgateway.FinishToolCalls || !got.Consumable() {
			t.Fatalf("%s: finish %q", name, got.FinishReason)
		}
		if len(got.ToolCalls) != 1 {
			t.Fatalf("%s: %d tool calls", name, len(got.ToolCalls))
		}
		call := got.ToolCalls[0]
		if call.ID != "call_1" || call.Name != "search_assets" {
			t.Fatalf("%s: fragments were not reassembled: %+v", name, call)
		}
		var arguments struct {
			Query string `json:"query"`
		}
		if err := json.Unmarshal(call.Arguments, &arguments); err != nil || arguments.Query != "逆光" {
			t.Fatalf("%s: arguments %s (%v)", name, call.Arguments, err)
		}
		if got.Usage.InputTotalTokens == nil || *got.Usage.InputTotalTokens != 120 {
			t.Fatalf("%s: input tokens %+v", name, got.Usage.InputTotalTokens)
		}
		if got.Usage.InputCachedTokens == nil || *got.Usage.InputCachedTokens != 64 {
			t.Fatalf("%s: cached tokens %+v", name, got.Usage.InputCachedTokens)
		}
		if got.Usage.OutputTokens == nil || *got.Usage.OutputTokens != 30 {
			t.Fatalf("%s: output tokens %+v", name, got.Usage.OutputTokens)
		}
	}
}

func TestCredentialNeverAppearsInTheRequestBody(t *testing.T) {
	fixture := newProtocolFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret-value" {
			t.Errorf("credential must travel in the auth header, got %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, openAIToolStream)
	})
	if _, err := invoke(t, fixture, llmgateway.NewOpenAICompatible(llmgateway.NewHTTPClient(5*time.Second)), llmgateway.ProviderOpenAICompatible, true); err != nil {
		t.Fatal(err)
	}
	if body, _ := fixture.body.Load().(string); strings.Contains(body, "secret-value") {
		t.Fatal("credential leaked into the request payload")
	}
}

func TestTruncatedAnswerNeverBecomesAnExecutableToolCall(t *testing.T) {
	// The arguments happen to be valid JSON, but the answer was cut off.
	const truncated = `{"id":"req-1","model":"vendor-model","choices":[{"index":0,"finish_reason":"length","message":{"content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"search_assets","arguments":"{\"query\":\"a\"}"}}]}}],"usage":{"prompt_tokens":10,"completion_tokens":1000}}`
	fixture := newProtocolFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, truncated)
	})
	result, err := invoke(t, fixture, llmgateway.NewOpenAICompatible(llmgateway.NewHTTPClient(5*time.Second)), llmgateway.ProviderOpenAICompatible, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.FinishReason != llmgateway.FinishLength {
		t.Fatalf("finish %q", result.FinishReason)
	}
	if result.Consumable() {
		t.Fatal("a length-truncated answer must never produce tool steps")
	}
}

func TestUnterminatedStreamIsUnknownNotSuccess(t *testing.T) {
	fixture := newProtocolFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"id":"req-1","model":"vendor-model","choices":[{"index":0,"delta":{"content":"部分"}}]}`+"\n\n")
	})
	_, err := invoke(t, fixture, llmgateway.NewOpenAICompatible(llmgateway.NewHTTPClient(5*time.Second)), llmgateway.ProviderOpenAICompatible, true)
	var providerErr *llmgateway.ProviderError
	if !errors.As(err, &providerErr) || providerErr.Outcome != llmgateway.OutcomeUnknown {
		t.Fatalf("an interrupted stream must stay unknown, got %v", err)
	}
	if providerErr.Retryable() {
		t.Fatal("unknown must never be retried automatically")
	}
}

func TestUnexpectedModelRoutingIsAProtocolError(t *testing.T) {
	const routed = `{"id":"req-1","model":"some-other-model","choices":[{"index":0,"finish_reason":"stop","message":{"content":"hi"}}]}`
	fixture := newProtocolFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, routed)
	})
	_, err := invoke(t, fixture, llmgateway.NewOpenAICompatible(llmgateway.NewHTTPClient(5*time.Second)), llmgateway.ProviderOpenAICompatible, false)
	var providerErr *llmgateway.ProviderError
	if !errors.As(err, &providerErr) || providerErr.Outcome != llmgateway.OutcomeProtocol {
		t.Fatalf("a silent model switch must be refused, got %v", err)
	}
}

func TestMissingUsageStaysUnknownInsteadOfZero(t *testing.T) {
	const noUsage = `{"id":"req-1","model":"vendor-model","choices":[{"index":0,"finish_reason":"stop","message":{"content":"hi"}}]}`
	fixture := newProtocolFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, noUsage)
	})
	result, err := invoke(t, fixture, llmgateway.NewOpenAICompatible(llmgateway.NewHTTPClient(5*time.Second)), llmgateway.ProviderOpenAICompatible, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Usage.InputTotalTokens != nil || result.Usage.OutputTokens != nil || result.Usage.Complete() {
		t.Fatalf("absent usage must stay nil, got %+v", result.Usage)
	}
}

func TestStatusEvidenceIsClassifiedAndNeverRetriedInternally(t *testing.T) {
	cases := []struct {
		status     int
		unaccepted []int
		outcome    llmgateway.Outcome
		retry      bool
	}{
		{status: http.StatusBadRequest, outcome: llmgateway.OutcomeRejected},
		{status: http.StatusUnauthorized, outcome: llmgateway.OutcomeRejected},
		{status: http.StatusPaymentRequired, outcome: llmgateway.OutcomeRejected},
		{status: http.StatusInternalServerError, outcome: llmgateway.OutcomeUnknown},
		{status: http.StatusServiceUnavailable, outcome: llmgateway.OutcomeUnknown},
		// A deployment that was never verified to refuse before processing
		// must not have its 429 treated as free: a retry could pay twice.
		{status: http.StatusTooManyRequests, outcome: llmgateway.OutcomeUnknown},
		// Only a deployment that declares it earns the retryable classification.
		{status: http.StatusTooManyRequests, unaccepted: []int{429}, outcome: llmgateway.OutcomeUnaccepted, retry: true},
	}
	for _, tc := range cases {
		fixture := newProtocolFixture(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = io.WriteString(w, `{"error":{"message":"x"}}`)
		})
		model := fixture.model(t, llmgateway.ProviderOpenAICompatible)
		model.UnacceptedStatuses = tc.unaccepted
		provider := llmgateway.NewOpenAICompatible(llmgateway.NewHTTPClient(5 * time.Second))
		_, err := provider.Invoke(context.Background(), llmgateway.ProviderRequest{
			Model: model, Snapshot: snapshotFor(model), Chat: toolRequest(), Credential: "secret-value",
		})
		var providerErr *llmgateway.ProviderError
		if !errors.As(err, &providerErr) {
			t.Fatalf("status %d: want provider error, got %v", tc.status, err)
		}
		if providerErr.Outcome != tc.outcome || providerErr.Retryable() != tc.retry {
			t.Fatalf("status %d (declared %v): outcome %s retryable %v",
				tc.status, tc.unaccepted, providerErr.Outcome, providerErr.Retryable())
		}
		if got := fixture.requests.Load(); got != 1 {
			t.Fatalf("status %d: the adapter made %d attempts; implicit retries must be off", tc.status, got)
		}
	}
}

func TestDuplicateToolCallIdentityIsRefused(t *testing.T) {
	const duplicated = `{"id":"req-1","model":"vendor-model","choices":[{"index":0,"finish_reason":"tool_calls","message":{"tool_calls":[
		{"id":"call_1","type":"function","function":{"name":"search_assets","arguments":"{}"}},
		{"id":"call_1","type":"function","function":{"name":"search_assets","arguments":"{}"}}]}}]}`
	fixture := newProtocolFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, duplicated)
	})
	_, err := invoke(t, fixture, llmgateway.NewOpenAICompatible(llmgateway.NewHTTPClient(5*time.Second)), llmgateway.ProviderOpenAICompatible, false)
	var providerErr *llmgateway.ProviderError
	if !errors.As(err, &providerErr) || providerErr.Outcome != llmgateway.OutcomeProtocol {
		t.Fatalf("duplicate call ids must be refused, got %v", err)
	}
}
