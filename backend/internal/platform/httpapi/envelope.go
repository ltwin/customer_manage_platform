package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// 错误码集合以 §4.1 为准，不自造；conflict 子码随各域 feature 生长。
const (
	CodeValidationFailed          = "validation_failed"
	CodeUnauthorized              = "unauthorized"
	CodeNotFound                  = "not_found"
	CodeInternal                  = "internal"
	CodeForbidden                 = "forbidden"
	CodeEmailVerificationRequired = "email_verification_required"
	CodeInvalidOrExpiredToken     = "invalid_or_expired_token"
	CodeRegistrationDisabled      = "registration_disabled"
	CodeRateLimited               = "rate_limited"

	// customer-profile-complete 的 409 conflict 子码（design D3/D4/D5）
	CodeCustomerMerged = "customer_merged"
	CodeLastIdentity   = "last_identity"
	CodeMergeConflict  = "merge_conflict"
	CodePackageInUse   = "package_in_use"

	CodeCustomerArchived        = "customer_archived"
	CodeInvalidStatusTransition = "invalid_status_transition"
	CodeUnpaidBalance           = "unpaid_balance"
	CodeOrderNotTerminal        = "order_not_terminal"
	CodeOrderInUse              = "order_in_use"
	CodeOrderAlreadyScheduled   = "order_already_scheduled"
	CodeCustomerChanged         = "customer_changed"
	CodeIdempotencyConflict     = "idempotency_conflict"

	// shoot-plan-core 的 409 conflict 子码。
	CodePlanRevisionConflict           = "plan_revision_conflict"
	CodeExecutionRevisionConflict      = "execution_revision_conflict"
	CodeInvalidPlanTransition          = "invalid_plan_transition"
	CodeReadinessIncomplete            = "readiness_incomplete"
	CodeReadinessAssignmentActive      = "readiness_assignment_active"
	CodeShotsIncomplete                = "shots_incomplete"
	CodeArchiveAcknowledgementRequired = "archive_acknowledgement_required"
	CodeArchivedReadOnly               = "archived_read_only"
	CodeReopenRequired                 = "reopen_required"
	CodeExecutionHistoryAckRequired    = "execution_history_ack_required"
	CodeExecutionEventAlreadyVoid      = "execution_event_already_void"
	CodeSupersedesEventMismatch        = "supersedes_event_mismatch"
	CodeCustomerLinkConflict           = "customer_link_conflict"
	CodeOrderLinkConflict              = "order_link_conflict"
	CodeProjectionRevisionConflict     = "projection_revision_conflict"
	CodeProjectionMissing              = "projection_missing"
	CodeProjectionNotActive            = "projection_not_active"
	CodeProjectionNotFuture            = "projection_not_future"
	CodeSourceChanged                  = "source_changed"
	CodeOrderStillLinked               = "order_still_linked"
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

func abortErrorWithDetails(
	c *gin.Context,
	status int,
	code, message string,
	details ScheduleConflictDetails,
) {
	var union ErrorDetails
	if err := union.FromScheduleConflictDetails(details); err != nil {
		abortError(c, http.StatusInternalServerError, CodeInternal, "内部错误")
		return
	}
	abortErrorWithTypedDetails(c, status, code, message, union)
}

func abortErrorWithTypedDetails(
	c *gin.Context,
	status int,
	code, message string,
	details ErrorDetails,
) {
	env := newErrorEnvelope(code, message)
	env.Error.Details = &details
	c.AbortWithStatusJSON(status, env)
}
