package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/samson/customer-manage-platform/backend/internal/creativeagent"
	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
	"github.com/samson/customer-manage-platform/backend/internal/creativemedia"
	"github.com/samson/customer-manage-platform/backend/internal/creativeskill"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/jobs"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func registerCreativeCanvas(r *gin.RouterGroup, h *handlers) {
	registerCreativeLibrary(r, h)
	registerCreativeMedia(r, h)
	w := ServerInterfaceWrapper{Handler: h, ErrorHandler: func(c *gin.Context, _ error, status int) {
		abortError(c, status, CodeValidationFailed, "请求参数不合法")
	}}
	r.GET("/creative/assets", w.ListCreativeAssets)
	r.POST("/creative/assets", w.CreateCreativeTextAsset)
	r.GET("/creative/assets/:id", w.GetCreativeTextAsset)
	r.GET("/creative/projects", w.ListCreativeProjects)
	r.POST("/creative/projects", w.CreateCreativeProject)
	r.POST("/creative/projects/:id/archive", w.ArchiveCreativeProject)
	r.POST("/creative/projects/:id/restore", w.RestoreCreativeProject)
	r.POST("/creative/projects/:id/rename", w.RenameCreativeProject)
	r.GET("/creative/canvases/:id", w.GetCreativeCanvas)
	r.GET("/creative/canvases/:id/events", w.WatchCreativeCanvas)
	r.POST("/creative/canvases/:id/commands", w.CommandCreativeCanvas)
	r.GET("/creative/content-revisions/:id", w.GetCreativeContentRevision)
	r.GET("/creative/documents/:id", w.GetCreativeDocument)
	r.GET("/creative/canvases/:id/nodes/:node_id/versions", w.ListCreativeNodeVersions)
	r.GET("/creative/canvases/:id/executions/:execution_id", w.GetCreativeNodeExecution)
	r.POST("/creative/canvases/:id/executions", w.RequestCreativeNodeExecution)
}
func (h *handlers) creativeScope(c *gin.Context) (store.AccountScope, bool) {
	ac, ok := auth.AccountContextFrom(c.Request.Context())
	if !ok {
		abortError(c, 401, CodeUnauthorized, "请先登录")
		return store.AccountScope{}, false
	}
	if h.scopeFactory == nil {
		abortError(c, 503, "creative_dependency_unavailable", "创意空间暂不可用")
		return store.AccountScope{}, false
	}
	return h.scopeFactory.ScopeFor(ac), true
}
func creativeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, creativeops.ErrValidation):
		abortError(c, 400, CodeValidationFailed, "请求字段不合法")
	case errors.Is(err, creativeops.ErrConflict):
		abortError(c, 409, "idempotency_conflict", "操作标识已用于另一请求")
	case errors.Is(err, creativeops.ErrExpired):
		abortError(c, 409, "creative_operation_expired", "操作已超过恢复期限，请保留草稿")
	case errors.Is(err, creativecanvas.ErrVersionConflict), errors.Is(err, creativelibrary.ErrVersionConflict), errors.Is(err, creativeagent.ErrRevisionConflict):
		abortError(c, 409, "creative_revision_conflict", "内容已被其他窗口修改，本机这次修改未保存")
	case errors.Is(err, creativeagent.ErrConsentRevoked), errors.Is(err, creativeagent.ErrLimit), errors.Is(err, creativeagent.ErrNotFound):
		creativeAgentError(c, err)
	case errors.Is(err, creativeskill.ErrNotFound), errors.Is(err, creativeskill.ErrLimit),
		errors.Is(err, creativeskill.ErrResourceUnavailable), errors.Is(err, creativeskill.ErrContentMismatch):
		creativeSkillError(c, err)
	case errors.Is(err, creativelibrary.ErrTrashed):
		abortError(c, 409, "creative_asset_trashed", "资产已在回收站，请恢复后编辑")
	case errors.Is(err, creativecanvas.ErrArchived):
		abortError(c, 409, "archived_read_only", "项目已归档，请恢复后编辑")
	case errors.Is(err, creativecanvas.ErrNotFound), errors.Is(err, creativelibrary.ErrNotFound), errors.Is(err, creativecontent.ErrNotFound):
		abortError(c, 404, CodeNotFound, "创意内容不存在")
	case errors.Is(err, creativecontent.ErrUsageDenied):
		abortError(c, 403, "creative_usage_denied", "此内容当前不允许使用")
	case errors.Is(err, store.ErrCreativeAccessDenied):
		abortError(c, 403, CodeForbidden, "创意空间当前不可用")
	case errors.Is(err, creativecanvas.ErrActionUnavailable):
		abortError(c, 422, "creative_action_unavailable", "此节点动作尚未配置")
	case errors.Is(err, creativecanvas.ErrLimit):
		abortError(c, 422, "creative_size_limit", "本次操作涉及的节点或关联过多，请缩小选区")
	case errors.Is(err, store.ErrCommitOutcomeUnknown):
		abortError(c, 503, "creative_commit_unknown", "提交结果待确认，请保留原操作重查")
	case errors.Is(err, creativemedia.ErrNotFound), errors.Is(err, creativemedia.ErrState), errors.Is(err, creativemedia.ErrExpired), errors.Is(err, creativemedia.ErrUnsupported), errors.Is(err, creativemedia.ErrSizeLimit), errors.Is(err, creativemedia.ErrQuota), errors.Is(err, creativemedia.ErrTicket), errors.Is(err, creativemedia.ErrRange), errors.Is(err, creativemedia.ErrEpoch), errors.Is(err, creativemedia.ErrUnknownResult), errors.Is(err, jobs.ErrNoHandlers):
		creativeMediaError(c, err)
	default:
		_ = c.Error(err) // Shared middleware records and renders unexpected errors.
	}
}
func creativeRead[T any](c *gin.Context, read func() (T, error)) {
	value, err := read()
	if err != nil {
		creativeError(c, err)
		return
	}
	c.JSON(200, value)
}
func (h *handlers) ListCreativeAssets(c *gin.Context, p ListCreativeAssetsParams) {
	scope, ok := h.creativeScope(c)
	if !ok {
		return
	}
	limit, cursor := 30, ""
	if p.Limit != nil {
		limit = *p.Limit
	}
	if p.Cursor != nil {
		cursor = *p.Cursor
	}
	creativeRead(c, func() (creativelibrary.AssetPage, error) {
		q := creativelibrary.Search{Limit: limit, Cursor: cursor}
		if p.View != nil {
			q.View = string(*p.View)
		}
		if p.GroupId != nil {
			q.GroupID = *p.GroupId
		}
		if p.IncludeDescendants != nil {
			q.Descendants = *p.IncludeDescendants
		}
		if p.Kind != nil {
			q.Kind = string(*p.Kind)
		}
		if p.Q != nil {
			q.Q = *p.Q
		}
		if p.TagIds != nil {
			q.TagIDs = *p.TagIds
		}
		if p.TagMode != nil {
			q.TagMode = string(*p.TagMode)
		}
		if p.Sort != nil {
			q.Sort = string(*p.Sort)
		}
		return creativelibrary.SearchAssets(c.Request.Context(), scope, q)
	})
}
func (h *handlers) ListCreativeProjects(c *gin.Context, p ListCreativeProjectsParams) {
	scope, ok := h.creativeScope(c)
	if !ok {
		return
	}
	limit, cursor, archived := 30, "", false
	if p.Limit != nil {
		limit = *p.Limit
	}
	if p.Cursor != nil {
		cursor = *p.Cursor
	}
	if p.Archived != nil {
		archived = *p.Archived
	}
	creativeRead(c, func() (creativecanvas.ProjectPage, error) {
		return creativecanvas.ListProjects(c.Request.Context(), scope, archived, limit, cursor)
	})
}
func (h *handlers) GetCreativeTextAsset(c *gin.Context, id string) {
	scope, ok := h.creativeScope(c)
	if !ok {
		return
	}
	creativeRead(c, func() (creativelibrary.Asset, error) { return creativelibrary.GetAsset(c.Request.Context(), scope, id) })
}
func (h *handlers) GetCreativeCanvas(c *gin.Context, id string) {
	scope, ok := h.creativeScope(c)
	if !ok {
		return
	}
	creativeRead(c, func() (creativecanvas.Canvas, error) { return creativecanvas.GetCanvas(c.Request.Context(), scope, id) })
}
func (h *handlers) GetCreativeContentRevision(c *gin.Context, id string) {
	scope, ok := h.creativeScope(c)
	if !ok {
		return
	}
	creativeRead(c, func() (creativecontent.Revision, error) { return creativecontent.Read(c.Request.Context(), scope, id) })
}

type creativeApplication func(context.Context, store.AccountScope, creativeops.Command) (creativeops.Receipt, error)
type creativeEnvelope struct {
	OperationID string          `json:"operation_id"`
	CreatedAt   time.Time       `json:"client_created_at"`
	Payload     json.RawMessage `json:"payload"`
}

func (h *handlers) creativeWrite(c *gin.Context, targetKey, target string, apply creativeApplication) {
	scope, ok := h.creativeScope(c)
	if !ok {
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20))
	if err != nil {
		abortError(c, 413, "creative_size_limit", "请求超过大小上限")
		return
	}
	var envelope creativeEnvelope
	if err := creativeops.Decode(body, &envelope); err != nil {
		creativeError(c, err)
		return
	}
	if err := creativeops.ValidateOperationKey(c.GetHeader("Idempotency-Key"), envelope.OperationID); err != nil {
		creativeError(c, err)
		return
	}
	var payload map[string]json.RawMessage
	if err := creativeops.Decode(envelope.Payload, &payload); err != nil {
		creativeError(c, err)
		return
	}
	if targetKey != "" {
		if _, exists := payload[targetKey]; exists {
			creativeError(c, creativeops.ErrValidation)
			return
		}
		payload[targetKey], err = json.Marshal(target)
		if err != nil {
			creativeError(c, err)
			return
		}
	}
	if apply == nil {
		var kind string
		if err := json.Unmarshal(payload["type"], &kind); err != nil {
			creativeError(c, creativeops.ErrValidation)
			return
		}
		delete(payload, "type")
		switch kind {
		case "reuse_version_prompt":
			apply = creativecanvas.ReuseVersionPrompt
		case "cancel_execution":
			apply = creativecanvas.CancelNodeExecution
		case "save_prompt":
			apply = creativecanvas.SaveNodePrompt
		case "batch":
			apply = creativecanvas.ApplyCommands
		case "undo":
			apply = creativecanvas.Undo
		case "redo":
			apply = creativecanvas.Redo
		case "add_node":
			apply = creativecanvas.AddNode
		case "move_node":
			apply = creativecanvas.MoveNode
		case "replace_content":
			apply = creativecanvas.ReplaceContent
		default:
			creativeError(c, creativeops.ErrValidation)
			return
		}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		creativeError(c, err)
		return
	}
	receipt, err := apply(c.Request.Context(), scope, creativeops.Command{OperationID: envelope.OperationID, CreatedAt: envelope.CreatedAt, Payload: raw})
	if err != nil {
		creativeError(c, err)
		return
	}
	c.Data(receipt.Outcome.HTTPStatus, "application/json", receipt.Outcome.Response)
}
func (h *handlers) CreateCreativeTextAsset(c *gin.Context, _ CreateCreativeTextAssetParams) {
	h.creativeWrite(c, "", "", creativelibrary.CreateAsset)
}
func (h *handlers) CreateCreativeProject(c *gin.Context, _ CreateCreativeProjectParams) {
	h.creativeWrite(c, "", "", creativecanvas.CreateProject)
}
func (h *handlers) ArchiveCreativeProject(c *gin.Context, id string, _ ArchiveCreativeProjectParams) {
	h.creativeWrite(c, "project_id", id, creativecanvas.ArchiveProject)
}
func (h *handlers) RestoreCreativeProject(c *gin.Context, id string, _ RestoreCreativeProjectParams) {
	h.creativeWrite(c, "project_id", id, creativecanvas.RestoreProject)
}
func (h *handlers) RenameCreativeProject(c *gin.Context, id string, _ RenameCreativeProjectParams) {
	h.creativeWrite(c, "project_id", id, creativecanvas.RenameProject)
}
func (h *handlers) CommandCreativeCanvas(c *gin.Context, id string, _ CommandCreativeCanvasParams) {
	h.creativeWrite(c, "canvas_id", id, nil)
}

func (h *handlers) ListCreativeNodeVersions(c *gin.Context, id string, nodeID string) {
	scope, ok := h.creativeScope(c)
	if !ok {
		return
	}
	creativeRead(c, func() (creativecanvas.VersionPage, error) {
		return creativecanvas.ListNodeVersions(c.Request.Context(), scope, id, nodeID)
	})
}

func (h *handlers) GetCreativeDocument(c *gin.Context, id string) {
	scope, ok := h.creativeScope(c)
	if !ok {
		return
	}
	creativeRead(c, func() (creativecanvas.Document, error) {
		return creativecanvas.ReadDocument(c.Request.Context(), scope, id)
	})
}
func (h *handlers) GetCreativeNodeExecution(c *gin.Context, id string, executionID string) {
	scope, ok := h.creativeScope(c)
	if !ok {
		return
	}
	creativeRead(c, func() (creativecanvas.NodeExecution, error) {
		return creativecanvas.GetNodeExecution(c.Request.Context(), scope, creativecanvas.ExecutionTarget{CanvasID: id, ExecutionID: executionID})
	})
}
func (h *handlers) RequestCreativeNodeExecution(c *gin.Context, id string, _ RequestCreativeNodeExecutionParams) {
	h.creativeWrite(c, "canvas_id", id, func(ctx context.Context, scope store.AccountScope, command creativeops.Command) (creativeops.Receipt, error) {
		service, err := creativecanvas.NewExecutionService(nil)
		if err != nil {
			return creativeops.Receipt{}, err
		}
		return service.Request(ctx, scope, command, nil)
	})
}
