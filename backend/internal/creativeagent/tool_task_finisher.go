package creativeagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// The finisher wakes on NOTIFY from the source triggers and rescans on this
// fallback, which covers a lost notification or a dropped listener
// (agent-tool-background design「收尾器」).
const (
	toolTaskChannel      = "creative_agent_tool_tasks"
	toolTaskFallbackScan = 5 * time.Second
	toolTaskBatch        = 50
	// A task whose finishing failed is retried after this; bounded, so a
	// broken item cannot starve the queue or spin.
	toolTaskRetryDelay = 30 * time.Second
)

// Task states and modes (data: 助手耗时工具任务与完成通知).
const (
	taskRunning   = "running"
	taskSucceeded = "succeeded"
	taskFailed    = "failed"
	taskCancelled = "cancelled"
	taskExpired   = "expired"

	modeForeground = "foreground"
	modeBackground = "background"
)

// toolTask is one creative_agent_tool_tasks row as the finisher reads it.
type toolTask struct {
	ID             string
	ConversationID string
	CanvasID       string
	SourceRunID    string
	ToolStepID     string
	ToolKey        string
	SourceID       string
	Mode           string
	State          string
	Revision       int64
	// RunGone is set by the finisher for a foreground task whose run may no
	// longer write (stopped, past its deadline): its result is recorded,
	// nothing is adopted on its behalf and nothing continues the run.
	RunGone bool
}

const toolTaskColumns = "id,conversation_id,canvas_id,source_run_id,tool_step_id,tool_key,source_id,mode,state,revision"

func scanToolTask(row store.Row) (toolTask, error) {
	var t toolTask
	err := row.Scan(&t.ID, &t.ConversationID, &t.CanvasID, &t.SourceRunID, &t.ToolStepID, &t.ToolKey, &t.SourceID, &t.Mode, &t.State, &t.Revision)
	return t, err
}

// toolTaskOutcome is what a source reports about its task once it is locked.
type toolTaskOutcome struct {
	// Done says the tool has its final answer: State and Result are set.
	Done bool
	// State is the task's terminal state.
	State string
	// Result is the tool result a foreground call gets, the same bytes a
	// background notice carries.
	Result string
	// Failed marks Result as a failed tool call for the model.
	Failed bool
	// RecheckAt, when the tool is not done, is when to look again even
	// without a source change (a source deadline). Zero waits for a trigger.
	RecheckAt time.Time
	// Conclude, when the tool is not done, is work that must run outside the
	// finishing transaction because it takes its own locks (adopting a
	// compression candidate). The finisher runs it after committing and
	// finishes the task with its answer in a new pass.
	Conclude func(ctx context.Context, scope store.AccountScope) (toolTaskOutcome, error)
}

// toolTaskSource finishes one kind of source. settle runs in the finisher's
// transaction after the run (foreground) or the project (background) is
// locked and before the task row: it locks its own rows in their domain
// order, may move them on (adopting a ready compression candidate), and
// reports the outcome. It never takes run, slot or conversation locks.
type toolTaskSource interface {
	settle(ctx context.Context, tx store.TxAccountScope, task toolTask) (toolTaskOutcome, error)
}

// toolTaskSourceFor is the source that finishes tasks of tool. Tests swap in
// their own through toolSources.
func (s *Service) toolTaskSourceFor(tool string) (toolTaskSource, bool) {
	if source, ok := s.toolSources[tool]; ok {
		return source, true
	}
	return nil, false
}

// RunToolTaskFinisher finishes due agent tool tasks until ctx ends: on every
// NOTIFY from a source trigger, and on a fallback tick.
func (s *Service) RunToolTaskFinisher(ctx context.Context, db *store.Store) {
	wake, err := db.Listen(ctx, toolTaskChannel)
	if err != nil {
		s.logger.Error("creative agent tool task listener", slog.String("error", err.Error()))
		return
	}
	ticker := time.NewTicker(toolTaskFallbackScan)
	defer ticker.Stop()
	timer := time.NewTimer(toolTaskFallbackScan)
	defer timer.Stop()
	for {
		for ctx.Err() == nil {
			passCtx, cancel := context.WithTimeout(ctx, time.Minute)
			n, err := s.FinishDueToolTasks(passCtx, db, toolTaskBatch)
			cancel()
			if err != nil && ctx.Err() == nil {
				s.logger.Warn("creative agent tool task finisher", slog.String("error", err.Error()))
			}
			if n < toolTaskBatch {
				break
			}
		}
		// Idle wake-ups ride the same loop: notices just published may have
		// set a wake time a few seconds out.
		for ctx.Err() == nil {
			passCtx, cancel := context.WithTimeout(ctx, time.Minute)
			n, err := s.WakeDueToolTasks(passCtx, db, toolTaskBatch)
			cancel()
			if err != nil && ctx.Err() == nil {
				s.logger.Warn("creative agent tool task wake", slog.String("error", err.Error()))
			}
			if n < toolTaskBatch {
				break
			}
		}
		next := toolTaskFallbackScan
		if !s.backgroundTasksDisabled {
			if at, err := db.NextToolTaskWake(ctx); err == nil && at != nil {
				next = min(next, max(time.Until(*at), 50*time.Millisecond))
			}
		}
		timer.Reset(next)
		select {
		case <-ctx.Done():
			return
		case _, ok := <-wake:
			if !ok {
				return
			}
		case <-ticker.C:
		case <-timer.C:
		}
	}
}

// FinishDueToolTasks handles one page of due tasks across accounts, each in
// its own account scope and transaction, and reports how many it looked at.
// A failing task is pushed back by toolTaskRetryDelay instead of blocking the
// page.
func (s *Service) FinishDueToolTasks(ctx context.Context, db *store.Store, batch int) (int, error) {
	refs, err := db.DueToolTasks(ctx, batch)
	if err != nil {
		return 0, err
	}
	var errs []error
	for _, ref := range refs {
		if ctx.Err() != nil {
			return len(refs), errors.Join(append(errs, ctx.Err())...)
		}
		scope := db.ScopeFor(auth.AccountContext{AccountID: ref.AccountID})
		if err := s.finishToolTask(ctx, scope, ref.ID, nil); err != nil {
			errs = append(errs, fmt.Errorf("tool task %s: %w", ref.ID, err))
			if retryErr := deferToolTask(ctx, scope, ref.ID); retryErr != nil {
				errs = append(errs, retryErr)
			}
		}
	}
	return len(refs), errors.Join(errs...)
}

func deferToolTask(ctx context.Context, scope store.AccountScope, id string) error {
	return scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		_, err := tx.Update(ctx, "creative_agent_tool_tasks",
			fmt.Sprintf("next_check_at=clock_timestamp()+interval '%d seconds',updated_at=clock_timestamp()", int(toolTaskRetryDelay.Seconds())),
			"id=$2 AND next_check_at IS NOT NULL", id)
		return err
	})
}

// finishToolTask runs one finishing pass in the lock order of the design
// (project → slot → run → source → task): the task is read unlocked to learn
// what to lock, then locked last and rechecked, so two workers that picked
// the same task serialize on the project and the second finds nothing due.
func (s *Service) finishToolTask(ctx context.Context, scope store.AccountScope, id string, fixed *toolTaskOutcome) error {
	var sceneRefusal *runGateRefused
	var conclude func(context.Context, store.AccountScope) (toolTaskOutcome, error)
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		peek, err := scanToolTask(tx.QueryRow(ctx, "creative_agent_tool_tasks", toolTaskColumns, "id=$2", id))
		if errors.Is(err, store.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		source, ok := s.toolTaskSourceFor(peek.ToolKey)
		if !ok {
			// Foreground scene tasks finish in the scene completion
			// transaction; nothing here owns this task yet.
			_, err = tx.Update(ctx, "creative_agent_tool_tasks", "next_check_at=NULL", "id=$2", id)
			return err
		}
		var gate *sceneGate
		if peek.Mode == modeForeground && peek.State == taskRunning {
			g, err := sceneWaiter{s: s}.admitRun(ctx, tx, peek.SourceRunID)
			switch {
			case errors.Is(err, creativescene.ErrRunGate):
				// The run may no longer write: the task still records its
				// result, nothing continues the run.
				sceneRefusal = &runGateRefused{runID: peek.SourceRunID, cause: err}
				peek.RunGone = true
				if err := lockTaskProject(ctx, tx, peek.CanvasID); err != nil {
					return err
				}
			case err != nil:
				return err
			default:
				gate = g
			}
		} else {
			if err := lockTaskProject(ctx, tx, peek.CanvasID); err != nil {
				return err
			}
			if peek.Mode == modeBackground {
				if err := lockToolTaskRunInTx(ctx, tx, peek.SourceRunID); err != nil {
					return err
				}
			}
		}
		// A worker that waited on these locks for another one finds the task
		// already handled and does not settle the source a second time.
		var outcome toolTaskOutcome
		if fixed != nil {
			// A conclusion that ran after an earlier pass: its answer stands
			// as long as the task is still running.
			outcome = *fixed
		} else {
			due, err := tx.Exists(ctx, "creative_agent_tool_tasks", "id=$2 AND next_check_at IS NOT NULL AND next_check_at<=clock_timestamp()", id)
			if err != nil || !due {
				return err
			}
			if outcome, err = source.settle(ctx, tx, peek); err != nil {
				return err
			}
		}
		task, err := scanToolTask(tx.QueryRowForUpdate(ctx, "creative_agent_tool_tasks", toolTaskColumns, "id=$2", id))
		if err != nil {
			return err
		}
		if task.State != taskRunning {
			_, err = tx.Update(ctx, "creative_agent_tool_tasks", "next_check_at=NULL", "id=$2", id)
			return err
		}
		if task.Mode != peek.Mode {
			// Detached between the peek and the lock: the next pass locks
			// for the new mode.
			return nil
		}
		if !outcome.Done {
			if outcome.Conclude != nil {
				// Should the conclusion never report back (a crash), the
				// task comes due again after the retry delay.
				conclude = outcome.Conclude
				_, err = tx.Update(ctx, "creative_agent_tool_tasks",
					fmt.Sprintf("next_check_at=clock_timestamp()+interval '%d seconds'", int(toolTaskRetryDelay.Seconds())), "id=$2", id)
				return err
			}
			var recheck any
			if !outcome.RecheckAt.IsZero() {
				recheck = outcome.RecheckAt
			}
			_, err = tx.Update(ctx, "creative_agent_tool_tasks", "next_check_at=$3", "id=$2", id, recheck)
			return err
		}
		if err = s.finishTaskInTx(ctx, tx, task, outcome); err != nil {
			return err
		}
		if task.Mode == modeForeground && gate != nil {
			return s.publishForegroundInTx(ctx, tx, gate, task, outcome)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if sceneRefusal != nil {
		// A waiting run that may no longer write is ended the way the scene
		// sweep ends one, outside the finishing transaction.
		if err = (sceneWaiter{s: s}).Abort(ctx, scope, sceneRefusal.runID, sceneRefusal.cause); err != nil {
			return err
		}
	}
	if conclude == nil {
		return nil
	}
	concluded, err := conclude(ctx, scope)
	if err != nil {
		return err
	}
	if !concluded.Done {
		return fmt.Errorf("creative agent: tool task %s concluded without an answer", id)
	}
	return s.finishToolTask(ctx, scope, id, &concluded)
}

type runGateRefused struct {
	runID string
	cause error
}

// lockTaskProject takes the project lock a background finish needs. An
// archived project still finishes the task; its notice is suppressed later.
func lockTaskProject(ctx context.Context, tx store.TxAccountScope, canvasID string) error {
	err := creativecanvas.LockProjectForWorkInTx(ctx, tx, canvasID)
	if errors.Is(err, creativecanvas.ErrArchived) {
		return nil
	}
	return err
}

// finishTaskRow writes a task's terminal state and result.
func finishTaskRow(ctx context.Context, tx store.TxAccountScope, task toolTask, outcome toolTaskOutcome) error {
	switch outcome.State {
	case taskSucceeded, taskFailed, taskCancelled, taskExpired:
	default:
		return fmt.Errorf("creative agent: tool task %s settled as %q", task.ID, outcome.State)
	}
	result := json.RawMessage(outcome.Result)
	if !json.Valid(result) {
		return fmt.Errorf("creative agent: tool task %s result is not JSON", task.ID)
	}
	_, err := tx.Update(ctx, "creative_agent_tool_tasks",
		"state=$3,result=$4,finished_at=clock_timestamp(),next_check_at=NULL,revision=revision+1,updated_at=clock_timestamp()",
		"id=$2 AND state='running'", task.ID, outcome.State, []byte(result))
	return err
}

// publishForegroundInTx hands a foreground task's result to the call that
// parked on it and continues the run, as a scene job's completion does.
func (s *Service) publishForegroundInTx(ctx context.Context, tx store.TxAccountScope, g *sceneGate, task toolTask, outcome toolTaskOutcome) error {
	if err := s.publishParkedStepInTx(ctx, tx, g, task.ToolStepID, outcome.Result, outcome.Failed); err != nil {
		return err
	}
	if g.c.State != RunWaitingClient {
		return nil
	}
	now, err := tx.CreativeNow(ctx)
	if err != nil {
		return err
	}
	_, err = s.resumeClientWait(ctx, tx, g.c, now)
	return err
}
