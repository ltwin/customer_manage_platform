package creativeagent

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// maxSupplements bounds how many times the photographer can add input to one
// waiting run. It was the old tool call count and keeps that number.
const maxSupplements = 12

func (s *Service) waitForInput(ctx context.Context, tx store.TxAccountScope, c controlledRun, reason string, now time.Time) error {
	token, err := randomClaimToken()
	if err != nil {
		return err
	}
	if _, err = tx.Update(ctx, "creative_agent_runs", "waiting_token=$3,wait_reason=$4", "id=$2", c.ID, token, reason); err != nil {
		return err
	}
	if err = s.moveControlledRun(ctx, tx, c, RunWaitingInput, "", now); err != nil {
		return err
	}
	return releaseSlotInTx(ctx, tx, c.ID, c.token, now)
}

// Wait reasons of a waiting_client run.
const (
	waitClientJob = "client_job"
	waitSlotBusy  = "slot_busy"
)

// errParked is what a tool returns once it has handed its work to a page as a
// client job. callTool turns it into an interrupt; the step stays prepared and
// the job's completion writes its result.
type errParked struct{ jobID string }

func (e errParked) Error() string { return "creative agent: call parked on client job " + e.jobID }

// clientJobInfo prefixes the interrupt info of a parked call. The info is a
// plain string so the gob checkpoint needs no registered type.
const clientJobInfo = "client_job:"

func parkedJobOf(info any) (string, bool) {
	s, ok := info.(string)
	if !ok || !strings.HasPrefix(s, clientJobInfo) {
		return "", false
	}
	return strings.TrimPrefix(s, clientJobInfo), true
}

// parkedRun is the interruption of an attempt whose tools parked on client
// jobs. It is errRunInterrupted to everything else; completeAttempt decides on
// the jobs it names, not on step states (design review B2).
type parkedRun struct{ jobIDs []string }

func (p *parkedRun) Error() string { return errRunInterrupted.Error() + ": waiting on client jobs" }
func (p *parkedRun) Unwrap() error { return errRunInterrupted }

// parkOnClient settles an attempt that parked. Jobs that already finished (a
// fast page, or one that completed while the attempt was still unwinding) put
// the run straight back in the queue with its slot; otherwise it waits. The
// run is locked, so its jobs come after it in the lock order.
func (s *Service) parkOnClient(ctx context.Context, tx store.TxAccountScope, c controlledRun, jobIDs []string, now time.Time) error {
	jobs, err := creativescene.LockJobs(ctx, tx, jobIDs)
	if err != nil {
		return err
	}
	for _, job := range jobs {
		if !creativescene.Terminal(job.State) {
			return s.waitForClient(ctx, tx, c, now)
		}
	}
	pending, err := tx.Count(ctx, "creative_agent_steps", "run_id=$2 AND state='prepared'", c.ID)
	if err != nil {
		return err
	}
	if pending > 0 {
		return s.waitForClient(ctx, tx, c, now)
	}
	return s.enqueueControlledRun(ctx, tx, c, now)
}

// waitForClient parks a run on its client jobs: a new waiting token, the
// reason, waiting_client and the slot released, in one transaction. The jobs
// resume it (see resumeClientWait); a photographer's message cannot.
func (s *Service) waitForClient(ctx context.Context, tx store.TxAccountScope, c controlledRun, now time.Time) error {
	token, err := randomClaimToken()
	if err != nil {
		return err
	}
	if _, err = tx.Update(ctx, "creative_agent_runs", "waiting_token=$3,wait_reason=$4", "id=$2", c.ID, token, waitClientJob); err != nil {
		return err
	}
	if err = s.moveControlledRun(ctx, tx, c, RunWaitingClient, "", now); err != nil {
		return err
	}
	return releaseSlotInTx(ctx, tx, c.ID, c.token, now)
}

// resumeClientWait continues a waiting_client run once none of its tool steps
// is still waiting: it takes the slot and queues the run. The caller holds the
// project lock and the run (project -> slot -> gateway -> run). A slot another
// run holds leaves it waiting, with reason slot_busy; the run sweep retries.
func (s *Service) resumeClientWait(ctx context.Context, tx store.TxAccountScope, c controlledRun, now time.Time) (bool, error) {
	pending, err := tx.Count(ctx, "creative_agent_steps", "run_id=$2 AND state='prepared'", c.ID)
	if err != nil || pending > 0 {
		return false, err
	}
	if err = acquireSlotInTx(ctx, tx, c.ConversationID, c.ID, c.token, now); err != nil {
		var busy BusyError
		if !errors.As(err, &busy) {
			return false, err
		}
		if c.WaitReason == waitSlotBusy {
			return false, nil
		}
		if _, err = tx.Update(ctx, "creative_agent_runs", "wait_reason=$3,revision=revision+1,updated_at=$4", "id=$2", c.ID, waitSlotBusy, now); err != nil {
			return false, err
		}
		return false, runStateChangedInTx(ctx, tx, c.ID, map[string]string{"state": RunWaitingClient, "reason": waitSlotBusy, "settlement_state": c.settlement()})
	}
	if _, err = tx.Update(ctx, "creative_agent_runs", "waiting_token=NULL,wait_reason=NULL", "id=$2", c.ID); err != nil {
		return false, err
	}
	return true, s.enqueueControlledRun(ctx, tx, c, now)
}

// Supplements are photographer messages under the original consent. Model,
// Skill, deadline and existing input batches remain fixed.
func (s *Service) SupplementRun(ctx context.Context, scope store.AccountScope, command creativeops.Command) (creativeops.Receipt, error) {
	var in struct {
		RunID        string               `json:"run_id"`
		Revision     creativeops.Revision `json:"expected_revision"`
		WaitingToken string               `json:"waiting_token"`
		Text         string               `json:"text"`
	}
	return (creativeops.Executor{}).Run(ctx, scope, creativeops.Operation{Key: "agent.supplement_run", Capability: "agent_start", Validate: func(raw json.RawMessage) error {
		if err := creativeops.Decode(raw, &in); err != nil {
			return err
		}
		if in.RunID == "" || in.WaitingToken == "" || in.Revision < 1 || strings.TrimSpace(in.Text) == "" {
			return creativeops.ErrValidation
		}
		if len(in.Text) > s.limits.MessageBodyBytes {
			return ErrLimit
		}
		return nil
	}, Apply: func(ctx context.Context, tx store.TxAccountScope, _ json.RawMessage) (creativeops.Outcome, error) {
		// A waiting run may sit in an archived project; it continues only
		// after the project is restored (owner 2026-10-11, D6).
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
		c, err := s.lockControlledRun(ctx, tx, in.RunID)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		if c.State != RunWaitingInput || c.cancelled != nil {
			return creativeops.Outcome{}, ErrRunState
		}
		if c.Revision != in.Revision || c.WaitingToken != in.WaitingToken {
			return creativeops.Outcome{}, ErrRevisionConflict
		}
		now, err := tx.CreativeNow(ctx)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		if !now.Before(c.DeadlineAt) {
			return creativeops.Outcome{}, creativeops.ErrExpired
		}
		var batches int
		rows, err := tx.QueryPage(ctx, "creative_run_inputs", "batch_ordinal", "run_id=$2", []store.OrderBy{{Column: "batch_ordinal", Desc: true}}, 1, 0, c.ID)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		if rows.Next() {
			err = rows.Scan(&batches)
		}
		rowErr := rows.Err()
		rows.Close()
		if err != nil {
			return creativeops.Outcome{}, err
		}
		if rowErr != nil {
			return creativeops.Outcome{}, rowErr
		}
		if batches >= maxSupplements {
			return creativeops.Outcome{}, ErrLimit
		}
		// The run resumes on its model only if something can still serve it.
		if _, err := s.models.Model(c.ModelKey); err != nil {
			return creativeops.Outcome{}, translateGateway(err)
		}
		consent, err := readConsentForUpdate(ctx, tx, c.EgressConsentID)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		if err = coversRun(consent, c.ConversationID, nil); err != nil {
			return creativeops.Outcome{}, err
		}
		if err = acquireSlotInTx(ctx, tx, c.ConversationID, c.ID, c.token, now); err != nil {
			return creativeops.Outcome{}, err
		}
		if _, err = s.appendMessageInTx(ctx, tx, c.ConversationID, newMessage{Role: "user", Status: "complete", RunID: c.ID, Body: Body{Blocks: []Block{{Type: "text", Text: in.Text}}}}); err != nil {
			return creativeops.Outcome{}, err
		}
		if err = tx.Insert(ctx, "creative_run_inputs", []string{"run_id", "batch_ordinal", "ordinal", "input_role", "read_level", "body"}, c.ID, batches+1, 0, "instruction", "text", in.Text); err != nil {
			return creativeops.Outcome{}, err
		}
		if _, err = tx.Update(ctx, "creative_agent_runs", "waiting_token=NULL,wait_reason=NULL", "id=$2", c.ID); err != nil {
			return creativeops.Outcome{}, err
		}
		if err = s.enqueueControlledRun(ctx, tx, c, now); err != nil {
			return creativeops.Outcome{}, err
		}
		run, err := scanRun(tx.QueryRow(ctx, "creative_agent_runs", runColumns, "id=$2", c.ID))
		if err != nil {
			return creativeops.Outcome{}, err
		}
		return runOutcome(run, 202)
	}}, command)
}

// Checkpoints before a supplement remain replayable. Message metadata marks
// which immutable batches are already in Eino state; it grants no authority.
func (h *runtimeHandler) BeforeModelRewriteState(ctx context.Context, state *adk.ChatModelAgentState, _ *adk.ModelContext) (context.Context, *adk.ChatModelAgentState, error) {
	seen := map[string]bool{}
	for _, message := range state.Messages {
		if key, ok := message.Extra["creative_supplement"].(string); ok {
			seen[key] = true
		}
	}
	rows, err := h.scope.QueryPage(ctx, "creative_run_inputs", "batch_ordinal,body", "run_id=$2 AND batch_ordinal>0", []store.OrderBy{{Column: "batch_ordinal"}, {Column: "ordinal"}}, maxSupplements+1, 0, h.held.RunID)
	if err != nil {
		return ctx, state, err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
		if count > maxSupplements {
			return ctx, state, ErrLimit
		}
		var batch int
		var body string
		if err = rows.Scan(&batch, &body); err != nil {
			return ctx, state, err
		}
		key := strconv.Itoa(batch)
		if !seen[key] {
			m := schema.UserMessage(body)
			m.Extra = map[string]any{"creative_supplement": key}
			state.Messages = append(state.Messages, m)
		}
	}
	if err = rows.Err(); err != nil && !errors.Is(err, store.ErrNoRows) {
		return ctx, state, err
	}
	limitHistoryImages(state.Messages, h.service.limits.ImagePreviewCount)
	// The in-run guard: a run whose own turns outgrow the window sheds old
	// payloads and whole earlier turns, never the summary (design §5).
	// Tools can change with the current consent or model. Count the current
	// normalized schemas, exactly as the gateway does. System messages and
	// expanded inputs are already in state.Messages, so do not charge them or
	// admission's assembly margin a second time.
	definitions, err := llmgateway.EinoToolDefinitions(state.ToolInfos)
	if err != nil {
		return ctx, state, err
	}
	fixed := toolDefinitionBytes(definitions)
	budget := min(requestByteCeiling(h.service.limits, h.held.Model), h.measure.Ratio.bytesFor(h.held.Model.InputWindowTokens())) - fixed
	state.Messages = compactMessages(state.Messages, budget)
	return ctx, state, nil
}
