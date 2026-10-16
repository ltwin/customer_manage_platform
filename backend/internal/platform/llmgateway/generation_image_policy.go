package llmgateway

import "encoding/json"

const imagePromptLimit = 16000

// ImagePolicy 绑定一个模型在一个部署上的已编译能力、费用规则和Adapter版本。
type ImagePolicy struct {
	Definition GenerationPolicyDefinition
	Target     GenerationPolicyTarget
	Policy     *CompiledGenerationPolicy
}

// imageGenerationInputSchema 共用生成输入信封，参数能力仍由每个部署定义。
func imageGenerationInputSchema(parameters map[string]any) (json.RawMessage, error) {
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
	return json.Marshal(map[string]any{
		"type": "object", "additionalProperties": false,
		"required": []string{"prompt", "parameters"},
		"properties": map[string]any{
			"prompt":     map[string]any{"type": "string", "minLength": 1, "maxLength": imagePromptLimit},
			"parameters": parameters,
			"references": map[string]any{"type": "array", "maxItems": 50, "items": reference},
		},
	})
}
