package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

func (h *handlers) GetShootPlanAssignmentReminders(c *gin.Context, id Id) {
	scope, ok := h.reminderScope(c)
	if !ok {
		return
	}
	if h.reminders == nil {
		abortError(c, http.StatusNotFound, CodeNotFound, "资源不存在")
		return
	}
	view, err := h.reminders.GetPlanAssignmentReminders(c.Request.Context(), scope, string(id))
	if h.abortReminderError(c, err) {
		return
	}
	groups := make([]PlanAssignmentReminderGroup, 0, len(view.Groups))
	for _, g := range view.Groups {
		modes := make([]PlanAssignmentReminderDeliveryMode, 0, len(g.DeliveryModes))
		for _, mode := range g.DeliveryModes {
			modes = append(modes, PlanAssignmentReminderDeliveryMode(mode))
		}
		groups = append(groups, PlanAssignmentReminderGroup{
			GroupId:        g.GroupID,
			ReminderId:     g.ReminderID,
			ReminderStatus: ReminderStatus(g.ReminderStatus),
			DueDate:        openapi_types.Date{Time: g.DueDate},
			ItemCount:      g.ItemCount,
			SlotId:         g.SlotID,
			Content:        g.Content,
			RecipientKind:  PlanAssignmentReminderRecipientKind(g.RecipientKind),
			DeliveryModes:  modes,
		})
	}
	c.JSON(http.StatusOK, PlanAssignmentReminderView{
		PlanId:                 &view.PlanID,
		UnscheduledSourceCount: view.UnscheduledSourceCount,
		Groups:                 groups,
	})
}
