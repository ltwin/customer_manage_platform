package creativeagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/creativeskill"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// Wake timing, as Claude Code waits for a burst of background results
// (owner 2026-11-15): open a turn 2 seconds after the group's last notice,
// at most 10 seconds after its first one (review R7).
const (
	wakeQuiet    = 2 * time.Second
	wakeMaxDelay = 10 * time.Second
	// wakeRecheck is how soon a group whose conversation is busy looks again.
	wakeRecheck = 2 * time.Second
	// wakeChainLimit stops automatic turns after this many in a row with no
	// photographer message between them.
	wakeChainLimit = 5
)

// Reasons a notice does not open a turn and waits for the photographer's
// next message instead.
const (
	skipSourceStopped = "source_stopped"
	skipSourceFailed  = "source_failed"
	skipChainLimit    = "chain_limit"
	skipEgress        = "egress"
	skipCredits       = "credits"
	skipUnavailable   = "unavailable"
)

const triggerBackgroundTool = "background_tool"

// wakeTask is a pending notice as the waker reads it.
type wakeTask struct {
	ID, ConversationID, CanvasID, SourceRunID, ModelKey, NotificationMessageID string
	SkillVersionID                                                             *string
	FinishedAt                                                                 time.Time
}

const wakeTaskColumns = "id,conversation_id,canvas_id,source_run_id,model_key,notification_message_id,skill_version_id,finished_at"

func scanWakeTask(row interface{ Scan(...any) error }) (wakeTask, error) {
	var t wakeTask
	err := row.Scan(&t.ID, &t.ConversationID, &t.CanvasID, &t.SourceRunID, &t.ModelKey, &t.NotificationMessageID, &t.SkillVersionID, &t.FinishedAt)
	return t, err
}

// wakeGroupInTx is the conversation's pending notices of one configuration
// (model and main skill version) that may still open a turn, oldest first.
func wakeGroupInTx(ctx context.Context, tx store.TxAccountScope, conversationID, modelKey string, skill *string) ([]wakeTask, error) {
	rows, err := tx.QueryPage(ctx, "creative_agent_tool_tasks", wakeTaskColumns,
		"conversation_id=$2 AND notification_state='pending' AND wake_skip_reason IS NULL AND wake_run_id IS NULL AND model_key=$3 AND skill_version_id IS NOT DISTINCT FROM $4",
		[]store.OrderBy{{Column: "finished_at"}, {Column: "id"}}, maxNoticesPerRequest*4, 0, conversationID, modelKey, skill)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []wakeTask
	for rows.Next() {
		t, err := scanWakeTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// scheduleWakeInTx sets the wake time of the configuration group a new
// notice joined: min(last + 2s, first + 10s), recomputed with every notice.
func scheduleWakeInTx(ctx context.Context, tx store.TxAccountScope, taskID string) error {
	task, err := scanWakeTask(tx.QueryRow(ctx, "creative_agent_tool_tasks", wakeTaskColumns, "id=$2", taskID))
	if err != nil {
		return err
	}
	group, err := wakeGroupInTx(ctx, tx, task.ConversationID, task.ModelKey, task.SkillVersionID)
	if err != nil || len(group) == 0 {
		return err
	}
	first, last := group[0].FinishedAt, group[len(group)-1].FinishedAt
	wake := last.Add(wakeQuiet)
	if limit := first.Add(wakeMaxDelay); limit.Before(wake) {
		wake = limit
	}
	ids := make([]string, len(group))
	for i, t := range group {
		ids[i] = t.ID
	}
	_, err = tx.Update(ctx, "creative_agent_tool_tasks", "wake_after=$3,updated_at=clock_timestamp()", "id = ANY($2::text[])", ids, wake)
	return err
}

// WakeDueToolTasks opens turns for the configuration groups whose wake time
// passed, each in its own account scope and transaction.
func (s *Service) WakeDueToolTasks(ctx context.Context, db *store.Store, batch int) (int, error) {
	if s.backgroundTasksDisabled {
		return 0, nil
	}
	refs, err := db.WakeDueToolTasks(ctx, batch)
	if err != nil {
		return 0, err
	}
	var errs []error
	for _, ref := range refs {
		if ctx.Err() != nil {
			return len(refs), errors.Join(append(errs, ctx.Err())...)
		}
		scope := db.ScopeFor(auth.AccountContext{AccountID: ref.AccountID})
		if err := s.wakeToolTask(ctx, scope, ref.ID); err != nil {
			errs = append(errs, fmt.Errorf("wake %s: %w", ref.ID, err))
		}
	}
	return len(refs), errors.Join(errs...)
}

// wakeToolTask decides one due group: notices whose source run was stopped
// or failed wait for the photographer; a busy conversation looks again
// shortly; otherwise one automatic turn opens with the group's model and
// skill, unless a deterministic refusal (chain limit, egress, credits,
// availability) leaves the notices for the photographer's next message.
// Locks: project, slot, budget, conversation (the run creation order), then
// the task rows.
func (s *Service) wakeToolTask(ctx context.Context, scope store.AccountScope, id string) error {
	return scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		task, err := scanWakeTask(tx.QueryRow(ctx, "creative_agent_tool_tasks", wakeTaskColumns,
			"id=$2 AND notification_state='pending' AND wake_run_id IS NULL AND wake_after IS NOT NULL AND wake_after<=clock_timestamp()", id))
		if errors.Is(err, store.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		skip := func(ids []string, reason string) error {
			if len(ids) == 0 {
				return nil
			}
			_, err := tx.Update(ctx, "creative_agent_tool_tasks", "wake_after=NULL,wake_skip_reason=$3,updated_at=clock_timestamp()",
				"id = ANY($2::text[]) AND notification_state='pending' AND wake_run_id IS NULL", ids, reason)
			return err
		}
		postpone := func(ids []string) error {
			_, err := tx.Update(ctx, "creative_agent_tool_tasks", "wake_after=clock_timestamp()+$3::interval,updated_at=clock_timestamp()",
				"id = ANY($2::text[]) AND notification_state='pending' AND wake_run_id IS NULL", ids, fmt.Sprintf("%d milliseconds", wakeRecheck.Milliseconds()))
			return err
		}
		group, err := wakeGroupInTx(ctx, tx, task.ConversationID, task.ModelKey, task.SkillVersionID)
		if err != nil {
			return err
		}
		// A turn already open (or waiting) in the conversation takes the
		// notices itself; look again shortly.
		busy, err := tx.Exists(ctx, "creative_agent_runs", "conversation_id=$2 AND state NOT IN ('succeeded','partial','failed','cancelled')", task.ConversationID)
		if err != nil {
			return err
		}
		var eligible, stopped, failed, unavailable []string
		var first *wakeTask
		for i, t := range group {
			var state string
			var stoppedAt *time.Time
			err := tx.QueryRow(ctx, "creative_agent_runs", "state,cancel_requested_at", "id=$2", t.SourceRunID).Scan(&state, &stoppedAt)
			if errors.Is(err, store.ErrNoRows) {
				unavailable = append(unavailable, t.ID)
				continue
			}
			if err != nil {
				return err
			}
			switch {
			case state == RunCancelled || stoppedAt != nil:
				stopped = append(stopped, t.ID)
			case state == RunFailed:
				failed = append(failed, t.ID)
			default:
				eligible = append(eligible, t.ID)
				if first == nil {
					first = &group[i]
				}
			}
		}
		if err := skip(stopped, skipSourceStopped); err != nil {
			return err
		}
		if err := skip(failed, skipSourceFailed); err != nil {
			return err
		}
		if err := skip(unavailable, skipUnavailable); err != nil {
			return err
		}
		if len(eligible) == 0 {
			return nil
		}
		if busy {
			return postpone(eligible)
		}
		chained, err := automaticRunsInARowInTx(ctx, tx, task.ConversationID)
		if err != nil {
			return err
		}
		if chained >= wakeChainLimit {
			return skip(eligible, skipChainLimit)
		}
		var runID string
		err = tx.Savepoint(ctx, "creative_wake", func() error {
			var err error
			runID, err = s.openWakeRunInTx(ctx, tx, *first)
			return err
		})
		var busyErr BusyError
		switch {
		case errors.As(err, &busyErr):
			return postpone(eligible)
		case errors.Is(err, ErrEgressRequired), errors.Is(err, ErrConsentRevoked):
			return skip(eligible, skipEgress)
		case errors.Is(err, ErrBudgetExceeded):
			return skip(eligible, skipCredits)
		case errors.Is(err, ErrModelCapability), errors.Is(err, ErrUnsupportedSegment), errors.Is(err, creativeskill.ErrNotFound),
			errors.Is(err, creativeskill.ErrDisabled), errors.Is(err, creativecanvas.ErrArchived), errors.Is(err, store.ErrCreativeAccessDenied),
			errors.Is(err, ErrConversationArchived), errors.Is(err, ErrNotFound):
			return skip(eligible, skipUnavailable)
		case err != nil:
			return err
		}
		// The new run carries the group; should it end before its first
		// dispatch, the notices wait for the photographer rather than open
		// another paid turn.
		_, err = tx.Update(ctx, "creative_agent_tool_tasks", "wake_after=NULL,wake_run_id=$3,updated_at=clock_timestamp()",
			"id = ANY($2::text[]) AND notification_state='pending' AND wake_run_id IS NULL", eligible, runID)
		return err
	})
}

// automaticRunsInARowInTx counts automatic turns since the photographer
// last spoke. A supplement or steer also breaks the chain even though it
// belongs to a turn that already exists.
func automaticRunsInARowInTx(ctx context.Context, tx store.TxAccountScope, conversationID string) (int, error) {
	rows, err := tx.QueryPage(ctx, "creative_agent_runs", "trigger_kind", `conversation_id=$2 AND (
 trigger_kind IS NULL OR NOT EXISTS (
 SELECT 1 FROM creative_agent_messages trigger_message
 JOIN creative_agent_messages photographer_message
   ON photographer_message.account_id=trigger_message.account_id
   AND photographer_message.conversation_id=trigger_message.conversation_id
 WHERE trigger_message.account_id=$1 AND trigger_message.id=creative_agent_runs.trigger_message_id
   AND photographer_message.role='user' AND photographer_message.ordinal>trigger_message.ordinal))`,
		[]store.OrderBy{{Column: "created_at", Desc: true}, {Column: "id", Desc: true}}, wakeChainLimit+1, 0, conversationID)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var kind *string
		if err := rows.Scan(&kind); err != nil {
			return 0, err
		}
		if kind == nil || *kind != triggerBackgroundTool {
			break
		}
		n++
	}
	return n, rows.Err()
}

// openWakeRunInTx opens the automatic turn for a group through the shared
// run path: budget, limits, the egress gate, frozen history, registration
// and its job, with the group's model and main skill version and the first
// notice as its trigger. The model is charged for what it uses.
func (s *Service) openWakeRunInTx(ctx context.Context, tx store.TxAccountScope, first wakeTask) (string, error) {
	if err := tx.RequireCreativeCapability(ctx, "agent_start"); err != nil {
		return "", err
	}
	model, err := s.models.Model(first.ModelKey)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrModelCapability, err)
	}
	var skill *ResolvedSkill
	if first.SkillVersionID != nil {
		var raw []byte
		if err := tx.QueryRow(ctx, "creative_agent_runs", "skill_snapshot", "id=$2", first.SourceRunID).Scan(&raw); err != nil {
			return "", err
		}
		var snapshot creativeskill.Snapshot
		if err := json.Unmarshal(raw, &snapshot); err != nil || snapshot.ID != *first.SkillVersionID {
			return "", ErrUnsupportedSegment
		}
		skill = &ResolvedSkill{Snapshot: snapshot}
	}
	now, err := tx.CreativeNow(ctx)
	if err != nil {
		return "", err
	}
	runID, err := creativeops.NewResourceID("ccrn")
	if err != nil {
		return "", err
	}
	claim, err := randomClaimToken()
	if err != nil {
		return "", err
	}
	deadline := now.Add(s.limits.RunDuration())
	if err := creativecanvas.LockProjectForWorkInTx(ctx, tx, first.CanvasID); err != nil {
		return "", err
	}
	if err := acquireSlotInTx(ctx, tx, first.ConversationID, runID, claim, now); err != nil {
		return "", err
	}
	reservation, err := s.reserveInTx(ctx, tx, runID, model, deadline)
	if err != nil {
		return "", err
	}
	conversation, err := lockOpenConversationInTx(ctx, tx, first.ConversationID)
	if err != nil {
		return "", err
	}
	consent, err := effectiveConsentInTx(ctx, tx, "")
	if err != nil {
		return "", err
	}
	if skill != nil {
		if modelSupportsSkill(skill.Snapshot.Manifest, model.Capability) != "" {
			return "", ErrModelCapability
		}
		h := runtimeHandler{service: s, libraryEnabled: consent.Scope.Mode == ScopeAccountLibrary && consentHas(consent, "media_metadata")}
		h.held.Model = model
		if h.skillUnavailable(skill.Snapshot) != "" {
			return "", ErrUnsupportedSegment
		}
	}
	trigger, err := s.messageByIDInTx(ctx, tx, first.NotificationMessageID)
	if err != nil {
		return "", err
	}
	_, err = s.openRunInTx(ctx, tx, openRun{
		ID: runID, ConversationID: first.ConversationID, CanvasID: conversation.CanvasID, Trigger: trigger,
		ConsentID: consent.ID, Model: model, Resolved: ResolvedInstruction{Skill: skill},
		ClaimToken: claim, ReservationID: reservation.ID, Deadline: deadline, Now: now,
		TriggerKind:    triggerBackgroundTool,
		JobOperationID: uuid.NewSHA1(toolTaskNamespace, []byte("wake:"+runID)).String(),
		JobCreatedAt:   now,
	})
	if err != nil {
		return "", err
	}
	return runID, nil
}

// messageByIDInTx reads one message of the account.
func (s *Service) messageByIDInTx(ctx context.Context, tx store.TxAccountScope, id string) (Message, error) {
	m, err := scanMessage(tx.QueryRow(ctx, "creative_agent_messages",
		"id, ordinal, run_id, role, status, schema_version, body, revision, created_at", "id=$2", id))
	if errors.Is(err, store.ErrNoRows) {
		return Message{}, ErrNotFound
	}
	return m, err
}
