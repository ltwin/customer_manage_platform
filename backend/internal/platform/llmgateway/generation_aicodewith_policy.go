package llmgateway

import (
	"encoding/json"
	"fmt"
)

// AICodeWithImagePolicyVersion 是内置生图规则的版本；改价格、上界或参数须升级。
const AICodeWithImagePolicyVersion = "aicodewith-images-2026-10-09"

// AICodeWithImageMode 是三个GPT Image模型唯一的模式：带图片参考即为图生图。
const AICodeWithImageMode = "image"

// 参考图上限是产品决策（owner 2026-10-09），中转本身允许16张；单张10MB与中转官方页面一致。
const (
	aicodewithMaxReferenceImages = 9
	aicodewithMaxReferenceBytes  = 10 << 20
	aicodewithMaxImages          = 4
)

// 用量硬上界由实测给出（2026-10-09）：单张输出最多实测8196 token（21:9 4K高画质），
// 中转对自动比例按约14300 token预留，取16000；参考图无论多大都约1521 token，取2000；
// 提示词限16000字符，按每字符最多3 token计。超出上界的结算进入争议，不会按零记账。
const (
	aicodewithOutputTokensPerImage = 16000
	aicodewithInputTokensPerImage  = 2000
	aicodewithMaxPromptChars       = 16000
	aicodewithMaxTextTokens        = 3 * aicodewithMaxPromptChars
)

// 单价按中转实测费率（USD微单位/token）：图像输出约27.1、图像输入约7.2；
// 文字输入无法单独测准，按OpenAI公开的5取。结算附中转回报金额作对账证据。
var (
	aicodewithTextRate   = GenerationRatio{Numerator: 5, Denominator: 1}
	aicodewithInputRate  = GenerationRatio{Numerator: 73, Denominator: 10}
	aicodewithOutputRate = GenerationRatio{Numerator: 272, Denominator: 10}
)

// AICodeWithImageRatios 是中转接受的全部比例；auto由模型决定画幅。
var AICodeWithImageRatios = []string{"auto", "1:1", "1:2", "2:1", "1:3", "3:1", "2:3", "3:2", "3:4", "4:3", "4:5", "5:4", "9:16", "16:9", "21:9", "9:21"}

// AICodeWithImagePolicy 是一个模型的已编译规则及其Adapter注册信息。
type AICodeWithImagePolicy struct {
	Definition GenerationPolicyDefinition
	Target     GenerationPolicyTarget
	Policy     *CompiledGenerationPolicy
}

// AICodeWithImageDeploymentID 是模型在当前账号控制面中的部署身份。
func AICodeWithImageDeploymentID(model string) string { return "aicodewith-images-" + model }

// AICodeWithImagePolicies 编译三个模型的内置规则。currency 必须是网关账本币种。
func AICodeWithImagePolicies(currency string) ([]AICodeWithImagePolicy, error) {
	if currency != "USD" {
		return nil, fmt.Errorf("%w: aicodewith image prices are in USD", ErrValidation)
	}
	out := make([]AICodeWithImagePolicy, 0, len(aicodewithImageModels))
	for _, model := range aicodewithImageModels {
		d, target, err := aicodewithImagePolicy(model, currency)
		if err != nil {
			return nil, err
		}
		raw, err := json.Marshal(d)
		if err != nil {
			return nil, err
		}
		compiled, err := CompileGenerationPolicy(raw, target)
		if err != nil {
			return nil, fmt.Errorf("compile %s policy: %w", model, err)
		}
		out = append(out, AICodeWithImagePolicy{Definition: compiled.Definition(), Target: target, Policy: compiled})
	}
	return out, nil
}

func aicodewithImagePolicy(model, currency string) (GenerationPolicyDefinition, GenerationPolicyTarget, error) {
	parameters := map[string]any{
		"type": "object", "additionalProperties": false,
		"properties": map[string]any{
			"aspect_ratio": map[string]any{"type": "string", "enum": AICodeWithImageRatios},
			"resolution":   map[string]any{"type": "string", "enum": []string{"1K", "2K", "4K"}},
			"quality":      map[string]any{"type": "string", "enum": []string{"low", "medium", "high"}},
			"count":        map[string]any{"type": "integer", "enum": []int{1, 2, 4}},
			"background":   map[string]any{"type": "string", "enum": []string{"auto", "opaque", "transparent"}},
		},
	}
	reference := map[string]any{
		"type": "object", "additionalProperties": false,
		"required": []string{"role", "revision_id", "digest", "kind", "mime", "byte_size"},
		"properties": map[string]any{
			"role": map[string]any{"type": "string", "enum": []string{"reference"}},
			// 文字参考通过句内引用编入提示词；视频和音频这些模型读不了。
			"kind":        map[string]any{"type": "string", "enum": []string{"image", "text"}},
			"revision_id": map[string]any{"type": "string"},
			"digest":      map[string]any{"type": "string"},
			"mime":        map[string]any{"type": "string"},
			"byte_size":   map[string]any{"type": "integer"},
		},
	}
	schema, err := json.Marshal(map[string]any{
		"type": "object", "additionalProperties": false,
		"required": []string{"prompt", "parameters"},
		"properties": map[string]any{
			"prompt":     map[string]any{"type": "string", "minLength": 1, "maxLength": aicodewithMaxPromptChars},
			"parameters": parameters,
			"references": map[string]any{"type": "array", "maxItems": 50, "items": reference},
		},
	})
	if err != nil {
		return GenerationPolicyDefinition{}, GenerationPolicyTarget{}, err
	}
	images := func(aggregate, field, unit string) GenerationValueRef {
		return GenerationValueRef{Source: "references", Aggregate: aggregate, Field: field, Kind: GenerationImage, Role: "reference", Unit: unit}
	}
	values := func(v ...any) []json.RawMessage {
		out := make([]json.RawMessage, 0, len(v))
		for _, x := range v {
			raw, _ := json.Marshal(x)
			out = append(out, raw)
		}
		return out
	}
	count := GenerationValueRef{Source: "parameter", Field: "count", Unit: "count"}
	component := func(id, usage string, estimate GenerationValueRef, rate GenerationRatio) GenerationPriceComponent {
		// 引擎只能用单一事实作估算量，这里的估算不代表费用；预留按上界，结算按实际token。
		return GenerationPriceComponent{ID: id, Estimate: estimate, Usage: usage, Unit: "count", Divisor: 1, QuantityRound: "exact",
			Tiers: []GenerationPriceTier{{ID: id + "_base", Fallback: true, Rate: rate}}}
	}
	mode := GenerationPolicyMode{
		Definition: GenerationModeDefinition{Key: AICodeWithImageMode, Version: "1", Kind: GenerationImage, InputSchema: schema},
		Parameters: map[string]GenerationFactType{
			"aspect_ratio": {Type: "string"}, "resolution": {Type: "string"}, "quality": {Type: "string"},
			"count": {Type: "number", Unit: "count"}, "background": {Type: "string"},
		},
		Defaults: map[string]json.RawMessage{
			"aspect_ratio": json.RawMessage(`"1:1"`), "resolution": json.RawMessage(`"2K"`), "quality": json.RawMessage(`"medium"`),
			"count": json.RawMessage(`1`), "background": json.RawMessage(`"auto"`),
		},
		// 中转偶尔少给结果也照常收费；下限取1，实际得到几张就落几张。
		Output: GenerationOutputPolicy{Kind: GenerationImage, MinOutputs: 1, MaxOutputs: aicodewithMaxImages, MIMEs: []string{"image/png"}, MaxBytes: aicodewithMaxImages * aicodewithImageMaxBytes},
		Rules: []GenerationConstraint{
			{ID: "reference_image_count", Assert: GenerationCondition{Op: "lte", Ref: images("count", "", "count"), Values: values(aicodewithMaxReferenceImages)}},
			{ID: "reference_image_format", Assert: GenerationCondition{Op: "in", Ref: images("each", "mime", ""), Values: values("image/jpeg", "image/png", "image/webp")}},
			{ID: "reference_image_bytes", Assert: GenerationCondition{Op: "lte", Ref: images("each", "bytes", "byte"), Values: values(aicodewithMaxReferenceBytes)}},
		},
		Pricing: []GenerationPriceComponent{
			component("text_input", "text_input_tokens", count, aicodewithTextRate),
			component("image_input", "image_input_tokens", images("count", "", "count"), aicodewithInputRate),
			component("image_output", "image_output_tokens", count, aicodewithOutputRate),
		},
	}
	bound := func(n int64) *int64 { return &n }
	d := GenerationPolicyDefinition{
		EngineVersion: GenerationPolicyEngineVersion, Version: AICodeWithImagePolicyVersion,
		Profile:      GenerationProfileDefinition{ModelKey: model, ModelRevision: "2026-10-09", Version: "1"},
		DeploymentID: AICodeWithImageDeploymentID(model), AdapterVersion: AICodeWithImageAdapterVersion,
		Currency: currency, Modes: []GenerationPolicyMode{mode},
	}
	target := GenerationPolicyTarget{
		ModelKey: model, ModelRevision: d.Profile.ModelRevision, DeploymentID: d.DeploymentID, AdapterVersion: d.AdapterVersion,
		Modes: []string{AICodeWithImageMode},
		Usage: map[string]GenerationUsageDefinition{
			"text_input_tokens":   {Unit: "count", UpperBound: bound(aicodewithMaxTextTokens)},
			"image_input_tokens":  {Unit: "count", UpperBound: bound(aicodewithMaxReferenceImages * aicodewithInputTokensPerImage)},
			"image_output_tokens": {Unit: "count", UpperBound: bound(aicodewithMaxImages * aicodewithOutputTokensPerImage)},
		},
	}
	return d, target, nil
}
