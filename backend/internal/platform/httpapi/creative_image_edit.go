package httpapi

import (
	"encoding/json"
	"errors"

	"github.com/gin-gonic/gin"
	"github.com/samson/customer-manage-platform/backend/internal/creativeimageedit"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func registerCreativeImageEdit(r *gin.RouterGroup, h *handlers) {
	w := ServerInterfaceWrapper{Handler: h, ErrorHandler: func(c *gin.Context, _ error, status int) {
		abortError(c, status, CodeValidationFailed, "请求参数不合法")
	}}
	r.GET("/creative/image-edit-capabilities", w.GetCreativeImageEditCapabilities)
	r.POST("/creative/image-edit-renders", w.CreateCreativeImageEditRender)
	r.GET("/creative/image-edit-renders/:id", w.GetCreativeImageEditRender)
	r.POST("/creative/image-edit-renders/:id/discard", w.DiscardCreativeImageEditRender)
	r.POST("/creative/canvases/:id/nodes/:node_id/image-edits/apply", w.ApplyCreativeImageEdit)
}

func (h *handlers) GetCreativeImageEditCapabilities(c *gin.Context) {
	scope, ok := h.creativeScope(c)
	if !ok {
		return
	}
	if err := scope.WithReadSnapshot(c.Request.Context(), func(tx store.ReadTxAccountScope) error {
		return tx.RequireCreativeRead(c.Request.Context())
	}); err != nil {
		creativeError(c, err)
		return
	}
	setNoStore(c)
	c.JSON(200, h.creativeImageEdit.Capabilities())
}

func (h *handlers) imageEditAvailable(c *gin.Context) bool {
	if h.creativeImageEdit != nil {
		return true
	}
	abortError(c, 503, "creative_image_edit_unavailable", "图片编辑暂不可用")
	return false
}

func (h *handlers) CreateCreativeImageEditRender(c *gin.Context, _ CreateCreativeImageEditRenderParams) {
	scope, ok := h.creativeScope(c)
	if !ok || !h.imageEditAvailable(c) {
		return
	}
	var in struct {
		OperationID      string          `json:"operation_id"`
		CanvasID         string          `json:"canvas_id"`
		NodeID           string          `json:"node_id"`
		SourceRevisionID string          `json:"source_revision_id"`
		Action           string          `json:"action"`
		Parameters       json.RawMessage `json:"parameters"`
	}
	if !readSmallJSON(c, &in) {
		return
	}
	if err := creativeops.ValidateOperationKey(c.GetHeader("Idempotency-Key"), in.OperationID); err != nil {
		creativeError(c, err)
		return
	}
	req := creativeimageedit.CreateInput{OperationID: in.OperationID, CanvasID: in.CanvasID, NodeID: in.NodeID, SourceRevisionID: in.SourceRevisionID, Action: in.Action, CallerKind: "photographer"}
	var err error
	switch in.Action {
	case creativeimageedit.ActionCompress:
		var p struct {
			Quality int `json:"quality"`
		}
		err = creativeops.Decode(in.Parameters, &p)
		req.Quality = p.Quality
	case creativeimageedit.ActionCrop:
		var p struct {
			Left   *int `json:"left"`
			Top    *int `json:"top"`
			Width  *int `json:"width"`
			Height *int `json:"height"`
		}
		err = creativeops.Decode(in.Parameters, &p)
		if err == nil {
			if p.Left == nil || p.Top == nil || p.Width == nil || p.Height == nil {
				err = creativeops.ErrValidation
			} else {
				req.Crop = creativeimageedit.Crop{Left: *p.Left, Top: *p.Top, Width: *p.Width, Height: *p.Height}
			}
		}
	default:
		err = creativeops.ErrValidation
	}
	if err != nil {
		creativeError(c, err)
		return
	}
	result, err := h.creativeImageEdit.Create(c.Request.Context(), scope, req)
	if err != nil {
		creativeError(c, err)
		return
	}
	setNoStore(c)
	c.JSON(202, result)
}

func (h *handlers) GetCreativeImageEditRender(c *gin.Context, id string) {
	scope, ok := h.creativeScope(c)
	if !ok || !h.imageEditAvailable(c) {
		return
	}
	setNoStore(c)
	creativeRead(c, func() (creativeimageedit.Render, error) {
		return h.creativeImageEdit.Get(c.Request.Context(), scope, id)
	})
}

func (h *handlers) DiscardCreativeImageEditRender(c *gin.Context, id string) {
	scope, ok := h.creativeScope(c)
	if !ok || !h.imageEditAvailable(c) {
		return
	}
	setNoStore(c)
	creativeRead(c, func() (creativeimageedit.Render, error) {
		return h.creativeImageEdit.Discard(c.Request.Context(), scope, id)
	})
}

func (h *handlers) ApplyCreativeImageEdit(c *gin.Context, id, nodeID string, _ ApplyCreativeImageEditParams) {
	if !h.imageEditAvailable(c) {
		return
	}
	h.creativeWriteTargets(c, map[string]string{"canvas_id": id, "node_id": nodeID}, h.creativeImageEdit.Apply)
}

func imageEditError(c *gin.Context, err error) bool {
	var busy creativeimageedit.BusyError
	switch {
	case errors.As(err, &busy):
		var details ErrorDetails
		if e := details.FromCreativeImageEditBusyDetails(CreativeImageEditBusyDetails{RenderId: busy.RenderID}); e != nil {
			abortError(c, 500, CodeInternal, "内部错误")
			return true
		}
		abortErrorWithTypedDetails(c, 409, "creative_image_edit_node_busy", "节点已有图片编辑候选，可继续采用或丢弃", details)
	case errors.Is(err, creativeimageedit.ErrNotReduced):
		abortError(c, 422, "creative_image_edit_not_reduced", "压缩未减小文件，请调整质量后重试")
	case errors.Is(err, creativeimageedit.ErrColour):
		abortError(c, 422, "creative_image_edit_colour_unknown", "图片色彩信息无法可靠识别")
	case errors.Is(err, creativeimageedit.ErrNotFound):
		abortError(c, 404, CodeNotFound, "图片编辑候选不存在")
	case errors.Is(err, creativeimageedit.ErrNodeBusy):
		abortError(c, 409, "creative_image_edit_node_busy", "节点有尚未完成的任务")
	case errors.Is(err, creativeimageedit.ErrBusy):
		abortError(c, 429, "creative_image_edit_busy", "图片编辑任务过多，请稍后重试")
	case errors.Is(err, creativeimageedit.ErrIdempotency):
		abortError(c, 409, "idempotency_conflict", "操作标识已用于另一请求")
	case errors.Is(err, creativeimageedit.ErrSourceChanged):
		abortError(c, 409, "creative_image_edit_source_changed", "节点内容已变化，请重新编辑")
	case errors.Is(err, creativeimageedit.ErrRenderNotReady):
		abortError(c, 409, "creative_image_edit_render_not_ready", "图片编辑候选尚不可采用")
	case errors.Is(err, creativeimageedit.ErrExpired):
		abortError(c, 410, "creative_image_edit_expired", "图片编辑候选已过期或丢弃，请重新编辑")
	default:
		return false
	}
	return true
}
