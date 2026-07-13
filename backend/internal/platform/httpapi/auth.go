package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	customerdomain "github.com/samson/customer-manage-platform/backend/internal/customer"
	orderdomain "github.com/samson/customer-manage-platform/backend/internal/order"
	pkgcatalog "github.com/samson/customer-manage-platform/backend/internal/package"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/idempotency"
	scheduledomain "github.com/samson/customer-manage-platform/backend/internal/schedule"
)

const defaultAccountTimezone = "Asia/Shanghai"

type AccountTimezoneProvider interface {
	TimezoneForAccount(context.Context, string) (string, error)
}

type defaultTimezoneProvider struct{}

func (defaultTimezoneProvider) TimezoneForAccount(context.Context, string) (string, error) {
	return defaultAccountTimezone, nil
}

// authMiddleware 校验 Bearer token 并注入 AccountContext；
// 缺失 / 无效 / 过期一律 401 封套（鉴权不变量，design 2.2）。
func authMiddleware(svc *auth.Service) gin.HandlerFunc {
	const bearerPrefix = "Bearer "
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if !strings.HasPrefix(header, bearerPrefix) {
			abortError(c, http.StatusUnauthorized, CodeUnauthorized, "未认证")
			return
		}
		accountID, err := svc.ParseToken(strings.TrimPrefix(header, bearerPrefix))
		if err != nil {
			abortError(c, http.StatusUnauthorized, CodeUnauthorized, "token 无效或已过期")
			return
		}
		ctx := auth.WithAccountContext(c.Request.Context(), auth.AccountContext{AccountID: accountID})
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

// handlers 实现 codegen 的 ServerInterface：薄适配层，领域逻辑在 auth.Service（ADR-003）。
type handlers struct {
	logger          *slog.Logger
	auth            *auth.Service
	scopeFactory    ScopeFactory
	customer        *customerdomain.Service
	orders          *orderdomain.Service
	packages        *pkgcatalog.Service
	idempotency     *idempotency.Executor
	timezone        AccountTimezoneProvider
	schedule        *scheduledomain.Service
	avatar          *customerdomain.AvatarApplication
	avatarProcessor AvatarProcessor
}

var _ ServerInterface = (*handlers)(nil)

// Login 处理 POST /auth/login（契约见 api/openapi.yaml）。
func (h *handlers) Login(c *gin.Context) {
	var body LoginJSONRequestBody
	if err := c.ShouldBindJSON(&body); err != nil || body.Password == "" {
		abortError(c, http.StatusBadRequest, CodeValidationFailed, "password 必填")
		return
	}
	token, err := h.auth.Login(c.Request.Context(), body.Password)
	if errors.Is(err, auth.ErrInvalidPassword) {
		abortError(c, http.StatusUnauthorized, CodeUnauthorized, "密码错误")
		return
	}
	if err != nil {
		_ = c.Error(err) // 由封套渲染中间件统一化为 500 internal
		return
	}
	c.JSON(http.StatusOK, gin.H{"token": token})
}

// GetMe 处理 GET /me：返回账号信息，永不含 password_hash。
func (h *handlers) GetMe(c *gin.Context) {
	ac, ok := auth.AccountContextFrom(c.Request.Context())
	if !ok {
		abortError(c, http.StatusUnauthorized, CodeUnauthorized, "未认证")
		return
	}
	acct, err := h.auth.AccountByID(c.Request.Context(), ac.AccountID)
	if err != nil {
		_ = c.Error(err)
		return
	}
	timezone, err := h.timezone.TimezoneForAccount(c.Request.Context(), ac.AccountID)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, Account{
		Id:        &acct.ID,
		CreatedAt: &acct.CreatedAt,
		Timezone:  timezone,
	})
}
