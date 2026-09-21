package llmgateway

import (
	"encoding/json"
	"math/big"
	"strconv"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

// 编译schema与原始数值同时保留，避免float64比较把大整数或小数舍入成另一项合法参数。
type generationSchema struct {
	compiled   *openapi3.Schema
	definition map[string]any
}

// 有界数值表示限制解析成本；超出该范围的精确标识应由模型以字符串参数承载。
func generationRational(n json.Number) (*big.Rat, bool) {
	s := string(n)
	if len(s) > 128 {
		return nil, false
	}
	if at := strings.IndexAny(s, "eE"); at >= 0 {
		exponent, err := strconv.Atoi(s[at+1:])
		if err != nil || exponent < -1024 || exponent > 1024 {
			return nil, false
		}
	}
	return new(big.Rat).SetString(s)
}

func generationSchemaNumbersSupported(value any) bool {
	switch v := value.(type) {
	case json.Number:
		_, ok := generationRational(v)
		return ok
	case map[string]any:
		for _, child := range v {
			if !generationSchemaNumbersSupported(child) {
				return false
			}
		}
	case []any:
		for _, child := range v {
			if !generationSchemaNumbersSupported(child) {
				return false
			}
		}
	}
	return true
}

// 通用schema库负责结构和非数值规则；这里用原始十进制补齐精确数值判断。
func generationExactConstraints(schema map[string]any, value any) bool {
	if enums, ok := schema["enum"].([]any); ok {
		matched := false
		for _, candidate := range enums {
			if generationJSONEqual(candidate, value) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	switch v := value.(type) {
	case json.Number:
		number, ok := generationRational(v)
		if !ok {
			return false
		}
		if schema["type"] == "integer" && !number.IsInt() {
			return false
		}
		for _, key := range []string{"minimum", "maximum"} {
			if raw, ok := schema[key].(json.Number); ok {
				bound, ok := generationRational(raw)
				if !ok {
					return false
				}
				if cmp := number.Cmp(bound); key == "minimum" && cmp < 0 || key == "maximum" && cmp > 0 {
					return false
				}
			}
		}
	case map[string]any:
		properties, ok := schema["properties"].(map[string]any)
		if !ok {
			return true
		}
		for key, child := range v {
			if definition, ok := properties[key].(map[string]any); ok && !generationExactConstraints(definition, child) {
				return false
			}
		}
	case []any:
		items, ok := schema["items"].(map[string]any)
		if !ok {
			return true
		}
		for _, child := range v {
			if !generationExactConstraints(items, child) {
				return false
			}
		}
	}
	return true
}

// 当前不开放format；名称被库认识不等于对应值校验器已经安装。
// 数量/长度约束限定到安全整数范围，防止库内部有符号转换溢出。
func generationSchemaConstraintsSupported(schema map[string]any) bool {
	if _, present := schema["format"]; present {
		return false
	}
	for _, key := range []string{"minLength", "maxLength", "minItems", "maxItems"} {
		if raw, present := schema[key]; present {
			number, ok := raw.(json.Number)
			if !ok {
				return false
			}
			bound, ok := generationRational(number)
			if !ok || !bound.IsInt() || bound.Sign() < 0 || bound.Cmp(new(big.Rat).SetInt64(1<<31-1)) > 0 {
				return false
			}
		}
	}
	for _, pair := range [][2]string{{"minimum", "maximum"}, {"minLength", "maxLength"}, {"minItems", "maxItems"}} {
		minValue, minPresent := schema[pair[0]].(json.Number)
		maxValue, maxPresent := schema[pair[1]].(json.Number)
		if minPresent && maxPresent {
			minNumber, minOK := generationRational(minValue)
			maxNumber, maxOK := generationRational(maxValue)
			if !minOK || !maxOK || minNumber.Cmp(maxNumber) > 0 {
				return false
			}
		}
	}
	if raw, present := schema["enum"]; present {
		enums, ok := raw.([]any)
		if !ok || len(enums) == 0 || len(enums) > 256 {
			return false
		}
		for i, value := range enums {
			for _, earlier := range enums[:i] {
				if generationJSONEqual(earlier, value) {
					return false
				}
			}
		}
	}
	if properties, ok := schema["properties"].(map[string]any); ok {
		for _, raw := range properties {
			child, ok := raw.(map[string]any)
			if !ok || !generationSchemaConstraintsSupported(child) {
				return false
			}
		}
	}
	if items, ok := schema["items"].(map[string]any); ok && !generationSchemaConstraintsSupported(items) {
		return false
	}
	return true
}

func removeGenerationEnums(schema *openapi3.Schema) {
	schema.Enum = nil
	for _, property := range schema.Properties {
		if property.Value != nil {
			removeGenerationEnums(property.Value)
		}
	}
	if schema.Items != nil && schema.Items.Value != nil {
		removeGenerationEnums(schema.Items.Value)
	}
}

// 枚举对象按键集合比较，数组保留顺序，数值使用十进制精确等价。
func generationJSONEqual(left, right any) bool {
	switch l := left.(type) {
	case json.Number:
		r, ok := right.(json.Number)
		if !ok {
			return false
		}
		a, aOK := generationRational(l)
		b, bOK := generationRational(r)
		return aOK && bOK && a.Cmp(b) == 0
	case string:
		r, ok := right.(string)
		return ok && l == r
	case bool:
		r, ok := right.(bool)
		return ok && l == r
	case []any:
		r, ok := right.([]any)
		if !ok || len(l) != len(r) {
			return false
		}
		for i, value := range l {
			if !generationJSONEqual(value, r[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		r, ok := right.(map[string]any)
		if !ok || len(l) != len(r) {
			return false
		}
		for key, value := range l {
			other, ok := r[key]
			if !ok || !generationJSONEqual(value, other) {
				return false
			}
		}
		return true
	default:
		return false
	}
}
