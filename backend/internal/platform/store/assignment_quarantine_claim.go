package store

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ClaimedAssignmentQuarantine is one due quarantine row leased via SKIP LOCKED.
type ClaimedAssignmentQuarantine struct {
	SourceEventID string
	PlanID        string
	AttemptCount  int
}

// ClaimDueAssignmentQuarantinesInScope leases unresolved quarantine rows that are
// due for repair. Uses FOR UPDATE SKIP LOCKED so dual connection pools do not deadlock.
func (sc TxAccountScope) ClaimDueAssignmentQuarantinesInScope(
	ctx context.Context,
	limit int,
	leaseOwner string,
	leaseFor time.Duration,
) ([]ClaimedAssignmentQuarantine, error) {
	if sc.AccountID() == "" {
		return nil, ErrEmptyAccountScope
	}
	if limit < 1 || leaseOwner == "" {
		return nil, errors.New("quarantine claim args incomplete")
	}
	if leaseFor <= 0 {
		leaseFor = 30 * time.Second
	}
	sqlText := `
WITH due AS (
  SELECT source_event_id
  FROM plan_assignment_reminder_quarantines
  WHERE account_id = $1
    AND resolved_at IS NULL
    AND next_retry_at <= clock_timestamp()
    AND (lease_until IS NULL OR lease_until < clock_timestamp())
  ORDER BY next_retry_at, source_event_id
  LIMIT $2
  FOR UPDATE SKIP LOCKED
),
leased AS (
  UPDATE plan_assignment_reminder_quarantines q
  SET lease_owner = $3,
      lease_until = clock_timestamp() + ($4::text)::interval
  FROM due
  WHERE q.account_id = $1
    AND q.source_event_id = due.source_event_id
    AND q.resolved_at IS NULL
  RETURNING q.source_event_id, q.plan_id, q.attempt_count
)
SELECT source_event_id, plan_id, attempt_count FROM leased
ORDER BY source_event_id`
	rows, err := sc.scope.execRunner().Query(ctx, sqlText,
		sc.AccountID(), limit, leaseOwner, fmt.Sprintf("%d milliseconds", leaseFor.Milliseconds()),
	)
	if err != nil {
		return nil, fmt.Errorf("claim due assignment quarantines: %w", err)
	}
	defer rows.Close()
	out := make([]ClaimedAssignmentQuarantine, 0, limit)
	for rows.Next() {
		var row ClaimedAssignmentQuarantine
		if err := rows.Scan(&row.SourceEventID, &row.PlanID, &row.AttemptCount); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
