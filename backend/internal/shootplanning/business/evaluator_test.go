package business

import (
	"errors"
	"testing"
	"time"
)

func intPtr(value int) *int { return &value }

func TestEvaluateOrderDefaultRulesUnknownAndZero(t *testing.T) {
	input := OrderEvaluationInput{
		Public: PublicInputs{PlannedLookCount: intPtr(2), CurrentShotCount: 8},
		Facts: Facts{
			RentedLocationCount:      intPtr(1),
			AssistantCount:           intPtr(1),
			RetouchedPhotoCount:      intPtr(18),
			EstimatedDurationMinutes: intPtr(420),
		},
		Rules:     DefaultRuleProfile(),
		BasePrice: intPtr(268000),
	}

	result, err := EvaluateOrder(input)
	if err != nil {
		t.Fatalf("evaluate order: %v", err)
	}
	if result.ProposedTotal == nil || *result.ProposedTotal != 316000 {
		t.Fatalf("proposed total = %v, want 316000", result.ProposedTotal)
	}
	if len(result.Lines) != 4 {
		t.Fatalf("lines = %d, want 4", len(result.Lines))
	}
	wantAmounts := []*int{intPtr(30000), intPtr(18000), nil, intPtr(0)}
	for index, want := range wantAmounts {
		got := result.Lines[index].Amount
		if want == nil {
			if got != nil {
				t.Fatalf("line %d amount = %v, want nil", index, *got)
			}
			continue
		}
		if got == nil || *got != *want {
			t.Fatalf("line %d amount = %v, want %d", index, got, *want)
		}
	}
	wantWarnings := []Warning{WarningUnknownRate, WarningUnknownLinesExcluded}
	if len(result.Warnings) != len(wantWarnings) {
		t.Fatalf("warnings = %v, want %v", result.Warnings, wantWarnings)
	}
	for index := range wantWarnings {
		if result.Warnings[index] != wantWarnings[index] {
			t.Fatalf("warnings = %v, want %v", result.Warnings, wantWarnings)
		}
	}

	zero := input
	zero.Facts.AssistantCount = intPtr(0)
	zeroResult, err := EvaluateOrder(zero)
	if err != nil {
		t.Fatalf("evaluate explicit zero: %v", err)
	}
	if zeroResult.Lines[2].Amount == nil || *zeroResult.Lines[2].Amount != 0 {
		t.Fatalf("assistant zero amount = %v, want 0", zeroResult.Lines[2].Amount)
	}
	if len(zeroResult.Warnings) != 0 {
		t.Fatalf("assistant zero warnings = %v, want none", zeroResult.Warnings)
	}
}

func TestEvaluateOrderUnknownBaseAndAbsoluteZero(t *testing.T) {
	input := OrderEvaluationInput{
		Public: PublicInputs{PlannedLookCount: intPtr(1), CurrentShotCount: 0},
		Facts: Facts{
			RentedLocationCount: intPtr(0),
			AssistantCount:      intPtr(0),
			RetouchedPhotoCount: intPtr(0),
		},
		Rules: DefaultRuleProfile(),
	}

	result, err := EvaluateOrder(input)
	if err != nil {
		t.Fatalf("evaluate unknown base: %v", err)
	}
	if result.ProposedTotal != nil || result.CalculationMode != CalculationDeltaFromBase {
		t.Fatalf("unknown base result = %#v", result)
	}

	input.AbsoluteTargetPrice = intPtr(0)
	result, err = EvaluateOrder(input)
	if err != nil {
		t.Fatalf("evaluate absolute zero: %v", err)
	}
	if result.ProposedTotal == nil || *result.ProposedTotal != 0 || result.CalculationMode != CalculationAbsoluteTarget {
		t.Fatalf("absolute zero result = %#v", result)
	}
}

func TestEvaluateOrderRejectsOverflow(t *testing.T) {
	rules := DefaultRuleProfile()
	rules.ExtraLookUnitAmount = intPtr(MaxMoney)
	_, err := EvaluateOrder(OrderEvaluationInput{
		Public:    PublicInputs{PlannedLookCount: intPtr(100000), CurrentShotCount: 0},
		Facts:     Facts{RentedLocationCount: intPtr(0), AssistantCount: intPtr(0), RetouchedPhotoCount: intPtr(0)},
		Rules:     rules,
		BasePrice: intPtr(0),
	})
	if !errors.Is(err, ErrCalculationOverflow) {
		t.Fatalf("overflow error = %v, want %v", err, ErrCalculationOverflow)
	}
}

func TestEvaluateOrderPreservesUnknownIncludedThreshold(t *testing.T) {
	rules := DefaultRuleProfile()
	rules.IncludedLookCount = nil
	result, err := EvaluateOrder(OrderEvaluationInput{
		Public: PublicInputs{PlannedLookCount: intPtr(2), CurrentShotCount: 0},
		Facts: Facts{
			RentedLocationCount: intPtr(0),
			AssistantCount:      intPtr(0),
			RetouchedPhotoCount: intPtr(0),
		},
		Rules:     rules,
		BasePrice: intPtr(100),
	})
	if err != nil {
		t.Fatalf("evaluate unknown included threshold: %v", err)
	}
	if result.Lines[0].Quantity != nil || result.Lines[0].Amount != nil {
		t.Fatalf("unknown included threshold must keep line unknown: %+v", result.Lines[0])
	}
	wantWarnings := []Warning{WarningUnknownSource, WarningUnknownLinesExcluded}
	if len(result.Warnings) != len(wantWarnings) {
		t.Fatalf("warnings = %v, want %v", result.Warnings, wantWarnings)
	}
	for index := range wantWarnings {
		if result.Warnings[index] != wantWarnings[index] {
			t.Fatalf("warnings = %v, want %v", result.Warnings, wantWarnings)
		}
	}
}

func TestEffectiveRuleCanonicalHashAndUnknownKeys(t *testing.T) {
	rules, err := effectiveRules([]byte(`{"included_look_count":null,"assistant_unit_amount":0}`), 7)
	if err != nil {
		t.Fatalf("effective rules: %v", err)
	}
	if rules.Profile.IncludedLookCount != nil {
		t.Fatalf("included look override null was not preserved: %+v", rules.Profile)
	}
	if rules.Profile.AssistantUnitAmount == nil || *rules.Profile.AssistantUnitAmount != 0 {
		t.Fatalf("assistant override zero was not preserved: %+v", rules.Profile)
	}
	wantVersion := "planning-business-v1:7:7e07da7a3225b45db197cd6dd150f3d11615272b000536abf6287370b0104005"
	if rules.RuleVersion != wantVersion {
		t.Fatalf("rule version = %q, want %q", rules.RuleVersion, wantVersion)
	}
	wantUnknown := []string{"extra_shot_unit_amount", "included_look_count", "included_shot_count"}
	if len(rules.UnknownKeys) != len(wantUnknown) {
		t.Fatalf("unknown keys = %v, want %v", rules.UnknownKeys, wantUnknown)
	}
	for index := range wantUnknown {
		if rules.UnknownKeys[index] != wantUnknown[index] {
			t.Fatalf("unknown keys = %v, want %v", rules.UnknownKeys, wantUnknown)
		}
	}
}

func TestEvaluateDurationDistinguishesUnknownZeroAndValue(t *testing.T) {
	start := time.Date(2026, time.August, 16, 1, 30, 0, 0, time.UTC)

	for _, test := range []struct {
		name    string
		minutes *int
		wantErr error
		wantEnd time.Time
	}{
		{name: "unknown", wantErr: ErrDurationUnknown},
		{name: "zero", minutes: intPtr(0), wantErr: ErrDurationNotPositive},
		{name: "seven hours", minutes: intPtr(420), wantEnd: start.Add(7 * time.Hour)},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := EvaluateDuration(test.minutes, &start)
			if test.wantErr != nil {
				if !errors.Is(err, test.wantErr) {
					t.Fatalf("error = %v, want %v", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("evaluate duration: %v", err)
			}
			if result.ProposedEndAt == nil || !result.ProposedEndAt.Equal(test.wantEnd) {
				t.Fatalf("proposed end = %v, want %v", result.ProposedEndAt, test.wantEnd)
			}
		})
	}
}

func TestCanonicalTargetFingerprints(t *testing.T) {
	price := 268000
	orderHash, orderFrame, err := FingerprintOrderTarget(OrderTarget{
		ID: "ord_1", CustomerID: "cus_1", Status: "consulting", Price: &price,
	})
	if err != nil {
		t.Fatalf("fingerprint order: %v", err)
	}
	wantOrderFrame := `{"schema":"order-business-target-v1","id":"ord_1","customer_id":"cus_1","package_id":null,"status":"consulting","price":268000}`
	if string(orderFrame) != wantOrderFrame || orderHash != "ae7389b04487e3656ef88e95daaba71715b268d23c2d14529d33f02fdb55300a" {
		t.Fatalf("order fingerprint = %s %s", orderHash, orderFrame)
	}

	start := time.Date(2026, time.August, 16, 1, 30, 0, 0, time.UTC)
	end := time.Date(2026, time.August, 16, 8, 30, 0, 0, time.UTC)
	slotHash, slotFrame, err := FingerprintScheduleTarget(ScheduleTarget{
		ID: "slot_1", Type: "shoot", OrderID: "ord_1", StartsAt: start, EndsAt: end,
	})
	if err != nil {
		t.Fatalf("fingerprint schedule: %v", err)
	}
	wantSlotFrame := `{"schema":"schedule-slot-business-target-v1","id":"slot_1","type":"shoot","order_id":"ord_1","start_at":"2026-08-16T01:30:00Z","end_at":"2026-08-16T08:30:00Z"}`
	if string(slotFrame) != wantSlotFrame || slotHash != "11fa3f0f96567e0c0448ebb17acb761a80056d2482f558bfb63926b7940c89a9" {
		t.Fatalf("slot fingerprint = %s %s", slotHash, slotFrame)
	}
}
