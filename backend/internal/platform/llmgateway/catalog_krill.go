package llmgateway

// Krill's channel tutorial and signed-in pricing table, read 2026-10-10.
// The public landing page quotes different rates; use the channel table.
const priceSourceKrill = "https://www.krill-code.com/app/pricing (Openai 官渠, Standard, read 2026-10-10; conservative undiscounted upper bound including long context and cache writes)"

// krillDeployments returns Krill's channel deployment keyed by the catalog model
// key it serves. The key is named rather than inferred from the vendor's
// request id, so renaming either side cannot silently drop Krill from a route.
func krillDeployments(credential func(string) (string, bool)) map[string]Deployment {
	_, enabled := credential("KRILL_AI_API_KEY")
	disabled := ""
	if !enabled {
		// Krill heads the route, so this is what the model list shows when
		// neither vendor is configured; either key makes the model usable.
		disabled = "未配置 KRILL_AI_API_KEY 或 AICODEWITH_API_KEY"
	}
	models := []struct {
		modelKey string
		id       string
		rate     Rate
	}{
		// Use the >272K tier throughout. Uncached input includes cache writes,
		// so reserve at the larger cache-write rate, without the 0.15 discount.
		{modelKey: "gpt-6.1-sol", id: "gpt-6.1-sol", rate: Rate{InputCached: 400_000, InputUncached: 5_000_000, Output: 15_000_000}},
		{modelKey: "gpt-6-sol", id: "gpt-6-sol", rate: Rate{InputCached: 800_000, InputUncached: 5_000_000, Output: 15_000_000}},
		{modelKey: "gpt-6-astra", id: "gpt-6-astra", rate: Rate{InputCached: 2_000_000, InputUncached: 12_500_000, Output: 50_000_000}},
	}
	deployments := make(map[string]Deployment, len(models))
	for _, m := range models {
		deployments[m.modelKey] = Deployment{
			Provider: ProviderOpenAIResponses, VendorKey: "krill-ai",
			DeploymentKey: "krill-ai/codex/" + m.id,
			BaseURL:       "https://api.krill-code.net/codex/v1", CredentialEnv: "KRILL_AI_API_KEY",
			RequestModelID: m.id, AcceptedModelIDs: []string{m.id}, ReasoningEffort: "medium",
			Capability: Capability{
				ImageInput: true, ToolCalling: true, ToolChoice: true, JSONObject: true,
				ContextTokens: 1_000_000, MaxOutputTokens: 128_000,
			},
			Price: PriceSchedule{
				Version: "krill-" + m.id + "-upper-bound-2026-10-10", Currency: "USD",
				Source: priceSourceKrill, Peak: m.rate, OffPeak: m.rate,
			},
			// No documented evidence of pre-processing refusal, query or cancel.
			QueryCapability: "none", CancelCapability: "none",
			Enabled: enabled, DisabledReason: disabled,
		}
	}
	return deployments
}
