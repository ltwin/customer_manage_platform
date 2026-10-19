package llmgateway

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// LockCallerGroupInTx lets a caller decide its lifecycle from stable request
// facts. Call after the caller's slot lock, before its run lock. It serializes
// new group admission, then locks all budget buckets before any request.
// The caller still owns its run state; the Gateway never reads caller tables.
func (s *Service) LockCallerGroupInTx(ctx context.Context, tx store.TxAccountScope, caller, group string) ([]RequestView, error) {
	if caller == "" || group == "" {
		return nil, ErrValidation
	}
	var identity string
	err := tx.QueryRowForUpdate(ctx, "llm_call_groups", "caller_group_id", "caller_service=$2 AND caller_group_id=$3", caller, group).Scan(&identity)
	if errors.Is(err, store.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	rows, err := tx.QueryPage(ctx, "llm_usage_reservations", "request_id,budget_period,currency,settlement_state", "caller_service=$2 AND caller_group_id=$3", []store.OrderBy{{Column: "budget_period"}, {Column: "currency"}, {Column: "id"}}, 1000, 0, caller, group)
	if err != nil {
		return nil, err
	}
	type entry struct {
		request  *string
		period   time.Time
		currency string
		state    SettlementState
	}
	var entries []entry
	for rows.Next() {
		var e entry
		if err = rows.Scan(&e.request, &e.period, &e.currency, &e.state); err != nil {
			rows.Close()
			return nil, err
		}
		entries = append(entries, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(entries) == 1000 {
		return nil, fmt.Errorf("%w: caller group bound", ErrValidation)
	}
	for _, e := range entries {
		if e.state == SettlementNotApplicable {
			continue
		}
		if err = s.lockBudget(ctx, tx, e.period, e.currency); err != nil {
			return nil, err
		}
	}
	var views []RequestView
	for _, e := range entries {
		if e.request == nil {
			continue
		}
		// A run's group holds one request per model step, each with the whole
		// prompt; the views need none of it.
		row, err := scanRequestView(tx.QueryRowForUpdate(ctx, "llm_requests", requestViewColumns, "id=$2 AND caller_service=$3 AND caller_group_id=$4", *e.request, caller, group))
		if err != nil {
			return nil, err
		}
		settlement, err := settlementOf(ctx, tx, row.id)
		if err != nil {
			return nil, err
		}
		views = append(views, row.view(settlement))
	}
	return views, nil
}

// CallerGroupRequestIDs lists every request the group admitted, without locks.
// A logical chat turn may own several separately metered requests, so a caller
// summarising what a group cost must read the group, not its own step links.
func (s *Service) CallerGroupRequestIDs(ctx context.Context, scope store.AccountScope, caller, group string) ([]string, error) {
	if caller == "" || group == "" {
		return nil, ErrValidation
	}
	rows, err := scope.QueryPage(ctx, "llm_usage_reservations", "request_id", "caller_service=$2 AND caller_group_id=$3 AND request_id IS NOT NULL", []store.OrderBy{{Column: "id"}}, 1000, 0, caller, group)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 1000 {
		return nil, fmt.Errorf("%w: caller group bound", ErrValidation)
	}
	return ids, nil
}

// AbandonCallerGroupResultsInTx releases this caller's completed-result retains
// only after its application has closed the run in the same transaction. Unknown
// and in-flight results remain available for accounting/reconciliation.
func (s *Service) AbandonCallerGroupResultsInTx(ctx context.Context, tx store.TxAccountScope, caller, group string) error {
	_, err := tx.Update(ctx, "llm_result_consumers", "state='abandoned',released_at=$4", "caller_service=$2 AND state='pending' AND request_id IN (SELECT id FROM llm_requests WHERE account_id=$1 AND caller_service=$2 AND caller_group_id=$3 AND state IN ('succeeded','failed','cancelled'))", caller, group, s.now().UTC())
	return err
}
