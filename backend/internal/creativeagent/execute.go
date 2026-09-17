package creativeagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativeskill"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/jobs"
	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const (
	// leaseDuration is how long a worker's ownership stays valid without being
	// renewed. It is short on purpose: the rescue path that reads it judges a
	// silent worker by this, not by how long the run has been going.
	leaseDuration = 30 * time.Second
	// leaseRenewal keeps the lease alive while a model turn is in flight. A turn
	// is allowed to take the whole run window, so the lease cannot simply be set
	// to the deadline — then it would say nothing about whether anyone is there.
	leaseRenewal = 10 * time.Second
	// Finishing retries persistence only, within its own bounded cleanup window.
	finishTimeout  = 30 * time.Second
	finishAttempts = 3
)

var errEmptyAnswer = errors.New("model returned no deliverable answer")

// agentName is the framework's name for this agent. It is not shown to anyone
// and is not a skill identity.
const agentName = "creative-assistant"

// quotedContentOpen and quotedContentClose mark material the photographer
// located rather than typed. They describe, never instruct: a delimiter that
// told the model what to do with the text would make quoted content a way to
// issue orders.
const (
	quotedContentOpen  = "【引用内容开始】"
	quotedContentClose = "【引用内容结束】"
)

type runTask struct {
	RunID string `json:"run_id"`
}

// claim is one worker's proof that it owns a run right now: the epoch and token
// every later effect is checked against, plus the facts the turn needs.
type claim struct {
	RunID          string
	ConversationID string
	Epoch          int64
	Token          string
	ConsentID      string
	Model          llmgateway.ModelConfig
	Skill          *creativeskill.Snapshot
	Deadline       time.Time
	Limits         Limits
}

// turnOutcome is what one attempt produced, independent of whether it failed.
// A run that delivered an answer and then failed is partial, not failed, so the
// two facts are kept apart.
type turnOutcome struct {
	StepID    string
	RequestID string
	Delivered bool
}

// workRun advances one run from queued to a terminal state. A redelivered task
// finds nothing to claim and does nothing, which is the correct answer rather
// than an error: River guarantees at-least-once delivery, and the run's own
// state is what guarantees at-most-once effect.
func (s *Service) workRun(ctx context.Context, scope store.AccountScope, request jobs.Request) error {
	var task runTask
	if err := json.Unmarshal(request.Payload, &task); err != nil || task.RunID == "" {
		return jobs.ErrInvalidTask
	}
	held, err := s.claimRun(ctx, scope, task.RunID)
	if err != nil {
		if errors.Is(err, ErrRunState) || errors.Is(err, ErrNotFound) {
			return nil
		}
		return err
	}
	execution := *s
	execution.limits = held.Limits
	s = &execution
	attemptCtx, cancelAttempt := context.WithCancel(ctx)
	defer cancelAttempt()
	stop := s.renewLease(attemptCtx, scope, held, cancelAttempt)
	var source *string
	queryErr := scope.QueryRow(attemptCtx, "creative_agent_runs", "source_run_id", "id=$2", held.RunID).Scan(&source)
	var outcome turnOutcome
	var turnErr error
	if queryErr != nil {
		turnErr = queryErr
	} else if source != nil {
		outcome, turnErr = s.executeRecordedRetry(attemptCtx, scope, held)
	} else {
		outcome, turnErr = s.executeTurn(attemptCtx, scope, held)
	}
	stop()
	// The run reaches a terminal state inside the attempt that claimed it. The
	// turn's error is recorded there rather than returned, because returning it
	// would have River redeliver a task whose run is already past queued and
	// leave it running with nobody advancing it.
	return s.completeAttempt(ctx, scope, held, outcome, turnErr)
}

// claimRun takes ownership. The slot is locked before the run, matching the
// global order, and the epoch is bumped so anything still holding the previous
// one can no longer write.
func (s *Service) claimRun(ctx context.Context, scope store.AccountScope, runID string) (claim, error) {
	var held claim
	// Both of these end the run rather than refuse the takeover. Returning an
	// error from inside the transaction would roll the ending back with it, and
	// the run would sit in the queue — or, once the takeover has committed, in
	// running — with nobody left to advance it.
	//
	// They are reported out here rather than as errors for the same reason the
	// commit itself is: the write has to survive.
	ended := ""
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.RequireCreativeCapability(ctx, "agent_start"); err != nil {
			return err
		}
		var slotRun *string
		if err := tx.QueryRowForUpdate(ctx, "creative_agent_slots", "run_id", "").Scan(&slotRun); err != nil {
			if errors.Is(err, store.ErrNoRows) {
				return ErrRunState
			}
			return err
		}
		if slotRun == nil || *slotRun != runID {
			return ErrRunState
		}
		// An in-flight duplicate must stop before taking the budget/run locks:
		// the executing worker may still be inside gateway admission. The slot
		// lock prevents another claimant or finisher from changing ownership.
		var state, token string
		var reservationID *string
		if err := tx.QueryRow(ctx, "creative_agent_runs", "state,initial_llm_reservation_id", "id=$2", runID).
			Scan(&state, &reservationID); err != nil {
			if errors.Is(err, store.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if state != RunQueued {
			return ErrRunState
		}
		// Claim may close an expired recovered run. Lock Gateway facts before
		// the run so a late consumer (request -> run) cannot deadlock it.
		if _, err := s.gateway.LockCallerGroupInTx(ctx, tx, callerService, runID); err != nil {
			return err
		}
		var epoch int64
		var modelKey string
		var skillSnapshot, limitsSnapshot []byte
		var deadline time.Time
		err := tx.QueryRowForUpdate(ctx, "creative_agent_runs",
			"state,execution_epoch,claim_token,model_key,egress_consent_id,conversation_id,skill_snapshot,deadline_at,limits_snapshot",
			"id=$2", runID).Scan(&state, &epoch, &token, &modelKey, &held.ConsentID,
			&held.ConversationID, &skillSnapshot, &deadline, &limitsSnapshot)
		if errors.Is(err, store.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if state != RunQueued || slotRun == nil || *slotRun != runID {
			return ErrRunState
		}
		now, err := tx.CreativeNow(ctx)
		if err != nil {
			return err
		}
		endQueued := func(code string) error {
			run, err := scanRun(tx.QueryRow(ctx, "creative_agent_runs", runColumns, "id=$2", runID))
			if err != nil {
				return err
			}
			// A recovered queued run can already have paid results. The
			// common closer projects locked Gateway facts and delivered work.
			return s.endControlledRun(ctx, tx, controlledRun{Run: run, token: token, epoch: epoch}, RunFailed, code, now)
		}
		if !now.Before(deadline) {
			// The window closed while the task waited in the queue. Nothing is
			// dispatched after it; the run ends here instead of being advanced.
			ended = "creative_run_deadline_exceeded"
			return endQueued(ended)
		}
		// The deployment's own model configuration, resolved before the takeover
		// commits. It is an in-memory catalog read, so it belongs inside this
		// transaction — and it has to be, because a model disabled or dropped
		// while the task queued would otherwise fail after the run is already
		// running and its slot already taken, with no path back to a terminal
		// state: the redelivered task would find nothing to claim and stop.
		model, err := s.models.Model(modelKey)
		if err != nil {
			ended = "creative_model_capability_missing"
			return endQueued(ended)
		}
		if err := json.Unmarshal(limitsSnapshot, &held.Limits); err != nil {
			return err
		}
		held.Model = model
		next, err := randomClaimToken()
		if err != nil {
			return err
		}
		if _, err := tx.Update(ctx, "creative_agent_runs",
			"state=$3, execution_epoch=execution_epoch+1, claim_token=$4, lease_until=$5, started_at=coalesce(started_at,$6), revision=revision+1, updated_at=$6",
			"id=$2 AND claim_token=$7", runID, RunRunning, next, now.Add(leaseDuration), now, token); err != nil {
			return err
		}
		// The slot's copy rotates with the run's, so a release has to present
		// the identity this takeover created rather than the one before it.
		if _, err := tx.Update(ctx, "creative_agent_slots",
			"claim_token=$3, updated_at=$4", "run_id=$2", runID, next, now); err != nil {
			return err
		}
		if _, err := appendRunEventInTx(ctx, tx, runID, "run.state_changed",
			map[string]string{"state": RunRunning}); err != nil {
			return err
		}
		held.RunID, held.Epoch, held.Token, held.Deadline = runID, epoch+1, next, deadline
		if len(skillSnapshot) > 0 {
			var snapshot creativeskill.Snapshot
			if err := json.Unmarshal(skillSnapshot, &snapshot); err != nil {
				return err
			}
			held.Skill = &snapshot
		}
		return nil
	})
	if err != nil {
		return claim{}, err
	}
	if ended != "" {
		s.logger.WarnContext(ctx, "creative agent run ended before it could start",
			slog.String("run_id", runID), slog.String("error_code", ended))
		return claim{}, ErrRunState
	}
	return held, nil
}

// renewLease keeps the claim visibly alive while a turn is in flight and
// returns the function that stops it. It never extends the run's deadline: the
// lease says a worker is present, the deadline says the work may still happen.
func (s *Service) renewLease(ctx context.Context, scope store.AccountScope, held claim, cancelAttempt context.CancelFunc) func() {
	done := make(chan struct{})
	exited := make(chan struct{})
	var once sync.Once
	go func() {
		defer close(exited)
		ticker := time.NewTicker(leaseRenewal)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
					now, err := tx.CreativeNow(ctx)
					if err != nil {
						return err
					}
					// Only the holder renews. A worker whose claim was taken over
					// must not keep a lease alive for somebody else's epoch.
					updated, err := tx.Update(ctx, "creative_agent_runs", "lease_until=$4, updated_at=$5",
						"id=$2 AND claim_token=$3 AND state='running' AND execution_epoch=$6 AND lease_until>$5 AND cancel_requested_at IS NULL", held.RunID, held.Token,
						now.Add(leaseDuration), now, held.Epoch)
					if err == nil && updated == 0 {
						return ErrRunState
					}
					return err
				}); err != nil {
					s.logger.WarnContext(ctx, "creative agent could not renew a run lease",
						slog.String("run_id", held.RunID), slog.String("error", err.Error()))
					cancelAttempt()
					return
				}
			}
		}
	}()
	return func() { once.Do(func() { close(done); cancelAttempt() }); <-exited }
}

// executeTurn runs the bounded model/tool loop through the framework. Everything the
// deployment's capability decides is checked here, before any bytes move.
func (s *Service) executeTurn(ctx context.Context, scope store.AccountScope, held claim) (turnOutcome, error) {
	ctx, cancel := context.WithDeadline(ctx, held.Deadline)
	defer cancel()
	execution, err := s.newRunExecution(ctx, scope, held)
	if err != nil {
		return turnOutcome{}, err
	}
	return execution.run(ctx)
}

// currentStep carries the step identity from the turn key to the consumer. The
// two run on the framework's goroutines, so the handoff is guarded rather than
// left to the call chain's implied ordering.
type currentStep struct {
	mu         sync.Mutex
	stepID     string
	bindingKey string
}

func (c *currentStep) set(stepID, bindingKey string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stepID, c.bindingKey = stepID, bindingKey
}

func (c *currentStep) get() (stepID, bindingKey string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stepID, c.bindingKey
}

// instructionOf is the system prompt. A run without a skill has none: an
// invented default would be instructions nobody wrote or reviewed.
func instructionOf(skill *creativeskill.Snapshot) string {
	if skill == nil {
		return ""
	}
	return skill.Instructions
}

// runTokenCeiling is the most one run may ever consume: one request per model
// turn, each bounded by the run's own input ceiling and the model's output one.
// It is derived rather than configured so it cannot drift from the limits the
// run was created under.
func runTokenCeiling(limits Limits, model llmgateway.ModelConfig) int64 {
	return int64(limits.ModelTurns) * int64(limits.InputTextBytes+model.Capability.MaxOutputTokens)
}

// stillRunnable re-reads the skill through the directory port, with no cache.
// A version withdrawn between creating the run and dispatching it must stop the
// dispatch — which is exactly why the frozen snapshot on the run cannot answer
// this question, even though it is the snapshot that supplies the instructions.
func (s *Service) stillRunnable(ctx context.Context, scope store.AccountScope, held claim) error {
	if held.Skill == nil {
		return nil
	}
	if !held.Model.Capability.ToolCalling {
		return ErrModelCapability
	}
	live, err := s.skills.ResolveVersion(ctx, scope, held.Skill.SkillID, held.Skill.ID)
	if err != nil {
		return err
	}
	if !live.Usable() {
		return ErrUnsupportedSegment
	}
	if available, _ := runnableHere(s.registeredToolRefs(), live.Manifest.ToolAllowlist); !available {
		return ErrUnsupportedSegment
	}
	if live.Digest != held.Skill.Digest {
		// A version is immutable, so this cannot happen without something having
		// gone wrong underneath. Refusing is cheaper than sending instructions
		// that no longer match what the run recorded following.
		return creativeskill.ErrContentMismatch
	}
	return nil
}

// assemble turns the frozen inputs into the messages that will be sent, and
// reports the revisions among them so the dispatch check can re-examine each.
//
// Nothing here re-reads the conversation: the inputs were fixed when the run
// was created, and a message appended since belongs to the next run.
func (s *Service) assemble(ctx context.Context, scope store.AccountScope, held claim) ([]*schema.Message, []string, error) {
	var messages []*schema.Message
	var instruction strings.Builder
	var revisions []string
	seenRevisions := make(map[string]bool)
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		rows, err := tx.QueryPage(ctx, "creative_run_inputs", "input_role,body,content_revision_id,source_revision_snapshot",
			"run_id=$2 AND batch_ordinal=$3", []store.OrderBy{{Column: "ordinal"}}, s.limits.InstructionSegments+s.limits.HistoryMessages, 0,
			held.RunID, initialInputBatch)
		if err != nil {
			return err
		}
		defer rows.Close()
		type frozen struct {
			role       string
			body       *string
			revisionID *string
			snapshot   []byte
		}
		var items []frozen
		for rows.Next() {
			var item frozen
			if err := rows.Scan(&item.role, &item.body, &item.revisionID, &item.snapshot); err != nil {
				return err
			}
			items = append(items, item)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		for _, item := range items {
			switch {
			case item.role == "history":
				role := "user"
				if len(item.snapshot) > 0 {
					var meta struct {
						Role string `json:"role"`
					}
					if err := json.Unmarshal(item.snapshot, &meta); err == nil && meta.Role != "" {
						role = meta.Role
					}
				}
				if item.body == nil {
					continue
				}
				if role == "assistant" {
					messages = append(messages, schema.AssistantMessage(*item.body, nil))
					continue
				}
				messages = append(messages, schema.UserMessage(*item.body))
			case item.body != nil:
				appendParagraph(&instruction, *item.body)
			case item.revisionID != nil:
				// The located material is read here, under this account's scope,
				// and its display permission is checked again. A revision is
				// immutable, so the words cannot have changed; the permission to
				// show them can have been taken back.
				revision, err := creativecontent.RequireUsable(ctx, tx, *item.revisionID, "display")
				if err != nil {
					return translateContent(err)
				}
				if revision.Kind != "text" || revision.Payload.Body == nil {
					return ErrUnsupportedSegment
				}
				appendParagraph(&instruction, quotedContentOpen+"\n"+*revision.Payload.Body+"\n"+quotedContentClose)
				if !seenRevisions[*item.revisionID] {
					revisions = append(revisions, *item.revisionID)
					seenRevisions[*item.revisionID] = true
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	if instruction.Len() == 0 {
		// A run with nothing to say is refused rather than sent as an empty
		// prompt, which some deployments answer and others reject.
		return nil, nil, creativeops.ErrValidation
	}
	return append(messages, schema.UserMessage(instruction.String())), revisions, nil
}

func appendParagraph(b *strings.Builder, text string) {
	if b.Len() > 0 {
		b.WriteString("\n\n")
	}
	b.WriteString(text)
}

// admitDispatchInTx is the last gate, run inside the dispatch transaction. The
// run is read before the consent and the consent before the content, matching
// the global order.
func (s *Service) admitDispatchInTx(ctx context.Context, tx store.TxAccountScope, held claim, revisions []string) error {
	if err := tx.RequireCreativeCapability(ctx, "agent_start"); err != nil {
		return err
	}
	now, err := tx.CreativeNow(ctx)
	if err != nil {
		return err
	}
	if !now.Before(held.Deadline) {
		return llmgateway.ErrDeadline
	}
	var state string
	var epoch int64
	var cancelRequested, leaseUntil *time.Time
	err = tx.QueryRowForUpdate(ctx, "creative_agent_runs", "state,execution_epoch,cancel_requested_at,lease_until",
		"id=$2 AND claim_token=$3", held.RunID, held.Token).Scan(&state, &epoch, &cancelRequested, &leaseUntil)
	if errors.Is(err, store.ErrNoRows) {
		return ErrRunState
	}
	if err != nil {
		return err
	}
	if state != RunRunning || epoch != held.Epoch || cancelRequested != nil || leaseUntil == nil || !now.Before(*leaseUntil) {
		return ErrRunState
	}
	// The skill control lock, between the run and the consent: the design puts
	// the skill control row after operation/account/slot/budget/conversation/run
	// and before the resource locks, and the dispatch intent this transaction is
	// about to record must not be able to overtake a withdrawal.
	if held.Skill != nil {
		if err := s.skills.RequireRunnableInTx(ctx, tx, held.Skill.SkillID, held.Skill.ID); err != nil {
			return translateSkill(err)
		}
	}
	consent, err := readConsentForUpdate(ctx, tx, held.ConsentID)
	if err != nil {
		return err
	}
	// The content refs were matched against this consent when the run was
	// created and a consent's scope never changes afterwards, so what is
	// re-checked here is whether it still stands at all.
	if err := coversRun(consent, held.ConversationID, held.Model.VendorKey, nil); err != nil {
		return err
	}
	for _, revisionID := range revisions {
		if _, err := creativecontent.RequireUsable(ctx, tx, revisionID, "display"); err != nil {
			return translateContent(err)
		}
	}
	return nil
}

// prepareModelStep fixes this turn as a persisted step before anything is sent.
// The ordinal comes from the run's own counter under its row lock, so two
// workers cannot hand out the same position.
func (s *Service) prepareModelStep(ctx context.Context, scope store.AccountScope, held claim, previousModel string,
	chat llmgateway.ChatRequest) (stepID, bindingKey string, err error) {
	// The 256 KiB input contract, checked on the request as assembled — the
	// system prompt the skill contributed, the history, the quoted bodies, the
	// separators and the tool definitions, all of it.
	//
	// It cannot be checked when the run is created: what is counted there is a
	// content_ref's identifier, while what is sent is the revision's whole text,
	// and one legitimate reference can be larger than the entire budget. Nor can
	// the gateway stand in for it — the gateway reserves against whatever it is
	// handed, which is a different question from whether this deployment agreed
	// to send that much.
	//
	// EstimateInputTokens is the gateway's own accounting, borrowed rather than
	// reproduced: measuring the hold one way and the ceiling another is how a
	// request passes one and fails the other.
	if bytes := llmgateway.EstimateInputTokens(chat); bytes > int64(s.limits.InputTextBytes) {
		return "", "", fmt.Errorf("%w: 本次请求 %d 字节，上限 %d", ErrLimit, bytes, s.limits.InputTextBytes)
	}
	hash, err := llmgateway.RequestHash(chat, s.models.Snapshot(held.Model))
	if err != nil {
		return "", "", translateGateway(err)
	}
	payload, err := json.Marshal(chat)
	if err != nil {
		return "", "", err
	}
	err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		var ordinal int64
		var state string
		var epoch int64
		if err := tx.QueryRowForUpdate(ctx, "creative_agent_runs", "next_step_ordinal,state,execution_epoch",
			"id=$2 AND claim_token=$3", held.RunID, held.Token).Scan(&ordinal, &state, &epoch); err != nil {
			if errors.Is(err, store.ErrNoRows) {
				return ErrRunState
			}
			return err
		}
		if state != RunRunning || epoch != held.Epoch {
			return ErrRunState
		}
		var previousOrdinal int64
		if previousModel != "" {
			if err := tx.QueryRow(ctx, "creative_agent_steps", "ordinal", "id=$2 AND run_id=$3 AND kind='model'", previousModel, held.RunID).Scan(&previousOrdinal); err != nil {
				return errCheckpointJournal
			}
		}
		// The predecessor frame selects the next recorded turn. Identical
		// prompts in different positions remain different paid calls.
		rows, err := tx.QueryPage(ctx, "creative_agent_steps", "id,ordinal,input_hash", "run_id=$2 AND kind='model' AND ordinal>$3", []store.OrderBy{{Column: "ordinal"}}, 1, 0, held.RunID, previousOrdinal)
		if err != nil {
			return err
		}
		var existingOrdinal int64
		var existingHash string
		if rows.Next() {
			err = rows.Scan(&stepID, &existingOrdinal, &existingHash)
		}
		rowErr := rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if rowErr != nil {
			return rowErr
		}
		if stepID != "" {
			if existingHash != hash {
				return errCheckpointJournal
			}
			bindingKey = turnBindingKey(held.RunID, existingOrdinal)
			return nil
		}
		count, err := tx.Count(ctx, "creative_agent_steps", "run_id=$2 AND kind='model'", held.RunID)
		if err != nil {
			return err
		}
		if count >= int64(s.limits.ModelTurns) {
			return errModelLimit
		}
		pending, err := tx.Count(ctx, "creative_agent_steps", "run_id=$2 AND kind='tool' AND state='prepared'", held.RunID)
		if err != nil {
			return err
		}
		if pending > 0 {
			return ErrRunState
		}
		if stepID, err = creativeops.NewResourceID("ccst"); err != nil {
			return err
		}
		bindingKey = turnBindingKey(held.RunID, ordinal)
		now, err := tx.CreativeNow(ctx)
		if err != nil {
			return err
		}
		if err := tx.Insert(ctx, "creative_agent_steps",
			[]string{"id", "run_id", "ordinal", "kind", "state", "execution_epoch", "input_hash", "input", "started_at"},
			stepID, held.RunID, ordinal, "model", "prepared", held.Epoch, hash, payload, now); err != nil {
			return err
		}
		if _, err := tx.Update(ctx, "creative_agent_runs",
			"next_step_ordinal=next_step_ordinal+1, updated_at=$3", "id=$2", held.RunID, now); err != nil {
			return err
		}
		_, err = appendRunEventInTx(ctx, tx, held.RunID, "step.state_changed",
			map[string]string{"step_id": stepID, "kind": "model", "state": "prepared"})
		return err
	})
	if err != nil {
		return "", "", err
	}
	return stepID, bindingKey, nil
}

// consumeResultInTx joins this run's own records to the transaction that marks
// the gateway result consumed. The step result and the message the photographer
// will read commit together with that mark, so there is no state in which the
// answer was paid for, consumed, and not visible.
func (s *Service) consumeResultInTx(ctx context.Context, tx store.TxAccountScope, held claim,
	stepID string, result llmgateway.Result) (bool, error) {
	if stepID == "" {
		return false, ErrRunState
	}
	output, err := json.Marshal(map[string]any{
		"finish_reason": string(result.FinishReason),
		"model":         result.Model,
		"text_bytes":    len(result.Text),
		"tool_calls":    result.ToolCalls,
	})
	if err != nil {
		return false, err
	}
	now, err := tx.CreativeNow(ctx)
	if err != nil {
		return false, err
	}
	if _, err := tx.Update(ctx, "creative_agent_steps",
		"state='succeeded', output=$3, finished_at=$4, updated_at=$4",
		"id=$2 AND state='prepared'", stepID, output, now); err != nil {
		return false, err
	}
	text := strings.TrimSpace(result.Text)
	if text == "" || len(result.ToolCalls) > 0 || !result.Consumable() {
		// An answer with no words is recorded as a step and produces no message.
		// Writing an empty one would put a blank turn in the conversation the
		// photographer cannot act on.
		return false, nil
	}
	message, err := s.appendMessageInTx(ctx, tx, held.ConversationID, newMessage{
		Role: "assistant", Status: "complete", RunID: held.RunID, SourceStepID: stepID,
		Body: Body{Blocks: []Block{{Type: "text", Text: text}}},
	})
	if err != nil {
		return false, err
	}
	_, err = appendRunEventInTx(ctx, tx, held.RunID, "message.completed",
		map[string]string{"message_id": message.ID, "step_id": stepID})
	return true, err
}

// finishRun records the outcome and releases ownership after the model turn.
// Only confirmed transaction rollbacks are retried; database outages still
// require the recovery path to finish the persisted run later.
func (s *Service) finishRun(ctx context.Context, scope store.AccountScope, held claim, outcome turnOutcome, turnErr error) error {
	// The provider call has ended. Cancellation of that call must not also
	// cancel the short transaction that releases its slot and records the result.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), finishTimeout)
	defer cancel()
	delivered, err := scope.Count(ctx, "creative_agent_messages", "run_id=$2 AND role='assistant' AND status='complete'", held.RunID)
	if err != nil {
		return err
	}
	outcome.Delivered = outcome.Delivered || delivered > 0
	if turnErr == nil && !outcome.Delivered {
		// Read-only runtime tools do not deliver an answer: a provider success is
		// not delivery. Keep consumed results and accounting, but fail the run.
		turnErr = errEmptyAnswer
	}
	state, code := RunSucceeded, ""
	if turnErr != nil {
		state, code = RunFailed, failureCode(turnErr)
		if outcome.Delivered {
			// Words already reached the photographer. Calling that a failure
			// would deny something they can see on their own screen.
			state = RunPartial
		}
		s.logger.WarnContext(ctx, "creative agent run did not complete",
			slog.String("run_id", held.RunID), slog.String("error_code", code),
			slog.String("error", turnErr.Error()))
	}
	settlement := s.settlementOfRun(ctx, scope, held.RunID, outcome.RequestID)
	for attempt := 0; attempt < finishAttempts; attempt++ {
		err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
			now, err := tx.CreativeNow(ctx)
			if err != nil {
				return err
			}
			closed, err := s.closeRunInTx(ctx, tx, runCloseGuard{RunID: held.RunID, Token: held.Token, Epoch: held.Epoch, State: RunRunning}, state, code, settlement, now)
			if err != nil || !closed {
				return err
			}
			if turnErr != nil {
				if _, err := tx.Update(ctx, "creative_agent_steps", "state='failed',error_code=$4,finished_at=$5,updated_at=$5", "run_id=$2 AND execution_epoch=$3 AND kind='tool' AND state='prepared'", held.RunID, held.Epoch, code, now); err != nil {
					return err
				}
			}
			if outcome.StepID == "" {
				return nil
			}
			// The cross-reference is filled after the fact because the request id
			// only exists once the gateway has prepared it. It is an index, not the
			// authority: the binding the gateway stored is derived from the step id,
			// so the link survives even if this update never happens.
			if outcome.RequestID != "" {
				if _, err := tx.Update(ctx, "creative_agent_steps", "llm_request_id=$3, updated_at=$4",
					"id=$2 AND llm_request_id IS NULL", outcome.StepID, outcome.RequestID, now); err != nil {
					return err
				}
			}
			if turnErr == nil {
				return nil
			}
			stepState := "failed"
			if errors.Is(turnErr, llmgateway.ErrUnknown) {
				stepState = "unknown"
			}
			if _, err := tx.Update(ctx, "creative_agent_steps", "state=$3,error_code=$4,finished_at=$5,updated_at=$5", "id=$2 AND state='prepared'", outcome.StepID, stepState, failureCode(turnErr), now); err != nil {
				return err
			}
			return nil
		})
		// Only retry errors that prove the transaction rolled back. A failed
		// COMMIT acknowledgement is not permission to repeat unknown effects.
		if !store.IsSerializationFailure(err) || ctx.Err() != nil {
			return err
		}
	}
	return err
}

// settlementOf projects the gateway's money fact onto the run. It is a
// projection and may lag: the gateway keeps updating it after the run is over,
// and a terminal run never reopens because of what it says.
func (s *Service) settlementOf(ctx context.Context, scope store.AccountScope, requestID string) string {
	if requestID == "" {
		return "not_started"
	}
	view, err := s.gateway.Get(ctx, scope, requestID)
	if err != nil {
		return "unknown"
	}
	switch view.Settlement {
	case llmgateway.SettlementSettled, llmgateway.SettlementReleased:
		return "settled"
	case llmgateway.SettlementUnknown:
		return "unknown"
	case llmgateway.SettlementUnclaimed:
		return "not_started"
	default:
		return "pending"
	}
}

// A terminal transition must match the control state as well as the worker
// token: the controller invalidates an epoch before the next token is issued.
type runCloseGuard struct {
	RunID string
	Token string
	Epoch int64
	State string
}

// closeRunInTx ends a run, gives the slot back and gives back any budget the
// run reserved but never spent. The release names the run and the token
// together, so a worker whose claim was taken over cannot free an occupancy
// that now belongs to somebody else.
//
// Every terminal path comes through here, which is the point: the hold taken at
// creation outlives the run otherwise, and an account whose runs all end before
// dispatch would find its month spent without one model call having been made.
func (s *Service) closeRunInTx(ctx context.Context, tx store.TxAccountScope, expected runCloseGuard, state, code, settlement string, now time.Time) (bool, error) {
	runID, token := expected.RunID, expected.Token
	if settlement == "" {
		settlement = "not_started"
	}
	// All terminal paths take slot -> budget -> run. Updating run before
	// locking slot deadlocks with a concurrent claimant holding slot -> run.
	var slotRun, slotToken *string
	if err := tx.QueryRowForUpdate(ctx, "creative_agent_slots", "run_id,claim_token", "").Scan(&slotRun, &slotToken); err != nil {
		return false, err
	}
	if slotRun == nil || *slotRun != runID || slotToken == nil || *slotToken != token {
		return false, nil
	}
	// Ownership is stable under the slot lock. Check it before releasing any
	// group admission: an old claimant must not cancel a later epoch's work.
	var currentToken, currentState string
	var currentEpoch int64
	var finished *time.Time
	err := tx.QueryRow(ctx, "creative_agent_runs", "claim_token,execution_epoch,state,finished_at", "id=$2", runID).Scan(&currentToken, &currentEpoch, &currentState, &finished)
	if errors.Is(err, store.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if currentToken != token || currentEpoch != expected.Epoch || currentState != expected.State || finished != nil {
		return false, nil
	}
	if err := s.gateway.ReleaseUnusedGroupInTx(ctx, tx, callerService, runID); err != nil {
		return false, err
	}
	updated, err := tx.Update(ctx, "creative_agent_runs",
		"state=$3, error_code=$4, settlement_state=$5, finished_at=$6, lease_until=NULL, revision=revision+1, updated_at=$6",
		"id=$2 AND claim_token=$7 AND execution_epoch=$8 AND state=$9 AND finished_at IS NULL", runID, state, nullableText(code), settlement, now, token, expected.Epoch, expected.State)
	if err != nil {
		return false, err
	}
	if updated == 0 {
		// The guarded transition did not commit. Roll back budget cleanup too;
		// a no-op close must never leave earlier writes behind.
		return false, ErrRunState
	}
	if err := s.gateway.AbandonCallerGroupResultsInTx(ctx, tx, callerService, runID); err != nil {
		return false, err
	}
	if err := releaseSlotInTx(ctx, tx, runID, token, now); err != nil {
		return false, err
	}
	// Checkpoint and context lifetimes share the run's terminal clock. Pending
	// or unknown accounting evidence still needs the later cleanup controller's
	// explicit preservation checks; this timestamp alone is not GC permission.
	for _, table := range []string{"creative_agent_checkpoints", "creative_agent_context_items"} {
		if _, err := tx.Update(ctx, table, "retained_until=GREATEST(retained_until,$3)", "run_id=$2", runID, now.Add(contextRetention)); err != nil {
			return false, err
		}
	}
	_, err = appendRunEventInTx(ctx, tx, runID, "run.finished",
		map[string]string{"state": state, "error_code": code, "settlement_state": settlement})
	return err == nil, err
}

func nullableText(value string) any {
	if value == "" {
		return nil
	}
	return value
}

// failureCode is what the photographer is told. Each case names something they
// can act on; anything unrecognised stays generic rather than leaking the
// internals of a failure they cannot do anything about.
func failureCode(err error) string {
	switch {
	case errors.Is(err, errCheckpointMissing):
		return "creative_checkpoint_missing"
	case errors.Is(err, errCheckpointVersion):
		return "creative_checkpoint_incompatible"
	case errors.Is(err, errCheckpointJournal), errors.Is(err, errCheckpointConflict):
		return "creative_checkpoint_inconsistent"
	case errors.Is(err, errRunInterrupted):
		return "creative_run_interrupted"
	case errors.Is(err, errToolLimit):
		return "creative_tool_limit"
	case errors.Is(err, errModelLimit):
		return "creative_model_turn_limit"
	case errors.Is(err, errToolDenied):
		return "creative_tool_not_allowed"
	case errors.Is(err, errEmptyAnswer):
		return "creative_model_empty_result"
	case errors.Is(err, ErrConsentRevoked):
		return "creative_egress_revoked"
	case errors.Is(err, ErrEgressRequired):
		return "creative_egress_required"
	case errors.Is(err, ErrUnsupportedSegment):
		return "creative_skill_unavailable"
	case errors.Is(err, ErrBudgetExceeded):
		return "creative_llm_budget_exceeded"
	case errors.Is(err, ErrModelCapability):
		return "creative_model_capability_missing"
	case errors.Is(err, ErrRunState):
		return "creative_run_superseded"
	case errors.Is(err, llmgateway.ErrUnknown):
		return "creative_model_result_unknown"
	case errors.Is(err, llmgateway.ErrDeadline), errors.Is(err, context.DeadlineExceeded):
		return "creative_run_deadline_exceeded"
	case errors.Is(err, ErrLimit):
		return "creative_context_limit"
	case errors.Is(err, ErrNotFound):
		return "creative_input_unavailable"
	default:
		return "creative_run_failed"
	}
}

// drainAgent reads the framework's event stream to the end. The assistant text
// is not taken from here: it is whatever the gateway persisted and this run
// already consumed, so a stream that ends early cannot invent an answer.
func drainAgent(it *adk.AsyncIterator[*adk.AgentEvent]) error {
	var first error
	for {
		event, ok := it.Next()
		if !ok {
			return first
		}
		if event.Err != nil && first == nil {
			first = event.Err
		}
		if event.Action != nil && event.Action.Interrupted != nil && first == nil {
			first = errRunInterrupted
		}
	}
}

func (s *Service) settlementOfRun(ctx context.Context, scope store.AccountScope, runID, lastRequest string) string {
	rows, err := scope.QueryPage(ctx, "creative_agent_steps", "llm_request_id,ordinal", "run_id=$2 AND kind='model'", []store.OrderBy{{Column: "ordinal"}}, s.limits.ModelTurns, 0, runID)
	if err != nil {
		return "unknown"
	}
	type requestLink struct {
		id      *string
		ordinal int64
	}
	var links []requestLink
	for rows.Next() {
		var link requestLink
		if err = rows.Scan(&link.id, &link.ordinal); err != nil {
			rows.Close()
			return "unknown"
		}
		links = append(links, link)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return "unknown"
	}
	requests := map[string]bool{}
	for _, link := range links {
		if link.id != nil {
			requests[*link.id] = true
			continue
		}
		// The request may have reached the provider before its secondary index was
		// written. A missing checkpoint must not turn that unknown bill into zero.
		view, err := s.gateway.RequestForBinding(ctx, scope, callerService, turnBindingKey(runID, link.ordinal))
		if errors.Is(err, llmgateway.ErrNotFound) {
			continue
		}
		if err != nil {
			return "unknown"
		}
		requests[view.ID] = true
	}
	if lastRequest != "" {
		requests[lastRequest] = true
	}
	state := "not_started"
	for id := range requests {
		next := s.settlementOf(ctx, scope, id)
		if next == "unknown" {
			return next
		}
		if next == "pending" || state == "pending" {
			state = "pending"
		} else if next == "settled" {
			state = "settled"
		}
	}
	return state
}
