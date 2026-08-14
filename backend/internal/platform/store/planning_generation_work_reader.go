package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"

	"github.com/samson/customer-manage-platform/backend/internal/shootplanning/planningreminder"
)

// PlanningGenerationWorkReader returns the core-owned claim/list port bound to
// this physical account transaction.
func (sc TxAccountScope) PlanningGenerationWorkReader() planningreminder.PlanningGenerationWorkReader {
	return planningreminder.BindTrustedWorkReader(planningreminder.TrustedWorkOperations{
		ListPlanIDs: func(ctx context.Context, targetGeneration int64) ([]string, error) {
			if targetGeneration < 0 {
				return nil, errors.New("target generation must be non-negative")
			}
			rows, err := sc.Query(ctx, "planning_reminder_generation_work",
				"plan_id", "generation <= $2", targetGeneration)
			if err != nil {
				return nil, fmt.Errorf("list plan ids through target: %w", err)
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
		},
		ClaimPending: func(ctx context.Context, targetGeneration int64, limit int) ([]planningreminder.GenerationWork, error) {
			if targetGeneration < 1 {
				return nil, nil
			}
			if limit < 1 {
				return nil, errors.New("claim limit must be positive")
			}
			if sc.AccountID() == "" {
				return nil, ErrEmptyAccountScope
			}
			sqlText := `
				WITH claimed AS (
					SELECT generation
					FROM planning_reminder_generation_work
					WHERE account_id = $1
					  AND state = 'pending'
					  AND generation <= $2
					ORDER BY generation
					LIMIT $3
					FOR UPDATE SKIP LOCKED
				)
				SELECT w.generation, w.plan_id, w.mutation_kind, w.source_event_id, w.state, w.created_at, w.applied_at
				FROM planning_reminder_generation_work w
				INNER JOIN claimed c ON c.generation = w.generation
				WHERE w.account_id = $1
				ORDER BY w.generation`
			rows, err := sc.scope.execRunner().Query(ctx, sqlText, sc.AccountID(), targetGeneration, limit)
			if err != nil {
				return nil, fmt.Errorf("claim pending planning reminder work: %w", err)
			}
			defer rows.Close()
			out := make([]planningreminder.GenerationWork, 0, limit)
			for rows.Next() {
				work, err := scanGenerationWork(rows)
				if err != nil {
					return nil, err
				}
				out = append(out, work)
			}
			return out, rows.Err()
		},
		MarkQuarantined: func(ctx context.Context, generation int64) error {
			if generation < 1 {
				return errors.New("generation must be positive")
			}
			updated, err := sc.Update(ctx, "planning_reminder_generation_work",
				"state = $2",
				"generation = $3 AND state = $4",
				string(planningreminder.WorkQuarantined), generation, string(planningreminder.WorkPending))
			if err != nil {
				return fmt.Errorf("mark generation work quarantined: %w", err)
			}
			if updated != 1 {
				return errors.New("planning reminder generation is missing or not pending")
			}
			return nil
		},
		LoadWork: func(ctx context.Context, generation int64) (planningreminder.GenerationWork, error) {
			row := sc.QueryRow(ctx, "planning_reminder_generation_work",
				"generation, plan_id, mutation_kind, source_event_id, state, created_at, applied_at",
				"generation = $2", generation)
			return scanGenerationWork(row)
		},
	})
}

func scanGenerationWork(row interface{ Scan(...any) error }) (planningreminder.GenerationWork, error) {
	var (
		work      planningreminder.GenerationWork
		kind      string
		state     string
		source    sql.NullString
		appliedAt sql.NullTime
	)
	if err := row.Scan(&work.Generation, &work.PlanID, &kind, &source, &state, &work.CreatedAt, &appliedAt); err != nil {
		if errors.Is(err, ErrNoRows) {
			return planningreminder.GenerationWork{}, err
		}
		return planningreminder.GenerationWork{}, fmt.Errorf("scan generation work: %w", err)
	}
	work.MutationKind = planningreminder.MutationKind(kind)
	work.State = planningreminder.GenerationWorkState(state)
	if source.Valid {
		value := source.String
		work.SourceEventID = &value
	}
	if appliedAt.Valid {
		value := appliedAt.Time.UTC()
		work.AppliedAt = &value
	}
	work.CreatedAt = work.CreatedAt.UTC()
	return work, nil
}

// AdvanceAppliedWatermarkIfContiguous advances applied_generation only while
// every smaller generation is applied AND has a matching resolution row.
func (sc TxAccountScope) AdvanceAppliedWatermarkIfContiguous(ctx context.Context) (int64, error) {
	var applied, target int64
	if err := sc.QueryRow(ctx, "planning_reminder_account_generations",
		"applied_generation, target_generation", "TRUE").Scan(&applied, &target); err != nil {
		return 0, fmt.Errorf("load planning reminder watermark: %w", err)
	}
	for applied < target {
		next := applied + 1
		var state string
		err := sc.QueryRow(ctx, "planning_reminder_generation_work", "state", "generation = $2", next).Scan(&state)
		if err != nil {
			return 0, fmt.Errorf("load next planning reminder work: %w", err)
		}
		if state != string(planningreminder.WorkApplied) {
			break
		}
		exists, err := sc.Exists(ctx, "planning_reminder_generation_resolutions", "generation = $2", next)
		if err != nil {
			return 0, fmt.Errorf("load next planning reminder resolution: %w", err)
		}
		if !exists {
			break
		}
		applied = next
	}
	if _, err := sc.Update(ctx, "planning_reminder_account_generations",
		"applied_generation = $2, updated_at = clock_timestamp()", "TRUE", applied); err != nil {
		return 0, fmt.Errorf("advance planning reminder applied watermark: %w", err)
	}
	return applied, nil
}

// CaptureFenceTargetGeneration returns the locked fence target (caller must lock first).
func (sc TxAccountScope) CaptureFenceTargetGeneration(ctx context.Context) (int64, error) {
	var target int64
	if err := sc.QueryRow(ctx, "planning_reminder_account_generations", "target_generation", "TRUE").Scan(&target); err != nil {
		return 0, fmt.Errorf("capture planning reminder target generation: %w", err)
	}
	return target, nil
}
