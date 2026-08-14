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

const (
	AnonymousMutationOuterIPLimit              = 300
	AnonymousMutationOuterTokenIPLimit         = 120
	AnonymousMutationOuterTokenGenerationLimit = 600
	AnonymousMutationOuterWindow               = 10 * time.Minute

	AnonymousMutationBusinessTokenIPLimit         = 30
	AnonymousMutationBusinessTokenGenerationLimit = 200
	AnonymousMutationBusinessWindow               = time.Hour
)

// MutationIPDigestResolver builds digests for anonymous mutation rate dimensions.
type MutationIPDigestResolver interface {
	ResolveMutationIP(ctx context.Context, sourceIP string, now time.Time) (securitybudget.DigestCandidates, error)
	ResolveMutationTokenGenerationIP(
		ctx context.Context,
		sourceIP string,
		tokenGenerationID string,
		now time.Time,
	) (securitybudget.DigestCandidates, error)
	ResolveMutationTokenGeneration(
		ctx context.Context,
		tokenGenerationID string,
		now time.Time,
	) (securitybudget.DigestCandidates, error)
}

// FailClosedMutationIPDigestResolver HMAC-digests mutation dimensions; missing key fails closed.
type FailClosedMutationIPDigestResolver struct {
	Key []byte
}

func (r FailClosedMutationIPDigestResolver) ResolveMutationIP(
	_ context.Context,
	sourceIP string,
	now time.Time,
) (securitybudget.DigestCandidates, error) {
	return r.digest("planshare-mutation-ip-v1", now, sourceIP)
}

func (r FailClosedMutationIPDigestResolver) ResolveMutationTokenGenerationIP(
	_ context.Context,
	sourceIP string,
	tokenGenerationID string,
	now time.Time,
) (securitybudget.DigestCandidates, error) {
	return r.digest("planshare-mutation-token-generation-ip-v1", now, tokenGenerationID, sourceIP)
}

func (r FailClosedMutationIPDigestResolver) ResolveMutationTokenGeneration(
	_ context.Context,
	tokenGenerationID string,
	now time.Time,
) (securitybudget.DigestCandidates, error) {
	return r.digest("planshare-mutation-token-generation-v1", now, tokenGenerationID)
}

func (r FailClosedMutationIPDigestResolver) digest(
	domain string,
	now time.Time,
	parts ...string,
) (securitybudget.DigestCandidates, error) {
	if len(r.Key) == 0 {
		return securitybudget.DigestCandidates{}, securitybudget.ErrUnavailable
	}
	for _, part := range parts {
		if part == "" {
			return securitybudget.DigestCandidates{}, securitybudget.ErrInvalidCandidates
		}
	}
	day := now.UTC().Format("2006-01-02")
	mac := hmac.New(sha256.New, r.Key)
	_, _ = fmt.Fprintf(mac, "%s\n%s", domain, day)
	for _, part := range parts {
		_, _ = fmt.Fprintf(mac, "\n%s", part)
	}
	return securitybudget.DigestCandidates{
		Current: securitybudget.DigestIdentity{
			Version: securitybudget.DigestVersion(day),
			HMAC:    mac.Sum(nil),
		},
	}, nil
}

// AnonymousMutationBudget is the typed mutation attempt/quota port.
type AnonymousMutationBudget interface {
	ConsumeMutationOuterIP(
		ctx context.Context,
		candidates securitybudget.DigestCandidates,
		now time.Time,
	) (retryAfter time.Duration, allowed bool, err error)
	ConsumeMutationOuterTokenGenerationIP(
		ctx context.Context,
		candidates securitybudget.DigestCandidates,
		now time.Time,
	) (retryAfter time.Duration, allowed bool, err error)
	ConsumeMutationOuterTokenGeneration(
		ctx context.Context,
		candidates securitybudget.DigestCandidates,
		now time.Time,
	) (retryAfter time.Duration, allowed bool, err error)
	ConsumeMutationBusinessTokenGenerationIP(
		ctx context.Context,
		candidates securitybudget.DigestCandidates,
		now time.Time,
	) (retryAfter time.Duration, allowed bool, err error)
	ConsumeMutationBusinessTokenGeneration(
		ctx context.Context,
		candidates securitybudget.DigestCandidates,
		now time.Time,
	) (retryAfter time.Duration, allowed bool, err error)
}

// SecurityBudgetMutationGate adapts securitybudget.Gate for anonymous mutations.
type SecurityBudgetMutationGate struct {
	Gate securitybudget.Gate
}

func (g SecurityBudgetMutationGate) ConsumeMutationOuterIP(
	ctx context.Context,
	candidates securitybudget.DigestCandidates,
	now time.Time,
) (time.Duration, bool, error) {
	return g.consume(ctx, securitybudget.ActionAnonymousMutationOuter, securitybudget.DimensionIP,
		candidates, AnonymousMutationOuterWindow, AnonymousMutationOuterIPLimit, now)
}

func (g SecurityBudgetMutationGate) ConsumeMutationOuterTokenGenerationIP(
	ctx context.Context,
	candidates securitybudget.DigestCandidates,
	now time.Time,
) (time.Duration, bool, error) {
	return g.consume(ctx, securitybudget.ActionAnonymousMutationOuter, securitybudget.DimensionTokenGenerationIP,
		candidates, AnonymousMutationOuterWindow, AnonymousMutationOuterTokenIPLimit, now)
}

func (g SecurityBudgetMutationGate) ConsumeMutationOuterTokenGeneration(
	ctx context.Context,
	candidates securitybudget.DigestCandidates,
	now time.Time,
) (time.Duration, bool, error) {
	return g.consume(ctx, securitybudget.ActionAnonymousMutationOuter, securitybudget.DimensionTokenGeneration,
		candidates, AnonymousMutationOuterWindow, AnonymousMutationOuterTokenGenerationLimit, now)
}

func (g SecurityBudgetMutationGate) ConsumeMutationBusinessTokenGenerationIP(
	ctx context.Context,
	candidates securitybudget.DigestCandidates,
	now time.Time,
) (time.Duration, bool, error) {
	return g.consume(ctx, securitybudget.ActionAnonymousMutationBusiness, securitybudget.DimensionTokenGenerationIP,
		candidates, AnonymousMutationBusinessWindow, AnonymousMutationBusinessTokenIPLimit, now)
}

func (g SecurityBudgetMutationGate) ConsumeMutationBusinessTokenGeneration(
	ctx context.Context,
	candidates securitybudget.DigestCandidates,
	now time.Time,
) (time.Duration, bool, error) {
	return g.consume(ctx, securitybudget.ActionAnonymousMutationBusiness, securitybudget.DimensionTokenGeneration,
		candidates, AnonymousMutationBusinessWindow, AnonymousMutationBusinessTokenGenerationLimit, now)
}

func (g SecurityBudgetMutationGate) consume(
	ctx context.Context,
	action securitybudget.Action,
	dimension securitybudget.Dimension,
	candidates securitybudget.DigestCandidates,
	window time.Duration,
	limit int,
	now time.Time,
) (time.Duration, bool, error) {
	if g.Gate == nil {
		return 0, false, securitybudget.ErrUnavailable
	}
	return g.Gate.Consume(ctx, securitybudget.PolicyV1, action, dimension, candidates, window, limit, now)
}

// ErrAnonymousMutationRateLimited maps to HTTP 429.
var ErrAnonymousMutationRateLimited = errors.New("anonymous_mutation_rate_limited")

// MutationRateLimitedError carries Retry-After.
type MutationRateLimitedError struct {
	RetryAfter time.Duration
}

func (e MutationRateLimitedError) Error() string { return ErrAnonymousMutationRateLimited.Error() }
func (e MutationRateLimitedError) Unwrap() error { return ErrAnonymousMutationRateLimited }

// ConsumeAnonymousMutationOuterIP applies the pre-resolver IP outer ceiling.
func ConsumeAnonymousMutationOuterIP(
	ctx context.Context,
	budget AnonymousMutationBudget,
	resolver MutationIPDigestResolver,
	sourceIP string,
	now time.Time,
) (retryAfter time.Duration, err error) {
	if budget == nil || resolver == nil {
		return 0, securitybudget.ErrUnavailable
	}
	candidates, err := resolver.ResolveMutationIP(ctx, sourceIP, now)
	if err != nil {
		return 0, err
	}
	retry, allowed, err := budget.ConsumeMutationOuterIP(ctx, candidates, now)
	if err != nil {
		return 0, err
	}
	if !allowed {
		return retry, MutationRateLimitedError{RetryAfter: retry}
	}
	return 0, nil
}

// ConsumeAnonymousMutationOuterAfterGrant applies token-bound outer ceilings.
func ConsumeAnonymousMutationOuterAfterGrant(
	ctx context.Context,
	budget AnonymousMutationBudget,
	resolver MutationIPDigestResolver,
	sourceIP string,
	tokenGenerationID string,
	now time.Time,
) (retryAfter time.Duration, err error) {
	if budget == nil || resolver == nil {
		return 0, securitybudget.ErrUnavailable
	}
	tokenIP, err := resolver.ResolveMutationTokenGenerationIP(ctx, sourceIP, tokenGenerationID, now)
	if err != nil {
		return 0, err
	}
	retry, allowed, err := budget.ConsumeMutationOuterTokenGenerationIP(ctx, tokenIP, now)
	if err != nil {
		return 0, err
	}
	if !allowed {
		return retry, MutationRateLimitedError{RetryAfter: retry}
	}
	tokenOnly, err := resolver.ResolveMutationTokenGeneration(ctx, tokenGenerationID, now)
	if err != nil {
		return 0, err
	}
	retry, allowed, err = budget.ConsumeMutationOuterTokenGeneration(ctx, tokenOnly, now)
	if err != nil {
		return 0, err
	}
	if !allowed {
		return retry, MutationRateLimitedError{RetryAfter: retry}
	}
	return 0, nil
}

// ConsumeAnonymousMutationBusinessQuota reserves business mutation quota.
func ConsumeAnonymousMutationBusinessQuota(
	ctx context.Context,
	budget AnonymousMutationBudget,
	resolver MutationIPDigestResolver,
	sourceIP string,
	tokenGenerationID string,
	now time.Time,
) (retryAfter time.Duration, err error) {
	if budget == nil || resolver == nil {
		return 0, securitybudget.ErrUnavailable
	}
	tokenIP, err := resolver.ResolveMutationTokenGenerationIP(ctx, sourceIP, tokenGenerationID, now)
	if err != nil {
		return 0, err
	}
	retry, allowed, err := budget.ConsumeMutationBusinessTokenGenerationIP(ctx, tokenIP, now)
	if err != nil {
		return 0, err
	}
	if !allowed {
		return retry, MutationRateLimitedError{RetryAfter: retry}
	}
	tokenOnly, err := resolver.ResolveMutationTokenGeneration(ctx, tokenGenerationID, now)
	if err != nil {
		return 0, err
	}
	retry, allowed, err = budget.ConsumeMutationBusinessTokenGeneration(ctx, tokenOnly, now)
	if err != nil {
		return 0, err
	}
	if !allowed {
		return retry, MutationRateLimitedError{RetryAfter: retry}
	}
	return 0, nil
}
