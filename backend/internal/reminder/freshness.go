package reminder

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/planshare"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

var (
	// ErrFreshnessRetryable means the final guarded read must be retried after temporal/ensure work.
	ErrFreshnessRetryable = errors.New("reminder_freshness_retryable")
	// ErrFreshnessNotCurrent means applied_generation/quarantine/guard failed closed.
	ErrFreshnessNotCurrent = errors.New("reminder_freshness_not_current")
)

// AssignmentReminderFreshness advances generation work and proves account/plan currency.
type AssignmentReminderFreshness struct {
	Consumer *AssignmentEventConsumer
}

// EnsureAccountCurrent drains pending work until applied_generation >= target.
func (f AssignmentReminderFreshness) EnsureAccountCurrent(ctx context.Context, scope store.AccountScope, targetGeneration int64) error {
	if f.Consumer == nil {
		return errors.New("assignment reminder freshness consumer required")
	}
	if targetGeneration < 0 {
		return errors.New("target generation must be non-negative")
	}
	for range 32 {
		var applied int64
		var unresolved bool
		err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
			if err := tx.QueryRow(ctx, "planning_reminder_account_generations",
				"applied_generation", "TRUE").Scan(&applied); err != nil {
				return err
			}
			var err error
			unresolved, err = HasUnresolvedQuarantineInScope(ctx, tx)
			return err
		})
		if err != nil {
			return err
		}
		if unresolved {
			return fmt.Errorf("%w: unresolved quarantine", ErrFreshnessNotCurrent)
		}
		if applied >= targetGeneration {
			return nil
		}
		n, err := f.Consumer.ProcessAccountOnce(ctx, scope, 8)
		if err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("%w: applied_generation=%d target=%d", ErrFreshnessNotCurrent, applied, targetGeneration)
		}
	}
	return fmt.Errorf("%w: ensure account current exceeded bound", ErrFreshnessRetryable)
}

// EnsurePlanCurrent currently shares account-wide ensure.
func (f AssignmentReminderFreshness) EnsurePlanCurrent(ctx context.Context, scope store.AccountScope, _ string, targetGeneration int64) error {
	return f.EnsureAccountCurrent(ctx, scope, targetGeneration)
}

// NewAssignmentReminderFreshness wires the production consumer for Bearer ensure.
func NewAssignmentReminderFreshness() *AssignmentReminderFreshness {
	consumer := &AssignmentEventConsumer{
		SourceReader: planshare.NewAssignmentReminderSourceReader(),
		Projection:   NewAssignmentProjectionRepository(),
		Facts:        DefaultPlanFactReader{},
	}
	return &AssignmentReminderFreshness{Consumer: consumer}
}

// FreshListResult is the Bearer list payload after final clock_timestamp guard.
type FreshListResult struct {
	Items []Reminder
	Total int64
}

type lockedPlanningGroup struct {
	GroupID    string
	PlanID     string
	SlotID     string
	ReminderID string
	ValidUntil time.Time
}

// ListWithFreshnessFinalRead performs capture → ensure → final fence read with clock_timestamp guard.
func (s *Service) ListWithFreshnessFinalRead(
	ctx context.Context,
	scope store.AccountScope,
	filter ListFilter,
	freshness AssignmentReminderFreshness,
) (FreshListResult, error) {
	normalized, err := normalizeListFilter(filter)
	if err != nil {
		return FreshListResult{}, err
	}
	var target int64
	err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if _, err := tx.PlanningReminderFence().LockCurrentAccount(ctx); err != nil {
			return err
		}
		var cerr error
		target, cerr = tx.CaptureFenceTargetGeneration(ctx)
		return cerr
	})
	if err != nil {
		return FreshListResult{}, err
	}
	if err := freshness.EnsureAccountCurrent(ctx, scope, target); err != nil {
		return FreshListResult{}, err
	}

	var out FreshListResult
	err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if _, err := tx.PlanningReminderFence().LockCurrentAccount(ctx); err != nil {
			return err
		}
		lockedTarget, err := tx.CaptureFenceTargetGeneration(ctx)
		if err != nil {
			return err
		}
		if lockedTarget != target {
			return fmt.Errorf("%w: target moved", ErrFreshnessRetryable)
		}
		var applied int64
		if err := tx.QueryRow(ctx, "planning_reminder_account_generations",
			"applied_generation", "TRUE").Scan(&applied); err != nil {
			return err
		}
		unresolved, err := HasUnresolvedQuarantineInScope(ctx, tx)
		if err != nil {
			return err
		}
		if unresolved || applied < target {
			return fmt.Errorf("%w: watermark/quarantine", ErrFreshnessNotCurrent)
		}

		idRows, err := tx.Query(ctx, "plan_assignment_reminder_groups",
			"group_id", "state = $2", GroupStateCurrent)
		if err != nil {
			return err
		}
		groupIDs := make([]string, 0)
		for idRows.Next() {
			var id string
			if err := idRows.Scan(&id); err != nil {
				idRows.Close()
				return err
			}
			groupIDs = append(groupIDs, id)
		}
		if err := idRows.Err(); err != nil {
			idRows.Close()
			return err
		}
		idRows.Close()
		sort.Strings(groupIDs)

		groups := make([]lockedPlanningGroup, 0, len(groupIDs))
		for _, id := range groupIDs {
			var g lockedPlanningGroup
			if err := tx.QueryRowForUpdate(ctx, "plan_assignment_reminder_groups",
				"group_id, plan_id, slot_id, reminder_id, valid_until",
				"group_id = $2 AND state = $3", id, GroupStateCurrent,
			).Scan(&g.GroupID, &g.PlanID, &g.SlotID, &g.ReminderID, &g.ValidUntil); err != nil {
				if errors.Is(err, store.ErrNoRows) {
					continue
				}
				return err
			}
			g.ValidUntil = g.ValidUntil.UTC()
			groups = append(groups, g)
			var remID string
			if err := tx.QueryRowForUpdate(ctx, "reminders", "id",
				"id = $2 AND type = $3", g.ReminderID, TypePlanAssignmentChecklist,
			).Scan(&remID); err != nil && !errors.Is(err, store.ErrNoRows) {
				return err
			}
		}

		tentative, err := PostgresRepository{}.List(ctx, tx.BoundAccountScope(), normalized)
		if err != nil {
			return err
		}
		ok, err := assertFinalFreshnessGuard(ctx, tx, target, groups)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("%w: final guard", ErrFreshnessRetryable)
		}
		out = FreshListResult(tentative)
		return nil
	})
	if err != nil {
		return FreshListResult{}, err
	}
	return out, nil
}

func assertFinalFreshnessGuard(
	ctx context.Context,
	tx store.TxAccountScope,
	target int64,
	groups []lockedPlanningGroup,
) (bool, error) {
	var applied int64
	if err := tx.QueryRow(ctx, "planning_reminder_account_generations",
		"applied_generation", "TRUE").Scan(&applied); err != nil {
		return false, err
	}
	unresolved, err := HasUnresolvedQuarantineInScope(ctx, tx)
	if err != nil {
		return false, err
	}
	if unresolved || applied < target {
		return false, nil
	}
	if _, err := tx.QueryClockTimestamp(ctx); err != nil {
		return false, err
	}
	for _, g := range groups {
		exists, err := tx.Exists(ctx, "plan_assignment_reminder_groups",
			"group_id = $2 AND state = $3 AND valid_until > clock_timestamp()",
			g.GroupID, GroupStateCurrent)
		if err != nil {
			return false, err
		}
		if !exists {
			locked, lockErr := tx.PlanningReminderFence().LockCurrentAccount(ctx)
			if lockErr == nil {
				_, _ = EnsureTemporalInvalidationInScope(ctx, tx, locked, g.PlanID, g.SlotID, g.ValidUntil)
			}
			return false, nil
		}
	}
	return true, nil
}

// PlanAssignmentReminderGroupView is one current group for Bearer plan read (D9).
type PlanAssignmentReminderGroupView struct {
	GroupID        string
	ReminderID     string
	ReminderStatus string
	DueDate        time.Time
	ItemCount      int
	SlotID         string
	Content        string
	RecipientKind  string
	DeliveryModes  []string
}

// PlanAssignmentReminderView is GET /shoot-plans/{id}/assignment-reminders payload.
type PlanAssignmentReminderView struct {
	PlanID                 string
	UnscheduledSourceCount int
	Groups                 []PlanAssignmentReminderGroupView
}

var (
	planAssignmentRecipientKind = "account_owner"
	planAssignmentDeliveryModes = []string{"in_app", "telegram_digest_if_bound"}
)

// GetPlanAssignmentReminders runs capture → ensure → final guarded plan read.
func (s *Service) GetPlanAssignmentReminders(
	ctx context.Context,
	scope store.AccountScope,
	planID string,
) (PlanAssignmentReminderView, error) {
	planID = strings.TrimSpace(planID)
	if planID == "" {
		return PlanAssignmentReminderView{}, fmt.Errorf("%w: plan not found", ErrNotFound)
	}
	if s.freshness == nil {
		return PlanAssignmentReminderView{}, errors.New("assignment reminder freshness required")
	}
	var last error
	for range 4 {
		view, err := s.getPlanAssignmentRemindersOnce(ctx, scope, planID, *s.freshness)
		if err == nil {
			return view, nil
		}
		last = err
		if !errors.Is(err, ErrFreshnessRetryable) {
			return PlanAssignmentReminderView{}, err
		}
	}
	return PlanAssignmentReminderView{}, last
}

func (s *Service) getPlanAssignmentRemindersOnce(
	ctx context.Context,
	scope store.AccountScope,
	planID string,
	freshness AssignmentReminderFreshness,
) (PlanAssignmentReminderView, error) {
	exists, err := scope.Exists(ctx, "shoot_plans", "id = $2", planID)
	if err != nil {
		return PlanAssignmentReminderView{}, err
	}
	if !exists {
		return PlanAssignmentReminderView{}, fmt.Errorf("%w: plan not found", ErrNotFound)
	}

	var target int64
	err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if _, err := tx.PlanningReminderFence().LockCurrentAccount(ctx); err != nil {
			return err
		}
		var cerr error
		target, cerr = tx.CaptureFenceTargetGeneration(ctx)
		return cerr
	})
	if err != nil {
		return PlanAssignmentReminderView{}, err
	}
	if err := freshness.EnsurePlanCurrent(ctx, scope, planID, target); err != nil {
		return PlanAssignmentReminderView{}, err
	}

	var out PlanAssignmentReminderView
	err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if _, err := tx.PlanningReminderFence().LockCurrentAccount(ctx); err != nil {
			return err
		}
		lockedTarget, err := tx.CaptureFenceTargetGeneration(ctx)
		if err != nil {
			return err
		}
		if lockedTarget != target {
			return fmt.Errorf("%w: target moved", ErrFreshnessRetryable)
		}
		var applied int64
		if err := tx.QueryRow(ctx, "planning_reminder_account_generations",
			"applied_generation", "TRUE").Scan(&applied); err != nil {
			return err
		}
		unresolved, err := HasUnresolvedQuarantineInScope(ctx, tx)
		if err != nil {
			return err
		}
		if unresolved || applied < target {
			return fmt.Errorf("%w: watermark/quarantine", ErrFreshnessNotCurrent)
		}

		idRows, err := tx.Query(ctx, "plan_assignment_reminder_groups",
			"group_id", "plan_id = $2 AND state = $3", planID, GroupStateCurrent)
		if err != nil {
			return err
		}
		groupIDs := make([]string, 0)
		for idRows.Next() {
			var id string
			if err := idRows.Scan(&id); err != nil {
				idRows.Close()
				return err
			}
			groupIDs = append(groupIDs, id)
		}
		if err := idRows.Err(); err != nil {
			idRows.Close()
			return err
		}
		idRows.Close()
		sort.Strings(groupIDs)

		groups := make([]lockedPlanningGroup, 0, len(groupIDs))
		views := make([]PlanAssignmentReminderGroupView, 0, len(groupIDs))
		for _, id := range groupIDs {
			var (
				g         lockedPlanningGroup
				dueDate   time.Time
				remStatus string
				content   string
				itemCount int
			)
			if err := tx.QueryRowForUpdate(ctx, "plan_assignment_reminder_groups",
				"group_id, plan_id, slot_id, reminder_id, valid_until, due_date",
				"group_id = $2 AND state = $3", id, GroupStateCurrent,
			).Scan(&g.GroupID, &g.PlanID, &g.SlotID, &g.ReminderID, &g.ValidUntil, &dueDate); err != nil {
				if errors.Is(err, store.ErrNoRows) {
					continue
				}
				return err
			}
			g.ValidUntil = g.ValidUntil.UTC()
			groups = append(groups, g)

			if err := tx.QueryRowForUpdate(ctx, "reminders", "id, status, content",
				"id = $2 AND type = $3", g.ReminderID, TypePlanAssignmentChecklist,
			).Scan(&g.ReminderID, &remStatus, &content); err != nil {
				if errors.Is(err, store.ErrNoRows) {
					return fmt.Errorf("%w: planning reminder missing for current group", ErrFreshnessNotCurrent)
				}
				return err
			}
			count, err := tx.Count(ctx, "plan_assignment_reminder_members", "group_id = $2", g.GroupID)
			if err != nil {
				return err
			}
			itemCount = int(count)
			views = append(views, PlanAssignmentReminderGroupView{
				GroupID:        g.GroupID,
				ReminderID:     g.ReminderID,
				ReminderStatus: remStatus,
				DueDate:        dateOnly(dueDate),
				ItemCount:      itemCount,
				SlotID:         g.SlotID,
				Content:        content,
				RecipientKind:  planAssignmentRecipientKind,
				DeliveryModes:  append([]string(nil), planAssignmentDeliveryModes...),
			})
		}

		unscheduled, err := tx.Count(ctx, "plan_assignment_reminder_sources",
			"plan_id = $2 AND projection_state = $3 AND source_state = $4",
			planID, ProjectionStateUnscheduled, SourceStateActive)
		if err != nil {
			return err
		}

		ok, err := assertFinalFreshnessGuard(ctx, tx, target, groups)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("%w: final guard", ErrFreshnessRetryable)
		}
		out = PlanAssignmentReminderView{
			PlanID:                 planID,
			UnscheduledSourceCount: int(unscheduled),
			Groups:                 views,
		}
		return nil
	})
	if err != nil {
		return PlanAssignmentReminderView{}, err
	}
	if out.Groups == nil {
		out.Groups = []PlanAssignmentReminderGroupView{}
	}
	return out, nil
}
