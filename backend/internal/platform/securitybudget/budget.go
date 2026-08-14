package securitybudget

import (
	"context"
	"errors"
	"time"
)

// PolicyVersion is a closed policy generation for pre-auth counters.
type PolicyVersion string

const PolicyV1 PolicyVersion = "v1"

// Action is a closed counter action. Planshare maps SharePolicyV1 here; auth
// actions must not be mixed into this table.
type Action string

const (
	ActionAnonymousReadOuter        Action = "planshare_anonymous_read_v1"
	ActionAnonymousMutationOuter    Action = "planshare_anonymous_mutation_outer_v1"
	ActionAnonymousMutationBusiness Action = "planshare_anonymous_mutation_business_v1"
)

// Dimension is a closed counter dimension. Raw IP / token / body are forbidden.
type Dimension string

const (
	DimensionIP                Dimension = "ip"
	DimensionTokenIP           Dimension = "token_ip"
	DimensionTokenGeneration   Dimension = "token_generation"
	DimensionTokenGenerationIP Dimension = "token_generation_ip"
)

// DigestVersion labels the HMAC key epoch (daily rollover).
type DigestVersion string

const DigestVersionV1 DigestVersion = "d1"

// DigestIdentity is an opaque HMAC digest candidate. It must never carry raw IP.
type DigestIdentity struct {
	Version DigestVersion
	HMAC    []byte
}

// DigestCandidates carries current/previous identities for rollover grace.
type DigestCandidates struct {
	Current            DigestIdentity
	Previous           *DigestIdentity
	RolloverGraceUntil time.Time
}

var (
	ErrInvalidCandidates = errors.New("invalid security attempt digest candidates")
	ErrUnavailable       = errors.New("security attempt budget unavailable")
	ErrInvariant         = errors.New("security attempt budget invariant violated")
)

// Gate is the platform-security consume surface used by anonymous rate limits.
type Gate interface {
	Consume(
		ctx context.Context,
		policy PolicyVersion,
		action Action,
		dimension Dimension,
		candidates DigestCandidates,
		window time.Duration,
		limit int,
		now time.Time,
	) (retryAfter time.Duration, allowed bool, err error)
}

func ValidateClosed(policy PolicyVersion, action Action, dimension Dimension) error {
	if policy != PolicyV1 {
		return ErrInvalidCandidates
	}
	switch action {
	case ActionAnonymousReadOuter, ActionAnonymousMutationOuter, ActionAnonymousMutationBusiness:
	default:
		return ErrInvalidCandidates
	}
	switch dimension {
	case DimensionIP, DimensionTokenIP, DimensionTokenGeneration, DimensionTokenGenerationIP:
	default:
		return ErrInvalidCandidates
	}
	return nil
}

func ValidateCandidates(candidates DigestCandidates, now time.Time) error {
	if err := validateIdentity(candidates.Current); err != nil {
		return err
	}
	if candidates.Previous == nil {
		return nil
	}
	if err := validateIdentity(*candidates.Previous); err != nil {
		return err
	}
	if candidates.RolloverGraceUntil.IsZero() || now.After(candidates.RolloverGraceUntil) {
		return ErrInvalidCandidates
	}
	return nil
}

func validateIdentity(identity DigestIdentity) error {
	if identity.Version == "" || len(identity.HMAC) != 32 {
		return ErrInvalidCandidates
	}
	return nil
}
