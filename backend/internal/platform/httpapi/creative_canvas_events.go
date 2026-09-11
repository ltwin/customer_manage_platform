package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

type canvasEventSource interface {
	SubscribeCanvas(context.Context, store.AccountScope, string) (<-chan struct{}, func(), error)
}

func (h *handlers) WatchCreativeCanvas(c *gin.Context, id string) {
	scope, ok := h.creativeScope(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	if err := creativecanvas.CheckReadAccess(ctx, scope, id); err != nil {
		creativeError(c, err)
		return
	}
	source, ok := h.scopeFactory.(canvasEventSource)
	if !ok {
		abortError(c, 503, "creative_dependency_unavailable", "画布实时同步暂不可用")
		return
	}
	setupCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	events, unsubscribe, err := source.SubscribeCanvas(setupCtx, scope, id)
	cancel()
	if err != nil {
		abortError(c, 503, "creative_dependency_unavailable", "画布实时同步暂不可用")
		return
	}
	defer unsubscribe()
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache, no-transform")
	c.Header("X-Accel-Buffering", "no")
	control := http.NewResponseController(c.Writer)
	write := func(value string) bool {
		// A slow reader must not keep a handler blocked indefinitely.
		_ = control.SetWriteDeadline(time.Now().Add(10 * time.Second))
		defer func() { _ = control.SetWriteDeadline(time.Time{}) }()
		if _, err := fmt.Fprint(c.Writer, value); err != nil {
			return false
		}
		return control.Flush() == nil
	}
	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()
	// Periodic reconnection bounds resource lifetime and reuses normal auth refresh.
	lifetime := time.NewTimer(5 * time.Minute)
	defer lifetime.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-lifetime.C:
			return
		case <-heartbeat.C:
			if _, err := h.auth.ParseToken(strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")); err != nil {
				return
			}
			if !write(": heartbeat\n\n") {
				return
			}
		case _, open := <-events:
			if !open {
				return
			}
			// Recheck ownership/capability before disclosing that this canvas changed.
			if _, err := h.auth.ParseToken(strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")); err != nil {
				return
			}
			if err := creativecanvas.CheckReadAccess(ctx, scope, id); err != nil {
				if errors.Is(err, creativecanvas.ErrNotFound) || errors.Is(err, store.ErrCreativeAccessDenied) || errors.Is(err, store.ErrNoRows) {
					_ = write("event: unavailable\ndata: {}\n\n")
				}
				// Transient database failures close the stream so the client retries.
				return
			}
			if !write("event: invalidate\ndata: {}\n\n") {
				return
			}
		}
	}
}
