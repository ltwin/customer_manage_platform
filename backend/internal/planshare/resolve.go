package planshare

import (
	"context"
	"errors"

	"github.com/samson/customer-manage-platform/backend/internal/platform/txcap"
)

var ErrShareNotFound = errors.New("share not found")

// TokenCommitmentRecord is the narrow selector lookup result used by Resolve.
// It never includes account capability or plaintext secret.
type TokenCommitmentRecord struct {
	Selector    string
	Commitment  ShareSecretCommitment
	Fingerprint string
}

// TokenCommitmentLookup is the repository port for anonymous selector resolution.
type TokenCommitmentLookup interface {
	LookupCommitmentBySelector(ctx context.Context, selector string) (TokenCommitmentRecord, bool, error)
}

// TrustedCapabilityFactory constructs sealed txcap values from a verified row.
// Only store adapters should implement this.
type TrustedCapabilityFactory interface {
	NewShareCapability(fingerprint string) (txcap.ValidatedShareContext, txcap.ShareTransactionCapability)
}

// Resolver validates presented share tokens without exposing AccountScope.
type Resolver struct {
	lookup  TokenCommitmentLookup
	factory TrustedCapabilityFactory
}

func NewResolver(lookup TokenCommitmentLookup, factory TrustedCapabilityFactory) *Resolver {
	return &Resolver{lookup: lookup, factory: factory}
}

func (r *Resolver) Resolve(
	ctx context.Context,
	presentedToken string,
) (txcap.ValidatedShareContext, txcap.ShareTransactionCapability, error) {
	if r == nil || r.lookup == nil || r.factory == nil {
		return nil, nil, errors.New("share token resolver is incomplete")
	}
	parsed, err := ParseShareToken(presentedToken)
	if err != nil {
		return nil, nil, ErrShareNotFound
	}
	record, found, err := r.lookup.LookupCommitmentBySelector(ctx, parsed.Selector)
	if err != nil {
		return nil, nil, err
	}
	if !CompareShareCommitment(found, parsed.Secret, record.Commitment) {
		return nil, nil, ErrShareNotFound
	}
	fingerprint := record.Fingerprint
	if fingerprint == "" {
		fingerprint = ShareFingerprint(parsed.Selector, record.Commitment)
	}
	validated, capability := r.factory.NewShareCapability(fingerprint)
	return validated, capability, nil
}

// DefaultTrustedCapabilityFactory uses txcap constructors after commitment verify.
type DefaultTrustedCapabilityFactory struct{}

func (DefaultTrustedCapabilityFactory) NewShareCapability(
	fingerprint string,
) (txcap.ValidatedShareContext, txcap.ShareTransactionCapability) {
	ctx := txcap.NewValidatedShareContext(fingerprint)
	return ctx, txcap.NewShareTransactionCapability(ctx)
}
