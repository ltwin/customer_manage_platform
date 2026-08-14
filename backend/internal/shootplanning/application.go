package shootplanning

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/planningmedia"
	"github.com/samson/customer-manage-platform/backend/internal/platform/idempotency"
	"github.com/samson/customer-manage-platform/backend/internal/platform/planningcapability"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning/crm"
)

var (
	ErrValidation                   = errors.New("shoot planning validation failed")
	ErrReopenRequired               = errors.New("reopen_required")
	ErrReadinessNotFound            = errors.New("readiness_not_found")
	ErrShotNotFound                 = errors.New("shot_not_found")
	ErrReadinessAssignmentActive    = errors.New("readiness_assignment_active")
	ErrArchiveAcknowledgement       = errors.New("archive_acknowledgement_required")
	ErrArchiveWiringMismatch        = errors.New("archive capability wiring mismatch")
	ErrReadinessGuardWiringMismatch = errors.New("readiness removal guard wiring mismatch")
	ErrExecutionHistoryAckRequired  = errors.New("execution_history_ack_required")
)

func validationError(message string) error {
	return fmt.Errorf("%w: %s", ErrValidation, message)
}

type ReadinessRemovalGuard interface {
	AssertRemovableInScope(context.Context, store.TxAccountScope, string, string) error
}

type ShotAccessRefProjector interface {
	BatchShotAccessRefsInScope(context.Context, store.TxAccountScope, string, []string) (map[string][]planningmedia.AssetAccessRef, error)
}

type DisabledReadinessRemovalGuard struct{}

func (DisabledReadinessRemovalGuard) AssertRemovableInScope(context.Context, store.TxAccountScope, string, string) error {
	return nil
}

type PlanArchiveReminderParticipant interface {
	OnPlanArchivedInScope(context.Context, store.TxAccountScope, string, int64, int64, time.Time) error
}

type DisabledPlanArchiveReminderParticipant struct{}

func (DisabledPlanArchiveReminderParticipant) OnPlanArchivedInScope(context.Context, store.TxAccountScope, string, int64, int64, time.Time) error {
	return nil
}

type ArchiveAcknowledgement struct {
	Version string   `json:"version"`
	Effects []string `json:"effects"`
}

type ArchiveAcknowledgementRequiredError struct {
	Required ArchiveAcknowledgement
}

func (e *ArchiveAcknowledgementRequiredError) Error() string {
	return ErrArchiveAcknowledgement.Error()
}
func (e *ArchiveAcknowledgementRequiredError) Unwrap() error { return ErrArchiveAcknowledgement }

type ArchiveImpactPolicy interface {
	Capability() planningcapability.ArchiveCapability
	RequiredAcknowledgement() ArchiveAcknowledgement
}

type CoreOnlyArchiveImpactPolicyV1 struct{}

func (CoreOnlyArchiveImpactPolicyV1) Capability() planningcapability.ArchiveCapability {
	return planningcapability.ArchiveCapabilityCore
}
func (CoreOnlyArchiveImpactPolicyV1) RequiredAcknowledgement() ArchiveAcknowledgement {
	return ArchiveAcknowledgementRegistryV1(planningcapability.ArchiveCapabilityCore)
}

type PlanningShareArchiveImpactPolicyV1 struct{}

func (PlanningShareArchiveImpactPolicyV1) Capability() planningcapability.ArchiveCapability {
	return planningcapability.ArchiveCapabilityPlanningShare
}
func (PlanningShareArchiveImpactPolicyV1) RequiredAcknowledgement() ArchiveAcknowledgement {
	return ArchiveAcknowledgementRegistryV1(planningcapability.ArchiveCapabilityPlanningShare)
}

type PlanningShareReminderArchiveImpactPolicyV1 struct{}

func (PlanningShareReminderArchiveImpactPolicyV1) Capability() planningcapability.ArchiveCapability {
	return planningcapability.ArchiveCapabilityReminder
}
func (PlanningShareReminderArchiveImpactPolicyV1) RequiredAcknowledgement() ArchiveAcknowledgement {
	return ArchiveAcknowledgementRegistryV1(planningcapability.ArchiveCapabilityReminder)
}

type ApplicationOption func(*Application) error

func WithPlanReadyObservationSink(sink PlanReadyObservationSink) ApplicationOption {
	return func(app *Application) error {
		if sink == nil {
			return ErrPlanReadyObservationWiringMismatch
		}
		app.readyObservationSink = sink
		return nil
	}
}

func WithIngestionRoutesEnabled() ApplicationOption {
	return func(app *Application) error {
		app.ingestionRoutesEnabled = true
		return nil
	}
}

func WithReadinessRemovalGuard(guard ReadinessRemovalGuard) ApplicationOption {
	return func(app *Application) error {
		if guard == nil {
			return errors.New("readiness removal guard is nil")
		}
		app.removalGuard = guard
		return nil
	}
}

func WithArchiveImpactPolicy(policy ArchiveImpactPolicy) ApplicationOption {
	return func(app *Application) error {
		if policy == nil {
			return errors.New("archive impact policy is nil")
		}
		app.archivePolicy = policy
		return nil
	}
}

func WithArchiveReminderParticipant(participant PlanArchiveReminderParticipant) ApplicationOption {
	return func(app *Application) error {
		if participant == nil {
			return errors.New("archive reminder participant is nil")
		}
		app.archiveParticipant = participant
		return nil
	}
}

// WithCRMReminder wires the CRM closed-union reminder participant.
// enabled=true requires a non-disabled real adapter; enabled=false requires Disabled.
func WithCRMReminder(participant crm.CRMReminderLifecycleParticipant, enabled bool) ApplicationOption {
	return func(app *Application) error {
		if app.crm == nil {
			return errors.New("crm engine is required")
		}
		_, err := app.crm.WithReminder(participant, enabled)
		return err
	}
}

type Application struct {
	repo                   PostgresRepository
	idempotency            *idempotency.Executor
	removalGuard           ReadinessRemovalGuard
	archivePolicy          ArchiveImpactPolicy
	archiveParticipant     PlanArchiveReminderParticipant
	now                    func() time.Time
	mediaProjector         ShotAccessRefProjector
	readyObservationSink   PlanReadyObservationSink
	ingestionRoutesEnabled bool
	crm                    *crm.Engine
}

func WithShotAccessRefProjector(projector ShotAccessRefProjector) ApplicationOption {
	return func(app *Application) error { app.mediaProjector = projector; return nil }
}

func NewApplication(repo PostgresRepository, executor *idempotency.Executor, options ...ApplicationOption) (*Application, error) {
	if executor == nil {
		return nil, errors.New("idempotency executor is required")
	}
	app := &Application{
		repo: repo, idempotency: executor,
		removalGuard:         DisabledReadinessRemovalGuard{},
		archivePolicy:        CoreOnlyArchiveImpactPolicyV1{},
		archiveParticipant:   DisabledPlanArchiveReminderParticipant{},
		readyObservationSink: NoopPlanReadyObservationSink{},
		crm:                  crm.NewEngine(),
		now:                  time.Now,
	}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("application option is nil")
		}
		if err := option(app); err != nil {
			return nil, err
		}
	}
	capability := app.archivePolicy.Capability()
	if !planningcapability.Known(capability) {
		return nil, ErrArchiveWiringMismatch
	}
	registryAcknowledgement := ArchiveAcknowledgementRegistryV1(capability)
	policyAcknowledgement := app.archivePolicy.RequiredAcknowledgement()
	if !exactAcknowledgement(&policyAcknowledgement, registryAcknowledgement) {
		return nil, ErrArchiveWiringMismatch
	}
	participantDisabled := isDisabledArchiveParticipant(app.archiveParticipant)
	guardDisabled := isDisabledReadinessRemovalGuard(app.removalGuard)
	if capability == planningcapability.ArchiveCapabilityReminder {
		if participantDisabled {
			return nil, ErrArchiveWiringMismatch
		}
	} else if !participantDisabled {
		return nil, ErrArchiveWiringMismatch
	}
	if capability != planningcapability.ArchiveCapabilityCore && guardDisabled {
		return nil, ErrReadinessGuardWiringMismatch
	}
	if app.ingestionRoutesEnabled && isNoopPlanReadyObservationSink(app.readyObservationSink) {
		return nil, ErrPlanReadyObservationWiringMismatch
	}
	return app, nil
}

func isDisabledArchiveParticipant(participant PlanArchiveReminderParticipant) bool {
	switch participant.(type) {
	case DisabledPlanArchiveReminderParticipant, *DisabledPlanArchiveReminderParticipant:
		return true
	default:
		return false
	}
}

func isDisabledReadinessRemovalGuard(guard ReadinessRemovalGuard) bool {
	switch guard.(type) {
	case DisabledReadinessRemovalGuard, *DisabledReadinessRemovalGuard:
		return true
	default:
		return false
	}
}

func (a *Application) CreatePlan(ctx context.Context, scope store.AccountScope, key string, input CreatePlanInput) (PlanDetail, error) {
	normalized, err := normalizeCreatePlanInput(input)
	if err != nil {
		return PlanDetail{}, err
	}
	canonical, _ := json.Marshal(struct {
		Title   string `json:"title"`
		Subject string `json:"subject"`
	}{Title: normalized.Title, Subject: normalized.Subject})
	response, err := a.idempotency.Execute(ctx, scope, idempotency.Request{
		Operation: idempotency.OperationShootPlanCreate, Key: key,
		ResourceIdentity: idempotency.PlanCollectionResource(), CanonicalBody: canonical,
	}, func(tx store.TxAccountScope) (idempotency.StoredResponse, error) {
		capability, err := tx.ArchiveCapability().Current(ctx)
		if err != nil {
			return idempotency.StoredResponse{}, err
		}
		if capability.Capability != a.archivePolicy.Capability() {
			return idempotency.StoredResponse{}, ErrArchiveWiringMismatch
		}
		plan, err := a.repo.CreateInScope(ctx, tx, normalized)
		if err != nil {
			return idempotency.StoredResponse{}, err
		}
		detail, err := a.repo.DetailInScope(ctx, tx, plan.ID, false)
		if err != nil {
			return idempotency.StoredResponse{}, err
		}
		detail.RequiredArchiveAcknowledgement = ArchiveAcknowledgementRegistryV1(capability.Capability)
		if detail.RequiredArchiveAcknowledgement.Version == "" {
			return idempotency.StoredResponse{}, ErrArchiveWiringMismatch
		}
		body, err := json.Marshal(detail)
		return idempotency.StoredResponse{Status: 201, Body: body}, err
	})
	if err != nil {
		return PlanDetail{}, err
	}
	var detail PlanDetail
	if err := json.Unmarshal(response.Body, &detail); err != nil {
		return PlanDetail{}, fmt.Errorf("decode create plan replay: %w", err)
	}
	return detail, nil
}

func (a *Application) ListPlans(ctx context.Context, scope store.AccountScope, filter ListPlansFilter) (ListPlansResult, error) {
	return a.repo.List(ctx, scope, filter)
}

func (a *Application) GetPlan(ctx context.Context, scope store.AccountScope, id string, includeHistory bool) (PlanDetail, error) {
	var detail PlanDetail
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		capability, err := tx.ArchiveCapability().Current(ctx)
		if err != nil {
			return err
		}
		if capability.Capability != a.archivePolicy.Capability() {
			return ErrArchiveWiringMismatch
		}
		detail, err = a.repo.DetailInScope(ctx, tx, id, includeHistory)
		if err != nil {
			return err
		}
		detail.RequiredArchiveAcknowledgement = ArchiveAcknowledgementRegistryV1(capability.Capability)
		if detail.RequiredArchiveAcknowledgement.Version == "" {
			return ErrArchiveWiringMismatch
		}
		return nil
	})
	return detail, err
}

type PlanMutationResult struct {
	PlanID            string         `json:"plan_id"`
	Revision          int64          `json:"revision"`
	Status            PlanStatus     `json:"status"`
	ChangedProjection map[string]any `json:"changed_projection"`
}

func (a *Application) CRM() *crm.Engine {
	if a == nil {
		return nil
	}
	return a.crm
}

func (a *Application) ApplyCRMLink(
	ctx context.Context,
	scope store.AccountScope,
	key, planID string,
	expectedRevision int64,
	command crm.Command,
) (PlanMutationResult, error) {
	planID = strings.TrimSpace(planID)
	if planID == "" || expectedRevision < 1 || command.Kind == "" {
		return PlanMutationResult{}, validationError("crm command invalid")
	}
	canonical, err := marshalCRMCommand(expectedRevision, command)
	if err != nil {
		return PlanMutationResult{}, err
	}
	return a.executePlanMutation(ctx, scope, idempotency.Request{
		Operation: idempotency.OperationShootPlanCRMLink, Key: key,
		ResourceIdentity: idempotency.PlanResource(planID), CanonicalBody: canonical,
	}, func(tx store.TxAccountScope) (PlanMutationResult, error) {
		outcome, err := a.crm.ApplyCommandInScope(ctx, tx, planID, expectedRevision, command)
		if err != nil {
			return PlanMutationResult{}, mapCRMError(err)
		}
		return PlanMutationResult{
			PlanID: outcome.PlanID, Revision: outcome.Revision, Status: PlanStatus(outcome.Status),
			ChangedProjection: crmChangedProjection(outcome),
		}, nil
	}, "decode crm command replay")
}

const crmSourceRetryAttempts = 3

func (a *Application) executePlanMutation(
	ctx context.Context,
	scope store.AccountScope,
	request idempotency.Request,
	mutate func(store.TxAccountScope) (PlanMutationResult, error),
	decodeErr string,
) (PlanMutationResult, error) {
	for attempt := 0; attempt < crmSourceRetryAttempts; attempt++ {
		response, err := a.idempotency.Execute(ctx, scope, request, func(tx store.TxAccountScope) (idempotency.StoredResponse, error) {
			result, err := mutate(tx)
			if err != nil {
				return idempotency.StoredResponse{}, err
			}
			body, err := json.Marshal(result)
			return idempotency.StoredResponse{Status: 200, Body: body}, err
		})
		if errors.Is(err, crm.ErrSourceChangedRetry) {
			continue
		}
		if err != nil {
			return PlanMutationResult{}, mapCRMError(err)
		}
		var result PlanMutationResult
		if err := json.Unmarshal(response.Body, &result); err != nil {
			return PlanMutationResult{}, fmt.Errorf("%s: %w", decodeErr, err)
		}
		return result, nil
	}
	return PlanMutationResult{}, crm.ErrSourceChanged
}

func marshalCRMCommand(expectedRevision int64, command crm.Command) ([]byte, error) {
	body := map[string]any{"expected_revision": expectedRevision, "operation": string(command.Kind)}
	if command.CustomerID != nil {
		body["customer_id"] = strings.TrimSpace(*command.CustomerID)
	}
	if command.OrderID != nil {
		body["order_id"] = strings.TrimSpace(*command.OrderID)
	}
	if command.ProjectionRevision != nil {
		body["projection_revision"] = *command.ProjectionRevision
	}
	return json.Marshal(body)
}

func crmChangedProjection(outcome crm.ApplyOutcome) map[string]any {
	projection := map[string]any{
		"crm_state":           string(outcome.Connection.State),
		"connection_revision": outcome.Connection.ConnectionRevision,
		"customer_id":         outcome.Connection.CustomerID,
		"order_id":            outcome.Connection.OrderID,
	}
	if outcome.Projection != nil && outcome.Projection.Exists {
		projection["projection_revision"] = outcome.Projection.ProjectionRevision
		projection["schedule_status"] = string(outcome.Projection.Status)
		projection["apply_suppressed"] = outcome.Projection.ApplySuppressed
	}
	return projection
}

func mapCRMError(err error) error {
	switch {
	case errors.Is(err, crm.ErrPlanRevisionConflict):
		return ErrPlanRevisionConflict
	case errors.Is(err, crm.ErrArchivedReadOnly):
		return ErrArchivedReadOnly
	case errors.Is(err, crm.ErrReopenRequired):
		return ErrReopenRequired
	case errors.Is(err, crm.ErrNotFound):
		return ErrPlanNotFound
	default:
		return err
	}
}

func (a *Application) ApplyPlanCommand(
	ctx context.Context,
	scope store.AccountScope,
	key, planID string,
	expectedRevision int64,
	command PlanCommand,
) (PlanMutationResult, error) {
	planID = strings.TrimSpace(planID)
	if command == nil || planID == "" || expectedRevision < 1 {
		return PlanMutationResult{}, validationError("plan command invalid")
	}
	canonical, err := marshalPlanCommand(expectedRevision, command)
	if err != nil {
		return PlanMutationResult{}, err
	}
	return a.executePlanMutation(ctx, scope, idempotency.Request{
		Operation: idempotency.OperationShootPlanCommand, Key: key,
		ResourceIdentity: idempotency.PlanResource(planID), CanonicalBody: canonical,
	}, func(tx store.TxAccountScope) (PlanMutationResult, error) {
		return a.applyPlanCommandInScope(ctx, tx, planID, expectedRevision, command)
	}, "decode plan command replay")
}

type PlanTransition struct {
	ExpectedRevision              int64
	Kind                          PlanTransitionKind
	ExpectedExecutionFactRevision *int64
	ArchiveAcknowledgement        *ArchiveAcknowledgement
}

type PlanTransitionResult struct {
	PlanID               string     `json:"plan_id"`
	Revision             int64      `json:"revision"`
	Status               PlanStatus `json:"status"`
	FinalizationRevision *int64     `json:"finalization_revision,omitempty"`
}

func (a *Application) TransitionPlan(ctx context.Context, scope store.AccountScope, key, planID string, transition PlanTransition) (PlanTransitionResult, error) {
	planID = strings.TrimSpace(planID)
	if planID == "" || transition.ExpectedRevision < 1 {
		return PlanTransitionResult{}, validationError("plan transition invalid")
	}
	canonical, err := marshalPlanTransition(transition)
	if err != nil {
		return PlanTransitionResult{}, err
	}
	response, err := a.idempotency.Execute(ctx, scope, idempotency.Request{
		Operation: idempotency.OperationShootPlanTransition, Key: key,
		ResourceIdentity: idempotency.TransitionResource(planID), CanonicalBody: canonical,
	}, func(tx store.TxAccountScope) (idempotency.StoredResponse, error) {
		result, err := a.transitionPlanInScope(ctx, tx, planID, transition)
		if err != nil {
			return idempotency.StoredResponse{}, err
		}
		if transition.Kind == TransitionMarkReady {
			if err := a.readyObservationSink.AccumulateAndRecordFirstReadyInScope(ctx, tx, PlanReadyObservationFact{
				PlanID: planID,
				TickID: "ready:" + key,
			}); err != nil {
				return idempotency.StoredResponse{}, err
			}
		}
		body, err := json.Marshal(result)
		return idempotency.StoredResponse{Status: 200, Body: body}, err
	})
	if err != nil {
		return PlanTransitionResult{}, err
	}
	var result PlanTransitionResult
	if err := json.Unmarshal(response.Body, &result); err != nil {
		return PlanTransitionResult{}, err
	}
	return result, nil
}

func ArchiveAcknowledgementRegistryV1(capability planningcapability.ArchiveCapability) ArchiveAcknowledgement {
	switch capability {
	case planningcapability.ArchiveCapabilityCore:
		return ArchiveAcknowledgement{Version: "core-v1", Effects: []string{"plan_becomes_read_only", "execution_history_retained"}}
	case planningcapability.ArchiveCapabilityPlanningShare:
		return ArchiveAcknowledgement{Version: "planning-share-v1", Effects: []string{
			"plan_becomes_read_only", "execution_history_retained", "active_share_links_become_unavailable",
			"share_feedback_retained", "share_assignments_retained",
		}}
	case planningcapability.ArchiveCapabilityReminder:
		return ArchiveAcknowledgement{Version: "planning-share-reminder-v1", Effects: []string{
			"plan_becomes_read_only", "execution_history_retained", "active_share_links_become_unavailable",
			"share_feedback_retained", "share_assignments_retained", "active_assignment_reminders_withdrawn",
		}}
	default:
		return ArchiveAcknowledgement{}
	}
}

func normalizeCreatePlanInput(input CreatePlanInput) (CreatePlanInput, error) {
	input.Title = strings.TrimSpace(input.Title)
	input.Subject = strings.TrimSpace(input.Subject)
	if runeLen(input.Title) < 1 || runeLen(input.Title) > 160 || runeLen(input.Subject) < 1 || runeLen(input.Subject) > 240 {
		return CreatePlanInput{}, validationError("title 或 subject 非法")
	}
	return input, nil
}

func marshalPlanCommand(expectedRevision int64, command PlanCommand) ([]byte, error) {
	body := map[string]any{"expected_revision": expectedRevision, "operation": command.operation()}
	switch command := command.(type) {
	case UpdateBriefCommand:
		if command.Title != nil {
			body["title"] = strings.TrimSpace(*command.Title)
		}
		if command.Subject != nil {
			body["subject"] = strings.TrimSpace(*command.Subject)
		}
		if command.CreativeBrief != nil {
			body["creative_brief"] = canonicalCreativeBriefPatch(*command.CreativeBrief)
		}
	case UpsertShotCommand:
		if command.ShotID != nil {
			body["shot_id"] = strings.TrimSpace(*command.ShotID)
		}
		body["shot"] = canonicalShotWrite(command.Shot)
		if command.InsertAfterShotID != nil {
			body["insert_after_shot_id"] = strings.TrimSpace(*command.InsertAfterShotID)
		}
	case ReorderShotsCommand:
		body["ordered_shot_ids"] = command.OrderedShotIDs
	case RemoveShotCommand:
		body["shot_id"] = strings.TrimSpace(command.ShotID)
		body["acknowledge_execution_history"] = command.AcknowledgeExecutionHistory
	case UpsertReadinessCommand:
		if command.ReadinessID != nil {
			body["readiness_id"] = strings.TrimSpace(*command.ReadinessID)
		}
		body["item"] = canonicalReadinessWrite(command.Item)
	case RemoveReadinessCommand:
		body["readiness_id"] = strings.TrimSpace(command.ReadinessID)
	case SetPreflightCommand:
		body["readiness_id"] = strings.TrimSpace(command.ReadinessID)
		body["preflight_status"] = command.PreflightStatus
	case LinkReadinessCommand:
		body["shot_id"] = strings.TrimSpace(command.ShotID)
		body["readiness_id"] = strings.TrimSpace(command.ReadinessID)
	case UnlinkReadinessCommand:
		body["shot_id"] = strings.TrimSpace(command.ShotID)
		body["readiness_id"] = strings.TrimSpace(command.ReadinessID)
	case SetPublicScaleCommand:
		putOptionalInt(body, "planned_look_count", command.PlannedLookCount)
		putOptionalInt(body, "planned_scene_count", command.PlannedSceneCount)
	case SetExecutionWindowCommand:
		body["starts_at"] = command.StartsAt
		body["ends_at"] = command.EndsAt
		body["timezone"] = command.Timezone
		body["live_window_starts_at"] = command.LiveWindowStartsAt
		body["live_window_ends_at"] = command.LiveWindowEndsAt
	case ClearExecutionWindowCommand:
	default:
		return nil, validationError("unknown plan command")
	}
	return json.Marshal(body)
}

func marshalPlanTransition(transition PlanTransition) ([]byte, error) {
	payload := any(map[string]any{})
	switch transition.Kind {
	case TransitionMarkReady, TransitionStart, TransitionReopen:
		if transition.ExpectedExecutionFactRevision != nil || transition.ArchiveAcknowledgement != nil {
			return nil, validationError("transition payload invalid")
		}
	case TransitionComplete:
		if transition.ExpectedExecutionFactRevision == nil || *transition.ExpectedExecutionFactRevision < 0 || transition.ArchiveAcknowledgement != nil {
			return nil, validationError("expected execution fact revision is required")
		}
		payload = map[string]any{"expected_execution_fact_revision": *transition.ExpectedExecutionFactRevision}
	case TransitionArchive:
		if transition.ExpectedExecutionFactRevision != nil {
			return nil, validationError("archive transition payload invalid")
		}
		payload = transition.ArchiveAcknowledgement
	default:
		return nil, ErrInvalidPlanTransition
	}
	return json.Marshal(map[string]any{
		"expected_revision": transition.ExpectedRevision,
		"transition":        transition.Kind,
		"payload":           payload,
	})
}

func canonicalCreativeBriefPatch(patch CreativeBriefPatch) map[string]any {
	result := make(map[string]any)
	putOptionalString(result, "work_title", patch.WorkTitle)
	putOptionalString(result, "character_name", patch.CharacterName)
	putOptionalString(result, "theme_statement", patch.ThemeStatement)
	putOptionalString(result, "mood", patch.Mood)
	if patch.VisualKeywords.Specified {
		if patch.VisualKeywords.Null {
			result["visual_keywords"] = nil
		} else {
			values := make([]string, len(patch.VisualKeywords.Value))
			for index, value := range patch.VisualKeywords.Value {
				values[index] = strings.TrimSpace(value)
			}
			result["visual_keywords"] = values
		}
	}
	return result
}

func canonicalShotWrite(input ShotWrite) map[string]any {
	result := make(map[string]any)
	if input.Title != nil {
		result["title"] = strings.TrimSpace(*input.Title)
	}
	putOptionalString(result, "scene", input.Scene)
	putOptionalString(result, "action", input.Action)
	putOptionalString(result, "expression", input.Expression)
	putOptionalString(result, "composition", input.Composition)
	putOptionalString(result, "lighting_text", input.Lighting)
	putOptionalString(result, "notes", input.Notes)
	putOptionalString(result, "framing_tag", input.FramingTag)
	putOptionalString(result, "lighting_direction_tag", input.LightingDirectionTag)
	putOptionalString(result, "lighting_quality_tag", input.LightingQualityTag)
	putOptionalString(result, "palette_tag", input.PaletteTag)
	putOptionalString(result, "shot_type_tag", input.ShotTypeTag)
	return result
}

func canonicalReadinessWrite(input ReadinessWrite) map[string]any {
	result := make(map[string]any)
	if input.Category != nil {
		result["category"] = *input.Category
	}
	if input.Title != nil {
		result["title"] = strings.TrimSpace(*input.Title)
	}
	if input.Requirement != nil {
		result["requirement"] = *input.Requirement
	}
	if input.PreflightStatus != nil {
		result["preflight_status"] = *input.PreflightStatus
	}
	if input.ResponsibilityHint != nil {
		result["responsibility_hint"] = *input.ResponsibilityHint
	}
	putOptionalInt(result, "default_preparation_lead_days", input.DefaultPreparationLeadDays)
	return result
}

func putOptionalString(target map[string]any, key string, value Optional[string]) {
	if !value.Specified {
		return
	}
	if value.Null {
		target[key] = nil
		return
	}
	target[key] = strings.TrimSpace(value.Value)
}

func putOptionalInt(target map[string]any, key string, value Optional[int]) {
	if !value.Specified {
		return
	}
	if value.Null {
		target[key] = nil
		return
	}
	target[key] = value.Value
}

func exactAcknowledgement(actual *ArchiveAcknowledgement, required ArchiveAcknowledgement) bool {
	if actual == nil || actual.Version != required.Version || len(actual.Effects) != len(required.Effects) {
		return false
	}
	for index := range required.Effects {
		if actual.Effects[index] != required.Effects[index] {
			return false
		}
	}
	return true
}

func newPlanID() string { return "spl_" + uuid.NewString() }
