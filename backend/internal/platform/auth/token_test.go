package auth

import (
	"bytes"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestAccessTokenVersionedClaimsAndTTL(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 31, 1, 2, 3, 0, time.UTC)
	issuer := NewTokenIssuer("synthetic-root-secret").WithClock(func() time.Time { return now })
	issuer.WithRandom(bytes.NewReader(bytes.Repeat([]byte{0x44}, 16)))
	wire, expiresAt, err := issuer.IssueForSession("account-id", "session-id")
	if err != nil {
		t.Fatalf("issue access token: %v", err)
	}
	if !expiresAt.Equal(now.Add(10 * time.Minute)) {
		t.Fatalf("access expiry: got %s want %s", expiresAt, now.Add(10*time.Minute))
	}
	accountID, err := issuer.Parse(wire)
	if err != nil || accountID != "account-id" {
		t.Fatalf("parse issued access token: account=%q err=%v", accountID, err)
	}

	var claims accessClaims
	if _, err := jwt.ParseWithClaims(
		wire,
		&claims,
		func(*jwt.Token) (any, error) { return issuer.key, nil },
		jwt.WithTimeFunc(func() time.Time { return now }),
	); err != nil {
		t.Fatalf("inspect issued claims: %v", err)
	}
	if claims.Version != 2 || claims.Issuer == "" || len(claims.Audience) != 1 ||
		claims.Subject == "" || claims.Session == "" || claims.ID == "" ||
		claims.IssuedAt == nil || claims.ExpiresAt == nil {
		t.Fatalf("issued access token is missing required claims: %#v", claims)
	}

	rotated := NewTokenIssuer("rotated-synthetic-root").WithClock(func() time.Time { return now })
	if _, err := rotated.Parse(wire); err == nil {
		t.Fatal("root rotation should reject old access token")
	}
}

func TestAccessTokenRejectsLegacyAndMissingRequiredClaims(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 31, 1, 2, 3, 0, time.UTC)
	issuer := NewTokenIssuer("synthetic-root-secret").WithClock(func() time.Time { return now })
	tests := map[string]accessClaims{
		"legacy version": validAccessClaims(now, 1),
		"future issued at": func() accessClaims {
			claims := validAccessClaims(now, 2)
			claims.IssuedAt = jwt.NewNumericDate(now.Add(time.Minute))
			return claims
		}(),
		"extra audience": func() accessClaims {
			claims := validAccessClaims(now, 2)
			claims.Audience = append(claims.Audience, "unexpected-audience")
			return claims
		}(),
		"missing issued at": func() accessClaims {
			claims := validAccessClaims(now, 2)
			claims.IssuedAt = nil
			return claims
		}(),
		"missing expiry": func() accessClaims {
			claims := validAccessClaims(now, 2)
			claims.ExpiresAt = nil
			return claims
		}(),
	}
	for name, claims := range tests {
		name, claims := name, claims
		t.Run(name, func(t *testing.T) {
			wire, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(issuer.key)
			if err != nil {
				t.Fatalf("sign malformed token: %v", err)
			}
			if _, err := issuer.Parse(wire); err == nil {
				t.Fatal("malformed access token should be rejected")
			}
		})
	}
}

func validAccessClaims(now time.Time, version int) accessClaims {
	return accessClaims{
		Version: version,
		Session: "session-id",
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer: "photographer-crm", Subject: "account-id",
			Audience: jwt.ClaimStrings{"photographer-crm-web"}, ID: "token-id",
			IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(10 * time.Minute)),
		},
	}
}
