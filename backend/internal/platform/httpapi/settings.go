package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/oapi-codegen/nullable"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/settings"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning/business"
)

func (h *handlers) GetSettings(c *gin.Context) {
	scope, ok := h.settingsScope(c)
	if !ok {
		return
	}
	if h.settings == nil {
		abortError(c, http.StatusNotFound, CodeNotFound, "资源不存在")
		return
	}
	s, err := h.settings.Get(c.Request.Context(), scope)
	if h.abortSettingsError(c, err) {
		return
	}
	c.JSON(http.StatusOK, toAPISettings(s))
}

func (h *handlers) UpdateSettings(c *gin.Context) {
	scope, ok := h.settingsScope(c)
	if !ok {
		return
	}
	if h.settings == nil {
		abortError(c, http.StatusNotFound, CodeNotFound, "资源不存在")
		return
	}
	var rawBody map[string]json.RawMessage
	if err := c.ShouldBindBodyWith(&rawBody, binding.JSON); err != nil {
		abortError(c, http.StatusBadRequest, CodeValidationFailed, "请求体格式错误")
		return
	}
	var body UpdateSettingsJSONRequestBody
	if err := c.ShouldBindBodyWith(&body, binding.JSON); err != nil {
		abortError(c, http.StatusBadRequest, CodeValidationFailed, "请求体格式错误")
		return
	}
	input := settings.PatchInput{
		Timezone:          body.Timezone,
		BirthdayLeadDays:  body.BirthdayLeadDays,
		FollowUpAfterDays: body.FollowUpAfterDays,
		DigestHour:        body.DigestHour,
		DeliverySLADays:   body.DeliverySlaDays,
	}
	if body.HealthTiers != nil {
		healthTiers := settings.HealthTiers{
			SleepingRatio:       float64(body.HealthTiers.SleepingRatio),
			AtRiskRatio:         float64(body.HealthTiers.AtRiskRatio),
			LostRatio:           float64(body.HealthTiers.LostRatio),
			FallbackCadenceDays: body.HealthTiers.FallbackCadenceDays,
		}
		input.HealthTiers = &healthTiers
	}
	if body.ChurnThresholds != nil {
		entries := make([]settings.ChurnThreshold, 0, len(*body.ChurnThresholds))
		for _, e := range *body.ChurnThresholds {
			entries = append(entries, settings.ChurnThreshold{
				ShootType: string(e.ShootType),
				Days:      e.Days,
			})
		}
		input.ChurnThresholds = &entries
	}
	if rawAvailability, ok := rawBody["availability"]; ok {
		availability, err := settings.DecodeScheduleAvailabilityJSON(rawAvailability)
		if h.abortSettingsError(c, err) {
			return
		}
		input.Availability = &availability
	}
	if body.PlanningBusinessRules != nil {
		input.PlanningBusinessRules = &settings.PlanningBusinessRulesPatch{
			ExpectedRevision: body.PlanningBusinessRules.ExpectedRevision,
			Overrides:        fromAPIPlanningBusinessRuleOverrides(body.PlanningBusinessRules.Overrides),
		}
	}
	s, err := h.settings.Patch(c.Request.Context(), scope, input)
	if h.abortSettingsError(c, err) {
		return
	}
	c.JSON(http.StatusOK, toAPISettings(s))
}

func (h *handlers) settingsScope(c *gin.Context) (store.AccountScope, bool) {
	ac, ok := auth.AccountContextFrom(c.Request.Context())
	if !ok {
		abortError(c, http.StatusUnauthorized, CodeUnauthorized, "未认证")
		return store.AccountScope{}, false
	}
	if h.scopeFactory == nil {
		abortError(c, http.StatusInternalServerError, CodeInternal, "内部错误")
		return store.AccountScope{}, false
	}
	return h.scopeFactory.ScopeFor(ac), true
}

func (h *handlers) abortSettingsError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	var ve settings.ValidationError
	if errors.As(err, &ve) || errors.Is(err, settings.ErrValidation) {
		msg := err.Error()
		if errors.As(err, &ve) {
			msg = ve.Message
		}
		abortError(c, http.StatusBadRequest, CodeValidationFailed, msg)
		return true
	}
	if errors.Is(err, settings.ErrPlanningBusinessRuleRevision) {
		abortError(c, http.StatusConflict, CodePlanRevisionConflict, "经营规则版本已变化，请刷新后重试")
		return true
	}
	_ = c.Error(err)
	return true
}

func toAPISettings(s settings.Settings) Settings {
	thresholds := make([]ChurnThreshold, 0, len(s.ChurnThresholds))
	for _, e := range s.ChurnThresholds {
		thresholds = append(thresholds, ChurnThreshold{
			ShootType: ShootType(e.ShootType),
			Days:      e.Days,
		})
	}
	return Settings{
		Timezone:          s.Timezone,
		BirthdayLeadDays:  s.BirthdayLeadDays,
		FollowUpAfterDays: s.FollowUpAfterDays,
		ChurnThresholds:   thresholds,
		DigestHour:        s.DigestHour,
		DeliverySlaDays:   s.DeliverySLADays,
		HealthTiers: HealthTiers{
			SleepingRatio:       float32(s.HealthTiers.SleepingRatio),
			AtRiskRatio:         float32(s.HealthTiers.AtRiskRatio),
			LostRatio:           float32(s.HealthTiers.LostRatio),
			FallbackCadenceDays: s.HealthTiers.FallbackCadenceDays,
		},
		TelegramChatId:                s.TelegramChatID,
		Availability:                  toAPIScheduleAvailability(s.Availability),
		PlanningBusinessRuleOverrides: toAPIPlanningBusinessRuleOverrides(s.PlanningBusinessRuleOverrides),
		PlanningBusinessRuleRevision:  s.PlanningBusinessRuleRevision,
	}
}

func fromAPIPlanningBusinessRuleOverrides(input PlanningBusinessRuleOverrides) business.RuleOverrides {
	result := make(business.RuleOverrides)
	putNullableRule(result, "included_look_count", input.IncludedLookCount)
	putNullableRule(result, "extra_look_unit_amount", input.ExtraLookUnitAmount)
	putNullableRule(result, "rented_location_unit_amount", input.RentedLocationUnitAmount)
	putNullableRule(result, "assistant_unit_amount", input.AssistantUnitAmount)
	putNullableRule(result, "included_retouched_photo_count", input.IncludedRetouchedPhotoCount)
	putNullableRule(result, "extra_retouch_unit_amount", input.ExtraRetouchUnitAmount)
	putNullableRule(result, "included_shot_count", input.IncludedShotCount)
	putNullableRule(result, "extra_shot_unit_amount", input.ExtraShotUnitAmount)
	return result
}

func putNullableRule(target business.RuleOverrides, key string, value nullable.Nullable[int]) {
	if !value.IsSpecified() {
		return
	}
	if value.IsNull() {
		target[key] = nil
		return
	}
	integer := value.MustGet()
	target[key] = &integer
}

func toAPIPlanningBusinessRuleOverrides(input business.RuleOverrides) PlanningBusinessRuleOverrides {
	var output PlanningBusinessRuleOverrides
	output.IncludedLookCount = apiNullableRule(input, "included_look_count")
	output.ExtraLookUnitAmount = apiNullableRule(input, "extra_look_unit_amount")
	output.RentedLocationUnitAmount = apiNullableRule(input, "rented_location_unit_amount")
	output.AssistantUnitAmount = apiNullableRule(input, "assistant_unit_amount")
	output.IncludedRetouchedPhotoCount = apiNullableRule(input, "included_retouched_photo_count")
	output.ExtraRetouchUnitAmount = apiNullableRule(input, "extra_retouch_unit_amount")
	output.IncludedShotCount = apiNullableRule(input, "included_shot_count")
	output.ExtraShotUnitAmount = apiNullableRule(input, "extra_shot_unit_amount")
	return output
}

func apiNullableRule(input business.RuleOverrides, key string) nullable.Nullable[int] {
	value, exists := input[key]
	if !exists {
		return nullable.Nullable[int]{}
	}
	if value == nil {
		return nullable.NewNullNullable[int]()
	}
	return nullable.NewNullableWithValue(*value)
}

func toAPIScheduleAvailability(value settings.ScheduleAvailability) ScheduleAvailability {
	return ScheduleAvailability{
		Weekly: ScheduleAvailabilityWeekly{
			N1: toAPIScheduleAvailabilityWindow(value.Weekly.Monday),
			N2: toAPIScheduleAvailabilityWindow(value.Weekly.Tuesday),
			N3: toAPIScheduleAvailabilityWindow(value.Weekly.Wednesday),
			N4: toAPIScheduleAvailabilityWindow(value.Weekly.Thursday),
			N5: toAPIScheduleAvailabilityWindow(value.Weekly.Friday),
			N6: toAPIScheduleAvailabilityWindow(value.Weekly.Saturday),
			N7: toAPIScheduleAvailabilityWindow(value.Weekly.Sunday),
		},
		MinOpeningMinutes: value.MinOpeningMinutes,
		TurnaroundMinutes: value.TurnaroundMinutes,
	}
}

func toAPIScheduleAvailabilityWindow(value *settings.ScheduleAvailabilityWindow) nullable.Nullable[ScheduleAvailabilityWindow] {
	if value == nil {
		return nullable.NewNullNullable[ScheduleAvailabilityWindow]()
	}
	return nullable.NewNullableWithValue(ScheduleAvailabilityWindow{Start: value.Start, End: value.End})
}
