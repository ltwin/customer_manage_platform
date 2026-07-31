package auth

import (
	"crypto/rand"
	"fmt"
	"io"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const accessTokenTTL = 10 * time.Minute

type accessClaims struct {
	Version int    `json:"ver"`
	Session string `json:"sid"`
	jwt.RegisteredClaims
}

type TokenIssuer struct {
	key        []byte
	replayKey  []byte
	limiterKey []byte
	issuer     string
	audience   string
	ttl        time.Duration
	now        func() time.Time
	random     io.Reader
}

func NewTokenIssuer(secret string) *TokenIssuer {
	key := deriveKey(secret, accessSigningLabel)
	return &TokenIssuer{
		key: key, replayKey: deriveKey(secret, replayAEADLabel), limiterKey: deriveKey(secret, limiterHMACLabel),
		issuer: "photographer-crm", audience: "photographer-crm-web",
		ttl: accessTokenTTL, now: time.Now, random: rand.Reader,
	}
}

func (ti *TokenIssuer) LimiterDigester() *LimiterDigester {
	return newLimiterDigester(ti.limiterKey)
}

func (ti *TokenIssuer) ReplayCipher(random io.Reader) (*ReplayCipher, error) {
	return newReplayCipher(ti.replayKey, random)
}

func (ti *TokenIssuer) WithClock(now func() time.Time) *TokenIssuer {
	if now != nil {
		ti.now = now
	}
	return ti
}

func (ti *TokenIssuer) WithRandom(random io.Reader) *TokenIssuer {
	if random != nil {
		ti.random = random
	}
	return ti
}

func (ti *TokenIssuer) WithIdentity(issuer, audience string) *TokenIssuer {
	if issuer != "" {
		ti.issuer = issuer
	}
	if audience != "" {
		ti.audience = audience
	}
	return ti
}

// Issue 保留给现有内部测试/工具；生产认证流程使用 IssueForSession。
func (ti *TokenIssuer) Issue(accountID string) (string, error) {
	token, _, err := ti.IssueForSession(accountID, accountID)
	return token, err
}

func (ti *TokenIssuer) IssueForSession(accountID, sessionID string) (string, time.Time, error) {
	now := ti.now().UTC()
	jti, err := randomID(ti.random)
	if err != nil {
		return "", time.Time{}, err
	}
	expiresAt := now.Add(ti.ttl)
	claims := accessClaims{
		Version: 2,
		Session: sessionID,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer: ti.issuer, Subject: accountID,
			Audience: jwt.ClaimStrings{ti.audience}, ID: jti,
			IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(ti.key)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign access token: %w", err)
	}
	return signed, expiresAt, nil
}

func (ti *TokenIssuer) Parse(token string) (string, error) {
	var claims accessClaims
	_, err := jwt.ParseWithClaims(token, &claims, func(t *jwt.Token) (any, error) {
		if t.Method != jwt.SigningMethodHS256 {
			return nil, ErrInvalidToken
		}
		return ti.key, nil
	},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(ti.issuer), jwt.WithAudience(ti.audience), jwt.WithTimeFunc(ti.now),
		jwt.WithExpirationRequired(), jwt.WithIssuedAt(),
	)
	if err != nil || claims.Version != 2 || claims.Subject == "" || claims.Session == "" || claims.ID == "" ||
		claims.IssuedAt == nil || claims.ExpiresAt == nil || claims.Issuer != ti.issuer ||
		len(claims.Audience) != 1 || claims.Audience[0] != ti.audience {
		return "", ErrInvalidToken
	}
	return claims.Subject, nil
}
