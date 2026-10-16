package llmgateway

// priceSourceFourRouter records the bound 4Router is reserved at. Its groups
// bill the official rates times a multiplier shown only in the console, so the
// official rate is the bound that holds whichever group the key is bound to.
const priceSourceFourRouter = "official Anthropic rates as of 2026-10-16 (see priceSourceAnthropic); https://docs.4router.net/guides/model-groups/ says group multipliers are shown in the console"

// fourRouterClaudeDeployments returns 4Router's Claude deployments keyed by the
// catalog model key each serves. 4Router heads these routes because AiCodeWith
// was measured too slow for agent work (owner 2026-10-16); AiCodeWith stays the
// fallback.
//
// 4Router is a relay speaking the Anthropic Messages protocol at its root URL
// (https://docs.4router.net/guides/manual-config/). Which Claude models a key
// reaches depends on the group the key is bound to in the console; its docs
// list only the 5 line, but /v1/models listed both 5.5 ids for our key.
//
// Measured 2026-10-16 (TestLiveClaude*, 4Router alone in its catalog):
//   - Official response shapes throughout: msg_011C… ids, cache read/creation
//     counts, output_tokens_details.thinking_tokens, ping events in the stream,
//     omitted thinking text with signatures. No identity prompt was injected.
//   - Signed thinking handed back within a tool loop is accepted; a forced
//     tool_choice and temperature are refused with 400 exactly as officially.
//   - Automatic prompt caching reads back on the next call.
//   - The same acceptance ran in 65 s against AiCodeWith's 136 s (the tool loop
//     in 47 s against 106 s); TLS setup 0.1 s, output 80-99 tokens a second.
//   - Thinking is truly adaptive here: an easy step, or a system prompt asking
//     for brevity, draws no thinking at all. AiCodeWith's channel always
//     thought, so a route switch can change how much a step thinks.
func fourRouterClaudeDeployments(credential func(string) (string, bool)) map[string]Deployment {
	_, enabled := credential("FOUR_ROUTER_API_KEY")
	disabled := ""
	if !enabled {
		// 4Router heads the route, so this is what the model list shows when
		// neither vendor is configured; either key makes the model usable.
		disabled = "未配置 FOUR_ROUTER_API_KEY 或 AICODEWITH_API_KEY"
	}
	models := []struct {
		id   string
		rate Rate
	}{
		// The uncached rate is the five-minute cache-write rate, as for
		// AiCodeWith: every request is cached and writes book as uncached.
		{id: "claude-opus-5-5", rate: Rate{InputCached: 200_000, InputUncached: 5_000_000, Output: 20_000_000}},
		{id: "claude-sonnet-5-5", rate: Rate{InputCached: 200_000, InputUncached: 2_500_000, Output: 10_000_000}},
	}
	deployments := make(map[string]Deployment, len(models))
	for _, m := range models {
		deployments[m.id] = Deployment{
			Provider: ProviderAnthropicMessage, VendorKey: "fourrouter",
			DeploymentKey: "4router/api/" + m.id,
			BaseURL:       "https://4router.net", CredentialEnv: "FOUR_ROUTER_API_KEY",
			RequestModelID: m.id, AcceptedModelIDs: []string{m.id}, ReasoningEffort: "medium",
			Capability: Capability{
				ImageInput: true, ToolCalling: true,
				// A forced tool_choice is a 400 on the 5.5 line, Messages has no
				// json_object mode and these models reject temperature.
				ContextTokens: 1_000_000, MaxOutputTokens: 128_000,
			},
			Price: PriceSchedule{
				Version: "4router-" + m.id + "-upper-bound-2026-10-16", Currency: "USD",
				Source: priceSourceFourRouter, Peak: m.rate, OffPeak: m.rate,
			},
			// No documented evidence of pre-processing refusal, query or cancel.
			QueryCapability: "none", CancelCapability: "none",
			Enabled: enabled, DisabledReason: disabled,
		}
	}
	return deployments
}
