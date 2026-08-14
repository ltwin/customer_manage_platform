package planshare

import (
	"time"
)

const (
	PolicyVersionV1 = "v1"

	policyMinExpiryOffset = time.Hour
	policyMaxExpiryOffset = 366 * 24 * time.Hour
	policyDefaultHorizon  = 30 * 24 * time.Hour
	policyFullWindowGrace = 24 * time.Hour
	policyQuoteTTL        = 10 * time.Minute

	// AnonymousReadOuterLimit is SharePolicyV1 valid-token read ceiling.
	AnonymousReadOuterLimit    = 300
	AnonymousReadOuterWindow   = 10 * time.Minute
	anonymousProjectionRetries = 3
)

// SharePolicyV1 is the single owner of token expiry ranges and default quotes.
type SharePolicyV1 struct{}

// ExpiryBoundsV1 is the closed command-time expiry interval.
type ExpiryBoundsV1 struct {
	MinExpiresAt time.Time
	MaxExpiresAt time.Time
}

// DefaultQuoteV1 freezes a 10-minute window for submitting the resolved default.
type DefaultQuoteV1 struct {
	EvaluatedAt             time.Time `json:"evaluated_at"`
	ValidUntil              time.Time `json:"valid_until"`
	ViewLevel               ViewLevel `json:"view_level"`
	ExecutionWindowRevision *int64    `json:"execution_window_revision,omitempty"`
	PolicyVersion           string    `json:"policy_version"`
}

// ExpiryPolicyProjectionV1 is returned once per share view in management GET.
type ExpiryPolicyProjectionV1 struct {
	ViewLevel                ViewLevel      `json:"view_level"`
	MinExpiresAt             time.Time      `json:"min_expires_at"`
	MaxExpiresAt             time.Time      `json:"max_expires_at"`
	ResolvedDefaultExpiresAt time.Time      `json:"resolved_default_expires_at"`
	PolicyVersion            string         `json:"policy_version"`
	DefaultQuote             DefaultQuoteV1 `json:"default_quote"`
}

// ExecutionWindowHint is the public window used for full default expiry.
type ExecutionWindowHint struct {
	EndsAt   time.Time
	Revision int64
}

func (SharePolicyV1) Version() string { return PolicyVersionV1 }

func (SharePolicyV1) Bounds(evaluatedAt time.Time) ExpiryBoundsV1 {
	now := evaluatedAt.UTC()
	return ExpiryBoundsV1{
		MinExpiresAt: now.Add(policyMinExpiryOffset),
		MaxExpiresAt: now.Add(policyMaxExpiryOffset),
	}
}

func (p SharePolicyV1) DesiredDefault(
	view ViewLevel,
	evaluatedAt time.Time,
	window *ExecutionWindowHint,
) time.Time {
	now := evaluatedAt.UTC()
	if view == ViewLevelFull && window != nil {
		return window.EndsAt.UTC().Add(policyFullWindowGrace)
	}
	return now.Add(policyDefaultHorizon)
}

func (p SharePolicyV1) Clamp(desired, evaluatedAt time.Time) time.Time {
	bounds := p.Bounds(evaluatedAt)
	desired = desired.UTC()
	if desired.Before(bounds.MinExpiresAt) {
		return bounds.MinExpiresAt
	}
	if desired.After(bounds.MaxExpiresAt) {
		return bounds.MaxExpiresAt
	}
	return desired
}

func (p SharePolicyV1) ResolvedDefault(
	view ViewLevel,
	evaluatedAt time.Time,
	window *ExecutionWindowHint,
) time.Time {
	return p.Clamp(p.DesiredDefault(view, evaluatedAt, window), evaluatedAt)
}

func (p SharePolicyV1) Project(
	view ViewLevel,
	evaluatedAt time.Time,
	window *ExecutionWindowHint,
) ExpiryPolicyProjectionV1 {
	bounds := p.Bounds(evaluatedAt)
	resolved := p.ResolvedDefault(view, evaluatedAt, window)
	quote := DefaultQuoteV1{
		EvaluatedAt:   evaluatedAt.UTC(),
		ValidUntil:    evaluatedAt.UTC().Add(policyQuoteTTL),
		ViewLevel:     view,
		PolicyVersion: p.Version(),
	}
	if view == ViewLevelFull && window != nil {
		rev := window.Revision
		quote.ExecutionWindowRevision = &rev
	}
	return ExpiryPolicyProjectionV1{
		ViewLevel:                view,
		MinExpiresAt:             bounds.MinExpiresAt,
		MaxExpiresAt:             bounds.MaxExpiresAt,
		ResolvedDefaultExpiresAt: resolved,
		PolicyVersion:            p.Version(),
		DefaultQuote:             quote,
	}
}

func (p SharePolicyV1) ValidateExplicit(expiresAt, commandNow time.Time) error {
	bounds := p.Bounds(commandNow)
	expiresAt = expiresAt.UTC()
	if expiresAt.Before(bounds.MinExpiresAt) || expiresAt.After(bounds.MaxExpiresAt) {
		return ErrExpiryOutOfRange
	}
	return nil
}

func (p SharePolicyV1) ValidateQuotedDefault(
	expiresAt time.Time,
	quote DefaultQuoteV1,
	view ViewLevel,
	commandNow time.Time,
	window *ExecutionWindowHint,
) error {
	if quote.PolicyVersion != p.Version() || quote.ViewLevel != view {
		return ErrExpiryQuoteStale
	}
	now := commandNow.UTC()
	if now.Before(quote.EvaluatedAt.UTC()) || now.After(quote.ValidUntil.UTC()) {
		return ErrExpiryQuoteExpired
	}
	if view == ViewLevelFull {
		if window == nil {
			if quote.ExecutionWindowRevision != nil {
				return ErrExpiryQuoteStale
			}
		} else if quote.ExecutionWindowRevision == nil || *quote.ExecutionWindowRevision != window.Revision {
			return ErrExpiryQuoteStale
		}
	} else if quote.ExecutionWindowRevision != nil {
		return ErrExpiryQuoteStale
	}
	expected := p.ResolvedDefault(view, quote.EvaluatedAt, window)
	if !expiresAt.UTC().Equal(expected) {
		return ErrExpiryQuoteStale
	}
	return nil
}
