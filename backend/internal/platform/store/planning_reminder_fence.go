package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/samson/customer-manage-platform/backend/internal/shootplanning/planningreminder"
)

// PlanningReminderFence derives the only neutral fence view from this bound
// physical transaction. The returned value cannot expose SQL or account choice.
func (sc TxAccountScope) PlanningReminderFence() planningreminder.FenceTxView {
	return planningreminder.BindTrustedFenceTxView(planningreminder.TrustedFenceOperations{
		Lock: func(ctx context.Context) error {
			row := sc.InsertOnConflictDoNothingReturning(
				ctx,
				"planning_reminder_account_generations",
				[]string{"target_generation", "applied_generation"},
				[]string{"account_id"},
				[]string{"account_id"},
				int64(0), int64(0),
			)
			var accountID string
			if err := row.Scan(&accountID); err != nil && !errors.Is(err, ErrNoRows) {
				return fmt.Errorf("bootstrap planning reminder fence: %w", err)
			}
			var target, applied int64
			if err := sc.QueryRowForUpdate(ctx, "planning_reminder_account_generations",
				"target_generation, applied_generation", "TRUE").Scan(&target, &applied); err != nil {
				return fmt.Errorf("lock planning reminder fence: %w", err)
			}
			return nil
		},
		Reserve: func(ctx context.Context, fact planningreminder.MutationFact) (int64, error) {
			var current int64
			if err := sc.QueryRow(ctx, "planning_reminder_account_generations", "target_generation", "TRUE").Scan(&current); err != nil {
				return 0, fmt.Errorf("load planning reminder target generation: %w", err)
			}
			next := current + 1
			updated, err := sc.Update(ctx, "planning_reminder_account_generations",
				"target_generation = $2, updated_at = clock_timestamp()", "target_generation = $3", next, current)
			if err != nil {
				return 0, fmt.Errorf("advance planning reminder target generation: %w", err)
			}
			if updated != 1 {
				return 0, errors.New("planning reminder fence lost its transaction lock")
			}
			var source any
			if fact.SourceEventID != nil {
				source = *fact.SourceEventID
			}
			if err := sc.Insert(ctx, "planning_reminder_generation_work",
				[]string{"generation", "plan_id", "mutation_kind", "source_event_id"},
				next, fact.PlanID, string(fact.MutationKind), source); err != nil {
				return 0, fmt.Errorf("insert planning reminder generation work: %w", err)
			}
			return next, nil
		},
		Apply: func(ctx context.Context, generation int64) error {
			exists, err := sc.Exists(ctx, "planning_reminder_generation_resolutions", "generation = $2", generation)
			if err != nil {
				return fmt.Errorf("require planning reminder resolution before apply: %w", err)
			}
			if !exists {
				return errors.New("planning reminder generation missing matching resolution")
			}
			updated, err := sc.Update(ctx, "planning_reminder_generation_work",
				"state = $2, applied_at = clock_timestamp()",
				"generation = $3 AND state IN ($4, $5)",
				"applied", generation, "pending", "quarantined")
			if err != nil {
				return fmt.Errorf("mark planning reminder generation applied: %w", err)
			}
			if updated != 1 {
				return errors.New("planning reminder generation is missing or already terminal")
			}
			if _, err := sc.AdvanceAppliedWatermarkIfContiguous(ctx); err != nil {
				return err
			}
			return nil
		},
	})
}
