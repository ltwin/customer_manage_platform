package creativeagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/cloudwego/eino/adk"

	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
	"github.com/samson/customer-manage-platform/backend/internal/creativeskill"
	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// SceneJobWaiter is the run side of scene client jobs: a job's completion or
// expiry reaches the run that parked on it. The server and the creative
// worker register it with creativescene.SetJobWaiter at start-up.
func (s *Service) SceneJobWaiter() creativescene.JobWaiter { return sceneWaiter{s: s} }

type sceneWaiter struct{ s *Service }

// sceneGate is a run admitted in the completing transaction: locked, with the
// claim a parked call's result is published under.
type sceneGate struct {
	c    controlledRun
	held claim
}

// Admit locks the run in the run lock order (project, slot, gateway group,
// run, skill, consent) and rechecks what its write needs. A run still running
// or queued is admitted too: the job finished while the attempt that parked
// was unwinding, or while a rerun after a crash had not reached the call yet.
func (w sceneWaiter) Admit(ctx context.Context, tx store.TxAccountScope, runID string) (creativescene.RunGate, error) {
	refuse := func(err error) (creativescene.RunGate, error) {
		if runGateRefusal(err) {
			return nil, fmt.Errorf("%w: %w", creativescene.ErrRunGate, err)
		}
		return nil, err
	}
	if err := tx.RequireCreativeCapability(ctx, "agent_start"); err != nil {
		return refuse(err)
	}
	var canvasID string
	if err := tx.QueryRow(ctx, "creative_agent_runs", "canvas_id", "id=$2", runID).Scan(&canvasID); err != nil {
		if errors.Is(err, store.ErrNoRows) {
			return refuse(ErrNotFound)
		}
		return nil, err
	}
	if err := creativecanvas.LockProjectForWorkInTx(ctx, tx, canvasID); err != nil {
		return refuse(err)
	}
	c, err := w.s.lockControlledRun(ctx, tx, runID)
	if err != nil {
		return refuse(err)
	}
	switch c.State {
	case RunWaitingClient, RunRunning, RunQueued:
	default:
		return refuse(ErrRunState)
	}
	if c.cancelled != nil {
		return refuse(ErrRunState)
	}
	held, err := w.s.parkedClaimInTx(ctx, tx, c)
	if err != nil {
		return nil, err
	}
	now, err := tx.CreativeNow(ctx)
	if err != nil {
		return nil, err
	}
	if err = w.s.admitParkedRunInTx(ctx, tx, held, now); err != nil {
		return refuse(err)
	}
	return &sceneGate{c: c, held: held}, nil
}

// runGateRefusal tells a run that may no longer write from a failure to check.
func runGateRefusal(err error) bool {
	return errors.Is(err, store.ErrCreativeAccessDenied) || errors.Is(err, llmgateway.ErrDeadline) ||
		errors.Is(err, ErrConsentRevoked) || errors.Is(err, ErrEgressRequired) || errors.Is(err, ErrNotFound) ||
		errors.Is(err, ErrRunState) || errors.Is(err, ErrUnsupportedSegment) || errors.Is(err, creativeskill.ErrNotFound) ||
		errors.Is(err, creativecanvas.ErrArchived)
}

// Settle publishes the job's result as the parked call's tool result, in the
// order callTool publishes one, and queues a waiting run that waits on nothing
// else. A step that is no longer prepared was already written; a run still
// running or queued picks the result up from the journal.
func (w sceneWaiter) Settle(ctx context.Context, tx store.TxAccountScope, gate creativescene.RunGate, job creativescene.Job, res creativescene.JobResult) error {
	g, ok := gate.(*sceneGate)
	if !ok {
		return errors.New("creative agent: foreign run gate")
	}
	if job.ToolCallID == nil {
		return nil
	}
	var state, name, parent string
	var index int
	var input []byte
	if err := tx.QueryRowForUpdate(ctx, "creative_agent_steps", "state,tool_key,parent_model_step_id,tool_call_index,input",
		"id=$2 AND run_id=$3 AND kind='tool'", *job.ToolCallID, g.c.ID).Scan(&state, &name, &parent, &index, &input); err != nil {
		return fmt.Errorf("creative agent: parked step %s: %w", *job.ToolCallID, err)
	}
	if state == "prepared" {
		var call llmgateway.ToolCall
		if err := json.Unmarshal(input, &call); err != nil {
			return err
		}
		result, failed, err := sceneToolResult(ctx, tx, job, res, string(call.Arguments))
		if err != nil {
			return err
		}
		h, err := w.s.parkedHandlerInTx(ctx, tx, g.held)
		if err != nil {
			return err
		}
		code := ""
		if failed {
			code = "creative_tool_failed"
		}
		if _, err = h.publishResultInTx(ctx, tx, publishedCall{StepID: *job.ToolCallID, CallID: runtimeCallID(parent, index), Name: name,
			Args: string(call.Arguments), Result: result, Failed: failed, Code: code}); err != nil {
			return err
		}
	}
	if g.c.State != RunWaitingClient {
		return nil
	}
	now, err := tx.CreativeNow(ctx)
	if err != nil {
		return err
	}
	// Admit took the project lock, so the slot comes next in order.
	_, err = w.s.resumeClientWait(ctx, tx, g.c, now)
	return err
}

// Abort ends a waiting run that failed Admit; ending it cancels its jobs. A
// run that is running or queued is left to its worker, whose own admission
// meets the same refusal.
func (w sceneWaiter) Abort(ctx context.Context, scope store.AccountScope, runID string, cause error) error {
	return scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		c, err := w.s.lockControlledRun(ctx, tx, runID)
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if c.State != RunWaitingClient {
			return nil
		}
		now, err := tx.CreativeNow(ctx)
		if err != nil {
			return err
		}
		state, code := RunFailed, failureCode(cause)
		switch {
		case c.cancelled != nil:
			state, code = RunCancelled, "creative_run_cancelled"
		case !now.Before(c.DeadlineAt):
			code = "creative_run_deadline_exceeded"
		}
		return w.s.endControlledRun(ctx, tx, c, state, code, now)
	})
}

// parkedClaimInTx is the claim of a run the caller holds, as claimRun would
// have built it, without taking the run over.
func (s *Service) parkedClaimInTx(ctx context.Context, tx store.TxAccountScope, c controlledRun) (claim, error) {
	held := claim{RunID: c.ID, ConversationID: c.ConversationID, Epoch: c.epoch, Token: c.token, ConsentID: c.EgressConsentID, Deadline: c.DeadlineAt, Kind: c.Kind}
	var skillSnapshot, limitsSnapshot []byte
	if err := tx.QueryRow(ctx, "creative_agent_runs", "skill_snapshot,limits_snapshot", "id=$2", c.ID).Scan(&skillSnapshot, &limitsSnapshot); err != nil {
		return claim{}, err
	}
	if err := json.Unmarshal(limitsSnapshot, &held.Limits); err != nil {
		return claim{}, err
	}
	if len(skillSnapshot) > 0 {
		var snapshot creativeskill.Snapshot
		if err := json.Unmarshal(skillSnapshot, &snapshot); err != nil {
			return claim{}, err
		}
		held.Skill = &snapshot
	}
	// Only the tool availability reads the model, and a model withdrawn since
	// does not stop a result from being recorded.
	if model, err := s.models.Model(c.ModelKey); err == nil {
		held.Model = model
	}
	return held, nil
}

// parkedHandlerInTx is the runtime view publishResultInTx needs for a parked
// call: the run's claim, its frozen limits and its input revisions.
func (s *Service) parkedHandlerInTx(ctx context.Context, tx store.TxAccountScope, held claim) (*runtimeHandler, error) {
	rows, err := tx.Query(ctx, "creative_run_inputs", "content_revision_id", "run_id=$2 AND content_revision_id IS NOT NULL", held.RunID)
	if err != nil {
		return nil, err
	}
	var revisions []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		revisions = append(revisions, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	service := *s
	service.limits = held.Limits
	return &runtimeHandler{BaseChatModelAgentMiddleware: &adk.BaseChatModelAgentMiddleware{}, service: &service, held: held, revisions: revisions, current: &currentStep{}}, nil
}

var _ creativescene.JobWaiter = sceneWaiter{}

// admitParkedRunInTx rechecks what a run's write needs once the caller holds
// the run: its deadline, active skills and a standing consent. It is the part
// of admitRunInTx that does not depend on holding the claim's lease.
func (s *Service) admitParkedRunInTx(ctx context.Context, tx store.TxAccountScope, held claim, now time.Time) error {
	if !now.Before(held.Deadline) {
		return llmgateway.ErrDeadline
	}
	// The skill control lock, between the run and the consent: the design puts
	// the skill control row after operation/account/slot/budget/conversation/run
	// and before the resource locks, and the dispatch intent this transaction is
	// about to record must not be able to overtake a withdrawal.
	if err := s.requireActiveSkillsInTx(ctx, tx, held); err != nil {
		return err
	}
	consent, err := readConsentForUpdate(ctx, tx, held.ConsentID)
	if err != nil {
		return err
	}
	// The content refs were matched against this consent when the run was
	// created and a consent's scope never changes afterwards, so what is
	// re-checked here is whether it still stands at all.
	return coversRun(consent, held.ConversationID, nil)
}
