package llmgateway_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
)

func TestFourRouterLeadsTheClaudeRoutes(t *testing.T) {
	for _, tc := range []struct {
		name               string
		fourRouter, aicode bool
		vendor, baseURL    string
	}{
		{name: "neither"},
		{name: "existing only", aicode: true, vendor: "aicodewith", baseURL: "https://api.aicodewith.ai"},
		{name: "4router only", fourRouter: true, vendor: "fourrouter", baseURL: "https://4router.net"},
		{name: "4router preferred", fourRouter: true, aicode: true, vendor: "fourrouter", baseURL: "https://4router.net"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			catalog, err := llmgateway.DefaultCatalog(func(env string) (string, bool) {
				return "test-key", env == "FOUR_ROUTER_API_KEY" && tc.fourRouter || env == "AICODEWITH_API_KEY" && tc.aicode
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(catalog.List()) != 8 || !slices.Contains(catalog.Vendors(), "fourrouter") {
				t.Fatal("model identities or vendors changed")
			}
			for _, key := range []string{"claude-opus-5-5", "claude-sonnet-5-5"} {
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
				if model.VendorKey != tc.vendor || model.BaseURL != tc.baseURL || model.RequestModelID != key ||
					model.Provider != llmgateway.ProviderAnthropicMessage || !model.HandsBackReasoning() {
					t.Fatalf("wrong route: %+v", model.Deployment)
				}
				// Both vendors serve the same model: the same refusals and the
				// same reservation bound, whichever one answers.
				if model.Capability.ToolChoice || model.Capability.Temperature || model.Capability.JSONObject ||
					model.Price.UpperBound().InputUncached*4 != model.Price.UpperBound().Output {
					t.Fatalf("%s: %+v", key, model.Deployment)
				}
				if tc.aicode {
					old, err := catalog.Deployment(key, "aicodewith/api/"+key)
					if err != nil || old.VendorKey != "aicodewith" {
						t.Fatalf("old request cannot resume: %+v %v", old, err)
					}
				}
			}
		})
	}
}
