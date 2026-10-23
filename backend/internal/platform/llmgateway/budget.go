package llmgateway

import (
	"context"
	"errors"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// MonthBudget is what is left of the account's model budget this month.
type MonthBudget struct {
	// RemainingMicros is the limit less what is spent and still held; it is
	// never negative.
	RemainingMicros int64
	Currency        string
	// PeriodEnd is when the month bucket resets (UTC).
	PeriodEnd time.Time
}

// MonthBudget reads the current month bucket without reserving anything. A
// month nobody has spent in yet has the whole ceiling left.
func (s *Service) MonthBudget(ctx context.Context, scope store.AccountScope) (MonthBudget, error) {
	period := monthStart(s.now())
	out := MonthBudget{Currency: s.catalog.Currency(), PeriodEnd: period.AddDate(0, 1, 0)}
	var limit, reserved, spent int64
	err := scope.QueryRow(ctx, "llm_budgets", "limit_micros,reserved_micros,spent_micros",
		"period_start=$2 AND currency=$3", period, out.Currency).Scan(&limit, &reserved, &spent)
	switch {
	case errors.Is(err, store.ErrNoRows):
		out.RemainingMicros = s.policy.MonthlyLimitMicros
	case err != nil:
		return MonthBudget{}, err
	default:
		out.RemainingMicros = max(0, limit-reserved-spent)
	}
	return out, nil
}
