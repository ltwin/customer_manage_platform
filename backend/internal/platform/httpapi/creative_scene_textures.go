package httpapi

import (
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// The built-in textures' files (api: 读取内置贴图). They belong to no
// account, so there is no ticket: any account that may read the creative
// space reads them. A file's name is its sha256, so it never changes and the
// browser keeps it.

var sceneTextureName = regexp.MustCompile(`^[0-9a-f]{64}\.jpg$`)

func registerSceneTextures(r *gin.RouterGroup, h *handlers) {
	w := ServerInterfaceWrapper{Handler: h, ErrorHandler: func(c *gin.Context, _ error, status int) {
		abortError(c, status, CodeValidationFailed, "请求参数不合法")
	}}
	r.GET("/creative/scene-textures/:file", w.GetCreativeSceneTexture)
	r.HEAD("/creative/scene-textures/:file", w.GetCreativeSceneTexture)
}

func (h *handlers) GetCreativeSceneTexture(c *gin.Context, file string, params GetCreativeSceneTextureParams) {
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
	if !sceneTextureName.MatchString(file) {
		abortError(c, 404, CodeNotFound, "没有这张内置贴图")
		return
	}
	etag := strconv.Quote(strings.TrimSuffix(file, ".jpg"))
	if params.IfNoneMatch != nil && *params.IfNoneMatch == etag {
		setSceneTextureCache(c, etag)
		c.Status(http.StatusNotModified)
		return
	}
	if h.sceneTextures == nil {
		abortError(c, 503, "creative_texture_unavailable", "内置贴图暂时无法读取")
		return
	}
	body, size, err := h.sceneTextures.Open(c.Request.Context(), file)
	switch {
	case errors.Is(err, creativescene.ErrBuiltinTextureUnknown):
		abortError(c, 404, CodeNotFound, "没有这张内置贴图")
		return
	case err != nil:
		if h.logger != nil {
			h.logger.ErrorContext(c.Request.Context(), "built-in texture unavailable", "file", file, "error", err)
		}
		var details ErrorDetails
		if e := details.FromCreativeSceneTextureDetails(CreativeSceneTextureDetails{File: file}); e != nil {
			abortError(c, 503, "creative_texture_unavailable", "内置贴图暂时无法读取")
			return
		}
		abortErrorWithTypedDetails(c, 503, "creative_texture_unavailable", "内置贴图暂时无法读取", details)
		return
	}
	defer func() { _ = body.Close() }()
	setSceneTextureCache(c, etag)
	c.Header("Content-Type", "image/jpeg")
	c.Header("Content-Length", strconv.FormatInt(size, 10))
	c.Header("X-Content-Type-Options", "nosniff")
	c.Status(http.StatusOK)
	if c.Request.Method == http.MethodHead {
		return
	}
	_, _ = io.Copy(c.Writer, body)
}

func setSceneTextureCache(c *gin.Context, etag string) {
	c.Header("ETag", etag)
	c.Header("Cache-Control", "private, max-age=31536000, immutable")
}
