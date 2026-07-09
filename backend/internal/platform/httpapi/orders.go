package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/oapi-codegen/nullable"

	orderdomain "github.com/samson/customer-manage-platform/backend/internal/order"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const maxOrderBodyBytes = 1 << 20

func (h *handlers) listOrdersRoute(c *gin.Context) {
	params, ok := bindListOrdersParams(c)
	if !ok {
		return
	}
	h.ListOrders(c, params)
}

func (h *handlers) CreateOrder(c *gin.Context) {
	scope, ok := h.orderScope(c)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxOrderBodyBytes)
	var body CreateOrderJSONRequestBody
	if err := c.ShouldBindJSON(&body); err != nil {
		abortError(c, http.StatusBadRequest, CodeValidationFailed, "请求体格式错误")
		return
	}
	input := orderdomain.CreateInput{
		CustomerID:  body.CustomerId,
		PackageID:   body.PackageId,
		Title:       body.Title,
		Price:       body.Price,
		DepositPaid: body.DepositPaid,
		BalancePaid: body.BalancePaid,
		ShotAt:      body.ShotAt,
		DeliveredAt: body.DeliveredAt,
		Note:        body.Note,
	}
	if body.Status != nil {
		status := string(*body.Status)
		input.Status = &status
	}
	created, err := h.orders.Create(c.Request.Context(), scope, input)
	if h.abortOrderError(c, err) {
		return
	}
	c.JSON(http.StatusCreated, toAPIOrder(created))
}

func (h *handlers) ListOrders(c *gin.Context, params ListOrdersParams) {
	scope, ok := h.orderScope(c)
	if !ok {
		return
	}
	filter := orderdomain.ListFilter{}
	if params.CustomerId != nil {
		filter.CustomerID = *params.CustomerId
	}
	if params.Status != nil {
		filter.Status = string(*params.Status)
	}
	if params.UnpaidBalance != nil {
		filter.UnpaidBalance = *params.UnpaidBalance
	}
	if params.Page != nil {
		filter.Page = int(*params.Page)
	}
	if params.PageSize != nil {
		filter.PageSize = int(*params.PageSize)
	}
	result, err := h.orders.List(c.Request.Context(), scope, filter)
	if h.abortOrderError(c, err) {
		return
	}
	items := make([]OrderListItem, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, toAPIOrderListItem(item))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": result.Total})
}

func (h *handlers) UpdateOrder(c *gin.Context, id Id) {
	scope, ok := h.orderScope(c)
	if !ok {
		return
	}
	body, raw, ok := bindUpdateOrderBody(c)
	if !ok {
		return
	}
	input := orderdomain.UpdateInput{
		DepositPaid: body.DepositPaid,
		BalancePaid: body.BalancePaid,
		Title:       body.Title,
		Note:        body.Note,
	}
	if body.Status != nil {
		status := string(*body.Status)
		input.Status = &status
	}
	input.Price = nullableIntFromRaw(raw, "price", body.Price)
	input.ShotAt = nullableTimeFromRaw(raw, "shot_at", body.ShotAt)
	input.DeliveredAt = nullableTimeFromRaw(raw, "delivered_at", body.DeliveredAt)
	updated, err := h.orders.Update(c.Request.Context(), scope, id, input)
	if h.abortOrderError(c, err) {
		return
	}
	c.JSON(http.StatusOK, toAPIOrder(updated))
}

func (h *handlers) DeleteOrder(c *gin.Context, id Id) {
	scope, ok := h.orderScope(c)
	if !ok {
		return
	}
	err := h.orders.Delete(c.Request.Context(), scope, id)
	if h.abortOrderError(c, err) {
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *handlers) orderScope(c *gin.Context) (store.AccountScope, bool) {
	ac, ok := auth.AccountContextFrom(c.Request.Context())
	if !ok {
		abortError(c, http.StatusUnauthorized, CodeUnauthorized, "未认证")
		return store.AccountScope{}, false
	}
	if h.scopeFactory == nil || h.orders == nil {
		_ = c.Error(errors.New("order route dependencies missing"))
		return store.AccountScope{}, false
	}
	return h.scopeFactory.ScopeFor(ac), true
}

func (h *handlers) abortOrderError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, orderdomain.ErrValidation):
		abortError(c, http.StatusBadRequest, CodeValidationFailed, orderMessage(err))
	case errors.Is(err, orderdomain.ErrNotFound):
		abortError(c, http.StatusNotFound, CodeNotFound, orderMessage(err))
	case errors.Is(err, orderdomain.ErrCustomerArchived):
		abortError(c, http.StatusConflict, CodeCustomerArchived, orderMessage(err))
	case errors.Is(err, orderdomain.ErrInvalidStatusTransition):
		abortError(c, http.StatusConflict, CodeInvalidStatusTransition, orderMessage(err))
	case errors.Is(err, orderdomain.ErrUnpaidBalance):
		abortError(c, http.StatusConflict, CodeUnpaidBalance, orderMessage(err))
	case errors.Is(err, orderdomain.ErrOrderNotTerminal):
		abortError(c, http.StatusConflict, CodeOrderNotTerminal, orderMessage(err))
	default:
		_ = c.Error(err)
	}
	return true
}

func bindListOrdersParams(c *gin.Context) (ListOrdersParams, bool) {
	var params ListOrdersParams
	if customerID := strings.TrimSpace(c.Query("customer_id")); customerID != "" {
		params.CustomerId = &customerID
	}
	if status := strings.TrimSpace(c.Query("status")); status != "" {
		value := OrderStatus(status)
		params.Status = &value
	}
	if raw := strings.TrimSpace(c.Query("unpaid_balance")); raw != "" {
		value, err := strconv.ParseBool(raw)
		if err != nil {
			abortError(c, http.StatusBadRequest, CodeValidationFailed, "unpaid_balance 必须是 boolean")
			return ListOrdersParams{}, false
		}
		params.UnpaidBalance = &value
	}
	page, ok := bindOptionalPageParam(c, "page")
	if !ok {
		return ListOrdersParams{}, false
	}
	if page != nil {
		value := Page(*page)
		params.Page = &value
	}
	pageSize, ok := bindOptionalPageParam(c, "page_size")
	if !ok {
		return ListOrdersParams{}, false
	}
	if pageSize != nil {
		value := PageSize(*pageSize)
		params.PageSize = &value
	}
	return params, true
}

func bindUpdateOrderBody(c *gin.Context) (UpdateOrderJSONRequestBody, []byte, bool) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxOrderBodyBytes)
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		abortError(c, http.StatusBadRequest, CodeValidationFailed, "请求体格式错误")
		return UpdateOrderJSONRequestBody{}, nil, false
	}
	var body UpdateOrderJSONRequestBody
	if err := json.Unmarshal(raw, &body); err != nil {
		abortError(c, http.StatusBadRequest, CodeValidationFailed, "请求体格式错误")
		return UpdateOrderJSONRequestBody{}, nil, false
	}
	return body, raw, true
}

func nullableIntFromRaw(raw []byte, name string, value *int) nullable.Nullable[int] {
	var result nullable.Nullable[int]
	field, ok := jsonField(raw, name)
	if !ok {
		return result
	}
	if isJSONNull(field) {
		result.SetNull()
		return result
	}
	if value != nil {
		result.Set(*value)
	}
	return result
}

func nullableTimeFromRaw(raw []byte, name string, value *time.Time) nullable.Nullable[time.Time] {
	var result nullable.Nullable[time.Time]
	field, ok := jsonField(raw, name)
	if !ok {
		return result
	}
	if isJSONNull(field) {
		result.SetNull()
		return result
	}
	if value != nil {
		result.Set(*value)
	}
	return result
}

func jsonField(raw []byte, name string) (json.RawMessage, bool) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, false
	}
	value, ok := fields[name]
	return value, ok
}

func isJSONNull(value json.RawMessage) bool {
	return strings.TrimSpace(string(value)) == "null"
}

func orderMessage(err error) string {
	msg := err.Error()
	if strings.Contains(msg, ": ") {
		parts := strings.SplitN(msg, ": ", 2)
		return parts[1]
	}
	return msg
}

func toAPIOrder(o orderdomain.Order) Order {
	return Order{
		AccountId:   stringPointer(o.AccountID),
		BalancePaid: o.BalancePaid,
		CreatedAt:   timePointer(o.CreatedAt),
		CustomerId:  o.CustomerID,
		DeliveredAt: o.DeliveredAt,
		DepositPaid: o.DepositPaid,
		Id:          stringPointer(o.ID),
		Note:        o.Note,
		PackageId:   o.PackageID,
		Price:       o.Price,
		ShotAt:      o.ShotAt,
		Status:      OrderStatus(o.Status),
		Title:       o.Title,
	}
}

func toAPIOrderListItem(item orderdomain.ListItem) OrderListItem {
	return OrderListItem{
		AccountId:           stringPointer(item.AccountID),
		BalancePaid:         item.BalancePaid,
		CreatedAt:           timePointer(item.CreatedAt),
		CustomerDisplayName: item.CustomerDisplayName,
		CustomerId:          item.CustomerID,
		DeliveredAt:         item.DeliveredAt,
		DepositPaid:         item.DepositPaid,
		Id:                  stringPointer(item.ID),
		Note:                item.Note,
		PackageId:           item.PackageID,
		PackageName:         item.PackageName,
		Price:               item.Price,
		ShotAt:              item.ShotAt,
		Status:              OrderStatus(item.Status),
		Title:               item.Title,
	}
}
