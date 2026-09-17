package llmgateway_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func (f *fixture) session(scope store.AccountScope, group string) llmgateway.ModelSession {
	// A real caller's turn key comes from its own persisted step; this counter
	// stands in for that durable sequence.
	turn := 0
	return llmgateway.ModelSession{
		CallSession: f.callSession(scope, group),
		TurnKey: func(context.Context, llmgateway.ChatRequest) (string, error) {
			turn++
			return fmt.Sprintf("%s:turn-%d", group, turn), nil
		},
	}
}

func (f *fixture) callSession(scope store.AccountScope, group string) llmgateway.CallSession {
	return llmgateway.CallSession{
		Scope:            scope,
		Limits:           limitsOf,
		CallerService:    "creative_agent",
		CallerGroupID:    group,
		GroupLimitMicros: 5_000_000,
		GroupTokenLimit:  500_000,
		GroupDeadline:    f.now.Add(time.Hour),
		ModelKey:         "chat",
		OutputLimit:      256,
		Deadline:         f.now.Add(time.Hour),
	}
}

func TestEinoAdapterRunsOneMeteredTurn(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	result := okResult()
	result.FinishReason = llmgateway.FinishToolCalls
	result.ToolCalls = []llmgateway.ToolCall{{ID: "call_1", Name: "search_assets", Arguments: json.RawMessage(`{"query":"逆光"}`)}}
	f.provider.result = result

	adapter, err := f.gateway.NewEinoModel(f.session(f.a, "run-eino"))
	if err != nil {
		t.Fatal(err)
	}
	message, err := adapter.Generate(t.Context(), []*schema.Message{
		schema.SystemMessage("你是摄影创作助手"),
		schema.UserMessage("找三张逆光参考"),
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if message.Role != schema.Assistant || message.Content != "三个方向" {
		t.Fatalf("message %+v", message)
	}
	if len(message.ToolCalls) != 1 || message.ToolCalls[0].Function.Name != "search_assets" {
		t.Fatalf("tool calls %+v", message.ToolCalls)
	}
	if message.ResponseMeta == nil || message.ResponseMeta.Usage == nil || message.ResponseMeta.Usage.PromptTokens != 1000 {
		t.Fatalf("usage was not carried back: %+v", message.ResponseMeta)
	}

	// The auxiliary framework path must land in the same ledger.
	if _, spent, _ := f.budget(t, "gw-a"); spent == 0 {
		t.Fatal("an Eino turn must be metered like any other gateway call")
	}
	var requests int
	if err := f.db.QueryRow(`SELECT count(*) FROM llm_requests WHERE account_id='gw-a'`).Scan(&requests); err != nil {
		t.Fatal(err)
	}
	if requests != 1 {
		t.Fatalf("the adapter created %d requests for one turn", requests)
	}
}

func TestEinoAdapterKeepsToolConfigurationIsolated(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	adapter, err := f.gateway.NewEinoModel(f.session(f.a, "run-eino"))
	if err != nil {
		t.Fatal(err)
	}
	tool := &schema.ToolInfo{
		Name: "search_assets", Desc: "查参考",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"query": {Type: schema.String, Desc: "关键词", Required: true},
		}),
	}
	withTools, err := adapter.WithTools([]*schema.ToolInfo{tool})
	if err != nil {
		t.Fatal(err)
	}
	if withTools == model.ToolCallingChatModel(adapter) {
		t.Fatal("WithTools must return an isolated configuration, not the shared instance")
	}

	var seen llmgateway.ProviderRequest
	f.provider.observe = func(r llmgateway.ProviderRequest) { seen = r }
	if _, err := withTools.Generate(t.Context(), []*schema.Message{schema.UserMessage("找参考")}); err != nil {
		t.Fatal(err)
	}
	if len(seen.Chat.Tools) != 1 || seen.Chat.Tools[0].Name != "search_assets" {
		t.Fatalf("configured tools did not reach the dispatch: %+v", seen.Chat.Tools)
	}

	// The original adapter still carries no tools.
	if _, err := adapter.Generate(t.Context(), []*schema.Message{schema.UserMessage("再找一次")}); err != nil {
		t.Fatal(err)
	}
	if len(seen.Chat.Tools) != 0 {
		t.Fatalf("the shared instance was mutated: %+v", seen.Chat.Tools)
	}
}

func TestEinoAdapterRefusesUnboundMultimodalState(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	adapter, err := f.gateway.NewEinoModel(f.session(f.a, "run-eino"))
	if err != nil {
		t.Fatal(err)
	}
	message := schema.UserMessage("看这张图")
	message.UserInputMultiContent = []schema.MessageInputPart{{Type: schema.ChatMessagePartTypeImageURL}}
	_, err = adapter.Generate(t.Context(), []*schema.Message{message})
	if !errors.Is(err, llmgateway.ErrCapability) {
		t.Fatalf("framework-carried media must be refused, got %v", err)
	}
	if f.provider.attempts() != 0 {
		t.Fatal("an unbound media part reached the provider")
	}
}

func TestEinoStreamReturnsOneValidatedResult(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	adapter, err := f.gateway.NewEinoModel(f.session(f.a, "run-eino"))
	if err != nil {
		t.Fatal(err)
	}
	reader, err := adapter.Stream(t.Context(), []*schema.Message{schema.UserMessage("找参考")})
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	first, err := reader.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if first.Content != "三个方向" {
		t.Fatalf("stream message %+v", first)
	}
	if _, err := reader.Recv(); err == nil {
		t.Fatal("the gateway must expose exactly one complete result")
	}
}

func TestEinoAdapterUsesCallTimeToolsWithoutChangingSession(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	adapter, err := f.gateway.NewEinoModel(f.session(f.a, "runtime-tools"))
	if err != nil {
		t.Fatal(err)
	}
	info := &schema.ToolInfo{Name: "read_run_result", Desc: "读取运行结果", ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{"item_id": {Type: schema.String, Required: true}})}
	var seen llmgateway.ProviderRequest
	f.provider.observe = func(r llmgateway.ProviderRequest) { seen = r }
	for _, tc := range []struct {
		options []model.Option
		tools   int
	}{{[]model.Option{model.WithTools([]*schema.ToolInfo{info})}, 1}, {[]model.Option{model.WithTools(nil)}, 0}, {nil, 0}} {
		if _, err = adapter.Generate(t.Context(), []*schema.Message{schema.UserMessage("读取")}, tc.options...); err != nil {
			t.Fatal(err)
		}
		if len(seen.Chat.Tools) != tc.tools {
			t.Fatalf("tools=%+v", seen.Chat.Tools)
		}
	}
	if _, err = adapter.Generate(t.Context(), []*schema.Message{schema.UserMessage("读取")}, model.WithModel("another-model")); !errors.Is(err, llmgateway.ErrCapability) {
		t.Fatalf("model override: %v", err)
	}
}
