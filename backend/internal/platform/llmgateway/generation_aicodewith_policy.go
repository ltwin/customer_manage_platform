package llmgateway

import (
	"encoding/json"
	"fmt"
	"strings"
)

// AICodeWithImagePolicyVersion 是内置生图规则的版本；改价格、上界或参数须升级。
const AICodeWithImagePolicyVersion = "aicodewith-images-2026-11-09"

// AICodeWithImageMode 是各GPT Image模型唯一的模式：带图片参考即为图生图。
const AICodeWithImageMode = "image"

// 参考图数量上限是产品决策（owner 2026-10-09），中转本身允许16张。
// 单张大小上限为25 MiB（owner 2026-10-13），属于本地策略，供应商API上限尚未确认。
const (
	aicodewithMaxReferenceImages = 9
	aicodewithMaxReferenceBytes  = 25 << 20
)

// 中转2026-11改版后按次计费、不回报用量，每个任务只出一张图；失败不扣费。
// 单价（USD微单位/张）按市场行情估算（owner 2026-11-09：先参照tapnow等，定价不是当前重点），
// 待按中转账单核实后再升版本。
var aicodewithResolutionRates = map[string]int64{"1K": 40_000, "2K": 80_000, "4K": 160_000}

// aicodewithQualityFactors 只用于2.6线：中转按 low…max 五档校验并生成（2026-11-09实测）。
// 2与2.5线的quality被中转丢弃，不加价。
var aicodewithQualityFactors = []struct {
	Quality string
	Factor  GenerationRatio
}{
	{"low", GenerationRatio{Numerator: 1, Denominator: 2}},
	{"high", GenerationRatio{Numerator: 3, Denominator: 2}},
	{"xhigh", GenerationRatio{Numerator: 2, Denominator: 1}},
	{"max", GenerationRatio{Numerator: 3, Denominator: 1}},
}

// aicodewithQualities 是各模型线中转接受的画质档位；2.6之外沿用三档以兼容已有草稿。
func aicodewithQualities(model string) []string {
	if strings.HasPrefix(model, "gpt-image-2.6-") {
		return []string{"low", "medium", "high", "xhigh", "max"}
	}
	return []string{"low", "medium", "high"}
}

// AICodeWithImageRatios 是中转接受的全部比例；auto由模型决定画幅。
var AICodeWithImageRatios = []string{"auto", "1:1", "1:2", "2:1", "1:3", "3:1", "2:3", "3:2", "3:4", "4:3", "4:5", "5:4", "9:16", "16:9", "21:9", "9:21"}

// AICodeWithImagePolicy 保留旧调用方类型名；新部署共用供应商无关的策略绑定。
type AICodeWithImagePolicy = ImagePolicy

// AICodeWithImageDeploymentID 是模型在当前账号控制面中的部署身份。
func AICodeWithImageDeploymentID(model string) string { return "aicodewith-images-" + model }

// AICodeWithImagePolicies 编译各模型的内置规则。currency 必须是网关账本币种。
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
			"quality":      map[string]any{"type": "string", "enum": aicodewithQualities(model)},
			"count":        map[string]any{"type": "integer", "enum": []int{1}},
			"background":   map[string]any{"type": "string", "enum": []string{"auto", "opaque", "transparent"}},
		},
	}
	schema, err := imageGenerationInputSchema(parameters)
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
	param := func(field string, v any) *GenerationCondition {
		return &GenerationCondition{Op: "eq", Ref: GenerationValueRef{Source: "parameter", Field: field}, Values: values(v)}
	}
	var tiers []GenerationPriceTier
	for _, res := range []string{"1K", "2K", "4K"} {
		tiers = append(tiers, GenerationPriceTier{ID: "resolution_" + res, When: param("resolution", res), Rate: GenerationRatio{Numerator: aicodewithResolutionRates[res], Denominator: 1}})
	}
	// 只有比例才带清晰度；auto与兜底都按最贵档预留，结算同价。
	tiers = append(tiers, GenerationPriceTier{ID: "resolution_fallback", Fallback: true, Rate: GenerationRatio{Numerator: aicodewithResolutionRates["4K"], Denominator: 1}})
	price := GenerationPriceComponent{ID: "images", Estimate: GenerationValueRef{Source: "parameter", Field: "count", Unit: "count"}, Usage: "images", Unit: "count", Divisor: 1, QuantityRound: "exact", Tiers: tiers}
	if strings.HasPrefix(model, "gpt-image-2.6-") {
		for _, q := range aicodewithQualityFactors {
			price.Modifiers = append(price.Modifiers, GenerationPriceModifier{ID: "quality_" + q.Quality, When: *param("quality", q.Quality), Factor: q.Factor})
		}
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
		Output: GenerationOutputPolicy{Kind: GenerationImage, MinOutputs: 1, MaxOutputs: 1, MIMEs: []string{"image/png"}, MaxBytes: aicodewithImageMaxBytes},
		Rules: []GenerationConstraint{
			{ID: "reference_image_count", Assert: GenerationCondition{Op: "lte", Ref: images("count", "", "count"), Values: values(aicodewithMaxReferenceImages)}},
			{ID: "reference_image_format", Assert: GenerationCondition{Op: "in", Ref: images("each", "mime", ""), Values: values("image/jpeg", "image/png", "image/webp")}},
			{ID: "reference_image_bytes", Assert: GenerationCondition{Op: "lte", Ref: images("each", "bytes", "byte"), Values: values(aicodewithMaxReferenceBytes)}},
		},
		Pricing: []GenerationPriceComponent{price},
	}
	bound := func(n int64) *int64 { return &n }
	d := GenerationPolicyDefinition{
		EngineVersion: GenerationPolicyEngineVersion, Version: AICodeWithImagePolicyVersion,
		Profile:      GenerationProfileDefinition{ModelKey: model, ModelRevision: "2026-11-09", Version: "1"},
		DeploymentID: AICodeWithImageDeploymentID(model), AdapterVersion: AICodeWithImageAdapterVersion,
		Currency: currency, Modes: []GenerationPolicyMode{mode},
	}
	target := GenerationPolicyTarget{
		ModelKey: model, ModelRevision: d.Profile.ModelRevision, DeploymentID: d.DeploymentID, AdapterVersion: d.AdapterVersion,
		Modes: []string{AICodeWithImageMode},
		Usage: map[string]GenerationUsageDefinition{"images": {Unit: "count", UpperBound: bound(1)}},
	}
	return d, target, nil
}
