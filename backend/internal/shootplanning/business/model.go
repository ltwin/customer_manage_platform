package business

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const DraftTTL = 24 * time.Hour

var (
	ErrNotFound             = errors.New("business_not_found")
	ErrRevisionConflict     = errors.New("business_revision_conflict")
	ErrPlanRevisionConflict = errors.New("plan_revision_conflict")
	ErrDraftUnavailable     = errors.New("business_draft_unavailable")
	ErrDraftStale           = errors.New("stale_business_draft")
	ErrDraftDecision        = errors.New("business_draft_decision_invalid")
	ErrInvalidInput         = errors.New("business_input_invalid")
	ErrNoMaterialChange     = errors.New("business_draft_no_material_change")
	ErrUnknownTotal         = errors.New("business_draft_unknown_total")
	ErrScheduleCreate       = errors.New("business_draft_requires_schedule_create")
	ErrReopenRequired       = errors.New("reopen_required")
	ErrArchivedReadOnly     = errors.New("archived_read_only")
)

type DraftUnavailableError struct {
	OrderAdjustment  *UnavailableReason
	ScheduleDuration *UnavailableReason
}

func (e *DraftUnavailableError) Error() string { return ErrDraftUnavailable.Error() }
func (e *DraftUnavailableError) Unwrap() error { return ErrDraftUnavailable }

type StaleDraftError struct {
	DraftID  string
	Kind     DraftKind
	Reason   StaleReason
	Revision int64
}

func (e *StaleDraftError) Error() string { return ErrDraftStale.Error() }
func (e *StaleDraftError) Unwrap() error { return ErrDraftStale }

type DraftKind string

const (
	DraftOrderAdjustment  DraftKind = "order_adjustment"
	DraftScheduleDuration DraftKind = "schedule_duration"
)

type DraftStatus string

const (
	StatusFresh     DraftStatus = "fresh"
	StatusStale     DraftStatus = "stale"
	StatusApplied   DraftStatus = "applied"
	StatusDismissed DraftStatus = "dismissed"
)

type StaleReason string

const (
	StaleSuperseded         StaleReason = "superseded"
	StaleExpired            StaleReason = "expired"
	StalePlanRevision       StaleReason = "plan_revision_changed"
	StaleFactsRevision      StaleReason = "business_facts_revision_changed"
	StaleCRMConnection      StaleReason = "crm_connection_changed"
	StaleCRMProjection      StaleReason = "crm_projection_changed"
	StaleRuleVersion        StaleReason = "rule_version_changed"
	StaleOrderTargetMissing StaleReason = "order_target_missing"
	StaleOrderStage         StaleReason = "order_stage_ineligible"
	StaleOrderTarget        StaleReason = "order_target_changed"
	StaleSlotTargetMissing  StaleReason = "slot_target_missing"
	StaleScheduleStage      StaleReason = "schedule_stage_ineligible"
	StaleSlotTarget         StaleReason = "slot_target_changed"
)

type FactsView struct {
	Facts
	Revision  int64      `json:"revision"`
	UpdatedAt *time.Time `json:"updated_at"`
}

type RuleOverrides map[string]*int

type EffectiveRules struct {
	Profile     RuleProfile   `json:"-"`
	Overrides   RuleOverrides `json:"overrides"`
	Revision    int64         `json:"-"`
	RuleVersion string        `json:"rule_version"`
	UnknownKeys []string      `json:"unknown_keys"`
	ProfileHash string        `json:"-"`
}

type PlanSource struct {
	ID           string
	Status       string
	Revision     int64
	PublicInputs PublicInputs
}

type CRMSource struct {
	ConnectionRevision int64
	ProjectionRevision *int64
	OrderID            *string
	SlotID             *string
}

type SourceSnapshot struct {
	PlanRevision          int64   `json:"plan_revision"`
	BusinessFactsRevision int64   `json:"business_facts_revision"`
	PlannedLookCount      *int    `json:"planned_look_count"`
	CurrentShotCount      int     `json:"current_shot_count"`
	ConnectionRevision    int64   `json:"connection_revision"`
	ProjectionRevision    *int64  `json:"projection_revision"`
	OrderID               string  `json:"order_id"`
	OrderFingerprint      string  `json:"order_target_fingerprint"`
	SlotID                *string `json:"slot_id"`
	SlotFingerprint       *string `json:"slot_target_fingerprint"`
	RuleVersion           string  `json:"rule_version"`
}

type Acknowledgement struct {
	Version string   `json:"version"`
	Effects []string `json:"effects"`
}

func OrderAcknowledgement() *Acknowledgement {
	return &Acknowledgement{
		Version: "order-adjustment-v1",
		Effects: []string{
			"order_price_updated",
			"price_adjustment_audit_recorded",
			"schedule_unchanged",
			"customer_not_notified",
		},
	}
}

func ScheduleAcknowledgement() *Acknowledgement {
	return &Acknowledgement{
		Version: "schedule-duration-v1",
		Effects: []string{
			"schedule_end_updated",
			"order_price_unchanged",
			"customer_not_notified",
		},
	}
}

type OrderDraftPayload struct {
	BasePrice           *int             `json:"base_price"`
	CalculationMode     CalculationMode  `json:"calculation_mode"`
	AbsoluteTargetPrice *int             `json:"absolute_target_price"`
	Lines               []AdjustmentLine `json:"lines"`
	ProposedTotal       *int             `json:"proposed_total"`
	Warnings            []Warning        `json:"warnings"`
	RuleVersion         string           `json:"rule_version"`
}

type ScheduleTargetMode string

const (
	ScheduleUpdateExisting ScheduleTargetMode = "update_existing"
	ScheduleCreateNew      ScheduleTargetMode = "create_new"
)

type ScheduleDraftPayload struct {
	TargetMode      ScheduleTargetMode `json:"target_mode"`
	OriginalStartAt *time.Time         `json:"original_start_at"`
	OriginalEndAt   *time.Time         `json:"original_end_at"`
	ProposedEndAt   *time.Time         `json:"proposed_end_at"`
	BasisMinutes    int                `json:"basis_minutes"`
	Warnings        []Warning          `json:"warnings"`
	RuleVersion     string             `json:"rule_version"`
}

type DraftRecord struct {
	ID                  string
	PlanID              string
	GenerationID        string
	Kind                DraftKind
	Source              SourceSnapshot
	Payload             json.RawMessage
	TerminalStatus      DraftStatus
	Revision            int64
	SupersededByDraftID *string
	AppliedAdjustmentID *string
	CreatedAt           time.Time
	ExpiresAt           time.Time
	AppliedAt           *time.Time
	DismissedAt         *time.Time
}

type OrderDraftView struct {
	ID           string       `json:"id"`
	GenerationID string       `json:"generation_id"`
	Kind         DraftKind    `json:"kind"`
	Status       DraftStatus  `json:"status"`
	StaleReason  *StaleReason `json:"stale_reason,omitempty"`
	Revision     int64        `json:"revision"`
	OrderDraftPayload
	RequiredAcknowledgement *Acknowledgement `json:"required_acknowledgement"`
	CreatedAt               time.Time        `json:"created_at"`
	ExpiresAt               time.Time        `json:"expires_at"`
}

type ScheduleDraftView struct {
	ID           string       `json:"id"`
	GenerationID string       `json:"generation_id"`
	Kind         DraftKind    `json:"kind"`
	Status       DraftStatus  `json:"status"`
	StaleReason  *StaleReason `json:"stale_reason,omitempty"`
	Revision     int64        `json:"revision"`
	ScheduleDraftPayload
	RequiredAcknowledgement *Acknowledgement `json:"required_acknowledgement"`
	CreatedAt               time.Time        `json:"created_at"`
	ExpiresAt               time.Time        `json:"expires_at"`
}

type Detail struct {
	Facts            FactsView          `json:"facts"`
	PublicInputs     PublicInputs       `json:"public_inputs"`
	EffectiveRules   EffectiveRules     `json:"effective_rules"`
	OrderAdjustment  *OrderDraftView    `json:"order_adjustment"`
	ScheduleDuration *ScheduleDraftView `json:"schedule_duration"`
}

type GenerateInput struct {
	ExpectedPlanRevision  int64       `json:"expected_plan_revision"`
	ExpectedFactsRevision int64       `json:"expected_business_facts_revision"`
	DraftKinds            []DraftKind `json:"draft_kinds"`
	AbsoluteTargetPrice   *int        `json:"absolute_target_price"`
}

type UnavailableReason string

const (
	UnavailableOrderRequired     UnavailableReason = "order_required"
	UnavailableOrderCancelled    UnavailableReason = "order_cancelled"
	UnavailableCalculation       UnavailableReason = "business_calculation_overflow"
	UnavailableDurationUnknown   UnavailableReason = "duration_unknown"
	UnavailableDurationPositive  UnavailableReason = "duration_not_positive"
	UnavailableDurationRange     UnavailableReason = "duration_out_of_range"
	UnavailableScheduleStage     UnavailableReason = "schedule_stage_ineligible"
	UnavailableScheduleNotFuture UnavailableReason = "schedule_slot_not_future"
)

type GenerationItem struct {
	State         string             `json:"state"`
	OrderDraft    *OrderDraftView    `json:"order_draft,omitempty"`
	ScheduleDraft *ScheduleDraftView `json:"schedule_draft,omitempty"`
	Reason        *UnavailableReason `json:"reason,omitempty"`
}

type GenerationResult struct {
	GenerationID          string          `json:"generation_id"`
	PlanID                string          `json:"plan_id"`
	PlanRevision          int64           `json:"plan_revision"`
	BusinessFactsRevision int64           `json:"business_facts_revision"`
	RuleVersion           string          `json:"rule_version"`
	OrderAdjustment       *GenerationItem `json:"order_adjustment,omitempty"`
	ScheduleDuration      *GenerationItem `json:"schedule_duration,omitempty"`
}

type Decision string

const (
	DecisionDismiss       Decision = "dismiss"
	DecisionApplyOrder    Decision = "apply_order_adjustment"
	DecisionApplySchedule Decision = "apply_schedule_duration"
)

type DecisionInput struct {
	ExpectedDraftRevision int64            `json:"expected_draft_revision"`
	Decision              Decision         `json:"decision"`
	Acknowledgement       *Acknowledgement `json:"acknowledgement,omitempty"`
}

type AppliedTarget struct {
	TargetID     string     `json:"target_id"`
	AdjustmentID string     `json:"adjustment_id,omitempty"`
	BeforePrice  *int       `json:"before_price"`
	AfterPrice   *int       `json:"after_price"`
	BeforeEndAt  *time.Time `json:"before_end_at"`
	AfterEndAt   *time.Time `json:"after_end_at"`
}

type DecisionResult struct {
	DraftID       string         `json:"draft_id"`
	Kind          DraftKind      `json:"kind"`
	Status        DraftStatus    `json:"status"`
	Revision      int64          `json:"revision"`
	AppliedTarget *AppliedTarget `json:"applied_target,omitempty"`
}

type ApplyOrderCommand struct {
	AdjustmentID            string
	PlanID                  string
	DraftID                 string
	AfterPrice              int
	CalculationMode         CalculationMode
	BasePrice               *int
	Lines                   []AdjustmentLine
	Warnings                []Warning
	RuleVersion             string
	BeforeTargetFingerprint string
}

type AppliedOrder struct {
	Target            OrderTarget
	AdjustmentID      string
	BeforePrice       *int
	AfterPrice        int
	BeforeFingerprint string
	AfterFingerprint  string
}

type LockedOrderScope interface {
	Target() OrderTarget
	Apply(context.Context, ApplyOrderCommand) (AppliedOrder, error)
}

type OrderAdjustmentParticipant interface {
	WithLockedTargetInScope(
		context.Context,
		store.TxAccountScope,
		string,
		func(LockedOrderScope) error,
	) (AppliedOrder, error)
}

type ApplyScheduleCommand struct {
	PlanID        string
	DraftID       string
	ProposedEndAt time.Time
}

type AppliedSchedule struct {
	Target      ScheduleTarget
	BeforeEndAt time.Time
	AfterEndAt  time.Time
}

type LockedScheduleScope interface {
	Target() ScheduleTarget
	ApplyEnd(context.Context, ApplyScheduleCommand) (AppliedSchedule, error)
}

type ScheduleDurationParticipant interface {
	WithLockedTargetInScope(
		context.Context,
		store.TxAccountScope,
		string,
		string,
		func(LockedScheduleScope) error,
	) (AppliedSchedule, error)
}
