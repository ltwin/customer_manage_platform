package creativeagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/jobs"
	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

type controlledRun struct {
	Run
	token     string
	epoch     int64
	lease     *time.Time
	cancelled *time.Time
	requests  []llmgateway.RequestView
}

func terminalRun(state string) bool {
	return state == RunSucceeded || state == RunPartial || state == RunFailed || state == RunCancelled
}

// Slot -> Gateway group/budgets/requests -> run. Dispatch uses request -> run,
// so cancellation can neither miss a committed dispatch nor deadlock behind it.
func (s *Service) lockControlledRun(ctx context.Context, tx store.TxAccountScope, id string) (controlledRun, error) {
	var c controlledRun
	var slot *string
	if err := tx.QueryRowForUpdate(ctx, "creative_agent_slots", "run_id", "").Scan(&slot); err != nil {
		if errors.Is(err, store.ErrNoRows) {
			return c, ErrNotFound
		}
		return c, err
	}
	exists, err := tx.Exists(ctx, "creative_agent_runs", "id=$2", id)
	if err != nil {
		return c, err
	}
	if !exists {
		return c, ErrNotFound
	}
	c.requests, err = s.gateway.LockCallerGroupInTx(ctx, tx, callerService, id)
	if err != nil {
		return c, err
	}
	c.Run, err = scanRun(tx.QueryRowForUpdate(ctx, "creative_agent_runs", runColumns, "id=$2", id))
	if err != nil {
		return c, err
	}
	err = tx.QueryRow(ctx, "creative_agent_runs", "claim_token,execution_epoch,lease_until,cancel_requested_at", "id=$2", id).Scan(&c.token, &c.epoch, &c.lease, &c.cancelled)
	return c, err
}
func (c controlledRun) unsettled() bool {
	for _, r := range c.requests {
		switch r.State {
		case llmgateway.StateDispatching, llmgateway.StateStreaming, llmgateway.StateUnknown:
			return true
		}
	}
	return false
}
func (c controlledRun) settlement() string {
	result := "not_started"
	for _, r := range c.requests {
		if r.State == llmgateway.StateUnknown || r.Settlement == llmgateway.SettlementUnknown {
			return "unknown"
		}
		switch r.Settlement {
		case llmgateway.SettlementReserved, llmgateway.SettlementPartial:
			result = "pending"
		case llmgateway.SettlementSettled, llmgateway.SettlementReleased:
			if result == "not_started" {
				result = "settled"
			}
		}
	}
	if c.unsettled() {
		return "unknown"
	}
	return result
}
func runOutcome(run Run, status int) (creativeops.Outcome, error) {
	raw, err := json.Marshal(run)
	if err != nil {
		return creativeops.Outcome{}, err
	}
	return creativeops.Outcome{HTTPStatus: status, Response: raw, ResultKind: "agent_run", ResultID: &run.ID, ResultRevision: &run.Revision}, nil
}

// CancelRun targets the run identity, not a stale revision from a polling tab.
func (s *Service) CancelRun(ctx context.Context, scope store.AccountScope, command creativeops.Command) (creativeops.Receipt, error) {
	return s.controlCommand(ctx, scope, command, false)
}
func (s *Service) CloseReconciliation(ctx context.Context, scope store.AccountScope, command creativeops.Command) (creativeops.Receipt, error) {
	return s.controlCommand(ctx, scope, command, true)
}
func (s *Service) controlCommand(ctx context.Context, scope store.AccountScope, command creativeops.Command, closeWaiting bool) (creativeops.Receipt, error) {
	var in struct {
		RunID string `json:"run_id"`
	}
	key := "agent.cancel_run"
	if closeWaiting {
		key = "agent.close_reconciliation"
	}
	return (creativeops.Executor{}).Run(ctx, scope, creativeops.Operation{Key: key, Capability: "creative_read", Validate: func(raw json.RawMessage) error {
		if err := creativeops.Decode(raw, &in); err != nil {
			return err
		}
		if in.RunID == "" {
			return creativeops.ErrValidation
		}
		return nil
	}, Apply: func(ctx context.Context, tx store.TxAccountScope, _ json.RawMessage) (creativeops.Outcome, error) {
		c, err := s.lockControlledRun(ctx, tx, in.RunID)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		if terminalRun(c.State) {
			if closeWaiting {
				return creativeops.Outcome{}, ErrRunState
			}
			return runOutcome(c.Run, 200)
		}
		if closeWaiting && c.State != RunReconciling {
			return creativeops.Outcome{}, ErrRunState
		}
		now, err := tx.CreativeNow(ctx)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		if !closeWaiting {
			if _, err = tx.Update(ctx, "creative_agent_runs", "cancel_requested_at=coalesce(cancel_requested_at,$3)", "id=$2", c.ID, now); err != nil {
				return creativeops.Outcome{}, err
			}
			c.cancelled = &now
		}
		if err = s.gateway.ReleaseUnusedGroupInTx(ctx, tx, callerService, c.ID); err != nil {
			return creativeops.Outcome{}, err
		}
		if c.unsettled() && !closeWaiting && now.Before(c.DeadlineAt) {
			err = s.moveControlledRun(ctx, tx, c, RunReconciling, "creative_run_cancelled", now)
		} else {
			state, code := RunFailed, "creative_reconciliation_closed"
			if c.cancelled != nil {
				state, code = RunCancelled, "creative_run_cancelled"
			}
			err = s.endControlledRun(ctx, tx, c, state, code, now)
		}
		if err != nil {
			return creativeops.Outcome{}, err
		}
		run, err := scanRun(tx.QueryRow(ctx, "creative_agent_runs", runColumns, "id=$2", c.ID))
		if err != nil {
			return creativeops.Outcome{}, err
		}
		return runOutcome(run, 200)
	}}, command)
}
func (s *Service) moveControlledRun(ctx context.Context, tx store.TxAccountScope, c controlledRun, state, code string, now time.Time) error {
	if _, err := tx.Update(ctx, "creative_agent_runs", "state=$3,error_code=$4,settlement_state=$5,execution_epoch=execution_epoch+1,lease_until=NULL,revision=revision+1,updated_at=$6", "id=$2", c.ID, state, nullableText(code), c.settlement(), now); err != nil {
		return err
	}
	_, err := appendRunEventInTx(ctx, tx, c.ID, "run.state_changed", map[string]string{"state": state, "reason": code, "settlement_state": c.settlement()})
	return err
}
func (s *Service) endControlledRun(ctx context.Context, tx store.TxAccountScope, c controlledRun, state, code string, now time.Time) error {
	delivered, err := tx.Count(ctx, "creative_agent_messages", "run_id=$2 AND role='assistant' AND status='complete'", c.ID)
	if err != nil {
		return err
	}
	if delivered > 0 && state != RunSucceeded {
		state = RunPartial
	}
	if err = s.gateway.ReleaseUnusedGroupInTx(ctx, tx, callerService, c.ID); err != nil {
		return err
	}
	c.requests, err = s.gateway.LockCallerGroupInTx(ctx, tx, callerService, c.ID)
	if err != nil {
		return err
	}
	if err = s.gateway.AbandonCallerGroupResultsInTx(ctx, tx, callerService, c.ID); err != nil {
		return err
	}
	if _, err = tx.Update(ctx, "creative_agent_runs", "state=$3,error_code=$4,settlement_state=$5,execution_epoch=execution_epoch+1,lease_until=NULL,revision=revision+1,finished_at=$6,updated_at=$6", "id=$2", c.ID, state, nullableText(code), c.settlement(), now); err != nil {
		return err
	}
	if _, err = tx.Update(ctx, "creative_agent_steps", "state=CASE WHEN kind='model' AND $5 THEN 'unknown' ELSE 'failed' END,error_code=$3,finished_at=$4,updated_at=$4", "run_id=$2 AND state='prepared'", c.ID, nullableText(code), now, c.unsettled()); err != nil {
		return err
	}
	if err = releaseSlotInTx(ctx, tx, c.ID, c.token, now); err != nil {
		return err
	}
	for _, table := range []string{"creative_agent_checkpoints", "creative_agent_context_items"} {
		if _, err = tx.Update(ctx, table, "retained_until=GREATEST(retained_until,$3)", "run_id=$2", c.ID, now.Add(contextRetention)); err != nil {
			return err
		}
	}
	_, err = appendRunEventInTx(ctx, tx, c.ID, "run.finished", map[string]string{"state": state, "error_code": code, "settlement_state": c.settlement()})
	return err
}
func (s *Service) enqueueControlledRun(ctx context.Context, tx store.TxAccountScope, c controlledRun, now time.Time) error {
	if err := s.moveControlledRun(ctx, tx, c, RunQueued, "", now); err != nil {
		return err
	}
	raw, err := json.Marshal(runTask{RunID: c.ID})
	if err != nil {
		return err
	}
	_, err = jobs.EnqueueInTx(ctx, tx.Jobs(s.runtime), jobs.Request{Kind: jobRunKind, OperationID: uuid.NewSHA1(uuid.NameSpaceOID, []byte(c.ID+":resume:"+strconv.FormatInt(c.epoch+1, 10))).String(), CreatedAt: now, Payload: raw})
	return err
}

// completeAttempt observes the Gateway after the bounded worker exits. Unknown
// transport results retain the slot and cannot become a fresh model attempt.
func (s *Service) completeAttempt(ctx context.Context, scope store.AccountScope, held claim, out turnOutcome, turnErr error) error {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), finishTimeout)
	defer cancel()
	handled := false
	err := scope.WithTxScope(cleanup, func(tx store.TxAccountScope) error {
		c, err := s.lockControlledRun(cleanup, tx, held.RunID)
		if err != nil {
			return err
		}
		if c.token != held.Token || c.epoch != held.Epoch || c.State != RunRunning {
			handled = true
			return nil
		}
		if !c.unsettled() {
			if errors.Is(turnErr, errRunInterrupted) {
				handled = true
				now, err := tx.CreativeNow(cleanup)
				if err != nil {
					return err
				}
				if !now.Before(c.DeadlineAt) {
					return s.endControlledRun(cleanup, tx, c, RunFailed, "creative_run_deadline_exceeded", now)
				}
				exists, err := tx.Exists(cleanup, "creative_agent_checkpoints", "run_id=$2", c.ID)
				if err != nil {
					return err
				}
				if !exists {
					return errCheckpointMissing
				}
				return s.waitForInput(cleanup, tx, c, "checkpoint_interrupted", now)
			}
			return nil
		}
		handled = true
		now, err := tx.CreativeNow(cleanup)
		if err != nil {
			return err
		}
		if !now.Before(c.DeadlineAt) {
			return s.endControlledRun(cleanup, tx, c, RunFailed, "creative_run_deadline_exceeded", now)
		}
		return s.moveControlledRun(cleanup, tx, c, RunReconciling, "creative_llm_unknown", now)
	})
	if err != nil || handled {
		return err
	}
	return s.finishRun(ctx, scope, held, out, turnErr)
}

// SweepRuns rescues a bounded set of persistent runs. No provider dispatch or
// waiting happens here; eligible continuation is atomically queued for a worker.
func (s *Service) SweepRuns(ctx context.Context, scope store.AccountScope, batch int) (int, error) {
	if batch < 1 || batch > 1000 {
		return 0, creativeops.ErrValidation
	}
	rows, err := scope.QueryPage(ctx, "creative_agent_runs", "id", "(state='running' AND lease_until<clock_timestamp()) OR (state='queued' AND updated_at<clock_timestamp()-interval '30 seconds') OR state='reconciling' OR (state IN ('waiting_input','waiting_apply') AND deadline_at<=clock_timestamp()) OR (finished_at IS NOT NULL AND settlement_state IN ('unknown','pending'))", []store.OrderBy{{Column: "updated_at"}, {Column: "id"}}, batch, 0)
	if err != nil {
		return 0, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	for i, id := range ids {
		if err = s.sweepRun(ctx, scope, id); err != nil {
			return i, err
		}
	}
	return len(ids), nil
}
func (s *Service) sweepRun(ctx context.Context, scope store.AccountScope, id string) error {
	return scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		c, err := s.lockControlledRun(ctx, tx, id)
		if err != nil {
			return err
		}
		now, err := tx.CreativeNow(ctx)
		if err != nil {
			return err
		}
		if terminalRun(c.State) {
			if _, err = tx.Update(ctx, "creative_agent_runs", "settlement_state=$3,updated_at=$4", "id=$2", c.ID, c.settlement(), now); err != nil {
				return err
			}
			if err = s.gateway.AbandonCallerGroupResultsInTx(ctx, tx, callerService, c.ID); err != nil {
				return err
			}
			if c.SettlementState != c.settlement() {
				_, err = appendRunEventInTx(ctx, tx, c.ID, "usage.updated", map[string]string{"settlement_state": c.settlement()})
			}
			return err
		}
		if c.State == RunRunning && c.lease != nil && now.Before(*c.lease) {
			return nil
		}
		if !now.Before(c.DeadlineAt) || c.cancelled != nil && !c.unsettled() {
			state, code := RunFailed, "creative_run_deadline_exceeded"
			if c.cancelled != nil {
				state, code = RunCancelled, "creative_run_cancelled"
			}
			return s.endControlledRun(ctx, tx, c, state, code, now)
		}
		if c.State == RunWaitingInput || c.State == RunWaitingApply {
			return nil
		}
		if c.unsettled() {
			return s.moveControlledRun(ctx, tx, c, RunReconciling, "creative_llm_unknown", now)
		}
		for _, request := range c.requests {
			if request.State == llmgateway.StateFailed || request.State == llmgateway.StateCancelled {
				return s.endControlledRun(ctx, tx, c, RunFailed, "creative_run_failed", now)
			}
		}
		count, err := tx.Count(ctx, "creative_agent_steps", "run_id=$2", c.ID)
		if err != nil {
			return err
		}
		delivered, err := tx.Count(ctx, "creative_agent_messages", "run_id=$2 AND role='assistant' AND status='complete'", c.ID)
		if err != nil {
			return err
		}
		pending, err := tx.Count(ctx, "creative_agent_steps", "run_id=$2 AND state<>'succeeded'", c.ID)
		if err != nil {
			return err
		}
		if delivered > 0 && pending == 0 {
			return s.endControlledRun(ctx, tx, c, RunSucceeded, "", now)
		}
		checkpoint, err := tx.Exists(ctx, "creative_agent_checkpoints", "run_id=$2 AND retained_until>$3", c.ID, now)
		if err != nil {
			return err
		}
		if count > 0 && !checkpoint && c.SourceRunID == "" {
			return s.endControlledRun(ctx, tx, c, RunFailed, "creative_checkpoint_missing", now)
		}
		return s.enqueueControlledRun(ctx, tx, c, now)
	})
}
func (s *Service) SweepAllAccounts(ctx context.Context, db *store.Store, after string, batch int) (string, error) {
	ids, err := db.MaintenanceAccountIDs(ctx, after, batch)
	if err != nil {
		return after, err
	}
	var errs []error
	attempted := after
	for _, id := range ids {
		if ctx.Err() != nil {
			return attempted, errors.Join(append(errs, ctx.Err())...)
		}
		attempted = id
		if _, err = s.SweepRuns(ctx, db.ScopeFor(auth.AccountContext{AccountID: id}), batch); err != nil {
			errs = append(errs, fmt.Errorf("account %s: %w", id, err))
		}
	}
	next := ""
	if len(ids) == batch {
		next = ids[len(ids)-1]
	}
	return next, errors.Join(errs...)
}
