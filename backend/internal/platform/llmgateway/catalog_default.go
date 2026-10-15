package llmgateway

// DefaultCatalogVersion changes whenever a deployment entry, capability or
// price recipe below changes. It is persisted with every request, so an edit
// never rewrites an in-flight call.
const DefaultCatalogVersion = "deepseek-aicodewith-krill-2026-10-10"

// defaultContextWindowTokens is the input window every built-in model uses.
// The providers read far more, but each turn resends the whole window and the
// first turn's hold is priced from it (see creativeagent's request ceiling),
// so the window is a cost decision, not a capability (owner 2026-10-14; raised
// from 128k to 256k by owner 2026-10-15).
const defaultContextWindowTokens = 256_000

// priceSourceDeepSeek records where the rates came from and when, because a
// price recipe without a provenance cannot be reconciled against an invoice.
const priceSourceDeepSeek = "https://api-docs.deepseek.com/quick_start/pricing (read 2026-09-11)"

// priceSourceOpenAI records the official rates AiCodeWith bills against. It
// charges them times a channel discount (1–1.1 of 10 on the subscription
// channels), and falls back to a dearer channel when a cheap one is throttled,
// so the official rate is the only bound that always holds.
const priceSourceOpenAI = "https://artificialanalysis.ai/models (official OpenAI rates, read 2026-10-09); https://docs.aicodewith.ai/zh/docs/subscription-package-selection"

// deepSeekPeakWindows are the published UTC peak hours, Monday through Friday.
// Off-peak is half of peak, so every reservation is bounded by the peak rate.
func deepSeekPeakWindows() []PeakWindow {
	weekdays := []int{1, 2, 3, 4, 5}
	return []PeakWindow{
		{Weekdays: weekdays, StartHour: 1, EndHour: 4},
		{Weekdays: weekdays, StartHour: 6, EndHour: 10},
	}
}

// DefaultCatalog is the built-in deployment directory. Prices are integer
// micros of one USD per one million tokens. A model stays listed but disabled
// with a visible reason when its credential is not configured, so the product
// never advertises a capability the deployment cannot actually serve.
func DefaultCatalog(credential func(string) (string, bool)) (*Catalog, error) {
	if credential == nil {
		credential = osCredential
	}
	_, hasDeepSeek := credential("DEEPSEEK_API_KEY")
	disabled := ""
	if !hasDeepSeek {
		disabled = "未配置 DEEPSEEK_API_KEY"
	}

	// Two entries share one vendor model because the reasoning mode is fixed
	// per deployment: it changes both the output budget and whether an
	// explicit tool_choice is accepted at all.
	flash := Deployment{
		Provider:       ProviderOpenAICompatible,
		VendorKey:      "deepseek",
		DeploymentKey:  "deepseek/api/flash",
		BaseURL:        "https://api.deepseek.com",
		CredentialEnv:  "DEEPSEEK_API_KEY",
		RequestModelID: "deepseek-flash",
		// Non-thinking: the whole output budget goes to the answer and an
		// explicit tool_choice is honoured.
		Thinking: "disabled",
		// The retired aliases still answer, and are billed at the Flash price.
		AcceptedModelIDs: []string{"deepseek-flash", "deepseek-v4-flash", "deepseek-v4-flash-vision-exp"},
		Capability: Capability{
			// Measured 2026-10-09 against the real API (non-thinking and
			// thinking): an image costs about 184 prompt tokens at 64–400 px
			// and levels off near 994 from 1600 px up (3000 px costs the same),
			// so the 4096-token bound per image in EstimateInputTokens holds
			// with room to spare.
			ImageInput:      true,
			ToolCalling:     true,
			ToolChoice:      true,
			JSONObject:      true,
			Temperature:     true,
			ContextTokens:   1_000_000,
			MaxOutputTokens: 384_000,
		},
		Price: PriceSchedule{
			Version:     "deepseek-flash-2026-09-11",
			Currency:    "USD",
			Source:      priceSourceDeepSeek,
			Peak:        Rate{InputCached: 6_000, InputUncached: 300_000, Output: 1_200_000},
			OffPeak:     Rate{InputCached: 3_000, InputUncached: 150_000, Output: 600_000},
			PeakWindows: deepSeekPeakWindows(),
		},
		// 429 is documented as "Rate Limit Reached: requests sent too quickly",
		// i.e. refused before processing. No other status is treated as free.
		// Evidence: the vendor's own error table (priceSourceDeepSeek's site,
		// read 2026-09-11), not a measured 429 — the live acceptance run never
		// reached the rate limit. A wrong entry here would license one duplicate
		// charge, so nothing is added without a documented pre-processing
		// refusal.
		UnacceptedStatuses: []int{429},
		LimitKey:           "deepseek/api",
		// Our own conservative admission, well below the vendor's concurrency
		// allowance: this platform is one small deployment, not a fleet.
		Concurrency:      4,
		RateLimitPerMin:  60,
		QueryCapability:  "none",
		CancelCapability: "none",
		Enabled:          hasDeepSeek,
		DisabledReason:   disabled,
	}

	// The same deployment in thinking mode. Verified 2026-09-11 against the
	// real API: it answers 400 "Thinking mode does not support this
	// tool_choice", so the capability is refused before dispatch instead.
	flashThinking := flash
	flashThinking.DeploymentKey = "deepseek/api/flash-thinking"
	flashThinking.Thinking = "enabled"
	flashThinking.Capability.ToolChoice = false

	pro := Deployment{
		Provider:         ProviderOpenAICompatible,
		VendorKey:        "deepseek",
		DeploymentKey:    "deepseek/api/v4-pro",
		BaseURL:          "https://api.deepseek.com",
		CredentialEnv:    "DEEPSEEK_API_KEY",
		RequestModelID:   "deepseek-v4-pro",
		Thinking:         "disabled",
		AcceptedModelIDs: []string{"deepseek-v4-pro"},
		Capability: Capability{
			// The published matrix lists vision as unsupported for V4 Pro, and the
			// API proves it the dangerous way: a 1600 px image added 5 prompt
			// tokens and the answer still described a picture it never saw
			// (measured 2026-10-09). It must never receive one.
			ImageInput:      false,
			ToolCalling:     true,
			ToolChoice:      true,
			JSONObject:      true,
			Temperature:     true,
			ContextTokens:   1_000_000,
			MaxOutputTokens: 384_000,
		},
		Price: PriceSchedule{
			Version:     "deepseek-v4-pro-2026-09-11",
			Currency:    "USD",
			Source:      priceSourceDeepSeek,
			Peak:        Rate{InputCached: 44_000, InputUncached: 1_320_000, Output: 3_960_000},
			OffPeak:     Rate{InputCached: 22_000, InputUncached: 660_000, Output: 1_980_000},
			PeakWindows: deepSeekPeakWindows(),
		},
		UnacceptedStatuses: []int{429},
		LimitKey:           "deepseek/api",
		Concurrency:        4,
		RateLimitPerMin:    60,
		QueryCapability:    "none",
		CancelCapability:   "none",
		Enabled:            hasDeepSeek,
		DisabledReason:     disabled,
	}

	// Each model lists its deployments in priority order. A second vendor for
	// the same model is one more deployment appended to its list.
	models := []Model{
		{ModelKey: "deepseek-flash", DisplayName: "DeepSeek Flash", Capability: flash.Capability, ContextWindowTokens: defaultContextWindowTokens, Deployments: []string{flash.DeploymentKey}},
		{ModelKey: "deepseek-flash-thinking", DisplayName: "DeepSeek Flash（思考模式）", Capability: flashThinking.Capability, ContextWindowTokens: defaultContextWindowTokens, Deployments: []string{flashThinking.DeploymentKey}},
		{ModelKey: "deepseek-v4-pro", DisplayName: "DeepSeek V4 Pro", Capability: pro.Capability, ContextWindowTokens: defaultContextWindowTokens, Deployments: []string{pro.DeploymentKey}},
	}
	deployments := []Deployment{flash, flashThinking, pro}
	gptModels, gptDeployments := aicodewithModels(credential)
	krill := krillDeployments(credential)
	for i := range gptModels {
		if d, ok := krill[gptModels[i].ModelKey]; ok {
			// Krill leads when configured; AiCodeWith stays the fallback.
			gptModels[i].Deployments = append([]string{d.DeploymentKey}, gptModels[i].Deployments...)
			gptDeployments = append(gptDeployments, d)
		}
	}
	return NewRoutedCatalog(DefaultCatalogVersion, "USD", append(models, gptModels...), append(deployments, gptDeployments...))
}

// aicodewithModels are OpenAI's GPT-6 line, reached through the AiCodeWith
// relay. They speak the Responses protocol so their reasoning can come back on
// later turns; over chat completions it would be dropped between tool calls.
//
// Measured 2026-10-09 against the relay (TestLiveAICodeWith*): each model
// answers under exactly its own id; a 400 px image costs about 200 input
// tokens and is described correctly; sealed reasoning handed back on the next
// turn is accepted, three loops in a row; a turn takes 3–8 s at medium effort,
// well inside the two-minute transport bound. At medium a simple step often
// carries no reasoning at all, which is the model's choice, not a lost item.
func aicodewithModels(credential func(string) (string, bool)) ([]Model, []Deployment) {
	_, enabled := credential("AICODEWITH_API_KEY")
	disabled := ""
	if !enabled {
		disabled = "未配置 AICODEWITH_API_KEY"
	}
	deployment := func(id string, rate Rate) Deployment {
		return Deployment{
			Provider:         ProviderOpenAIResponses,
			VendorKey:        "aicodewith",
			DeploymentKey:    "aicodewith/api/" + id,
			BaseURL:          "https://api.aicodewith.ai/v1",
			CredentialEnv:    "AICODEWITH_API_KEY",
			RequestModelID:   id,
			ReasoningEffort:  "medium",
			ReasoningContext: "all_turns",
			AcceptedModelIDs: []string{id},
			Capability: Capability{
				ImageInput:  true,
				ToolCalling: true,
				ToolChoice:  true,
				JSONObject:  true,
				// Reasoning models fix their own sampling.
				Temperature:     false,
				ContextTokens:   1_000_000,
				MaxOutputTokens: 128_000,
			},
			Price: PriceSchedule{
				Version:  id + "-2026-10-09",
				Currency: "USD",
				Source:   priceSourceOpenAI,
				Peak:     rate,
				OffPeak:  rate,
			},
			// The relay documents no status as refused before processing.
			LimitKey:         "aicodewith/api",
			Concurrency:      4,
			RateLimitPerMin:  60,
			QueryCapability:  "none",
			CancelCapability: "none",
			Enabled:          enabled,
			DisabledReason:   disabled,
		}
	}
	deployments := []Deployment{
		// The relay's everyday default: near Astra's capability at Sol's price.
		deployment("gpt-6.1-sol", Rate{InputCached: 100_000, InputUncached: 2_000_000, Output: 10_000_000}),
		deployment("gpt-6-sol", Rate{InputCached: 200_000, InputUncached: 2_000_000, Output: 10_000_000}),
		deployment("gpt-6-astra", Rate{InputCached: 1_000_000, InputUncached: 10_000_000, Output: 50_000_000}),
	}
	names := []string{"GPT-6.1 Sol", "GPT-6 Sol", "GPT-6 Astra"}
	models := make([]Model, len(deployments))
	for i, d := range deployments {
		models[i] = Model{ModelKey: d.RequestModelID, DisplayName: names[i], Capability: d.Capability, ContextWindowTokens: defaultContextWindowTokens, Deployments: []string{d.DeploymentKey}}
	}
	return models, deployments
}
