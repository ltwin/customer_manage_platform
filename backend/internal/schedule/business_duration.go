package schedule

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning/business"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning/crm"
)

var ErrBusinessDurationScope = errors.New("schedule_business_duration_scope_invariant")

type BusinessDurationProjectionSink interface {
	LockPlanningReminderFenceInScope(context.Context, store.TxAccountScope) error
	ReprojectScheduleMutationInScope(context.Context, store.TxAccountScope, crm.ScheduleMutationFact, time.Time) error
}

type BusinessDurationParticipant struct {
	projection BusinessDurationProjectionSink
	now        func() time.Time
}

func NewBusinessDurationParticipant(projection BusinessDurationProjectionSink) BusinessDurationParticipant {
	return BusinessDurationParticipant{projection: projection, now: time.Now}
}

type lockedBusinessDurationScope struct {
	tx         store.TxAccountScope
	projection BusinessDurationProjectionSink
	target     business.ScheduleTarget
	planID     string
	active     bool
	used       bool
	result     business.AppliedSchedule
	now        func() time.Time
}

func (p BusinessDurationParticipant) WithLockedTargetInScope(
	ctx context.Context,
	tx store.TxAccountScope,
	planID, slotID string,
	callback func(business.LockedScheduleScope) error,
) (business.AppliedSchedule, error) {
	if p.projection == nil || callback == nil || planID == "" || slotID == "" {
		return business.AppliedSchedule{}, ErrBusinessDurationScope
	}
	if err := p.projection.LockPlanningReminderFenceInScope(ctx, tx); err != nil {
		return business.AppliedSchedule{}, err
	}
	var orderID string
	if err := tx.QueryRow(ctx, "schedule_slots", "order_id", "id = $2", slotID).Scan(&orderID); err != nil {
		if errors.Is(err, store.ErrNoRows) {
			return business.AppliedSchedule{}, business.ErrNotFound
		}
		return business.AppliedSchedule{}, err
	}
	var customerID string
	if err := tx.QueryRow(ctx, "orders", "customer_id", "id = $2", orderID).Scan(&customerID); err != nil {
		return business.AppliedSchedule{}, business.ErrNotFound
	}
	if err := lockBusinessDurationRow(ctx, tx, "customers", customerID); err != nil {
		return business.AppliedSchedule{}, err
	}
	if err := lockBusinessDurationRow(ctx, tx, "orders", orderID); err != nil {
		return business.AppliedSchedule{}, err
	}
	var target business.ScheduleTarget
	if err := tx.QueryRowForUpdate(ctx, "schedule_slots",
		"id, type, order_id, start_at, end_at", "id = $2", slotID,
	).Scan(&target.ID, &target.Type, &target.OrderID, &target.StartsAt, &target.EndsAt); err != nil {
		return business.AppliedSchedule{}, business.ErrNotFound
	}
	if err := lockBusinessDurationRow(ctx, tx, "shoot_plans", planID); err != nil {
		return business.AppliedSchedule{}, err
	}
	locked := &lockedBusinessDurationScope{
		tx: tx, projection: p.projection, target: target, planID: planID,
		active: true, now: p.now,
	}
	callbackErr := callback(locked)
	locked.active = false
	if callbackErr != nil {
		return business.AppliedSchedule{}, callbackErr
	}
	if !locked.used {
		return business.AppliedSchedule{}, ErrBusinessDurationScope
	}
	return locked.result, nil
}

func (s *lockedBusinessDurationScope) Target() business.ScheduleTarget {
	return s.target
}

func (s *lockedBusinessDurationScope) ApplyEnd(
	ctx context.Context,
	command business.ApplyScheduleCommand,
) (business.AppliedSchedule, error) {
	if !s.active || s.used || command.PlanID != s.planID || command.DraftID == "" ||
		!command.ProposedEndAt.After(s.target.StartsAt) {
		return business.AppliedSchedule{}, ErrBusinessDurationScope
	}
	s.used = true
	updated, err := s.tx.Update(ctx, "schedule_slots",
		"end_at = $2", "id = $3", command.ProposedEndAt.UTC(), s.target.ID,
	)
	if err != nil {
		return business.AppliedSchedule{}, fmt.Errorf("apply schedule business duration: %w", err)
	}
	if updated != 1 {
		return business.AppliedSchedule{}, business.ErrNotFound
	}
	oldOrderID := s.target.OrderID
	if err := s.projection.ReprojectScheduleMutationInScope(ctx, s.tx, crm.ScheduleMutationFact{
		Change: crm.ScheduleUpdate, SlotID: s.target.ID,
		OldOrderID: &oldOrderID, NewOrderID: &oldOrderID,
	}, s.clock()); err != nil {
		return business.AppliedSchedule{}, err
	}
	after := s.target
	after.EndsAt = command.ProposedEndAt.UTC()
	s.result = business.AppliedSchedule{
		Target: after, BeforeEndAt: s.target.EndsAt, AfterEndAt: after.EndsAt,
	}
	return s.result, nil
}

func lockBusinessDurationRow(ctx context.Context, tx store.TxAccountScope, table, id string) error {
	var lockedID string
	if err := tx.QueryRowForUpdate(ctx, table, "id", "id = $2", id).Scan(&lockedID); err != nil {
		if errors.Is(err, store.ErrNoRows) {
			return business.ErrNotFound
		}
		return err
	}
	return nil
}

func (s *lockedBusinessDurationScope) clock() time.Time {
	if s.now == nil {
		return time.Now().UTC()
	}
	return s.now().UTC()
}

var _ business.ScheduleDurationParticipant = BusinessDurationParticipant{}
