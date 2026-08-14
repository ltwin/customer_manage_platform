package reminder

import "time"

const (
	AssignmentKindReadiness     = "readiness"
	AssignmentKindOnSiteSupport = "on_site_support"

	SourceStateActive  = "active"
	SourceStateRevoked = "revoked"

	ProjectionStateGrouped          = "grouped"
	ProjectionStateUnscheduled      = "unscheduled"
	ProjectionStateIneligibleOnSite = "ineligible_on_site"
	ProjectionStateRevoked          = "revoked"

	GroupStateCurrent   = "current"
	GroupStateWithdrawn = "withdrawn"

	WithdrawReasonSourceChanged     = "source_changed"
	WithdrawReasonSlotUnavailable   = "slot_unavailable"
	WithdrawReasonOrderInactive     = "order_inactive"
	WithdrawReasonPlanArchived      = "plan_archived"
	WithdrawReasonAssignmentRevoked = "assignment_revoked"
	WithdrawReasonShootStarted      = "shoot_started"

	canonicalGroupVersion = "plan-assignment-reminder-group.v1"
	slotTypeShoot         = "shoot"
)

// SafeAssignmentInput 是 planshare 安全投影形状（无 receipt/token/feedback）。
type SafeAssignmentInput struct {
	AssignmentID                 string
	AssignmentRevision           int64
	AssignmentKind               string
	SourceState                  string // active | revoked
	ReadinessItemID              *string
	ContentSnapshot              string
	ClaimedByDisplayNameSnapshot *string
	ContentFingerprint           string
	PreparationLeadDaysSnapshot  *int
	LeadRuleVersion              *string
	SourceOccurredAt             time.Time
}

// PlanOrderSlotSidecar 是 reducer 所需的 plan/order/slot 事实侧车（禁止 Order.shot_at）。
type PlanOrderSlotSidecar struct {
	PlanArchived bool
	OrderID      *string
	OrderActive  bool // 非 cancelled/deleted
	SlotID       *string
	SlotType     string
	SlotStartAt  *time.Time
}

// ReduceDesiredReminderGroupsInput 是 pure reducer 输入。
type ReduceDesiredReminderGroupsInput struct {
	AccountID            string
	PlanID               string
	Assignments          []SafeAssignmentInput
	Sidecar              PlanOrderSlotSidecar
	Timezone             string
	Now                  time.Time
	ActivationGeneration int64
}

// DesiredSourceState 是 source 投影目标态。
type DesiredSourceState struct {
	AssignmentID                 string
	AssignmentRevision           int64
	AssignmentKind               string
	ReadinessItemID              *string
	ContentSnapshot              string
	ClaimedByDisplayNameSnapshot *string
	ContentFingerprint           string
	PreparationLeadDaysSnapshot  *int
	LeadRuleVersion              *string
	SourceState                  string
	ProjectionState              string
	SourceOccurredAt             time.Time
}

// DesiredMember 是 desired group 成员（按 assignment_id 排序后写入 position）。
type DesiredMember struct {
	AssignmentID       string
	AssignmentRevision int64
	ContentFingerprint string
	ContentSnapshot    string
	Position           int
}

// DesiredReminderGroup 是 material group 目标态。
type DesiredReminderGroup struct {
	OrderID              string
	SlotID               string
	DueDate              time.Time // date-only UTC midnight
	TimezoneSnapshot     string
	ValidUntil           time.Time // exclusive StartAt
	LeadRuleVersion      string
	GroupFingerprint     string
	ActivationGeneration int64
	Members              []DesiredMember
}

// ReduceDesiredReminderGroupsOutput 是 pure reducer 输出。
type ReduceDesiredReminderGroupsOutput struct {
	Sources []DesiredSourceState
	Groups  []DesiredReminderGroup
}

// CurrentReminderGroup 是落盘 current/withdrawn group 的只读投影（diff 输入）。
type CurrentReminderGroup struct {
	GroupID              string
	PlanID               string
	OrderID              string
	SlotID               string
	DueDate              time.Time
	TimezoneSnapshot     string
	ValidUntil           time.Time
	LeadRuleVersion      string
	GroupFingerprint     string
	ValidityRevision     int64
	State                string
	ReminderID           string
	ReminderStatus       string
	ReminderCreatedAt    time.Time
	ActivationGeneration int64
}

// ProjectionWritePlan 是 truth-table 写出计划（repository 负责锁与落盘）。
type ProjectionWritePlan struct {
	SourceUpserts   []DesiredSourceState
	NoOpGroupIDs    []string
	TemporalUpdates []TemporalGroupUpdate
	Withdrawals     []GroupWithdrawal
	Creates         []DesiredReminderGroup
}

// TemporalGroupUpdate 保留同一 occurrence，仅推进 temporal metadata。
type TemporalGroupUpdate struct {
	GroupID          string
	ReminderID       string
	ValidUntil       time.Time
	TimezoneSnapshot string
	NextValidityRev  int64
}

// GroupWithdrawal 撤回 current group；pending reminder → dismissed。
type GroupWithdrawal struct {
	GroupID    string
	ReminderID string
	Reason     string
	Dismiss    bool // true when reminder was pending
}
