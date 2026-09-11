package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func (h *handlers) GetCreativeCapabilities(c *gin.Context) {

	result := CreativeFoundationCapabilities{SchemaVersion: 1, Reason: "foundation_only", NodeTypes: []string{}, Tools: []string{}}
	if h.scopeFactory == nil {
		c.JSON(http.StatusOK, result)
		return
	}
	scope, ok := h.creativeScope(c)
	if !ok {
		return
	}
	capability, err := scope.CreativeCapabilities(c.Request.Context())
	if err != nil {
		creativeError(c, err)
		return
	}
	if capability.Read {
		result.Available = true
		result.Reason = "read_only"
		for _, definition := range creativecanvas.NodeDefinitions() {
			if definition.CreationSupported {
				result.NodeTypes = append(result.NodeTypes, definition.TypeKey)
			}
		}
		if capability.ManualWrite {
			result.Reason = "available"
			result.Tools = []string{"create_asset", "create_project", "rename_project", "archive_project", "restore_project", "add_node", "move_node", "replace_content", "batch", "undo", "redo"}
		}
	}
	c.JSON(http.StatusOK, result)
}

func (h *handlers) GetCreativeOperation(c *gin.Context, operationID uuid.UUID) {
	ac, ok := auth.AccountContextFrom(c.Request.Context())
	if !ok {
		abortError(c, 401, CodeUnauthorized, "请先登录")
		return
	}
	if h.scopeFactory == nil {
		abortError(c, 503, "creative_dependency_unavailable", "创意空间暂不可用")
		return
	}
	receipt, err := (creativeops.Executor{}).Lookup(c.Request.Context(), h.scopeFactory.ScopeFor(ac), operationID.String())
	if err != nil {
		switch {
		case errors.Is(err, creativeops.ErrNotFound):
			abortError(c, 404, CodeNotFound, "操作记录不存在")
		case errors.Is(err, creativeops.ErrExpired):
			abortError(c, 409, "creative_operation_expired", "操作记录已超过恢复期限")
		case errors.Is(err, creativeops.ErrValidation):
			abortError(c, 400, CodeValidationFailed, "操作标识不合法")
		case errors.Is(err, store.ErrCreativeAccessDenied):
			abortError(c, 403, CodeForbidden, "创意空间当前不可用")
		case errors.Is(err, store.ErrCommitOutcomeUnknown):
			abortError(c, 503, "creative_commit_unknown", "请保留操作标识并重查结果")
		default:
			_ = c.Error(err) // The shared envelope middleware logs and renders it.
		}
		return
	}
	var response map[string]interface{}
	decoder := json.NewDecoder(bytes.NewReader(receipt.Outcome.Response))
	decoder.UseNumber()
	if err := decoder.Decode(&response); err != nil {
		_ = c.Error(err)
		return
	}
	result := CreativeOperationReceipt{OperationId: operationID, OperationType: receipt.OperationType, HttpStatus: CreativeOperationReceiptHttpStatus(receipt.Outcome.HTTPStatus), Response: response, ResultKind: receipt.Outcome.ResultKind, ResultId: receipt.Outcome.ResultID, CreatedAt: receipt.CreatedAt, RetainedUntil: receipt.RetainedUntil}
	if receipt.Outcome.ResultRevision != nil {
		revision := strconv.FormatInt(int64(*receipt.Outcome.ResultRevision), 10)
		result.ResultRevision = &revision
	}
	c.JSON(http.StatusOK, result)
}
