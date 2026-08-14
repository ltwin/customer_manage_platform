package reminder

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// MarkDone routes planning reminders through fence CAS (0 generation); legacy stays on SetStatus.
func (s *Service) MarkDone(ctx context.Context, scope store.AccountScope, id string) (Reminder, error) {
	return s.setStatusRouted(ctx, scope, id, StatusDone)
}

// Dismiss routes planning reminders through fence CAS (0 generation); legacy stays on SetStatus.
func (s *Service) Dismiss(ctx context.Context, scope store.AccountScope, id string) (Reminder, error) {
	return s.setStatusRouted(ctx, scope, id, StatusDismissed)
}

func (s *Service) setStatusRouted(ctx context.Context, scope store.AccountScope, id, status string) (Reminder, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Reminder{}, ValidationError{Message: "id 必填"}
	}
	current, err := s.repo.Find(ctx, scope, id)
	if err != nil {
		return Reminder{}, err
	}
	if current.Type != TypePlanAssignmentChecklist {
		return s.repo.SetStatus(ctx, scope, id, status)
	}
	return setPlanningReminderStatusInFence(ctx, scope, id, status)
}

func setPlanningReminderStatusInFence(ctx context.Context, scope store.AccountScope, id, status string) (Reminder, error) {
	var updated Reminder
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if _, err := tx.PlanningReminderFence().LockCurrentAccount(ctx); err != nil {
			return err
		}
		var (
			reminderID string
			curStatus  string
			remType    string
		)
		err := tx.QueryRowForUpdate(ctx, "reminders", "id, status, type",
			"id = $2", id).Scan(&reminderID, &curStatus, &remType)
		if errors.Is(err, store.ErrNoRows) {
			return fmtNotFound()
		}
		if err != nil {
			return err
		}
		if remType != TypePlanAssignmentChecklist {
			return errors.New("planning status path requires plan_assignment_checklist")
		}
		if curStatus == status {
			updated, err = PostgresRepository{}.Find(ctx, tx.BoundAccountScope(), id)
			return err
		}
		if curStatus != StatusPending {
			return ValidationError{Message: "仅 pending 提醒可变更状态"}
		}
		var groupID string
		err = tx.QueryRowForUpdate(ctx, "plan_assignment_reminder_groups",
			"group_id", "reminder_id = $2", id).Scan(&groupID)
		if err != nil && !errors.Is(err, store.ErrNoRows) {
			return err
		}
		n, err := tx.Update(ctx, "reminders", "status = $2",
			"id = $3 AND status = $4 AND type = $5",
			status, id, StatusPending, TypePlanAssignmentChecklist)
		if err != nil {
			return err
		}
		if n != 1 {
			return ValidationError{Message: "仅 pending 提醒可变更状态"}
		}
		// 0 generation: do not ReserveGeneration / rewrite assignment / readiness / order.
		updated, err = PostgresRepository{}.Find(ctx, tx.BoundAccountScope(), id)
		return err
	})
	return updated, err
}

func fmtNotFound() error {
	return fmt.Errorf("%w: 提醒不存在", ErrNotFound)
}
