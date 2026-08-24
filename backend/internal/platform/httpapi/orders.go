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
	openapi_types "github.com/oapi-codegen/runtime/types"

	orderdomain "github.com/samson/customer-manage-platform/backend/internal/order"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/clock"
	"github.com/samson/customer-manage-platform/backend/internal/platform/idempotency"
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

func (h *handlers) createOrderRoute(c *gin.Context) {
	params := CreateOrderParams{}
	if key := c.GetHeader("Idempotency-Key"); key != "" {
		params.IdempotencyKey = &key
	}
	h.CreateOrder(c, params)
}

func (h *handlers) CreateOrder(c *gin.Context, params CreateOrderParams) {
	scope, ok := h.orderScope(c)
	if !ok {
		return
	}
	body, raw, ok := bindCreateOrderBody(c)
	if !ok {
		return
	}
	if !rejectExplicitNullAmounts(c, raw) {
		return
	}
	input := orderdomain.CreateInput{
		CreationMode:  stringValue(body.CreationMode),
		CustomerID:    body.CustomerId,
		PackageID:     body.PackageId,
		Title:         body.Title,
		Price:         body.Price,
		DepositPaid:   body.DepositPaid,
		BalancePaid:   body.BalancePaid,
		ShotAt:        body.ShotAt,
		DeliveredAt:   body.DeliveredAt,
		DeliveryDueAt: domainDateFromAPI(body.DeliveryDueAt),
		AmountPaid:    body.AmountPaid,
		// 金额字段建单为二态：缺省交由 DEC-10 推定收敛；显式 null 由
		// rejectExplicitNullAmounts 统一 400，不静默走推定。
		OutstandingAmount: body.OutstandingAmount,
		PaidAt:            body.PaidAt,
		Note:              body.Note,
	}
	if body.Status != nil {
		status := string(*body.Status)
		input.Status = &status
	}
	if params.IdempotencyKey == nil {
		created, err := h.orders.Create(c.Request.Context(), scope, input)
		if h.abortOrderError(c, err) {
			return
		}
		c.JSON(http.StatusCreated, toAPIOrder(created))
		return
	}
	if h.idempotency == nil {
		_ = c.Error(errors.New("idempotency dependency missing"))
		return
	}
	prepared, err := h.orders.PrepareCreateInScope(c.Request.Context(), scope, input)
	if h.abortOrderError(c, err) {
		return
	}
	canonical, err := json.Marshal(prepared.Input)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response, err := h.idempotency.ExecuteCreate(
		c.Request.Context(),
		scope,
		idempotency.OperationOrderCreate,
		*params.IdempotencyKey,
		canonical,
		func(tx store.TxAccountScope) (idempotency.StoredResponse, error) {
			created, err := h.orders.CreatePreparedInScope(c.Request.Context(), tx, prepared)
			if err != nil {
				return idempotency.StoredResponse{}, err
			}
			body, err := json.Marshal(toAPIOrder(created))
			if err != nil {
				return idempotency.StoredResponse{}, err
			}
			return idempotency.StoredResponse{Status: http.StatusCreated, Body: body}, nil
		},
	)
	if h.abortOrderError(c, err) {
		return
	}
	c.Data(response.Status, "application/json", response.Body)
}

func (h *handlers) ListOrders(c *gin.Context, params ListOrdersParams) {
	scope, ok := h.orderScope(c)
	if !ok {
		return
	}
	filter := orderdomain.ListFilter{}
	if params.Id != nil {
		filter.ID = *params.Id
	}
	if params.CustomerId != nil {
		filter.CustomerID = *params.CustomerId
	}
	if params.Status != nil {
		filter.Status = string(*params.Status)
	}
	if params.UnpaidBalance != nil {
		filter.UnpaidBalance = *params.UnpaidBalance
	}
	filter.SchedulableAt = params.SchedulableAt
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
	input.DeliveryDueAt = domainNullableDateFromAPI(body.DeliveryDueAt)
	input.AmountPaid = nullableIntFromRaw(raw, "amount_paid", body.AmountPaid)
	input.OutstandingAmount = nullableIntFromRaw(raw, "outstanding_amount", body.OutstandingAmount)
	input.PaidAt = nullableTimeFromRaw(raw, "paid_at", body.PaidAt)
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
	case errors.Is(err, orderdomain.ErrOrderInUse):
		var details orderdomain.OrderInUseError
		if !errors.As(err, &details) {
			_ = c.Error(err)
			break
		}
		abortErrorWithDetails(c, http.StatusConflict, CodeOrderInUse, orderMessage(err), ScheduleConflictDetails{
			ScheduleSlotId:  details.SlotID,
			ScheduleStartAt: details.StartAt,
		})
	case errors.Is(err, idempotency.ErrConflict):
		abortError(c, http.StatusConflict, CodeIdempotencyConflict, orderMessage(err))
	case errors.Is(err, idempotency.ErrValidation):
		abortError(c, http.StatusBadRequest, CodeValidationFailed, orderMessage(err))
	default:
		_ = c.Error(err)
	}
	return true
}

func bindListOrdersParams(c *gin.Context) (ListOrdersParams, bool) {
	var params ListOrdersParams
	if id := strings.TrimSpace(c.Query("id")); id != "" {
		params.Id = &id
	}
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
	if raw := strings.TrimSpace(c.Query("schedulable_at")); raw != "" {
		value, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			abortError(c, http.StatusBadRequest, CodeValidationFailed, "schedulable_at 必须是 RFC3339 date-time")
			return ListOrdersParams{}, false
		}
		params.SchedulableAt = &value
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

func bindCreateOrderBody(c *gin.Context) (CreateOrderJSONRequestBody, []byte, bool) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxOrderBodyBytes)
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		abortError(c, http.StatusBadRequest, CodeValidationFailed, "请求体格式错误")
		return CreateOrderJSONRequestBody{}, nil, false
	}
	var body CreateOrderJSONRequestBody
	if err := json.Unmarshal(raw, &body); err != nil {
		abortError(c, http.StatusBadRequest, CodeValidationFailed, "请求体格式错误")
		return CreateOrderJSONRequestBody{}, nil, false
	}
	return body, raw, true
}

// rejectExplicitNullAmounts 金额字段无 null 语义（§4.2）：建单侧 *int 绑定无法区分
// 「显式 null」与「缺省」，不拦会让 null 静默走 DEC-10 推定；与 PATCH 的三态拒绝同口径 400。
func rejectExplicitNullAmounts(c *gin.Context, raw []byte) bool {
	for _, field := range []string{"amount_paid", "outstanding_amount", "paid_at"} {
		if value, ok := jsonField(raw, field); ok && isJSONNull(value) {
			abortError(c, http.StatusBadRequest, CodeValidationFailed, field+" 不接受 null")
			return false
		}
	}
	return true
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

func stringValue(value *OrderCreationMode) string {
	if value == nil {
		return ""
	}
	return string(*value)
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

		DeliveryDueAt:         apiDatePointer(o.DeliveryDueAt),
		DeliveryDueIsOverride: &o.DeliveryDueIsOverride,

		AmountPaid:        o.AmountPaid,
		OutstandingAmount: o.OutstandingAmount,
		PaidAt:            o.PaidAt,

		ChannelSnapshot:   CustomerChannel(o.ChannelSnapshot),
		ShootTypeSnapshot: shootTypePointer(o.ShootTypeSnapshot),
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
		PlanningSummary:     toAPIPlanningSummary(item.PlanningSummary),

		DeliveryDueAt:         apiDatePointer(item.DeliveryDueAt),
		DeliveryDueIsOverride: &item.DeliveryDueIsOverride,

		AmountPaid:        item.AmountPaid,
		OutstandingAmount: item.OutstandingAmount,
		PaidAt:            item.PaidAt,

		ChannelSnapshot:   CustomerChannel(item.ChannelSnapshot),
		ShootTypeSnapshot: shootTypePointer(item.ShootTypeSnapshot),
	}
}

// shootTypePointer 把领域 *string 拍摄类型投影为契约枚举指针。
func shootTypePointer(value *string) *ShootType {
	if value == nil {
		return nil
	}
	converted := ShootType(*value)
	return &converted
}

// apiDatePointer 把领域 date-only 值投影为契约 date 类型。
func apiDatePointer(value *time.Time) *openapi_types.Date {
	if value == nil {
		return nil
	}
	return &openapi_types.Date{Time: *value}
}

// domainNullableDateFromAPI 保留契约的三态：未传 / 显式 null / 有值。
// 显式 null 表示撤销订单级覆盖并交还自动派生，与「未传」语义不同。
func domainNullableDateFromAPI(value nullable.Nullable[openapi_types.Date]) nullable.Nullable[time.Time] {
	var result nullable.Nullable[time.Time]
	if !value.IsSpecified() {
		return result
	}
	if value.IsNull() {
		result.SetNull()
		return result
	}
	result.Set(clock.DateOnly(value.MustGet().Time))
	return result
}

// domainDateFromAPI 把契约 date 转成领域 date-only（UTC 午夜）。
func domainDateFromAPI(value *openapi_types.Date) *time.Time {
	if value == nil {
		return nil
	}
	normalized := clock.DateOnly(value.Time)
	return &normalized
}
