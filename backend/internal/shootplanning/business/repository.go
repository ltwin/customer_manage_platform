package business

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

type readScope interface {
	Query(context.Context, string, string, string, ...any) (store.Rows, error)
	QueryRow(context.Context, string, string, string, ...any) store.Row
	Count(context.Context, string, string, ...any) (int64, error)
}

type Repository struct{}

func NewRepository() Repository { return Repository{} }

func (Repository) LoadPlanSource(ctx context.Context, scope readScope, planID string) (PlanSource, error) {
	var source PlanSource
	err := scope.QueryRow(ctx, "shoot_plans",
		"id, status, revision, planned_look_count",
		"id = $2", planID,
	).Scan(&source.ID, &source.Status, &source.Revision, &source.PublicInputs.PlannedLookCount)
	if errors.Is(err, store.ErrNoRows) {
		return PlanSource{}, ErrNotFound
	}
	if err != nil {
		return PlanSource{}, fmt.Errorf("load business plan source: %w", err)
	}
	count, err := scope.Count(ctx, "shoot_plan_shots", "plan_id = $2 AND removed_at IS NULL", planID)
	if err != nil {
		return PlanSource{}, fmt.Errorf("count business plan shots: %w", err)
	}
	source.PublicInputs.CurrentShotCount = int(count)
	return source, nil
}

func (Repository) LockPlanSource(ctx context.Context, tx store.TxAccountScope, planID string) (PlanSource, error) {
	var source PlanSource
	err := tx.QueryRowForUpdate(ctx, "shoot_plans",
		"id, status, revision, planned_look_count",
		"id = $2", planID,
	).Scan(&source.ID, &source.Status, &source.Revision, &source.PublicInputs.PlannedLookCount)
	if errors.Is(err, store.ErrNoRows) {
		return PlanSource{}, ErrNotFound
	}
	if err != nil {
		return PlanSource{}, fmt.Errorf("lock business plan source: %w", err)
	}
	count, err := tx.Count(ctx, "shoot_plan_shots", "plan_id = $2 AND removed_at IS NULL", planID)
	if err != nil {
		return PlanSource{}, fmt.Errorf("count locked business plan shots: %w", err)
	}
	source.PublicInputs.CurrentShotCount = int(count)
	return source, nil
}

func (Repository) LoadFacts(ctx context.Context, scope readScope, planID string) (FactsView, error) {
	var view FactsView
	var updatedAt time.Time
	err := scope.QueryRow(ctx, "planning_business_facts",
		"rented_location_count, assistant_count, retouched_photo_count, estimated_duration_minutes, revision, updated_at",
		"plan_id = $2", planID,
	).Scan(
		&view.RentedLocationCount,
		&view.AssistantCount,
		&view.RetouchedPhotoCount,
		&view.EstimatedDurationMinutes,
		&view.Revision,
		&updatedAt,
	)
	if errors.Is(err, store.ErrNoRows) {
		return FactsView{Revision: 0}, nil
	}
	if err != nil {
		return FactsView{}, fmt.Errorf("load planning business facts: %w", err)
	}
	view.UpdatedAt = &updatedAt
	return view, nil
}

func (Repository) LockFacts(ctx context.Context, tx store.TxAccountScope, planID string) (FactsView, error) {
	var view FactsView
	var updatedAt time.Time
	err := tx.QueryRowForUpdate(ctx, "planning_business_facts",
		"rented_location_count, assistant_count, retouched_photo_count, estimated_duration_minutes, revision, updated_at",
		"plan_id = $2", planID,
	).Scan(
		&view.RentedLocationCount,
		&view.AssistantCount,
		&view.RetouchedPhotoCount,
		&view.EstimatedDurationMinutes,
		&view.Revision,
		&updatedAt,
	)
	if errors.Is(err, store.ErrNoRows) {
		return FactsView{Revision: 0}, nil
	}
	if err != nil {
		return FactsView{}, fmt.Errorf("lock planning business facts: %w", err)
	}
	view.UpdatedAt = &updatedAt
	return view, nil
}

func (Repository) ReplaceFacts(
	ctx context.Context,
	tx store.TxAccountScope,
	plan PlanSource,
	expectedPlanRevision, expectedFactsRevision int64,
	facts Facts,
) (FactsView, PlanSource, bool, error) {
	current, err := NewRepository().LockFacts(ctx, tx, plan.ID)
	if err != nil {
		return FactsView{}, PlanSource{}, false, err
	}
	if plan.Revision != expectedPlanRevision {
		return FactsView{}, PlanSource{}, false, ErrPlanRevisionConflict
	}
	if current.Revision != expectedFactsRevision {
		return FactsView{}, PlanSource{}, false, ErrRevisionConflict
	}
	if factsEqual(current.Facts, facts) && current.Revision > 0 {
		return current, plan, false, nil
	}
	now := time.Now().UTC()
	if current.Revision == 0 {
		if err := tx.Insert(ctx, "planning_business_facts", []string{
			"plan_id", "rented_location_count", "assistant_count", "retouched_photo_count",
			"estimated_duration_minutes", "revision", "updated_at",
		}, plan.ID, facts.RentedLocationCount, facts.AssistantCount, facts.RetouchedPhotoCount,
			facts.EstimatedDurationMinutes, int64(1), now); err != nil {
			return FactsView{}, PlanSource{}, false, fmt.Errorf("insert planning business facts: %w", err)
		}
	} else {
		updated, err := tx.Update(ctx, "planning_business_facts",
			"rented_location_count = $2, assistant_count = $3, retouched_photo_count = $4, estimated_duration_minutes = $5, revision = revision + 1, updated_at = $6",
			"plan_id = $7 AND revision = $8",
			facts.RentedLocationCount, facts.AssistantCount, facts.RetouchedPhotoCount,
			facts.EstimatedDurationMinutes, now, plan.ID, expectedFactsRevision,
		)
		if err != nil {
			return FactsView{}, PlanSource{}, false, fmt.Errorf("update planning business facts: %w", err)
		}
		if updated != 1 {
			return FactsView{}, PlanSource{}, false, ErrRevisionConflict
		}
	}
	updated, err := tx.Update(ctx, "shoot_plans",
		"revision = revision + 1, updated_at = $2",
		"id = $3 AND revision = $4", now, plan.ID, expectedPlanRevision,
	)
	if err != nil {
		return FactsView{}, PlanSource{}, false, fmt.Errorf("advance plan for business facts: %w", err)
	}
	if updated != 1 {
		return FactsView{}, PlanSource{}, false, ErrPlanRevisionConflict
	}
	view := FactsView{Facts: facts, Revision: expectedFactsRevision + 1, UpdatedAt: &now}
	plan.Revision++
	return view, plan, true, nil
}

type CurrentTargets struct {
	CRM   CRMSource
	Order *OrderTarget
	Slot  *ScheduleTarget
}

func (Repository) LoadCurrentTargets(ctx context.Context, scope readScope, planID string) (CurrentTargets, error) {
	var current CurrentTargets
	var projectionRevision sql.NullInt64
	err := scope.QueryRow(ctx, "plan_crm_connections",
		"connection_revision, order_id",
		"plan_id = $2", planID,
	).Scan(&current.CRM.ConnectionRevision, &current.CRM.OrderID)
	if errors.Is(err, store.ErrNoRows) {
		return CurrentTargets{}, ErrNotFound
	}
	if err != nil {
		return CurrentTargets{}, fmt.Errorf("load business crm connection: %w", err)
	}
	err = scope.QueryRow(ctx, "plan_schedule_projections",
		"projection_revision, slot_id",
		"plan_id = $2", planID,
	).Scan(&projectionRevision, &current.CRM.SlotID)
	if err != nil && !errors.Is(err, store.ErrNoRows) {
		return CurrentTargets{}, fmt.Errorf("load business crm projection: %w", err)
	}
	if projectionRevision.Valid {
		value := projectionRevision.Int64
		current.CRM.ProjectionRevision = &value
	}
	if current.CRM.OrderID == nil {
		return current, nil
	}
	var order OrderTarget
	err = scope.QueryRow(ctx, "orders",
		"id, customer_id, package_id, status, price",
		"id = $2", *current.CRM.OrderID,
	).Scan(&order.ID, &order.CustomerID, &order.PackageID, &order.Status, &order.Price)
	if errors.Is(err, store.ErrNoRows) {
		return current, nil
	}
	if err != nil {
		return CurrentTargets{}, fmt.Errorf("load business order target: %w", err)
	}
	current.Order = &order
	rows, err := scope.Query(ctx, "schedule_slots",
		"id, type, order_id, start_at, end_at",
		"order_id = $2 AND type = 'shoot'", order.ID,
	)
	if err != nil {
		return CurrentTargets{}, fmt.Errorf("load business schedule target: %w", err)
	}
	defer rows.Close()
	if rows.Next() {
		var slot ScheduleTarget
		if err := rows.Scan(&slot.ID, &slot.Type, &slot.OrderID, &slot.StartsAt, &slot.EndsAt); err != nil {
			return CurrentTargets{}, fmt.Errorf("scan business schedule target: %w", err)
		}
		current.Slot = &slot
	}
	if err := rows.Err(); err != nil {
		return CurrentTargets{}, fmt.Errorf("iterate business schedule target: %w", err)
	}
	return current, nil
}

func (Repository) LoadEffectiveRules(ctx context.Context, scope readScope) (EffectiveRules, error) {
	var raw []byte
	var revision int64
	err := scope.QueryRow(ctx, "settings",
		"planning_business_rule_overrides, planning_business_rule_revision", "TRUE",
	).Scan(&raw, &revision)
	if errors.Is(err, store.ErrNoRows) {
		raw = []byte(`{}`)
		revision = 0
	} else if err != nil {
		return EffectiveRules{}, fmt.Errorf("load planning business rules: %w", err)
	}
	return effectiveRules(raw, revision)
}

func effectiveRules(raw []byte, revision int64) (EffectiveRules, error) {
	var encoded map[string]json.RawMessage
	if err := json.Unmarshal(raw, &encoded); err != nil {
		return EffectiveRules{}, fmt.Errorf("decode planning business rules: %w", err)
	}
	overrides := make(RuleOverrides, len(encoded))
	unknownSet := make(map[string]struct{})
	profile := DefaultRuleProfile()
	for key, value := range encoded {
		var decoded *int
		if string(value) != "null" {
			var integer int
			if err := json.Unmarshal(value, &integer); err != nil {
				return EffectiveRules{}, fmt.Errorf("decode planning business rule %s: %w", key, err)
			}
			decoded = &integer
		}
		overrides[key] = cloneInt(decoded)
		if !applyRuleOverride(&profile, key, decoded) {
			unknownSet[key] = struct{}{}
		}
	}
	if err := validateRuleProfile(profile); err != nil {
		return EffectiveRules{}, err
	}
	for key, value := range map[string]*int{
		"included_look_count":            profile.IncludedLookCount,
		"extra_look_unit_amount":         profile.ExtraLookUnitAmount,
		"rented_location_unit_amount":    profile.RentedLocationUnitAmount,
		"assistant_unit_amount":          profile.AssistantUnitAmount,
		"included_retouched_photo_count": profile.IncludedRetouchedPhotoCount,
		"extra_retouch_unit_amount":      profile.ExtraRetouchUnitAmount,
		"included_shot_count":            profile.IncludedShotCount,
		"extra_shot_unit_amount":         profile.ExtraShotUnitAmount,
	} {
		if value == nil {
			unknownSet[key] = struct{}{}
		}
	}
	unknownKeys := make([]string, 0, len(unknownSet))
	for key := range unknownSet {
		unknownKeys = append(unknownKeys, key)
	}
	sort.Strings(unknownKeys)
	canonical, err := json.Marshal(struct {
		Version   string        `json:"version"`
		Overrides RuleOverrides `json:"overrides"`
	}{Version: RuleVersionV1, Overrides: overrides})
	if err != nil {
		return EffectiveRules{}, err
	}
	digest, _, err := fingerprintFrame(json.RawMessage(canonical))
	if err != nil {
		return EffectiveRules{}, err
	}
	return EffectiveRules{
		Profile: profile, Overrides: overrides, Revision: revision,
		RuleVersion: fmt.Sprintf("%s:%d:%s", RuleVersionV1, revision, digest),
		UnknownKeys: unknownKeys, ProfileHash: digest,
	}, nil
}

func applyRuleOverride(profile *RuleProfile, key string, value *int) bool {
	switch key {
	case "included_look_count":
		profile.IncludedLookCount = cloneInt(value)
	case "extra_look_unit_amount":
		profile.ExtraLookUnitAmount = cloneInt(value)
	case "rented_location_unit_amount":
		profile.RentedLocationUnitAmount = cloneInt(value)
	case "assistant_unit_amount":
		profile.AssistantUnitAmount = cloneInt(value)
	case "included_retouched_photo_count":
		profile.IncludedRetouchedPhotoCount = cloneInt(value)
	case "extra_retouch_unit_amount":
		profile.ExtraRetouchUnitAmount = cloneInt(value)
	case "included_shot_count":
		profile.IncludedShotCount = cloneInt(value)
	case "extra_shot_unit_amount":
		profile.ExtraShotUnitAmount = cloneInt(value)
	default:
		return false
	}
	return true
}

func (Repository) InsertDraft(ctx context.Context, tx store.TxAccountScope, record DraftRecord) error {
	source, err := json.Marshal(record.Source)
	if err != nil {
		return err
	}
	if err := tx.Insert(ctx, "planning_business_drafts", []string{
		"id", "plan_id", "generation_id", "kind", "source_snapshot", "payload",
		"terminal_status", "revision", "created_at", "expires_at",
	}, record.ID, record.PlanID, record.GenerationID, string(record.Kind), source, []byte(record.Payload),
		string(record.TerminalStatus), record.Revision, record.CreatedAt, record.ExpiresAt); err != nil {
		return fmt.Errorf("insert planning business draft: %w", err)
	}
	return nil
}

func (Repository) SupersedeFresh(ctx context.Context, tx store.TxAccountScope, planID string, kind DraftKind, replacementID string) error {
	_, err := tx.Update(ctx, "planning_business_drafts",
		"superseded_by_draft_id = $2, revision = revision + 1",
		"plan_id = $3 AND kind = $4 AND id <> $2 AND terminal_status = 'fresh' AND superseded_by_draft_id IS NULL",
		replacementID, planID, string(kind),
	)
	if err != nil {
		return fmt.Errorf("supersede planning business draft: %w", err)
	}
	return nil
}

func (Repository) LoadDraft(ctx context.Context, scope readScope, planID, draftID string) (DraftRecord, error) {
	row := scope.QueryRow(ctx, "planning_business_drafts",
		"id, plan_id, generation_id, kind, source_snapshot, payload, terminal_status, revision, superseded_by_draft_id, applied_adjustment_id, created_at, expires_at, applied_at, dismissed_at",
		"plan_id = $2 AND id = $3", planID, draftID,
	)
	record, err := scanDraft(row)
	if errors.Is(err, store.ErrNoRows) {
		return DraftRecord{}, ErrNotFound
	}
	return record, err
}

func (Repository) LoadLatestDrafts(ctx context.Context, scope readScope, planID string) (map[DraftKind]DraftRecord, error) {
	rows, err := scope.Query(ctx, "planning_business_drafts",
		"id, plan_id, generation_id, kind, source_snapshot, payload, terminal_status, revision, superseded_by_draft_id, applied_adjustment_id, created_at, expires_at, applied_at, dismissed_at",
		`plan_id = $2 AND id IN (
			SELECT DISTINCT ON (latest.kind) latest.id
			FROM planning_business_drafts AS latest
			WHERE latest.account_id = $1 AND latest.plan_id = $2
			ORDER BY latest.kind, latest.created_at DESC, latest.id DESC
		)`, planID,
	)
	if err != nil {
		return nil, fmt.Errorf("load planning business drafts: %w", err)
	}
	defer rows.Close()
	latest := make(map[DraftKind]DraftRecord, 2)
	for rows.Next() {
		record, err := scanDraft(rows)
		if err != nil {
			return nil, err
		}
		latest[record.Kind] = record
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return latest, nil
}

func (Repository) LockDraft(ctx context.Context, tx store.TxAccountScope, planID, draftID string) (DraftRecord, error) {
	row := tx.QueryRowForUpdate(ctx, "planning_business_drafts",
		"id, plan_id, generation_id, kind, source_snapshot, payload, terminal_status, revision, superseded_by_draft_id, applied_adjustment_id, created_at, expires_at, applied_at, dismissed_at",
		"plan_id = $2 AND id = $3", planID, draftID,
	)
	record, err := scanDraft(row)
	if errors.Is(err, store.ErrNoRows) {
		return DraftRecord{}, ErrNotFound
	}
	return record, err
}

type scanner interface{ Scan(...any) error }

func scanDraft(row scanner) (DraftRecord, error) {
	var record DraftRecord
	var source []byte
	err := row.Scan(
		&record.ID, &record.PlanID, &record.GenerationID, &record.Kind, &source, &record.Payload,
		&record.TerminalStatus, &record.Revision, &record.SupersededByDraftID,
		&record.AppliedAdjustmentID, &record.CreatedAt, &record.ExpiresAt,
		&record.AppliedAt, &record.DismissedAt,
	)
	if err != nil {
		return DraftRecord{}, err
	}
	if err := json.Unmarshal(source, &record.Source); err != nil {
		return DraftRecord{}, fmt.Errorf("decode planning business source snapshot: %w", err)
	}
	return record, nil
}

func (Repository) MarkDismissed(ctx context.Context, tx store.TxAccountScope, record DraftRecord, now time.Time) (int64, error) {
	if record.TerminalStatus == StatusDismissed {
		return record.Revision, nil
	}
	updated, err := tx.Update(ctx, "planning_business_drafts",
		"terminal_status = 'dismissed', dismissed_at = $2, revision = revision + 1",
		"id = $3 AND revision = $4 AND terminal_status = 'fresh'", now, record.ID, record.Revision,
	)
	if err != nil {
		return 0, fmt.Errorf("dismiss planning business draft: %w", err)
	}
	if updated != 1 {
		return 0, ErrRevisionConflict
	}
	return record.Revision + 1, nil
}

func (Repository) MarkApplied(
	ctx context.Context,
	tx store.TxAccountScope,
	record DraftRecord,
	adjustmentID *string,
	now time.Time,
) (int64, error) {
	updated, err := tx.Update(ctx, "planning_business_drafts",
		"terminal_status = 'applied', applied_at = $2, applied_adjustment_id = $3, revision = revision + 1",
		"id = $4 AND revision = $5 AND terminal_status = 'fresh'",
		now, adjustmentID, record.ID, record.Revision,
	)
	if err != nil {
		return 0, fmt.Errorf("apply planning business draft: %w", err)
	}
	if updated != 1 {
		return 0, ErrRevisionConflict
	}
	return record.Revision + 1, nil
}

func factsEqual(left, right Facts) bool {
	return equalInt(left.RentedLocationCount, right.RentedLocationCount) &&
		equalInt(left.AssistantCount, right.AssistantCount) &&
		equalInt(left.RetouchedPhotoCount, right.RetouchedPhotoCount) &&
		equalInt(left.EstimatedDurationMinutes, right.EstimatedDurationMinutes)
}

func equalInt(left, right *int) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}
