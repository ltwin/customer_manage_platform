package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/samson/customer-manage-platform/backend/internal/accountprofile"
	"github.com/samson/customer-manage-platform/backend/internal/creativeagent"
	"github.com/samson/customer-manage-platform/backend/internal/creativemedia"
	customerdomain "github.com/samson/customer-manage-platform/backend/internal/customer"
	dashboarddomain "github.com/samson/customer-manage-platform/backend/internal/dashboard"
	orderdomain "github.com/samson/customer-manage-platform/backend/internal/order"
	pkgcatalog "github.com/samson/customer-manage-platform/backend/internal/package"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/idempotency"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/reminder"
	"github.com/samson/customer-manage-platform/backend/internal/reminder/digest"
	scheduledomain "github.com/samson/customer-manage-platform/backend/internal/schedule"
	"github.com/samson/customer-manage-platform/backend/internal/settings"
)

const defaultAccountTimezone = "Asia/Shanghai"

const refreshCookieName = "__Host-crm_refresh"

const maxAuthJSONBodyBytes = 4 << 10

type AccountTimezoneProvider interface {
	TimezoneForAccount(context.Context, string) (string, error)
}

type TelegramBindingIssuer interface {
	IssueBindToken(context.Context, store.AccountScope) (digest.BindLink, error)
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
	logger              *slog.Logger
	auth                *auth.Service
	scopeFactory        ScopeFactory
	customer            *customerdomain.Service
	orders              *orderdomain.Service
	packages            *pkgcatalog.Service
	idempotency         *idempotency.Executor
	timezone            AccountTimezoneProvider
	schedule            *scheduledomain.Service
	avatar              *customerdomain.AvatarApplication
	avatarProcessor     AvatarProcessor
	accountProfile      *accountprofile.Service
	settings            *settings.Service
	reminders           *reminder.Service
	dashboard           *dashboarddomain.Service
	dataExport          DataExportService
	dataExportMap       dataExportProjector
	dataExportEncode    dataExportEncoder
	telegramBinding     TelegramBindingIssuer
	publicBaseURL       string
	registrationEnabled bool
	trustedProxyCIDRs   []netip.Prefix
	now                 func() time.Time
	planningMedia       *planningMediaHandlers
	anonymousShare      anonymousShareRouteDeps
	creativeMedia       *creativemedia.Service
	creativeAgent       *creativeagent.Service
	creativeTools       *creativeops.Runtime
	creativeToolsError  error
}

var _ ServerInterface = (*handlers)(nil)

// Generated OpenAPI keeps planning-media in the platform contract. Runtime
// routing uses the dedicated multipart/streaming adapter below; these forwards
// preserve one generated interface without duplicating request parsing.
func (h *handlers) ListShootPlanAssets(c *gin.Context, _ Id, _ ListShootPlanAssetsParams) {
	h.planningMedia.list(c)
}

func (h *handlers) UploadShootPlanAsset(c *gin.Context, _ Id, _ UploadShootPlanAssetParams) {
	h.planningMedia.upload(c)
}

func (h *handlers) CreateShootPlanAssetBinding(c *gin.Context, _ Id, _ string, _ CreateShootPlanAssetBindingParams) {
	h.planningMedia.bind(c)
}

func (h *handlers) ReleaseShootPlanAssetBinding(c *gin.Context, _ Id, _ string, _ string, _ ReleaseShootPlanAssetBindingParams) {
	h.planningMedia.release(c)
}

func (h *handlers) GetShootPlanAssetContent(c *gin.Context, _ Id, _ string, _ GetShootPlanAssetContentParams) {
	h.planningMedia.content(c)
}

func (h *handlers) GetAuthCapabilities(c *gin.Context) {
	setNoStore(c)
	c.JSON(http.StatusOK, AuthCapabilities{PublicRegistrationEnabled: h.registrationEnabled})
}

func (h *handlers) Register(c *gin.Context) {
	setNoStore(c)
	if !h.registrationEnabled {
		abortError(c, http.StatusServiceUnavailable, CodeRegistrationDisabled, "注册暂未开放")
		return
	}
	var body RegisterJSONRequestBody
	if err := bindStrictJSON(c, &body); err != nil {
		abortError(c, http.StatusBadRequest, CodeValidationFailed, "请求参数不合法")
		return
	}
	if _, err := h.auth.Register(c.Request.Context(), body.Email, body.Password, clientMeta(c, h.trustedProxyCIDRs)); err != nil {
		h.writeAuthError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, VerificationDispatch{Status: VerificationRequired})
}

func (h *handlers) ResendVerification(c *gin.Context) {
	setNoStore(c)
	var body ResendVerificationJSONRequestBody
	if err := bindStrictJSON(c, &body); err != nil {
		abortError(c, http.StatusBadRequest, CodeValidationFailed, "请求参数不合法")
		return
	}
	if _, err := h.auth.ResendVerification(c.Request.Context(), body.Email, clientMeta(c, h.trustedProxyCIDRs)); err != nil {
		h.writeAuthError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, VerificationDispatch{Status: VerificationRequired})
}

func (h *handlers) VerifyEmail(c *gin.Context) {
	setNoStore(c)
	if !h.requireTrustedOrigin(c) {
		return
	}
	var body VerifyEmailJSONRequestBody
	if err := bindStrictJSON(c, &body); err != nil {
		abortError(c, http.StatusBadRequest, CodeValidationFailed, "请求参数不合法")
		return
	}
	session, err := h.auth.VerifyEmail(c.Request.Context(), body.Token, clientMeta(c, h.trustedProxyCIDRs))
	h.logAuthSessionEvent(c.Request.Context(), "auth.email_verified", session, err)
	if err != nil {
		h.writeAuthError(c, err)
		return
	}
	h.writeSession(c, session)
}

// Login 处理 POST /auth/login（契约见 api/openapi.yaml）。
func (h *handlers) Login(c *gin.Context) {
	setNoStore(c)
	if !h.requireTrustedOrigin(c) {
		return
	}
	var body LoginJSONRequestBody
	if err := bindStrictJSON(c, &body); err != nil {
		abortError(c, http.StatusBadRequest, CodeValidationFailed, "请求参数不合法")
		return
	}
	session, err := h.auth.Login(c.Request.Context(), body.Email, body.Password, clientMeta(c, h.trustedProxyCIDRs))
	h.logAuthSessionEvent(c.Request.Context(), "auth.login", session, err)
	if err != nil {
		h.writeAuthError(c, err)
		return
	}
	h.writeSession(c, session)
}

func (h *handlers) Refresh(c *gin.Context) {
	setNoStore(c)
	if !h.requireTrustedOrigin(c) {
		return
	}
	wire, _ := c.Cookie(refreshCookieName)
	session, err := h.auth.Refresh(c.Request.Context(), wire, clientMeta(c, h.trustedProxyCIDRs))
	if err != nil {
		h.logRefreshReuseEvent(c.Request.Context(), err)
		if auth.IsAuthError(err, auth.AuthErrorUnauthorized) {
			h.clearRefreshCookie(c)
		}
		h.writeAuthError(c, err)
		return
	}
	h.writeSession(c, session)
}

func (h *handlers) Logout(c *gin.Context) {
	setNoStore(c)
	if !h.requireTrustedOrigin(c) {
		return
	}
	wire, _ := c.Cookie(refreshCookieName)
	if err := h.auth.Logout(c.Request.Context(), wire); err != nil {
		h.writeAuthError(c, err)
		return
	}
	h.clearRefreshCookie(c)
	c.Status(http.StatusNoContent)
}

func (h *handlers) ForgotPassword(c *gin.Context) {
	setNoStore(c)
	var body ForgotPasswordJSONRequestBody
	if err := bindStrictJSON(c, &body); err != nil {
		abortError(c, http.StatusBadRequest, CodeValidationFailed, "请求参数不合法")
		return
	}
	if _, err := h.auth.BeginPasswordReset(
		c.Request.Context(), body.Email, clientMeta(c, h.trustedProxyCIDRs),
	); err != nil {
		h.writeAuthError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"status": Accepted})
}

func (h *handlers) ResetPassword(c *gin.Context) {
	setNoStore(c)
	if !h.requireTrustedOrigin(c) {
		return
	}
	var body ResetPasswordJSONRequestBody
	if err := bindStrictJSON(c, &body); err != nil {
		abortError(c, http.StatusBadRequest, CodeValidationFailed, "请求参数不合法")
		return
	}
	if err := h.auth.ResetPassword(
		c.Request.Context(), body.Token, body.NewPassword, clientMeta(c, h.trustedProxyCIDRs),
	); err != nil {
		h.writeAuthError(c, err)
		return
	}
	h.logPasswordChangedEvent(c.Request.Context(), auth.AuthActionResetToken, "")
	h.clearRefreshCookie(c)
	c.Status(http.StatusNoContent)
}

func (h *handlers) ChangePassword(c *gin.Context) {
	setNoStore(c)
	if !h.requireTrustedOrigin(c) {
		return
	}
	account, ok := auth.AccountContextFrom(c.Request.Context())
	if !ok {
		abortError(c, http.StatusUnauthorized, CodeUnauthorized, "未认证")
		return
	}
	var body ChangePasswordJSONRequestBody
	if err := bindStrictJSON(c, &body); err != nil {
		abortError(c, http.StatusBadRequest, CodeValidationFailed, "请求参数不合法")
		return
	}
	if err := h.auth.ChangePassword(
		c.Request.Context(), account, body.CurrentPassword, body.NewPassword,
		clientMeta(c, h.trustedProxyCIDRs),
	); err != nil {
		h.writeAuthError(c, err)
		return
	}
	h.logPasswordChangedEvent(c.Request.Context(), auth.AuthActionChangePassword, account.AccountID)
	h.clearRefreshCookie(c)
	c.Status(http.StatusNoContent)
}

// GetMe 处理 GET /me：返回账号信息，永不含 password_hash。
func (h *handlers) GetMe(c *gin.Context) {
	ac, ok := auth.AccountContextFrom(c.Request.Context())
	if !ok {
		abortError(c, http.StatusUnauthorized, CodeUnauthorized, "未认证")
		return
	}
	acct, err := h.auth.CurrentAccount(c.Request.Context(), ac.AccountID)
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
		Email:     &acct.Email,
		CreatedAt: &acct.CreatedAt,
		Timezone:  timezone,
	})
}

func (h *handlers) requireTrustedOrigin(c *gin.Context) bool {
	if c.GetHeader("Origin") != h.publicBaseURL {
		abortError(c, http.StatusForbidden, CodeForbidden, "请求来源不受信任")
		return false
	}
	return true
}

func (h *handlers) writeSession(c *gin.Context, session auth.Session) {
	h.setRefreshCookie(c, session)
	c.JSON(http.StatusOK, AccessTokenResponse{
		AccessToken: session.AccessToken,
		ExpiresIn:   AccessTokenResponseExpiresIn(600),
		TokenType:   AccessTokenResponseTokenType("Bearer"),
	})
}

func (h *handlers) setRefreshCookie(c *gin.Context, session auth.Session) {
	expiresAt := session.RefreshExpiresAt
	if session.RefreshAbsoluteAt.Before(expiresAt) {
		expiresAt = session.RefreshAbsoluteAt
	}
	now := h.now().UTC()
	maxAge := int(expiresAt.Sub(now) / time.Second)
	if maxAge < 0 {
		maxAge = 0
	}
	http.SetCookie(c.Writer, &http.Cookie{
		Name: refreshCookieName, Value: session.RefreshToken,
		Path: "/", Expires: expiresAt.UTC(), MaxAge: maxAge,
		HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode,
	})
}

func (h *handlers) clearRefreshCookie(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name: refreshCookieName, Value: "", Path: "/",
		Expires: time.Unix(1, 0).UTC(), MaxAge: -1,
		HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode,
	})
}

func (h *handlers) writeAuthError(c *gin.Context, err error) {
	switch {
	case auth.IsAuthError(err, auth.AuthErrorValidation):
		abortError(c, http.StatusBadRequest, CodeValidationFailed, "请求参数不合法")
	case auth.IsAuthError(err, auth.AuthErrorUnauthorized):
		abortError(c, http.StatusUnauthorized, CodeUnauthorized, "认证失败")
	case auth.IsAuthError(err, auth.AuthErrorEmailVerificationRequired):
		abortError(c, http.StatusForbidden, CodeEmailVerificationRequired, "邮箱尚未验证")
	case auth.IsAuthError(err, auth.AuthErrorInvalidOrExpiredToken):
		abortError(c, http.StatusBadRequest, CodeInvalidOrExpiredToken, "token 无效或已过期")
	case auth.IsAuthError(err, auth.AuthErrorRateLimited):
		h.logRateLimitedEvent(c.Request.Context(), err)
		retryAfter, _ := auth.AuthRetryAfter(err)
		seconds := int64((retryAfter + time.Second - 1) / time.Second)
		if seconds < 1 {
			seconds = 1
		}
		c.Header("Retry-After", strconv.FormatInt(seconds, 10))
		abortError(c, http.StatusTooManyRequests, CodeRateLimited, "请求过于频繁，请稍后重试")
	default:
		_ = c.Error(err)
	}
}

func bindStrictJSON(c *gin.Context, target any) error {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxAuthJSONBodyBytes)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return err
	}
	return nil
}

func setNoStore(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
}
