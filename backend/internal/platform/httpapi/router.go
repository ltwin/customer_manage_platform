package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// Pinger 是健康检查所需的最小数据库探测面（测试注入失败用）。
type Pinger interface {
	Ping(ctx context.Context) error
}

// RouterDeps 是路由骨架的全部依赖。
type RouterDeps struct {
	Logger *slog.Logger
	DB     Pinger
}

// NewRouter 组装 HTTP 编排骨架。中间件链固定顺序：
// recovery → 请求日志 → 封套渲染 → auth（healthz 与 login 豁免；auth 见 S6 路由组）。
func NewRouter(deps RouterDeps) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(
		recoveryMiddleware(deps.Logger),
		requestLogMiddleware(deps.Logger),
		errorEnvelopeMiddleware(deps.Logger),
	)

	// 运维端点：无鉴权、不进 OpenAPI、不套业务封套（design 2.1）
	r.GET("/healthz", healthzHandler(deps.DB))

	// 未注册 API 路径与方法不匹配一律 404 not_found（不开启 405 区分，§4.1 无此错误码）；
	// 非 API 路径由 go:embed 静态 + SPA fallback 承接（S9 接线前先纯 404）
	r.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			abortError(c, http.StatusNotFound, CodeNotFound, "资源不存在")
			return
		}
		c.Status(http.StatusNotFound)
	})

	return r
}

// healthzHandler 探测数据库可达性：可达 200 ok，不可达 503 degraded。
func healthzHandler(db Pinger) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := db.Ping(c.Request.Context()); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "degraded"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	}
}
