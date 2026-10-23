package httpapi

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/oauth"
)

// The authorization server's protocol endpoints (epic creative-mcp-server).
// They answer OAuth clients, not the web app: errors use the OAuth format
// {error, error_description}, never the ErrorEnvelope, and they sit outside
// /api so the SPA fallback never claims them.

const (
	consentPath       = "/oauth/consent"
	registrationLimit = 8 << 10
	tokenRequestLimit = 16 << 10
	protectedResource = "/.well-known/oauth-protected-resource"
	authorizationMeta = "/.well-known/oauth-authorization-server"
)

func registerOAuth(r *gin.Engine, h *handlers) {
	cors := func(c *gin.Context) {
		// Metadata, registration and tokens carry no cookies; any origin may ask.
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type, Mcp-Protocol-Version")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
		}
	}
	for _, path := range []string{protectedResource + "/mcp", protectedResource} {
		r.OPTIONS(path, cors)
		r.GET(path, cors, func(c *gin.Context) { c.JSON(http.StatusOK, h.oauth.ProtectedResourceMetadata()) })
	}
	r.OPTIONS(authorizationMeta, cors)
	r.GET(authorizationMeta, cors, func(c *gin.Context) { c.JSON(http.StatusOK, h.oauth.AuthorizationServerMetadata()) })
	r.OPTIONS("/oauth/register", cors)
	r.POST("/oauth/register", cors, h.registerOAuthClient)
	r.GET("/oauth/authorize", h.authorizeOAuth)
	r.OPTIONS("/oauth/token", cors)
	r.POST("/oauth/token", cors, h.issueOAuthToken)
}

func oauthFailure(c *gin.Context, status int, err *oauth.Error) {
	if err.RetryAfter > 0 {
		c.Header("Retry-After", strconv.Itoa(int(err.RetryAfter.Seconds())+1))
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(status, gin.H{"error": err.Code, "error_description": err.Description})
}

func (h *handlers) registerOAuthClient(c *gin.Context) {
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, registrationLimit+1))
	if err != nil || len(body) > registrationLimit {
		oauthFailure(c, http.StatusBadRequest, &oauth.Error{Code: "invalid_client_metadata", Description: "注册信息超过 8 KB"})
		return
	}
	ip := clientMeta(c, h.trustedProxyCIDRs).SourceIP.String()
	resp, err := h.oauth.Register(c.Request.Context(), ip, body)
	var refused *oauth.Error
	switch {
	case errors.As(err, &refused) && refused.Code == "too_many_requests":
		oauthFailure(c, http.StatusTooManyRequests, refused)
	case errors.As(err, &refused):
		oauthFailure(c, http.StatusBadRequest, refused)
	case err != nil:
		h.logger.ErrorContext(c.Request.Context(), "oauth registration failed", "error", err.Error())
		oauthFailure(c, http.StatusInternalServerError, &oauth.Error{Code: "server_error", Description: "注册失败，请稍后再试"})
	default:
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusCreated, resp)
	}
}

// authorizeOAuth checks the request and hands the browser to the consent page.
// A client or callback it cannot verify is reported on this site only.
func (h *handlers) authorizeOAuth(c *gin.Context) {
	got, err := h.oauth.Authorize(c.Request.Context(), c.Request.URL.Query())
	if err != nil {
		h.logger.ErrorContext(c.Request.Context(), "oauth authorize failed", "error", err.Error())
		c.Redirect(http.StatusFound, consentPath+"?error=server_error")
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
	switch {
	case got.Request != "":
		c.Redirect(http.StatusFound, consentPath+"?"+url.Values{"request": {got.Request}}.Encode())
	case got.Redirect != "":
		c.Redirect(http.StatusFound, got.Redirect)
	default:
		c.Redirect(http.StatusFound, consentPath+"?"+url.Values{"error": {got.LocalError}}.Encode())
	}
}

func (h *handlers) issueOAuthToken(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, tokenRequestLimit)
	if err := c.Request.ParseForm(); err != nil {
		oauthFailure(c, http.StatusBadRequest, &oauth.Error{Code: "invalid_request", Description: "请求体须为 application/x-www-form-urlencoded"})
		return
	}
	form := c.Request.PostForm
	req := oauth.TokenRequest{
		GrantType: form.Get("grant_type"), ClientID: form.Get("client_id"), ClientSecret: form.Get("client_secret"),
		Code: form.Get("code"), RedirectURI: form.Get("redirect_uri"), CodeVerifier: form.Get("code_verifier"),
		RefreshToken: form.Get("refresh_token"), Resource: form.Get("resource"),
	}
	if user, pass, ok := c.Request.BasicAuth(); ok {
		// RFC 6749 §2.3.1: Basic credentials are form-encoded first.
		id, idErr := url.QueryUnescape(user)
		secret, secretErr := url.QueryUnescape(pass)
		if idErr != nil || secretErr != nil || (req.ClientID != "" && req.ClientID != id) || req.ClientSecret != "" {
			oauthFailure(c, http.StatusUnauthorized, &oauth.Error{Code: "invalid_client", Description: "客户端认证失败"})
			return
		}
		req.ClientID, req.ClientSecret, req.BasicAuth = id, secret, true
	}
	resp, err := h.oauth.Token(c.Request.Context(), req)
	var refused *oauth.Error
	switch {
	case errors.As(err, &refused) && refused.Code == "invalid_client":
		if req.BasicAuth {
			c.Header("WWW-Authenticate", `Basic realm="oauth"`)
		}
		oauthFailure(c, http.StatusUnauthorized, refused)
	case errors.As(err, &refused):
		oauthFailure(c, http.StatusBadRequest, refused)
	case err != nil:
		h.logger.ErrorContext(c.Request.Context(), "oauth token failed", "error", err.Error())
		oauthFailure(c, http.StatusInternalServerError, &oauth.Error{Code: "server_error", Description: "暂时无法签发令牌，请稍后再试"})
	default:
		c.Header("Cache-Control", "no-store")
		c.Header("Pragma", "no-cache")
		c.JSON(http.StatusOK, resp)
	}
}

// DecideOAuthAuthorization is the consent page's answer. It runs behind the
// web bearer: the connection belongs to whoever is signed in.
func (h *handlers) DecideOAuthAuthorization(c *gin.Context) {
	if h.oauth == nil {
		abortError(c, http.StatusNotFound, CodeNotFound, "外部助手暂未开放")
		return
	}
	ac, ok := auth.AccountContextFrom(c.Request.Context())
	if !ok {
		abortError(c, http.StatusUnauthorized, CodeUnauthorized, "请先登录")
		return
	}
	var body OAuthAuthorizeDecisionRequest
	if err := decodeStrictJSONBody(c, &body); err != nil || !body.Decision.Valid() || body.Request == "" {
		abortError(c, http.StatusBadRequest, CodeValidationFailed, "请求参数不合法")
		return
	}
	to, err := h.oauth.Decide(c.Request.Context(), ac.AccountID, body.Request, body.Decision == Allow)
	switch {
	case errors.Is(err, oauth.ErrRequestInvalid):
		abortError(c, http.StatusBadRequest, CodeValidationFailed, "授权请求已过期或无效，请回到外部助手重新发起连接")
	case errors.Is(err, oauth.ErrAccountInactive):
		abortError(c, http.StatusForbidden, CodeForbidden, "这个账号现在不能使用创意空间")
	case err != nil:
		_ = c.Error(err)
	default:
		c.JSON(http.StatusOK, OAuthAuthorizeDecisionResult{RedirectTo: to})
	}
}

// protocolPath names the paths that belong to the authorization server and
// the MCP endpoint. They are never the SPA: switched off, they are 404.
func protocolPath(path string) bool {
	if path == "/mcp" || strings.HasPrefix(path, "/mcp/") || strings.HasPrefix(path, "/.well-known/oauth-") {
		return true
	}
	return strings.HasPrefix(path, "/oauth/") && path != consentPath
}
