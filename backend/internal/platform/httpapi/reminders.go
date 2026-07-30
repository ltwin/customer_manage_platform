package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/reminder"
)

func (h *handlers) listRemindersRoute(c *gin.Context) {
	params, ok := bindListRemindersParams(c)
	if !ok {
		return
	}
	h.ListReminders(c, params)
}

func bindListRemindersParams(c *gin.Context) (ListRemindersParams, bool) {
	var params ListRemindersParams
	if status := c.Query("status"); status != "" {
		s := ReminderStatus(status)
		params.Status = &s
	}
	if customerID := c.Query("customer_id"); customerID != "" {
		params.CustomerId = &customerID
	}
	if dueBefore := c.Query("due_before"); dueBefore != "" {
		parsed, err := time.Parse("2006-01-02", dueBefore)
		if err != nil {
			abortError(c, http.StatusBadRequest, CodeValidationFailed, "due_before 格式须为 YYYY-MM-DD")
			return ListRemindersParams{}, false
		}
		d := openapi_types.Date{Time: parsed}
		params.DueBefore = &d
	}
	page, ok := bindOptionalPageParam(c, "page")
	if !ok {
		return ListRemindersParams{}, false
	}
	if page != nil {
		value := Page(*page)
		params.Page = &value
	}
	pageSize, ok := bindOptionalPageParam(c, "page_size")
	if !ok {
		return ListRemindersParams{}, false
	}
	if pageSize != nil {
		value := PageSize(*pageSize)
		params.PageSize = &value
	}
	return params, true
}

func (h *handlers) ListReminders(c *gin.Context, params ListRemindersParams) {
	scope, ok := h.reminderScope(c)
	if !ok {
		return
	}
	if h.reminders == nil {
		abortError(c, http.StatusNotFound, CodeNotFound, "资源不存在")
		return
	}
	filter := reminder.ListFilter{}
	if params.Status != nil {
		filter.Status = string(*params.Status)
	}
	if params.CustomerId != nil {
		filter.CustomerID = *params.CustomerId
	}
	if params.DueBefore != nil {
		d := params.DueBefore.Time
		filter.DueBefore = &d
	}
	if params.Page != nil {
		filter.Page = int(*params.Page)
	}
	if params.PageSize != nil {
		filter.PageSize = int(*params.PageSize)
	}
	result, err := h.reminders.List(c.Request.Context(), scope, filter)
	if h.abortReminderError(c, err) {
		return
	}
	items := make([]Reminder, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, toAPIReminder(item))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": result.Total})
}

func (h *handlers) CreateReminder(c *gin.Context) {
	scope, ok := h.reminderScope(c)
	if !ok {
		return
	}
	if h.reminders == nil {
		abortError(c, http.StatusNotFound, CodeNotFound, "资源不存在")
		return
	}
	var body CreateReminderJSONRequestBody
	if err := c.ShouldBindJSON(&body); err != nil {
		abortError(c, http.StatusBadRequest, CodeValidationFailed, "请求体格式错误")
		return
	}
	if body.Type != CreateReminderJSONBodyTypeCustom {
		abortError(c, http.StatusBadRequest, CodeValidationFailed, "type 仅支持 custom")
		return
	}
	created, err := h.reminders.CreateCustom(c.Request.Context(), scope, reminder.CreateCustomInput{
		CustomerID: body.CustomerId,
		DueDate:    body.DueDate.Time,
		Content:    body.Content,
	})
	if h.abortReminderError(c, err) {
		return
	}
	c.JSON(http.StatusCreated, toAPIReminder(created))
}

func (h *handlers) MarkReminderDone(c *gin.Context, id Id) {
	scope, ok := h.reminderScope(c)
	if !ok {
		return
	}
	if h.reminders == nil {
		abortError(c, http.StatusNotFound, CodeNotFound, "资源不存在")
		return
	}
	updated, err := h.reminders.MarkDone(c.Request.Context(), scope, string(id))
	if h.abortReminderError(c, err) {
		return
	}
	c.JSON(http.StatusOK, toAPIReminder(updated))
}

func (h *handlers) DismissReminder(c *gin.Context, id Id) {
	scope, ok := h.reminderScope(c)
	if !ok {
		return
	}
	if h.reminders == nil {
		abortError(c, http.StatusNotFound, CodeNotFound, "资源不存在")
		return
	}
	updated, err := h.reminders.Dismiss(c.Request.Context(), scope, string(id))
	if h.abortReminderError(c, err) {
		return
	}
	c.JSON(http.StatusOK, toAPIReminder(updated))
}

func (h *handlers) ScanReminders(c *gin.Context) {
	scope, ok := h.reminderScope(c)
	if !ok {
		return
	}
	if h.reminders == nil {
		abortError(c, http.StatusNotFound, CodeNotFound, "资源不存在")
		return
	}
	var body ScanRemindersJSONRequestBody
	// body 可选
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&body); err != nil {
			abortError(c, http.StatusBadRequest, CodeValidationFailed, "请求体格式错误")
			return
		}
	}
	var date *time.Time
	if body.Date != nil {
		d := body.Date.Time
		date = &d
	}
	result, err := h.reminders.Scan(c.Request.Context(), scope, date)
	if h.abortReminderError(c, err) {
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"created":        result.Created,
		"skipped":        result.Skipped,
		"auto_dismissed": result.AutoDismissed,
	})
}

func (h *handlers) reminderScope(c *gin.Context) (store.AccountScope, bool) {
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

func (h *handlers) abortReminderError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	var ve reminder.ValidationError
	if errors.As(err, &ve) || errors.Is(err, reminder.ErrValidation) {
		msg := err.Error()
		if errors.As(err, &ve) {
			msg = ve.Message
		}
		abortError(c, http.StatusBadRequest, CodeValidationFailed, msg)
		return true
	}
	if errors.Is(err, reminder.ErrNotFound) {
		abortError(c, http.StatusNotFound, CodeNotFound, "资源不存在")
		return true
	}
	_ = c.Error(err)
	return true
}

func toAPIReminder(r reminder.Reminder) Reminder {
	id := r.ID
	acct := r.AccountID
	created := r.CreatedAt
	return Reminder{
		Id:         &id,
		AccountId:  &acct,
		CreatedAt:  &created,
		Type:       ReminderType(r.Type),
		CustomerId: r.CustomerID,
		OrderId:    r.OrderID,
		DueDate:    openapi_types.Date{Time: r.DueDate},
		Content:    r.Content,
		Status:     ReminderStatus(r.Status),
		DedupKey:   r.DedupKey,
	}
}
