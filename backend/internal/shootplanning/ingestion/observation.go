package ingestion

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning"
)

type ObservationOutcome string

const (
	ObservationInProgress ObservationOutcome = "in_progress"
	ObservationFirstReady ObservationOutcome = "first_ready"
	ObservationAbandoned  ObservationOutcome = "abandoned"
)

type PlanBuildObservation struct {
	PlanID                     string             `json:"plan_id"`
	FirstIngestionAt           time.Time          `json:"first_ingestion_at"`
	FirstReadyAt               *time.Time         `json:"first_ready_at,omitempty"`
	LastActivityAt             time.Time          `json:"last_activity_at"`
	ActiveSeconds              int64              `json:"active_seconds"`
	IdleRuleVersion            int                `json:"idle_rule_version"`
	Outcome                    ObservationOutcome `json:"outcome"`
	TerminalAt                 *time.Time         `json:"terminal_at,omitempty"`
	PostTerminalIngestionCount int64              `json:"post_terminal_ingestion_count"`
	Revision                   int64              `json:"revision"`
}
type ActivityFact struct {
	PlanID    string
	SessionID string
	TickID    string
	Kind      string
}

func (r Repository) RecordBuildActivityInScope(ctx context.Context, tx store.TxAccountScope, fact ActivityFact) (PlanBuildObservation, bool, error) {
	now := r.now().UTC()
	var inserted string
	err := tx.InsertOnConflictDoNothingReturning(ctx, "shoot_plan_build_activity_ticks", []string{"plan_id", "session_id", "tick_id", "kind", "server_received_at", "accepted_delta_seconds"}, []string{"account_id", "plan_id", "tick_id"}, []string{"tick_id"}, fact.PlanID, fact.SessionID, fact.TickID, fact.Kind, now, 0).Scan(&inserted)
	if errors.Is(err, store.ErrNoRows) {
		observation, loadErr := r.GetObservationInScope(ctx, tx, fact.PlanID)
		return observation, false, loadErr
	}
	if err != nil {
		return PlanBuildObservation{}, false, fmt.Errorf("insert activity tick: %w", err)
	}
	observation, err := r.lockOrCreateObservation(ctx, tx, fact.PlanID, now)
	if err != nil {
		return PlanBuildObservation{}, false, err
	}
	if observation.Outcome != ObservationInProgress {
		updated, err := tx.Update(ctx, "shoot_plan_build_observations", "post_terminal_ingestion_count = post_terminal_ingestion_count + 1, revision = revision + 1", "plan_id = $2", fact.PlanID)
		if err != nil {
			return PlanBuildObservation{}, false, err
		}
		if updated != 1 {
			return PlanBuildObservation{}, false, ErrPlanNotFound
		}
		observation, err = r.GetObservationInScope(ctx, tx, fact.PlanID)
		return observation, true, err
	}
	delta := acceptedActivityDelta(observation.LastActivityAt, now)
	updated, err := tx.Update(ctx, "shoot_plan_build_observations", "last_activity_at = $2, active_seconds = active_seconds + $3, revision = revision + 1", "plan_id = $4 AND revision = $5", now, delta, fact.PlanID, observation.Revision)
	if err != nil {
		return PlanBuildObservation{}, false, err
	}
	if updated != 1 {
		return PlanBuildObservation{}, false, ErrSessionRevision
	}
	if _, err := tx.Update(ctx, "shoot_plan_build_activity_ticks", "accepted_delta_seconds = $2", "plan_id = $3 AND tick_id = $4", delta, fact.PlanID, fact.TickID); err != nil {
		return PlanBuildObservation{}, false, err
	}
	observation, err = r.GetObservationInScope(ctx, tx, fact.PlanID)
	return observation, true, err
}

func (r Repository) AccumulateAndRecordFirstReadyInScope(ctx context.Context, tx store.TxAccountScope, fact ActivityFact) (PlanBuildObservation, bool, error) {
	// Core may become ready without ever using ingestion.  In that case the
	// observation seam is deliberately a 0-row no-op and must not open a new
	// measurement window after the terminal product event.
	if _, err := r.GetObservationInScope(ctx, tx, fact.PlanID); errors.Is(err, ErrPlanNotFound) {
		return PlanBuildObservation{}, false, nil
	} else if err != nil {
		return PlanBuildObservation{}, false, err
	}
	fact.Kind = "ready"
	observation, accepted, err := r.RecordBuildActivityInScope(ctx, tx, fact)
	if err != nil || !accepted || observation.Outcome != ObservationInProgress {
		return observation, accepted, err
	}
	now := r.now().UTC()
	updated, err := tx.Update(ctx, "shoot_plan_build_observations", "outcome = 'first_ready', first_ready_at = $2, terminal_at = $2, revision = revision + 1", "plan_id = $3 AND outcome = 'in_progress' AND revision = $4", now, fact.PlanID, observation.Revision)
	if err != nil {
		return PlanBuildObservation{}, false, err
	}
	if updated != 1 {
		current, loadErr := r.GetObservationInScope(ctx, tx, fact.PlanID)
		return current, false, loadErr
	}
	observation, err = r.GetObservationInScope(ctx, tx, fact.PlanID)
	return observation, true, err
}

// PlanReadyObservationAdapter translates the core plan-ready fact into the
// ingestion-owned accumulator without exposing ingestion internals to the
// parent shootplanning package.
type PlanReadyObservationAdapter struct {
	repo Repository
}

func NewPlanReadyObservationAdapter(repo Repository) PlanReadyObservationAdapter {
	return PlanReadyObservationAdapter{repo: repo}
}

func (a PlanReadyObservationAdapter) AccumulateAndRecordFirstReadyInScope(
	ctx context.Context,
	tx store.TxAccountScope,
	fact shootplanning.PlanReadyObservationFact,
) error {
	if _, err := a.repo.GetObservationInScope(ctx, tx, fact.PlanID); errors.Is(err, ErrPlanNotFound) {
		return nil
	} else if err != nil {
		return err
	}
	sessionID, found, err := latestObservationSessionID(ctx, tx, fact.PlanID)
	if err != nil || !found {
		return err
	}
	_, _, err = a.repo.AccumulateAndRecordFirstReadyInScope(ctx, tx, ActivityFact{
		PlanID:    fact.PlanID,
		SessionID: sessionID,
		TickID:    fact.TickID,
	})
	return err
}

func latestObservationSessionID(ctx context.Context, tx store.TxAccountScope, planID string) (string, bool, error) {
	rows, err := tx.QueryPage(ctx, "shoot_plan_ingestion_sessions", "id", "plan_id = $2",
		[]store.OrderBy{{Column: "updated_at", Desc: true}, {Column: "id", Desc: true}}, 1, 0, planID)
	if err != nil {
		return "", false, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return "", false, err
		}
		return "", false, nil
	}
	var sessionID string
	if err := rows.Scan(&sessionID); err != nil {
		return "", false, err
	}
	return sessionID, true, nil
}
func (r Repository) AccumulateAndAbandonInScope(ctx context.Context, tx store.TxAccountScope, fact ActivityFact) (PlanBuildObservation, bool, error) {
	fact.Kind = "abandon"
	observation, accepted, err := r.RecordBuildActivityInScope(ctx, tx, fact)
	if err != nil || !accepted || observation.Outcome != ObservationInProgress {
		return observation, accepted, err
	}
	now := r.now().UTC()
	updated, err := tx.Update(ctx, "shoot_plan_build_observations", "outcome = 'abandoned', terminal_at = $2, revision = revision + 1", "plan_id = $3 AND outcome = 'in_progress' AND revision = $4", now, fact.PlanID, observation.Revision)
	if err != nil {
		return PlanBuildObservation{}, false, err
	}
	if updated != 1 {
		current, loadErr := r.GetObservationInScope(ctx, tx, fact.PlanID)
		return current, false, loadErr
	}
	observation, err = r.GetObservationInScope(ctx, tx, fact.PlanID)
	return observation, true, err
}
func (r Repository) GetObservationInScope(ctx context.Context, tx store.TxAccountScope, planID string) (PlanBuildObservation, error) {
	return scanObservation(tx.QueryRow(ctx, "shoot_plan_build_observations", observationColumns, "plan_id = $2", planID))
}
func (r Repository) lockOrCreateObservation(ctx context.Context, tx store.TxAccountScope, planID string, now time.Time) (PlanBuildObservation, error) {
	observation, err := scanObservation(tx.QueryRowForUpdate(ctx, "shoot_plan_build_observations", observationColumns, "plan_id = $2", planID))
	if err == nil {
		return observation, nil
	}
	if !errors.Is(err, ErrPlanNotFound) {
		return PlanBuildObservation{}, err
	}
	var inserted string
	err = tx.InsertOnConflictDoNothingReturning(ctx, "shoot_plan_build_observations", []string{"plan_id", "first_ingestion_at", "last_activity_at", "active_seconds", "idle_rule_version", "outcome", "post_terminal_ingestion_count", "revision"}, []string{"account_id", "plan_id"}, []string{"plan_id"}, planID, now, now, int64(0), 1, string(ObservationInProgress), int64(0), int64(1)).Scan(&inserted)
	if err != nil && !errors.Is(err, store.ErrNoRows) {
		return PlanBuildObservation{}, err
	}
	return scanObservation(tx.QueryRowForUpdate(ctx, "shoot_plan_build_observations", observationColumns, "plan_id = $2", planID))
}

const observationColumns = "plan_id,first_ingestion_at,first_ready_at,last_activity_at,active_seconds,idle_rule_version,outcome,terminal_at,post_terminal_ingestion_count,revision"

func scanObservation(row rowScanner) (PlanBuildObservation, error) {
	var o PlanBuildObservation
	err := row.Scan(&o.PlanID, &o.FirstIngestionAt, &o.FirstReadyAt, &o.LastActivityAt, &o.ActiveSeconds, &o.IdleRuleVersion, &o.Outcome, &o.TerminalAt, &o.PostTerminalIngestionCount, &o.Revision)
	if errors.Is(err, store.ErrNoRows) {
		return PlanBuildObservation{}, ErrPlanNotFound
	}
	return o, err
}
func acceptedActivityDelta(last, now time.Time) int {
	if !now.After(last) {
		return 0
	}
	seconds := int(now.Sub(last) / time.Second)
	if seconds > 300 {
		return 300
	}
	return seconds
}
