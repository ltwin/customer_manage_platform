package llmgateway

import (
	"encoding/json"
	"errors"
)

const GenerationPolicyEngineVersion = "generation-policy-v1"

var ErrGenerationFactsUnavailable = errors.New("generation facts unavailable")

// GenerationRatio 使用有界整数表达精确比例；价格的分子单位是币种微单位。
type GenerationRatio struct {
	Numerator   int64 `json:"numerator"`
	Denominator int64 `json:"denominator"`
}

// GenerationPolicyTarget 来自受信Adapter注册，不从外部请求或规则源反向生成。
// Usage声明实际计量维度和单位；没有硬上限保证的维度将UpperBound留空。
// 规则编译与报价不是派发许可，运行时仍须通过Gateway最终准入。
type GenerationPolicyTarget struct {
	ModelKey       string                               `json:"model_key"`
	ModelRevision  string                               `json:"model_revision"`
	DeploymentID   string                               `json:"deployment_id"`
	AdapterVersion string                               `json:"adapter_version"`
	Modes          []string                             `json:"modes"`
	Usage          map[string]GenerationUsageDefinition `json:"usage"`
}

// GenerationUsageDefinition 明确区分不支持的维度与支持但缺少硬上界的维度。
// UpperBound的单位必须与Unit相同，由Adapter能力实现保证，不能由规则源自报。
type GenerationUsageDefinition struct {
	Unit       string `json:"unit"`
	UpperBound *int64 `json:"upper_bound,omitempty"`
}

type GenerationPolicyDefinition struct {
	EngineVersion  string                      `json:"engine_version"`
	Version        string                      `json:"version"`
	Profile        GenerationProfileDefinition `json:"profile"`
	DeploymentID   string                      `json:"deployment_id"`
	AdapterVersion string                      `json:"adapter_version"`
	Currency       string                      `json:"currency"`
	Modes          []GenerationPolicyMode      `json:"modes"`
}

type GenerationPolicyMode struct {
	Definition GenerationModeDefinition      `json:"definition"`
	Parameters map[string]GenerationFactType `json:"parameters,omitempty"`
	Defaults   map[string]json.RawMessage    `json:"defaults,omitempty"`
	Output     GenerationOutputPolicy        `json:"output"`
	Rules      []GenerationConstraint        `json:"rules,omitempty"`
	Pricing    []GenerationPriceComponent    `json:"pricing"`
}

// GenerationFactType 将参数的类型和单位绑定到模式schema；第一版引用顶层标量参数。
type GenerationFactType struct {
	Type string `json:"type"`
	Unit string `json:"unit,omitempty"`
}

// GenerationValueRef 只允许注册的事实来源；references按实际发送条目计数，不去重。
// each逐项检查；其他聚合返回单值。单位必须与事实声明精确一致。
type GenerationValueRef struct {
	Source    string         `json:"source"`
	Field     string         `json:"field,omitempty"`
	Aggregate string         `json:"aggregate,omitempty"`
	Kind      GenerationKind `json:"kind,omitempty"`
	Role      string         `json:"role,omitempty"`
	Unit      string         `json:"unit,omitempty"`
}

// GenerationCondition 是封闭表达式树，不接受脚本或任意函数名。
// all/any/not使用Children；叶节点使用Ref和Values，exists不带Values。
type GenerationCondition struct {
	Op       string                `json:"op"`
	Children []GenerationCondition `json:"children,omitempty"`
	Ref      GenerationValueRef    `json:"ref,omitempty"`
	Values   []json.RawMessage     `json:"values,omitempty"`
}

type GenerationConstraint struct {
	ID     string               `json:"id"`
	When   *GenerationCondition `json:"when,omitempty"`
	Assert GenerationCondition  `json:"assert"`
}

type GenerationPriceComponent struct {
	ID            string                    `json:"id"`
	Estimate      GenerationValueRef        `json:"estimate"`
	Usage         string                    `json:"usage"`
	Unit          string                    `json:"unit"`
	Divisor       int64                     `json:"divisor"`
	QuantityRound string                    `json:"quantity_round"`
	Tiers         []GenerationPriceTier     `json:"tiers"`
	Modifiers     []GenerationPriceModifier `json:"modifiers,omitempty"`
	MinMicros     int64                     `json:"min_micros,omitempty"`
}

type GenerationPriceTier struct {
	ID       string               `json:"id"`
	When     *GenerationCondition `json:"when,omitempty"`
	Fallback bool                 `json:"fallback,omitempty"`
	Rate     GenerationRatio      `json:"rate"`
}

// GenerationPriceModifier 所有命中的乘数相乘；不使用隐式优先级。
type GenerationPriceModifier struct {
	ID     string              `json:"id"`
	When   GenerationCondition `json:"when"`
	Factor GenerationRatio     `json:"factor"`
}

type GenerationVerdict string

const (
	GenerationAllow      GenerationVerdict = "allow"
	GenerationDeny       GenerationVerdict = "deny"
	GenerationNeedsFacts GenerationVerdict = "needs_facts"
)

type GenerationRuleIssue struct {
	Mode         string                      `json:"mode"`
	RuleID       string                      `json:"rule_id"`
	Code         string                      `json:"code"`
	Observations []GenerationRuleObservation `json:"observations,omitempty"`
}

// GenerationRuleObservation 只暴露规则选中的事实，不含素材URL或凭证。
type GenerationRuleObservation struct {
	Ref       GenerationValueRef `json:"ref"`
	Index     *int               `json:"index,omitempty"`
	State     string             `json:"state"`
	Actual    json.RawMessage    `json:"actual,omitempty"`
	Truncated bool               `json:"truncated,omitempty"`
	Expected  []json.RawMessage  `json:"expected,omitempty"`
}

type GenerationPolicyResult struct {
	Verdict  GenerationVerdict
	Issues   []GenerationRuleIssue
	Prepared PreparedPolicyGeneration
}

type GenerationPriceLine struct {
	Component  string   `json:"component"`
	Tier       string   `json:"tier"`
	Modifiers  []string `json:"modifiers,omitempty"`
	Quantity   int64    `json:"quantity"`
	Unit       string   `json:"unit"`
	CostMicros int64    `json:"cost_micros"`
}

type GenerationPrice struct {
	Currency    string                `json:"currency"`
	TotalMicros int64                 `json:"total_micros"`
	Lines       []GenerationPriceLine `json:"lines"`
}

type GenerationQuote struct {
	Currency         string                `json:"currency"`
	EstimateMicros   int64                 `json:"estimate_micros"`
	UpperBoundMicros *int64                `json:"upper_bound_micros,omitempty"`
	Lines            []GenerationPriceLine `json:"lines"`
}
