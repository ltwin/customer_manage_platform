package crm

import "time"

const RuleVersion = 1

type ConnectionState string

const (
	StateIndependent    ConnectionState = "independent"
	StateCustomerLinked ConnectionState = "customer_linked"
	StateOrderLinked    ConnectionState = "order_linked"
	StateOrderCancelled ConnectionState = "order_cancelled"
	StateOrderDeleted   ConnectionState = "order_deleted"
)

type ProjectionStatus string

const (
	StatusMissingSlot            ProjectionStatus = "missing_slot"
	StatusActiveApplied          ProjectionStatus = "active_applied"
	StatusActiveManualOverride   ProjectionStatus = "active_manual_override"
	StatusActiveUnapplied        ProjectionStatus = "active_unapplied"
	StatusInactivePast           ProjectionStatus = "inactive_past"
	StatusInactiveOrderCancelled ProjectionStatus = "inactive_order_cancelled"
	StatusInactiveOrderDeleted   ProjectionStatus = "inactive_order_deleted"
	StatusInactiveUnlinked       ProjectionStatus = "inactive_unlinked"
)

type CommandKind string

const (
	KindLinkCustomer   CommandKind = "link_customer"
	KindLinkOrder      CommandKind = "link_order"
	KindUnlinkOrder    CommandKind = "unlink_order"
	KindUnlinkCustomer CommandKind = "unlink_customer"
	KindAdopt          CommandKind = "adopt_schedule_projection"
	KindSetManual      CommandKind = "set_execution_window"
	KindClearWindow    CommandKind = "clear_execution_window"
	KindOrderCancelled CommandKind = "order_cancelled"
	KindOrderDeleted   CommandKind = "order_deleted"
	KindSchedule       CommandKind = "schedule_changed"
	KindCustomerMerged CommandKind = "customer_merged"
)

type EventKind string

const (
	EventCustomerLinked    EventKind = "customer_linked"
	EventOrderLinked       EventKind = "order_linked"
	EventOrderUnlinked     EventKind = "order_unlinked"
	EventCustomerUnlinked  EventKind = "customer_unlinked"
	EventCustomerMerged    EventKind = "customer_merged"
	EventOrderCancelled    EventKind = "order_cancelled"
	EventOrderDeleted      EventKind = "order_deleted"
	EventScheduleProjected EventKind = "schedule_projected"
	EventScheduleCleared   EventKind = "schedule_cleared"
	EventScheduleAdopted   EventKind = "schedule_adopted"
	EventManualOverrode    EventKind = "manual_overrode"
	EventWindowSuppressed  EventKind = "window_suppressed"
)

type PlanLinkChange string

const (
	PlanLinkChangeLink   PlanLinkChange = "link"
	PlanLinkChangeUnlink PlanLinkChange = "unlink"
	PlanLinkChangeRelink PlanLinkChange = "relink"
)

type OrderLifecycleChange string

const (
	OrderLifecycleCancelled OrderLifecycleChange = "cancelled"
	OrderLifecycleDeleted   OrderLifecycleChange = "deleted"
)

type ScheduleChange string

const (
	ScheduleCreate      ScheduleChange = "create"
	ScheduleUpdate      ScheduleChange = "update"
	ScheduleDelete      ScheduleChange = "delete"
	ScheduleTypeChanged ScheduleChange = "type_changed"
	ScheduleOrderMoved  ScheduleChange = "order_moved"
)

type LinkedOrderSnapshot struct {
	OrderID      string    `json:"order_id"`
	CustomerID   string    `json:"customer_id"`
	Title        *string   `json:"title,omitempty"`
	PackageName  *string   `json:"package_name,omitempty"`
	StatusAtLink string    `json:"status_at_link"`
	LinkedAt     time.Time `json:"linked_at"`
}

type Connection struct {
	PlanID              string
	CustomerID          *string
	OrderID             *string
	LinkEpochID         *string
	LinkedOrderSnapshot *LinkedOrderSnapshot
	State               ConnectionState
	ConnectionRevision  int64
	NextEventSeq        int64
}

type Projection struct {
	PlanID                string
	OrderID               *string
	SlotID                *string
	SlotSourceFingerprint string
	StartsAt              *time.Time
	EndsAt                *time.Time
	Timezone              *string
	Status                ProjectionStatus
	ApplySuppressed       bool
	RuleVersion           int
	ProjectionRevision    int64
	Exists                bool
}

type Window struct {
	Source             string
	SourceRef          *string
	StartsAt           time.Time
	EndsAt             time.Time
	Timezone           string
	LiveWindowStartsAt time.Time
	LiveWindowEndsAt   time.Time
	RuleVersion        int
	Revision           int64
}

type SlotFact struct {
	ID       string
	OrderID  string
	Type     string
	StartAt  time.Time
	EndAt    time.Time
	Revision int64
}

type OrderFact struct {
	ID          string
	CustomerID  string
	Status      string
	Title       *string
	PackageName *string
}

type CustomerFact struct {
	ID     string
	Status string
}

type PlanFact struct {
	ID       string
	Status   string
	Revision int64
}

type Command struct {
	Kind                  CommandKind
	CustomerID            *string
	OrderID               *string
	ProjectionRevision    *int64
	Manual                *Window
	MergeTargetCustomerID *string
	ScheduleChange        ScheduleChange
}

type Event struct {
	Kind                    EventKind
	FromRefs                map[string]any
	ToRefs                  map[string]any
	LinkEpochSnapshot       *LinkedOrderSnapshot
	SourceFingerprint       string
	ConnectionRevisionAfter *int64
	ProjectionRevisionAfter *int64
	PlanRevisionAfter       *int64
	WindowRevisionAfter     *int64
}

type Result struct {
	Connection          Connection
	Projection          *Projection
	Window              *Window
	ClearWindow         bool
	Events              []Event
	ConnectionChanged   bool
	ProjectionChanged   bool
	WindowChanged       bool
	ReminderLinkChange  *PlanLinkChange
	ReminderOrderChange *OrderLifecycleChange
	ReminderSchedule    *ScheduleChange
}

func cloneConnection(in Connection) Connection {
	out := in
	if in.CustomerID != nil {
		value := *in.CustomerID
		out.CustomerID = &value
	}
	if in.OrderID != nil {
		value := *in.OrderID
		out.OrderID = &value
	}
	if in.LinkEpochID != nil {
		value := *in.LinkEpochID
		out.LinkEpochID = &value
	}
	if in.LinkedOrderSnapshot != nil {
		snap := *in.LinkedOrderSnapshot
		out.LinkedOrderSnapshot = &snap
	}
	return out
}

func cloneProjection(in *Projection) *Projection {
	if in == nil {
		return nil
	}
	out := *in
	if in.OrderID != nil {
		value := *in.OrderID
		out.OrderID = &value
	}
	if in.SlotID != nil {
		value := *in.SlotID
		out.SlotID = &value
	}
	if in.StartsAt != nil {
		value := *in.StartsAt
		out.StartsAt = &value
	}
	if in.EndsAt != nil {
		value := *in.EndsAt
		out.EndsAt = &value
	}
	if in.Timezone != nil {
		value := *in.Timezone
		out.Timezone = &value
	}
	return &out
}

func cloneWindow(in *Window) *Window {
	if in == nil {
		return nil
	}
	out := *in
	if in.SourceRef != nil {
		value := *in.SourceRef
		out.SourceRef = &value
	}
	return &out
}

func stringPtr(value string) *string { return &value }

func int64Ptr(value int64) *int64 { return &value }
