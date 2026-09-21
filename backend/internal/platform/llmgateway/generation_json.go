package llmgateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"
)

// 上限覆盖最大Prompt的六倍转义、64份有界参考、参数/schema和冻结元数据。
const maxGenerationJSONBytes = 6*maxTextBytes + (256 << 10)

// DecodeGenerationRequest 只解码公共信封，不执行任何模型或模式参数规则。
func DecodeGenerationRequest(raw []byte) (GenerationRequest, error) {
	var r GenerationRequest
	if err := decodeGenerationJSON(raw, &r); err != nil {
		return GenerationRequest{}, err
	}
	return normalizeGenerationEnvelope(r)
}

func decodeGenerationJSON(raw []byte, target any) error {
	return decodeGenerationJSONLimit(raw, target, maxGenerationJSONBytes)
}

func decodeGenerationJSONLimit(raw []byte, target any, limit int) error {
	if err := checkGenerationJSON(raw, limit); err != nil {
		return err
	}
	if err := generationJSONFields(raw, reflect.TypeOf(target).Elem()); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("%w: generation JSON fields", ErrValidation)
	}
	return nil
}

func checkGenerationJSON(raw []byte, limit int) error {
	if len(raw) > limit || !json.Valid(raw) || !generationJSONUnicode(raw) {
		return fmt.Errorf("%w: generation JSON", ErrValidation)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := generationJSONValue(dec, 0); err != nil {
		return fmt.Errorf("%w: ambiguous generation JSON", ErrValidation)
	}
	return nil
}

// generationJSONObject 保留数字字面值的精度，只规范键序与JSON编码。
func generationJSONObject(raw []byte, limit int) (json.RawMessage, error) {
	if err := checkGenerationJSON(raw, limit); err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var object map[string]any
	if err := dec.Decode(&object); err != nil || object == nil {
		return nil, fmt.Errorf("%w: generation JSON object", ErrValidation)
	}
	canonical, err := json.Marshal(object)
	if err != nil || len(canonical) > limit {
		return nil, fmt.Errorf("%w: generation JSON object size", ErrValidation)
	}
	return canonical, nil
}

// 固定信封按类型的json标签精确匹配；RawMessage内部名称交给对应模式schema。
func generationJSONFields(raw []byte, t reflect.Type) error {
	if t == reflect.TypeFor[json.RawMessage]() {
		return nil
	}
	switch t.Kind() {
	case reflect.Pointer:
		return generationJSONFields(raw, t.Elem())
	case reflect.Map:
		var entries map[string]json.RawMessage
		if err := json.Unmarshal(raw, &entries); err != nil {
			return fmt.Errorf("%w: generation JSON map", ErrValidation)
		}
		for _, value := range entries {
			if err := generationJSONFields(value, t.Elem()); err != nil {
				return err
			}
		}
	case reflect.Struct:
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			return fmt.Errorf("%w: generation JSON object fields", ErrValidation)
		}
		allowed := make(map[string]reflect.Type, t.NumField())
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			name := strings.Split(f.Tag.Get("json"), ",")[0]
			if name != "" && name != "-" {
				allowed[name] = f.Type
			}
		}
		for name, value := range fields {
			field, ok := allowed[name]
			if !ok {
				return fmt.Errorf("%w: generation JSON field name", ErrValidation)
			}
			if err := generationJSONFields(value, field); err != nil {
				return err
			}
		}
	case reflect.Slice:
		var values []json.RawMessage
		if err := json.Unmarshal(raw, &values); err != nil {
			return fmt.Errorf("%w: generation JSON array", ErrValidation)
		}
		for _, value := range values {
			if err := generationJSONFields(value, t.Elem()); err != nil {
				return err
			}
		}
	}
	return nil
}

// generationJSONValue 检查所有层级的重复键与null，不限制模型参数的命名风格。
func generationJSONValue(dec *json.Decoder, depth int) error {
	if depth > 32 {
		return ErrValidation
	}
	token, err := dec.Token()
	if err != nil {
		return err
	}
	if token == nil {
		return ErrValidation
	}
	delimiter, compound := token.(json.Delim)
	if !compound {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]bool)
		for dec.More() {
			token, err := dec.Token()
			if err != nil {
				return err
			}
			key, ok := token.(string)
			if !ok || seen[key] {
				return ErrValidation
			}
			seen[key] = true
			if err := generationJSONValue(dec, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for dec.More() {
			if err := generationJSONValue(dec, depth+1); err != nil {
				return err
			}
		}
	default:
		return ErrValidation
	}
	// json.Valid已确认括号匹配，这里只消费当前容器的结束标记。
	_, err = dec.Token()
	return err
}

// generationJSONUnicode 在标准解码器替换损坏字符之前校验原字节和代理对。
// 前置json.Valid已检查转义语法；跳过双反斜线，保留字面量“\ud800”等合法文本。
func generationJSONUnicode(raw []byte) bool {
	if !utf8.Valid(raw) {
		return false
	}
	for i := 0; i < len(raw); {
		if raw[i] != '\\' {
			i++
			continue
		}
		if raw[i+1] != 'u' {
			i += 2
			continue
		}
		code, err := strconv.ParseUint(string(raw[i+2:i+6]), 16, 16)
		if err != nil {
			return false
		}
		i += 6
		switch {
		case code >= 0xdc00 && code <= 0xdfff:
			return false
		case code >= 0xd800 && code <= 0xdbff:
			if len(raw)-i < 6 || raw[i] != '\\' || raw[i+1] != 'u' {
				return false
			}
			low, err := strconv.ParseUint(string(raw[i+2:i+6]), 16, 16)
			if err != nil || low < 0xdc00 || low > 0xdfff {
				return false
			}
			i += 6
		}
	}
	return true
}
