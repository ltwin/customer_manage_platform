package planshare

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// AssignmentSourceRef identifies one assignment within a plan.
type AssignmentSourceRef struct {
	PlanID       string
	AssignmentID string
}

// AssignmentReminderSourceSnapshot is the safe current assignment projection for
// reminder consumers. It intentionally excludes receipt/token/feedback/secret.
type AssignmentReminderSourceSnapshot struct {
	PlanID                       string
	AssignmentID                 string
	AssignmentRevision           int64
	AssignmentKind               AssignmentKind
	Status                       AssignmentStatus
	ReadinessItemID              *string
	ContentSnapshot              string
	ClaimedByDisplayNameSnapshot string
	ContentFingerprint           string
	PreparationLeadDaysSnapshot  *int
	LeadRuleVersion              *string
	ClaimedAt                    time.Time
	RevokedAt                    *time.Time
}

// AssignmentReminderSourceEvent is the immutable outbox envelope (no body).
type AssignmentReminderSourceEvent struct {
	EventID                     string
	PlanID                      string
	AssignmentID                string
	AssignmentRevision          int64
	AccountSourceGeneration     int64
	EventKind                   string
	AssignmentKind              AssignmentKind
	ReadinessItemID             *string
	PreparationLeadDaysSnapshot *int
	LeadRuleVersion             *string
	ContentFingerprint          string
	OccurredAt                  time.Time
}

// AssignmentReminderSourceReader is the planshare-owned safe source port.
// Implementations must not claim core generation work or write reminder epochs.
type AssignmentReminderSourceReader interface {
	LoadCurrentAssignmentSourceInScope(ctx context.Context, tx store.TxAccountScope, ref AssignmentSourceRef) (AssignmentReminderSourceSnapshot, error)
	ListAssignmentSourcesForPlanInScope(ctx context.Context, tx store.TxAccountScope, planID string) ([]AssignmentReminderSourceSnapshot, error)
	ListRetainedPlanIDsInScope(ctx context.Context, tx store.TxAccountScope) ([]string, error)
	LoadSourceEventInScope(ctx context.Context, tx store.TxAccountScope, eventID string) (AssignmentReminderSourceEvent, error)
	assignmentReminderSourceReaderSeal()
}

type assignmentReminderSourceReader struct{}

// NewAssignmentReminderSourceReader returns the planshare-owned safe reader.
func NewAssignmentReminderSourceReader() AssignmentReminderSourceReader {
	return assignmentReminderSourceReader{}
}

func (assignmentReminderSourceReader) assignmentReminderSourceReaderSeal() {}

func (assignmentReminderSourceReader) LoadCurrentAssignmentSourceInScope(
	ctx context.Context,
	tx store.TxAccountScope,
	ref AssignmentSourceRef,
) (AssignmentReminderSourceSnapshot, error) {
	if ref.PlanID == "" || ref.AssignmentID == "" {
		return AssignmentReminderSourceSnapshot{}, errors.New("assignment source ref incomplete")
	}
	row := tx.QueryRow(ctx, "share_assignments",
		"id, plan_id, assignment_kind, readiness_item_id, content_snapshot, claimed_by_display_name, preparation_lead_days_snapshot, lead_rule_version, status, revision, claimed_at, revoked_at",
		"plan_id = $2 AND id = $3", ref.PlanID, ref.AssignmentID)
	snap, err := scanAssignmentReminderSnapshot(row)
	if errors.Is(err, store.ErrNoRows) {
		return AssignmentReminderSourceSnapshot{}, ErrAssignmentNotFound
	}
	return snap, err
}

func (assignmentReminderSourceReader) ListAssignmentSourcesForPlanInScope(
	ctx context.Context,
	tx store.TxAccountScope,
	planID string,
) ([]AssignmentReminderSourceSnapshot, error) {
	if planID == "" {
		return nil, errors.New("plan id required")
	}
	rows, err := tx.Query(ctx, "share_assignments",
		"id, plan_id, assignment_kind, readiness_item_id, content_snapshot, claimed_by_display_name, preparation_lead_days_snapshot, lead_rule_version, status, revision, claimed_at, revoked_at",
		"plan_id = $2", planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]AssignmentReminderSourceSnapshot, 0)
	for rows.Next() {
		snap, err := scanAssignmentReminderSnapshot(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, snap)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AssignmentID < out[j].AssignmentID })
	return out, nil
}

func (assignmentReminderSourceReader) ListRetainedPlanIDsInScope(
	ctx context.Context,
	tx store.TxAccountScope,
) ([]string, error) {
	rows, err := tx.Query(ctx, "share_assignments", "plan_id", "TRUE")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[string]struct{}{}
	out := make([]string, 0)
	for rows.Next() {
		var planID string
		if err := rows.Scan(&planID); err != nil {
			return nil, err
		}
		if _, ok := seen[planID]; ok {
			continue
		}
		seen[planID] = struct{}{}
		out = append(out, planID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

func (assignmentReminderSourceReader) LoadSourceEventInScope(
	ctx context.Context,
	tx store.TxAccountScope,
	eventID string,
) (AssignmentReminderSourceEvent, error) {
	if eventID == "" {
		return AssignmentReminderSourceEvent{}, errors.New("source event id required")
	}
	row := tx.QueryRow(ctx, "share_assignment_source_event_v1",
		"event_id, plan_id, assignment_id, assignment_revision, account_source_generation, event_kind, assignment_kind, readiness_item_id, preparation_lead_days_snapshot, lead_rule_version, content_fingerprint, occurred_at",
		"event_id = $2", eventID)
	var (
		ev       AssignmentReminderSourceEvent
		asgnKind string
		ready    sql.NullString
		leadDays sql.NullInt64
		leadRule sql.NullString
		occurred time.Time
	)
	err := row.Scan(
		&ev.EventID, &ev.PlanID, &ev.AssignmentID, &ev.AssignmentRevision, &ev.AccountSourceGeneration,
		&ev.EventKind, &asgnKind, &ready, &leadDays, &leadRule, &ev.ContentFingerprint, &occurred,
	)
	if errors.Is(err, store.ErrNoRows) {
		return AssignmentReminderSourceEvent{}, ErrNotFound
	}
	if err != nil {
		return AssignmentReminderSourceEvent{}, fmt.Errorf("load assignment source event: %w", err)
	}
	ev.AssignmentKind = AssignmentKind(asgnKind)
	ev.OccurredAt = occurred.UTC()
	if ready.Valid {
		value := ready.String
		ev.ReadinessItemID = &value
	}
	if leadDays.Valid {
		value := int(leadDays.Int64)
		ev.PreparationLeadDaysSnapshot = &value
	}
	if leadRule.Valid {
		value := leadRule.String
		ev.LeadRuleVersion = &value
	}
	return ev, nil
}

func scanAssignmentReminderSnapshot(row interface{ Scan(...any) error }) (AssignmentReminderSourceSnapshot, error) {
	var (
		snap     AssignmentReminderSourceSnapshot
		kind     string
		status   string
		ready    sql.NullString
		leadDays sql.NullInt64
		leadRule sql.NullString
		revoked  sql.NullTime
	)
	err := row.Scan(
		&snap.AssignmentID, &snap.PlanID, &kind, &ready, &snap.ContentSnapshot, &snap.ClaimedByDisplayNameSnapshot,
		&leadDays, &leadRule, &status, &snap.AssignmentRevision, &snap.ClaimedAt, &revoked,
	)
	if err != nil {
		return AssignmentReminderSourceSnapshot{}, err
	}
	snap.AssignmentKind = AssignmentKind(kind)
	snap.Status = AssignmentStatus(status)
	snap.ContentFingerprint = contentFingerprint(snap.ContentSnapshot)
	snap.ClaimedAt = snap.ClaimedAt.UTC()
	if ready.Valid {
		value := ready.String
		snap.ReadinessItemID = &value
	}
	if leadDays.Valid {
		value := int(leadDays.Int64)
		snap.PreparationLeadDaysSnapshot = &value
	}
	if leadRule.Valid {
		value := leadRule.String
		snap.LeadRuleVersion = &value
	}
	if revoked.Valid {
		value := revoked.Time.UTC()
		snap.RevokedAt = &value
	}
	return snap, nil
}

var _ AssignmentReminderSourceReader = assignmentReminderSourceReader{}
