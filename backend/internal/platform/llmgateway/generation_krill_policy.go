package llmgateway

import "encoding/json"

const KrillImageAdapterVersion = "krill-images-1"

// KrillImagePolicies 复用同型号的产品能力限制，计费独立；Krill 的 token 账单未接入，
// 不能沿用 AiCodeWith 按张计价。规划复杂度尚无已核实 API 字段，暂不接受。
func KrillImagePolicies(currency string) ([]ImagePolicy, error) {
	var policies []ImagePolicy
	for _, model := range []string{"gpt-image-2.6-sunburst", "gpt-image-2.6-flare"} {
		def, target, err := aicodewithImagePolicy(model, currency)
		if err != nil {
			return nil, err
		}
		def.Version = "krill-images-2026-11-09"
		def.DeploymentID = "krill-images-" + model
		def.AdapterVersion = KrillImageAdapterVersion
		def.Billing = GenerationBillingDisabled
		def.Modes[0].Pricing = []GenerationPriceComponent{}
		target.DeploymentID = def.DeploymentID
		target.AdapterVersion = def.AdapterVersion
		target.Billing = GenerationBillingDisabled
		target.Usage = nil
		raw, err := json.Marshal(def)
		if err != nil {
			return nil, err
		}
		policy, err := CompileGenerationPolicy(raw, target)
		if err != nil {
			return nil, err
		}
		policies = append(policies, ImagePolicy{Definition: policy.Definition(), Target: target, Policy: policy})
	}
	return policies, nil
}
