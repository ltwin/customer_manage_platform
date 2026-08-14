package crm

import (
	"context"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// CRMReminderLifecycleFactV1 is a sealed union. External packages cannot
// invent variants because the marker method is unexported.
type CRMReminderLifecycleFactV1 interface {
	crmReminderLifecycleFactV1()
}

type ReminderShootSlotFactV1 struct {
	SlotID       string
	OrderID      string
	Timezone     string
	StartsAt     time.Time
	EndsAt       time.Time
	SlotRevision int64
	Type         string
}

type CRMPlanLinkChangedFactV1 struct {
	PlanID             string
	Change             PlanLinkChange
	ConnectionRevision int64
	ProjectionRevision *int64
	LinkEpochID        *string
	CustomerID         *string
	OrderID            *string
	OrderStatus        *string
	CurrentShootSlot   *ReminderShootSlotFactV1
}

type CRMOrderLifecycleChangedFactV1 struct {
	PlanID             string
	Change             OrderLifecycleChange
	SourceOrderID      string
	ConnectionRevision int64
	ProjectionRevision *int64
	LinkEpochID        *string
	CustomerID         *string
	CurrentOrderID     *string
	CurrentOrderStatus *string
	CurrentShootSlot   *ReminderShootSlotFactV1
}

type CRMScheduleChangedFactV1 struct {
	PlanID             string
	Change             ScheduleChange
	SourceSlotID       string
	ConnectionRevision int64
	ProjectionRevision int64
	LinkEpochID        *string
	CustomerID         *string
	OrderID            *string
	OrderStatus        *string
	CurrentShootSlot   *ReminderShootSlotFactV1
}

func (CRMPlanLinkChangedFactV1) crmReminderLifecycleFactV1()       {}
func (CRMOrderLifecycleChangedFactV1) crmReminderLifecycleFactV1() {}
func (CRMScheduleChangedFactV1) crmReminderLifecycleFactV1()       {}

type CRMReminderLifecycleParticipant interface {
	RecomputeForCRMFactInScope(
		ctx context.Context,
		tx store.TxAccountScope,
		fact CRMReminderLifecycleFactV1,
		generation int64,
		now time.Time,
	) error
}

type DisabledCRMReminderLifecycleParticipant struct{}

func (DisabledCRMReminderLifecycleParticipant) RecomputeForCRMFactInScope(
	context.Context, store.TxAccountScope, CRMReminderLifecycleFactV1, int64, time.Time,
) error {
	return nil
}

func isDisabledReminderParticipant(participant CRMReminderLifecycleParticipant) bool {
	switch participant.(type) {
	case DisabledCRMReminderLifecycleParticipant, *DisabledCRMReminderLifecycleParticipant:
		return true
	default:
		return false
	}
}
