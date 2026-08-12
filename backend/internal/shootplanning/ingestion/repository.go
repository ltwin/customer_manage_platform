package ingestion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

var (
	ErrSessionNotFound      = errors.New("ingestion_session_not_found")
	ErrSessionRevision      = errors.New("ingestion_session_revision_conflict")
	ErrSessionTerminal      = errors.New("ingestion_session_terminal")
	ErrEditingSessionExists = errors.New("editing_ingestion_session_exists")
	ErrPlanNotFound         = errors.New("ingestion_plan_not_found")
	ErrPlanRevision         = errors.New("ingestion_plan_revision_conflict")
)

const RetentionRuleV1 = 30 * 24 * time.Hour

type SessionState string

const (
	SessionEditing   SessionState = "editing"
	SessionCommitted SessionState = "committed"
	SessionAbandoned SessionState = "abandoned"
)

type Session struct {
	ID                     string            `json:"id"`
	PlanID                 string            `json:"plan_id"`
	State                  SessionState      `json:"state"`
	Revision               int64             `json:"revision"`
	ParserVersion          int               `json:"parser_version"`
	SourceText             *string           `json:"source_text,omitempty"`
	SourceChecksum         string            `json:"source_checksum"`
	SourceLineCount        int               `json:"source_line_count"`
	CandidateSnapshot      CandidateSnapshot `json:"candidate_snapshot"`
	CandidateSchemaVersion int               `json:"candidate_schema_version"`
	CreatedAt              time.Time         `json:"created_at"`
	UpdatedAt              time.Time         `json:"updated_at"`
	CommittedAt            *time.Time        `json:"committed_at,omitempty"`
	AbandonedAt            *time.Time        `json:"abandoned_at,omitempty"`
	RedactedAt             *time.Time        `json:"redacted_at,omitempty"`
}

type CreateSessionInput struct {
	SessionID            string
	PlanID               string
	ExpectedPlanRevision int64
	SourceText           string
	Parsed               ParseOutput
}

type Repository struct{ now func() time.Time }
type RepositoryOption func(*Repository)

func WithClock(now func() time.Time) RepositoryOption {
	return func(r *Repository) {
		if now != nil {
			r.now = now
		}
	}
}
func NewRepository(options ...RepositoryOption) Repository {
	r := Repository{now: time.Now}
	for _, option := range options {
		if option != nil {
			option(&r)
		}
	}
	return r
}

func (r Repository) CreateSession(ctx context.Context, scope store.AccountScope, input CreateSessionInput) (Session, error) {
	var created Session
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		var innerErr error
		created, innerErr = r.CreateSessionInScope(ctx, tx, input)
		return innerErr
	})
	return created, err
}

func (r Repository) CreateSessionInScope(ctx context.Context, tx store.TxAccountScope, input CreateSessionInput) (Session, error) {
	input.PlanID = strings.TrimSpace(input.PlanID)
	if input.PlanID == "" || input.ExpectedPlanRevision < 1 || input.Parsed.ParserVersion != ParserVersionV1 {
		return Session{}, ErrReparseInput
	}
	snapshot := snapshotFromParse(input.Parsed)
	snapshotJSON, err := json.Marshal(snapshot)
	if err != nil {
		return Session{}, err
	}
	id := strings.TrimSpace(input.SessionID)
	if id == "" {
		id = "ing_" + uuid.NewString()
	}
	now := r.now().UTC()
	var revision int64
	var state string
	err = tx.QueryRowForUpdate(ctx, "shoot_plans", "revision,status", "id = $2", input.PlanID).Scan(&revision, &state)
	if errors.Is(err, store.ErrNoRows) {
		return Session{}, ErrPlanNotFound
	}
	if err != nil {
		return Session{}, err
	}
	if revision != input.ExpectedPlanRevision {
		return Session{}, ErrPlanRevision
	}
	if state == "archived" {
		return Session{}, ErrSessionTerminal
	}
	_, err = tx.InsertReturningID(ctx, "shoot_plan_ingestion_sessions", []string{"id", "plan_id", "state", "revision", "parser_version", "source_text", "source_checksum", "source_line_count", "candidate_snapshot_json", "candidate_schema_version", "created_at", "updated_at"}, id, input.PlanID, string(SessionEditing), int64(1), input.Parsed.ParserVersion, nullableSource(input.SourceText), input.Parsed.SourceChecksum, input.Parsed.SourceLineCount, snapshotJSON, 1, now, now)
	if isUniqueViolation(err) {
		return Session{}, ErrEditingSessionExists
	}
	if err != nil {
		return Session{}, fmt.Errorf("create ingestion session: %w", err)
	}
	return r.GetSessionInScope(ctx, tx, input.PlanID, id)
}

func (r Repository) GetSession(ctx context.Context, scope store.AccountScope, planID, sessionID string) (Session, error) {
	return scanSession(scope.QueryRow(ctx, "shoot_plan_ingestion_sessions", sessionColumns, "plan_id = $2 AND id = $3", strings.TrimSpace(planID), strings.TrimSpace(sessionID)))
}
func (r Repository) GetSessionInScope(ctx context.Context, tx store.TxAccountScope, planID, sessionID string) (Session, error) {
	return scanSession(tx.QueryRow(ctx, "shoot_plan_ingestion_sessions", sessionColumns, "plan_id = $2 AND id = $3", strings.TrimSpace(planID), strings.TrimSpace(sessionID)))
}
func (r Repository) LockSession(ctx context.Context, tx store.TxAccountScope, planID, sessionID string) (Session, error) {
	return scanSession(tx.QueryRowForUpdate(ctx, "shoot_plan_ingestion_sessions", sessionColumns, "plan_id = $2 AND id = $3", strings.TrimSpace(planID), strings.TrimSpace(sessionID)))
}

func (r Repository) ReplacePreviewInScope(ctx context.Context, tx store.TxAccountScope, current Session, sourceText string, parsed ParseOutput) (Session, error) {
	if current.State != SessionEditing {
		return Session{}, ErrSessionTerminal
	}
	snapshotJSON, err := json.Marshal(snapshotFromParse(parsed))
	if err != nil {
		return Session{}, err
	}
	now := r.now().UTC()
	updated, err := tx.Update(ctx, "shoot_plan_ingestion_sessions", "source_text = $2, source_checksum = $3, source_line_count = $4, candidate_snapshot_json = $5, revision = revision + 1, updated_at = $6", "plan_id = $7 AND id = $8 AND state = 'editing' AND revision = $9", nullableSource(sourceText), parsed.SourceChecksum, parsed.SourceLineCount, snapshotJSON, now, current.PlanID, current.ID, current.Revision)
	if err != nil {
		return Session{}, err
	}
	if updated != 1 {
		return Session{}, ErrSessionRevision
	}
	return r.GetSessionInScope(ctx, tx, current.PlanID, current.ID)
}

func (r Repository) TransitionInScope(ctx context.Context, tx store.TxAccountScope, current Session, next SessionState) (Session, error) {
	if current.State != SessionEditing {
		return Session{}, ErrSessionTerminal
	}
	if next != SessionCommitted && next != SessionAbandoned {
		return Session{}, ErrReparseInput
	}
	now := r.now().UTC()
	set := "state = $2, revision = revision + 1, updated_at = $3, committed_at = $3"
	if next == SessionAbandoned {
		set = "state = $2, revision = revision + 1, updated_at = $3, abandoned_at = $3"
	}
	updated, err := tx.Update(ctx, "shoot_plan_ingestion_sessions", set, "plan_id = $4 AND id = $5 AND state = 'editing' AND revision = $6", string(next), now, current.PlanID, current.ID, current.Revision)
	if err != nil {
		return Session{}, err
	}
	if updated != 1 {
		return Session{}, ErrSessionRevision
	}
	return r.GetSessionInScope(ctx, tx, current.PlanID, current.ID)
}

func (r Repository) RedactTerminalSourceInScope(ctx context.Context, tx store.TxAccountScope, planID, sessionID string, expectedRevision int64) (Session, error) {
	now := r.now().UTC()
	updated, err := tx.Update(ctx, "shoot_plan_ingestion_sessions", "source_text = NULL, redacted_at = $2, revision = revision + 1, updated_at = $2", "plan_id = $3 AND id = $4 AND state <> 'editing' AND redacted_at IS NULL AND updated_at <= $5 AND revision = $6", now, planID, sessionID, now.Add(-RetentionRuleV1), expectedRevision)
	if err != nil {
		return Session{}, err
	}
	if updated != 1 {
		return Session{}, ErrSessionRevision
	}
	return r.GetSessionInScope(ctx, tx, planID, sessionID)
}

const sessionColumns = "id,plan_id,state,revision,parser_version,source_text,source_checksum,source_line_count,candidate_snapshot_json,candidate_schema_version,created_at,updated_at,committed_at,abandoned_at,redacted_at"

type rowScanner interface{ Scan(...any) error }

func scanSession(row rowScanner) (Session, error) {
	var s Session
	var snapshot []byte
	err := row.Scan(&s.ID, &s.PlanID, &s.State, &s.Revision, &s.ParserVersion, &s.SourceText, &s.SourceChecksum, &s.SourceLineCount, &snapshot, &s.CandidateSchemaVersion, &s.CreatedAt, &s.UpdatedAt, &s.CommittedAt, &s.AbandonedAt, &s.RedactedAt)
	if errors.Is(err, store.ErrNoRows) {
		return Session{}, ErrSessionNotFound
	}
	if err != nil {
		return Session{}, err
	}
	if err := json.Unmarshal(snapshot, &s.CandidateSnapshot); err != nil {
		return Session{}, fmt.Errorf("decode ingestion candidate snapshot: %w", err)
	}
	normalizeCandidateSnapshot(&s.CandidateSnapshot)
	return s, nil
}
func nullableSource(source string) any {
	if source == "" {
		return nil
	}
	return source
}
func isUniqueViolation(err error) bool {
	return store.IsUniqueViolation(err, "")
}
