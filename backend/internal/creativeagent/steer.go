package creativeagent

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// SteerRun puts a photographer's message into a run that is still going. The
// run keeps its state; the message reaches the model before its next request,
// or, if the final answer was being written, the run answers it once more
// before it ends. Accepted is not delivered: the input block says which.
func (s *Service) SteerRun(ctx context.Context, scope store.AccountScope, command creativeops.Command) (creativeops.Receipt, error) {
	var in struct {
		RunID    string               `json:"run_id"`
		Segments []InstructionSegment `json:"instruction_segments"`
	}
	var resolved ResolvedInstruction
	var resolveErr error
	if err := creativeops.Decode(command.Payload, &in); err == nil {
		resolved, resolveErr = s.resolveInput(ctx, scope, "", in.Segments)
	}
	return (creativeops.Executor{}).Run(ctx, scope, creativeops.Operation{Key: "agent.steer_run", Capability: "agent_start", Validate: func(raw json.RawMessage) error {
		if err := creativeops.Decode(raw, &in); err != nil {
			return err
		}
		if in.RunID == "" || len(in.Segments) == 0 {
			return creativeops.ErrValidation
		}
		return resolveErr
	}, Apply: func(ctx context.Context, tx store.TxAccountScope, _ json.RawMessage) (creativeops.Outcome, error) {
		var canvasID string
		if err := tx.QueryRow(ctx, "creative_agent_runs", "canvas_id", "id=$2", in.RunID).Scan(&canvasID); err != nil {
			if errors.Is(err, store.ErrNoRows) {
				return creativeops.Outcome{}, ErrNotFound
			}
			return creativeops.Outcome{}, err
		}
		if err := creativecanvas.LockProjectForWorkInTx(ctx, tx, canvasID); err != nil {
			return creativeops.Outcome{}, err
		}
		// The run row is also what recording a model result locks, so a steer
		// and the final answer are ordered: one is before the other.
		c, err := s.lockControlledRun(ctx, tx, in.RunID)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		switch c.State {
		case RunQueued, RunRunning, RunWaitingClient:
		default:
			return creativeops.Outcome{}, ErrRunState
		}
		// A compaction asks the model about the history, never with the
		// run's inputs: a steer there would be accepted and never read.
		if c.cancelled != nil || c.Kind == runKindCompact {
			return creativeops.Outcome{}, ErrRunState
		}
		now, err := tx.CreativeNow(ctx)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		if !now.Before(c.DeadlineAt) {
			return creativeops.Outcome{}, creativeops.ErrExpired
		}
		final, err := finalModelStepInTx(ctx, tx, c.ID)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		if final {
			// The answer is recorded; the run is closing. Refused here, the
			// message becomes the next turn instead of going unread.
			return creativeops.Outcome{}, ErrRunState
		}
		consent, err := effectiveConsentInTx(ctx, tx, c.EgressConsentID)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		if err = s.appendInputBatchInTx(ctx, tx, c, resolved, consent); err != nil {
			return creativeops.Outcome{}, err
		}
		run, err := scanRun(tx.QueryRow(ctx, "creative_agent_runs", runColumns, "id=$2", c.ID))
		if err != nil {
			return creativeops.Outcome{}, err
		}
		return runOutcome(run, 202)
	}}, command)
}

// finalModelStepInTx reports whether the run's latest model step will be its
// last: it is no longer prepared and asked for no tools. A failed or unknown
// step has no tool calls either. A run without a model step has not begun.
func finalModelStepInTx(ctx context.Context, tx store.TxAccountScope, runID string) (bool, error) {
	rows, err := tx.QueryPage(ctx, "creative_agent_steps", "state,output", "run_id=$2 AND kind='model'", []store.OrderBy{{Column: "ordinal", Desc: true}}, 1, 0, runID)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	if !rows.Next() {
		return false, rows.Err()
	}
	var state string
	var output []byte
	if err := rows.Scan(&state, &output); err != nil {
		return false, err
	}
	if state == "prepared" {
		return false, nil
	}
	var result struct {
		ToolCalls []json.RawMessage `json:"tool_calls"`
	}
	if len(output) > 0 {
		if err := json.Unmarshal(output, &result); err != nil {
			return false, err
		}
	}
	return len(result.ToolCalls) == 0, nil
}
