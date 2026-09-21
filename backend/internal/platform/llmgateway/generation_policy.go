package llmgateway

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
)

// Definition 返回独立能力投影，前端/Agent读取同一版本，不共享引擎可变内存。
func (p *CompiledGenerationPolicy) Definition() GenerationPolicyDefinition {
	var d GenerationPolicyDefinition
	// raw只由构造器编码且编译成功，此处解码不可能失败。
	_ = json.Unmarshal(p.raw, &d)
	return d
}
func (p *CompiledGenerationPolicy) Version() string { return p.definition.Version }

// Prepare 对每个候选先补默认值，再通过已有Profile校验，最后执行媒体/条件规则。
// 未知候选可能匹配，不能被另一个已知候选掩盖；付费派发仍由Gateway负责。
func (p *CompiledGenerationPolicy) Prepare(request GenerationRequest, facts GenerationFacts) (GenerationPolicyResult, error) {
	if p == nil {
		return GenerationPolicyResult{}, fmt.Errorf("%w: missing generation policy", ErrValidation)
	}
	r, err := normalizeGenerationEnvelope(request)
	if err != nil {
		return GenerationPolicyResult{}, err
	}
	if r.ModelKey != p.definition.Profile.ModelKey {
		return GenerationPolicyResult{}, fmt.Errorf("%w: generation policy model", ErrCapability)
	}
	if err := facts.validate(r.Input.References); err != nil {
		return GenerationPolicyResult{}, err
	}
	var issues []GenerationRuleIssue
	var prepared PreparedGeneration
	var selected GenerationPolicyMode
	matches, unknown := 0, false
	for _, mode := range p.definition.Modes {
		if mode.Definition.Kind != r.Kind || r.Mode != "" && r.Mode != mode.Definition.Key {
			continue
		}
		candidate := cloneGenerationRequest(r)
		candidate.Mode = mode.Definition.Key
		var params map[string]json.RawMessage
		if err := json.Unmarshal(candidate.Input.Parameters, &params); err != nil {
			return GenerationPolicyResult{}, err
		}
		for name, value := range mode.Defaults {
			if _, exists := params[name]; !exists {
				params[name] = value
			}
		}
		candidate.Input.Parameters, err = json.Marshal(params)
		if err != nil {
			return GenerationPolicyResult{}, err
		}
		value, err := p.profile.Prepare(candidate)
		if errors.Is(err, ErrCapability) {
			issues = append(issues, GenerationRuleIssue{Mode: mode.Definition.Key, RuleID: "input_schema", Code: "constraint_failed"})
			continue
		}
		if err != nil {
			return GenerationPolicyResult{}, err
		}
		env := generationEnvironment{input: candidate.Input, params: params, facts: facts}
		verdict, problems, err := evaluateGenerationRules(mode, env)
		if err != nil {
			return GenerationPolicyResult{}, err
		}
		issues = append(issues, problems...)
		if verdict == GenerationNeedsFacts {
			unknown = true
			continue
		}
		if verdict == GenerationDeny {
			continue
		}
		matches++
		prepared = value
		selected = mode
	}
	if matches > 1 {
		return GenerationPolicyResult{}, fmt.Errorf("%w: ambiguous generation policy mode", ErrConflict)
	}
	if unknown {
		return GenerationPolicyResult{Verdict: GenerationNeedsFacts, Issues: issues}, nil
	}
	if matches == 0 {
		return GenerationPolicyResult{Verdict: GenerationDeny, Issues: issues}, nil
	}
	result, err := p.freeze(prepared, selected, facts)
	if errors.Is(err, ErrGenerationFactsUnavailable) {
		return GenerationPolicyResult{Verdict: GenerationNeedsFacts, Issues: []GenerationRuleIssue{{Mode: selected.Definition.Key, RuleID: "pricing", Code: "needs_facts"}}}, nil
	}
	if err != nil {
		return GenerationPolicyResult{}, err
	}
	return GenerationPolicyResult{Verdict: GenerationAllow, Prepared: result}, nil
}

// generationPolicySnapshot 保留整个已编译规则包和受信部署契约，支持离线重算旧任务。
// 该快照仅供内部持久化，不是HTTP DTO；无账号/派发授权语义。
type generationPolicySnapshot struct {
	Policy                json.RawMessage           `json:"policy"`
	Target                GenerationPolicyTarget    `json:"target"`
	Generation            GenerationSnapshot        `json:"generation"`
	GenerationFingerprint string                    `json:"generation_fingerprint"`
	Media                 []GenerationMediaMetadata `json:"media,omitempty"`
	Quote                 GenerationQuote           `json:"quote"`
}
type PreparedPolicyGeneration struct {
	raw         json.RawMessage
	fingerprint string
	policy      *CompiledGenerationPolicy
	generation  PreparedGeneration
	mode        GenerationPolicyMode
	facts       GenerationFacts
	quote       GenerationQuote
}

func (p *CompiledGenerationPolicy) freeze(g PreparedGeneration, m GenerationPolicyMode, facts GenerationFacts) (PreparedPolicyGeneration, error) {
	input := g.Snapshot().Request.Input
	var params map[string]json.RawMessage
	if err := json.Unmarshal(input.Parameters, &params); err != nil {
		return PreparedPolicyGeneration{}, err
	}
	env := generationEnvironment{input: input, params: params, facts: facts}
	usage, err := generationEstimateUsage(m, env)
	if err != nil {
		return PreparedPolicyGeneration{}, err
	}
	if err := generationCheckUsage(m, p.target, usage); err != nil {
		return PreparedPolicyGeneration{}, err
	}
	env.usage = usage
	price, err := generationPriceUsage(p.definition.Currency, m, env)
	if err != nil {
		return PreparedPolicyGeneration{}, err
	}
	upper, err := generationPriceUpperBound(m, p.target)
	if err != nil {
		return PreparedPolicyGeneration{}, err
	}
	quote := GenerationQuote{Currency: price.Currency, EstimateMicros: price.TotalMicros, UpperBoundMicros: upper, Lines: price.Lines}
	snapshot := generationPolicySnapshot{Policy: p.raw, Target: p.target, Generation: g.Snapshot(), GenerationFingerprint: g.Fingerprint(), Media: facts.media, Quote: quote}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return PreparedPolicyGeneration{}, err
	}
	if err := checkGenerationJSON(raw, maxGenerationJSONBytes+maxGenerationPolicyBytes); err != nil {
		return PreparedPolicyGeneration{}, err
	}
	hash := sha256.Sum256(raw)
	return PreparedPolicyGeneration{raw: raw, fingerprint: hex.EncodeToString(hash[:]), policy: p, generation: g, mode: m, facts: facts, quote: quote}, nil
}
func (p PreparedPolicyGeneration) Fingerprint() string            { return p.fingerprint }
func (p PreparedPolicyGeneration) Generation() PreparedGeneration { return p.generation }
func (p PreparedPolicyGeneration) Quote() GenerationQuote {
	q := p.quote
	if q.UpperBoundMicros != nil {
		v := *q.UpperBoundMicros
		q.UpperBoundMicros = &v
	}
	q.Lines = slices.Clone(q.Lines)
	for i := range q.Lines {
		q.Lines[i].Modifiers = slices.Clone(q.Lines[i].Modifiers)
	}
	return q
}
func (p PreparedPolicyGeneration) MarshalSnapshot() ([]byte, error) {
	if p.policy == nil {
		return nil, fmt.Errorf("%w: unprepared generation policy", ErrValidation)
	}
	return slices.Clone(p.raw), nil
}

// PriceUsage 只计算冻结规则下的金额，不写账、不释放预算；未知或越界交由调用方核实。
func (p PreparedPolicyGeneration) PriceUsage(usage map[string]int64) (GenerationPrice, error) {
	if p.policy == nil {
		return GenerationPrice{}, fmt.Errorf("%w: unprepared generation policy", ErrValidation)
	}
	if err := generationCheckUsage(p.mode, p.policy.target, usage); err != nil {
		return GenerationPrice{}, err
	}
	input := p.generation.Snapshot().Request.Input
	var params map[string]json.RawMessage
	if err := json.Unmarshal(input.Parameters, &params); err != nil {
		return GenerationPrice{}, err
	}
	return generationPriceUsage(p.policy.definition.Currency, p.mode, generationEnvironment{input: input, params: params, facts: p.facts, usage: usage})
}

// RestorePolicyGeneration 只恢复受信存储与独立保存的指纹，绝不能暴露为外部授权接口。
// 重新编译的是快照中的旧程序，既不查询活动目录，也不接受新的Adapter上限覆盖它。
func RestorePolicyGeneration(raw []byte, expectedFingerprint string) (PreparedPolicyGeneration, error) {
	var s generationPolicySnapshot
	if err := decodeGenerationJSONLimit(raw, &s, maxGenerationJSONBytes+maxGenerationPolicyBytes); err != nil {
		return PreparedPolicyGeneration{}, err
	}
	policy, err := CompileGenerationPolicy(s.Policy, s.Target)
	if err != nil {
		return PreparedPolicyGeneration{}, err
	}
	genRaw, err := json.Marshal(s.Generation)
	if err != nil {
		return PreparedPolicyGeneration{}, err
	}
	generation, err := RestoreGeneration(genRaw, s.GenerationFingerprint)
	if err != nil {
		return PreparedPolicyGeneration{}, err
	}
	facts := GenerationFacts{references: generation.Snapshot().Request.Input.References, media: s.Media}
	result, err := policy.Prepare(generation.Snapshot().Request, facts)
	if err != nil {
		return PreparedPolicyGeneration{}, err
	}
	if result.Verdict != GenerationAllow || result.Prepared.fingerprint != expectedFingerprint {
		return PreparedPolicyGeneration{}, fmt.Errorf("%w: generation policy snapshot fingerprint", ErrConflict)
	}
	// 比较整个语义快照，拒绝篡改报价后只沿用原指纹；空白和键序不影响存储恢复。
	canonical, err := json.Marshal(s)
	if err != nil {
		return PreparedPolicyGeneration{}, err
	}
	hash := sha256.Sum256(canonical)
	if hex.EncodeToString(hash[:]) != expectedFingerprint {
		return PreparedPolicyGeneration{}, fmt.Errorf("%w: generation policy snapshot content", ErrConflict)
	}
	return result.Prepared, nil
}
