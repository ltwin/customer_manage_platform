package httpapi

import (
	"errors"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/platform/realtime"
)

func (h *handlers) WatchCreativeCanvas(c *gin.Context, id string) {
	h.serveRealtime(c, creativecanvas.EventSource{}, id, "")
}

// serveRealtime answers every refusal as an ordinary JSON error before the SSE
// headers, then hands the connection to the shared stream. The caller's bearer
// token is re-validated on each heartbeat.
func (h *handlers) serveRealtime(c *gin.Context, source realtime.Source, id, cursor string) {
	scope, ok := h.creativeScope(c)
	if !ok {
		return
	}
	if h.realtime == nil {
		abortError(c, 503, "creative_dependency_unavailable", "实时同步暂不可用")
		return
	}
	ctx := c.Request.Context()
	stream, err := h.realtime.Open(ctx, scope, source, id, cursor)
	switch {
	case err == nil:
	case errors.Is(err, realtime.ErrTooManyStreams):
		abortError(c, 429, "realtime_stream_limit", "同时打开的实时连接过多，请关闭其他窗口后重试")
		return
	case errors.Is(err, realtime.ErrSnapshotRequired):
		abortError(c, 409, "creative_snapshot_required", "事件位置已失效，请重新读取最新内容")
		return
	case errors.Is(err, realtime.ErrNotifierUnavailable):
		abortError(c, 503, "creative_dependency_unavailable", "实时同步暂不可用")
		return
	default:
		creativeError(c, err)
		return
	}
	token := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
	stream.Serve(ctx, c.Writer, func() error {
		_, err := h.auth.ParseToken(token)
		return err
	})
}
