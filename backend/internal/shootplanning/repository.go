package shootplanning

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning/crm"
)

var (
	ErrPlanNotFound         = errors.New("shoot plan not found")
	ErrPlanRevisionConflict = errors.New("plan_revision_conflict")
)

type CreatePlanInput struct {
	Title   string
	Subject string
}

type ListPlansFilter struct {
	Status       *PlanStatus
	CustomerID   string
	OrderID      string
	ArchivedOnly bool
	Page         int
	PageSize     int
}

type PlanListItem struct {
	ID                    string          `json:"id"`
	Title                 string          `json:"title"`
	Subject               string          `json:"subject"`
	Status                PlanStatus      `json:"status"`
	PublicScale           PublicPlanScale `json:"public_scale"`
	Revision              int64           `json:"revision"`
	ExecutionFactRevision int64           `json:"execution_fact_revision"`
	CreatedAt             time.Time       `json:"created_at"`
	UpdatedAt             time.Time       `json:"updated_at"`
	CRM                   PlanListCrm     `json:"crm_summary"`
	ExecutionWindow       *PlanListWindow `json:"execution_window_summary"`
	ExecutionStats        PlanListStats   `json:"execution_stats"`
	ReadinessSummary      PlanListReady   `json:"readiness_summary"`
}

// PlanListCrm：客户名取当前档案；订单标题与状态取链接时快照（订单删除后仍可显示）。
type PlanListCrm struct {
	CustomerID        *string `json:"customer_id"`
	CustomerName      *string `json:"customer_name"`
	OrderID           *string `json:"order_id"`
	OrderTitle        *string `json:"order_title"`
	OrderStatusAtLink *string `json:"order_status_at_link"`
}

type PlanListWindow struct {
	Source   string    `json:"source"`
	StartsAt time.Time `json:"starts_at"`
	EndsAt   time.Time `json:"ends_at"`
	Timezone string    `json:"timezone"`
}

type PlanListStats struct {
	Captured int64 `json:"captured_count"`
	Skipped  int64 `json:"skipped_count"`
}

type PlanListReady struct {
	RequiredTotal     int64 `json:"required_total"`
	RequiredUnchecked int64 `json:"required_unchecked"`
}

func nullStringPtr(value sql.NullString) *string {
	if !value.Valid || value.String == "" {
		return nil
	}
	stringValue := value.String
	return &stringValue
}

type ListPlansResult struct {
	Items []PlanListItem `json:"items"`
	Total int64          `json:"total"`
}

type PlanDetail struct {
	ShootPlan
	Shots                          []Shot                     `json:"shots"`
	ReadinessItems                 []ReadinessItem            `json:"readiness_items"`
	RequiredArchiveAcknowledgement ArchiveAcknowledgement     `json:"required_archive_acknowledgement"`
	ExecutionFacts                 []ExecutionFact            `json:"execution_history,omitempty"`
	Finalizations                  []PlanFinalizationSnapshot `json:"finalizations,omitempty"`
	CRM                            *PlanCRMView               `json:"crm,omitempty"`
}

type PlanCRMView struct {
	State               string                   `json:"state"`
	ConnectionRevision  int64                    `json:"connection_revision"`
	ProjectionRevision  *int64                   `json:"projection_revision,omitempty"`
	CustomerID          *string                  `json:"customer_id,omitempty"`
	OrderID             *string                  `json:"order_id,omitempty"`
	LinkedOrderSnapshot *crm.LinkedOrderSnapshot `json:"linked_order_snapshot,omitempty"`
	ScheduleProjection  *PlanScheduleView        `json:"schedule_projection,omitempty"`
}

type PlanScheduleView struct {
	Status          string     `json:"status"`
	ApplySuppressed bool       `json:"apply_suppressed"`
	SlotID          *string    `json:"slot_id,omitempty"`
	StartsAt        *time.Time `json:"starts_at,omitempty"`
	EndsAt          *time.Time `json:"ends_at,omitempty"`
	Timezone        *string    `json:"timezone,omitempty"`
}

type PlanFinalizationSnapshot struct {
	ID                         string    `json:"id"`
	PlanID                     string    `json:"plan_id"`
	FinalizationRevision       int64     `json:"finalization_revision"`
	PlanRevision               int64     `json:"plan_revision"`
	CurrentShotIDs             []string  `json:"current_shot_ids"`
	OutcomeEventRefs           []string  `json:"outcome_event_refs"`
	PreparationMissingEventIDs []string  `json:"preparation_missing_event_ids"`
	ExecutionFactRevision      int64     `json:"execution_fact_revision"`
	FinalizedAt                time.Time `json:"finalized_at"`
}

type PostgresRepository struct{}

type planReadScope interface {
	Query(context.Context, string, string, string, ...any) (store.Rows, error)
	QueryRow(context.Context, string, string, string, ...any) store.Row
	Count(context.Context, string, string, ...any) (int64, error)
}

func NewPostgresRepository() PostgresRepository { return PostgresRepository{} }

func (PostgresRepository) LockPlan(ctx context.Context, tx store.TxAccountScope, id string) (ShootPlan, error) {
	var plan ShootPlan
	var briefJSON []byte
	err := tx.QueryRowForUpdate(ctx, "shoot_plans",
		"id, title, subject, status, creative_brief, planned_look_count, planned_scene_count, revision, execution_fact_revision, created_at, updated_at, completed_at, archived_at",
		"id = $2", id).Scan(&plan.ID, &plan.Title, &plan.Subject, &plan.Status, &briefJSON,
		&plan.PublicScale.PlannedLookCount, &plan.PublicScale.PlannedSceneCount,
		&plan.Revision, &plan.ExecutionFactRevision, &plan.CreatedAt, &plan.UpdatedAt,
		&plan.CompletedAt, &plan.ArchivedAt)
	if errors.Is(err, store.ErrNoRows) {
		return ShootPlan{}, ErrPlanNotFound
	}
	if err != nil {
		return ShootPlan{}, fmt.Errorf("lock shoot plan: %w", err)
	}
	if err := json.Unmarshal(briefJSON, &plan.CreativeBrief); err != nil {
		return ShootPlan{}, fmt.Errorf("decode creative brief: %w", err)
	}
	return plan, nil
}

func (PostgresRepository) AdvancePlanRevision(ctx context.Context, tx store.TxAccountScope, id string, expected int64) (int64, error) {
	if expected < 1 {
		return 0, ErrPlanRevisionConflict
	}
	updated, err := tx.Update(ctx, "shoot_plans",
		"revision = revision + 1, updated_at = clock_timestamp()", "id = $2 AND revision = $3", id, expected)
	if err != nil {
		return 0, fmt.Errorf("advance shoot plan revision: %w", err)
	}
	if updated != 1 {
		return 0, ErrPlanRevisionConflict
	}
	return expected + 1, nil
}

func (PostgresRepository) Create(ctx context.Context, scope store.AccountScope, input CreatePlanInput) (ShootPlan, error) {
	title := strings.TrimSpace(input.Title)
	subject := strings.TrimSpace(input.Subject)
	if runeLen(title) < 1 || runeLen(title) > 160 || runeLen(subject) < 1 || runeLen(subject) > 240 {
		return ShootPlan{}, validationError("title 或 subject 非法")
	}
	var plan ShootPlan
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		created, err := PostgresRepository{}.CreateInScope(ctx, tx, CreatePlanInput{Title: title, Subject: subject})
		if err != nil {
			return err
		}
		plan = created
		return nil
	})
	return plan, err
}

func (PostgresRepository) CreateInScope(ctx context.Context, tx store.TxAccountScope, input CreatePlanInput) (ShootPlan, error) {
	id := newPlanID()
	if _, err := tx.InsertReturningID(ctx, "shoot_plans", []string{"id", "title", "subject"}, id, input.Title, input.Subject); err != nil {
		return ShootPlan{}, fmt.Errorf("create shoot plan in scope: %w", err)
	}
	if err := crm.InsertIndependentConnection(ctx, tx, id); err != nil {
		return ShootPlan{}, fmt.Errorf("create plan crm connection: %w", err)
	}
	return loadPlanFromTx(ctx, tx, id)
}

func loadPlanFromTx(ctx context.Context, tx store.TxAccountScope, id string) (ShootPlan, error) {
	return loadPlan(ctx, tx, id)
}

func (PostgresRepository) List(ctx context.Context, scope store.AccountScope, filter ListPlansFilter) (ListPlansResult, error) {
	if filter.Page == 0 {
		filter.Page = 1
	}
	if filter.PageSize == 0 {
		filter.PageSize = 20
	}
	if filter.Page < 1 || filter.PageSize < 1 || filter.PageSize > 100 {
		return ListPlansResult{}, validationError("pagination invalid")
	}
	cond := "status <> 'archived'"
	args := make([]any, 0, 1)
	if filter.ArchivedOnly {
		cond = "status = 'archived'"
	}
	if filter.Status != nil {
		if !validPlanStatus(*filter.Status) {
			return ListPlansResult{}, validationError("status invalid")
		}
		cond = "status = $2"
		args = append(args, string(*filter.Status))
	}
	if id := strings.TrimSpace(filter.CustomerID); id != "" {
		args = append(args, id)
		cond += fmt.Sprintf(" AND crm_customer_id = $%d", len(args)+1)
	}
	if id := strings.TrimSpace(filter.OrderID); id != "" {
		args = append(args, id)
		cond += fmt.Sprintf(" AND crm_order_id = $%d", len(args)+1)
	}
	total, err := scope.Count(ctx, "shoot_plan_list_projection", cond, args...)
	if err != nil {
		return ListPlansResult{}, fmt.Errorf("count shoot plans: %w", err)
	}
	rows, err := scope.QueryPage(ctx, "shoot_plan_list_projection",
		"id, title, subject, status, planned_look_count, planned_scene_count, revision, execution_fact_revision, created_at, updated_at, planned_shot_count, "+
			"crm_customer_id, crm_order_id, crm_customer_name, crm_order_title, crm_order_status_at_link, "+
			"window_starts_at, window_ends_at, window_timezone, window_source, captured_count, skipped_count, "+
			"readiness_required_total, readiness_required_unchecked",
		cond,
		[]store.OrderBy{{Column: "updated_at", Desc: true}, {Column: "id", Desc: true}},
		filter.PageSize, (filter.Page-1)*filter.PageSize, args...)
	if err != nil {
		return ListPlansResult{}, fmt.Errorf("list shoot plans: %w", err)
	}
	defer rows.Close()
	items := make([]PlanListItem, 0, filter.PageSize)
	for rows.Next() {
		var item PlanListItem
		var (
			customerID   sql.NullString
			orderID      sql.NullString
			customerName sql.NullString
			orderTitle   sql.NullString
			orderStatus  sql.NullString
			windowStart  sql.NullTime
			windowEnd    sql.NullTime
			windowTZ     sql.NullString
			windowSource sql.NullString
		)
		if err := rows.Scan(&item.ID, &item.Title, &item.Subject, &item.Status,
			&item.PublicScale.PlannedLookCount, &item.PublicScale.PlannedSceneCount,
			&item.Revision, &item.ExecutionFactRevision, &item.CreatedAt, &item.UpdatedAt,
			&item.PublicScale.PlannedShotCount,
			&customerID, &orderID, &customerName, &orderTitle, &orderStatus,
			&windowStart, &windowEnd, &windowTZ, &windowSource,
			&item.ExecutionStats.Captured, &item.ExecutionStats.Skipped,
			&item.ReadinessSummary.RequiredTotal, &item.ReadinessSummary.RequiredUnchecked); err != nil {
			return ListPlansResult{}, fmt.Errorf("scan shoot plan list: %w", err)
		}
		item.CRM = PlanListCrm{
			CustomerID:        nullStringPtr(customerID),
			CustomerName:      nullStringPtr(customerName),
			OrderID:           nullStringPtr(orderID),
			OrderTitle:        nullStringPtr(orderTitle),
			OrderStatusAtLink: nullStringPtr(orderStatus),
		}
		if windowSource.Valid && windowStart.Valid && windowEnd.Valid && windowTZ.Valid {
			item.ExecutionWindow = &PlanListWindow{
				Source:   windowSource.String,
				StartsAt: windowStart.Time,
				EndsAt:   windowEnd.Time,
				Timezone: windowTZ.String,
			}
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return ListPlansResult{}, fmt.Errorf("iterate shoot plan list: %w", err)
	}
	return ListPlansResult{Items: items, Total: total}, nil
}

func (PostgresRepository) Detail(ctx context.Context, scope store.AccountScope, id string, includeHistory bool) (PlanDetail, error) {
	return loadPlanDetail(ctx, scope, id, includeHistory)
}

func (PostgresRepository) DetailInScope(ctx context.Context, tx store.TxAccountScope, id string, includeHistory bool) (PlanDetail, error) {
	return loadPlanDetail(ctx, tx, id, includeHistory)
}

func loadPlanDetail(ctx context.Context, scope planReadScope, id string, includeHistory bool) (PlanDetail, error) {
	plan, err := loadPlan(ctx, scope, strings.TrimSpace(id))
	if err != nil {
		return PlanDetail{}, err
	}
	shots, err := loadShots(ctx, scope, id)
	if err != nil {
		return PlanDetail{}, err
	}
	readiness, err := loadReadiness(ctx, scope, id)
	if err != nil {
		return PlanDetail{}, err
	}
	window, err := loadWindow(ctx, scope, id)
	if err != nil {
		return PlanDetail{}, err
	}
	plan.ExecutionWindow = window
	detail := PlanDetail{ShootPlan: plan, Shots: shots, ReadinessItems: readiness}
	detail.CRM, err = loadCRMView(ctx, scope, id)
	if err != nil {
		return PlanDetail{}, err
	}
	if includeHistory {
		detail.ExecutionFacts, err = loadExecutionFacts(ctx, scope, id)
		if err != nil {
			return PlanDetail{}, err
		}
		detail.Finalizations, err = loadFinalizations(ctx, scope, id)
		if err != nil {
			return PlanDetail{}, err
		}
	}
	return detail, nil
}

func loadPlan(ctx context.Context, scope planReadScope, id string) (ShootPlan, error) {
	var plan ShootPlan
	var briefJSON []byte
	err := scope.QueryRow(ctx, "shoot_plans",
		"id, title, subject, status, creative_brief, planned_look_count, planned_scene_count, revision, execution_fact_revision, created_at, updated_at, completed_at, archived_at",
		"id = $2", id).Scan(
		&plan.ID, &plan.Title, &plan.Subject, &plan.Status, &briefJSON,
		&plan.PublicScale.PlannedLookCount, &plan.PublicScale.PlannedSceneCount,
		&plan.Revision, &plan.ExecutionFactRevision, &plan.CreatedAt, &plan.UpdatedAt,
		&plan.CompletedAt, &plan.ArchivedAt)
	if errors.Is(err, store.ErrNoRows) {
		return ShootPlan{}, ErrPlanNotFound
	}
	if err != nil {
		return ShootPlan{}, fmt.Errorf("load shoot plan: %w", err)
	}
	if err := json.Unmarshal(briefJSON, &plan.CreativeBrief); err != nil {
		return ShootPlan{}, fmt.Errorf("decode creative brief: %w", err)
	}
	shotCount, err := scope.Count(ctx, "shoot_plan_shots", "plan_id = $2 AND removed_at IS NULL", id)
	if err != nil {
		return ShootPlan{}, fmt.Errorf("count current shots: %w", err)
	}
	plan.PublicScale.PlannedShotCount = int(shotCount)
	return plan, nil
}

func loadShots(ctx context.Context, scope planReadScope, planID string) ([]Shot, error) {
	rows, err := scope.Query(ctx, "shoot_plan_shots",
		"id, plan_id, position, title, scene, action, expression, composition, lighting_text, notes, framing_tag, lighting_direction_tag, lighting_quality_tag, palette_tag, shot_type_tag, taxonomy_version, revision, execution_revision, next_event_seq, current_outcome_event_id, removed_at",
		"plan_id = $2 AND removed_at IS NULL", planID)
	if err != nil {
		return nil, fmt.Errorf("load shots: %w", err)
	}
	defer rows.Close()
	shots := make([]Shot, 0)
	currentEventByShot := make(map[string]string)
	for rows.Next() {
		var shot Shot
		var currentEventID *string
		if err := rows.Scan(&shot.ID, &shot.PlanID, &shot.Position, &shot.Title, &shot.Scene, &shot.Action,
			&shot.Expression, &shot.Composition, &shot.Lighting, &shot.Notes, &shot.FramingTag,
			&shot.LightingDirectionTag, &shot.LightingQualityTag, &shot.PaletteTag, &shot.ShotTypeTag,
			&shot.TaxonomyVersion, &shot.Revision, &shot.ExecutionRevision, &shot.NextEventSequence,
			&currentEventID, &shot.RemovedAt); err != nil {
			return nil, fmt.Errorf("scan shot: %w", err)
		}
		if currentEventID != nil {
			currentEventByShot[shot.ID] = *currentEventID
		}
		shots = append(shots, shot)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(shots, func(i, j int) bool {
		if shots[i].Position == shots[j].Position {
			return shots[i].ID < shots[j].ID
		}
		return shots[i].Position < shots[j].Position
	})
	linksByShot, err := loadShotReadinessLinks(ctx, scope, planID)
	if err != nil {
		return nil, err
	}
	outcomesByShot, err := loadCurrentOutcomes(ctx, scope, planID)
	if err != nil {
		return nil, err
	}
	for index := range shots {
		readinessItemIDs, linked := linksByShot[shots[index].ID]
		if !linked {
			readinessItemIDs = make([]string, 0)
		}
		shots[index].ReadinessItemIDs = readinessItemIDs
		if eventID, ok := currentEventByShot[shots[index].ID]; ok {
			outcome, exists := outcomesByShot[eventID]
			if !exists {
				return nil, errors.New("current outcome event projection is missing")
			}
			outcomeCopy := outcome
			shots[index].CurrentOutcome = &outcomeCopy
		}
	}
	return shots, nil
}

func loadShotReadinessLinks(ctx context.Context, scope planReadScope, planID string) (map[string][]string, error) {
	rows, err := scope.Query(ctx, "shoot_plan_shot_readiness_links", "shot_id, readiness_item_id", "plan_id = $2", planID)
	if err != nil {
		return nil, fmt.Errorf("load shot readiness links: %w", err)
	}
	defer rows.Close()
	ids := make(map[string][]string)
	for rows.Next() {
		var shotID, readinessID string
		if err := rows.Scan(&shotID, &readinessID); err != nil {
			return nil, err
		}
		ids[shotID] = append(ids[shotID], readinessID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for shotID := range ids {
		sort.Strings(ids[shotID])
	}
	return ids, nil
}

func loadReadiness(ctx context.Context, scope planReadScope, planID string) ([]ReadinessItem, error) {
	rows, err := scope.Query(ctx, "shoot_plan_readiness_items",
		"id, plan_id, category, title, requirement, preflight_status, responsibility_hint, default_preparation_lead_days, revision, removed_at, created_at",
		"plan_id = $2 AND removed_at IS NULL", planID)
	if err != nil {
		return nil, fmt.Errorf("load readiness: %w", err)
	}
	defer rows.Close()
	type readinessRow struct {
		item      ReadinessItem
		createdAt time.Time
	}
	loaded := make([]readinessRow, 0)
	for rows.Next() {
		var row readinessRow
		if err := rows.Scan(&row.item.ID, &row.item.PlanID, &row.item.Category, &row.item.Title, &row.item.Requirement,
			&row.item.PreflightStatus, &row.item.ResponsibilityHint, &row.item.DefaultPreparationLeadDays,
			&row.item.Revision, &row.item.RemovedAt, &row.createdAt); err != nil {
			return nil, fmt.Errorf("scan readiness: %w", err)
		}
		loaded = append(loaded, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(loaded, func(i, j int) bool {
		if loaded[i].createdAt.Equal(loaded[j].createdAt) {
			return loaded[i].item.ID < loaded[j].item.ID
		}
		return loaded[i].createdAt.Before(loaded[j].createdAt)
	})
	items := make([]ReadinessItem, len(loaded))
	for index := range loaded {
		items[index] = loaded[index].item
	}
	return items, nil
}

func loadWindow(ctx context.Context, scope planReadScope, planID string) (*PlanExecutionWindow, error) {
	var window PlanExecutionWindow
	err := scope.QueryRow(ctx, "shoot_plan_execution_windows",
		"source, source_ref, starts_at, ends_at, timezone, live_window_starts_at, live_window_ends_at, rule_version, revision",
		"plan_id = $2", planID).Scan(&window.Source, &window.SourceRef, &window.StartsAt, &window.EndsAt,
		&window.Timezone, &window.LiveWindowStartsAt, &window.LiveWindowEndsAt, &window.RuleVersion, &window.Revision)
	if errors.Is(err, store.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load execution window: %w", err)
	}
	return &window, nil
}

func loadCurrentOutcomes(ctx context.Context, scope planReadScope, planID string) (map[string]CurrentOutcome, error) {
	rows, err := scope.Query(ctx, "shoot_plan_execution_events",
		"id, result, skip_reason, checked_at, capture_mode", "plan_id = $2", planID)
	if err != nil {
		return nil, fmt.Errorf("load current outcomes: %w", err)
	}
	defer rows.Close()
	outcomes := make(map[string]CurrentOutcome)
	for rows.Next() {
		var outcome CurrentOutcome
		outcome.Set = true
		if err := rows.Scan(&outcome.EventID, &outcome.Result, &outcome.SkipReason, &outcome.CheckedAt, &outcome.CaptureMode); err != nil {
			return nil, fmt.Errorf("scan current outcome: %w", err)
		}
		outcomes[outcome.EventID] = outcome
	}
	return outcomes, rows.Err()
}

func loadExecutionFacts(ctx context.Context, scope planReadScope, planID string) ([]ExecutionFact, error) {
	rows, err := scope.Query(ctx, "shoot_plan_execution_events",
		"id, plan_id, shot_id, session_id, shot_event_seq, plan_revision, result, skip_reason, notes, checked_at, capture_mode, supersedes_event_id, revision",
		"plan_id = $2", planID)
	if err != nil {
		return nil, fmt.Errorf("load result execution facts: %w", err)
	}
	facts := make([]ExecutionFact, 0)
	for rows.Next() {
		fact := ExecutionFact{Kind: ExecutionFactResult}
		if err := rows.Scan(&fact.ID, &fact.PlanID, &fact.ShotID, &fact.SessionID, &fact.Sequence, &fact.PlanRevision,
			&fact.Result, &fact.SkipReason, &fact.Notes, &fact.CheckedAt, &fact.CaptureMode, &fact.SupersedesEventID, &fact.Revision); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan result execution fact: %w", err)
		}
		facts = append(facts, fact)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	rows, err = scope.Query(ctx, "shoot_plan_execution_event_voids",
		"id, plan_id, shot_id, target_event_id, shot_event_seq, reason, voided_at, revision",
		"plan_id = $2", planID)
	if err != nil {
		return nil, fmt.Errorf("load void execution facts: %w", err)
	}
	for rows.Next() {
		fact := ExecutionFact{Kind: ExecutionFactVoid}
		if err := rows.Scan(&fact.ID, &fact.PlanID, &fact.ShotID, &fact.TargetEventID, &fact.Sequence, &fact.Reason, &fact.VoidedAt, &fact.Revision); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan void execution fact: %w", err)
		}
		facts = append(facts, fact)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	sort.Slice(facts, func(i, j int) bool {
		if facts[i].ShotID == facts[j].ShotID {
			return facts[i].Sequence < facts[j].Sequence
		}
		return facts[i].ShotID < facts[j].ShotID
	})
	return facts, nil
}

func loadFinalizations(ctx context.Context, scope planReadScope, planID string) ([]PlanFinalizationSnapshot, error) {
	rows, err := scope.Query(ctx, "shoot_plan_finalization_snapshots",
		"id, plan_id, finalization_revision, plan_revision, current_shot_ids, outcome_event_refs, preparation_missing_event_ids, execution_fact_revision, finalized_at",
		"plan_id = $2", planID)
	if err != nil {
		return nil, fmt.Errorf("load finalization snapshots: %w", err)
	}
	defer rows.Close()
	snapshots := make([]PlanFinalizationSnapshot, 0)
	for rows.Next() {
		var snapshot PlanFinalizationSnapshot
		var shotIDsJSON, outcomeRefsJSON, preparationJSON []byte
		if err := rows.Scan(&snapshot.ID, &snapshot.PlanID, &snapshot.FinalizationRevision, &snapshot.PlanRevision,
			&shotIDsJSON, &outcomeRefsJSON, &preparationJSON, &snapshot.ExecutionFactRevision, &snapshot.FinalizedAt); err != nil {
			return nil, fmt.Errorf("scan finalization snapshot: %w", err)
		}
		if err := json.Unmarshal(shotIDsJSON, &snapshot.CurrentShotIDs); err != nil {
			return nil, fmt.Errorf("decode finalization shot ids: %w", err)
		}
		if err := json.Unmarshal(outcomeRefsJSON, &snapshot.OutcomeEventRefs); err != nil {
			return nil, fmt.Errorf("decode finalization outcome refs: %w", err)
		}
		if err := json.Unmarshal(preparationJSON, &snapshot.PreparationMissingEventIDs); err != nil {
			return nil, fmt.Errorf("decode finalization preparation refs: %w", err)
		}
		snapshots = append(snapshots, snapshot)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(snapshots, func(i, j int) bool { return snapshots[i].FinalizationRevision < snapshots[j].FinalizationRevision })
	return snapshots, nil
}

func validPlanStatus(status PlanStatus) bool {
	switch status {
	case PlanStatusDraft, PlanStatusReady, PlanStatusInProgress, PlanStatusCompleted, PlanStatusArchived:
		return true
	default:
		return false
	}
}

func loadCRMView(ctx context.Context, scope planReadScope, planID string) (*PlanCRMView, error) {
	var view PlanCRMView
	var customer, orderID sql.NullString
	var snapshot []byte
	err := scope.QueryRow(ctx, "plan_crm_connections",
		"state, connection_revision, customer_id, order_id, linked_order_snapshot",
		"plan_id = $2", planID).Scan(&view.State, &view.ConnectionRevision, &customer, &orderID, &snapshot)
	if errors.Is(err, store.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load plan crm connection: %w", err)
	}
	if customer.Valid {
		view.CustomerID = &customer.String
	}
	if orderID.Valid {
		view.OrderID = &orderID.String
	}
	if len(snapshot) > 0 {
		var snap crm.LinkedOrderSnapshot
		if err := json.Unmarshal(snapshot, &snap); err != nil {
			return nil, fmt.Errorf("decode linked order snapshot: %w", err)
		}
		view.LinkedOrderSnapshot = &snap
	}
	var projRev int64
	var status string
	var suppressed bool
	var slotID sql.NullString
	var start, end sql.NullTime
	var timezone sql.NullString
	err = scope.QueryRow(ctx, "plan_schedule_projections",
		"projection_revision, status, apply_suppressed, slot_id, starts_at, ends_at, timezone",
		"plan_id = $2", planID).Scan(&projRev, &status, &suppressed, &slotID, &start, &end, &timezone)
	if err != nil && !errors.Is(err, store.ErrNoRows) {
		return nil, fmt.Errorf("load plan schedule projection: %w", err)
	}
	if err == nil {
		view.ProjectionRevision = &projRev
		sched := PlanScheduleView{Status: status, ApplySuppressed: suppressed}
		if slotID.Valid {
			sched.SlotID = &slotID.String
		}
		if start.Valid {
			value := start.Time.UTC()
			sched.StartsAt = &value
		}
		if end.Valid {
			value := end.Time.UTC()
			sched.EndsAt = &value
		}
		if timezone.Valid {
			sched.Timezone = &timezone.String
		}
		view.ScheduleProjection = &sched
	}
	return &view, nil
}

func runeLen(value string) int { return len([]rune(value)) }
