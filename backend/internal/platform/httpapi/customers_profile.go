package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"

	customerdomain "github.com/samson/customer-manage-platform/backend/internal/customer"
)

// 客户档案五操作（customer-profile-complete）：HTTP 薄适配，
// 状态矩阵 / 联动 / 末位守护 / merge 事务全部在 customer 域内（ADR-003）。

func (h *handlers) UpdateCustomer(c *gin.Context, id Id) {
	scope, ok := h.customerScope(c)
	if !ok {
		return
	}
	var body UpdateCustomerJSONRequestBody
	if err := c.ShouldBindJSON(&body); err != nil {
		abortError(c, http.StatusBadRequest, CodeValidationFailed, "请求体格式错误")
		return
	}
	input := customerdomain.UpdateInput{
		DisplayName:        body.DisplayName,
		RealName:           body.RealName,
		Phone:              body.Phone,
		Birthday:           body.Birthday,
		ReferrerCustomerID: body.ReferrerCustomerId,
	}
	if body.Channel != nil {
		channel := string(*body.Channel)
		input.Channel = &channel
	}
	if body.Status != nil {
		status := string(*body.Status)
		input.Status = &status
	}
	updated, err := h.customer.Update(c.Request.Context(), scope, id, input)
	if h.abortCustomerError(c, err) {
		return
	}
	c.JSON(http.StatusOK, toAPICustomer(updated))
}

func (h *handlers) AddCustomerIdentity(c *gin.Context, id Id) {
	scope, ok := h.customerScope(c)
	if !ok {
		return
	}
	var body AddCustomerIdentityJSONRequestBody
	if err := c.ShouldBindJSON(&body); err != nil {
		abortError(c, http.StatusBadRequest, CodeValidationFailed, "请求体格式错误")
		return
	}
	identity, err := h.customer.AddIdentity(c.Request.Context(), scope, id, customerdomain.IdentityInput{
		Platform: string(body.Platform),
		Handle:   body.Handle,
		Remark:   body.Remark,
	})
	if h.abortCustomerError(c, err) {
		return
	}
	c.JSON(http.StatusCreated, SocialIdentity{
		AccountId:  stringPointer(identity.AccountID),
		CreatedAt:  timePointer(identity.CreatedAt),
		CustomerId: identity.CustomerID,
		Handle:     identity.Handle,
		Id:         stringPointer(identity.ID),
		Platform:   SocialPlatform(identity.Platform),
		Remark:     identity.Remark,
	})
}

func (h *handlers) DeleteCustomerIdentity(c *gin.Context, id Id, identityId string) {
	scope, ok := h.customerScope(c)
	if !ok {
		return
	}
	err := h.customer.DeleteIdentity(c.Request.Context(), scope, id, identityId)
	if h.abortCustomerError(c, err) {
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *handlers) AddCustomerNote(c *gin.Context, id Id) {
	scope, ok := h.customerScope(c)
	if !ok {
		return
	}
	var body AddCustomerNoteJSONRequestBody
	if err := c.ShouldBindJSON(&body); err != nil {
		abortError(c, http.StatusBadRequest, CodeValidationFailed, "请求体格式错误")
		return
	}
	note, err := h.customer.AddNote(c.Request.Context(), scope, id, body.Content)
	if h.abortCustomerError(c, err) {
		return
	}
	c.JSON(http.StatusCreated, toAPICustomerNote(note))
}

func (h *handlers) MergeCustomer(c *gin.Context, id Id) {
	scope, ok := h.customerScope(c)
	if !ok {
		return
	}
	var body MergeCustomerJSONRequestBody
	if err := c.ShouldBindJSON(&body); err != nil {
		abortError(c, http.StatusBadRequest, CodeValidationFailed, "请求体格式错误")
		return
	}
	target, err := h.customer.Merge(c.Request.Context(), scope, id, body.SourceCustomerId)
	if h.abortCustomerError(c, err) {
		return
	}
	// merge 可观测点（design 2.2）：只记 id 与结果，不记 handle / 手机号明文。
	h.logger.InfoContext(c.Request.Context(), "customer merged",
		"target_id", target.ID, "source_id", body.SourceCustomerId)
	c.JSON(http.StatusOK, toAPICustomer(target))
}
