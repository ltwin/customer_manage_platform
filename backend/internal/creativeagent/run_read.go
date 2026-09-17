package creativeagent

import (
	"context"
	"errors"

	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

type RunPage struct {
	Items []Run `json:"items"`
}
type StepSummary struct {
	ID            string  `json:"id"`
	Kind          string  `json:"kind"`
	State         string  `json:"state"`
	ToolKey       *string `json:"tool_key,omitempty"`
	ErrorCode     *string `json:"error_code,omitempty"`
	RetryOfStepID *string `json:"retry_of_step_id,omitempty"`
}

func (s *Service) ListRuns(ctx context.Context, scope store.AccountScope, conversationID string) (RunPage, error) {
	page := RunPage{Items: []Run{}}
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.RequireCreativeCapability(ctx, "creative_read"); err != nil {
			return err
		}
		exists, err := tx.Exists(ctx, "creative_agent_conversations", "id=$2", conversationID)
		if err != nil {
			return err
		}
		if !exists {
			return ErrNotFound
		}
		rows, err := tx.QueryPage(ctx, "creative_agent_runs", runColumns, "conversation_id=$2", []store.OrderBy{{Column: "created_at", Desc: true}, {Column: "id", Desc: true}}, 30, 0, conversationID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			run, err := scanRun(rows)
			if err != nil {
				return err
			}
			page.Items = append(page.Items, run)
		}
		return rows.Err()
	})
	return page, err
}
func (s *Service) ReadSteps(ctx context.Context, scope store.AccountScope, runID string) ([]StepSummary, error) {
	if runID == "" {
		return nil, creativeops.ErrValidation
	}
	steps := []StepSummary{}
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.RequireCreativeCapability(ctx, "creative_read"); err != nil {
			return err
		}
		exists, err := tx.Exists(ctx, "creative_agent_runs", "id=$2", runID)
		if err != nil {
			return err
		}
		if !exists {
			return ErrNotFound
		}
		rows, err := tx.QueryPage(ctx, "creative_agent_steps", "id,kind,state,tool_key,error_code,retry_of_step_id", "run_id=$2", []store.OrderBy{{Column: "ordinal"}}, 100, 0, runID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var step StepSummary
			if err = rows.Scan(&step.ID, &step.Kind, &step.State, &step.ToolKey, &step.ErrorCode, &step.RetryOfStepID); err != nil {
				return err
			}
			steps = append(steps, step)
		}
		if err = rows.Err(); err != nil && !errors.Is(err, store.ErrNoRows) {
			return err
		}
		return nil
	})
	return steps, err
}
