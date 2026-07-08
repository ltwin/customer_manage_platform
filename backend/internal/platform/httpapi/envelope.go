package httpapi

import "github.com/gin-gonic/gin"

// 错误码集合以 §4.1 为准，不自造；conflict 子码随各域 feature 生长。
const (
	CodeValidationFailed = "validation_failed"
	CodeUnauthorized     = "unauthorized"
	CodeNotFound         = "not_found"
	CodeInternal         = "internal"

	// customer-profile-complete 的 409 conflict 子码（design D3/D4/D5）
	CodeCustomerMerged = "customer_merged"
	CodeLastIdentity   = "last_identity"
	CodeMergeConflict  = "merge_conflict"
)

// newErrorEnvelope 构造统一错误封套（类型用 codegen 产物，保证与契约同源）。
func newErrorEnvelope(code, message string) ErrorEnvelope {
	var env ErrorEnvelope
	env.Error.Code = code
	env.Error.Message = message
	return env
}

// abortError 以错误封套终止请求：一切 /api/v1/** 非 2xx 响应的唯一出口。
func abortError(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, newErrorEnvelope(code, message))
}
