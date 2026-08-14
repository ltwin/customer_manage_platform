package planshare

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/securitybudget"
)

// ClientIPDigestResolver turns a trusted source IP into HMAC digest candidates.
// Raw IP must never enter planshare application logic — only digests.
type ClientIPDigestResolver interface {
	ResolveTokenIP(ctx context.Context, sourceIP string, tokenFingerprint string, now time.Time) (securitybudget.DigestCandidates, error)
}

// FailClosedIPDigestResolver rejects all digests when the HMAC key is missing.
type FailClosedIPDigestResolver struct {
	Key []byte
}

func (r FailClosedIPDigestResolver) ResolveTokenIP(
	_ context.Context,
	sourceIP string,
	tokenFingerprint string,
	now time.Time,
) (securitybudget.DigestCandidates, error) {
	if len(r.Key) == 0 {
		return securitybudget.DigestCandidates{}, securitybudget.ErrUnavailable
	}
	if sourceIP == "" || tokenFingerprint == "" {
		return securitybudget.DigestCandidates{}, securitybudget.ErrInvalidCandidates
	}
	day := now.UTC().Format("2006-01-02")
	mac := hmac.New(sha256.New, r.Key)
	_, _ = fmt.Fprintf(mac, "planshare-read-token-ip-v1\n%s\n%s\n%s", day, tokenFingerprint, sourceIP)
	sum := mac.Sum(nil)
	return securitybudget.DigestCandidates{
		Current: securitybudget.DigestIdentity{
			Version: securitybudget.DigestVersion(day),
			HMAC:    sum,
		},
	}, nil
}

// AnonymousReadBudget is the typed read outer attempt port.
type AnonymousReadBudget interface {
	ConsumeReadOuter(
		ctx context.Context,
		candidates securitybudget.DigestCandidates,
		now time.Time,
	) (retryAfter time.Duration, allowed bool, err error)
}

// SecurityBudgetReadGate adapts securitybudget.Gate for anonymous read.
type SecurityBudgetReadGate struct {
	Gate securitybudget.Gate
}

func (g SecurityBudgetReadGate) ConsumeReadOuter(
	ctx context.Context,
	candidates securitybudget.DigestCandidates,
	now time.Time,
) (time.Duration, bool, error) {
	if g.Gate == nil {
		return 0, false, securitybudget.ErrUnavailable
	}
	return g.Gate.Consume(
		ctx,
		securitybudget.PolicyV1,
		securitybudget.ActionAnonymousReadOuter,
		securitybudget.DimensionTokenIP,
		candidates,
		AnonymousReadOuterWindow,
		AnonymousReadOuterLimit,
		now,
	)
}

// ConsumeAnonymousReadOuter applies the read outer budget after token resolve.
func ConsumeAnonymousReadOuter(
	ctx context.Context,
	budget AnonymousReadBudget,
	resolver ClientIPDigestResolver,
	sourceIP string,
	fingerprint string,
	now time.Time,
) (retryAfter time.Duration, err error) {
	if budget == nil || resolver == nil {
		return 0, securitybudget.ErrUnavailable
	}
	candidates, err := resolver.ResolveTokenIP(ctx, sourceIP, fingerprint, now)
	if err != nil {
		return 0, err
	}
	retry, allowed, err := budget.ConsumeReadOuter(ctx, candidates, now)
	if err != nil {
		return 0, err
	}
	if !allowed {
		return retry, ErrAnonymousReadRateLimited
	}
	return 0, nil
}

// ErrAnonymousReadRateLimited maps to HTTP 429.
var ErrAnonymousReadRateLimited = errors.New("anonymous_read_rate_limited")
