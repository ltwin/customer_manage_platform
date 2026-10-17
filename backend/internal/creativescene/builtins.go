package creativescene

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"
)

// builtinsJSON is the only copy of the catalog; make generate copies it to
// the frontend, which builds the geometry and the parameter panel from it.
//
//go:embed builtins.json
var builtinsJSON []byte

type paramSpec struct {
	Type    string   `json:"type"`
	Default any      `json:"default"`
	Min     *float64 `json:"min,omitempty"`
	Max     *float64 `json:"max,omitempty"`
	Step    *float64 `json:"step,omitempty"`
	Unit    string   `json:"unit,omitempty"`
	Options []string `json:"options,omitempty"`
	Label   string   `json:"label,omitempty"`
}

type builtinSpec struct {
	Builtin string               `json:"builtin"`
	Label   string               `json:"label"`
	Mount   string               `json:"mount"`
	Params  map[string]paramSpec `json:"params"`
}

var (
	versionedID  = regexp.MustCompile(`^[a-z0-9_]+@[1-9][0-9]*$`)
	colorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	builtins     = loadBuiltins(builtinsJSON)
)

// loadBuiltins panics on a malformed catalog: it ships with the binary, so a
// bad entry is a build mistake rather than bad input.
func loadBuiltins(raw []byte) map[string]builtinSpec {
	var list []builtinSpec
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	dec.UseNumber()
	if err := dec.Decode(&list); err != nil {
		panic(fmt.Sprintf("creativescene: builtins.json: %v", err))
	}
	out := make(map[string]builtinSpec, len(list))
	for _, b := range list {
		if !versionedID.MatchString(b.Builtin) || b.Label == "" || len(b.Params) == 0 || !slices.Contains([]string{"floor", "wall", "ceiling", "free"}, b.Mount) {
			panic(fmt.Sprintf("creativescene: builtin %q is malformed", b.Builtin))
		}
		if _, dup := out[b.Builtin]; dup {
			panic(fmt.Sprintf("creativescene: builtin %q listed twice", b.Builtin))
		}
		for name, p := range b.Params {
			numeric := p.Type == "number" || p.Type == "integer"
			if numeric && (p.Min == nil || p.Max == nil || *p.Min > *p.Max) || p.Type == "enum" && len(p.Options) == 0 {
				panic(fmt.Sprintf("creativescene: %s.%s has no valid range", b.Builtin, name))
			}
			v, err := p.value(p.Default)
			if err != nil {
				panic(fmt.Sprintf("creativescene: %s.%s default: %v", b.Builtin, name, err))
			}
			p.Default = v
			b.Params[name] = p
		}
		out[b.Builtin] = b
	}
	return out
}

// value checks one parameter value and returns it in canonical form: float64
// for number and integer, bool, or string.
func (p paramSpec) value(v any) (any, error) {
	switch p.Type {
	case "number", "integer":
		f, ok := number(v)
		if !ok {
			return nil, errors.New("须为数值")
		}
		if p.Type == "integer" && f != math.Trunc(f) {
			return nil, errors.New("须为整数")
		}
		if f < *p.Min || f > *p.Max {
			return nil, fmt.Errorf("须在 %g 到 %g 之间", *p.Min, *p.Max)
		}
		return f, nil
	case "boolean":
		if b, ok := v.(bool); ok {
			return b, nil
		}
		return nil, errors.New("须为布尔值")
	case "enum":
		if s, ok := v.(string); ok && slices.Contains(p.Options, s) {
			return s, nil
		}
		return nil, errors.New("不是可选值")
	case "color":
		if s, ok := v.(string); ok && colorPattern.MatchString(s) {
			return s, nil
		}
		return nil, errors.New("须为 #rrggbb 颜色")
	}
	return nil, fmt.Errorf("未知参数类型 %q", p.Type)
}

func number(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, !math.IsNaN(n) && !math.IsInf(n, 0)
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	}
	return 0, false
}

func lookupBuiltin(id string) (builtinSpec, error) {
	b, ok := builtins[id]
	if !ok {
		return builtinSpec{}, fmt.Errorf("未知基础体 %q", id)
	}
	return b, nil
}

// mergeParams lays patch over base, checking every patched key against the
// catalog. Base is the defaults when adding and the current values when
// updating, so the result is always complete.
func (b builtinSpec) mergeParams(base, patch map[string]any) (map[string]any, error) {
	out := make(map[string]any, len(b.Params))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range patch {
		p, ok := b.Params[k]
		if !ok {
			return nil, fmt.Errorf("%s 没有参数 %q", b.Builtin, k)
		}
		canonical, err := p.value(v)
		if err != nil {
			return nil, fmt.Errorf("参数 %s %w", k, err)
		}
		out[k] = canonical
	}
	return out, nil
}

func (b builtinSpec) defaults() map[string]any {
	out := make(map[string]any, len(b.Params))
	for k, p := range b.Params {
		out[k] = p.Default
	}
	return out
}

// checkParams is S5 for a stored snapshot: every key known, none missing,
// every value in range.
func (b builtinSpec) checkParams(params map[string]any) error {
	for k, v := range params {
		p, ok := b.Params[k]
		if !ok {
			return fmt.Errorf("%s 没有参数 %q", b.Builtin, k)
		}
		if _, err := p.value(v); err != nil {
			return fmt.Errorf("参数 %s %w", k, err)
		}
	}
	for k := range b.Params {
		if _, ok := params[k]; !ok {
			return fmt.Errorf("缺少参数 %s", k)
		}
	}
	return nil
}
