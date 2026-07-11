package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/idempotency"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	scheduledomain "github.com/samson/customer-manage-platform/backend/internal/schedule"
)

const maxScheduleBodyBytes = 1 << 20

func (h *handlers) listScheduleSlotsRoute(c *gin.Context) {
	params, ok := bindListScheduleSlotsParams(c)
	if !ok {
		return
	}
	h.ListScheduleSlots(c, params)
}

func (h *handlers) createScheduleSlotRoute(c *gin.Context) {
	params := CreateScheduleSlotParams{}
	if key := c.GetHeader("Idempotency-Key"); key != "" {
		params.IdempotencyKey = &key
	}
	h.CreateScheduleSlot(c, params)
}

func (h *handlers) ListScheduleSlots(c *gin.Context, params ListScheduleSlotsParams) {
	scope, ok := h.scheduleScope(c)
	if !ok {
		return
	}
	items, err := h.schedule.List(c.Request.Context(), scope, scheduledomain.ListFilter{
		From: params.From,
		To:   params.To,
	})
	if h.abortScheduleError(c, err) {
		return
	}
	response := make([]ScheduleSlotListItem, 0, len(items))
	for _, item := range items {
		converted, err := toAPIScheduleListItem(item)
		if err != nil {
			_ = c.Error(err)
			return
		}
		response = append(response, converted)
	}
	c.JSON(http.StatusOK, response)
}

func (h *handlers) CreateScheduleSlot(c *gin.Context, params CreateScheduleSlotParams) {
	scope, ok := h.scheduleScope(c)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxScheduleBodyBytes)
	var body CreateScheduleSlotJSONRequestBody
	if err := c.ShouldBindJSON(&body); err != nil {
		abortError(c, http.StatusBadRequest, CodeValidationFailed, "请求体格式错误")
		return
	}
	input := scheduledomain.CreateInput{
		StartAt: body.StartAt,
		EndAt:   body.EndAt,
		Type:    string(body.Type),
		OrderID: body.OrderId,
		Note:    body.Note,
	}
	if params.IdempotencyKey == nil {
		created, err := h.schedule.Create(c.Request.Context(), scope, input)
		if h.abortScheduleError(c, err) {
			return
		}
		c.JSON(http.StatusCreated, scheduleCreatePayload(created))
		return
	}
	if h.idempotency == nil {
		_ = c.Error(errors.New("idempotency dependency missing"))
		return
	}
	prepared, err := h.schedule.PrepareCreate(input)
	if h.abortScheduleError(c, err) {
		return
	}
	canonical, err := json.Marshal(prepared.Input)
	if err != nil {
		_ = c.Error(err)
		return
	}

	var response idempotency.StoredResponse
	for attempt := 0; attempt < 2; attempt++ {
		response, err = h.idempotency.ExecuteCreate(
			c.Request.Context(),
			scope,
			idempotency.OperationScheduleSlotCreate,
			*params.IdempotencyKey,
			canonical,
			func(tx store.TxAccountScope) (idempotency.StoredResponse, error) {
				expectedCustomerID, err := h.schedule.LookupOrderCustomerIDInScope(
					c.Request.Context(),
					tx,
					prepared,
				)
				if err != nil {
					return idempotency.StoredResponse{}, err
				}
				created, err := h.schedule.CreatePreparedInScope(
					c.Request.Context(),
					tx,
					prepared,
					expectedCustomerID,
				)
				if err != nil {
					return idempotency.StoredResponse{}, err
				}
				body, err := json.Marshal(scheduleCreatePayload(created))
				if err != nil {
					return idempotency.StoredResponse{}, err
				}
				return idempotency.StoredResponse{Status: http.StatusCreated, Body: body}, nil
			},
		)
		var moved scheduledomain.CustomerChangedError
		if errors.As(err, &moved) {
			if attempt == 0 {
				continue
			}
			err = scheduledomain.ErrCustomerChanged
		}
		break
	}
	if h.abortScheduleError(c, err) {
		return
	}
	c.Data(response.Status, "application/json", response.Body)
}

func (h *handlers) UpdateScheduleSlot(c *gin.Context, id Id) {
	scope, ok := h.scheduleScope(c)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxScheduleBodyBytes)
	var body UpdateScheduleSlotJSONRequestBody
	if err := c.ShouldBindJSON(&body); err != nil {
		abortError(c, http.StatusBadRequest, CodeValidationFailed, "请求体格式错误")
		return
	}
	input := scheduledomain.UpdateInput{
		StartAt: body.StartAt,
		EndAt:   body.EndAt,
		OrderID: body.OrderId,
		Note:    body.Note,
	}
	if body.Type != nil {
		value := string(*body.Type)
		input.Type = &value
	}
	updated, err := h.schedule.Update(c.Request.Context(), scope, id, input)
	if h.abortScheduleError(c, err) {
		return
	}
	c.JSON(http.StatusOK, toAPIScheduleSlot(updated))
}

func (h *handlers) DeleteScheduleSlot(c *gin.Context, id Id) {
	scope, ok := h.scheduleScope(c)
	if !ok {
		return
	}
	if h.abortScheduleError(c, h.schedule.Delete(c.Request.Context(), scope, id)) {
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *handlers) scheduleScope(c *gin.Context) (store.AccountScope, bool) {
	ac, ok := auth.AccountContextFrom(c.Request.Context())
	if !ok {
		abortError(c, http.StatusUnauthorized, CodeUnauthorized, "未认证")
		return store.AccountScope{}, false
	}
	if h.scopeFactory == nil || h.schedule == nil {
		_ = c.Error(errors.New("schedule route dependencies missing"))
		return store.AccountScope{}, false
	}
	return h.scopeFactory.ScopeFor(ac), true
}

func (h *handlers) abortScheduleError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, scheduledomain.ErrValidation):
		abortError(c, http.StatusBadRequest, CodeValidationFailed, scheduleMessage(err))
	case errors.Is(err, scheduledomain.ErrNotFound):
		abortError(c, http.StatusNotFound, CodeNotFound, scheduleMessage(err))
	case errors.Is(err, scheduledomain.ErrOrderAlreadyScheduled):
		var details scheduledomain.OrderAlreadyScheduledError
		if !errors.As(err, &details) {
			_ = c.Error(err)
			break
		}
		abortErrorWithDetails(c, http.StatusConflict, CodeOrderAlreadyScheduled, scheduleMessage(err), ScheduleConflictDetails{
			ScheduleSlotId:  details.SlotID,
			ScheduleStartAt: details.StartAt,
		})
	case errors.Is(err, scheduledomain.ErrCustomerArchived):
		abortError(c, http.StatusConflict, CodeCustomerArchived, scheduleMessage(err))
	case errors.Is(err, scheduledomain.ErrCustomerChanged):
		abortError(c, http.StatusConflict, CodeCustomerChanged, scheduleMessage(err))
	case errors.Is(err, idempotency.ErrConflict):
		abortError(c, http.StatusConflict, CodeIdempotencyConflict, scheduleMessage(err))
	case errors.Is(err, idempotency.ErrValidation):
		abortError(c, http.StatusBadRequest, CodeValidationFailed, scheduleMessage(err))
	default:
		_ = c.Error(err)
	}
	return true
}

func bindListScheduleSlotsParams(c *gin.Context) (ListScheduleSlotsParams, bool) {
	from, ok := bindRequiredRFC3339(c, "from")
	if !ok {
		return ListScheduleSlotsParams{}, false
	}
	to, ok := bindRequiredRFC3339(c, "to")
	if !ok {
		return ListScheduleSlotsParams{}, false
	}
	return ListScheduleSlotsParams{From: from, To: to}, true
}

func bindRequiredRFC3339(c *gin.Context, name string) (time.Time, bool) {
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		abortError(c, http.StatusBadRequest, CodeValidationFailed, name+" 必填")
		return time.Time{}, false
	}
	value, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		abortError(c, http.StatusBadRequest, CodeValidationFailed, name+" 必须是 RFC3339 date-time")
		return time.Time{}, false
	}
	return value, true
}

func scheduleCreatePayload(result scheduledomain.CreateResult) gin.H {
	return gin.H{
		"slot":     toAPIScheduleSlot(result.Slot),
		"overlaps": result.Overlaps,
	}
}

func toAPIScheduleSlot(slot scheduledomain.Slot) ScheduleSlot {
	return ScheduleSlot{
		AccountId: stringPointer(slot.AccountID),
		CreatedAt: timePointer(slot.CreatedAt),
		EndAt:     slot.EndAt,
		Id:        stringPointer(slot.ID),
		Note:      slot.Note,
		OrderId:   slot.OrderID,
		StartAt:   slot.StartAt,
		Type:      SlotType(slot.Type),
	}
}

func toAPIScheduleListItem(item scheduledomain.ListItem) (ScheduleSlotListItem, error) {
	var result ScheduleSlotListItem
	if item.Type == scheduledomain.TypeShoot {
		if item.OrderID == nil {
			return result, errors.New("shoot schedule list item missing order id")
		}
		err := result.FromShootScheduleSlotListItem(ShootScheduleSlotListItem{
			AccountId:           stringPointer(item.AccountID),
			CreatedAt:           timePointer(item.CreatedAt),
			CustomerDisplayName: item.CustomerDisplayName,
			CustomerId:          item.CustomerID,
			CustomerStatus:      CustomerStatus(item.CustomerStatus),
			EndAt:               item.EndAt,
			Id:                  stringPointer(item.ID),
			Note:                item.Note,
			OrderId:             *item.OrderID,
			OrderStatus:         OrderStatus(item.OrderStatus),
			OrderTitle:          item.OrderTitle,
			PackageName:         item.PackageName,
			StartAt:             item.StartAt,
			Type:                Shoot,
		})
		return result, err
	}
	err := result.FromNonShootScheduleSlotListItem(NonShootScheduleSlotListItem{
		AccountId: stringPointer(item.AccountID),
		CreatedAt: timePointer(item.CreatedAt),
		EndAt:     item.EndAt,
		Id:        stringPointer(item.ID),
		Note:      item.Note,
		StartAt:   item.StartAt,
		Type:      NonShootScheduleSlotListItemType(item.Type),
	})
	return result, err
}

func scheduleMessage(err error) string {
	message := err.Error()
	if strings.Contains(message, ": ") {
		parts := strings.SplitN(message, ": ", 2)
		return parts[1]
	}
	return message
}
