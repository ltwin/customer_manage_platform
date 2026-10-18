package httpapi

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/oapi-codegen/nullable"

	"github.com/samson/customer-manage-platform/backend/internal/creativedocument"
	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
)

func registerCreativeDocuments(r *gin.RouterGroup, h *handlers) {
	w := ServerInterfaceWrapper{Handler: h, ErrorHandler: func(c *gin.Context, _ error, status int) {
		abortError(c, status, CodeValidationFailed, "请求参数不合法")
	}}
	r.GET("/creative/documents", w.ListCreativeDocuments)
	r.GET("/creative/documents/:id", w.GetCreativeDocument)
	r.POST("/creative/documents/:id/commands", w.CommandCreativeDocument)
	r.POST("/creative/documents/:id/trash", w.TrashCreativeDocument)
	r.POST("/creative/documents/:id/untrash", w.UntrashCreativeDocument)
	r.POST("/creative/documents/:id/purge", w.PurgeCreativeDocument)
	r.GET("/creative/documents/:id/revisions", w.ListCreativeDocumentRevisions)
	r.GET("/creative/documents/:id/revisions/:revision", w.GetCreativeDocumentRevision)
	r.GET("/creative/documents/:id/conversation", w.GetCreativeDocumentConversation)
	r.GET("/creative/documents/:id/scene-jobs", w.ListCreativeSceneJobs)
	r.POST("/creative/scene-jobs/claim", w.ClaimCreativeSceneJob)
	r.POST("/creative/scene-jobs/:job_id/lease", w.LeaseCreativeSceneJob)
	r.POST("/creative/scene-jobs/:job_id/complete", w.CompleteCreativeSceneJob)
}

// Document writes go through creativeWrite, which puts the path id into the
// payload so it is part of the idempotency hash.

func (h *handlers) GetCreativeDocument(c *gin.Context, id string) {
	scope, ok := h.creativeScope(c)
	if !ok {
		return
	}
	creativeRead(c, func() (json.RawMessage, error) {
		return creativedocument.Read(c.Request.Context(), scope, id)
	})
}

func (h *handlers) CommandCreativeDocument(c *gin.Context, id string, _ CommandCreativeDocumentParams) {
	h.creativeWrite(c, "document_id", id, creativedocument.Command)
}

func (h *handlers) TrashCreativeDocument(c *gin.Context, id string, _ TrashCreativeDocumentParams) {
	h.creativeWrite(c, "document_id", id, creativedocument.Trash)
}

func (h *handlers) UntrashCreativeDocument(c *gin.Context, id string, _ UntrashCreativeDocumentParams) {
	h.creativeWrite(c, "document_id", id, creativedocument.Untrash)
}

func (h *handlers) PurgeCreativeDocument(c *gin.Context, id string, _ PurgeCreativeDocumentParams) {
	h.creativeWrite(c, "document_id", id, creativedocument.Purge)
}

func (h *handlers) ListCreativeDocuments(c *gin.Context, p ListCreativeDocumentsParams) {
	scope, ok := h.creativeScope(c)
	if !ok {
		return
	}
	q := creativedocument.ListQuery{Type: valueOf(p.Type), Q: valueOf(p.Q), Trashed: valueOf(p.Trashed), Cursor: valueOf(p.Cursor)}
	if p.Limit != nil {
		if *p.Limit < 1 {
			creativeError(c, creativeops.Explain(creativeops.ErrValidation, "limit 必须在 1 到 50 之间"))
			return
		}
		q.Limit = *p.Limit
	}
	creativeRead(c, func() (creativedocument.ListPage, error) {
		return creativedocument.List(c.Request.Context(), scope, q)
	})
}

func (h *handlers) ListCreativeDocumentRevisions(c *gin.Context, id string, p ListCreativeDocumentRevisionsParams) {
	scope, ok := h.creativeScope(c)
	if !ok {
		return
	}
	q := creativedocument.RevisionQuery{RunID: valueOf(p.RunId), Cursor: valueOf(p.Cursor)}
	if p.Limit != nil {
		if *p.Limit < 1 {
			creativeError(c, creativeops.Explain(creativeops.ErrValidation, "limit 必须在 1 到 100 之间"))
			return
		}
		q.Limit = *p.Limit
	}
	if p.SinceRevision != nil {
		since, err := parseRevision(*p.SinceRevision)
		if err != nil {
			creativeError(c, err)
			return
		}
		q.SinceRevision = since
	}
	creativeRead(c, func() (creativedocument.RevisionPage, error) {
		return creativedocument.ListRevisions(c.Request.Context(), scope, id, q)
	})
}

func (h *handlers) GetCreativeDocumentRevision(c *gin.Context, id string, revision CreativeRevision, p GetCreativeDocumentRevisionParams) {
	scope, ok := h.creativeScope(c)
	if !ok {
		return
	}
	n, err := parseRevision(revision)
	if err != nil {
		creativeError(c, err)
		return
	}
	var units []string
	if p.Units != nil {
		units = strings.Split(*p.Units, ",")
	}
	creativeRead(c, func() (json.RawMessage, error) {
		return creativedocument.ReadRevision(c.Request.Context(), scope, id, n, units)
	})
}

func (h *handlers) GetCreativeDocumentConversation(c *gin.Context, id string, p GetCreativeDocumentConversationParams) {
	scope, ok := h.creativeScope(c)
	if !ok {
		return
	}
	creativeRead(c, func() (CreativeDocumentConversation, error) {
		conversation, err := creativedocument.ReadConversation(c.Request.Context(), scope, id, p.CanvasId)
		out := CreativeDocumentConversation{ConversationId: nullable.NewNullNullable[string]()}
		if conversation != nil {
			out.ConversationId = nullable.NewNullableWithValue(*conversation)
		}
		return out, err
	})
}

func valueOf[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}

func parseRevision(s string) (int64, error) {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 1 || strconv.FormatInt(n, 10) != s {
		return 0, creativeops.Explain(creativeops.ErrValidation, "修订号 %q 不合法", s)
	}
	return n, nil
}

func revisionOrNull(r *creativeops.Revision) nullable.Nullable[CreativeRevision] {
	if r == nil {
		return nullable.NewNullNullable[CreativeRevision]()
	}
	return nullable.NewNullableWithValue(strconv.FormatInt(int64(*r), 10))
}

// creativeDocumentError maps document errors, including those a canvas
// add_node meets when it creates or places a document.
func creativeDocumentError(c *gin.Context, err error) bool {
	var conflict *creativedocument.ConflictError
	switch {
	case errors.As(err, &conflict):
		details := CreativeDocumentConflictDetails{Conflicts: make([]CreativeDocumentConflict, 0, len(conflict.Conflicts))}
		for _, x := range conflict.Conflicts {
			details.Conflicts = append(details.Conflicts, CreativeDocumentConflict{Unit: x.Unit, ExpectedRev: revisionOrNull(x.ExpectedRev), CurrentRev: revisionOrNull(x.CurrentRev)})
		}
		var union ErrorDetails
		if e := union.FromCreativeDocumentConflictDetails(details); e != nil {
			abortError(c, 500, CodeInternal, "内部错误")
			return true
		}
		abortErrorWithTypedDetails(c, 409, "creative_document_conflict", "文档已被修改", union)
	case errors.Is(err, creativedocument.ErrTrashed):
		abortError(c, 409, "creative_state_conflict", "文档在回收站，请先恢复")
	case errors.Is(err, creativedocument.ErrNotTrashed):
		abortError(c, 409, "creative_state_conflict", "只能彻底删除回收站中的文档")
	case errors.Is(err, creativedocument.ErrSizeLimit):
		abortError(c, 413, "creative_size_limit", "文档超过大小上限")
	case errors.Is(err, creativedocument.ErrRevisionNotFound):
		abortError(c, 404, CodeNotFound, "修订不存在")
	case errors.Is(err, creativescene.ErrJobNotFound):
		abortError(c, 404, CodeNotFound, "任务不存在")
	// The host drops its work: the job was reclaimed, finished or cancelled, or
	// the run it belongs to may no longer write.
	case errors.Is(err, creativescene.ErrJobLeaseLost):
		abortError(c, 409, "creative_state_conflict", "任务已被回收或已结束，请丢弃这次的结果")
	case errors.Is(err, creativedocument.ErrNotFound):
		abortError(c, 404, CodeNotFound, "文档不存在")
	// Model failures never use 404 or creative_state_conflict: the editor reads
	// those as "the document is gone" and "the document is in the trash".
	case errors.Is(err, creativescene.ErrUploadUnavailable):
		abortError(c, 409, "creative_upload_unavailable", "模型上传已失效，请重新生成")
	case errors.Is(err, creativescene.ErrModelUnavailable):
		abortError(c, 400, "creative_model_unavailable", "引用的模型不可用，请重新读取场景")
	default:
		return false
	}
	return true
}
