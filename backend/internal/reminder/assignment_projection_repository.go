package reminder

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// AssignmentProjectionRepository 负责 assignment reminder source/group/member 落盘。
// S1 不含 fence consumer / quarantine / epoch。
type AssignmentProjectionRepository struct{}

func NewAssignmentProjectionRepository() AssignmentProjectionRepository {
	return AssignmentProjectionRepository{}
}

// ApplyDesiredInScope 对单一 plan 应用 reducer 输出（truth-table write plan）。
func (AssignmentProjectionRepository) ApplyDesiredInScope(
	ctx context.Context,
	tx store.TxAccountScope,
	planID string,
	desired ReduceDesiredReminderGroupsOutput,
	writePlan ProjectionWritePlan,
	now time.Time,
) error {
	if err := upsertSources(ctx, tx, planID, desired.Sources, now); err != nil {
		return err
	}
	for _, w := range writePlan.Withdrawals {
		if err := withdrawGroup(ctx, tx, w, now); err != nil {
			return err
		}
	}
	for _, u := range writePlan.TemporalUpdates {
		if err := updateTemporal(ctx, tx, u); err != nil {
			return err
		}
	}
	for _, g := range writePlan.Creates {
		if err := createOccurrence(ctx, tx, planID, g, now); err != nil {
			return err
		}
	}
	return nil
}

// ListCurrentGroupsInScope 列出 plan 的 current groups（含 reminder status）。
func (AssignmentProjectionRepository) ListCurrentGroupsInScope(
	ctx context.Context,
	tx store.TxAccountScope,
	planID string,
) ([]CurrentReminderGroup, error) {
	groupRows, err := tx.Query(ctx, "plan_assignment_reminder_groups",
		"group_id, plan_id, order_id, slot_id, due_date, timezone_snapshot, valid_until, lead_rule_version, group_fingerprint, validity_revision, state, reminder_id, activation_generation",
		"plan_id = $2 AND state = $3", planID, GroupStateCurrent)
	if err != nil {
		return nil, err
	}

	out := make([]CurrentReminderGroup, 0)
	for groupRows.Next() {
		var g CurrentReminderGroup
		var due time.Time
		var validUntil time.Time
		if err := groupRows.Scan(
			&g.GroupID, &g.PlanID, &g.OrderID, &g.SlotID, &due, &g.TimezoneSnapshot, &validUntil,
			&g.LeadRuleVersion, &g.GroupFingerprint, &g.ValidityRevision, &g.State, &g.ReminderID,
			&g.ActivationGeneration,
		); err != nil {
			groupRows.Close()
			return nil, err
		}
		g.DueDate = dateOnly(due)
		g.ValidUntil = validUntil.UTC()
		out = append(out, g)
	}
	if err := groupRows.Err(); err != nil {
		groupRows.Close()
		return nil, err
	}
	groupRows.Close()

	for i := range out {
		rem, err := PostgresRepository{}.Find(ctx, tx.BoundAccountScope(), out[i].ReminderID)
		if err != nil {
			return nil, err
		}
		out[i].ReminderStatus = rem.Status
		out[i].ReminderCreatedAt = rem.CreatedAt
	}
	return out, nil
}

func upsertSources(ctx context.Context, tx store.TxAccountScope, planID string, sources []DesiredSourceState, now time.Time) error {
	cols := []string{
		"plan_id", "assignment_id", "assignment_revision", "assignment_kind",
		"readiness_item_id", "content_snapshot", "claimed_by_display_name_snapshot", "content_fingerprint",
		"preparation_lead_days_snapshot", "lead_rule_version", "source_state", "projection_state",
		"source_occurred_at", "updated_at",
	}
	conflict := []string{"account_id", "plan_id", "assignment_id"}
	updates := []string{
		"assignment_revision", "assignment_kind", "readiness_item_id", "content_snapshot",
		"claimed_by_display_name_snapshot", "content_fingerprint", "preparation_lead_days_snapshot",
		"lead_rule_version", "source_state", "projection_state", "source_occurred_at", "updated_at",
	}
	for _, s := range sources {
		if err := tx.Upsert(ctx, "plan_assignment_reminder_sources", cols, conflict, updates,
			planID, s.AssignmentID, s.AssignmentRevision, s.AssignmentKind,
			nullableString(s.ReadinessItemID), s.ContentSnapshot, nullableString(s.ClaimedByDisplayNameSnapshot), s.ContentFingerprint,
			nullableInt(s.PreparationLeadDaysSnapshot), nullableString(s.LeadRuleVersion), s.SourceState, s.ProjectionState,
			s.SourceOccurredAt.UTC(), now.UTC(),
		); err != nil {
			return fmt.Errorf("upsert assignment reminder source: %w", err)
		}
	}
	return nil
}

func withdrawGroup(ctx context.Context, tx store.TxAccountScope, w GroupWithdrawal, now time.Time) error {
	n, err := tx.Update(ctx, "plan_assignment_reminder_groups",
		"state = $2, withdrawn_reason = $3, withdrawn_at = $4",
		"group_id = $5 AND state = $6",
		GroupStateWithdrawn, w.Reason, now.UTC(), w.GroupID, GroupStateCurrent)
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("withdraw group %s: expected 1 row, got %d", w.GroupID, n)
	}
	if w.Dismiss {
		if _, err := tx.Update(ctx, "reminders",
			"status = $2",
			"id = $3 AND status = $4",
			StatusDismissed, w.ReminderID, StatusPending); err != nil {
			return err
		}
	}
	return nil
}

func updateTemporal(ctx context.Context, tx store.TxAccountScope, u TemporalGroupUpdate) error {
	n, err := tx.Update(ctx, "plan_assignment_reminder_groups",
		"valid_until = $2, timezone_snapshot = $3, validity_revision = $4",
		"group_id = $5 AND state = $6 AND validity_revision = $7",
		u.ValidUntil.UTC(), u.TimezoneSnapshot, u.NextValidityRev,
		u.GroupID, GroupStateCurrent, u.NextValidityRev-1)
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("temporal update group %s: expected 1 row, got %d", u.GroupID, n)
	}
	return nil
}

func createOccurrence(ctx context.Context, tx store.TxAccountScope, planID string, g DesiredReminderGroup, now time.Time) error {
	groupID := "parg_" + uuid.NewString()
	reminderID := "rem_" + uuid.NewString()
	dedup := PlanAssignmentChecklistDedupKey(groupID)
	content := buildChecklistContent(g.Members)

	if _, err := tx.InsertReturningID(ctx, "reminders",
		[]string{"id", "type", "customer_id", "order_id", "plan_id", "due_date", "content", "status", "dedup_key"},
		reminderID,
		TypePlanAssignmentChecklist,
		nil,
		g.OrderID,
		planID,
		FormatDate(g.DueDate),
		content,
		StatusPending,
		dedup,
	); err != nil {
		return fmt.Errorf("insert plan assignment checklist reminder: %w", err)
	}

	if err := tx.Insert(ctx, "plan_assignment_reminder_groups",
		[]string{
			"group_id", "plan_id", "order_id", "slot_id", "due_date", "timezone_snapshot",
			"valid_until", "lead_rule_version", "activation_generation", "validity_revision",
			"group_fingerprint", "state", "reminder_id", "created_at",
		},
		groupID, planID, g.OrderID, g.SlotID, FormatDate(g.DueDate), g.TimezoneSnapshot,
		g.ValidUntil.UTC(), g.LeadRuleVersion, g.ActivationGeneration, int64(1),
		g.GroupFingerprint, GroupStateCurrent, reminderID, now.UTC(),
	); err != nil {
		return fmt.Errorf("insert reminder group: %w", err)
	}

	for _, m := range g.Members {
		if err := tx.Insert(ctx, "plan_assignment_reminder_members",
			[]string{
				"group_id", "plan_id", "assignment_id", "assignment_revision",
				"content_fingerprint", "position", "activation_generation",
			},
			groupID, planID, m.AssignmentID, m.AssignmentRevision,
			m.ContentFingerprint, m.Position, g.ActivationGeneration,
		); err != nil {
			return fmt.Errorf("insert reminder member: %w", err)
		}
	}
	return nil
}

func buildChecklistContent(members []DesiredMember) string {
	n := len(members)
	var b strings.Builder
	fmt.Fprintf(&b, "认领项核对（%d项）", n)
	if n == 0 {
		return b.String()
	}
	b.WriteString("：")
	limit := 3
	if n < limit {
		limit = n
	}
	parts := make([]string, 0, limit)
	for i := 0; i < limit; i++ {
		parts = append(parts, truncateRunes(members[i].ContentSnapshot, 40))
	}
	b.WriteString(strings.Join(parts, "；"))
	if n > 3 {
		fmt.Fprintf(&b, "等%d项", n)
	}
	return b.String()
}

func truncateRunes(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max])
}

func nullableInt(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}

// ReconcilePlanProjection 组合 reduce + diff + apply（S1 测试与后续 consumer 入口）。
func ReconcilePlanProjection(
	ctx context.Context,
	tx store.TxAccountScope,
	repo AssignmentProjectionRepository,
	in ReduceDesiredReminderGroupsInput,
) (ProjectionWritePlan, error) {
	desired, err := ReduceDesiredReminderGroups(in)
	if err != nil {
		return ProjectionWritePlan{}, err
	}
	current, err := repo.ListCurrentGroupsInScope(ctx, tx, in.PlanID)
	if err != nil {
		return ProjectionWritePlan{}, err
	}
	reason := InferWithdrawReason(in.Sidecar, desired.Sources, in.Now)
	plan := DiffDesiredAgainstCurrent(current, desired.Groups, func(CurrentReminderGroup) string {
		return reason
	})
	plan.SourceUpserts = desired.Sources
	if err := repo.ApplyDesiredInScope(ctx, tx, in.PlanID, desired, plan, in.Now); err != nil {
		return ProjectionWritePlan{}, err
	}
	return plan, nil
}
