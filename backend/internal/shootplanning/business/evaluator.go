// Package business owns private planning facts and explainable business drafts.
package business

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"time"
)

const (
	RuleVersionV1      = "planning-business-v1"
	MaxMoney           = math.MaxInt32
	MaxCount           = 100000
	MaxSmallCount      = 100
	MaxDurationMinutes = 10080
)

var (
	ErrCalculationOverflow = errors.New("business_calculation_overflow")
	ErrDurationUnknown     = errors.New("duration_unknown")
	ErrDurationNotPositive = errors.New("duration_not_positive")
	ErrDurationOutOfRange  = errors.New("duration_out_of_range")
	ErrInvalidBusinessRule = errors.New("invalid_business_rule")
	ErrInvalidBusinessFact = errors.New("invalid_business_fact")
)

type Facts struct {
	RentedLocationCount      *int `json:"rented_location_count"`
	AssistantCount           *int `json:"assistant_count"`
	RetouchedPhotoCount      *int `json:"retouched_photo_count"`
	EstimatedDurationMinutes *int `json:"estimated_duration_minutes"`
}

type PublicInputs struct {
	PlannedLookCount *int `json:"planned_look_count"`
	CurrentShotCount int  `json:"current_shot_count"`
}

type RuleProfile struct {
	IncludedLookCount           *int `json:"included_look_count"`
	ExtraLookUnitAmount         *int `json:"extra_look_unit_amount"`
	RentedLocationUnitAmount    *int `json:"rented_location_unit_amount"`
	AssistantUnitAmount         *int `json:"assistant_unit_amount"`
	IncludedRetouchedPhotoCount *int `json:"included_retouched_photo_count"`
	ExtraRetouchUnitAmount      *int `json:"extra_retouch_unit_amount"`
	IncludedShotCount           *int `json:"included_shot_count"`
	ExtraShotUnitAmount         *int `json:"extra_shot_unit_amount"`
}

func DefaultRuleProfile() RuleProfile {
	return RuleProfile{
		IncludedLookCount:           intPtrValue(1),
		ExtraLookUnitAmount:         intPtrValue(30000),
		RentedLocationUnitAmount:    intPtrValue(18000),
		AssistantUnitAmount:         nil,
		IncludedRetouchedPhotoCount: intPtrValue(12),
		ExtraRetouchUnitAmount:      intPtrValue(0),
		IncludedShotCount:           nil,
		ExtraShotUnitAmount:         nil,
	}
}

type CalculationMode string

const (
	CalculationDeltaFromBase  CalculationMode = "delta_from_base"
	CalculationAbsoluteTarget CalculationMode = "absolute_target"
)

type LineKind string

const (
	LineExtraLook      LineKind = "extra_look"
	LineRentedLocation LineKind = "rented_location"
	LineAssistant      LineKind = "assistant"
	LineExtraRetouch   LineKind = "extra_retouch"
	LineExtraShot      LineKind = "extra_shot"
)

type Warning string

const (
	WarningUnknownSource        Warning = "unknown_source_fact"
	WarningUnknownRate          Warning = "unknown_rate"
	WarningUnknownLinesExcluded Warning = "unknown_adjustment_lines_excluded"
	WarningNoMaterialChange     Warning = "no_material_change"
)

type AdjustmentSourceFact struct {
	Field         string `json:"field"`
	Owner         string `json:"owner"`
	ObservedValue *int   `json:"observed_value"`
	RuleKey       string `json:"rule_key"`
}

type AdjustmentLine struct {
	Kind       LineKind             `json:"kind"`
	Label      string               `json:"label"`
	Quantity   *int                 `json:"quantity"`
	UnitAmount *int                 `json:"unit_amount"`
	Amount     *int                 `json:"amount"`
	SourceFact AdjustmentSourceFact `json:"source_fact"`
}

type OrderEvaluationInput struct {
	Public              PublicInputs
	Facts               Facts
	Rules               RuleProfile
	BasePrice           *int
	AbsoluteTargetPrice *int
}

type OrderEvaluation struct {
	BasePrice           *int             `json:"base_price"`
	CalculationMode     CalculationMode  `json:"calculation_mode"`
	AbsoluteTargetPrice *int             `json:"absolute_target_price"`
	Lines               []AdjustmentLine `json:"lines"`
	ProposedTotal       *int             `json:"proposed_total"`
	Warnings            []Warning        `json:"warnings"`
}

func EvaluateOrder(input OrderEvaluationInput) (OrderEvaluation, error) {
	if err := validateRuleProfile(input.Rules); err != nil {
		return OrderEvaluation{}, err
	}
	if err := ValidateFacts(input.Facts); err != nil {
		return OrderEvaluation{}, err
	}
	if input.Public.PlannedLookCount != nil && (*input.Public.PlannedLookCount < 0 || *input.Public.PlannedLookCount > MaxCount) {
		return OrderEvaluation{}, ErrInvalidBusinessFact
	}
	if input.Public.CurrentShotCount < 0 || input.Public.CurrentShotCount > MaxCount {
		return OrderEvaluation{}, ErrInvalidBusinessFact
	}
	if err := validateMoney(input.BasePrice); err != nil {
		return OrderEvaluation{}, err
	}
	if err := validateMoney(input.AbsoluteTargetPrice); err != nil {
		return OrderEvaluation{}, err
	}

	lines := make([]AdjustmentLine, 0, 5)
	lines = append(lines, thresholdLine(
		LineExtraLook, "超出包含造型", "planned_look_count", "public_plan_scale",
		input.Public.PlannedLookCount, input.Rules.IncludedLookCount,
		input.Rules.ExtraLookUnitAmount, "extra_look_unit_amount",
	))
	lines = append(lines, countLine(
		LineRentedLocation, "付费场地", "rented_location_count", "planning_business_facts",
		input.Facts.RentedLocationCount, input.Rules.RentedLocationUnitAmount, "rented_location_unit_amount",
	))
	lines = append(lines, countLine(
		LineAssistant, "助理", "assistant_count", "planning_business_facts",
		input.Facts.AssistantCount, input.Rules.AssistantUnitAmount, "assistant_unit_amount",
	))
	lines = append(lines, thresholdLine(
		LineExtraRetouch, "超出包含精修", "retouched_photo_count", "planning_business_facts",
		input.Facts.RetouchedPhotoCount, input.Rules.IncludedRetouchedPhotoCount,
		input.Rules.ExtraRetouchUnitAmount, "extra_retouch_unit_amount",
	))
	if input.Rules.IncludedShotCount != nil && input.Rules.ExtraShotUnitAmount != nil {
		lines = append(lines, thresholdLine(
			LineExtraShot, "超出包含镜头", "current_shot_count", "current_shot_aggregate",
			intPtrValue(input.Public.CurrentShotCount), input.Rules.IncludedShotCount,
			input.Rules.ExtraShotUnitAmount, "extra_shot_unit_amount",
		))
	}
	for _, line := range lines {
		if line.Amount != nil && *line.Amount > MaxMoney {
			return OrderEvaluation{}, ErrCalculationOverflow
		}
	}

	warnings := orderedWarnings(lines)
	result := OrderEvaluation{
		BasePrice:           cloneInt(input.BasePrice),
		CalculationMode:     CalculationDeltaFromBase,
		AbsoluteTargetPrice: cloneInt(input.AbsoluteTargetPrice),
		Lines:               lines,
		Warnings:            warnings,
	}
	if input.AbsoluteTargetPrice != nil {
		result.CalculationMode = CalculationAbsoluteTarget
		result.ProposedTotal = cloneInt(input.AbsoluteTargetPrice)
		return result, nil
	}
	if input.BasePrice == nil {
		return result, nil
	}
	total := int64(*input.BasePrice)
	for _, line := range lines {
		if line.Amount == nil {
			continue
		}
		total += int64(*line.Amount)
		if total > MaxMoney {
			return OrderEvaluation{}, ErrCalculationOverflow
		}
	}
	value := int(total)
	result.ProposedTotal = &value
	return result, nil
}

func ValidateFacts(facts Facts) error {
	if !within(facts.RentedLocationCount, 0, MaxSmallCount) ||
		!within(facts.AssistantCount, 0, MaxSmallCount) ||
		!within(facts.RetouchedPhotoCount, 0, MaxCount) ||
		!within(facts.EstimatedDurationMinutes, 0, MaxDurationMinutes) {
		return ErrInvalidBusinessFact
	}
	return nil
}

func validateRuleProfile(rules RuleProfile) error {
	if !within(rules.IncludedLookCount, 0, MaxCount) ||
		!within(rules.IncludedRetouchedPhotoCount, 0, MaxCount) ||
		!within(rules.IncludedShotCount, 0, MaxCount) ||
		validateMoney(rules.ExtraLookUnitAmount) != nil ||
		validateMoney(rules.RentedLocationUnitAmount) != nil ||
		validateMoney(rules.AssistantUnitAmount) != nil ||
		validateMoney(rules.ExtraRetouchUnitAmount) != nil ||
		validateMoney(rules.ExtraShotUnitAmount) != nil {
		return ErrInvalidBusinessRule
	}
	return nil
}

func validateMoney(value *int) error {
	if !within(value, 0, MaxMoney) {
		return ErrInvalidBusinessRule
	}
	return nil
}

func thresholdLine(
	kind LineKind,
	label, field, owner string,
	observed *int,
	included *int,
	unitAmount *int,
	ruleKey string,
) AdjustmentLine {
	line := baseLine(kind, label, field, owner, observed, unitAmount, ruleKey)
	if observed == nil || included == nil {
		return line
	}
	extra := *observed - *included
	if extra < 0 {
		extra = 0
	}
	line.Quantity = intPtrValue(extra)
	line.Amount = multipliedAmount(extra, unitAmount)
	return line
}

func countLine(
	kind LineKind,
	label, field, owner string,
	count, unitAmount *int,
	ruleKey string,
) AdjustmentLine {
	line := baseLine(kind, label, field, owner, count, unitAmount, ruleKey)
	if count == nil {
		return line
	}
	line.Quantity = cloneInt(count)
	line.Amount = multipliedAmount(*count, unitAmount)
	return line
}

func baseLine(
	kind LineKind,
	label, field, owner string,
	observed, unitAmount *int,
	ruleKey string,
) AdjustmentLine {
	return AdjustmentLine{
		Kind:       kind,
		Label:      label,
		UnitAmount: cloneInt(unitAmount),
		SourceFact: AdjustmentSourceFact{
			Field: field, Owner: owner, ObservedValue: cloneInt(observed), RuleKey: ruleKey,
		},
	}
}

func multipliedAmount(quantity int, unitAmount *int) *int {
	if quantity == 0 {
		return intPtrValue(0)
	}
	if unitAmount == nil {
		return nil
	}
	amount := int64(quantity) * int64(*unitAmount)
	if amount > MaxMoney {
		return intPtrValue(MaxMoney + 1)
	}
	return intPtrValue(int(amount))
}

func orderedWarnings(lines []AdjustmentLine) []Warning {
	var unknownSource, unknownRate, excluded bool
	for _, line := range lines {
		if line.Quantity == nil {
			unknownSource = true
			excluded = true
			continue
		}
		if *line.Quantity > 0 && line.UnitAmount == nil {
			unknownRate = true
			excluded = true
		}
	}
	warnings := make([]Warning, 0, 3)
	if unknownSource {
		warnings = append(warnings, WarningUnknownSource)
	}
	if unknownRate {
		warnings = append(warnings, WarningUnknownRate)
	}
	if excluded {
		warnings = append(warnings, WarningUnknownLinesExcluded)
	}
	return warnings
}

type DurationEvaluation struct {
	BasisMinutes  int        `json:"basis_minutes"`
	ProposedEndAt *time.Time `json:"proposed_end_at"`
}

func EvaluateDuration(minutes *int, startsAt *time.Time) (DurationEvaluation, error) {
	if minutes == nil {
		return DurationEvaluation{}, ErrDurationUnknown
	}
	if *minutes == 0 {
		return DurationEvaluation{}, ErrDurationNotPositive
	}
	if *minutes < 0 || *minutes > MaxDurationMinutes {
		return DurationEvaluation{}, ErrDurationOutOfRange
	}
	result := DurationEvaluation{BasisMinutes: *minutes}
	if startsAt != nil {
		end := startsAt.UTC().Add(time.Duration(*minutes) * time.Minute)
		result.ProposedEndAt = &end
	}
	return result, nil
}

type OrderTarget struct {
	ID         string
	CustomerID string
	PackageID  *string
	Status     string
	Price      *int
}

type ScheduleTarget struct {
	ID       string
	Type     string
	OrderID  string
	StartsAt time.Time
	EndsAt   time.Time
}

func FingerprintOrderTarget(target OrderTarget) (string, []byte, error) {
	frame := struct {
		Schema     string  `json:"schema"`
		ID         string  `json:"id"`
		CustomerID string  `json:"customer_id"`
		PackageID  *string `json:"package_id"`
		Status     string  `json:"status"`
		Price      *int    `json:"price"`
	}{
		Schema: "order-business-target-v1", ID: target.ID, CustomerID: target.CustomerID,
		PackageID: target.PackageID, Status: target.Status, Price: target.Price,
	}
	return fingerprintFrame(frame)
}

func FingerprintScheduleTarget(target ScheduleTarget) (string, []byte, error) {
	frame := struct {
		Schema   string `json:"schema"`
		ID       string `json:"id"`
		Type     string `json:"type"`
		OrderID  string `json:"order_id"`
		StartsAt string `json:"start_at"`
		EndsAt   string `json:"end_at"`
	}{
		Schema: "schedule-slot-business-target-v1", ID: target.ID, Type: target.Type,
		OrderID: target.OrderID, StartsAt: target.StartsAt.UTC().Format(time.RFC3339Nano),
		EndsAt: target.EndsAt.UTC().Format(time.RFC3339Nano),
	}
	return fingerprintFrame(frame)
}

func fingerprintFrame(frame any) (string, []byte, error) {
	body, err := json.Marshal(frame)
	if err != nil {
		return "", nil, err
	}
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:]), body, nil
}

func within(value *int, minValue, maxValue int) bool {
	return value == nil || (*value >= minValue && *value <= maxValue)
}

func cloneInt(value *int) *int {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

func intPtrValue(value int) *int { return &value }
