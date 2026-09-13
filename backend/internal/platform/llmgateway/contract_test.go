package llmgateway_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
)

func textRequest(modelKey string) llmgateway.ChatRequest {
	return llmgateway.ChatRequest{
		ContractVersion: llmgateway.ContractVersion,
		ModelKey:        modelKey,
		OutputLimit:     256,
		Messages: []llmgateway.Message{{
			Role:   llmgateway.RoleUser,
			Blocks: []llmgateway.Block{{Kind: llmgateway.BlockText, Text: "帮我整理三张逆光人像的参考方向"}},
		}},
	}
}

const okToolSchema = `{"type":"object","additionalProperties":false,"properties":{"query":{"type":"string","maxLength":40}},"required":["query"]}`

func TestValidateRejectsUnsupportedSchemaKeywords(t *testing.T) {
	cases := map[string]string{
		"oneOf not in the tested subset": `{"type":"object","additionalProperties":false,"oneOf":[]}`,
		"missing additionalProperties":   `{"type":"object","properties":{}}`,
		"additionalProperties true":      `{"type":"object","additionalProperties":true}`,
		"array without items":            `{"type":"object","additionalProperties":false,"properties":{"a":{"type":"array"}}}`,
		"unsupported leaf type":          `{"type":"object","additionalProperties":false,"properties":{"a":{"type":"null"}}}`,
		"non object root":                `{"type":"string"}`,
	}
	for name, schema := range cases {
		t.Run(name, func(t *testing.T) {
			r := textRequest("m")
			r.Tools = []llmgateway.ToolDefinition{{Name: "search", Description: "d", SchemaVersion: 1, InputSchema: json.RawMessage(schema)}}
			if err := r.Validate(); !errors.Is(err, llmgateway.ErrValidation) {
				t.Fatalf("want validation error, got %v", err)
			}
		})
	}
	r := textRequest("m")
	r.Tools = []llmgateway.ToolDefinition{{Name: "search", Description: "d", SchemaVersion: 1, InputSchema: json.RawMessage(okToolSchema)}}
	if err := r.Validate(); err != nil {
		t.Fatalf("supported subset rejected: %v", err)
	}
}

func TestValidateRejectsToolMessageWithoutVerifiedCall(t *testing.T) {
	r := textRequest("m")
	r.Messages = append(r.Messages, llmgateway.Message{
		Role:       llmgateway.RoleTool,
		ToolCallID: "call_1",
		Blocks:     []llmgateway.Block{{Kind: llmgateway.BlockText, Text: "result"}},
	})
	if err := r.Validate(); !errors.Is(err, llmgateway.ErrValidation) {
		t.Fatalf("unverified tool_call_id accepted: %v", err)
	}

	r.Messages = []llmgateway.Message{
		r.Messages[0],
		{Role: llmgateway.RoleAssistant, ToolCalls: []llmgateway.ToolCall{{ID: "call_1", Name: "search", Arguments: json.RawMessage(`{"query":"x"}`)}}},
		{Role: llmgateway.RoleTool, ToolCallID: "call_1", Blocks: []llmgateway.Block{{Kind: llmgateway.BlockText, Text: "result"}}},
	}
	if err := r.Validate(); err != nil {
		t.Fatalf("verified tool round trip rejected: %v", err)
	}
}

func TestValidateRejectsUnboundImageHandle(t *testing.T) {
	r := textRequest("m")
	r.Messages[0].Blocks = append(r.Messages[0].Blocks, llmgateway.Block{
		Kind:  llmgateway.BlockImageInput,
		Image: &llmgateway.ImageInput{RevisionID: "ccrv_1", MIME: "image/jpeg"},
	})
	if err := r.Validate(); !errors.Is(err, llmgateway.ErrValidation) {
		t.Fatalf("image without digest or opener accepted: %v", err)
	}
}

func TestRequestHashIgnoresTransientMaterialAndTracksPrice(t *testing.T) {
	snapshot := llmgateway.ModelSnapshot{
		ModelKey: "m", CatalogVersion: "c1", DeploymentKey: "d1", PriceVersion: "p1",
		AcceptedModelIDs: []string{"vendor-model"},
	}
	r := textRequest("m")
	r.Tools = []llmgateway.ToolDefinition{{Name: "search", Description: "d", SchemaVersion: 1, InputSchema: json.RawMessage(okToolSchema)}}

	base, err := llmgateway.RequestHash(r, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	reordered := r
	reordered.Tools = []llmgateway.ToolDefinition{{
		Name: "search", Description: "d", SchemaVersion: 1,
		InputSchema: json.RawMessage(`{"additionalProperties":false,"properties":{"query":{"maxLength":40,"type":"string"}},"required":["query"],"type":"object"}`),
	}}
	same, err := llmgateway.RequestHash(reordered, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if same != base {
		t.Fatal("key order changed the request identity")
	}

	repriced := snapshot
	repriced.PriceVersion = "p2"
	changed, err := llmgateway.RequestHash(r, repriced)
	if err != nil {
		t.Fatal(err)
	}
	if changed == base {
		t.Fatal("a different price version must produce a different request identity")
	}
}

func TestCostMicrosRoundsUpWithExactArithmetic(t *testing.T) {
	// 1 token at $0.15 per 1M tokens is a fraction of one micro and must not
	// be billed as zero.
	if got := llmgateway.CostMicros(1, 150_000); got != 1 {
		t.Fatalf("want 1 micro, got %d", got)
	}
	if got := llmgateway.CostMicros(1_000_000, 150_000); got != 150_000 {
		t.Fatalf("want 150000 micros, got %d", got)
	}
	if got := llmgateway.CostMicros(0, 150_000); got != 0 {
		t.Fatalf("want 0, got %d", got)
	}
}

func TestPriceScheduleUsesWindowRateAndConservativeBound(t *testing.T) {
	price := llmgateway.PriceSchedule{
		Version:  "p1",
		Currency: "USD",
		Peak:     llmgateway.Rate{InputCached: 6000, InputUncached: 300_000, Output: 1_200_000},
		OffPeak:  llmgateway.Rate{InputCached: 3000, InputUncached: 150_000, Output: 600_000},
		PeakWindows: []llmgateway.PeakWindow{
			{Weekdays: []int{1, 2, 3, 4, 5}, StartHour: 1, EndHour: 4},
		},
	}
	// 2026-09-14 is a Monday.
	peak := time.Date(2026, 9, 14, 2, 0, 0, 0, time.UTC)
	off := time.Date(2026, 9, 14, 5, 0, 0, 0, time.UTC)
	weekend := time.Date(2026, 9, 13, 2, 0, 0, 0, time.UTC)

	if price.RateAt(peak).Output != 1_200_000 {
		t.Fatal("peak window must use the peak rate")
	}
	if price.RateAt(off).Output != 600_000 {
		t.Fatal("outside the window must use the off-peak rate")
	}
	if price.RateAt(weekend).Output != 600_000 {
		t.Fatal("weekend must use the off-peak rate")
	}
	if price.UpperBound().Output != 1_200_000 {
		t.Fatal("a reservation must be bounded by the most expensive rate")
	}
}

func testCatalog(t *testing.T, price llmgateway.PriceSchedule) *llmgateway.Catalog {
	t.Helper()
	c, err := llmgateway.NewCatalog("cat-1", "USD", []llmgateway.ModelConfig{{
		ModelKey: "chat", DisplayName: "Chat", Provider: llmgateway.ProviderOpenAICompatible,
		DeploymentKey: "dep-1", BaseURL: "https://example.invalid", CredentialEnv: "TEST_KEY",
		RequestModelID: "vendor-model", AcceptedModelIDs: []string{"vendor-model"},
		Capability: llmgateway.Capability{ToolCalling: true, ContextTokens: 1000, MaxOutputTokens: 500},
		Price:      price, LimitKey: "dep-1", Concurrency: 2, RateLimitPerMin: 10,
		QueryCapability: "none", CancelCapability: "none", Enabled: true,
	}})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func usablePrice() llmgateway.PriceSchedule {
	return llmgateway.PriceSchedule{
		Version: "p1", Currency: "USD",
		Peak:    llmgateway.Rate{InputCached: 6000, InputUncached: 300_000, Output: 1_200_000},
		OffPeak: llmgateway.Rate{InputCached: 3000, InputUncached: 150_000, Output: 600_000},
	}
}

func TestCatalogRefusesModelWithoutUsablePrice(t *testing.T) {
	price := usablePrice()
	price.Peak, price.OffPeak = llmgateway.Rate{}, llmgateway.Rate{}
	_, err := llmgateway.NewCatalog("cat-1", "USD", []llmgateway.ModelConfig{{
		ModelKey: "chat", Provider: llmgateway.ProviderOpenAICompatible, DeploymentKey: "dep-1",
		BaseURL: "https://example.invalid", CredentialEnv: "TEST_KEY", RequestModelID: "m",
		AcceptedModelIDs: []string{"m"},
		Capability:       llmgateway.Capability{ContextTokens: 10, MaxOutputTokens: 10},
		Price:            price, LimitKey: "dep-1", Concurrency: 1, RateLimitPerMin: 1,
		QueryCapability: "none", CancelCapability: "none", Enabled: true,
	}})
	if !errors.Is(err, llmgateway.ErrValidation) {
		t.Fatalf("a model without a price recipe must not be admitted: %v", err)
	}
}

func TestCheckCapabilityRefusesBeforeDispatch(t *testing.T) {
	catalog := testCatalog(t, usablePrice())
	model, err := catalog.Model("chat")
	if err != nil {
		t.Fatal(err)
	}

	over := textRequest("chat")
	over.OutputLimit = 5000
	if err := llmgateway.CheckCapability(over, model); !errors.Is(err, llmgateway.ErrCapability) {
		t.Fatalf("output beyond the deployment bound must be refused: %v", err)
	}

	withImage := textRequest("chat")
	withImage.Messages[0].Blocks = append(withImage.Messages[0].Blocks, llmgateway.Block{
		Kind: llmgateway.BlockImageInput,
		Image: &llmgateway.ImageInput{
			RevisionID: "ccrv_1", Digest: "sha256-" + string(make([]byte, 0)), MIME: "image/jpeg",
		},
	})
	if err := llmgateway.CheckCapability(withImage, model); !errors.Is(err, llmgateway.ErrCapability) {
		t.Fatalf("image input on a text-only deployment must be refused: %v", err)
	}

	temperature := 0.5
	withTemp := textRequest("chat")
	withTemp.Temperature = &temperature
	if err := llmgateway.CheckCapability(withTemp, model); !errors.Is(err, llmgateway.ErrCapability) {
		t.Fatalf("unsupported temperature must be refused instead of dropped: %v", err)
	}
}

func TestDisabledModelKeepsAVisibleReason(t *testing.T) {
	catalog, err := llmgateway.NewCatalog("cat-1", "USD", []llmgateway.ModelConfig{{
		ModelKey: "chat", Provider: llmgateway.ProviderOpenAICompatible, DeploymentKey: "dep-1",
		BaseURL: "https://example.invalid", CredentialEnv: "TEST_KEY", RequestModelID: "m",
		AcceptedModelIDs: []string{"m"},
		Capability:       llmgateway.Capability{ContextTokens: 10, MaxOutputTokens: 10},
		Price:            usablePrice(), LimitKey: "dep-1", Concurrency: 1, RateLimitPerMin: 1,
		QueryCapability: "none", CancelCapability: "none",
		Enabled: false, DisabledReason: "尚未取得供应商额度",
	}})
	if err != nil {
		t.Fatal(err)
	}
	entries := catalog.List()
	if len(entries) != 1 || entries[0].Available || entries[0].UnavailableWhy == "" {
		t.Fatalf("a disabled model must stay listed with a reason: %+v", entries)
	}
	if _, err := catalog.Model("chat"); !errors.Is(err, llmgateway.ErrUnavailable) {
		t.Fatalf("a disabled model must not dispatch: %v", err)
	}
}

func TestThinkingDeploymentRefusesToolChoiceBeforeDispatch(t *testing.T) {
	// Verified against the real DeepSeek API on 2026-09-11: thinking mode
	// answers 400 for an explicit tool_choice, so the catalog must refuse it
	// locally instead of spending an attempt to learn that.
	catalog, err := llmgateway.DefaultCatalog(func(string) (string, bool) { return "k", true })
	if err != nil {
		t.Fatal(err)
	}
	thinking, err := catalog.Model("deepseek-flash-thinking")
	if err != nil {
		t.Fatal(err)
	}
	plain, err := catalog.Model("deepseek-flash")
	if err != nil {
		t.Fatal(err)
	}
	if thinking.Thinking != "enabled" || plain.Thinking != "disabled" {
		t.Fatalf("the reasoning mode must be fixed per deployment: %q / %q", thinking.Thinking, plain.Thinking)
	}

	r := textRequest("deepseek-flash-thinking")
	r.Tools = []llmgateway.ToolDefinition{{Name: "search", Description: "d", SchemaVersion: 1, InputSchema: json.RawMessage(okToolSchema)}}
	r.ToolChoice = "required"
	if err := llmgateway.CheckCapability(r, thinking); !errors.Is(err, llmgateway.ErrCapability) {
		t.Fatalf("thinking mode must refuse an explicit tool choice: %v", err)
	}
	r.ModelKey = "deepseek-flash"
	if err := llmgateway.CheckCapability(r, plain); err != nil {
		t.Fatalf("the non-thinking deployment must accept it: %v", err)
	}
}

func TestDefaultCatalogStaysListedWithoutCredential(t *testing.T) {
	catalog, err := llmgateway.DefaultCatalog(func(string) (string, bool) { return "", false })
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range catalog.List() {
		if entry.Available || entry.UnavailableWhy == "" {
			t.Fatalf("without a credential a model must be visibly unavailable: %+v", entry)
		}
	}
}
