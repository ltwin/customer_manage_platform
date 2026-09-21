package llmgateway

import (
	"encoding/json"
	"fmt"
	"math/big"
	"reflect"
	"slices"
	"strings"
)

const maxGenerationPolicyBytes = 512 << 10

// CompiledGenerationPolicy 只持有编译后的私有副本；配置切换不能改变已获得的实例。
type CompiledGenerationPolicy struct {
	definition GenerationPolicyDefinition
	target     GenerationPolicyTarget
	raw        json.RawMessage
	profile    *GenerationProfile
}

func CompileGenerationPolicy(raw []byte, target GenerationPolicyTarget) (*CompiledGenerationPolicy, error) {
	canonical, err := generationJSONObject(raw, maxGenerationPolicyBytes)
	if err != nil {
		return nil, err
	}
	// 先固定持久表示再检查规则，避免转义膨胀让已接受的策略无法恢复。
	raw = canonical
	if err := generationPolicyRequiredFields(raw, reflect.TypeFor[GenerationPolicyDefinition]()); err != nil {
		return nil, err
	}
	var d GenerationPolicyDefinition
	if err := decodeGenerationJSON(raw, &d); err != nil {
		return nil, err
	}
	if len(d.Modes) == 0 || len(d.Modes) > 64 || d.EngineVersion != GenerationPolicyEngineVersion || !generationToken(d.Version) || !generationToken(d.DeploymentID) || !generationToken(d.AdapterVersion) || len(d.Currency) != 3 || strings.Trim(d.Currency, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") != "" {
		return nil, fmt.Errorf("%w: generation policy identity", ErrValidation)
	}
	if d.Profile.ModelKey != target.ModelKey || d.Profile.ModelRevision != target.ModelRevision || d.DeploymentID != target.DeploymentID || d.AdapterVersion != target.AdapterVersion || len(target.Modes) > 64 || len(target.Usage) > 64 {
		return nil, fmt.Errorf("%w: generation adapter binding", ErrCapability)
	}
	for i, key := range target.Modes {
		if !generationToken(key) || slices.Contains(target.Modes[:i], key) {
			return nil, fmt.Errorf("%w: generation adapter modes", ErrValidation)
		}
	}
	for key, spec := range target.Usage {
		if !generationToken(key) || !generationUnit(spec.Unit) || spec.UpperBound != nil && *spec.UpperBound < 0 {
			return nil, fmt.Errorf("%w: generation usage limit", ErrValidation)
		}
	}
	var bindings []GenerationMode
	for _, m := range d.Modes {
		if !slices.Contains(target.Modes, m.Definition.Key) {
			return nil, fmt.Errorf("%w: generation adapter mode", ErrCapability)
		}
		for _, price := range m.Pricing {
			spec, ok := target.Usage[price.Usage]
			if !ok || spec.Unit != price.Unit {
				return nil, fmt.Errorf("%w: generation adapter usage binding", ErrCapability)
			}
		}
		if err := compileGenerationPolicyMode(m); err != nil {
			return nil, err
		}
		if _, err := generationPriceUpperBound(m, target); err != nil {
			return nil, err
		}
		output := m.Output
		bindings = append(bindings, GenerationMode{Definition: m.Definition, Output: func(GenerationInput) (GenerationOutputPolicy, error) { return output, nil }})
	}
	profile, err := NewGenerationProfile(d.Profile, bindings)
	if err != nil {
		return nil, err
	}
	canonical, err = json.Marshal(d)
	if err != nil {
		return nil, err
	}
	if len(canonical) > maxGenerationPolicyBytes {
		return nil, fmt.Errorf("%w: canonical generation policy size", ErrValidation)
	}
	// 预留快照的policy包装层，保证编译成功的规则可按同一解析深度恢复。
	wrapped := append([]byte(`{"policy":`), canonical...)
	wrapped = append(wrapped, '}')
	if err := checkGenerationJSON(wrapped, maxGenerationPolicyBytes+16); err != nil {
		return nil, err
	}
	target.Modes = slices.Clone(target.Modes)
	usage := make(map[string]GenerationUsageDefinition, len(target.Usage))
	for k, v := range target.Usage {
		if v.UpperBound != nil {
			n := *v.UpperBound
			v.UpperBound = &n
		}
		usage[k] = v
	}
	target.Usage = usage
	return &CompiledGenerationPolicy{definition: d, target: target, raw: canonical, profile: profile}, nil
}

func compileGenerationPolicyMode(m GenerationPolicyMode) error {
	schema, _, err := compileGenerationSchema(m.Definition.InputSchema)
	if err != nil {
		return err
	}
	if err := m.Output.validate(); err != nil {
		return err
	}
	if m.Output.Kind != m.Definition.Kind || len(m.Parameters) > 128 || len(m.Defaults) > 128 || len(m.Rules) > 128 || len(m.Pricing) == 0 || len(m.Pricing) > 16 {
		return fmt.Errorf("%w: generation policy mode bounds", ErrValidation)
	}
	properties, _ := schema.definition["properties"].(map[string]any)
	params, _ := properties["parameters"].(map[string]any)
	parameterProperties, _ := params["properties"].(map[string]any)
	for name, spec := range m.Parameters {
		field, _ := parameterProperties[name].(map[string]any)
		typ, _ := field["type"].(string)
		if !generationToken(name) || !generationFactTypeValid(spec) || typ != spec.Type && (typ != "integer" || spec.Type != "number") {
			return fmt.Errorf("%w: parameter fact type", ErrValidation)
		}
	}
	for name, value := range m.Defaults {
		spec, ok := m.Parameters[name]
		if !ok {
			return fmt.Errorf("%w: default parameter binding", ErrValidation)
		}
		if _, err := generationScalar(value, spec); err != nil {
			return err
		}
		field, _ := parameterProperties[name].(map[string]any)
		raw, err := json.Marshal(field)
		if err != nil {
			return err
		}
		// 默认值需要通过完整字段schema，不能等到第一次请求才发现配置不可执行。
		sub, _, err := compileGenerationSchema(json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"value":` + string(raw) + `}}`))
		if err != nil {
			return err
		}
		val, err := generationDecodeValue(value)
		if err != nil {
			return err
		}
		if sub.compiled.VisitJSON(map[string]any{"value": val}) != nil || !generationExactConstraints(sub.definition, map[string]any{"value": val}) {
			return fmt.Errorf("%w: default violates parameter schema", ErrValidation)
		}
	}
	usages := make(map[string]GenerationFactType)
	ids := make(map[string]bool)
	for _, price := range m.Pricing {
		if !generationPolicyID(ids, price.ID) || !generationToken(price.Usage) || !generationUnit(price.Unit) || price.Divisor <= 0 || price.MinMicros < 0 || (price.QuantityRound != "ceil" && price.QuantityRound != "exact") || len(price.Tiers) == 0 || len(price.Tiers) > 32 || len(price.Modifiers) > 16 {
			return fmt.Errorf("%w: generation price component", ErrValidation)
		}
		if _, ok := usages[price.Usage]; ok {
			return fmt.Errorf("%w: overlapping generation usage component", ErrValidation)
		}
		usages[price.Usage] = GenerationFactType{Type: "number", Unit: price.Unit}
	}
	nodes := 0
	for _, rule := range m.Rules {
		if !generationPolicyID(ids, rule.ID) {
			return fmt.Errorf("%w: generation rule ID", ErrValidation)
		}
		if rule.When != nil {
			if err := compileGenerationCondition(*rule.When, m.Parameters, nil, 0, &nodes); err != nil {
				return err
			}
		}
		if err := compileGenerationCondition(rule.Assert, m.Parameters, nil, 0, &nodes); err != nil {
			return err
		}
	}
	for _, price := range m.Pricing {
		typ, err := generationReferenceType(price.Estimate, m.Parameters, nil)
		if err != nil || typ.Type != "number" || typ.Unit != price.Unit || price.Estimate.Aggregate == "each" {
			return fmt.Errorf("%w: generation estimate meter", ErrValidation)
		}
		fallback := 0
		for _, tier := range price.Tiers {
			if !generationPolicyID(ids, tier.ID) || !generationRatioValid(tier.Rate) {
				return fmt.Errorf("%w: generation price tier", ErrValidation)
			}
			if tier.Fallback {
				fallback++
				if tier.When != nil {
					return fmt.Errorf("%w: conditional price fallback", ErrValidation)
				}
			} else {
				if tier.When == nil {
					return fmt.Errorf("%w: missing price condition", ErrValidation)
				}
				if err := compileGenerationCondition(*tier.When, m.Parameters, usages, 0, &nodes); err != nil {
					return err
				}
			}
		}
		if fallback != 1 {
			return fmt.Errorf("%w: price requires one explicit fallback", ErrValidation)
		}
		for _, mod := range price.Modifiers {
			if !generationPolicyID(ids, mod.ID) || !generationRatioValid(mod.Factor) {
				return fmt.Errorf("%w: generation price modifier", ErrValidation)
			}
			if err := compileGenerationCondition(mod.When, m.Parameters, usages, 0, &nodes); err != nil {
				return err
			}
		}
	}
	return nil
}

func generationPolicyID(seen map[string]bool, id string) bool {
	if !generationToken(id) || seen[id] {
		return false
	}
	seen[id] = true
	return true
}
func generationRatioValid(r GenerationRatio) bool { return r.Numerator >= 0 && r.Denominator > 0 }
func generationUnit(s string) bool {
	return slices.Contains([]string{"count", "millisecond", "byte", "pixel", "ratio", "fps", "scalar"}, s)
}
func generationFactTypeValid(t GenerationFactType) bool {
	return t.Type == "number" && generationUnit(t.Unit) || (t.Type == "string" || t.Type == "boolean") && t.Unit == ""
}

func generationReferenceType(r GenerationValueRef, parameters, usage map[string]GenerationFactType) (GenerationFactType, error) {
	var typ GenerationFactType
	switch r.Source {
	case "parameter", "usage":
		if r.Aggregate != "" || r.Kind != "" || r.Role != "" {
			return typ, fmt.Errorf("%w: scalar fact selector", ErrValidation)
		}
		if r.Source == "parameter" {
			typ = parameters[r.Field]
		} else {
			typ = usage[r.Field]
		}
	case "prompt":
		if r.Field != "" || r.Aggregate != "" || r.Kind != "" || r.Role != "" {
			return typ, fmt.Errorf("%w: prompt selector", ErrValidation)
		}
		typ = GenerationFactType{Type: "string"}
	case "references":
		if r.Kind != "" && !generationKind(r.Kind) || r.Role != "" && !generationToken(r.Role) {
			return typ, fmt.Errorf("%w: reference selector", ErrValidation)
		}
		if r.Aggregate == "count" && r.Field == "" {
			typ = GenerationFactType{Type: "number", Unit: "count"}
		} else {
			typ = generationMediaFactType(r.Field)
			if !slices.Contains([]string{"each", "sum", "min", "max"}, r.Aggregate) || r.Aggregate != "each" && typ.Type != "number" {
				return typ, fmt.Errorf("%w: reference aggregation", ErrValidation)
			}
		}
	default:
		return typ, fmt.Errorf("%w: unknown generation fact source", ErrValidation)
	}
	if typ.Type == "" || typ.Unit != r.Unit {
		return typ, fmt.Errorf("%w: generation fact path or unit", ErrValidation)
	}
	return typ, nil
}
func generationMediaFactType(field string) GenerationFactType {
	switch field {
	case "duration_ms":
		return GenerationFactType{Type: "number", Unit: "millisecond"}
	case "bytes":
		return GenerationFactType{Type: "number", Unit: "byte"}
	case "width", "height":
		return GenerationFactType{Type: "number", Unit: "pixel"}
	case "frame_rate":
		return GenerationFactType{Type: "number", Unit: "fps"}
	case "aspect_ratio":
		return GenerationFactType{Type: "number", Unit: "ratio"}
	case "mime", "kind", "role":
		return GenerationFactType{Type: "string"}
	default:
		return GenerationFactType{}
	}
}

func compileGenerationCondition(c GenerationCondition, params, usage map[string]GenerationFactType, depth int, nodes *int) error {
	*nodes++
	if depth > 12 || *nodes > 2048 {
		return fmt.Errorf("%w: generation expression bounds", ErrValidation)
	}
	if c.Op == "all" || c.Op == "any" || c.Op == "not" {
		if len(c.Children) == 0 || len(c.Children) > 64 || c.Op == "not" && len(c.Children) != 1 || c.Ref != (GenerationValueRef{}) || len(c.Values) != 0 {
			return fmt.Errorf("%w: generation logical expression", ErrValidation)
		}
		for _, child := range c.Children {
			if err := compileGenerationCondition(child, params, usage, depth+1, nodes); err != nil {
				return err
			}
		}
		return nil
	}
	typ, err := generationReferenceType(c.Ref, params, usage)
	if err != nil {
		return err
	}
	if len(c.Children) != 0 {
		return fmt.Errorf("%w: generation leaf children", ErrValidation)
	}
	n := len(c.Values)
	switch c.Op {
	case "exists":
		if n != 0 {
			return fmt.Errorf("%w: presence operands", ErrValidation)
		}
	case "eq":
		if n != 1 {
			return fmt.Errorf("%w: equality operands", ErrValidation)
		}
	case "in":
		if n == 0 || n > 64 {
			return fmt.Errorf("%w: membership operands", ErrValidation)
		}
	case "lte", "gte":
		if n != 1 || typ.Type != "number" {
			return fmt.Errorf("%w: numeric operands", ErrValidation)
		}
	case "between":
		if n != 2 || typ.Type != "number" {
			return fmt.Errorf("%w: range operands", ErrValidation)
		}
	default:
		return fmt.Errorf("%w: unknown generation operator", ErrValidation)
	}
	for _, v := range c.Values {
		value, err := generationScalar(v, typ)
		if err != nil {
			return err
		}
		if value.kind == "string" && len(value.text) > 4096 {
			return fmt.Errorf("%w: generation condition literal size", ErrValidation)
		}
	}
	if c.Op == "between" {
		lo, _ := generationScalar(c.Values[0], typ)
		hi, _ := generationScalar(c.Values[1], typ)
		if lo.number.Cmp(hi.number) > 0 {
			return fmt.Errorf("%w: reversed generation range", ErrValidation)
		}
	}
	return nil
}

func generationRate(r GenerationRatio) *big.Rat {
	return new(big.Rat).SetFrac(big.NewInt(r.Numerator), big.NewInt(r.Denominator))
}

// 必填零值也必须显式提供，避免漏写价格分子后被Go零值解释为免费。
func generationPolicyRequiredFields(raw []byte, typ reflect.Type) error {
	if typ == reflect.TypeFor[json.RawMessage]() {
		return nil
	}
	switch typ.Kind() {
	case reflect.Pointer:
		return generationPolicyRequiredFields(raw, typ.Elem())
	case reflect.Struct:
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			return err
		}
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			parts := strings.Split(f.Tag.Get("json"), ",")
			name := parts[0]
			value, ok := fields[name]
			if !ok {
				if !slices.Contains(parts, "omitempty") {
					return fmt.Errorf("%w: missing generation policy field %s", ErrValidation, name)
				}
				continue
			}
			if err := generationPolicyRequiredFields(value, f.Type); err != nil {
				return err
			}
		}
	case reflect.Slice:
		var values []json.RawMessage
		if err := json.Unmarshal(raw, &values); err != nil {
			return err
		}
		for _, v := range values {
			if err := generationPolicyRequiredFields(v, typ.Elem()); err != nil {
				return err
			}
		}
	case reflect.Map:
		var values map[string]json.RawMessage
		if err := json.Unmarshal(raw, &values); err != nil {
			return err
		}
		for _, v := range values {
			if err := generationPolicyRequiredFields(v, typ.Elem()); err != nil {
				return err
			}
		}
	}
	return nil
}
