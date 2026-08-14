package store

import (
	"context"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/securitybudget"
)

// SecurityAttemptBudgetGate adapts Store to securitybudget.Gate.
type SecurityAttemptBudgetGate struct {
	Store *Store
}

func (g SecurityAttemptBudgetGate) Consume(
	ctx context.Context,
	policy securitybudget.PolicyVersion,
	action securitybudget.Action,
	dimension securitybudget.Dimension,
	candidates securitybudget.DigestCandidates,
	window time.Duration,
	limit int,
	now time.Time,
) (time.Duration, bool, error) {
	if g.Store == nil {
		return 0, false, securitybudget.ErrUnavailable
	}
	return g.Store.ConsumeSecurityAttemptBudget(ctx, policy, action, dimension, candidates, window, limit, now)
}
