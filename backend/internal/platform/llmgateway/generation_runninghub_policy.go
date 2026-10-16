package llmgateway

import (
	"encoding/json"
	"fmt"
)

const RunningHubImageAdapterVersion = "runninghub-images-1"

func RunningHubImageModels() []string {
	return []string{"gpt-image-2", "nano-banana-pro", "nano-banana-2", "nano-banana-2.1"}
}
func RunningHubImageRatios(model string) []string {
	switch model {
	case "gpt-image-2":
		return []string{"1:1", "1:2", "2:1", "1:3", "3:1", "2:3", "3:2", "3:4", "4:3", "4:5", "5:4", "9:16", "16:9", "21:9", "9:21"}
	case "nano-banana-2", "nano-banana-2.1":
		return []string{"1:1", "16:9", "9:16", "4:3", "3:4", "3:2", "2:3", "5:4", "4:5", "21:9", "1:4", "4:1", "1:8", "8:1"}
	default:
		return []string{"1:1", "3:2", "2:3", "3:4", "4:3", "4:5", "5:4", "9:16", "16:9", "21:9"}
	}
}

// RunningHubImagePolicies 只描述能力；供应商计费暂不启用，不声明零价或实际用量。
func RunningHubImagePolicies(currency string) ([]ImagePolicy, error) {
	var out []ImagePolicy
	for _, model := range RunningHubImageModels() {
		p, err := runningHubImagePolicy(currency, model)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}
func runningHubImagePolicy(currency, model string) (ImagePolicy, error) {
	props := map[string]any{
		"aspect_ratio": map[string]any{"type": "string", "enum": RunningHubImageRatios(model)},
		"resolution":   map[string]any{"type": "string", "enum": []string{"1K", "2K", "4K"}},
		"count":        map[string]any{"type": "integer", "enum": []int{1}},
		"background":   map[string]any{"type": "string", "enum": []string{"auto"}},
	}
	defaults := map[string]json.RawMessage{"aspect_ratio": json.RawMessage(`"1:1"`), "resolution": json.RawMessage(`"2K"`), "count": json.RawMessage(`1`), "background": json.RawMessage(`"auto"`)}
	params := map[string]GenerationFactType{"aspect_ratio": {Type: "string"}, "resolution": {Type: "string"}, "count": {Type: "number", Unit: "count"}, "background": {Type: "string"}}
	if model == "gpt-image-2" {
		props["quality"] = map[string]any{"type": "string", "enum": []string{"low", "medium", "high"}}
		defaults["quality"] = json.RawMessage(`"medium"`)
		params["quality"] = GenerationFactType{Type: "string"}
	}
	schema, err := imageGenerationInputSchema(map[string]any{"type": "object", "additionalProperties": false, "properties": props})
	if err != nil {
		return ImagePolicy{}, err
	}
	maxRefs := int64(9) // 与现有画布上限一致，严于官方GPT/Pro的10和Nano 2/2.1的14。
	maxReferenceBytes := int64(25 << 20)
	if model == "nano-banana-2.1" {
		maxReferenceBytes = 10 << 20 // 2.1 SKU声明单张参考图上限10MB。
	}
	formats := []json.RawMessage{json.RawMessage(`"image/jpeg"`), json.RawMessage(`"image/png"`)}
	if model == "gpt-image-2" {
		formats = append(formats, json.RawMessage(`"image/webp"`))
	}
	mode := GenerationPolicyMode{Definition: GenerationModeDefinition{Key: "image", Version: "1", Kind: GenerationImage, InputSchema: schema}, Parameters: params, Defaults: defaults,
		Pricing: []GenerationPriceComponent{}, Output: GenerationOutputPolicy{Kind: GenerationImage, MinOutputs: 1, MaxOutputs: 1, MIMEs: []string{"image/png", "image/jpeg", "image/webp"}, MaxBytes: 25 << 20},
		Rules: []GenerationConstraint{
			{ID: "reference_count", Assert: GenerationCondition{Op: "lte", Ref: GenerationValueRef{Source: "references", Aggregate: "count", Kind: GenerationImage, Role: "reference", Unit: "count"}, Values: []json.RawMessage{json.RawMessage(fmt.Sprint(maxRefs))}}},
			{ID: "reference_format", Assert: GenerationCondition{Op: "in", Ref: GenerationValueRef{Source: "references", Aggregate: "each", Field: "mime", Kind: GenerationImage, Role: "reference"}, Values: formats}},
			{ID: "reference_bytes", Assert: GenerationCondition{Op: "lte", Ref: GenerationValueRef{Source: "references", Aggregate: "each", Field: "bytes", Kind: GenerationImage, Role: "reference", Unit: "byte"}, Values: []json.RawMessage{json.RawMessage(fmt.Sprint(maxReferenceBytes))}}},
		},
	}
	def := GenerationPolicyDefinition{EngineVersion: GenerationPolicyEngineVersion, Version: "runninghub-capabilities-1", Profile: GenerationProfileDefinition{ModelKey: model, ModelRevision: "2026-10-15", Version: "1"}, DeploymentID: "runninghub-images-" + model, AdapterVersion: RunningHubImageAdapterVersion, Currency: currency, Billing: GenerationBillingDisabled, Modes: []GenerationPolicyMode{mode}}
	target := GenerationPolicyTarget{ModelKey: model, ModelRevision: def.Profile.ModelRevision, DeploymentID: def.DeploymentID, AdapterVersion: def.AdapterVersion, Modes: []string{"image"}, Billing: GenerationBillingDisabled}
	raw, err := json.Marshal(def)
	if err != nil {
		return ImagePolicy{}, err
	}
	compiled, err := CompileGenerationPolicy(raw, target)
	if err != nil {
		return ImagePolicy{}, err
	}
	return ImagePolicy{Definition: compiled.Definition(), Target: target, Policy: compiled}, nil
}
