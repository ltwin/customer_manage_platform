package reminder

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/planshare"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning/crm"
)

// CRMReminderLifecycleAdapter is the reminder-owned real CRM closed-union participant.
// It projects source/group/member/reminder only; resolution + MarkApplied stay in CRM engine.
type CRMReminderLifecycleAdapter struct {
	Sources    planshare.AssignmentReminderSourceReader
	Projection AssignmentProjectionRepository
}

// NewCRMReminderLifecycleAdapter wires the planshare safe reader + projection repository.
func NewCRMReminderLifecycleAdapter(
	sources planshare.AssignmentReminderSourceReader,
) *CRMReminderLifecycleAdapter {
	if sources == nil {
		sources = planshare.NewAssignmentReminderSourceReader()
	}
	return &CRMReminderLifecycleAdapter{
		Sources:    sources,
		Projection: NewAssignmentProjectionRepository(),
	}
}

var _ crm.CRMReminderLifecycleParticipant = (*CRMReminderLifecycleAdapter)(nil)

// RecomputeForCRMFactInScope applies S1 reducer/projection for one CRM fact. Never writes Inbox.
func (a *CRMReminderLifecycleAdapter) RecomputeForCRMFactInScope(
	ctx context.Context,
	tx store.TxAccountScope,
	fact crm.CRMReminderLifecycleFactV1,
	generation int64,
	now time.Time,
) error {
	if a == nil || a.Sources == nil {
		return errors.New("crm reminder lifecycle adapter incomplete")
	}
	if generation < 1 {
		return errors.New("crm reminder generation must be >= 1")
	}
	planID, sidecar, err := sidecarFromCRMFact(fact)
	if err != nil {
		return err
	}
	timezone, err := loadAccountTimezoneInScope(ctx, tx)
	if err != nil {
		return err
	}
	assignments, err := loadSafeAssignmentsForPlan(ctx, tx, a.Sources, planID)
	if err != nil {
		return err
	}
	before, err := CountInboxInScope(ctx, tx)
	if err != nil {
		return err
	}
	if _, err := ReconcilePlanProjection(ctx, tx, a.Projection, ReduceDesiredReminderGroupsInput{
		AccountID:            tx.AccountID(),
		PlanID:               planID,
		Assignments:          assignments,
		Sidecar:              sidecar,
		Timezone:             timezone,
		Now:                  now.UTC(),
		ActivationGeneration: generation,
	}); err != nil {
		return err
	}
	after, err := CountInboxInScope(ctx, tx)
	if err != nil {
		return err
	}
	if after != before {
		return errors.New("crm lifecycle adapter must not write assignment inbox")
	}
	return nil
}

func sidecarFromCRMFact(fact crm.CRMReminderLifecycleFactV1) (string, PlanOrderSlotSidecar, error) {
	switch f := fact.(type) {
	case crm.CRMPlanLinkChangedFactV1:
		return f.PlanID, sidecarFromOrderSlot(f.OrderID, f.OrderStatus, f.CurrentShootSlot, false), nil
	case crm.CRMOrderLifecycleChangedFactV1:
		return f.PlanID, sidecarFromOrderSlot(f.CurrentOrderID, f.CurrentOrderStatus, f.CurrentShootSlot, false), nil
	case crm.CRMScheduleChangedFactV1:
		return f.PlanID, sidecarFromOrderSlot(f.OrderID, f.OrderStatus, f.CurrentShootSlot, false), nil
	default:
		return "", PlanOrderSlotSidecar{}, fmt.Errorf("unsupported CRM reminder fact %T", fact)
	}
}

func sidecarFromOrderSlot(
	orderID *string,
	orderStatus *string,
	slot *crm.ReminderShootSlotFactV1,
	planArchived bool,
) PlanOrderSlotSidecar {
	sc := PlanOrderSlotSidecar{PlanArchived: planArchived}
	if orderID != nil && *orderID != "" {
		value := *orderID
		sc.OrderID = &value
		sc.OrderActive = orderStatus != nil &&
			*orderStatus != "cancelled" &&
			*orderStatus != "deleted"
	}
	if slot == nil || slot.Type != slotTypeShoot {
		return sc
	}
	id := slot.SlotID
	start := slot.StartsAt.UTC()
	sc.SlotID = &id
	sc.SlotType = slot.Type
	sc.SlotStartAt = &start
	return sc
}

func loadAccountTimezoneInScope(ctx context.Context, tx store.TxAccountScope) (string, error) {
	var tz string
	err := tx.QueryRow(ctx, "settings", "timezone", "TRUE").Scan(&tz)
	if errors.Is(err, store.ErrNoRows) || tz == "" {
		return "Asia/Shanghai", nil
	}
	return tz, err
}

func loadSafeAssignmentsForPlan(
	ctx context.Context,
	tx store.TxAccountScope,
	sources planshare.AssignmentReminderSourceReader,
	planID string,
) ([]SafeAssignmentInput, error) {
	snaps, err := sources.ListAssignmentSourcesForPlanInScope(ctx, tx, planID)
	if err != nil {
		return nil, err
	}
	out := make([]SafeAssignmentInput, 0, len(snaps))
	for _, s := range snaps {
		out = append(out, snapshotToSafe(s, s.ClaimedAt))
	}
	return out, nil
}
