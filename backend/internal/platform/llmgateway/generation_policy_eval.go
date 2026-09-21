package llmgateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"slices"
	"unicode/utf8"
)

type generationTruth uint8

const (
	generationFalse generationTruth = iota
	generationTrue
	generationUnknown
)

type generationValue struct {
	state   string
	number  *big.Rat
	text    string
	boolean bool
	kind    string
	index   *int
}
type generationEnvironment struct {
	input  GenerationInput
	params map[string]json.RawMessage
	facts  GenerationFacts
	usage  map[string]int64
}

func generationDecodeValue(raw []byte) (any, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var v any
	err := d.Decode(&v)
	return v, err
}
func generationScalar(raw []byte, typ GenerationFactType) (generationValue, error) {
	value, err := generationDecodeValue(raw)
	if err != nil {
		return generationValue{}, err
	}
	v := generationValue{state: "known", kind: typ.Type}
	switch typ.Type {
	case "number":
		n, ok := value.(json.Number)
		if !ok {
			return v, fmt.Errorf("%w: numeric generation fact", ErrValidation)
		}
		v.number, ok = generationRational(n)
		if !ok {
			return v, fmt.Errorf("%w: generation numeric bounds", ErrValidation)
		}
	case "string":
		text, ok := value.(string)
		if !ok {
			return v, fmt.Errorf("%w: string generation fact", ErrValidation)
		}
		v.text = text
	case "boolean":
		b, ok := value.(bool)
		if !ok {
			return v, fmt.Errorf("%w: boolean generation fact", ErrValidation)
		}
		v.boolean = b
	default:
		return v, fmt.Errorf("%w: generation fact type", ErrValidation)
	}
	return v, nil
}
func generationNumber(n int64) generationValue {
	return generationValue{state: "known", kind: "number", number: new(big.Rat).SetInt64(n)}
}
func generationString(s string) generationValue {
	return generationValue{state: "known", kind: "string", text: s}
}
func generationMediaValue(field string, ref GenerationReference, m GenerationMediaMetadata) generationValue {
	var n *int64
	switch field {
	case "kind":
		return generationString(string(m.Kind))
	case "mime":
		return generationString(m.MIME)
	case "role":
		return generationString(ref.Role)
	case "bytes":
		return generationNumber(m.ByteSize)
	case "duration_ms":
		if m.Kind != GenerationVideo && m.Kind != GenerationAudio {
			return generationValue{state: "not_applicable"}
		}
		n = m.DurationMS
	case "width", "height", "aspect_ratio":
		if m.Kind != GenerationImage && m.Kind != GenerationVideo {
			return generationValue{state: "not_applicable"}
		}
		switch field {
		case "width":
			n = m.Width
		case "height":
			n = m.Height
		default:
			if m.Width == nil || m.Height == nil || *m.Height == 0 {
				return generationValue{state: "unknown"}
			}
			return generationValue{state: "known", kind: "number", number: new(big.Rat).SetFrac64(*m.Width, *m.Height)}
		}
	case "frame_rate":
		if m.Kind != GenerationVideo {
			return generationValue{state: "not_applicable"}
		}
		if m.FrameRate == nil {
			return generationValue{state: "unknown"}
		}
		return generationValue{state: "known", kind: "number", number: generationRate(*m.FrameRate)}
	}
	if n == nil {
		return generationValue{state: "unknown"}
	}
	return generationNumber(*n)
}

func generationResolve(r GenerationValueRef, e generationEnvironment, params map[string]GenerationFactType) ([]generationValue, error) {
	switch r.Source {
	case "parameter":
		raw, ok := e.params[r.Field]
		if !ok {
			return []generationValue{{state: "absent"}}, nil
		}
		v, err := generationScalar(raw, params[r.Field])
		return []generationValue{v}, err
	case "usage":
		n, ok := e.usage[r.Field]
		if !ok {
			return []generationValue{{state: "unknown"}}, nil
		}
		return []generationValue{generationNumber(n)}, nil
	case "prompt":
		if e.input.Prompt == "" {
			return []generationValue{{state: "absent"}}, nil
		}
		return []generationValue{generationString(e.input.Prompt)}, nil
	}
	var values []generationValue
	count := int64(0)
	for i, ref := range e.input.References {
		if r.Kind != "" && r.Kind != ref.Kind || r.Role != "" && r.Role != ref.Role {
			continue
		}
		count++
		if r.Aggregate == "count" {
			continue
		}
		v := generationMediaValue(r.Field, ref, e.facts.media[i])
		index := i
		v.index = &index
		values = append(values, v)
	}
	if r.Aggregate == "count" {
		return []generationValue{generationNumber(count)}, nil
	}
	if r.Aggregate == "each" {
		return values, nil
	}
	if len(values) == 0 {
		if r.Aggregate == "sum" {
			return []generationValue{generationNumber(0)}, nil
		}
		return []generationValue{{state: "absent"}}, nil
	}
	// 必须扫描全体条目合并状态，不能因素材排列顺序掩盖未知事实。
	unknown, notApplicable := false, false
	for _, v := range values {
		unknown = unknown || v.state == "unknown"
		notApplicable = notApplicable || v.state == "not_applicable"
	}
	if unknown {
		return []generationValue{{state: "unknown"}}, nil
	}
	if notApplicable {
		return []generationValue{{state: "not_applicable"}}, nil
	}
	result := generationNumber(0)
	for i, v := range values {
		switch r.Aggregate {
		case "sum":
			result.number.Add(result.number, v.number)
		case "min":
			if i == 0 || v.number.Cmp(result.number) < 0 {
				result.number.Set(v.number)
			}
		case "max":
			if i == 0 || v.number.Cmp(result.number) > 0 {
				result.number.Set(v.number)
			}
		}
	}
	return []generationValue{result}, nil
}

func generationCompare(v generationValue, c GenerationCondition) generationTruth {
	if v.state == "unknown" {
		return generationUnknown
	}
	if c.Op == "exists" {
		if v.state == "known" {
			return generationTrue
		}
		return generationFalse
	}
	if v.state != "known" {
		return generationFalse
	}
	var expected []generationValue
	for _, raw := range c.Values {
		n, err := generationScalar(raw, GenerationFactType{Type: v.kind})
		if err != nil {
			return generationFalse
		}
		expected = append(expected, n)
	}
	equal := func(other generationValue) bool {
		switch v.kind {
		case "number":
			return v.number.Cmp(other.number) == 0
		case "string":
			return v.text == other.text
		case "boolean":
			return v.boolean == other.boolean
		}
		return false
	}
	ok := false
	switch c.Op {
	case "eq":
		ok = equal(expected[0])
	case "in":
		for _, other := range expected {
			if equal(other) {
				ok = true
				break
			}
		}
	case "lte":
		ok = v.number.Cmp(expected[0].number) <= 0
	case "gte":
		ok = v.number.Cmp(expected[0].number) >= 0
	case "between":
		ok = v.number.Cmp(expected[0].number) >= 0 && v.number.Cmp(expected[1].number) <= 0
	}
	if ok {
		return generationTrue
	}
	return generationFalse
}

func evaluateGenerationCondition(c GenerationCondition, e generationEnvironment, params map[string]GenerationFactType) (generationTruth, []GenerationRuleObservation, error) {
	var observations []GenerationRuleObservation
	if c.Op == "all" || c.Op == "any" || c.Op == "not" {
		seenTrue, seenFalse, seenUnknown := false, false, false
		for _, child := range c.Children {
			truth, obs, err := evaluateGenerationCondition(child, e, params)
			if err != nil {
				return generationUnknown, nil, err
			}
			observations = append(observations, obs...)
			switch truth {
			case generationTrue:
				seenTrue = true
			case generationFalse:
				seenFalse = true
			default:
				seenUnknown = true
			}
		}
		switch c.Op {
		case "all":
			if seenFalse {
				return generationFalse, observations, nil
			}
			if !seenUnknown {
				return generationTrue, observations, nil
			}
		case "any":
			if seenTrue {
				return generationTrue, observations, nil
			}
			if !seenUnknown {
				return generationFalse, observations, nil
			}
		case "not":
			if seenTrue {
				return generationFalse, observations, nil
			}
			if seenFalse {
				return generationTrue, observations, nil
			}
		}
		return generationUnknown, observations, nil
	}
	values, err := generationResolve(c.Ref, e, params)
	if err != nil {
		return generationUnknown, nil, err
	}
	result := generationTrue
	for _, v := range values {
		if v.state == "not_applicable" && c.Op != "exists" {
			return generationUnknown, nil, fmt.Errorf("%w: inapplicable generation fact comparison", ErrCapability)
		}
		truth := generationCompare(v, c)
		if truth == generationFalse {
			result = generationFalse
		} else if truth == generationUnknown && result == generationTrue {
			result = generationUnknown
		}
		observations = append(observations, GenerationRuleObservation{Ref: c.Ref, Index: v.index, State: v.state, Actual: generationActual(v), Truncated: v.kind == "string" && len(v.text) > 1024, Expected: cloneGenerationExpected(c.Values)})
	}
	return result, observations, nil
}
func generationActual(v generationValue) json.RawMessage {
	if v.state != "known" {
		return nil
	}
	var raw []byte
	switch v.kind {
	case "number":
		if v.number.IsInt() {
			return json.RawMessage(v.number.Num().String())
		}
		raw, _ = json.Marshal(v.number.RatString())
	case "boolean":
		if v.boolean {
			return json.RawMessage("true")
		}
		return json.RawMessage("false")
	case "string":
		text := v.text
		if len(text) > 1024 {
			text = text[:1024]
			for !utf8.ValidString(text) {
				text = text[:len(text)-1]
			}
		}
		raw, _ = json.Marshal(text)
	}
	// 以上分支只编码原生字符串，encoding/json不会返回错误。
	return raw
}
func evaluateGenerationRules(m GenerationPolicyMode, e generationEnvironment) (GenerationVerdict, []GenerationRuleIssue, error) {
	verdict := GenerationAllow
	var issues []GenerationRuleIssue
	for _, rule := range m.Rules {
		truth := generationTrue
		var obs []GenerationRuleObservation
		if rule.When != nil {
			v, o, err := evaluateGenerationCondition(*rule.When, e, m.Parameters)
			if err != nil {
				return "", nil, err
			}
			truth = v
			obs = o
		}
		if truth == generationFalse {
			continue
		}
		check, o, err := evaluateGenerationCondition(rule.Assert, e, m.Parameters)
		if err != nil {
			return "", nil, err
		}
		obs = append(obs, o...)
		// 未知前提与已成立结论构成真蕴含，其余未知前提需要补事实。
		if truth == generationUnknown && check != generationTrue {
			check = generationUnknown
		}
		if check == generationTrue {
			continue
		}
		code := "constraint_failed"
		if check == generationUnknown {
			code = "needs_facts"
			if verdict == GenerationAllow {
				verdict = GenerationNeedsFacts
			}
		} else {
			verdict = GenerationDeny
		}
		issues = append(issues, GenerationRuleIssue{Mode: m.Definition.Key, RuleID: rule.ID, Code: code, Observations: obs})
	}
	return verdict, issues, nil
}

func cloneGenerationExpected(values []json.RawMessage) []json.RawMessage {
	result := make([]json.RawMessage, len(values))
	for i, v := range values {
		result[i] = slices.Clone(v)
	}
	return result
}
