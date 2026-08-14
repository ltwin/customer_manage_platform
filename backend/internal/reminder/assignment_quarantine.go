package reminder

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// QuarantineRecord is a durable, body-free corrupt-event marker.
type QuarantineRecord struct {
	SourceEventID           string
	PlanID                  string
	AssignmentID            string
	AssignmentRevision      int64
	AccountSourceGeneration int64
	ErrorCode               string
	PayloadFingerprint      string
	AttemptCount            int
	NextRetryAt             time.Time
	LeaseOwner              *string
	LeaseUntil              *time.Time
	QuarantinedAt           time.Time
	ResolvedAt              *time.Time
	ResolutionKind          *string
}

// UpsertQuarantineInScope persists quarantine independently of projection txs.
func UpsertQuarantineInScope(ctx context.Context, tx store.TxAccountScope, row QuarantineRecord) error {
	if row.SourceEventID == "" || row.PlanID == "" || row.AssignmentID == "" ||
		row.ErrorCode == "" || row.PayloadFingerprint == "" {
		return errors.New("quarantine row incomplete")
	}
	if row.AttemptCount < 1 {
		row.AttemptCount = 1
	}
	if row.NextRetryAt.IsZero() {
		row.NextRetryAt = time.Now().UTC().Add(quarantineBackoff(row.AttemptCount))
	}
	if row.QuarantinedAt.IsZero() {
		row.QuarantinedAt = time.Now().UTC()
	}
	existing, err := loadUnresolvedQuarantine(ctx, tx, row.SourceEventID)
	if err != nil && !errors.Is(err, store.ErrNoRows) {
		return err
	}
	if err == nil && existing.ResolvedAt == nil {
		nextAttempt := existing.AttemptCount + 1
		_, err := tx.Update(ctx, "plan_assignment_reminder_quarantines",
			"attempt_count = $2, next_retry_at = $3, error_code = $4, payload_fingerprint = $5",
			"source_event_id = $6 AND resolved_at IS NULL",
			nextAttempt, time.Now().UTC().Add(quarantineBackoff(nextAttempt)),
			row.ErrorCode, row.PayloadFingerprint, row.SourceEventID)
		return err
	}
	return tx.Insert(ctx, "plan_assignment_reminder_quarantines",
		[]string{
			"source_event_id", "plan_id", "assignment_id", "assignment_revision",
			"account_source_generation", "error_code", "payload_fingerprint",
			"attempt_count", "next_retry_at", "quarantined_at",
		},
		row.SourceEventID, row.PlanID, row.AssignmentID, row.AssignmentRevision,
		row.AccountSourceGeneration, row.ErrorCode, row.PayloadFingerprint,
		row.AttemptCount, row.NextRetryAt.UTC(), row.QuarantinedAt.UTC(),
	)
}

// PersistCorruptQuarantine marks work quarantined and upserts durable quarantine
// in a dedicated transaction after a projection rollback.
func PersistCorruptQuarantine(
	ctx context.Context,
	scope store.AccountScope,
	row QuarantineRecord,
	generation int64,
) error {
	return scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if _, err := tx.PlanningReminderFence().LockCurrentAccount(ctx); err != nil {
			return err
		}
		if err := UpsertQuarantineInScope(ctx, tx, row); err != nil {
			return err
		}
		return tx.PlanningGenerationWorkReader().MarkQuarantined(ctx, generation)
	})
}

// ResolveQuarantineInScope marks quarantine resolved after successful repair.
func ResolveQuarantineInScope(ctx context.Context, tx store.TxAccountScope, sourceEventID, resolutionKind string) error {
	n, err := tx.Update(ctx, "plan_assignment_reminder_quarantines",
		"resolved_at = $2, resolution_kind = $3, lease_owner = NULL, lease_until = NULL",
		"source_event_id = $4 AND resolved_at IS NULL",
		time.Now().UTC(), resolutionKind, sourceEventID)
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("unresolved quarantine not found")
	}
	return nil
}

// HasUnresolvedQuarantineInScope reports blocking quarantine for freshness.
func HasUnresolvedQuarantineInScope(ctx context.Context, tx store.TxAccountScope) (bool, error) {
	n, err := tx.Count(ctx, "plan_assignment_reminder_quarantines", "resolved_at IS NULL")
	return n > 0, err
}

func loadUnresolvedQuarantine(ctx context.Context, tx store.TxAccountScope, sourceEventID string) (QuarantineRecord, error) {
	row := tx.QueryRow(ctx, "plan_assignment_reminder_quarantines",
		"source_event_id, plan_id, assignment_id, assignment_revision, account_source_generation, error_code, payload_fingerprint, attempt_count, next_retry_at, lease_owner, lease_until, quarantined_at, resolved_at, resolution_kind",
		"source_event_id = $2", sourceEventID)
	var (
		q          QuarantineRecord
		leaseOwner sql.NullString
		leaseUntil sql.NullTime
		resolvedAt sql.NullTime
		resKind    sql.NullString
	)
	err := row.Scan(
		&q.SourceEventID, &q.PlanID, &q.AssignmentID, &q.AssignmentRevision, &q.AccountSourceGeneration,
		&q.ErrorCode, &q.PayloadFingerprint, &q.AttemptCount, &q.NextRetryAt, &leaseOwner, &leaseUntil,
		&q.QuarantinedAt, &resolvedAt, &resKind,
	)
	if err != nil {
		return QuarantineRecord{}, err
	}
	q.NextRetryAt = q.NextRetryAt.UTC()
	q.QuarantinedAt = q.QuarantinedAt.UTC()
	if leaseOwner.Valid {
		value := leaseOwner.String
		q.LeaseOwner = &value
	}
	if leaseUntil.Valid {
		value := leaseUntil.Time.UTC()
		q.LeaseUntil = &value
	}
	if resolvedAt.Valid {
		value := resolvedAt.Time.UTC()
		q.ResolvedAt = &value
	}
	if resKind.Valid {
		value := resKind.String
		q.ResolutionKind = &value
	}
	return q, nil
}

func quarantineBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	// 30s, 60s, 120s ... capped at 15m
	d := time.Duration(30*(1<<(attempt-1))) * time.Second
	if d > 15*time.Minute {
		return 15 * time.Minute
	}
	return d
}

// LoadQuarantineInScope loads quarantine by source event (resolved or not).
func LoadQuarantineInScope(ctx context.Context, tx store.TxAccountScope, sourceEventID string) (QuarantineRecord, error) {
	q, err := loadUnresolvedQuarantine(ctx, tx, sourceEventID)
	if err != nil {
		return QuarantineRecord{}, err
	}
	return q, nil
}
