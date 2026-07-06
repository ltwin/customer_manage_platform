package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// tokenTTL：JWT 有效期 30 天（design D3）；吊销 = 轮换 AUTH_TOKEN_SECRET。
const tokenTTL = 30 * 24 * time.Hour

// ErrInvalidToken：缺失 / 篡改 / 过期 token 的统一解析失败错误（对外一律 401）。
var ErrInvalidToken = errors.New("token 无效或已过期")

// TokenIssuer 负责 JWT（HS256）签发与解析；secret 只经环境变量注入（硬规则 4）。
type TokenIssuer struct {
	secret []byte
	ttl    time.Duration
	now    func() time.Time
}

// NewTokenIssuer 用注入的 secret 构造签发器。
func NewTokenIssuer(secret string) *TokenIssuer {
	return &TokenIssuer{secret: []byte(secret), ttl: tokenTTL, now: time.Now}
}

// Issue 为账号签发 token（claims: sub=account_id, iat, exp）。
func (ti *TokenIssuer) Issue(accountID string) (string, error) {
	now := ti.now()
	claims := jwt.RegisteredClaims{
		Subject:   accountID,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(ti.ttl)),
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(ti.secret)
	if err != nil {
		return "", fmt.Errorf("sign token: %w", err)
	}
	return signed, nil
}

// Parse 校验签名与有效期，返回 token 归属的 account_id。
func (ti *TokenIssuer) Parse(token string) (string, error) {
	var claims jwt.RegisteredClaims
	_, err := jwt.ParseWithClaims(token, &claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method %v", t.Header["alg"])
		}
		return ti.secret, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithTimeFunc(ti.now))
	if err != nil {
		return "", ErrInvalidToken
	}
	if claims.Subject == "" {
		return "", ErrInvalidToken
	}
	return claims.Subject, nil
}
