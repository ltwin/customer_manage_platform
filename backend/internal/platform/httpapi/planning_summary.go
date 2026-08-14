package httpapi

import (
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning/crm"
)

func toAPIPlanningSummary(summary *crm.Summary) *PlanningSummary {
	if summary == nil || summary.PlanCount < 1 || summary.PrimaryPlanID == "" {
		return nil
	}
	result := PlanningSummary{
		PlanCount:       summary.PlanCount,
		ActivePlanCount: summary.ActivePlanCount,
	}
	status := ShootPlanStatus(summary.PrimaryStatus)
	result.PrimaryPlan = &PlanningSummaryPrimaryPlan{
		Id:                      summary.PrimaryPlanID,
		Title:                   summary.PrimaryTitle,
		Status:                  status,
		ShotCount:               summary.ShotCount,
		ReadinessUncheckedCount: summary.UncheckedCount,
	}
	if summary.WindowStartsAt != nil && summary.WindowEndsAt != nil && summary.WindowTimezone != nil && summary.WindowSource != nil {
		source := PlanningSummaryExecutionWindowSource(*summary.WindowSource)
		result.ExecutionWindow = &PlanningSummaryExecutionWindow{
			StartsAt: *summary.WindowStartsAt,
			EndsAt:   *summary.WindowEndsAt,
			Timezone: *summary.WindowTimezone,
			Source:   source,
		}
	}
	if summary.LinkWarning != nil && *summary.LinkWarning != "" {
		warning := PlanningSummaryLinkWarning(*summary.LinkWarning)
		result.LinkWarning = &warning
	}
	return &result
}
