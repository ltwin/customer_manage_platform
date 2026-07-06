package httpapi

import (
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
)

// recoveryMiddleware 把 handler panic 化为 500 internal 封套，进程存活；
// error 级日志含堆栈（design 2.2 可观测点）。
func recoveryMiddleware(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				logger.LogAttrs(c.Request.Context(), slog.LevelError, "panic recovered",
					slog.Any("panic", r),
					slog.String("method", c.Request.Method),
					slog.String("path", c.Request.URL.Path),
					slog.String("stack", string(debug.Stack())),
				)
				abortError(c, http.StatusInternalServerError, CodeInternal, "内部错误")
			}
		}()
		c.Next()
	}
}

// requestLogMiddleware 输出结构化请求日志：method / path / status / 耗时 / account_id。
func requestLogMiddleware(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		attrs := []slog.Attr{
			slog.String("method", c.Request.Method),
			slog.String("path", c.Request.URL.Path),
			slog.Int("status", c.Writer.Status()),
			slog.Duration("duration", time.Since(start)),
		}
		if ac, ok := auth.AccountContextFrom(c.Request.Context()); ok {
			attrs = append(attrs, slog.String("account_id", ac.AccountID))
		}
		logger.LogAttrs(c.Request.Context(), slog.LevelInfo, "request", attrs...)
	}
}

// errorEnvelopeMiddleware 兜底渲染：handler 报了错（c.Error）但没写响应时，
// 统一化为 500 internal 封套，保证非 2xx 响应恒为 ErrorEnvelope。
func errorEnvelopeMiddleware(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		if len(c.Errors) > 0 && !c.Writer.Written() {
			logger.LogAttrs(c.Request.Context(), slog.LevelError, "unhandled handler error",
				slog.String("method", c.Request.Method),
				slog.String("path", c.Request.URL.Path),
				slog.String("error", c.Errors.String()),
			)
			abortError(c, http.StatusInternalServerError, CodeInternal, "内部错误")
		}
	}
}
