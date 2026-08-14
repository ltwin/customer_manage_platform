package crm

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const (
	SummaryViewByCustomer = "planning_summary_by_customer"
	SummaryViewByOrder    = "planning_summary_by_order"
	SummaryViewBySlot     = "planning_summary_by_slot"
)

type summaryScope interface {
	Query(context.Context, string, string, string, ...any) (store.Rows, error)
}

func LoadSummaries(ctx context.Context, scope summaryScope, view string, ids []string) (map[string]Summary, error) {
	out := make(map[string]Summary, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	idColumn := summaryIDColumn(view)
	rows, err := scope.Query(ctx, view, idColumn+", "+summaryColumns, idColumn+" = ANY($2::text[])", ids)
	if err != nil {
		return nil, fmt.Errorf("load planning summaries: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var scopeID string
		summary, err := scanSummary(rows, &scopeID)
		if err != nil {
			return nil, err
		}
		out[scopeID] = summary
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func summaryIDColumn(view string) string {
	switch view {
	case SummaryViewByOrder:
		return "order_id"
	case SummaryViewBySlot:
		return "slot_id"
	default:
		return "customer_id"
	}
}

func scanSummary(row interface{ Scan(...any) error }, scopeID *string) (Summary, error) {
	var summary Summary
	var windowStart, windowEnd sql.NullTime
	var timezone, source, warning sql.NullString
	if err := row.Scan(
		scopeID,
		&summary.PlanCount,
		&summary.ActivePlanCount,
		&summary.PrimaryPlanID,
		&summary.PrimaryTitle,
		&summary.PrimaryStatus,
		&summary.ShotCount,
		&summary.UncheckedCount,
		&windowStart,
		&windowEnd,
		&timezone,
		&source,
		&warning,
	); err != nil {
		return Summary{}, err
	}
	if windowStart.Valid {
		value := windowStart.Time.UTC()
		summary.WindowStartsAt = &value
	}
	if windowEnd.Valid {
		value := windowEnd.Time.UTC()
		summary.WindowEndsAt = &value
	}
	summary.WindowTimezone = nullStringPtr(timezone)
	summary.WindowSource = nullStringPtr(source)
	summary.LinkWarning = nullStringPtr(warning)
	return summary, nil
}
