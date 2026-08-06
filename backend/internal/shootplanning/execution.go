package shootplanning

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/platform/idempotency"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

var (
	ErrExecutionRevisionConflict = errors.New("execution_revision_conflict")
	ErrRunSessionNotFound        = errors.New("run_session_not_found")
	ErrExecutionEventNotFound    = errors.New("execution_event_not_found")
	ErrExecutionEventAlreadyVoid = errors.New("execution_event_already_void")
	ErrSupersedesMismatch        = errors.New("supersedes_event_mismatch")
)

type RunModeSession struct {
	ID                      string      `json:"id"`
	PlanID                  string      `json:"plan_id"`
	ExecutionWindowRevision *int64      `json:"execution_window_revision"`
	OpenedAt                time.Time   `json:"opened_at"`
	LastActiveAt            time.Time   `json:"last_active_at"`
	ClosedAt                *time.Time  `json:"closed_at"`
	CaptureMode             CaptureMode `json:"capture_mode"`
}

type RunInputSnapshot struct {
	PlanID                string          `json:"plan_id"`
	PlanRevision          int64           `json:"plan_revision"`
	ExecutionFactRevision int64           `json:"execution_fact_revision"`
	Shots                 []Shot          `json:"shots"`
	ReadinessItems        []ReadinessItem `json:"readiness_items"`
}

type OpenRunSessionResult struct {
	Session      RunModeSession   `json:"session"`
	PlanRevision int64            `json:"plan_revision"`
	Input        RunInputSnapshot `json:"input"`
}

func (a *Application) OpenRunSession(
	ctx context.Context,
	scope store.AccountScope,
	key, planID string,
	expectedRevision int64,
) (OpenRunSessionResult, error) {
	planID = strings.TrimSpace(planID)
	if planID == "" || expectedRevision < 1 {
		return OpenRunSessionResult{}, validationError("open run session invalid")
	}
	canonical, _ := json.Marshal(struct {
		ExpectedRevision int64 `json:"expected_revision"`
	}{ExpectedRevision: expectedRevision})
	response, err := a.idempotency.Execute(ctx, scope, idempotency.Request{
		Operation: idempotency.OperationRunSessionOpen, Key: key,
		ResourceIdentity: idempotency.RunSessionResource(planID), CanonicalBody: canonical,
	}, func(tx store.TxAccountScope) (idempotency.StoredResponse, error) {
		result, err := a.openRunSessionInScope(ctx, tx, key, planID, expectedRevision)
		if err != nil {
			return idempotency.StoredResponse{}, err
		}
		body, err := json.Marshal(result)
		return idempotency.StoredResponse{Status: 201, Body: body}, err
	})
	if err != nil {
		return OpenRunSessionResult{}, err
	}
	var result OpenRunSessionResult
	if err := json.Unmarshal(response.Body, &result); err != nil {
		return OpenRunSessionResult{}, fmt.Errorf("decode run session replay: %w", err)
	}
	return result, nil
}

func (a *Application) openRunSessionInScope(
	ctx context.Context,
	tx store.TxAccountScope,
	idempotencyKey, planID string,
	expectedRevision int64,
) (OpenRunSessionResult, error) {
	plan, err := a.repo.LockPlan(ctx, tx, planID)
	if err != nil {
		return OpenRunSessionResult{}, err
	}
	if plan.Revision != expectedRevision {
		return OpenRunSessionResult{}, ErrPlanRevisionConflict
	}
	planRevision := plan.Revision
	switch plan.Status {
	case PlanStatusReady:
		updated, err := tx.Update(ctx, "shoot_plans",
			"status = $2, revision = revision + 1, updated_at = clock_timestamp()",
			"id = $3 AND revision = $4", string(PlanStatusInProgress), planID, plan.Revision)
		if err != nil {
			return OpenRunSessionResult{}, err
		}
		if updated != 1 {
			return OpenRunSessionResult{}, ErrPlanRevisionConflict
		}
		planRevision++
	case PlanStatusInProgress:
	case PlanStatusArchived:
		return OpenRunSessionResult{}, ErrArchivedReadOnly
	default:
		return OpenRunSessionResult{}, ErrInvalidPlanTransition
	}
	now := a.now().UTC()
	window, err := loadWindow(ctx, tx, planID)
	if err != nil {
		return OpenRunSessionResult{}, err
	}
	captureMode := deriveSessionCaptureMode(now, window)
	var windowRevision *int64
	if window != nil {
		revision := window.Revision
		windowRevision = &revision
	}
	session := RunModeSession{
		ID: "run_" + uuid.NewString(), PlanID: planID, ExecutionWindowRevision: windowRevision,
		OpenedAt: now, LastActiveAt: now, CaptureMode: captureMode,
	}
	fingerprintFrame := tx.AccountID() + "\x00" + string(idempotency.OperationRunSessionOpen) + "\x00" + idempotencyKey
	fingerprint := sha256.Sum256([]byte(fingerprintFrame))
	if err := tx.Insert(ctx, "shoot_plan_run_sessions",
		[]string{"id", "plan_id", "execution_window_revision", "opened_at", "last_active_at", "capture_mode", "idempotency_key_fingerprint"},
		session.ID, planID, windowRevision, now, now, string(captureMode), fingerprint[:]); err != nil {
		return OpenRunSessionResult{}, err
	}
	shots, err := loadShots(ctx, tx, planID)
	if err != nil {
		return OpenRunSessionResult{}, err
	}
	readiness, err := loadReadiness(ctx, tx, planID)
	if err != nil {
		return OpenRunSessionResult{}, err
	}
	return OpenRunSessionResult{
		Session: session, PlanRevision: planRevision,
		Input: RunInputSnapshot{
			PlanID: planID, PlanRevision: planRevision, ExecutionFactRevision: plan.ExecutionFactRevision,
			Shots: shots, ReadinessItems: readiness,
		},
	}, nil
}

func deriveSessionCaptureMode(openedAt time.Time, window *PlanExecutionWindow) CaptureMode {
	if window == nil {
		return CaptureModeUnknown
	}
	if !openedAt.Before(window.LiveWindowStartsAt) && !openedAt.After(window.LiveWindowEndsAt) {
		return CaptureModeLive
	}
	if openedAt.After(window.EndsAt) {
		return CaptureModeBackfill
	}
	return CaptureModeUnknown
}

type AppendShotResultInput struct {
	ExpectedExecutionRevision int64
	SessionID                 *string
	Result                    ShotResult
	SkipReason                *string
	Notes                     *string
	SupersedesEventID         *string
}

type AppendShotResultResponse struct {
	Event                 ExecutionFact   `json:"event"`
	CurrentOutcome        *CurrentOutcome `json:"current_outcome"`
	ExecutionRevision     int64           `json:"execution_revision"`
	ExecutionFactRevision int64           `json:"execution_fact_revision"`
}

func (a *Application) AppendShotResult(
	ctx context.Context,
	scope store.AccountScope,
	key, planID, shotID string,
	input AppendShotResultInput,
) (AppendShotResultResponse, error) {
	planID, shotID = strings.TrimSpace(planID), strings.TrimSpace(shotID)
	normalized, canonical, err := normalizeAppendShotResult(input)
	if err != nil || planID == "" || shotID == "" {
		if err == nil {
			err = validationError("append shot result invalid")
		}
		return AppendShotResultResponse{}, err
	}
	response, err := a.idempotency.Execute(ctx, scope, idempotency.Request{
		Operation: idempotency.OperationShotCapture, Key: key,
		ResourceIdentity: idempotency.ShotCaptureResource(planID, shotID), CanonicalBody: canonical,
	}, func(tx store.TxAccountScope) (idempotency.StoredResponse, error) {
		result, err := a.appendShotResultInScope(ctx, tx, planID, shotID, normalized)
		if err != nil {
			return idempotency.StoredResponse{}, err
		}
		body, err := json.Marshal(result)
		return idempotency.StoredResponse{Status: 200, Body: body}, err
	})
	if err != nil {
		return AppendShotResultResponse{}, err
	}
	var result AppendShotResultResponse
	if err := json.Unmarshal(response.Body, &result); err != nil {
		return AppendShotResultResponse{}, fmt.Errorf("decode shot result replay: %w", err)
	}
	return result, nil
}

func normalizeAppendShotResult(input AppendShotResultInput) (AppendShotResultInput, []byte, error) {
	if input.ExpectedExecutionRevision < 0 || !validShotResult(input.Result) {
		return AppendShotResultInput{}, nil, validationError("shot result invalid")
	}
	if input.SkipReason != nil {
		value := strings.TrimSpace(*input.SkipReason)
		input.SkipReason = &value
	}
	if input.Result == ShotResultSkipped {
		if input.SkipReason == nil || !validSkipReason(*input.SkipReason) {
			return AppendShotResultInput{}, nil, validationError("skip reason invalid")
		}
	} else if input.SkipReason != nil {
		return AppendShotResultInput{}, nil, validationError("skip reason only applies to skipped")
	}
	input.Notes = normalizeOptionalTextPointer(input.Notes)
	if input.Notes != nil && runeLen(*input.Notes) > 1000 {
		return AppendShotResultInput{}, nil, validationError("execution notes invalid")
	}
	input.SessionID = normalizeOptionalTextPointer(input.SessionID)
	input.SupersedesEventID = normalizeOptionalTextPointer(input.SupersedesEventID)
	body := map[string]any{
		"expected_execution_revision": input.ExpectedExecutionRevision,
		"result":                      input.Result,
	}
	if input.SessionID != nil {
		body["session_id"] = *input.SessionID
	}
	if input.SkipReason != nil {
		body["skip_reason"] = *input.SkipReason
	}
	if input.Notes != nil {
		body["notes"] = *input.Notes
	}
	if input.SupersedesEventID != nil {
		body["supersedes_event_id"] = *input.SupersedesEventID
	}
	canonical, err := json.Marshal(body)
	return input, canonical, err
}

func (a *Application) appendShotResultInScope(
	ctx context.Context,
	tx store.TxAccountScope,
	planID, shotID string,
	input AppendShotResultInput,
) (AppendShotResultResponse, error) {
	plan, err := a.repo.LockPlan(ctx, tx, planID)
	if err != nil {
		return AppendShotResultResponse{}, err
	}
	if plan.Status == PlanStatusArchived {
		return AppendShotResultResponse{}, ErrArchivedReadOnly
	}
	if plan.Status != PlanStatusInProgress {
		return AppendShotResultResponse{}, ErrInvalidPlanTransition
	}
	var executionRevision, sequence int64
	var currentEventID *string
	if err := tx.QueryRowForUpdate(ctx, "shoot_plan_shots",
		"execution_revision, next_event_seq, current_outcome_event_id",
		"plan_id = $2 AND id = $3 AND removed_at IS NULL", planID, shotID).Scan(&executionRevision, &sequence, &currentEventID); errors.Is(err, store.ErrNoRows) {
		return AppendShotResultResponse{}, ErrShotNotFound
	} else if err != nil {
		return AppendShotResultResponse{}, err
	}
	if executionRevision != input.ExpectedExecutionRevision {
		return AppendShotResultResponse{}, ErrExecutionRevisionConflict
	}
	if !sameOptionalString(currentEventID, input.SupersedesEventID) {
		return AppendShotResultResponse{}, ErrSupersedesMismatch
	}
	captureMode := CaptureModeBackfill
	if input.SessionID != nil {
		if err := tx.QueryRow(ctx, "shoot_plan_run_sessions", "capture_mode",
			"plan_id = $2 AND id = $3 AND closed_at IS NULL", planID, *input.SessionID).Scan(&captureMode); errors.Is(err, store.ErrNoRows) {
			return AppendShotResultResponse{}, ErrRunSessionNotFound
		} else if err != nil {
			return AppendShotResultResponse{}, err
		}
	}
	now := a.now().UTC()
	if input.SessionID != nil {
		updated, err := tx.Update(ctx, "shoot_plan_run_sessions", "last_active_at = $2",
			"plan_id = $3 AND id = $4 AND closed_at IS NULL", now, planID, *input.SessionID)
		if err != nil {
			return AppendShotResultResponse{}, err
		}
		if updated != 1 {
			return AppendShotResultResponse{}, ErrRunSessionNotFound
		}
	}
	event := ExecutionFact{
		Kind: ExecutionFactResult, ID: "evt_" + uuid.NewString(), PlanID: planID, ShotID: shotID,
		SessionID: input.SessionID, Sequence: sequence, PlanRevision: plan.Revision, Result: input.Result,
		SkipReason: input.SkipReason, Notes: input.Notes, CheckedAt: now, CaptureMode: captureMode,
		SupersedesEventID: input.SupersedesEventID, Revision: 1,
	}
	if err := tx.Insert(ctx, "shoot_plan_execution_events",
		[]string{"id", "plan_id", "shot_id", "session_id", "shot_event_seq", "plan_revision", "result", "skip_reason", "notes", "checked_at", "capture_mode", "supersedes_event_id", "revision"},
		event.ID, planID, shotID, input.SessionID, sequence, plan.Revision, string(input.Result), input.SkipReason, input.Notes, now, string(captureMode), input.SupersedesEventID, 1); err != nil {
		return AppendShotResultResponse{}, err
	}
	var projectedEventID any
	var outcome *CurrentOutcome
	if input.Result != ShotResultCleared {
		projectedEventID = event.ID
		outcome = &CurrentOutcome{
			Set: true, EventID: event.ID, Result: event.Result, SkipReason: event.SkipReason,
			CheckedAt: event.CheckedAt, CaptureMode: event.CaptureMode,
		}
	}
	updated, err := tx.Update(ctx, "shoot_plan_shots",
		"execution_revision = execution_revision + 1, next_event_seq = next_event_seq + 1, current_outcome_event_id = $2, updated_at = clock_timestamp()",
		"plan_id = $3 AND id = $4 AND execution_revision = $5", projectedEventID, planID, shotID, executionRevision)
	if err != nil {
		return AppendShotResultResponse{}, err
	}
	if updated != 1 {
		return AppendShotResultResponse{}, ErrExecutionRevisionConflict
	}
	if err := advanceExecutionFactRevision(ctx, tx, planID, plan.ExecutionFactRevision); err != nil {
		return AppendShotResultResponse{}, err
	}
	return AppendShotResultResponse{
		Event: event, CurrentOutcome: outcome, ExecutionRevision: executionRevision + 1,
		ExecutionFactRevision: plan.ExecutionFactRevision + 1,
	}, nil
}

type VoidExecutionEventInput struct {
	ExpectedExecutionRevision int64
	Reason                    string
}

type VoidExecutionEventResponse struct {
	VoidEvent             ExecutionFact   `json:"void_event"`
	CurrentOutcome        *CurrentOutcome `json:"current_outcome"`
	ExecutionRevision     int64           `json:"execution_revision"`
	ExecutionFactRevision int64           `json:"execution_fact_revision"`
}

func (a *Application) VoidExecutionEvent(
	ctx context.Context,
	scope store.AccountScope,
	key, planID, eventID string,
	input VoidExecutionEventInput,
) (VoidExecutionEventResponse, error) {
	planID, eventID = strings.TrimSpace(planID), strings.TrimSpace(eventID)
	input.Reason = strings.TrimSpace(input.Reason)
	if planID == "" || eventID == "" || input.ExpectedExecutionRevision < 0 || runeLen(input.Reason) < 1 || runeLen(input.Reason) > 500 {
		return VoidExecutionEventResponse{}, validationError("void execution event invalid")
	}
	canonical, _ := json.Marshal(struct {
		ExpectedExecutionRevision int64  `json:"expected_execution_revision"`
		Reason                    string `json:"reason"`
	}{input.ExpectedExecutionRevision, input.Reason})
	response, err := a.idempotency.Execute(ctx, scope, idempotency.Request{
		Operation: idempotency.OperationExecutionEventVoid, Key: key,
		ResourceIdentity: idempotency.EventVoidResource(planID, eventID), CanonicalBody: canonical,
	}, func(tx store.TxAccountScope) (idempotency.StoredResponse, error) {
		result, err := a.voidExecutionEventInScope(ctx, tx, planID, eventID, input)
		if err != nil {
			return idempotency.StoredResponse{}, err
		}
		body, err := json.Marshal(result)
		return idempotency.StoredResponse{Status: 200, Body: body}, err
	})
	if err != nil {
		return VoidExecutionEventResponse{}, err
	}
	var result VoidExecutionEventResponse
	if err := json.Unmarshal(response.Body, &result); err != nil {
		return VoidExecutionEventResponse{}, fmt.Errorf("decode void replay: %w", err)
	}
	return result, nil
}

func (a *Application) voidExecutionEventInScope(
	ctx context.Context,
	tx store.TxAccountScope,
	planID, eventID string,
	input VoidExecutionEventInput,
) (VoidExecutionEventResponse, error) {
	plan, err := a.repo.LockPlan(ctx, tx, planID)
	if err != nil {
		return VoidExecutionEventResponse{}, err
	}
	if plan.Status == PlanStatusArchived {
		return VoidExecutionEventResponse{}, ErrArchivedReadOnly
	}
	if plan.Status != PlanStatusInProgress {
		return VoidExecutionEventResponse{}, ErrInvalidPlanTransition
	}
	var shotID string
	if err := tx.QueryRow(ctx, "shoot_plan_execution_events", "shot_id", "plan_id = $2 AND id = $3", planID, eventID).Scan(&shotID); errors.Is(err, store.ErrNoRows) {
		return VoidExecutionEventResponse{}, ErrExecutionEventNotFound
	} else if err != nil {
		return VoidExecutionEventResponse{}, err
	}
	var executionRevision, sequence int64
	if err := tx.QueryRowForUpdate(ctx, "shoot_plan_shots", "execution_revision, next_event_seq",
		"plan_id = $2 AND id = $3", planID, shotID).Scan(&executionRevision, &sequence); err != nil {
		return VoidExecutionEventResponse{}, err
	}
	if executionRevision != input.ExpectedExecutionRevision {
		return VoidExecutionEventResponse{}, ErrExecutionRevisionConflict
	}
	voided, err := tx.Exists(ctx, "shoot_plan_execution_event_voids", "plan_id = $2 AND shot_id = $3 AND target_event_id = $4", planID, shotID, eventID)
	if err != nil {
		return VoidExecutionEventResponse{}, err
	}
	if voided {
		return VoidExecutionEventResponse{}, ErrExecutionEventAlreadyVoid
	}
	now := a.now().UTC()
	voidEvent := ExecutionFact{
		Kind: ExecutionFactVoid, ID: "void_" + uuid.NewString(), PlanID: planID, ShotID: shotID,
		TargetEventID: eventID, Sequence: sequence, Reason: input.Reason, VoidedAt: now, Revision: 1,
	}
	if err := tx.Insert(ctx, "shoot_plan_execution_event_voids",
		[]string{"id", "plan_id", "shot_id", "target_event_id", "shot_event_seq", "reason", "voided_by_account_id", "voided_at", "revision"},
		voidEvent.ID, planID, shotID, eventID, sequence, input.Reason, tx.AccountID(), now, 1); err != nil {
		return VoidExecutionEventResponse{}, err
	}
	facts, err := loadExecutionFacts(ctx, tx, planID)
	if err != nil {
		return VoidExecutionEventResponse{}, err
	}
	shotFacts := make([]ExecutionFact, 0)
	for _, fact := range facts {
		if fact.ShotID == shotID {
			shotFacts = append(shotFacts, fact)
		}
	}
	projection, err := ReplayCurrentOutcome(shotFacts)
	if err != nil {
		return VoidExecutionEventResponse{}, err
	}
	var projectedEventID any
	var outcome *CurrentOutcome
	if projection.Set {
		projectedEventID = projection.EventID
		projectionCopy := projection
		outcome = &projectionCopy
	}
	updated, err := tx.Update(ctx, "shoot_plan_shots",
		"execution_revision = execution_revision + 1, next_event_seq = next_event_seq + 1, current_outcome_event_id = $2, updated_at = clock_timestamp()",
		"plan_id = $3 AND id = $4 AND execution_revision = $5", projectedEventID, planID, shotID, executionRevision)
	if err != nil {
		return VoidExecutionEventResponse{}, err
	}
	if updated != 1 {
		return VoidExecutionEventResponse{}, ErrExecutionRevisionConflict
	}
	if err := advanceExecutionFactRevision(ctx, tx, planID, plan.ExecutionFactRevision); err != nil {
		return VoidExecutionEventResponse{}, err
	}
	return VoidExecutionEventResponse{
		VoidEvent: voidEvent, CurrentOutcome: outcome, ExecutionRevision: executionRevision + 1,
		ExecutionFactRevision: plan.ExecutionFactRevision + 1,
	}, nil
}

func advanceExecutionFactRevision(ctx context.Context, tx store.TxAccountScope, planID string, expected int64) error {
	updated, err := tx.Update(ctx, "shoot_plans",
		"execution_fact_revision = execution_fact_revision + 1, updated_at = clock_timestamp()",
		"id = $2 AND execution_fact_revision = $3", planID, expected)
	if err != nil {
		return err
	}
	if updated != 1 {
		return ErrExecutionRevisionConflict
	}
	return nil
}

func sameOptionalString(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func normalizeOptionalTextPointer(value *string) *string {
	if value == nil {
		return nil
	}
	normalized := strings.TrimSpace(*value)
	if normalized == "" {
		return nil
	}
	return &normalized
}

func validShotResult(result ShotResult) bool {
	return result == ShotResultCaptured || result == ShotResultSkipped || result == ShotResultCleared
}

func validSkipReason(reason string) bool {
	switch reason {
	case "preparation_missing", "time_insufficient", "location_unavailable", "subject_unavailable", "creative_change", "technical_failure", "other":
		return true
	default:
		return false
	}
}

func preparationMissingEventIDs(ctx context.Context, tx store.TxAccountScope, planID string, currentShotIDs []string) ([]string, error) {
	if len(currentShotIDs) == 0 {
		return []string{}, nil
	}
	current := make(map[string]struct{}, len(currentShotIDs))
	for _, id := range currentShotIDs {
		current[id] = struct{}{}
	}
	rows, err := tx.Query(ctx, "shoot_plan_execution_events", "id, shot_id",
		`plan_id = $2 AND result = $3 AND skip_reason = $4 AND capture_mode = $5
         AND NOT EXISTS (
             SELECT 1 FROM shoot_plan_execution_event_voids AS voids
             WHERE voids.account_id = shoot_plan_execution_events.account_id
               AND voids.plan_id = shoot_plan_execution_events.plan_id
               AND voids.shot_id = shoot_plan_execution_events.shot_id
               AND voids.target_event_id = shoot_plan_execution_events.id
         )`,
		planID, string(ShotResultSkipped), "preparation_missing", string(CaptureModeLive))
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0)
	for rows.Next() {
		var id, shotID string
		if err := rows.Scan(&id, &shotID); err != nil {
			rows.Close()
			return nil, err
		}
		if _, exists := current[shotID]; !exists {
			continue
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	sort.Strings(ids)
	return ids, nil
}

func (a *Application) completePlanInScope(
	ctx context.Context,
	tx store.TxAccountScope,
	plan ShootPlan,
	expectedExecutionFactRevision int64,
) (PlanTransitionResult, error) {
	if plan.ExecutionFactRevision != expectedExecutionFactRevision {
		return PlanTransitionResult{}, ErrExecutionRevisionConflict
	}
	shots, err := loadShots(ctx, tx, plan.ID)
	if err != nil {
		return PlanTransitionResult{}, err
	}
	allComplete := len(shots) > 0
	shotIDs := make([]string, len(shots))
	outcomeRefs := make([]string, len(shots))
	for index, shot := range shots {
		shotIDs[index] = shot.ID
		if shot.CurrentOutcome == nil || !shot.CurrentOutcome.Set {
			allComplete = false
			continue
		}
		outcomeRefs[index] = shot.CurrentOutcome.EventID
	}
	if _, err := TransitionPlanState(plan.Status, TransitionComplete, TransitionFacts{
		HasCurrentShots: len(shots) > 0, AllCurrentShotsComplete: allComplete,
	}); err != nil {
		return PlanTransitionResult{}, err
	}
	preparationIDs, err := preparationMissingEventIDs(ctx, tx, plan.ID, shotIDs)
	if err != nil {
		return PlanTransitionResult{}, err
	}
	finalizationCount, err := tx.Count(ctx, "shoot_plan_finalization_snapshots", "plan_id = $2", plan.ID)
	if err != nil {
		return PlanTransitionResult{}, err
	}
	finalizationRevision := finalizationCount + 1
	shotIDsJSON, err := json.Marshal(shotIDs)
	if err != nil {
		return PlanTransitionResult{}, err
	}
	outcomeRefsJSON, err := json.Marshal(outcomeRefs)
	if err != nil {
		return PlanTransitionResult{}, err
	}
	preparationJSON, err := json.Marshal(preparationIDs)
	if err != nil {
		return PlanTransitionResult{}, err
	}
	now := a.now().UTC()
	if err := tx.Insert(ctx, "shoot_plan_finalization_snapshots",
		[]string{"id", "plan_id", "finalization_revision", "plan_revision", "current_shot_ids", "outcome_event_refs", "preparation_missing_event_ids", "execution_fact_revision", "finalized_at"},
		"final_"+uuid.NewString(), plan.ID, finalizationRevision, plan.Revision+1, shotIDsJSON, outcomeRefsJSON,
		preparationJSON, plan.ExecutionFactRevision, now); err != nil {
		return PlanTransitionResult{}, err
	}
	if _, err := tx.Update(ctx, "shoot_plan_run_sessions", "closed_at = $2, last_active_at = $2", "plan_id = $3 AND closed_at IS NULL", now, plan.ID); err != nil {
		return PlanTransitionResult{}, err
	}
	updated, err := tx.Update(ctx, "shoot_plans",
		"status = $2, revision = revision + 1, completed_at = $3, updated_at = $3",
		"id = $4 AND revision = $5 AND execution_fact_revision = $6",
		string(PlanStatusCompleted), now, plan.ID, plan.Revision, expectedExecutionFactRevision)
	if err != nil {
		return PlanTransitionResult{}, err
	}
	if updated != 1 {
		return PlanTransitionResult{}, ErrExecutionRevisionConflict
	}
	return PlanTransitionResult{
		PlanID: plan.ID, Revision: plan.Revision + 1, Status: PlanStatusCompleted,
		FinalizationRevision: &finalizationRevision,
	}, nil
}
