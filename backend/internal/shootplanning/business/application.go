package business

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/samson/customer-manage-platform/backend/internal/platform/idempotency"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

type MutationResult struct {
	PlanID            string         `json:"plan_id"`
	Revision          int64          `json:"revision"`
	Status            string         `json:"status"`
	ChangedProjection map[string]any `json:"changed_projection"`
}

type Application struct {
	repo        Repository
	detail      detailReader
	idempotency *idempotency.Executor
	orders      OrderAdjustmentParticipant
	schedules   ScheduleDurationParticipant
	now         func() time.Time
}

type detailReader interface {
	LoadPlanSource(context.Context, readScope, string) (PlanSource, error)
	LoadFacts(context.Context, readScope, string) (FactsView, error)
	LoadEffectiveRules(context.Context, readScope) (EffectiveRules, error)
	LoadCurrentTargets(context.Context, readScope, string) (CurrentTargets, error)
	LoadLatestDrafts(context.Context, readScope, string) (map[DraftKind]DraftRecord, error)
}

func NewApplication(
	repo Repository,
	executor *idempotency.Executor,
	orders OrderAdjustmentParticipant,
	schedules ScheduleDurationParticipant,
) (*Application, error) {
	if executor == nil {
		return nil, errors.New("business idempotency executor is required")
	}
	return &Application{
		repo: repo, detail: repo, idempotency: executor, orders: orders, schedules: schedules, now: time.Now,
	}, nil
}

func (a *Application) GetDetail(ctx context.Context, scope store.AccountScope, planID string) (Detail, error) {
	var detail Detail
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		var err error
		detail, err = a.getDetailInScope(ctx, tx, strings.TrimSpace(planID))
		return err
	})
	return detail, err
}

func (a *Application) getDetailInScope(ctx context.Context, scope readScope, planID string) (Detail, error) {
	plan, err := a.detail.LoadPlanSource(ctx, scope, planID)
	if err != nil {
		return Detail{}, err
	}
	facts, err := a.detail.LoadFacts(ctx, scope, plan.ID)
	if err != nil {
		return Detail{}, err
	}
	rules, err := a.detail.LoadEffectiveRules(ctx, scope)
	if err != nil {
		return Detail{}, err
	}
	targets, err := a.detail.LoadCurrentTargets(ctx, scope, plan.ID)
	if err != nil {
		return Detail{}, err
	}
	drafts, err := a.detail.LoadLatestDrafts(ctx, scope, plan.ID)
	if err != nil {
		return Detail{}, err
	}
	detail := Detail{Facts: facts, PublicInputs: plan.PublicInputs, EffectiveRules: rules}
	if record, ok := drafts[DraftOrderAdjustment]; ok {
		view, err := a.orderView(record, plan, facts, rules, targets)
		if err != nil {
			return Detail{}, err
		}
		detail.OrderAdjustment = &view
	}
	if record, ok := drafts[DraftScheduleDuration]; ok {
		view, err := a.scheduleView(record, plan, facts, rules, targets)
		if err != nil {
			return Detail{}, err
		}
		detail.ScheduleDuration = &view
	}
	return detail, nil
}

func (a *Application) SetFacts(
	ctx context.Context,
	scope store.AccountScope,
	key, planID string,
	expectedPlanRevision, expectedFactsRevision int64,
	facts Facts,
) (MutationResult, error) {
	if err := ValidateFacts(facts); err != nil || expectedPlanRevision < 1 || expectedFactsRevision < 0 {
		if err != nil {
			return MutationResult{}, err
		}
		return MutationResult{}, ErrRevisionConflict
	}
	canonical, err := json.Marshal(struct {
		PlanID                string `json:"plan_id"`
		ExpectedPlanRevision  int64  `json:"expected_plan_revision"`
		ExpectedFactsRevision int64  `json:"expected_business_facts_revision"`
		Facts                 Facts  `json:"facts"`
	}{planID, expectedPlanRevision, expectedFactsRevision, facts})
	if err != nil {
		return MutationResult{}, err
	}
	response, err := a.idempotency.Execute(ctx, scope, idempotency.Request{
		Operation: idempotency.OperationShootPlanBusinessFacts,
		Key:       key, ResourceIdentity: idempotency.PlanResource(planID), CanonicalBody: canonical,
	}, func(tx store.TxAccountScope) (idempotency.StoredResponse, error) {
		plan, err := a.repo.LockPlanSource(ctx, tx, planID)
		if err != nil {
			return idempotency.StoredResponse{}, err
		}
		if err := mutablePlanStatus(plan.Status); err != nil {
			return idempotency.StoredResponse{}, err
		}
		view, nextPlan, changed, err := a.repo.ReplaceFacts(
			ctx, tx, plan, expectedPlanRevision, expectedFactsRevision, facts,
		)
		if err != nil {
			return idempotency.StoredResponse{}, err
		}
		projection := make(map[string]any)
		if changed {
			projection["business_facts"] = view
		}
		result := MutationResult{
			PlanID: nextPlan.ID, Revision: nextPlan.Revision, Status: nextPlan.Status,
			ChangedProjection: projection,
		}
		body, err := json.Marshal(result)
		return idempotency.StoredResponse{Status: 200, Body: body}, err
	})
	if err != nil {
		return MutationResult{}, err
	}
	var result MutationResult
	if err := json.Unmarshal(response.Body, &result); err != nil {
		return MutationResult{}, fmt.Errorf("decode business facts replay: %w", err)
	}
	return result, nil
}

func (a *Application) Generate(
	ctx context.Context,
	scope store.AccountScope,
	key, planID string,
	input GenerateInput,
) (GenerationResult, error) {
	if err := validateGenerateInput(input); err != nil {
		return GenerationResult{}, err
	}
	canonical, err := json.Marshal(struct {
		PlanID string `json:"plan_id"`
		GenerateInput
	}{planID, input})
	if err != nil {
		return GenerationResult{}, err
	}
	response, err := a.idempotency.Execute(ctx, scope, idempotency.Request{
		Operation: idempotency.OperationShootPlanBusinessDraftGenerate,
		Key:       key, ResourceIdentity: idempotency.PlanResource(planID), CanonicalBody: canonical,
	}, func(tx store.TxAccountScope) (idempotency.StoredResponse, error) {
		result, err := a.generateInScope(ctx, tx, planID, input)
		if err != nil {
			return idempotency.StoredResponse{}, err
		}
		body, err := json.Marshal(result)
		return idempotency.StoredResponse{Status: 201, Body: body}, err
	})
	if err != nil {
		return GenerationResult{}, err
	}
	var result GenerationResult
	if err := json.Unmarshal(response.Body, &result); err != nil {
		return GenerationResult{}, fmt.Errorf("decode business generation replay: %w", err)
	}
	return result, nil
}

func (a *Application) generateInScope(
	ctx context.Context,
	tx store.TxAccountScope,
	planID string,
	input GenerateInput,
) (GenerationResult, error) {
	plan, err := a.repo.LockPlanSource(ctx, tx, planID)
	if err != nil {
		return GenerationResult{}, err
	}
	if err := mutablePlanStatus(plan.Status); err != nil {
		return GenerationResult{}, err
	}
	if plan.Revision != input.ExpectedPlanRevision {
		return GenerationResult{}, ErrPlanRevisionConflict
	}
	facts, err := a.repo.LockFacts(ctx, tx, plan.ID)
	if err != nil {
		return GenerationResult{}, err
	}
	if facts.Revision != input.ExpectedFactsRevision {
		return GenerationResult{}, ErrRevisionConflict
	}
	targets, err := a.repo.LoadCurrentTargets(ctx, tx, plan.ID)
	if err != nil {
		return GenerationResult{}, err
	}
	rules, err := a.repo.LoadEffectiveRules(ctx, tx)
	if err != nil {
		return GenerationResult{}, err
	}

	result := GenerationResult{
		PlanID: plan.ID, PlanRevision: plan.Revision, BusinessFactsRevision: facts.Revision,
		RuleVersion: rules.RuleVersion,
	}
	orderRequested := slices.Contains(input.DraftKinds, DraftOrderAdjustment)
	scheduleRequested := slices.Contains(input.DraftKinds, DraftScheduleDuration)
	var orderEvaluation *OrderEvaluation
	var schedulePayload *ScheduleDraftPayload
	if orderRequested {
		item := GenerationItem{State: "unavailable"}
		result.OrderAdjustment = &item
		reason := orderUnavailable(targets)
		if reason == nil {
			evaluation, evaluationErr := EvaluateOrder(OrderEvaluationInput{
				Public: plan.PublicInputs, Facts: facts.Facts, Rules: rules.Profile,
				BasePrice: targets.Order.Price, AbsoluteTargetPrice: input.AbsoluteTargetPrice,
			})
			if errors.Is(evaluationErr, ErrCalculationOverflow) {
				value := UnavailableCalculation
				result.OrderAdjustment.Reason = &value
			} else if evaluationErr != nil {
				return GenerationResult{}, evaluationErr
			} else {
				orderEvaluation = &evaluation
			}
		} else {
			result.OrderAdjustment.Reason = reason
		}
	}
	if scheduleRequested {
		item := GenerationItem{State: "unavailable"}
		result.ScheduleDuration = &item
		payload, reason, scheduleErr := a.evaluateSchedule(facts.Facts, targets)
		if scheduleErr != nil {
			return GenerationResult{}, scheduleErr
		}
		if reason != nil {
			result.ScheduleDuration.Reason = reason
		} else {
			schedulePayload = payload
		}
	}
	if orderEvaluation == nil && schedulePayload == nil {
		unavailable := &DraftUnavailableError{}
		if result.OrderAdjustment != nil {
			unavailable.OrderAdjustment = result.OrderAdjustment.Reason
		}
		if result.ScheduleDuration != nil {
			unavailable.ScheduleDuration = result.ScheduleDuration.Reason
		}
		return GenerationResult{}, unavailable
	}

	generationID := "pbg_" + uuid.NewString()
	result.GenerationID = generationID
	now := a.clock()
	source, err := sourceSnapshot(plan, facts, rules, targets)
	if err != nil {
		return GenerationResult{}, err
	}
	if orderEvaluation != nil {
		payload := OrderDraftPayload{
			BasePrice: orderEvaluation.BasePrice, CalculationMode: orderEvaluation.CalculationMode,
			AbsoluteTargetPrice: orderEvaluation.AbsoluteTargetPrice, Lines: orderEvaluation.Lines,
			ProposedTotal: orderEvaluation.ProposedTotal, Warnings: orderEvaluation.Warnings,
			RuleVersion: rules.RuleVersion,
		}
		record, err := newDraftRecord(plan.ID, generationID, DraftOrderAdjustment, source, payload, now)
		if err != nil {
			return GenerationResult{}, err
		}
		if err := a.repo.InsertDraft(ctx, tx, record); err != nil {
			return GenerationResult{}, err
		}
		if err := a.repo.SupersedeFresh(ctx, tx, plan.ID, record.Kind, record.ID); err != nil {
			return GenerationResult{}, err
		}
		view, err := a.orderView(record, plan, facts, rules, targets)
		if err != nil {
			return GenerationResult{}, err
		}
		result.OrderAdjustment = &GenerationItem{State: "generated", OrderDraft: &view}
	}
	if schedulePayload != nil {
		record, err := newDraftRecord(plan.ID, generationID, DraftScheduleDuration, source, *schedulePayload, now)
		if err != nil {
			return GenerationResult{}, err
		}
		if err := a.repo.InsertDraft(ctx, tx, record); err != nil {
			return GenerationResult{}, err
		}
		if err := a.repo.SupersedeFresh(ctx, tx, plan.ID, record.Kind, record.ID); err != nil {
			return GenerationResult{}, err
		}
		view, err := a.scheduleView(record, plan, facts, rules, targets)
		if err != nil {
			return GenerationResult{}, err
		}
		result.ScheduleDuration = &GenerationItem{State: "generated", ScheduleDraft: &view}
	}
	return result, nil
}

func (a *Application) Decide(
	ctx context.Context,
	scope store.AccountScope,
	key, planID, draftID string,
	input DecisionInput,
) (DecisionResult, error) {
	if input.ExpectedDraftRevision < 1 || !validDecision(input.Decision) {
		return DecisionResult{}, ErrDraftDecision
	}
	canonical, err := json.Marshal(struct {
		PlanID  string `json:"plan_id"`
		DraftID string `json:"draft_id"`
		DecisionInput
	}{planID, draftID, input})
	if err != nil {
		return DecisionResult{}, err
	}
	response, err := a.idempotency.Execute(ctx, scope, idempotency.Request{
		Operation: idempotency.OperationShootPlanBusinessDraftDecision,
		Key:       key, ResourceIdentity: idempotency.BusinessDraftResource(planID, draftID), CanonicalBody: canonical,
	}, func(tx store.TxAccountScope) (idempotency.StoredResponse, error) {
		result, err := a.decideInScope(ctx, tx, planID, draftID, input)
		if err != nil {
			return idempotency.StoredResponse{}, err
		}
		body, err := json.Marshal(result)
		return idempotency.StoredResponse{Status: 200, Body: body}, err
	})
	if err != nil {
		return DecisionResult{}, err
	}
	var result DecisionResult
	if err := json.Unmarshal(response.Body, &result); err != nil {
		return DecisionResult{}, fmt.Errorf("decode business decision replay: %w", err)
	}
	return result, nil
}

func (a *Application) decideInScope(
	ctx context.Context,
	tx store.TxAccountScope,
	planID, draftID string,
	input DecisionInput,
) (DecisionResult, error) {
	if input.Decision == DecisionDismiss {
		plan, err := a.repo.LockPlanSource(ctx, tx, planID)
		if err != nil {
			return DecisionResult{}, err
		}
		if err := mutablePlanStatus(plan.Status); err != nil {
			return DecisionResult{}, err
		}
		record, err := a.repo.LockDraft(ctx, tx, planID, draftID)
		if err != nil {
			return DecisionResult{}, err
		}
		if record.Revision != input.ExpectedDraftRevision {
			return DecisionResult{}, ErrRevisionConflict
		}
		if record.TerminalStatus == StatusApplied {
			return DecisionResult{}, ErrDraftDecision
		}
		revision, err := a.repo.MarkDismissed(ctx, tx, record, a.clock())
		if err != nil {
			return DecisionResult{}, err
		}
		return DecisionResult{DraftID: record.ID, Kind: record.Kind, Status: StatusDismissed, Revision: revision}, nil
	}

	preRead, err := a.repo.LoadDraft(ctx, tx, planID, draftID)
	if err != nil {
		return DecisionResult{}, err
	}
	if preRead.Kind == DraftOrderAdjustment && input.Decision == DecisionApplyOrder {
		if a.orders == nil {
			return DecisionResult{}, errors.New("order adjustment participant is not configured")
		}
		var result DecisionResult
		_, err := a.orders.WithLockedTargetInScope(ctx, tx, preRead.Source.OrderID, func(locked LockedOrderScope) error {
			var callbackErr error
			result, callbackErr = a.applyOrderInLockedScope(ctx, tx, planID, draftID, input, locked)
			return callbackErr
		})
		return result, err
	}
	if preRead.Kind == DraftScheduleDuration && input.Decision == DecisionApplySchedule {
		if a.schedules == nil || preRead.Source.SlotID == nil {
			return DecisionResult{}, ErrScheduleCreate
		}
		var result DecisionResult
		_, err := a.schedules.WithLockedTargetInScope(ctx, tx, planID, *preRead.Source.SlotID, func(locked LockedScheduleScope) error {
			var callbackErr error
			result, callbackErr = a.applyScheduleInLockedScope(ctx, tx, planID, draftID, input, locked)
			return callbackErr
		})
		return result, err
	}
	return DecisionResult{}, ErrDraftDecision
}

func (a *Application) applyOrderInLockedScope(
	ctx context.Context,
	tx store.TxAccountScope,
	planID, draftID string,
	input DecisionInput,
	locked LockedOrderScope,
) (DecisionResult, error) {
	plan, facts, rules, targets, record, err := a.lockDecisionSources(ctx, tx, planID, draftID, input.ExpectedDraftRevision)
	if err != nil {
		return DecisionResult{}, err
	}
	view, err := a.orderView(record, plan, facts, rules, targets)
	if err != nil {
		return DecisionResult{}, err
	}
	if view.Status != StatusFresh {
		return DecisionResult{}, staleDraftError(record, view.StaleReason)
	}
	if !acknowledgementEqual(input.Acknowledgement, view.RequiredAcknowledgement) {
		return DecisionResult{}, ErrDraftDecision
	}
	if view.ProposedTotal == nil {
		return DecisionResult{}, ErrUnknownTotal
	}
	if view.RequiredAcknowledgement == nil {
		return DecisionResult{}, ErrNoMaterialChange
	}
	targetHash, _, err := FingerprintOrderTarget(locked.Target())
	if err != nil || targetHash != record.Source.OrderFingerprint {
		reason := StaleOrderTarget
		return DecisionResult{}, staleDraftError(record, &reason)
	}
	adjustmentID := "opa_" + uuid.NewString()
	applied, err := locked.Apply(ctx, ApplyOrderCommand{
		AdjustmentID: adjustmentID, PlanID: planID, DraftID: draftID,
		AfterPrice: *view.ProposedTotal, CalculationMode: view.CalculationMode,
		BasePrice: view.BasePrice, Lines: view.Lines, Warnings: view.Warnings,
		RuleVersion: view.RuleVersion, BeforeTargetFingerprint: record.Source.OrderFingerprint,
	})
	if err != nil {
		return DecisionResult{}, err
	}
	revision, err := a.repo.MarkApplied(ctx, tx, record, &adjustmentID, a.clock())
	if err != nil {
		return DecisionResult{}, err
	}
	return DecisionResult{
		DraftID: draftID, Kind: record.Kind, Status: StatusApplied, Revision: revision,
		AppliedTarget: &AppliedTarget{
			TargetID: applied.Target.ID, AdjustmentID: applied.AdjustmentID,
			BeforePrice: applied.BeforePrice, AfterPrice: intPtrValue(applied.AfterPrice),
		},
	}, nil
}

func (a *Application) applyScheduleInLockedScope(
	ctx context.Context,
	tx store.TxAccountScope,
	planID, draftID string,
	input DecisionInput,
	locked LockedScheduleScope,
) (DecisionResult, error) {
	plan, facts, rules, targets, record, err := a.lockDecisionSources(ctx, tx, planID, draftID, input.ExpectedDraftRevision)
	if err != nil {
		return DecisionResult{}, err
	}
	view, err := a.scheduleView(record, plan, facts, rules, targets)
	if err != nil {
		return DecisionResult{}, err
	}
	if view.Status != StatusFresh {
		return DecisionResult{}, staleDraftError(record, view.StaleReason)
	}
	if view.TargetMode != ScheduleUpdateExisting || view.ProposedEndAt == nil {
		return DecisionResult{}, ErrScheduleCreate
	}
	if !acknowledgementEqual(input.Acknowledgement, view.RequiredAcknowledgement) {
		return DecisionResult{}, ErrDraftDecision
	}
	if view.RequiredAcknowledgement == nil {
		return DecisionResult{}, ErrNoMaterialChange
	}
	targetHash, _, err := FingerprintScheduleTarget(locked.Target())
	if err != nil || record.Source.SlotFingerprint == nil || targetHash != *record.Source.SlotFingerprint {
		reason := StaleSlotTarget
		return DecisionResult{}, staleDraftError(record, &reason)
	}
	applied, err := locked.ApplyEnd(ctx, ApplyScheduleCommand{
		PlanID: planID, DraftID: draftID, ProposedEndAt: *view.ProposedEndAt,
	})
	if err != nil {
		return DecisionResult{}, err
	}
	revision, err := a.repo.MarkApplied(ctx, tx, record, nil, a.clock())
	if err != nil {
		return DecisionResult{}, err
	}
	return DecisionResult{
		DraftID: draftID, Kind: record.Kind, Status: StatusApplied, Revision: revision,
		AppliedTarget: &AppliedTarget{
			TargetID: applied.Target.ID, BeforeEndAt: &applied.BeforeEndAt, AfterEndAt: &applied.AfterEndAt,
		},
	}, nil
}

func (a *Application) lockDecisionSources(
	ctx context.Context,
	tx store.TxAccountScope,
	planID, draftID string,
	expectedDraftRevision int64,
) (PlanSource, FactsView, EffectiveRules, CurrentTargets, DraftRecord, error) {
	plan, err := a.repo.LockPlanSource(ctx, tx, planID)
	if err != nil {
		return PlanSource{}, FactsView{}, EffectiveRules{}, CurrentTargets{}, DraftRecord{}, err
	}
	if err := mutablePlanStatus(plan.Status); err != nil {
		return PlanSource{}, FactsView{}, EffectiveRules{}, CurrentTargets{}, DraftRecord{}, err
	}
	facts, err := a.repo.LockFacts(ctx, tx, planID)
	if err != nil {
		return PlanSource{}, FactsView{}, EffectiveRules{}, CurrentTargets{}, DraftRecord{}, err
	}
	record, err := a.repo.LockDraft(ctx, tx, planID, draftID)
	if err != nil {
		return PlanSource{}, FactsView{}, EffectiveRules{}, CurrentTargets{}, DraftRecord{}, err
	}
	if record.Revision != expectedDraftRevision || record.TerminalStatus != StatusFresh {
		return PlanSource{}, FactsView{}, EffectiveRules{}, CurrentTargets{}, DraftRecord{}, ErrRevisionConflict
	}
	rules, err := a.repo.LoadEffectiveRules(ctx, tx)
	if err != nil {
		return PlanSource{}, FactsView{}, EffectiveRules{}, CurrentTargets{}, DraftRecord{}, err
	}
	targets, err := a.repo.LoadCurrentTargets(ctx, tx, planID)
	return plan, facts, rules, targets, record, err
}

func (a *Application) orderView(
	record DraftRecord,
	plan PlanSource,
	facts FactsView,
	rules EffectiveRules,
	targets CurrentTargets,
) (OrderDraftView, error) {
	var payload OrderDraftPayload
	if err := json.Unmarshal(record.Payload, &payload); err != nil {
		return OrderDraftView{}, fmt.Errorf("decode order business draft: %w", err)
	}
	status, reason, err := a.effectiveDraftStatus(record, plan, facts, rules, targets, payload.ScheduleTargetMode())
	if err != nil {
		return OrderDraftView{}, err
	}
	view := OrderDraftView{
		ID: record.ID, GenerationID: record.GenerationID, Kind: record.Kind,
		Status: status, StaleReason: reason, Revision: record.Revision,
		OrderDraftPayload: payload, CreatedAt: record.CreatedAt, ExpiresAt: record.ExpiresAt,
	}
	if status == StatusFresh && payload.ProposedTotal != nil &&
		(payload.BasePrice == nil || *payload.ProposedTotal != *payload.BasePrice) {
		view.RequiredAcknowledgement = OrderAcknowledgement()
	}
	return view, nil
}

func (a *Application) scheduleView(
	record DraftRecord,
	plan PlanSource,
	facts FactsView,
	rules EffectiveRules,
	targets CurrentTargets,
) (ScheduleDraftView, error) {
	var payload ScheduleDraftPayload
	if err := json.Unmarshal(record.Payload, &payload); err != nil {
		return ScheduleDraftView{}, fmt.Errorf("decode schedule business draft: %w", err)
	}
	status, reason, err := a.effectiveDraftStatus(record, plan, facts, rules, targets, payload.TargetMode)
	if err != nil {
		return ScheduleDraftView{}, err
	}
	view := ScheduleDraftView{
		ID: record.ID, GenerationID: record.GenerationID, Kind: record.Kind,
		Status: status, StaleReason: reason, Revision: record.Revision,
		ScheduleDraftPayload: payload, CreatedAt: record.CreatedAt, ExpiresAt: record.ExpiresAt,
	}
	if status == StatusFresh && payload.TargetMode == ScheduleUpdateExisting &&
		payload.OriginalEndAt != nil && payload.ProposedEndAt != nil &&
		!payload.OriginalEndAt.Equal(*payload.ProposedEndAt) {
		view.RequiredAcknowledgement = ScheduleAcknowledgement()
	}
	return view, nil
}

func (OrderDraftPayload) ScheduleTargetMode() ScheduleTargetMode { return "" }

func (a *Application) effectiveDraftStatus(
	record DraftRecord,
	plan PlanSource,
	facts FactsView,
	rules EffectiveRules,
	targets CurrentTargets,
	scheduleMode ScheduleTargetMode,
) (DraftStatus, *StaleReason, error) {
	if record.TerminalStatus == StatusApplied || record.TerminalStatus == StatusDismissed {
		return record.TerminalStatus, nil, nil
	}
	stale := func(reason StaleReason) (DraftStatus, *StaleReason, error) {
		return StatusStale, &reason, nil
	}
	if record.SupersededByDraftID != nil {
		return stale(StaleSuperseded)
	}
	if !a.clock().Before(record.ExpiresAt) {
		return stale(StaleExpired)
	}
	if plan.Revision != record.Source.PlanRevision {
		return stale(StalePlanRevision)
	}
	if facts.Revision != record.Source.BusinessFactsRevision {
		return stale(StaleFactsRevision)
	}
	if targets.CRM.ConnectionRevision != record.Source.ConnectionRevision {
		return stale(StaleCRMConnection)
	}
	if !equalInt64(targets.CRM.ProjectionRevision, record.Source.ProjectionRevision) {
		return stale(StaleCRMProjection)
	}
	if rules.RuleVersion != record.Source.RuleVersion {
		return stale(StaleRuleVersion)
	}
	if targets.Order == nil || targets.Order.ID != record.Source.OrderID {
		return stale(StaleOrderTargetMissing)
	}
	if record.Kind == DraftOrderAdjustment {
		if targets.Order.Status == "cancelled" {
			return stale(StaleOrderStage)
		}
		fingerprint, _, err := FingerprintOrderTarget(*targets.Order)
		if err != nil {
			return "", nil, err
		}
		if fingerprint != record.Source.OrderFingerprint {
			return stale(StaleOrderTarget)
		}
		return StatusFresh, nil, nil
	}
	if scheduleMode == ScheduleCreateNew {
		if targets.Order.Status != "consulting" && targets.Order.Status != "scheduled" {
			return stale(StaleScheduleStage)
		}
		if targets.Slot != nil {
			return stale(StaleSlotTarget)
		}
		return StatusFresh, nil, nil
	}
	if targets.Slot == nil || record.Source.SlotID == nil || targets.Slot.ID != *record.Source.SlotID {
		return stale(StaleSlotTargetMissing)
	}
	if targets.Order.Status != "consulting" && targets.Order.Status != "scheduled" {
		return stale(StaleScheduleStage)
	}
	fingerprint, _, err := FingerprintScheduleTarget(*targets.Slot)
	if err != nil {
		return "", nil, err
	}
	if record.Source.SlotFingerprint == nil || fingerprint != *record.Source.SlotFingerprint {
		return stale(StaleSlotTarget)
	}
	return StatusFresh, nil, nil
}

func (a *Application) evaluateSchedule(facts Facts, targets CurrentTargets) (*ScheduleDraftPayload, *UnavailableReason, error) {
	if targets.Order == nil {
		value := UnavailableOrderRequired
		return nil, &value, nil
	}
	duration, err := EvaluateDuration(facts.EstimatedDurationMinutes, nil)
	if err != nil {
		var reason UnavailableReason
		switch {
		case errors.Is(err, ErrDurationUnknown):
			reason = UnavailableDurationUnknown
		case errors.Is(err, ErrDurationNotPositive):
			reason = UnavailableDurationPositive
		case errors.Is(err, ErrDurationOutOfRange):
			reason = UnavailableDurationRange
		default:
			return nil, nil, err
		}
		return nil, &reason, nil
	}
	if targets.Order.Status != "consulting" && targets.Order.Status != "scheduled" {
		value := UnavailableScheduleStage
		return nil, &value, nil
	}
	if targets.Slot == nil {
		return &ScheduleDraftPayload{
			TargetMode: ScheduleCreateNew, BasisMinutes: duration.BasisMinutes,
			Warnings: []Warning{},
		}, nil, nil
	}
	if !targets.Slot.StartsAt.After(a.clock()) {
		value := UnavailableScheduleNotFuture
		return nil, &value, nil
	}
	calculated, err := EvaluateDuration(facts.EstimatedDurationMinutes, &targets.Slot.StartsAt)
	if err != nil {
		return nil, nil, err
	}
	warnings := make([]Warning, 0, 1)
	if calculated.ProposedEndAt != nil && calculated.ProposedEndAt.Equal(targets.Slot.EndsAt) {
		warnings = append(warnings, WarningNoMaterialChange)
	}
	return &ScheduleDraftPayload{
		TargetMode: ScheduleUpdateExisting, OriginalStartAt: &targets.Slot.StartsAt,
		OriginalEndAt: &targets.Slot.EndsAt, ProposedEndAt: calculated.ProposedEndAt,
		BasisMinutes: calculated.BasisMinutes, Warnings: warnings,
	}, nil, nil
}

func sourceSnapshot(
	plan PlanSource,
	facts FactsView,
	rules EffectiveRules,
	targets CurrentTargets,
) (SourceSnapshot, error) {
	if targets.Order == nil {
		return SourceSnapshot{}, ErrDraftUnavailable
	}
	orderFingerprint, _, err := FingerprintOrderTarget(*targets.Order)
	if err != nil {
		return SourceSnapshot{}, err
	}
	source := SourceSnapshot{
		PlanRevision: plan.Revision, BusinessFactsRevision: facts.Revision,
		PlannedLookCount: cloneInt(plan.PublicInputs.PlannedLookCount), CurrentShotCount: plan.PublicInputs.CurrentShotCount,
		ConnectionRevision: targets.CRM.ConnectionRevision, ProjectionRevision: cloneInt64(targets.CRM.ProjectionRevision),
		OrderID: targets.Order.ID, OrderFingerprint: orderFingerprint,
		RuleVersion: rules.RuleVersion,
	}
	if targets.Slot != nil {
		fingerprint, _, err := FingerprintScheduleTarget(*targets.Slot)
		if err != nil {
			return SourceSnapshot{}, err
		}
		source.SlotID = stringPtr(targets.Slot.ID)
		source.SlotFingerprint = stringPtr(fingerprint)
	}
	return source, nil
}

func newDraftRecord(
	planID, generationID string,
	kind DraftKind,
	source SourceSnapshot,
	payload any,
	now time.Time,
) (DraftRecord, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return DraftRecord{}, err
	}
	return DraftRecord{
		ID: "pbd_" + uuid.NewString(), PlanID: planID, GenerationID: generationID,
		Kind: kind, Source: source, Payload: body, TerminalStatus: StatusFresh,
		Revision: 1, CreatedAt: now, ExpiresAt: now.Add(DraftTTL),
	}, nil
}

func validateGenerateInput(input GenerateInput) error {
	if input.ExpectedPlanRevision < 1 || input.ExpectedFactsRevision < 0 || len(input.DraftKinds) < 1 || len(input.DraftKinds) > 2 {
		return ErrInvalidInput
	}
	seen := make(map[DraftKind]struct{}, len(input.DraftKinds))
	for _, kind := range input.DraftKinds {
		if kind != DraftOrderAdjustment && kind != DraftScheduleDuration {
			return ErrInvalidInput
		}
		if _, exists := seen[kind]; exists {
			return ErrInvalidInput
		}
		seen[kind] = struct{}{}
	}
	if input.AbsoluteTargetPrice != nil && !slices.Contains(input.DraftKinds, DraftOrderAdjustment) {
		return ErrInvalidInput
	}
	return validateMoney(input.AbsoluteTargetPrice)
}

func orderUnavailable(targets CurrentTargets) *UnavailableReason {
	if targets.Order == nil {
		value := UnavailableOrderRequired
		return &value
	}
	if targets.Order.Status == "cancelled" {
		value := UnavailableOrderCancelled
		return &value
	}
	return nil
}

func mutablePlanStatus(status string) error {
	switch status {
	case "draft", "ready", "in_progress":
		return nil
	case "completed":
		return ErrReopenRequired
	case "archived":
		return ErrArchivedReadOnly
	default:
		return ErrNotFound
	}
}

func acknowledgementEqual(left, right *Acknowledgement) bool {
	return left != nil && right != nil && left.Version == right.Version && slices.Equal(left.Effects, right.Effects)
}

func staleDraftError(record DraftRecord, reason *StaleReason) error {
	value := StaleSuperseded
	if reason != nil {
		value = *reason
	}
	return &StaleDraftError{
		DraftID:  record.ID,
		Kind:     record.Kind,
		Reason:   value,
		Revision: record.Revision,
	}
}

func validDecision(decision Decision) bool {
	return decision == DecisionDismiss || decision == DecisionApplyOrder || decision == DecisionApplySchedule
}

func equalInt64(left, right *int64) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

func stringPtr(value string) *string { return &value }

func (a *Application) clock() time.Time {
	if a.now == nil {
		return time.Now().UTC()
	}
	return a.now().UTC()
}
