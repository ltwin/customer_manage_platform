package llmgateway

import (
	"fmt"
	"math"
	"math/big"
)

func generationQuantity(ref GenerationValueRef, e generationEnvironment, params map[string]GenerationFactType) (int64, error) {
	values, err := generationResolve(ref, e, params)
	if err != nil {
		return 0, err
	}
	if len(values) != 1 || values[0].state == "unknown" {
		return 0, ErrGenerationFactsUnavailable
	}
	v := values[0]
	if v.state != "known" || v.number == nil || !v.number.IsInt() || !v.number.Num().IsInt64() || v.number.Sign() < 0 {
		return 0, fmt.Errorf("%w: generation quantity", ErrValidation)
	}
	return v.number.Num().Int64(), nil
}
func generationEstimateUsage(m GenerationPolicyMode, e generationEnvironment) (map[string]int64, error) {
	usage := make(map[string]int64, len(m.Pricing))
	for _, c := range m.Pricing {
		q, err := generationQuantity(c.Estimate, e, m.Parameters)
		if err != nil {
			return nil, err
		}
		usage[c.Usage] = q
	}
	return usage, nil
}
func generationCheckUsage(m GenerationPolicyMode, target GenerationPolicyTarget, usage map[string]int64) error {
	if len(usage) > len(m.Pricing) {
		return fmt.Errorf("%w: unexpected generation usage", ErrValidation)
	}
	for _, c := range m.Pricing {
		n, ok := usage[c.Usage]
		if !ok {
			return ErrGenerationFactsUnavailable
		}
		if n < 0 {
			return fmt.Errorf("%w: negative generation usage", ErrValidation)
		}
		if spec := target.Usage[c.Usage]; spec.UpperBound != nil && n > *spec.UpperBound {
			return fmt.Errorf("%w: generation usage bound exceeded", ErrCapability)
		}
	}
	return nil
}
func generationPriceUsage(currency string, m GenerationPolicyMode, e generationEnvironment) (GenerationPrice, error) {
	price := GenerationPrice{Currency: currency}
	for _, c := range m.Pricing {
		var selected, fallback *GenerationPriceTier
		for i := range c.Tiers {
			tier := &c.Tiers[i]
			if tier.Fallback {
				fallback = tier
				continue
			}
			truth, _, err := evaluateGenerationCondition(*tier.When, e, m.Parameters)
			if err != nil {
				return GenerationPrice{}, err
			}
			if truth == generationUnknown {
				return GenerationPrice{}, ErrGenerationFactsUnavailable
			}
			if truth == generationTrue {
				if selected != nil {
					return GenerationPrice{}, fmt.Errorf("%w: ambiguous generation price tier", ErrConflict)
				}
				selected = tier
			}
		}
		if selected == nil {
			selected = fallback
		}
		if selected == nil {
			return GenerationPrice{}, fmt.Errorf("%w: generation price unavailable", ErrCapability)
		}
		rate := generationRate(selected.Rate)
		var modifiers []string
		for _, mod := range c.Modifiers {
			truth, _, err := evaluateGenerationCondition(mod.When, e, m.Parameters)
			if err != nil {
				return GenerationPrice{}, err
			}
			if truth == generationUnknown {
				return GenerationPrice{}, ErrGenerationFactsUnavailable
			}
			if truth == generationTrue {
				rate.Mul(rate, generationRate(mod.Factor))
				modifiers = append(modifiers, mod.ID)
			}
		}
		n, ok := e.usage[c.Usage]
		if !ok {
			return GenerationPrice{}, ErrGenerationFactsUnavailable
		}
		cost, err := generationComponentCost(c, n, rate)
		if err != nil {
			return GenerationPrice{}, err
		}
		if price.TotalMicros > math.MaxInt64-cost {
			return GenerationPrice{}, fmt.Errorf("%w: generation total price overflow", ErrValidation)
		}
		price.TotalMicros += cost
		price.Lines = append(price.Lines, GenerationPriceLine{Component: c.ID, Tier: selected.ID, Modifiers: modifiers, Quantity: n, Unit: c.Unit, CostMicros: cost})
	}
	return price, nil
}

func generationComponentCost(c GenerationPriceComponent, quantity int64, rate *big.Rat) (int64, error) {
	units := new(big.Rat).SetFrac64(quantity, c.Divisor)
	if c.QuantityRound == "ceil" {
		units.SetInt(generationCeil(units))
	}
	amount := new(big.Rat).Mul(units, rate)
	micros := generationCeil(amount)
	if !micros.IsInt64() || micros.Sign() < 0 {
		return 0, fmt.Errorf("%w: generation component price overflow", ErrValidation)
	}
	return max(micros.Int64(), c.MinMicros), nil
}
func generationCeil(r *big.Rat) *big.Int {
	q, rem := new(big.Int), new(big.Int)
	q.QuoRem(r.Num(), r.Denom(), rem)
	if rem.Sign() > 0 {
		q.Add(q, big.NewInt(1))
	}
	return q
}

// 上界取所有价阶最大单价、全部可能增价乘数和每个组件的受信硬上限。
// 这是保守上界，不假定条件同时成立；没有完整硬上限时明确返回nil。
func generationPriceUpperBound(m GenerationPolicyMode, target GenerationPolicyTarget) (*int64, error) {
	total := int64(0)
	for _, c := range m.Pricing {
		spec, ok := target.Usage[c.Usage]
		if !ok || spec.UpperBound == nil {
			return nil, nil
		}
		rate := new(big.Rat)
		for _, tier := range c.Tiers {
			candidate := generationRate(tier.Rate)
			if candidate.Cmp(rate) > 0 {
				rate.Set(candidate)
			}
		}
		for _, mod := range c.Modifiers {
			factor := generationRate(mod.Factor)
			if factor.Cmp(big.NewRat(1, 1)) > 0 {
				rate.Mul(rate, factor)
			}
		}
		cost, err := generationComponentCost(c, *spec.UpperBound, rate)
		if err != nil {
			return nil, err
		}
		if total > math.MaxInt64-cost {
			return nil, fmt.Errorf("%w: generation upper bound overflow", ErrValidation)
		}
		total += cost
	}
	return &total, nil
}
