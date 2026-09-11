package httpapi

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/creativemedia"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// registerCreativeMedia adds the bearer-authenticated media routes.
func registerCreativeMedia(r *gin.RouterGroup, h *handlers) {
	w := ServerInterfaceWrapper{Handler: h, ErrorHandler: func(c *gin.Context, _ error, status int) {
		abortError(c, status, CodeValidationFailed, "请求参数不合法")
	}}
	r.GET("/creative/media-capabilities", w.GetCreativeMediaCapabilities)
	r.POST("/creative/uploads", w.CreateCreativeUpload)
	r.GET("/creative/uploads/:id", w.GetCreativeUpload)
	r.POST("/creative/uploads/:id/part-authorizations", w.AuthorizeCreativeUploadParts)
	r.POST("/creative/uploads/:id/complete", w.CompleteCreativeUpload)
	r.POST("/creative/uploads/:id/cancel", w.CancelCreativeUpload)
	r.GET("/creative/upload-candidates", w.ListCreativeUploadCandidates)
	r.POST("/creative/upload-candidates/:id/adopt", w.AdoptCreativeUploadCandidate)
	r.POST("/creative/upload-candidates/:id/discard", w.DiscardCreativeUploadCandidate)
	r.POST("/creative/media-access-tickets", w.IssueCreativeMediaTicket)
	r.POST("/creative/assets/from-canvas-node", w.CreateCreativeAssetFromNode)
}

// registerCreativeMediaBytes serves signed part writes and ticketed reads.
// Both validate a server-issued capability instead of a bearer token, so
// browser media tags and plain PUTs can reach them.
func registerCreativeMediaBytes(r *gin.RouterGroup, h *handlers) {
	w := ServerInterfaceWrapper{Handler: h, ErrorHandler: func(c *gin.Context, _ error, status int) {
		abortError(c, status, CodeValidationFailed, "请求参数不合法")
	}}
	r.PUT("/creative/media-parts", w.PutCreativeMediaPart)
	r.GET("/creative/media/:revision_id/:role", w.GetCreativeMedia)
	r.HEAD("/creative/media/:revision_id/:role", w.GetCreativeMedia)
}

func (h *handlers) mediaService(c *gin.Context) (*creativemedia.Service, bool) {
	if h.creativeMedia == nil {
		abortError(c, 503, "creative_dependency_unavailable", "媒体上传暂不可用")
		return nil, false
	}
	return h.creativeMedia, true
}
func creativeMediaError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, creativemedia.ErrNotFound):
		abortError(c, 404, CodeNotFound, "上传会话不存在")
	case errors.Is(err, creativemedia.ErrState):
		abortError(c, 409, "creative_state_conflict", "当前状态不允许此操作")
	case errors.Is(err, creativemedia.ErrExpired):
		abortError(c, 409, "creative_state_conflict", "上传会话已到期，请重新选择文件")
	case errors.Is(err, creativemedia.ErrUnsupported):
		abortError(c, 415, "creative_media_unsupported", "媒体格式不支持或与内容不一致")
	case errors.Is(err, creativemedia.ErrSizeLimit):
		abortError(c, 413, "creative_size_limit", "文件超过大小上限")
	case errors.Is(err, creativemedia.ErrQuota):
		abortError(c, 429, "creative_quota_exceeded", "媒体存储额度不足")
	case errors.Is(err, creativemedia.ErrTicket):
		abortError(c, 403, CodeForbidden, "媒体访问票据无效或已过期")
	case errors.Is(err, creativemedia.ErrRange):
		abortError(c, 416, "creative_range_unsatisfiable", "请求范围超出媒体大小")
	case errors.Is(err, creativemedia.ErrEpoch), errors.Is(err, creativemedia.ErrUnknownResult):
		abortError(c, 503, "creative_dependency_unavailable", "媒体处理暂时不可用，请稍后重查")
	default:
		creativeError(c, err)
	}
}

func (h *handlers) GetCreativeMediaCapabilities(c *gin.Context) {
	svc, ok := h.mediaService(c)
	if !ok {
		return
	}
	if _, ok := h.creativeScope(c); !ok {
		return
	}
	c.JSON(200, svc.Capabilities())
}
func (h *handlers) mediaWrite(c *gin.Context, targetKey, target string, apply creativeApplication) {
	if _, ok := h.mediaService(c); !ok {
		return
	}
	h.creativeWrite(c, targetKey, target, apply)
}
func (h *handlers) CreateCreativeUpload(c *gin.Context, _ CreateCreativeUploadParams) {
	h.mediaWrite(c, "", "", h.creativeMedia.CreateUpload)
}
func (h *handlers) CompleteCreativeUpload(c *gin.Context, id string, _ CompleteCreativeUploadParams) {
	h.mediaWrite(c, "upload_id", id, h.creativeMedia.CompleteUpload)
}
func (h *handlers) CancelCreativeUpload(c *gin.Context, id string, _ CancelCreativeUploadParams) {
	h.mediaWrite(c, "upload_id", id, h.creativeMedia.CancelUpload)
}
func (h *handlers) AdoptCreativeUploadCandidate(c *gin.Context, id string, _ AdoptCreativeUploadCandidateParams) {
	h.mediaWrite(c, "candidate_id", id, h.creativeMedia.AdoptCandidate)
}
func (h *handlers) DiscardCreativeUploadCandidate(c *gin.Context, id string, _ DiscardCreativeUploadCandidateParams) {
	h.mediaWrite(c, "candidate_id", id, h.creativeMedia.DiscardCandidate)
}
func (h *handlers) CreateCreativeAssetFromNode(c *gin.Context, _ CreateCreativeAssetFromNodeParams) {
	h.creativeWrite(c, "", "", creativecanvas.SaveNodeToLibrary)
}
func (h *handlers) GetCreativeUpload(c *gin.Context, id string) {
	svc, ok := h.mediaService(c)
	if !ok {
		return
	}
	scope, ok := h.creativeScope(c)
	if !ok {
		return
	}
	view, err := svc.GetUpload(c.Request.Context(), scope, id)
	if err != nil {
		creativeMediaError(c, err)
		return
	}
	c.JSON(200, view)
}
func (h *handlers) ListCreativeUploadCandidates(c *gin.Context) {
	svc, ok := h.mediaService(c)
	if !ok {
		return
	}
	scope, ok := h.creativeScope(c)
	if !ok {
		return
	}
	page, err := svc.ListCandidates(c.Request.Context(), scope)
	if err != nil {
		creativeMediaError(c, err)
		return
	}
	c.JSON(200, page)
}
func readSmallJSON(c *gin.Context, target any) bool {
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10))
	if err != nil {
		abortError(c, 413, "creative_size_limit", "请求超过大小上限")
		return false
	}
	if err := creativeops.Decode(body, target); err != nil {
		creativeError(c, err)
		return false
	}
	return true
}
func (h *handlers) AuthorizeCreativeUploadParts(c *gin.Context, id string) {
	svc, ok := h.mediaService(c)
	if !ok {
		return
	}
	scope, ok := h.creativeScope(c)
	if !ok {
		return
	}
	var input creativemedia.AuthorizePartsInput
	if !readSmallJSON(c, &input) {
		return
	}
	result, err := svc.AuthorizeParts(c.Request.Context(), scope, id, input.PartNumbers)
	if err != nil {
		creativeMediaError(c, err)
		return
	}
	setNoStore(c)
	c.JSON(200, result)
}
func (h *handlers) IssueCreativeMediaTicket(c *gin.Context) {
	svc, ok := h.mediaService(c)
	if !ok {
		return
	}
	scope, ok := h.creativeScope(c)
	if !ok {
		return
	}
	var input creativemedia.TicketInput
	if !readSmallJSON(c, &input) {
		return
	}
	ticket, err := svc.IssueTicket(c.Request.Context(), scope, input)
	if err != nil {
		creativeMediaError(c, err)
		return
	}
	setNoStore(c)
	c.JSON(200, ticket)
}
func (h *handlers) PutCreativeMediaPart(c *gin.Context, params PutCreativeMediaPartParams) {
	svc, ok := h.mediaService(c)
	if !ok {
		return
	}
	if err := svc.WriteLocalPart(c.Request.Context(), params.Token, c.Request.Body); err != nil {
		creativeMediaError(c, err)
		return
	}
	c.Status(200)
}

// parseRange accepts exactly one bytes range; anything else is unsatisfiable.
func parseRange(header string, size int64) (*creativemedia.ByteRange, error) {
	if header == "" {
		return nil, nil
	}
	spec, ok := strings.CutPrefix(header, "bytes=")
	if !ok || strings.Contains(spec, ",") {
		return nil, creativemedia.ErrRange
	}
	start, end, ok := strings.Cut(spec, "-")
	if !ok {
		return nil, creativemedia.ErrRange
	}
	var r creativemedia.ByteRange
	if start == "" {
		suffix, err := strconv.ParseInt(end, 10, 64)
		if err != nil || suffix <= 0 {
			return nil, creativemedia.ErrRange
		}
		if suffix > size {
			suffix = size
		}
		r.Start, r.End = size-suffix, size-1
		return &r, nil
	}
	var err error
	if r.Start, err = strconv.ParseInt(start, 10, 64); err != nil || r.Start < 0 {
		return nil, creativemedia.ErrRange
	}
	if end == "" {
		r.End = size - 1
	} else if r.End, err = strconv.ParseInt(end, 10, 64); err != nil {
		return nil, creativemedia.ErrRange
	}
	if r.End >= size {
		r.End = size - 1
	}
	if r.Start > r.End || r.Start >= size {
		return nil, creativemedia.ErrRange
	}
	return &r, nil
}
func (h *handlers) GetCreativeMedia(c *gin.Context, revisionID string, role GetCreativeMediaParamsRole, params GetCreativeMediaParams) {
	svc, ok := h.mediaService(c)
	if !ok {
		return
	}
	if h.scopeFactory == nil {
		abortError(c, 503, "creative_dependency_unavailable", "创意空间暂不可用")
		return
	}
	scopeFor := func(account string) store.AccountScope {
		return h.scopeFactory.ScopeFor(auth.AccountContext{AccountID: account})
	}
	// One authorization, one object open: the range is resolved from the
	// authorized size inside Open. Unsatisfiable ranges still answer 416 with
	// the total size only after the ticket and usage checks passed.
	header := c.GetHeader("Range")
	stream, err := svc.Open(c.Request.Context(), scopeFor, params.Ticket, revisionID, string(role), func(size int64) (*creativemedia.ByteRange, error) {
		return parseRange(header, size)
	})
	if errors.Is(err, creativemedia.ErrRange) && stream != nil {
		c.Header("Content-Range", "bytes */"+strconv.FormatInt(stream.Size, 10))
		abortError(c, 416, "creative_range_unsatisfiable", "请求范围超出媒体大小")
		return
	}
	if err != nil {
		creativeMediaError(c, err)
		return
	}
	rng := stream.Range
	defer func() { _ = stream.Body.Close() }()
	c.Header("Content-Type", stream.Mime)
	c.Header("ETag", strconv.Quote(stream.ETag))
	c.Header("Cache-Control", "private, no-store")
	c.Header("Accept-Ranges", "bytes")
	c.Header("X-Content-Type-Options", "nosniff")
	if stream.Download {
		c.Header("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(safeFileName(stream.FileName)))
	} else {
		c.Header("Content-Disposition", "inline")
	}
	status, length := http.StatusOK, stream.Size
	if rng != nil {
		status, length = http.StatusPartialContent, rng.End-rng.Start+1
		c.Header("Content-Range", "bytes "+strconv.FormatInt(rng.Start, 10)+"-"+strconv.FormatInt(rng.End, 10)+"/"+strconv.FormatInt(stream.Size, 10))
	}
	c.Header("Content-Length", strconv.FormatInt(length, 10))
	c.Status(status)
	if c.Request.Method == http.MethodHead {
		return
	}
	_, _ = io.Copy(c.Writer, stream.Body)
}
func safeFileName(name string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || strings.ContainsRune(`\/:*?"<>|;`, r) {
			return '_'
		}
		return r
	}, name)
}
