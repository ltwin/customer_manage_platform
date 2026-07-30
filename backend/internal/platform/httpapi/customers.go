package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/oapi-codegen/nullable"
	openapi_types "github.com/oapi-codegen/runtime/types"

	customerdomain "github.com/samson/customer-manage-platform/backend/internal/customer"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func (h *handlers) listCustomersRoute(c *gin.Context) {
	params, ok := bindListCustomersParams(c)
	if !ok {
		return
	}
	h.ListCustomers(c, params)
}

func (h *handlers) getCustomerRoute(c *gin.Context) {
	h.GetCustomer(c, c.Param("id"))
}

func (h *handlers) CreateCustomer(c *gin.Context) {
	scope, ok := h.customerScope(c)
	if !ok {
		return
	}
	var body CreateCustomerJSONRequestBody
	if err := c.ShouldBindJSON(&body); err != nil {
		abortError(c, http.StatusBadRequest, CodeValidationFailed, "请求体格式错误")
		return
	}
	identities := make([]customerdomain.IdentityInput, 0, len(body.Identities))
	for _, identity := range body.Identities {
		identities = append(identities, customerdomain.IdentityInput{
			Platform: string(identity.Platform),
			Handle:   identity.Handle,
			Remark:   identity.Remark,
		})
	}
	created, err := h.customer.Create(c.Request.Context(), scope, customerdomain.CreateInput{
		DisplayName:        body.DisplayName,
		Channel:            string(body.Channel),
		ReferrerCustomerID: body.ReferrerCustomerId,
		Identities:         identities,
	})
	if h.abortCustomerError(c, err) {
		return
	}
	c.JSON(http.StatusCreated, toAPICustomer(created))
}

func (h *handlers) ListCustomers(c *gin.Context, params ListCustomersParams) {
	scope, ok := h.customerScope(c)
	if !ok {
		return
	}
	filter := customerdomain.ListFilter{}
	if params.Q != nil {
		filter.Q = *params.Q
	}
	if params.Channel != nil {
		filter.Channel = string(*params.Channel)
	}
	if params.Status != nil {
		filter.Status = string(*params.Status)
	}
	if params.Page != nil {
		filter.Page = int(*params.Page)
	}
	if params.PageSize != nil {
		filter.PageSize = int(*params.PageSize)
	}
	result, err := h.customer.List(c.Request.Context(), scope, filter)
	if h.abortCustomerError(c, err) {
		return
	}
	items := make([]CustomerListItem, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, toAPICustomerListItem(item))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": result.Total})
}

func (h *handlers) GetCustomer(c *gin.Context, id Id) {
	scope, ok := h.customerScope(c)
	if !ok {
		return
	}
	detail, err := h.customer.Detail(c.Request.Context(), scope, id)
	if h.abortCustomerError(c, err) {
		return
	}
	c.JSON(http.StatusOK, toAPICustomerDetail(detail))
}

func (h *handlers) customerScope(c *gin.Context) (store.AccountScope, bool) {
	ac, ok := auth.AccountContextFrom(c.Request.Context())
	if !ok {
		abortError(c, http.StatusUnauthorized, CodeUnauthorized, "未认证")
		return store.AccountScope{}, false
	}
	if h.scopeFactory == nil || h.customer == nil {
		_ = c.Error(errors.New("customer route dependencies missing"))
		return store.AccountScope{}, false
	}
	return h.scopeFactory.ScopeFor(ac), true
}

func (h *handlers) abortCustomerError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, customerdomain.ErrValidation):
		abortError(c, http.StatusBadRequest, CodeValidationFailed, customerMessage(err))
	case errors.Is(err, customerdomain.ErrNotFound):
		abortError(c, http.StatusNotFound, CodeNotFound, customerMessage(err))
	case errors.Is(err, customerdomain.ErrCustomerMerged):
		abortError(c, http.StatusConflict, CodeCustomerMerged, customerMessage(err))
	case errors.Is(err, customerdomain.ErrLastIdentity):
		abortError(c, http.StatusConflict, CodeLastIdentity, customerMessage(err))
	case errors.Is(err, customerdomain.ErrMergeConflict):
		abortError(c, http.StatusConflict, CodeMergeConflict, customerMessage(err))
	case errors.Is(err, customerdomain.ErrAvatarRevisionConflict):
		abortError(c, http.StatusConflict, "avatar_revision_conflict", customerMessage(err))
	case errors.Is(err, customerdomain.ErrAvatarVersionStale):
		abortError(c, http.StatusConflict, "avatar_version_stale", customerMessage(err))
	default:
		_ = c.Error(err)
	}
	return true
}

func bindListCustomersParams(c *gin.Context) (ListCustomersParams, bool) {
	var params ListCustomersParams
	if q := strings.TrimSpace(c.Query("q")); q != "" {
		params.Q = &q
	}
	if channel := strings.TrimSpace(c.Query("channel")); channel != "" {
		value := CustomerChannel(channel)
		params.Channel = &value
	}
	if status := strings.TrimSpace(c.Query("status")); status != "" {
		value := status
		params.Status = &value
	}
	page, ok := bindOptionalPageParam(c, "page")
	if !ok {
		return ListCustomersParams{}, false
	}
	params.Page = page
	pageSize, ok := bindOptionalPageParam(c, "page_size")
	if !ok {
		return ListCustomersParams{}, false
	}
	params.PageSize = pageSize
	return params, true
}

func bindOptionalPageParam(c *gin.Context, name string) (*int, bool) {
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return nil, true
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		abortError(c, http.StatusBadRequest, CodeValidationFailed, name+" 必须是整数")
		return nil, false
	}
	// 契约 minimum:1；显式 0 不做静默纠偏（REV-002），缺省语义只属于「未传」。
	if value < 1 {
		abortError(c, http.StatusBadRequest, CodeValidationFailed, name+" 必须大于 0")
		return nil, false
	}
	return &value, true
}

func customerMessage(err error) string {
	msg := err.Error()
	if strings.Contains(msg, ": ") {
		parts := strings.SplitN(msg, ": ", 2)
		return parts[1]
	}
	return msg
}

func toAPICustomer(c customerdomain.Customer) Customer {
	avatarRevision, avatarVersion, avatarURL := avatarAPIFields(c.ID, c.AvatarRevision, c.AvatarVersion)
	return Customer{
		AccountId:            stringPointer(c.AccountID),
		AvatarRevision:       avatarRevision,
		AvatarUrl:            avatarURL,
		AvatarVersion:        avatarVersion,
		Birthday:             c.Birthday,
		Channel:              CustomerChannel(c.Channel),
		CreatedAt:            timePointer(c.CreatedAt),
		DisplayName:          c.DisplayName,
		Id:                   stringPointer(c.ID),
		MergedIntoCustomerId: c.MergedIntoCustomerID,
		Phone:                c.Phone,
		RealName:             c.RealName,
		ReferrerCustomerId:   c.ReferrerCustomerID,
		Status:               CustomerStatus(c.Status),
	}
}

func toAPICustomerListItem(item customerdomain.ListItem) CustomerListItem {
	avatarRevision, avatarVersion, avatarURL := avatarAPIFields(item.ID, item.AvatarRevision, item.AvatarVersion)
	return CustomerListItem{
		AccountId:            stringPointer(item.AccountID),
		AvatarRevision:       avatarRevision,
		AvatarUrl:            avatarURL,
		AvatarVersion:        avatarVersion,
		Birthday:             item.Birthday,
		Channel:              CustomerChannel(item.Channel),
		CreatedAt:            timePointer(item.CreatedAt),
		DisplayName:          item.DisplayName,
		Id:                   stringPointer(item.ID),
		LastShotAt:           nullableDate(item.LastShotAt),
		MergedIntoCustomerId: item.MergedIntoCustomerID,
		OrdersCount:          item.OrdersCount,
		Phone:                item.Phone,
		RealName:             item.RealName,
		ReferrerCustomerId:   item.ReferrerCustomerID,
		Status:               CustomerStatus(item.Status),
	}
}

func toAPICustomerDetail(detail customerdomain.Detail) CustomerDetail {
	identities := make([]SocialIdentity, 0, len(detail.Identities))
	for _, identity := range detail.Identities {
		identities = append(identities, SocialIdentity{
			AccountId:  stringPointer(identity.AccountID),
			CreatedAt:  timePointer(identity.CreatedAt),
			CustomerId: identity.CustomerID,
			Handle:     identity.Handle,
			Id:         stringPointer(identity.ID),
			Platform:   SocialPlatform(identity.Platform),
			Remark:     identity.Remark,
		})
	}
	notes := make([]CustomerNote, 0, len(detail.Notes))
	for _, note := range detail.Notes {
		notes = append(notes, toAPICustomerNote(note))
	}
	var referrer nullable.Nullable[CustomerSummary]
	referrer.SetNull()
	if detail.Referrer != nil {
		avatarRevision, avatarVersion, avatarURL := avatarAPIFields(
			detail.Referrer.ID,
			detail.Referrer.AvatarRevision,
			detail.Referrer.AvatarVersion,
		)
		referrer.Set(CustomerSummary{
			AvatarRevision: avatarRevision,
			AvatarUrl:      avatarURL,
			AvatarVersion:  avatarVersion,
			Channel:        CustomerChannel(detail.Referrer.Channel),
			DisplayName:    detail.Referrer.DisplayName,
			Id:             detail.Referrer.ID,
			Status:         CustomerStatus(detail.Referrer.Status),
		})
	}
	avatarRevision, avatarVersion, avatarURL := avatarAPIFields(detail.ID, detail.AvatarRevision, detail.AvatarVersion)
	return CustomerDetail{
		AccountId:            stringPointer(detail.AccountID),
		AvatarRevision:       avatarRevision,
		AvatarUrl:            avatarURL,
		AvatarVersion:        avatarVersion,
		Birthday:             detail.Birthday,
		Channel:              CustomerChannel(detail.Channel),
		CreatedAt:            timePointer(detail.CreatedAt),
		DisplayName:          detail.DisplayName,
		Id:                   stringPointer(detail.ID),
		Identities:           identities,
		MergedIntoCustomerId: detail.MergedIntoCustomerID,
		Notes:                notes,
		Phone:                detail.Phone,
		RealName:             detail.RealName,
		Referrer:             referrer,
		ReferrerCustomerId:   detail.ReferrerCustomerID,
		Stats: CustomerStats{
			LastShotAt:       nullableDate(detail.Stats.LastShotAt),
			OrdersCount:      detail.Stats.OrdersCount,
			TotalOrderAmount: detail.Stats.TotalOrderAmount,
		},
		Status: CustomerStatus(detail.Status),
	}
}

func avatarAPIFields(id string, revision int64, version *string) (*AvatarRevision, *AvatarVersion, *string) {
	revisionValue := AvatarRevision("ar-" + strconv.FormatInt(revision, 10))
	if version == nil {
		return &revisionValue, nil, nil
	}
	versionValue := AvatarVersion(*version)
	url := "/api/v1/customers/" + id + "/avatar/content?v=" + *version
	return &revisionValue, &versionValue, &url
}

func stringPointer(value string) *string {
	return &value
}

func toAPICustomerNote(note customerdomain.CustomerNote) CustomerNote {
	return CustomerNote{
		AccountId:  stringPointer(note.AccountID),
		Content:    note.Content,
		CreatedAt:  timePointer(note.CreatedAt),
		CustomerId: note.CustomerID,
		Id:         stringPointer(note.ID),
	}
}

func timePointer(value time.Time) *time.Time {
	return &value
}

func datePointer(value *string) *openapi_types.Date {
	if value == nil {
		return nil
	}
	parsed, err := time.Parse(openapi_types.DateFormat, *value)
	if err != nil {
		return nil
	}
	return &openapi_types.Date{Time: parsed}
}

// nullableDate 把领域层的可空日期映射为契约的 required+nullable 字段（缺失即显式 null）。
func nullableDate(value *string) nullable.Nullable[openapi_types.Date] {
	var result nullable.Nullable[openapi_types.Date]
	result.SetNull()
	if parsed := datePointer(value); parsed != nil {
		result.Set(*parsed)
	}
	return result
}
