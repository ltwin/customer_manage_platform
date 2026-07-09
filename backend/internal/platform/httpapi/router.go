package httpapi

import (
	"context"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	customerdomain "github.com/samson/customer-manage-platform/backend/internal/customer"
	orderdomain "github.com/samson/customer-manage-platform/backend/internal/order"
	pkgcatalog "github.com/samson/customer-manage-platform/backend/internal/package"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/platform/webui"
)

// Pinger 是健康检查所需的最小数据库探测面（测试注入失败用）。
type Pinger interface {
	Ping(ctx context.Context) error
}

// ScopeFactory 是业务路由获取账号隔离数据库句柄的最小依赖。
type ScopeFactory interface {
	ScopeFor(auth.AccountContext) store.AccountScope
}

// RouterDeps 是路由骨架的全部依赖。
type RouterDeps struct {
	Logger       *slog.Logger
	DB           Pinger
	ScopeFactory ScopeFactory
	Auth         *auth.Service
	Customer     *customerdomain.Service
	Orders       *orderdomain.Service
	Packages     *pkgcatalog.Service
}

// NewRouter 组装 HTTP 编排骨架。中间件链固定顺序：
// recovery → 请求日志 → 封套渲染 → auth（healthz 与 login 豁免）。
func NewRouter(deps RouterDeps) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.RedirectTrailingSlash = false
	r.Use(
		recoveryMiddleware(deps.Logger),
		requestLogMiddleware(deps.Logger),
		errorEnvelopeMiddleware(deps.Logger),
	)

	// 运维端点：无鉴权、不进 OpenAPI、不套业务封套（design 2.1）
	r.GET("/healthz", healthzHandler(deps.DB))

	// API 路由：handlers 实现 codegen ServerInterface；login 豁免 auth，其余一律先过 auth
	h := &handlers{
		logger:       deps.Logger,
		auth:         deps.Auth,
		scopeFactory: deps.ScopeFactory,
		customer:     deps.Customer,
		orders:       deps.Orders,
		packages:     deps.Packages,
	}
	api := r.Group("/api/v1")
	api.POST("/auth/login", h.Login)
	protected := api.Group("", authMiddleware(deps.Auth))
	protected.GET("/me", h.GetMe)
	protected.GET("/customers", h.listCustomersRoute)
	protected.POST("/customers", h.CreateCustomer)
	protected.GET("/customers/:id", h.getCustomerRoute)
	// customer-profile-complete：档案五操作
	protected.PATCH("/customers/:id", func(c *gin.Context) { h.UpdateCustomer(c, c.Param("id")) })
	protected.POST("/customers/:id/identities", func(c *gin.Context) { h.AddCustomerIdentity(c, c.Param("id")) })
	protected.DELETE("/customers/:id/identities/:identity_id", func(c *gin.Context) {
		h.DeleteCustomerIdentity(c, c.Param("id"), c.Param("identity_id"))
	})
	protected.POST("/customers/:id/notes", func(c *gin.Context) { h.AddCustomerNote(c, c.Param("id")) })
	protected.POST("/customers/:id/merge", func(c *gin.Context) { h.MergeCustomer(c, c.Param("id")) })
	protected.GET("/orders", h.listOrdersRoute)
	protected.POST("/orders", h.CreateOrder)
	protected.PATCH("/orders/:id", func(c *gin.Context) { h.UpdateOrder(c, c.Param("id")) })
	protected.DELETE("/orders/:id", func(c *gin.Context) { h.DeleteOrder(c, c.Param("id")) })
	protected.GET("/packages", h.listPackagesRoute)
	protected.POST("/packages", h.CreatePackage)
	protected.PATCH("/packages/:id", func(c *gin.Context) { h.UpdatePackage(c, c.Param("id")) })
	protected.DELETE("/packages/:id", func(c *gin.Context) { h.DeletePackage(c, c.Param("id")) })

	// 未注册 API 路径与方法不匹配一律 404 not_found（不开启 405 区分，§4.1 无此错误码）；
	// 非 API 路径恒由 go:embed 静态 + SPA fallback 承接（D7，不适用封套）
	serveStatic := staticHandler(webui.Dist())
	r.NoRoute(func(c *gin.Context) {
		if isAPIPath(c.Request.URL.Path) {
			abortError(c, http.StatusNotFound, CodeNotFound, "资源不存在")
			return
		}
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			c.Status(http.StatusNotFound)
			return
		}
		serveStatic(c)
	})

	return r
}

func isAPIPath(path string) bool {
	cleaned := path
	for strings.HasPrefix(cleaned, "//") {
		cleaned = strings.TrimPrefix(cleaned, "/")
	}
	return cleaned == "/api" || strings.HasPrefix(cleaned, "/api/")
}

// staticHandler 托管 go:embed 静态产物：命中文件直接服务，其余路径 SPA fallback 到 index.html；
// 产物未同步（仅 .gitkeep 的空 dist）时非 API 路径纯 404。
func staticHandler(dist fs.FS) gin.HandlerFunc {
	httpFS := http.FS(dist)
	return func(c *gin.Context) {
		path := strings.TrimPrefix(c.Request.URL.Path, "/")
		if path != "" && path != "index.html" {
			if info, err := fs.Stat(dist, path); err == nil && !info.IsDir() {
				c.FileFromFS(path, httpFS)
				return
			}
		}
		index, err := fs.ReadFile(dist, "index.html")
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", index)
	}
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
