package llmgateway

// DefaultCatalogVersion changes whenever a deployment entry, capability or
// price recipe below changes. It is persisted with every request, so an edit
// never rewrites an in-flight call.
const DefaultCatalogVersion = "deepseek-2026-09-11"

// priceSourceDeepSeek records where the rates came from and when, because a
// price recipe without a provenance cannot be reconciled against an invoice.
const priceSourceDeepSeek = "https://api-docs.deepseek.com/quick_start/pricing (read 2026-09-11)"

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
	flash := ModelConfig{
		ModelKey:       "deepseek-flash",
		DisplayName:    "DeepSeek Flash",
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
			// Vision is published as supported, but no measured byte-to-token
			// mapping exists yet, so a hard budget could not bound an image
			// request. It stays off until that mapping is verified.
			ImageInput:      false,
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
	flashThinking.ModelKey = "deepseek-flash-thinking"
	flashThinking.DisplayName = "DeepSeek Flash（思考模式）"
	flashThinking.DeploymentKey = "deepseek/api/flash-thinking"
	flashThinking.Thinking = "enabled"
	flashThinking.Capability.ToolChoice = false

	pro := ModelConfig{
		ModelKey:         "deepseek-v4-pro",
		DisplayName:      "DeepSeek V4 Pro",
		Provider:         ProviderOpenAICompatible,
		VendorKey:        "deepseek",
		DeploymentKey:    "deepseek/api/v4-pro",
		BaseURL:          "https://api.deepseek.com",
		CredentialEnv:    "DEEPSEEK_API_KEY",
		RequestModelID:   "deepseek-v4-pro",
		Thinking:         "disabled",
		AcceptedModelIDs: []string{"deepseek-v4-pro"},
		Capability: Capability{
			// The published matrix lists vision as unsupported for V4 Pro.
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

	return NewCatalog(DefaultCatalogVersion, "USD", []ModelConfig{flash, flashThinking, pro})
}
