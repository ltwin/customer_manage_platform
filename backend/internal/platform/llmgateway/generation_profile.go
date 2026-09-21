package llmgateway

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/getkin/kin-openapi/openapi3"
)

type GenerationProfileDefinition struct {
	ModelKey      string `json:"model_key"`
	ModelRevision string `json:"model_revision"`
	Version       string `json:"version"`
}

// GenerationModeDefinition 同时服务后端校验和前端/Agent的能力投影。
// InputSchema描述整个GenerationInput，使用当前支持的封闭schema子集。
type GenerationModeDefinition struct {
	Key         string          `json:"key"`
	Version     string          `json:"version"`
	Kind        GenerationKind  `json:"kind"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// GenerationMode 由模型实现注册；回调必须是纯函数，不做网络、数据库或计费操作。
// Normalize检查跨字段规则并返回规范参数，不能更换Prompt或参考素材。
// Output根据规范化输入给出结果约束；两者的行为变化必须提升模式版本。
type GenerationMode struct {
	Definition GenerationModeDefinition
	Normalize  func(GenerationInput) (json.RawMessage, error)
	Output     func(GenerationInput) (GenerationOutputPolicy, error)
}

type generationMode struct {
	binding GenerationMode
	schema  *generationSchema
}

// GenerationProfile 在构造时复制模式目录；模型参数校验的唯一入口是Prepare。
// 本类型不代表已启用的供应商部署，也不替代账号或预算校验。
type GenerationProfile struct {
	definition GenerationProfileDefinition
	modes      []generationMode
}

func NewGenerationProfile(d GenerationProfileDefinition, modes []GenerationMode) (*GenerationProfile, error) {
	if !generationToken(d.ModelKey) || !generationToken(d.ModelRevision) || !generationToken(d.Version) || len(modes) == 0 || len(modes) > 64 {
		return nil, fmt.Errorf("%w: generation profile identity or modes", ErrValidation)
	}
	p := &GenerationProfile{definition: d}
	seen := make(map[string]bool, len(modes))
	for _, m := range modes {
		def := m.Definition
		if !generationToken(def.Key) || !generationToken(def.Version) || !generationKind(def.Kind) || seen[def.Key] || m.Output == nil {
			return nil, fmt.Errorf("%w: generation mode registration", ErrValidation)
		}
		seen[def.Key] = true
		schema, raw, err := compileGenerationSchema(def.InputSchema)
		if err != nil {
			return nil, err
		}
		m.Definition.InputSchema = raw
		p.modes = append(p.modes, generationMode{binding: m, schema: schema})
	}
	return p, nil
}

func (p *GenerationProfile) Definition() GenerationProfileDefinition { return p.definition }
func (p *GenerationProfile) Modes() []GenerationModeDefinition {
	result := make([]GenerationModeDefinition, 0, len(p.modes))
	for _, m := range p.modes {
		d := m.binding.Definition
		d.InputSchema = slices.Clone(d.InputSchema)
		result = append(result, d)
	}
	return result
}

// Prepare 显式模式只校验该模式；自动模式要求恰好一个完整匹配，不按注册顺序猜测。
func (p *GenerationProfile) Prepare(request GenerationRequest) (PreparedGeneration, error) {
	r, err := normalizeGenerationEnvelope(request)
	if err != nil {
		return PreparedGeneration{}, err
	}
	if r.ModelKey != p.definition.ModelKey {
		return PreparedGeneration{}, fmt.Errorf("%w: generation model binding", ErrCapability)
	}
	var result PreparedGeneration
	matches := 0
	for _, m := range p.modes {
		def := m.binding.Definition
		if def.Kind != r.Kind || r.Mode != "" && r.Mode != def.Key {
			continue
		}
		candidate, err := p.prepareMode(r, m)
		if err != nil {
			if errors.Is(err, ErrCapability) {
				continue
			}
			return PreparedGeneration{}, err
		}
		matches++
		if matches > 1 {
			return PreparedGeneration{}, fmt.Errorf("%w: ambiguous generation mode", ErrConflict)
		}
		result = candidate
	}
	if matches == 0 {
		return PreparedGeneration{}, fmt.Errorf("%w: no matching generation mode", ErrCapability)
	}
	return result, nil
}

func (p *GenerationProfile) prepareMode(r GenerationRequest, m generationMode) (PreparedGeneration, error) {
	if err := validateGenerationInput(m.schema, r.Input); err != nil {
		return PreparedGeneration{}, err
	}
	r = cloneGenerationRequest(r)
	r.Mode = m.binding.Definition.Key
	if m.binding.Normalize != nil {
		parameters, err := m.binding.Normalize(cloneGenerationRequest(r).Input)
		if err != nil {
			// 模式不匹配可参与自动选择；实现故障不能静默切到另一种模式。
			if errors.Is(err, ErrValidation) || errors.Is(err, ErrCapability) {
				return PreparedGeneration{}, fmt.Errorf("%w: generation mode semantics", ErrCapability)
			}
			return PreparedGeneration{}, fmt.Errorf("%w: generation mode normalizer", ErrProtocol)
		}
		r.Input.Parameters = slices.Clone(parameters)
		r, err = normalizeGenerationEnvelope(r)
		if err != nil {
			return PreparedGeneration{}, fmt.Errorf("%w: generation mode normalized input", ErrProtocol)
		}
		if err := validateGenerationInput(m.schema, r.Input); err != nil {
			return PreparedGeneration{}, fmt.Errorf("%w: generation mode normalized schema", ErrProtocol)
		}
	}
	output, err := m.binding.Output(cloneGenerationRequest(r).Input)
	if err != nil {
		return PreparedGeneration{}, fmt.Errorf("%w: generation mode output policy", ErrProtocol)
	}
	if err := output.validate(); err != nil || output.Kind != r.Kind {
		return PreparedGeneration{}, fmt.Errorf("%w: generation mode output policy", ErrProtocol)
	}
	output.MIMEs = slices.Clone(output.MIMEs)
	s := GenerationSnapshot{Request: r, ModelRevision: p.definition.ModelRevision, ProfileVersion: p.definition.Version, ModeVersion: m.binding.Definition.Version, InputSchema: slices.Clone(m.binding.Definition.InputSchema), Output: output}
	return freezeGeneration(s)
}

func compileGenerationSchema(raw json.RawMessage) (*generationSchema, json.RawMessage, error) {
	canonical, err := generationJSONObject(raw, 64<<10)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: generation mode schema JSON", ErrValidation)
	}
	// 共用封闭schema语法检查，拒绝远程引用及未知关键字，不让约束被库静默忽略。
	if err := validateToolSchema(canonical); err != nil {
		return nil, nil, fmt.Errorf("%w: unsupported generation mode schema", ErrValidation)
	}
	var schema openapi3.Schema
	if err := json.Unmarshal(canonical, &schema); err != nil {
		return nil, nil, fmt.Errorf("%w: generation mode schema", ErrValidation)
	}
	if err := schema.Validate(context.Background(), openapi3.EnableSchemaFormatValidation()); err != nil {
		return nil, nil, fmt.Errorf("%w: generation mode schema", ErrValidation)
	}
	dec := json.NewDecoder(bytes.NewReader(canonical))
	dec.UseNumber()
	var definition map[string]any
	if err := dec.Decode(&definition); err != nil || !generationSchemaNumbersSupported(definition) || !generationSchemaConstraintsSupported(definition) {
		return nil, nil, fmt.Errorf("%w: generation schema numeric bounds", ErrValidation)
	}
	// 库的复合枚举使用float64与反射比较；枚举统一交给精确递归比较执行。
	removeGenerationEnums(&schema)
	return &generationSchema{compiled: &schema, definition: definition}, canonical, nil
}

func validateGenerationInput(schema *generationSchema, input GenerationInput) error {
	raw, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("%w: generation mode input JSON", ErrValidation)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return fmt.Errorf("%w: generation mode input JSON", ErrValidation)
	}
	if err := schema.compiled.VisitJSON(value); err != nil {
		return fmt.Errorf("%w: generation mode input schema", ErrCapability)
	}
	if !generationExactConstraints(schema.definition, value) {
		return fmt.Errorf("%w: generation mode numeric schema", ErrCapability)
	}
	return nil
}

func normalizeGenerationEnvelope(r GenerationRequest) (GenerationRequest, error) {
	r = cloneGenerationRequest(r)
	if err := validateGenerationEnvelope(r); err != nil {
		return GenerationRequest{}, err
	}
	if len(r.Input.Parameters) == 0 {
		r.Input.Parameters = json.RawMessage(`{}`)
	}
	params, err := generationJSONObject(r.Input.Parameters, 64<<10)
	if err != nil {
		return GenerationRequest{}, fmt.Errorf("%w: generation parameter JSON", ErrValidation)
	}
	r.Input.Parameters = params
	return r, nil
}

func freezeGeneration(s GenerationSnapshot) (PreparedGeneration, error) {
	raw, err := json.Marshal(s)
	if err != nil {
		return PreparedGeneration{}, fmt.Errorf("%w: generation snapshot JSON", ErrValidation)
	}
	hash := sha256.Sum256(raw)
	return PreparedGeneration{snapshot: s, fingerprint: hex.EncodeToString(hash[:])}, nil
}

// RestoreGeneration 只接受受信存储中的快照与其独立保存的指纹，不接受外部请求自报许可。
// 无需当前目录，不重新推断模式、执行校验回调或补默认值；存储/账号授权由调用方负责。
func RestoreGeneration(raw []byte, expectedFingerprint string) (PreparedGeneration, error) {
	var s GenerationSnapshot
	if err := decodeGenerationJSON(raw, &s); err != nil {
		return PreparedGeneration{}, err
	}
	if !generationToken(s.ModelRevision) || !generationToken(s.ProfileVersion) || !generationToken(s.ModeVersion) || s.Request.Mode == "" {
		return PreparedGeneration{}, fmt.Errorf("%w: generation snapshot identity", ErrValidation)
	}
	r, err := normalizeGenerationEnvelope(s.Request)
	if err != nil {
		return PreparedGeneration{}, err
	}
	schema, canonical, err := compileGenerationSchema(s.InputSchema)
	if err != nil {
		return PreparedGeneration{}, err
	}
	if err := validateGenerationInput(schema, r.Input); err != nil {
		return PreparedGeneration{}, err
	}
	if err := s.Output.validate(); err != nil || s.Output.Kind != r.Kind {
		return PreparedGeneration{}, fmt.Errorf("%w: generation snapshot output policy", ErrValidation)
	}
	s.Request = r
	s.InputSchema = canonical
	prepared, err := freezeGeneration(s)
	if err != nil {
		return PreparedGeneration{}, err
	}
	if prepared.fingerprint != expectedFingerprint {
		return PreparedGeneration{}, fmt.Errorf("%w: generation snapshot fingerprint", ErrConflict)
	}
	return prepared, nil
}
