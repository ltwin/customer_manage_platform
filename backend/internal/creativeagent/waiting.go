package creativeagent

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
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
		Segments     []InstructionSegment `json:"instruction_segments"`
	}
	var resolved ResolvedInstruction
	var resolveErr error
	if err := creativeops.Decode(command.Payload, &in); err == nil {
		resolved, resolveErr = s.resolveInput(ctx, scope, in.Text, in.Segments)
	}
	return (creativeops.Executor{}).Run(ctx, scope, creativeops.Operation{Key: "agent.supplement_run", Capability: "agent_start", Validate: func(raw json.RawMessage) error {
		if err := creativeops.Decode(raw, &in); err != nil {
			return err
		}
		// Exactly one of text and instruction_segments; an empty list given
		// is still given.
		if in.RunID == "" || in.WaitingToken == "" || in.Revision < 1 || (in.Text == "") == (in.Segments == nil) || in.Segments != nil && len(in.Segments) == 0 {
			return creativeops.ErrValidation
		}
		if in.Text != "" && strings.TrimSpace(in.Text) == "" {
			return creativeops.ErrValidation
		}
		if len(in.Text) > s.limits.MessageBodyBytes {
			return ErrLimit
		}
		return resolveErr
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
		if err = s.appendInputBatchInTx(ctx, tx, c, resolved, consent); err != nil {
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

// resolveInput reads what a photographer puts into a run that is already going:
// words and references only. The run's Skill and commands were fixed when it
// started. A bare text is the supplement's original form.
func (s *Service) resolveInput(ctx context.Context, scope store.AccountScope, text string, segments []InstructionSegment) (ResolvedInstruction, error) {
	if text != "" {
		segments = []InstructionSegment{{Type: "text", Text: text}}
	}
	for _, segment := range segments {
		switch segment.Type {
		case "text":
			if strings.TrimSpace(segment.Text) == "" {
				return ResolvedInstruction{}, creativeops.ErrValidation
			}
		case "content_ref":
		default:
			return ResolvedInstruction{}, creativeops.ErrValidation
		}
	}
	return s.ResolveInstruction(ctx, scope, segments)
}

// appendInputBatchInTx puts one message into a run that is going: the
// conversation message, a batch of run inputs and the input block that shows
// it in the turn, in one transaction. The caller holds the run.
//
// A reference the photographer attached here is covered by the account's
// agreement to the send notice (owner 2026-11-11), not by the run's list of
// revisions; it must still be this account's, usable, and of a class the
// consent lets leave.
func (s *Service) appendInputBatchInTx(ctx context.Context, tx store.TxAccountScope, c controlledRun, resolved ResolvedInstruction, consent Consent) error {
	var batches int
	rows, err := tx.QueryPage(ctx, "creative_run_inputs", "batch_ordinal", "run_id=$2", []store.OrderBy{{Column: "batch_ordinal", Desc: true}}, 1, 0, c.ID)
	if err != nil {
		return err
	}
	if rows.Next() {
		err = rows.Scan(&batches)
	}
	rowErr := rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if rowErr != nil {
		return rowErr
	}
	if batches >= maxSupplements {
		return ErrLimit
	}
	// The run goes on with its model only if something can still serve it.
	model, err := s.models.Model(c.ModelKey)
	if err != nil {
		return translateGateway(err)
	}
	levels := map[string]string{}
	images := 0
	for _, ref := range resolved.ContentRefs {
		revision, err := creativecontent.RequireUsable(ctx, tx, ref.RevisionID, "display")
		if err != nil {
			return translateContent(err)
		}
		levels[ref.RevisionID] = "text"
		if revision.Kind != "image" {
			continue
		}
		levels[ref.RevisionID] = "image_preview"
		images++
		if images > s.limits.ImagePreviewCount {
			return ErrLimit
		}
		if !model.Capability.ImageInput || s.images == nil {
			return ErrModelCapability
		}
		if !consentHas(consent, "image_preview") {
			return ErrEgressRequired
		}
	}
	message, err := s.appendMessageInTx(ctx, tx, c.ConversationID, newMessage{Role: "user", Status: "complete", RunID: c.ID, Body: resolved.Body, ContentRefs: resolved.ContentRefs})
	if err != nil {
		return err
	}
	batch := batches + 1
	var inputs [][]any
	for i, block := range resolved.Body.Blocks {
		switch block.Type {
		case "text":
			text := block.Text
			inputs = append(inputs, []any{c.ID, batch, i, "instruction", "text", &text, nil, nil})
		case "content_ref":
			// The label is the body too, so a worker that reads only body
			// still gets words instead of a NULL.
			id, label := block.RefID, referenceLabel(block.RefID)
			inputs = append(inputs, []any{c.ID, batch, i, "instruction", levels[id], &label, &id, nil})
		}
	}
	if err = tx.InsertRows(ctx, "creative_run_inputs", inputColumns, inputs); err != nil {
		return err
	}
	_, err = writeBlockInTx(ctx, tx, c.ID, inputSourceKey(batch), blockChange{Type: BlockInput, State: "pending", Set: inputPayload{MessageID: message.ID, Body: resolved.Body}})
	return err
}

func inputSourceKey(batch int) string { return "input:" + strconv.Itoa(batch) }

// markInputsDeliveredInTx records that a request carries these batches: from
// here they are the model's to answer. Marking again changes nothing; a
// supplement written before input blocks existed has none to mark.
func markInputsDeliveredInTx(ctx context.Context, tx store.TxAccountScope, runID string, batches []int) error {
	for _, batch := range batches {
		shown, err := tx.Exists(ctx, "creative_agent_blocks", "run_id=$2 AND source_key=$3", runID, inputSourceKey(batch))
		if err != nil {
			return err
		}
		if !shown {
			continue
		}
		if _, err := writeBlockInTx(ctx, tx, runID, inputSourceKey(batch), blockChange{Type: BlockInput, State: "delivered"}); err != nil {
			return err
		}
	}
	return nil
}

// deliveredInputRevisions are the references of batches a model request has
// already carried. A resumed run finds them in its checkpoint, not through
// unseenInputs, so the dispatch check has to be told about them here. One
// that went as a bare label is checked too: refusing is the safe side.
func deliveredInputRevisions(ctx context.Context, scope store.AccountScope, runID string) ([]string, error) {
	rows, err := scope.Query(ctx, "creative_run_inputs", "content_revision_id", `run_id=$2 AND batch_ordinal>0 AND content_revision_id IS NOT NULL
AND EXISTS (SELECT 1 FROM creative_agent_blocks b WHERE b.account_id=$1 AND b.run_id=$2 AND b.type='input' AND b.state='delivered' AND b.source_key='input:'||batch_ordinal)`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
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
	messages, batches, err := h.unseenInputs(ctx, seen)
	if err != nil {
		return ctx, state, err
	}
	state.Messages = append(state.Messages, messages...)
	h.setDelivery(batches)
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

// unseenInputs turns the batches put into the run since the framework last
// saw them into messages, in order: a text row is one message, a reference is
// the message resolveReference builds for it, as the first batch's are. A
// reference is read under the run's consent and joins the revisions every
// later dispatch checks again.
func (h *runtimeHandler) unseenInputs(ctx context.Context, seen map[string]bool) ([]*schema.Message, []int, error) {
	var messages []*schema.Message
	var batches []int
	err := h.scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		rows, err := tx.QueryPage(ctx, "creative_run_inputs", "batch_ordinal,body,content_revision_id", "run_id=$2 AND batch_ordinal>0",
			[]store.OrderBy{{Column: "batch_ordinal"}, {Column: "ordinal"}}, maxSupplements*maxBlocks+1, 0, h.held.RunID)
		if err != nil {
			return err
		}
		type input struct {
			batch    int
			body, id *string
		}
		var inputs []input
		for rows.Next() {
			var in input
			if err = rows.Scan(&in.batch, &in.body, &in.id); err != nil {
				rows.Close()
				return err
			}
			inputs = append(inputs, in)
		}
		err = rows.Err()
		rows.Close()
		if err != nil && !errors.Is(err, store.ErrNoRows) {
			return err
		}
		if len(inputs) > 0 && inputs[len(inputs)-1].batch > maxSupplements {
			return ErrLimit
		}
		var consent *Consent
		for _, in := range inputs {
			key := strconv.Itoa(in.batch)
			if seen[key] {
				continue
			}
			var m *schema.Message
			switch {
			case in.id != nil:
				if consent == nil {
					c, err := readConsentForUpdate(ctx, tx, h.held.ConsentID)
					if err != nil {
						return err
					}
					consent = &c
				}
				ref, sent, err := h.service.resolveReference(ctx, tx, h.held.RunID, *in.id, *consent, h.held.Model.Capability.ImageInput)
				if err != nil {
					return err
				}
				if sent && !slices.Contains(h.revisions, *in.id) {
					h.revisions = append(h.revisions, *in.id)
				}
				m = ref
			case in.body != nil:
				m = schema.UserMessage(*in.body)
			default:
				continue
			}
			if m.Extra == nil {
				m.Extra = map[string]any{}
			}
			m.Extra["creative_supplement"] = key
			messages = append(messages, m)
			if len(batches) == 0 || batches[len(batches)-1] != in.batch {
				batches = append(batches, in.batch)
			}
		}
		return nil
	})
	return messages, batches, err
}
