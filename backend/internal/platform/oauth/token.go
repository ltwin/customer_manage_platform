package oauth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// TokenRequest is the token endpoint's form, with HTTP Basic credentials
// folded into ClientID and ClientSecret by the handler.
type TokenRequest struct {
	GrantType    string
	ClientID     string
	ClientSecret string
	// BasicAuth says the credentials came in the Authorization header.
	BasicAuth    bool
	Code         string
	RedirectURI  string
	CodeVerifier string
	RefreshToken string
	Resource     string
}

// TokenResponse is the RFC 6749 success body.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
}

type accessClaims struct {
	Account string `json:"acct"`
	Scope   string `json:"scope"`
	jwt.RegisteredClaims
}

// Token serves both grants. Every refusal is an OAuth error; a replayed code or
// rotated refresh token also disconnects the connection, because only a leaked
// credential gets used twice.
func (s *Service) Token(ctx context.Context, req TokenRequest) (TokenResponse, error) {
	// One resource only: a client that leaves it out can only mean this one.
	if req.Resource != "" && req.Resource != s.resource {
		return TokenResponse{}, oauthError("invalid_target", "resource 必须是 "+s.resource)
	}
	client, err := s.authenticateClient(ctx, req)
	if err != nil {
		return TokenResponse{}, err
	}
	now := s.now().UTC()
	refresh, err := s.randomToken()
	if err != nil {
		return TokenResponse{}, err
	}
	refresh = "cxr_" + refresh
	var conn Connection
	switch req.GrantType {
	case "authorization_code":
		conn, err = s.store.RedeemCode(ctx, digest(req.Code), func(code AuthorizationCode, c Connection) (RedeemOutcome, error) {
			switch {
			case code.UsedAt != nil:
				return RedeemOutcome{At: now, Revoke: "refresh_replay"}, oauthError("invalid_grant", "授权码已使用")
			case !now.Before(code.ExpiresAt):
				return RedeemOutcome{}, oauthError("invalid_grant", "授权码已过期")
			case code.ClientID != client.ID:
				return RedeemOutcome{}, oauthError("invalid_client", "client_id 与授权码不符")
			case c.RevokedAt != nil:
				return RedeemOutcome{}, oauthError("invalid_grant", "连接已断开")
			case req.RedirectURI != code.RedirectURI:
				return RedeemOutcome{}, oauthError("invalid_grant", "redirect_uri 与授权请求不符")
			case !pkceMatches(req.CodeVerifier, code.CodeChallenge):
				return RedeemOutcome{}, oauthError("invalid_grant", "code_verifier 不符")
			}
			return RedeemOutcome{At: now, Issue: true, Next: RefreshSeed{Hash: digest(refresh), AbsoluteExpiresAt: now.Add(refreshAbsolute)}}, nil
		})
	case "refresh_token":
		conn, err = s.store.RedeemRefresh(ctx, digest(req.RefreshToken), func(t RefreshToken, c Connection) (RedeemOutcome, error) {
			switch {
			case c.ClientID != client.ID:
				return RedeemOutcome{}, oauthError("invalid_client", "client_id 与 refresh token 不符")
			case t.State == "rotated":
				return RedeemOutcome{At: now, Revoke: "refresh_replay"}, oauthError("invalid_grant", "refresh token 已使用，连接已断开")
			case t.State != "current" || c.RevokedAt != nil:
				return RedeemOutcome{}, oauthError("invalid_grant", "连接已断开")
			case !now.Before(t.AbsoluteExpiresAt) || now.Sub(t.LastUsedAt) >= refreshIdle:
				return RedeemOutcome{}, oauthError("invalid_grant", "refresh token 已过期，请重新授权")
			case !c.AccountActive:
				return RedeemOutcome{}, oauthError("invalid_grant", "账号不可用")
			}
			return RedeemOutcome{At: now, Issue: true, Next: RefreshSeed{Hash: digest(refresh), AbsoluteExpiresAt: t.AbsoluteExpiresAt}}, nil
		})
	default:
		return TokenResponse{}, oauthError("unsupported_grant_type", "只支持 authorization_code 与 refresh_token")
	}
	if errors.Is(err, ErrNotFound) {
		return TokenResponse{}, oauthError("invalid_grant", "授权码或 refresh token 无效")
	}
	var refused *Error
	if errors.As(err, &refused) {
		s.logger.WarnContext(ctx, "oauth token refused", "grant_type", req.GrantType, "client_id", client.ID,
			"connection_id", conn.ID, "reason", refused.Description)
		return TokenResponse{}, err
	}
	if err != nil {
		return TokenResponse{}, err
	}
	access, err := s.issueAccess(conn, now)
	if err != nil {
		return TokenResponse{}, err
	}
	return TokenResponse{AccessToken: access, TokenType: "Bearer", ExpiresIn: int(accessTokenTTL.Seconds()), RefreshToken: refresh, Scope: Scope}, nil
}

// authenticateClient checks the client against how it registered: public
// clients present no secret, confidential ones present theirs the way they said.
func (s *Service) authenticateClient(ctx context.Context, req TokenRequest) (Client, error) {
	if req.ClientID == "" {
		return Client{}, oauthError("invalid_client", "缺少 client_id")
	}
	client, err := s.store.Client(ctx, req.ClientID)
	if errors.Is(err, ErrNotFound) {
		return Client{}, oauthError("invalid_client", "client_id 未注册")
	}
	if err != nil {
		return Client{}, err
	}
	switch client.AuthMethod {
	case "none":
		if req.ClientSecret != "" {
			return Client{}, oauthError("invalid_client", "公共客户端不应带密钥")
		}
	case "client_secret_basic", "client_secret_post":
		if req.ClientSecret == "" || subtle.ConstantTimeCompare(digest(req.ClientSecret), client.SecretHash) != 1 ||
			req.BasicAuth != (client.AuthMethod == "client_secret_basic") {
			return Client{}, oauthError("invalid_client", "客户端认证失败")
		}
	default:
		return Client{}, oauthError("invalid_client", "客户端认证方式不受支持")
	}
	return client, nil
}

func pkceMatches(verifier, challenge string) bool {
	if len(verifier) < 43 || len(verifier) > 128 || !pkceAlphabet(verifier) {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	return subtle.ConstantTimeCompare([]byte(base64.RawURLEncoding.EncodeToString(sum[:])), []byte(challenge)) == 1
}

func (s *Service) issueAccess(conn Connection, now time.Time) (string, error) {
	jti, err := s.randomToken()
	if err != nil {
		return "", err
	}
	claims := accessClaims{Account: conn.AccountID, Scope: Scope, RegisteredClaims: jwt.RegisteredClaims{
		Issuer: s.base, Subject: conn.ID, Audience: jwt.ClaimStrings{s.resource}, ID: jti,
		IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(accessTokenTTL)),
	}}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.accessKey)
	if err != nil {
		return "", fmt.Errorf("sign mcp access token: %w", err)
	}
	return signed, nil
}

// VerifyAccess checks one MCP request's bearer token: signature, issuer,
// audience, expiry, then the connection itself, so a disconnect takes effect on
// the very next request. A valid call also records use, at most once a minute.
func (s *Service) VerifyAccess(ctx context.Context, raw string) (Connection, time.Time, error) {
	var claims accessClaims
	_, err := jwt.ParseWithClaims(raw, &claims, func(*jwt.Token) (any, error) { return s.accessKey, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithIssuer(s.base), jwt.WithAudience(s.resource),
		jwt.WithExpirationRequired(), jwt.WithIssuedAt(), jwt.WithTimeFunc(s.now))
	if err != nil || !strings.HasPrefix(claims.Subject, "cxcon_") || claims.Scope != Scope || len(claims.Audience) != 1 {
		return Connection{}, time.Time{}, ErrInvalidToken
	}
	conn, err := s.store.Connection(ctx, claims.Subject)
	if errors.Is(err, ErrNotFound) {
		return Connection{}, time.Time{}, ErrInvalidToken
	}
	if err != nil {
		return Connection{}, time.Time{}, err
	}
	if conn.RevokedAt != nil || !conn.AccountActive || conn.AccountID != claims.Account {
		return Connection{}, time.Time{}, ErrInvalidToken
	}
	now := s.now().UTC()
	if conn.LastUsedAt == nil || now.Sub(*conn.LastUsedAt) >= touchInterval {
		if err := s.store.TouchConnection(ctx, conn.ID, now); err != nil {
			// Activity is a hint for the workspace; it never fails a call.
			s.logger.WarnContext(ctx, "mcp connection touch failed", "connection_id", conn.ID, "error", err.Error())
		} else {
			conn.LastUsedAt = &now
		}
	}
	return conn, claims.ExpiresAt.Time, nil
}
