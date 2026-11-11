package httpapi

import (
	"context"
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

// The built-in models' GLBs (api: 读取内置模型文件), read as the built-in
// textures are: no account owns them, a file's name is its sha256 and never
// changes.

var sceneModelName = regexp.MustCompile(`^[0-9a-f]{64}\.glb$`)

// SceneModelFiles reads the built-in models' files
// (creativescene.BuiltinModels): ErrBuiltinModelUnknown for a name the
// catalog does not list, any other error when the store cannot give it.
type SceneModelFiles interface {
	Open(ctx context.Context, name string) (io.ReadCloser, int64, error)
}

func registerSceneModels(r *gin.RouterGroup, h *handlers) {
	w := ServerInterfaceWrapper{Handler: h, ErrorHandler: func(c *gin.Context, _ error, status int) {
		abortError(c, status, CodeValidationFailed, "请求参数不合法")
	}}
	r.GET("/creative/scene-models/:file", w.GetCreativeSceneModel)
	r.HEAD("/creative/scene-models/:file", w.GetCreativeSceneModel)
}

func (h *handlers) GetCreativeSceneModel(c *gin.Context, file string, params GetCreativeSceneModelParams) {
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
	if !sceneModelName.MatchString(file) {
		abortError(c, 404, CodeNotFound, "没有这个内置模型")
		return
	}
	etag := strconv.Quote(strings.TrimSuffix(file, ".glb"))
	if params.IfNoneMatch != nil && *params.IfNoneMatch == etag {
		setSceneTextureCache(c, etag)
		c.Status(http.StatusNotModified)
		return
	}
	unavailable := func(err error) {
		if h.logger != nil {
			h.logger.ErrorContext(c.Request.Context(), "built-in model unavailable", "file", file, "error", err)
		}
		var details ErrorDetails
		if e := details.FromCreativeSceneTextureDetails(CreativeSceneTextureDetails{File: file}); e != nil {
			abortError(c, 503, "creative_model_file_unavailable", "内置模型暂时无法读取")
			return
		}
		abortErrorWithTypedDetails(c, 503, "creative_model_file_unavailable", "内置模型暂时无法读取", details)
	}
	if h.sceneModels == nil {
		unavailable(errors.New("no built-in model store"))
		return
	}
	body, size, err := h.sceneModels.Open(c.Request.Context(), file)
	switch {
	case errors.Is(err, creativescene.ErrBuiltinModelUnknown):
		abortError(c, 404, CodeNotFound, "没有这个内置模型")
		return
	case err != nil:
		unavailable(err)
		return
	}
	defer func() { _ = body.Close() }()
	setSceneTextureCache(c, etag)
	c.Header("Content-Type", "model/gltf-binary")
	c.Header("Content-Length", strconv.FormatInt(size, 10))
	c.Header("X-Content-Type-Options", "nosniff")
	c.Status(http.StatusOK)
	if c.Request.Method == http.MethodHead {
		return
	}
	_, _ = io.Copy(c.Writer, body)
}
