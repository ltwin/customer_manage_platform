package store

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// QueryClockTimestamp returns PostgreSQL clock_timestamp() inside the current tx.
// Callers must not use application clocks for freshness or call authorization.
func (sc TxAccountScope) QueryClockTimestamp(ctx context.Context) (time.Time, error) {
	if sc.AccountID() == "" {
		return time.Time{}, ErrEmptyAccountScope
	}
	var ts time.Time
	if err := sc.scope.execRunner().QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&ts); err != nil {
		return time.Time{}, fmt.Errorf("query clock_timestamp: %w", err)
	}
	return ts.UTC(), nil
}

// DigestCallPermitInsert is the guarded INSERT input for a calling attempt.
type DigestCallPermitInsert struct {
	DeliveryID               string
	AttemptID                string
	IntentRevision           int64
	RecipientBindingRevision int64
	ClaimID                  string
	RecipientChatID          string
	HasEarliestValidUntil    bool
	EarliestValidUntil       time.Time
}

// DigestCallPermitResult is returned by a successful guarded calling CAS.
type DigestCallPermitResult struct {
	AttemptID                        string
	IntentRevision                   int64
	DBAuthorizedAt                   time.Time
	StartDeadline                    time.Time
	BusinessRemainingAtAuthorization time.Duration
}

// ErrDigestCallNotAuthorized means the final guarded CAS matched 0 rows.
var ErrDigestCallNotAuthorized = errors.New("digest_call_not_authorized")

// BeginDigestCallingPermitInScope atomically supersedes expired calling permits and
// inserts a new calling permit when Delivery claim, active intent, binding, and
// temporal guards all still hold under clock_timestamp().
func (sc TxAccountScope) BeginDigestCallingPermitInScope(
	ctx context.Context,
	in DigestCallPermitInsert,
) (DigestCallPermitResult, error) {
	if sc.AccountID() == "" {
		return DigestCallPermitResult{}, ErrEmptyAccountScope
	}
	if in.DeliveryID == "" || in.AttemptID == "" || in.IntentRevision < 1 || in.ClaimID == "" || in.RecipientChatID == "" {
		return DigestCallPermitResult{}, errors.New("digest call permit input incomplete")
	}

	if _, err := sc.scope.execRunner().Exec(ctx, `
UPDATE delivery_send_attempt_permits
SET outcome = 'superseded', finalized_at = clock_timestamp()
WHERE account_id = $1
  AND delivery_id = $2
  AND outcome = 'calling'
  AND start_deadline <= clock_timestamp()`,
		sc.AccountID(), in.DeliveryID,
	); err != nil {
		return DigestCallPermitResult{}, fmt.Errorf("supersede expired calling permits: %w", err)
	}

	sqlText := `
WITH auth AS (
  SELECT clock_timestamp() AS db_authorized_at
),
guard AS (
  SELECT
    a.db_authorized_at,
    CASE
      WHEN $7::boolean THEN LEAST(a.db_authorized_at + interval '5 seconds', $8::timestamptz)
      ELSE a.db_authorized_at + interval '5 seconds'
    END AS start_deadline
  FROM auth a
  WHERE EXISTS (
    SELECT 1 FROM telegram_deliveries d
    WHERE d.account_id = $1
      AND d.id = $2
      AND d.status = 'pending'
      AND d.claim_id = $5
      AND d.lease_until > a.db_authorized_at
      AND d.active_intent_revision = $3
  )
  AND EXISTS (
    SELECT 1 FROM settings s
    WHERE s.account_id = $1
      AND s.telegram_chat_id = $6
      AND s.telegram_binding_revision = $4
  )
  AND EXISTS (
    SELECT 1 FROM plan_assignment_reminder_digest_intents i
    WHERE i.account_id = $1
      AND i.delivery_id = $2
      AND i.intent_revision = $3
      AND i.recipient_chat_id_snapshot = $6
      AND i.recipient_binding_revision = $4
      AND i.payload_text IS NOT NULL
      AND (
        NOT $7::boolean
        OR (i.earliest_valid_until IS NOT NULL AND a.db_authorized_at < i.earliest_valid_until)
      )
  )
  AND NOT EXISTS (
    SELECT 1 FROM delivery_send_attempt_permits p
    WHERE p.account_id = $1
      AND p.delivery_id = $2
      AND p.outcome = 'calling'
      AND p.start_deadline > a.db_authorized_at
  )
  AND (NOT $7::boolean OR a.db_authorized_at < $8::timestamptz)
),
ins AS (
  INSERT INTO delivery_send_attempt_permits (
    account_id, delivery_id, attempt_id, intent_revision, recipient_binding_revision,
    db_authorized_at, start_deadline, outcome
  )
  SELECT $1, $2, $9, $3, $4, g.db_authorized_at, g.start_deadline, 'calling'
  FROM guard g
  WHERE g.start_deadline > g.db_authorized_at
  RETURNING attempt_id, intent_revision, db_authorized_at, start_deadline
)
SELECT attempt_id, intent_revision, db_authorized_at, start_deadline FROM ins`

	var (
		attemptID      string
		intentRevision int64
		authorizedAt   time.Time
		deadline       time.Time
	)
	err := sc.scope.execRunner().QueryRow(ctx, sqlText,
		sc.AccountID(),
		in.DeliveryID,
		in.IntentRevision,
		in.RecipientBindingRevision,
		in.ClaimID,
		in.RecipientChatID,
		in.HasEarliestValidUntil,
		nullableTime(in.HasEarliestValidUntil, in.EarliestValidUntil),
		in.AttemptID,
	).Scan(&attemptID, &intentRevision, &authorizedAt, &deadline)
	if errors.Is(err, ErrNoRows) {
		return DigestCallPermitResult{}, ErrDigestCallNotAuthorized
	}
	if err != nil {
		if IsUniqueViolation(err, "delivery_send_attempt_permits_one_calling_idx") ||
			IsUniqueViolation(err, "delivery_send_attempt_permits_pkey") {
			return DigestCallPermitResult{}, ErrDigestCallNotAuthorized
		}
		return DigestCallPermitResult{}, fmt.Errorf("begin digest calling permit: %w", err)
	}
	authorizedAt = authorizedAt.UTC()
	deadline = deadline.UTC()
	return DigestCallPermitResult{
		AttemptID:                        attemptID,
		IntentRevision:                   intentRevision,
		DBAuthorizedAt:                   authorizedAt,
		StartDeadline:                    deadline,
		BusinessRemainingAtAuthorization: deadline.Sub(authorizedAt),
	}, nil
}

func nullableTime(ok bool, t time.Time) any {
	if !ok {
		return nil
	}
	return t.UTC()
}
