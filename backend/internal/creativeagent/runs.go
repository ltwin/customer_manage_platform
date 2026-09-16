package creativeagent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativeskill"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/jobs"
	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

var (
	// ErrBusy means this account's single write slot is already held. It is not
	// a failure of the request: the same words submitted after the current run
	// finishes will be accepted.
	ErrBusy = errors.New("creative agent write slot is busy")
	// ErrBudgetExceeded is the gateway's refusal translated into this package's
	// vocabulary, the same way the content store's refusals are — llmgateway's
	// own sentinels are not mapped at the HTTP edge.
	ErrBudgetExceeded = errors.New("creative agent budget exceeded")
	// ErrModelCapability covers both "this deployment disabled the model" and
	// "the model cannot serve this request". A photographer can act on either by
	// choosing another model, and neither is a malformed request.
	ErrModelCapability = errors.New("creative agent model cannot serve this request")
	// ErrEgressRequired means the authorisation on file does not cover what this
	// run would send. It is deliberately distinct from ErrConsentRevoked: one
	// was never granted, the other was taken back.
	ErrEgressRequired = errors.New("creative egress consent does not cover this run")
	// ErrRunState is a run asked to do something its current state forbids.
	ErrRunState = errors.New("creative run state does not allow this")
)

// BusyError names the run that actually holds the slot, so the photographer is
// told which piece of work to wait for rather than just "busy". It answers to
// errors.Is(err, ErrBusy) so callers that do not need the id stay simple.
type BusyError struct{ ActiveRunID string }

func (BusyError) Error() string        { return ErrBusy.Error() }
func (BusyError) Is(target error) bool { return target == ErrBusy }

// callerService is this application's name in the gateway's ledger. It is fixed
// forever: the reservation and request identities are derived from it, so a
// change would detach every recorded hold from the run that owns it. The
// underscore is not a style choice — the ledger's own column refuses anything
// outside [a-z0-9_].
const callerService = "creative_agent"

// jobRunKind is the queue task that advances one run. The payload carries the
// run id and nothing else; the worker re-reads every fact from the database.
const jobRunKind = "creative_agent_run"

const runEventSchemaVersion = 1

// Run states. Only a subset is written in this milestone; the set is the
// column's domain, declared whole so a later milestone adds a transition rather
// than discovering the vocabulary.
const (
	RunQueued       = "queued"
	RunRunning      = "running"
	RunWaitingInput = "waiting_input"
	RunWaitingApply = "waiting_apply"
	RunReconciling  = "reconciling"
	RunSucceeded    = "succeeded"
	RunPartial      = "partial"
	RunFailed       = "failed"
	RunCancelled    = "cancelled"
)

// Seq is a run's event cursor. It crosses the API as a decimal string for the
// same reason a revision does, but zero is a real value here: it means nothing
// has been recorded yet, where a revision of zero would mean a row that was
// never written. Reusing Revision would make "nothing pruned" unencodable.
type Seq int64

func (s Seq) MarshalJSON() ([]byte, error) {
	if s < 0 {
		return nil, creativeops.ErrValidation
	}
	return json.Marshal(strconv.FormatInt(int64(s), 10))
}

func (s *Seq) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err != nil || text == "" || (len(text) > 1 && text[0] == '0') {
		return creativeops.ErrValidation
	}
	value, err := strconv.ParseInt(text, 10, 64)
	if err != nil || value < 0 || strconv.FormatInt(value, 10) != text {
		return creativeops.ErrValidation
	}
	*s = Seq(value)
	return nil
}

// Run is what a reader is told about one execution. Sequence counters cross the
// API as decimal strings for the same reason ordinals do.
type Run struct {
	ID               string               `json:"id"`
	ConversationID   string               `json:"conversation_id"`
	CanvasID         string               `json:"canvas_id"`
	TriggerMessageID string               `json:"trigger_message_id"`
	EgressConsentID  string               `json:"egress_consent_id"`
	ModelKey         string               `json:"model_key"`
	SkillID          string               `json:"skill_id,omitempty"`
	SkillVersionID   string               `json:"skill_version_id,omitempty"`
	State            string               `json:"state"`
	SettlementState  string               `json:"settlement_state"`
	ErrorCode        string               `json:"error_code,omitempty"`
	LimitsVersion    string               `json:"limits_version"`
	Revision         creativeops.Revision `json:"revision"`
	LastEventSeq     Seq                  `json:"last_event_seq"`
	PrunedThroughSeq Seq                  `json:"pruned_through_seq"`
	DeadlineAt       time.Time            `json:"deadline_at"`
	CreatedAt        time.Time            `json:"created_at"`
	StartedAt        *time.Time           `json:"started_at,omitempty"`
	FinishedAt       *time.Time           `json:"finished_at,omitempty"`
}

const runColumns = "id,conversation_id,canvas_id,trigger_message_id,egress_consent_id,model_key," +
	"skill_id,skill_version_id,state,settlement_state,error_code,limits_version,revision," +
	"last_event_seq,pruned_through_seq,deadline_at,created_at,started_at,finished_at"

func scanRun(row interface{ Scan(...any) error }) (Run, error) {
	var r Run
	var skillID, versionID, errorCode *string
	var revision, lastSeq, prunedSeq int64
	if err := row.Scan(&r.ID, &r.ConversationID, &r.CanvasID, &r.TriggerMessageID, &r.EgressConsentID,
		&r.ModelKey, &skillID, &versionID, &r.State, &r.SettlementState, &errorCode, &r.LimitsVersion,
		&revision, &lastSeq, &prunedSeq, &r.DeadlineAt, &r.CreatedAt, &r.StartedAt, &r.FinishedAt); err != nil {
		return Run{}, err
	}
	r.Revision = creativeops.Revision(revision)
	r.LastEventSeq, r.PrunedThroughSeq = Seq(lastSeq), Seq(prunedSeq)
	if skillID != nil {
		r.SkillID = *skillID
	}
	if versionID != nil {
		r.SkillVersionID = *versionID
	}
	if errorCode != nil {
		r.ErrorCode = *errorCode
	}
	return r, nil
}

// instructionSchemaVersion is the submission protocol's own number. It is not
// the message body's: one describes what a client may send, the other what the
// server stored, and they change for different reasons.
const instructionSchemaVersion = 1

// Instruction is one submission, exactly as the contract declares it. It is
// nested rather than flattened into the run's own fields so the schema the
// OpenAPI document froze and the struct that parses it are the same shape —
// there is no generated binding between them yet, and two spellings of one
// protocol is how they drift.
type Instruction struct {
	SchemaVersion int                  `json:"schema_version"`
	Segments      []InstructionSegment `json:"instruction_segments"`
}

type createRun struct {
	ConversationID  string      `json:"conversation_id"`
	Instruction     Instruction `json:"instruction"`
	ModelKey        string      `json:"model_key"`
	EgressConsentID string      `json:"egress_consent_id"`
}

// CreateRun turns one submission into work. Everything that makes the run real
// — the photographer's message, the frozen inputs, the write slot, the budget
// hold, the first event and the queue entry — commits together or not at all.
// A visible run with no queued task, or a queued task with no visible run,
// would each be a way for work to disappear.
//
// The skill is resolved before this transaction opens. A platform skill belongs
// to the publishing account, so reading it needs that account's scope and can
// never happen inside this one; what makes the pre-read sound is that a version
// is immutable. Its availability is the mutable part, and that is re-checked
// before dispatch, which is where the invariant actually has to hold.
func (s *Service) CreateRun(ctx context.Context, scope store.AccountScope, command creativeops.Command) (creativeops.Receipt, error) {
	var input createRun
	var model llmgateway.ModelConfig
	// The bounded pre-read. Its cost is paid on every call, but its refusal is
	// deferred into Validate, which a replayed operation never reaches: a retry
	// of a submission that already succeeded must return the original receipt
	// even if the skill it named has since been withdrawn.
	var resolved ResolvedInstruction
	var resolveErr error
	if err := creativeops.Decode(command.Payload, &input); err == nil {
		resolved, resolveErr = s.ResolveInstruction(ctx, scope, input.Instruction.Segments)
	}
	return (creativeops.Executor{}).Run(ctx, scope, creativeops.Operation{
		Key: "agent.create_run", Capability: "agent_start",
		Validate: func(raw json.RawMessage) error {
			if err := creativeops.Decode(raw, &input); err != nil {
				return err
			}
			if input.ConversationID == "" || input.EgressConsentID == "" ||
				input.Instruction.SchemaVersion != instructionSchemaVersion {
				return creativeops.ErrValidation
			}
			if err := validSegments(input.Instruction.Segments); err != nil {
				return err
			}
			if resolveErr != nil {
				return resolveErr
			}
			var err error
			if model, err = s.models.Model(input.ModelKey); err != nil {
				// An unknown key is a malformed request — the client was handed
				// the catalog. A disabled one is a real model this deployment
				// currently cannot reach, which the photographer can act on.
				if errors.Is(err, llmgateway.ErrNotFound) {
					return creativeops.ErrValidation
				}
				return ErrModelCapability
			}
			return nil
		},
		Apply: func(ctx context.Context, tx store.TxAccountScope, _ json.RawMessage) (creativeops.Outcome, error) {
			now, err := tx.CreativeNow(ctx)
			if err != nil {
				return creativeops.Outcome{}, err
			}
			runID, err := creativeops.NewResourceID("ccrn")
			if err != nil {
				return creativeops.Outcome{}, err
			}
			claim, err := randomClaimToken()
			if err != nil {
				return creativeops.Outcome{}, err
			}
			deadline := now.Add(s.limits.RunDuration())

			// Lock order: the account's write slot, then the gateway's group and
			// budget rows, then the conversation, then the consent, then the
			// content revisions it authorises.
			if err := acquireSlotInTx(ctx, tx, runID, claim, now); err != nil {
				return creativeops.Outcome{}, err
			}
			reservation, err := s.reserveInTx(ctx, tx, runID, model, deadline)
			if err != nil {
				return creativeops.Outcome{}, err
			}
			var canvasID string
			err = tx.QueryRowForUpdate(ctx, "creative_agent_conversations", "canvas_id", "id=$2", input.ConversationID).Scan(&canvasID)
			if errors.Is(err, store.ErrNoRows) {
				return creativeops.Outcome{}, ErrNotFound
			}
			if err != nil {
				return creativeops.Outcome{}, err
			}
			consent, err := readConsentForUpdate(ctx, tx, input.EgressConsentID)
			if err != nil {
				return creativeops.Outcome{}, err
			}
			if err := coversRun(consent, input.ConversationID, model.VendorKey, resolved.ContentRefs); err != nil {
				return creativeops.Outcome{}, err
			}
			// The second read of every named revision, inside the transaction
			// that creates the run. The first one happened before this function
			// and could have been overtaken by a withdrawal; this one cannot.
			for _, ref := range resolved.ContentRefs {
				// Being able to display a revision and having authorised it to
				// leave the account are two different permissions, and the run
				// needs both.
				approved, err := tx.Exists(ctx, "creative_egress_consent_contents",
					"consent_id=$2 AND content_revision_id=$3", consent.ID, ref.RevisionID)
				if err != nil {
					return creativeops.Outcome{}, err
				}
				if !approved {
					return creativeops.Outcome{}, ErrEgressRequired
				}
				if _, err := creativecontent.RequireUsable(ctx, tx, ref.RevisionID, "display"); err != nil {
					return creativeops.Outcome{}, translateContent(err)
				}
			}

			// What is left of one request's input ceiling after the submission
			// and the skill's own instructions have taken their share. History
			// fills the remainder rather than competing for a second budget.
			history, err := s.freezeHistoryInTx(ctx, tx, input.ConversationID,
				s.limits.InputTextBytes-resolved.Bytes-skillInstructionBytes(resolved.Skill))
			if err != nil {
				return creativeops.Outcome{}, err
			}
			trigger, err := s.appendMessageInTx(ctx, tx, input.ConversationID, newMessage{
				Role: "user", Status: "complete", Body: resolved.Body, RunID: runID,
				ContentRefs: resolved.ContentRefs, SkillRefs: resolved.SkillRefs(),
			})
			if err != nil {
				return creativeops.Outcome{}, err
			}
			run, err := s.insertRunInTx(ctx, tx, runInsert{
				ID: runID, ConversationID: input.ConversationID, CanvasID: canvasID,
				TriggerMessageID: trigger.ID, ConsentID: consent.ID, Model: model,
				Skill: resolved.Skill, ClaimToken: claim, ReservationID: reservation.ID,
				Deadline: deadline, Now: now,
			})
			if err != nil {
				return creativeops.Outcome{}, err
			}
			if err := s.freezeInputsInTx(ctx, tx, runID, history, resolved.Body.Blocks); err != nil {
				return creativeops.Outcome{}, err
			}
			if resolved.Skill != nil {
				snapshot := resolved.Skill.Snapshot
				if err := tx.Insert(ctx, "creative_run_skill_refs",
					[]string{"run_id", "segment_ordinal", "skill_id", "skill_version_id", "skill_owner_account_id", "digest"},
					runID, resolved.Skill.SegmentOrdinal, snapshot.SkillID, snapshot.ID,
					snapshot.OwnerAccountID, snapshot.Digest); err != nil {
					return creativeops.Outcome{}, err
				}
			}
			if run.LastEventSeq, err = appendRunEventInTx(ctx, tx, runID, "run.accepted",
				map[string]string{"state": RunQueued, "run_revision": "1"}); err != nil {
				return creativeops.Outcome{}, err
			}
			task, err := json.Marshal(runTask{RunID: runID})
			if err != nil {
				return creativeops.Outcome{}, err
			}
			if _, err := jobs.EnqueueInTx(ctx, tx.Jobs(s.runtime), jobs.Request{
				Kind: jobRunKind,
				// Deterministic per run, so a retried creation cannot enqueue a
				// second task for work that already has one.
				OperationID: uuid.NewSHA1(uuid.MustParse(command.OperationID), []byte(jobRunKind+":"+runID)).String(),
				CreatedAt:   command.CreatedAt,
				Payload:     task,
			}); err != nil {
				return creativeops.Outcome{}, err
			}
			response, err := json.Marshal(run)
			if err != nil {
				return creativeops.Outcome{}, err
			}
			revision := run.Revision
			return creativeops.Outcome{HTTPStatus: 202, Response: response,
				ResultKind: "agent_run", ResultID: &run.ID, ResultRevision: &revision}, nil
		},
	}, command)
}

// skillInstructionBytes is what the chosen skill will cost in every request. It
// is charged against the input ceiling before history is allowed to fill the
// rest, because instructions the photographer chose outrank turns they can
// still scroll back to.
func skillInstructionBytes(skill *ResolvedSkill) int {
	if skill == nil {
		return 0
	}
	return len(skill.Snapshot.Instructions)
}

// unmarshalBody is the one place a stored body re-enters the program. Reading
// never revalidates: a v1 row was written under a v1 rule and is rendered as it
// was, not re-judged against today's block types.
func unmarshalBody(raw []byte, body *Body) error { return json.Unmarshal(raw, body) }

// jsonValue encodes a nullable JSON column. A nil value must reach the driver
// as NULL rather than as the four bytes "null", which the column would happily
// store as a JSON null nobody can tell from a real one.
func jsonValue(value any) (any, error) {
	if value == nil {
		return nil, nil
	}
	return json.Marshal(value)
}

// coversRun decides whether the authorisation on file reaches what this run
// would actually send. Every clause answers a different question, and a run may
// only start when all of them say yes.
func coversRun(consent Consent, conversationID, vendorKey string, refs []ContentRef) error {
	switch {
	case consent.RevokedAt != nil:
		return ErrConsentRevoked
	case consent.ConversationID != conversationID:
		// A consent granted for another conversation authorises another set of
		// words; the photographer's own messages are part of what leaves.
		return ErrEgressRequired
	case consent.VendorKey != vendorKey:
		// The company that receives the bytes is what was disclosed. Switching
		// models inside one vendor is free; crossing to another is not.
		return ErrEgressRequired
	case consent.PolicyVersion != policyVersion:
		// A new disclosure needs a new consent rather than a silent reuse of an
		// agreement to different words.
		return ErrEgressRequired
	case !slices.Contains(consent.Scope.DataClasses, "text"):
		return ErrEgressRequired
	case len(refs) > 0 && consent.Scope.Mode != ScopeSelectedRevisions:
		// account_library authorises whatever a search finds in the library. A
		// revision the photographer named by hand is not that, so it needs to
		// have been listed. The listed-ness itself is checked below.
		return ErrEgressRequired
	}
	return nil
}

// turnBindingKey is the durable name of one model turn. It is derived from the
// run and the step's ordinal rather than the step id, because the hold for the
// first turn has to exist before that turn does: a run is admitted against the
// budget when it is created, and the worker that later runs it must claim that
// same hold instead of taking a second one beside it.
//
// It is durable in both directions — the ordinal comes from the run's own
// counter under its row lock, never from anything this process invented — which
// is what lets a resumed turn replay the request it already paid for.
func turnBindingKey(runID string, ordinal int64) string {
	return runID + "#" + strconv.FormatInt(ordinal, 10)
}

// firstTurnOrdinal is what creative_agent_runs.next_step_ordinal starts at, so
// the hold taken at creation and the first turn's binding name the same thing.
const firstTurnOrdinal = 1

// reserveInTx takes the conservative hold before any request exists. The bound
// is this run's own per-request ceiling rather than a measurement of the first
// prompt: the limits snapshot is what the run will be judged by, and a hold
// derived from it cannot be undercut by an assembly that grows later.
//
// The reservation is created under the first turn's own identity. Any other id
// would leave this hold unclaimed forever while the turn took a second one, and
// two holds per run is how an account's month is spent without a single model
// call being made.
func (s *Service) reserveInTx(ctx context.Context, tx store.TxAccountScope, runID string,
	model llmgateway.ModelConfig, deadline time.Time) (llmgateway.Reservation, error) {
	reservation, err := s.gateway.ReserveInTx(ctx, tx, llmgateway.ReserveInput{
		CallerService:     callerService,
		CallerOperationID: llmgateway.ReserveOperationID(callerService, turnBindingKey(runID, firstTurnOrdinal)),
		// One run is one caller group, so its whole ceiling is enforced across
		// every turn it will make and cannot reset at a month boundary.
		CallerGroupID: runID,
		// Derived, not chosen: at most one request per model turn, each bounded
		// by the run's own input and output ceilings.
		GroupTokenLimit:      int64(s.limits.ModelTurns) * int64(s.limits.InputTextBytes+model.Capability.MaxOutputTokens),
		GroupDeadline:        deadline,
		ModelKey:             model.ModelKey,
		InputTokenUpperBound: int64(s.limits.InputTextBytes),
		OutputLimit:          model.Capability.MaxOutputTokens,
		ExpiresAt:            deadline,
	})
	if err != nil {
		return llmgateway.Reservation{}, translateGateway(err)
	}
	return reservation, nil
}

// translateGateway maps the gateway's vocabulary into this package's, because
// llmgateway's sentinels have no mapping at the HTTP edge — the same reason
// creativecontent's are translated in the resolver. The original is kept as the
// wrapped cause: the edge renders a fixed message either way, and a log that
// says only "invalid" cannot be acted on.
func translateGateway(err error) error {
	switch {
	case errors.Is(err, llmgateway.ErrBudget):
		return fmt.Errorf("%w: %w", ErrBudgetExceeded, err)
	case errors.Is(err, llmgateway.ErrCapability), errors.Is(err, llmgateway.ErrUnavailable):
		return fmt.Errorf("%w: %w", ErrModelCapability, err)
	case errors.Is(err, llmgateway.ErrValidation):
		return fmt.Errorf("%w: %w", creativeops.ErrValidation, err)
	default:
		return err
	}
}

// translateSkill maps the skill store's refusals for the dispatch path. Absence
// passes through — the edge maps creativeskill.ErrNotFound already — but a
// withdrawal becomes this package's "this deployment cannot run that", because
// it is the same fact the resolver reports when a skill is switched off and the
// photographer acts on it the same way.
func translateSkill(err error) error {
	if errors.Is(err, creativeskill.ErrDisabled) {
		return fmt.Errorf("%w: %w", ErrUnsupportedSegment, err)
	}
	return err
}

func translateContent(err error) error {
	switch {
	case errors.Is(err, creativecontent.ErrNotFound), errors.Is(err, creativecontent.ErrMissingRoot):
		return ErrNotFound
	case errors.Is(err, creativecontent.ErrUsageDenied):
		return ErrEgressRequired
	default:
		return err
	}
}

type runInsert struct {
	ID, ConversationID, CanvasID string
	TriggerMessageID, ConsentID  string
	Model                        llmgateway.ModelConfig
	Skill                        *ResolvedSkill
	ClaimToken, ReservationID    string
	Deadline, Now                time.Time
}

func (s *Service) insertRunInTx(ctx context.Context, tx store.TxAccountScope, in runInsert) (Run, error) {
	modelSnapshot, err := json.Marshal(s.models.Snapshot(in.Model))
	if err != nil {
		return Run{}, err
	}
	limitsSnapshot, err := json.Marshal(s.limits)
	if err != nil {
		return Run{}, err
	}
	var skillID, versionID *string
	var skillSnapshot []byte
	if in.Skill != nil {
		// The whole frozen version travels with the run. Resolving it again is
		// how availability is re-checked, not how the instructions are found:
		// a version withdrawn mid-run must still render what it already said.
		if skillSnapshot, err = json.Marshal(in.Skill.Snapshot); err != nil {
			return Run{}, err
		}
		skillID, versionID = &in.Skill.Snapshot.SkillID, &in.Skill.Snapshot.ID
	}
	if err := tx.Insert(ctx, "creative_agent_runs",
		[]string{"id", "conversation_id", "canvas_id", "trigger_message_id", "egress_consent_id",
			"model_key", "model_snapshot", "skill_id", "skill_version_id", "skill_snapshot",
			"state", "claim_token", "next_step_ordinal", "last_event_seq", "limits_version",
			"limits_snapshot", "initial_llm_reservation_id", "deadline_at", "created_at", "updated_at"},
		in.ID, in.ConversationID, in.CanvasID, in.TriggerMessageID, in.ConsentID,
		in.Model.ModelKey, modelSnapshot, skillID, versionID, nullableJSON(skillSnapshot),
		RunQueued, in.ClaimToken, 1, 0, limitsVersion,
		limitsSnapshot, in.ReservationID, in.Deadline, in.Now, in.Now); err != nil {
		return Run{}, err
	}
	run := Run{
		ID: in.ID, ConversationID: in.ConversationID, CanvasID: in.CanvasID,
		TriggerMessageID: in.TriggerMessageID, EgressConsentID: in.ConsentID,
		ModelKey: in.Model.ModelKey, State: RunQueued, SettlementState: "not_started",
		LimitsVersion: limitsVersion, Revision: 1,
		DeadlineAt: in.Deadline, CreatedAt: in.Now,
	}
	if in.Skill != nil {
		run.SkillID, run.SkillVersionID = in.Skill.Snapshot.SkillID, in.Skill.Snapshot.ID
	}
	return run, nil
}

func nullableJSON(raw []byte) any {
	if raw == nil {
		return nil
	}
	return raw
}

// acquireSlotInTx takes the account's one write slot. The row is locked before
// it is read, so two submissions racing for the slot do not both see it free.
func acquireSlotInTx(ctx context.Context, tx store.TxAccountScope, runID, claim string, now time.Time) error {
	exists, err := tx.Exists(ctx, "creative_agent_slots", "")
	if err != nil {
		return err
	}
	if !exists {
		// The empty row is the slot; creating it is not taking it.
		if err := tx.Insert(ctx, "creative_agent_slots", []string{"updated_at"}, now); err != nil {
			return err
		}
	}
	var heldRun, heldToken *string
	if err := tx.QueryRowForUpdate(ctx, "creative_agent_slots", "run_id,claim_token", "").Scan(&heldRun, &heldToken); err != nil {
		return err
	}
	if heldRun != nil {
		if *heldRun == runID && heldToken != nil && *heldToken == claim {
			return nil // Replay of this very acquisition.
		}
		return BusyError{ActiveRunID: *heldRun}
	}
	_, err = tx.Update(ctx, "creative_agent_slots",
		"run_id=$2, claim_token=$3, acquired_at=$4, updated_at=$4", "", runID, claim, now)
	return err
}

// releaseSlotInTx frees the slot only for the holder that still matches. An old
// run whose work ended long ago must never clear an occupancy somebody else has
// since taken.
func releaseSlotInTx(ctx context.Context, tx store.TxAccountScope, runID, claim string, now time.Time) error {
	_, err := tx.Update(ctx, "creative_agent_slots",
		"run_id=NULL, claim_token=NULL, acquired_at=NULL, updated_at=$4",
		"run_id=$2 AND claim_token=$3", runID, claim, now)
	return err
}

// appendRunEventInTx hands out the next sequence under the run's row lock. A
// gap would make a reader that is following along stop and ask for a fresh
// snapshot, so the counter may never be derived from MAX(seq)+1.
func appendRunEventInTx(ctx context.Context, tx store.TxAccountScope, runID, eventType string, payload any) (Seq, error) {
	var last int64
	err := tx.QueryRowForUpdate(ctx, "creative_agent_runs", "last_event_seq", "id=$2", runID).Scan(&last)
	if errors.Is(err, store.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	seq := last + 1
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	if err := tx.Insert(ctx, "creative_run_events",
		[]string{"run_id", "seq", "schema_version", "event_type", "payload"},
		runID, seq, runEventSchemaVersion, eventType, body); err != nil {
		return 0, err
	}
	if _, err := tx.Update(ctx, "creative_agent_runs", "last_event_seq=$3", "id=$2", runID, seq); err != nil {
		return 0, err
	}
	return Seq(seq), nil
}

func randomClaimToken() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

// ReadRun answers about one run. It is the panel's query and the test's, and it
// never reveals another account's work because the scope decides what is
// visible before the id does.
func (s *Service) ReadRun(ctx context.Context, scope store.AccountScope, runID string) (Run, error) {
	if runID == "" {
		return Run{}, creativeops.ErrValidation
	}
	var run Run
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.RequireCreativeCapability(ctx, "creative_read"); err != nil {
			return err
		}
		var err error
		run, err = scanRun(tx.QueryRow(ctx, "creative_agent_runs", runColumns, "id=$2", runID))
		if errors.Is(err, store.ErrNoRows) {
			return ErrNotFound
		}
		return err
	})
	if err != nil {
		return Run{}, err
	}
	return run, nil
}
