package llmgateway_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
)

func TestKrillRoutingPreservesExistingDeployments(t *testing.T) {
	for _, tc := range []struct {
		name          string
		krill, aicode bool
		vendor        string
	}{
		{name: "neither"},
		{name: "existing only", aicode: true, vendor: "aicodewith"},
		{name: "krill only", krill: true, vendor: "krill-ai"},
		{name: "krill preferred", krill: true, aicode: true, vendor: "krill-ai"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			catalog, err := llmgateway.DefaultCatalog(func(env string) (string, bool) {
				return "test-key", env == "KRILL_AI_API_KEY" && tc.krill || env == "AICODEWITH_API_KEY" && tc.aicode
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(catalog.List()) != 8 || !slices.Contains(catalog.Vendors(), "krill-ai") {
				t.Fatal("model identities or vendors changed")
			}
			for _, key := range []string{"gpt-6.1-sol", "gpt-6-sol", "gpt-6-astra"} {
				model, err := catalog.Model(key)
				if tc.vendor == "" {
					if !errors.Is(err, llmgateway.ErrUnavailable) {
						t.Fatalf("%s: %v", key, err)
					}
					continue
				}
				if err != nil {
					t.Fatal(err)
				}
				if model.VendorKey != tc.vendor || model.RequestModelID != key {
					t.Fatalf("wrong route: %+v", model)
				}
				if tc.aicode {
					old, err := catalog.Deployment(key, "aicodewith/api/"+key)
					if err != nil || old.VendorKey != "aicodewith" || old.ReasoningContext != "all_turns" {
						t.Fatalf("old request cannot resume: %+v %v", old, err)
					}
				}
			}
		})
	}
}

func TestKrillResponsesUseChannelEndpointAndOmitRelayExtension(t *testing.T) {
	catalog, err := llmgateway.DefaultCatalog(func(env string) (string, bool) { return "test-key", env == "KRILL_AI_API_KEY" })
	if err != nil {
		t.Fatal(err)
	}
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "json", true: "stream"}[stream], func(t *testing.T) {
			fixture := newProtocolFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/codex/v1/responses" || r.Header.Get("Authorization") != "Bearer secret-value" {
					t.Errorf("wrong endpoint or authentication")
				}
				body := strings.ReplaceAll(responsesToolBody, "vendor-model", "gpt-6.1-sol")
				if stream {
					w.Header().Set("Content-Type", "text/event-stream")
					body = strings.ReplaceAll(responsesToolStream, "vendor-model", "gpt-6.1-sol")
				}
				_, _ = io.WriteString(w, body)
			})
			model, err := catalog.Model("gpt-6.1-sol")
			if err != nil {
				t.Fatal(err)
			}
			if model.BaseURL != "https://api.krill-code.net/codex/v1" {
				t.Fatal(model.BaseURL)
			}
			model.BaseURL = fixture.server.URL + "/codex/v1"
			chat := toolRequest()
			chat.ModelKey = model.ModelKey
			// Existing conversation history must not send AiCodeWith's sealed
			// reasoning to Krill, while retaining text and tool results.
			chat.Messages = append(chat.Messages, llmgateway.Message{Role: llmgateway.RoleAssistant,
				Reasoning: `{"format":"openai_responses","deployment":"aicodewith/api/gpt-6.1-sol","model":"gpt-6.1-sol","items":[` + responsesReasoningItem + `]}`,
				Blocks:    []llmgateway.Block{{Kind: llmgateway.BlockText, Text: "之前的回答"}},
			})
			result, err := invokeResponses(t, model, chat, stream, nil)
			if err != nil {
				t.Fatal(err)
			}
			if result.Model != model.ModelKey || len(result.ToolCalls) != 1 || result.Usage.InputCachedTokens == nil || *result.Usage.InputCachedTokens != 64 {
				t.Fatalf("bad result: %+v", result)
			}
			var body struct {
				Model     string            `json:"model"`
				Store     bool              `json:"store"`
				Reasoning map[string]string `json:"reasoning"`
				Include   []string          `json:"include"`
			}
			raw := fixture.body.Load().(string)
			if err := json.Unmarshal([]byte(raw), &body); err != nil {
				t.Fatal(err)
			}
			if body.Model != model.ModelKey || body.Store || body.Reasoning["effort"] != "medium" || !slices.Contains(body.Include, "reasoning.encrypted_content") {
				t.Fatalf("bad request: %s", raw)
			}
			if _, exists := body.Reasoning["context"]; exists {
				t.Fatal("relay extension sent to Krill")
			}
			if strings.Contains(raw, "gAAAA-sealed") || !strings.Contains(raw, "之前的回答") {
				t.Fatal("history migration lost text or leaked sealed reasoning")
			}
			if fixture.requests.Load() != 1 {
				t.Fatal("unexpected retry")
			}
		})
	}
}
