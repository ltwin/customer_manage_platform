package planshare_test

import (
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/planshare"
)

func TestSharePolicyClampAndExplicitBounds(t *testing.T) {
	policy := planshare.SharePolicyV1{}
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)

	// proposal / no-window desired = now+30d, within bounds.
	got := policy.ResolvedDefault(planshare.ViewLevelProposal, now, nil)
	want := now.Add(30 * 24 * time.Hour)
	if !got.Equal(want) {
		t.Fatalf("proposal default = %s want %s", got, want)
	}

	// past window clamps to lower bound.
	past := &planshare.ExecutionWindowHint{EndsAt: now.Add(-10 * 24 * time.Hour), Revision: 3}
	got = policy.ResolvedDefault(planshare.ViewLevelFull, now, past)
	if !got.Equal(now.Add(time.Hour)) {
		t.Fatalf("past window default = %s want lower bound", got)
	}

	// far-future window clamps to upper bound.
	far := &planshare.ExecutionWindowHint{EndsAt: now.Add(400 * 24 * time.Hour), Revision: 4}
	got = policy.ResolvedDefault(planshare.ViewLevelFull, now, far)
	if !got.Equal(now.Add(366 * 24 * time.Hour)) {
		t.Fatalf("far window default = %s want upper bound", got)
	}

	// normal window = ends_at+24h.
	normal := &planshare.ExecutionWindowHint{EndsAt: now.Add(2 * 24 * time.Hour), Revision: 5}
	got = policy.ResolvedDefault(planshare.ViewLevelFull, now, normal)
	if !got.Equal(normal.EndsAt.Add(24 * time.Hour)) {
		t.Fatalf("normal window default = %s", got)
	}

	lower := now.Add(time.Hour)
	upper := now.Add(366 * 24 * time.Hour)
	if err := policy.ValidateExplicit(lower, now); err != nil {
		t.Fatalf("lower equality: %v", err)
	}
	if err := policy.ValidateExplicit(upper, now); err != nil {
		t.Fatalf("upper equality: %v", err)
	}
	if err := policy.ValidateExplicit(lower.Add(-time.Second), now); err == nil {
		t.Fatal("below lower should reject")
	}
	if err := policy.ValidateExplicit(upper.Add(time.Second), now); err == nil {
		t.Fatal("above upper should reject")
	}
}

func TestSharePolicyExpiryQuoteStaleIndependent(t *testing.T) {
	policy := planshare.SharePolicyV1{}
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	window := &planshare.ExecutionWindowHint{EndsAt: now.Add(2 * 24 * time.Hour), Revision: 7}
	proj := policy.Project(planshare.ViewLevelFull, now, window)
	quote := proj.DefaultQuote

	// wrong absolute expires_at vs resolved default → stale (not expired).
	if err := policy.ValidateQuotedDefault(proj.ResolvedDefaultExpiresAt.Add(time.Minute), quote, planshare.ViewLevelFull, now.Add(time.Minute), window); err != planshare.ErrExpiryQuoteStale {
		t.Fatalf("mismatched expires_at err=%v", err)
	}

	// execution window revision drift → stale.
	drifted := &planshare.ExecutionWindowHint{EndsAt: window.EndsAt, Revision: window.Revision + 1}
	if err := policy.ValidateQuotedDefault(proj.ResolvedDefaultExpiresAt, quote, planshare.ViewLevelFull, now.Add(time.Minute), drifted); err != planshare.ErrExpiryQuoteStale {
		t.Fatalf("window revision drift err=%v", err)
	}

	// proposal quote must not carry execution window revision.
	proposalQuote := quote
	proposalQuote.ViewLevel = planshare.ViewLevelProposal
	if err := policy.ValidateQuotedDefault(policy.ResolvedDefault(planshare.ViewLevelProposal, quote.EvaluatedAt, nil), proposalQuote, planshare.ViewLevelProposal, now.Add(time.Minute), nil); err != planshare.ErrExpiryQuoteStale {
		t.Fatalf("proposal quote with window revision err=%v", err)
	}

	// happy path stays non-stale within TTL.
	if err := policy.ValidateQuotedDefault(proj.ResolvedDefaultExpiresAt, quote, planshare.ViewLevelFull, now.Add(time.Minute), window); err != nil {
		t.Fatalf("valid quoted default: %v", err)
	}
}
