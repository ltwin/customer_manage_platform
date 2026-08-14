package crm

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const (
	connectionColumns = "plan_id, customer_id, order_id, link_epoch_id, linked_order_snapshot, state, connection_revision, next_event_seq"
	projectionColumns = "plan_id, order_id, slot_id, slot_source_fingerprint, starts_at, ends_at, timezone, status, apply_suppressed, rule_version, projection_revision"
	windowColumns     = "source, source_ref, starts_at, ends_at, timezone, live_window_starts_at, live_window_ends_at, rule_version, revision"
	summaryColumns    = "plan_count, active_plan_count, primary_plan_id, primary_title, primary_status, primary_shot_count, primary_readiness_unchecked_count, window_starts_at, window_ends_at, window_timezone, window_source, link_warning"
)

func InsertIndependentConnection(ctx context.Context, tx store.TxAccountScope, planID string) error {
	return tx.Insert(ctx, "plan_crm_connections",
		[]string{"plan_id", "state", "connection_revision", "next_event_seq"},
		planID, string(StateIndependent), int64(1), int64(1))
}

func LoadConnection(ctx context.Context, tx store.TxAccountScope, planID string, forUpdate bool) (Connection, error) {
	var row store.Row
	if forUpdate {
		row = tx.QueryRowForUpdate(ctx, "plan_crm_connections", connectionColumns, "plan_id = $2", planID)
	} else {
		row = tx.QueryRow(ctx, "plan_crm_connections", connectionColumns, "plan_id = $2", planID)
	}
	conn, err := scanConnection(row)
	if errors.Is(err, store.ErrNoRows) {
		return Connection{}, ErrNotFound
	}
	return conn, err
}

func LoadProjection(ctx context.Context, tx store.TxAccountScope, planID string, forUpdate bool) (*Projection, error) {
	var row store.Row
	if forUpdate {
		row = tx.QueryRowForUpdate(ctx, "plan_schedule_projections", projectionColumns, "plan_id = $2", planID)
	} else {
		row = tx.QueryRow(ctx, "plan_schedule_projections", projectionColumns, "plan_id = $2", planID)
	}
	proj, err := scanProjection(row)
	if errors.Is(err, store.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &proj, nil
}

func LoadWindow(ctx context.Context, tx store.TxAccountScope, planID string) (*Window, error) {
	win, err := scanWindow(tx.QueryRow(ctx, "shoot_plan_execution_windows", windowColumns, "plan_id = $2", planID))
	if errors.Is(err, store.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &win, nil
}

func LoadAccountTimezone(ctx context.Context, tx store.TxAccountScope) (string, error) {
	var timezone string
	err := tx.QueryRow(ctx, "settings", "timezone", "TRUE").Scan(&timezone)
	if errors.Is(err, store.ErrNoRows) {
		return "Asia/Shanghai", nil
	}
	if err != nil {
		return "", err
	}
	if timezone == "" {
		return "Asia/Shanghai", nil
	}
	return timezone, nil
}

func PersistResult(ctx context.Context, tx store.TxAccountScope, plan PlanFact, before Connection, beforeProj *Projection, beforeWindow *Window, result Result) error {
	if result.ConnectionChanged {
		snapshot, err := snapshotJSON(result.Connection.LinkedOrderSnapshot)
		if err != nil {
			return err
		}
		updated, err := tx.Update(ctx, "plan_crm_connections",
			"customer_id = $2, order_id = $3, link_epoch_id = $4, linked_order_snapshot = $5, state = $6, connection_revision = $7, updated_at = clock_timestamp()",
			"plan_id = $8 AND connection_revision = $9",
			nullableString(result.Connection.CustomerID),
			nullableString(result.Connection.OrderID),
			nullableString(result.Connection.LinkEpochID),
			snapshot,
			string(result.Connection.State),
			result.Connection.ConnectionRevision,
			plan.ID,
			before.ConnectionRevision,
		)
		if err != nil {
			return fmt.Errorf("update plan crm connection: %w", err)
		}
		if updated != 1 {
			return ErrPlanRevisionConflict
		}
	}
	if result.ProjectionChanged && result.Projection != nil {
		if err := persistProjection(ctx, tx, beforeProj, result.Projection); err != nil {
			return err
		}
	}
	if result.WindowChanged && activeLifecycle(plan.Status) {
		if result.ClearWindow || result.Window == nil {
			if _, err := tx.Delete(ctx, "shoot_plan_execution_windows", "plan_id = $2", plan.ID); err != nil {
				return err
			}
		} else if err := persistWindow(ctx, tx, plan.ID, beforeWindow, *result.Window); err != nil {
			return err
		}
		updated, err := tx.Update(ctx, "shoot_plans",
			"revision = revision + 1, updated_at = clock_timestamp()",
			"id = $2 AND revision = $3", plan.ID, plan.Revision)
		if err != nil {
			return fmt.Errorf("advance shoot plan revision: %w", err)
		}
		if updated != 1 {
			return ErrPlanRevisionConflict
		}
	}
	if result.ConnectionChanged || result.ProjectionChanged {
		var seq int64
		if err := tx.QueryRowForUpdate(ctx, "plan_crm_connections", "next_event_seq", "plan_id = $2", plan.ID).Scan(&seq); err != nil {
			return err
		}
		for _, event := range result.Events {
			bodyFrom, err := json.Marshal(event.FromRefs)
			if err != nil {
				return err
			}
			bodyTo, err := json.Marshal(event.ToRefs)
			if err != nil {
				return err
			}
			snap, err := snapshotJSON(event.LinkEpochSnapshot)
			if err != nil {
				return err
			}
			if err := tx.Insert(ctx, "plan_crm_connection_events",
				[]string{
					"id", "plan_id", "crm_event_seq", "kind", "from_refs", "to_refs", "link_epoch_snapshot",
					"source_fingerprint", "connection_revision_after", "projection_revision_after",
					"plan_revision_after", "window_revision_after",
				},
				"pce_"+uuid.NewString(), plan.ID, seq, string(event.Kind), bodyFrom, bodyTo, snap,
				nullableString(&event.SourceFingerprint),
				nullableInt64(event.ConnectionRevisionAfter),
				nullableInt64(event.ProjectionRevisionAfter),
				nullableInt64(event.PlanRevisionAfter),
				nullableInt64(event.WindowRevisionAfter),
			); err != nil {
				return fmt.Errorf("insert crm connection event: %w", err)
			}
			seq++
		}
		if _, err := tx.Update(ctx, "plan_crm_connections", "next_event_seq = $2", "plan_id = $3", seq, plan.ID); err != nil {
			return err
		}
	}
	return nil
}

func persistProjection(ctx context.Context, tx store.TxAccountScope, before *Projection, proj *Projection) error {
	exists, err := tx.Exists(ctx, "plan_schedule_projections", "plan_id = $2", proj.PlanID)
	if err != nil {
		return err
	}
	if !exists {
		return tx.Insert(ctx, "plan_schedule_projections",
			[]string{"plan_id", "order_id", "slot_id", "slot_source_fingerprint", "starts_at", "ends_at", "timezone", "status", "apply_suppressed", "rule_version", "projection_revision"},
			proj.PlanID, nullableString(proj.OrderID), nullableString(proj.SlotID), nullableString(&proj.SlotSourceFingerprint),
			nullableTime(proj.StartsAt), nullableTime(proj.EndsAt), nullableString(proj.Timezone),
			string(proj.Status), proj.ApplySuppressed, proj.RuleVersion, proj.ProjectionRevision)
	}
	expected := int64(0)
	if before != nil && before.Exists {
		expected = before.ProjectionRevision
	}
	updated, err := tx.Update(ctx, "plan_schedule_projections",
		"order_id = $2, slot_id = $3, slot_source_fingerprint = $4, starts_at = $5, ends_at = $6, timezone = $7, status = $8, apply_suppressed = $9, rule_version = $10, projection_revision = $11, updated_at = clock_timestamp()",
		"plan_id = $12 AND projection_revision = $13",
		nullableString(proj.OrderID), nullableString(proj.SlotID), nullableString(&proj.SlotSourceFingerprint),
		nullableTime(proj.StartsAt), nullableTime(proj.EndsAt), nullableString(proj.Timezone),
		string(proj.Status), proj.ApplySuppressed, proj.RuleVersion, proj.ProjectionRevision, proj.PlanID, expected)
	if err != nil {
		return fmt.Errorf("update plan schedule projection: %w", err)
	}
	if updated != 1 {
		return ErrPlanRevisionConflict
	}
	return nil
}

func persistWindow(ctx context.Context, tx store.TxAccountScope, planID string, before *Window, window Window) error {
	exists, err := tx.Exists(ctx, "shoot_plan_execution_windows", "plan_id = $2", planID)
	if err != nil {
		return err
	}
	if !exists {
		return tx.Insert(ctx, "shoot_plan_execution_windows",
			[]string{"plan_id", "source", "source_ref", "starts_at", "ends_at", "timezone", "live_window_starts_at", "live_window_ends_at", "rule_version", "revision"},
			planID, window.Source, nullableString(window.SourceRef), window.StartsAt, window.EndsAt, window.Timezone,
			window.LiveWindowStartsAt, window.LiveWindowEndsAt, window.RuleVersion, window.Revision)
	}
	expected := int64(0)
	if before != nil {
		expected = before.Revision
	}
	updated, err := tx.Update(ctx, "shoot_plan_execution_windows",
		"source = $2, source_ref = $3, starts_at = $4, ends_at = $5, timezone = $6, live_window_starts_at = $7, live_window_ends_at = $8, rule_version = $9, revision = $10",
		"plan_id = $11 AND revision = $12",
		window.Source, nullableString(window.SourceRef), window.StartsAt, window.EndsAt, window.Timezone,
		window.LiveWindowStartsAt, window.LiveWindowEndsAt, window.RuleVersion, window.Revision, planID, expected)
	if err != nil {
		return fmt.Errorf("update shoot plan execution window: %w", err)
	}
	if updated != 1 {
		return ErrPlanRevisionConflict
	}
	return nil
}

func WriteGenerationResolution(ctx context.Context, tx store.TxAccountScope, planID string, generation int64) error {
	var mutationKind string
	if err := tx.QueryRow(ctx, "planning_reminder_generation_work",
		"mutation_kind", "generation = $2", generation).Scan(&mutationKind); err != nil {
		return fmt.Errorf("load generation work for resolution: %w", err)
	}
	return tx.Insert(ctx, "planning_reminder_generation_resolutions",
		[]string{"generation", "plan_id", "mutation_kind", "resolution_kind", "source_event_id", "resolved_at"},
		generation, planID, mutationKind, "lifecycle_applied", nil, time.Now().UTC())
}

func scanConnection(row interface{ Scan(...any) error }) (Connection, error) {
	var conn Connection
	var customer, orderID, epoch sql.NullString
	var snapshot []byte
	if err := row.Scan(&conn.PlanID, &customer, &orderID, &epoch, &snapshot, &conn.State, &conn.ConnectionRevision, &conn.NextEventSeq); err != nil {
		return Connection{}, err
	}
	conn.CustomerID = nullStringPtr(customer)
	conn.OrderID = nullStringPtr(orderID)
	conn.LinkEpochID = nullStringPtr(epoch)
	if len(snapshot) > 0 {
		var snap LinkedOrderSnapshot
		if err := json.Unmarshal(snapshot, &snap); err != nil {
			return Connection{}, fmt.Errorf("decode linked order snapshot: %w", err)
		}
		conn.LinkedOrderSnapshot = &snap
	}
	return conn, nil
}

func scanProjection(row interface{ Scan(...any) error }) (Projection, error) {
	var proj Projection
	var orderID, slotID, fingerprint, timezone sql.NullString
	var start, end sql.NullTime
	if err := row.Scan(&proj.PlanID, &orderID, &slotID, &fingerprint, &start, &end, &timezone, &proj.Status, &proj.ApplySuppressed, &proj.RuleVersion, &proj.ProjectionRevision); err != nil {
		return Projection{}, err
	}
	proj.Exists = true
	proj.OrderID = nullStringPtr(orderID)
	proj.SlotID = nullStringPtr(slotID)
	if fingerprint.Valid {
		proj.SlotSourceFingerprint = fingerprint.String
	}
	if start.Valid {
		value := start.Time.UTC()
		proj.StartsAt = &value
	}
	if end.Valid {
		value := end.Time.UTC()
		proj.EndsAt = &value
	}
	proj.Timezone = nullStringPtr(timezone)
	return proj, nil
}

func scanWindow(row interface{ Scan(...any) error }) (Window, error) {
	var window Window
	var sourceRef sql.NullString
	if err := row.Scan(&window.Source, &sourceRef, &window.StartsAt, &window.EndsAt, &window.Timezone,
		&window.LiveWindowStartsAt, &window.LiveWindowEndsAt, &window.RuleVersion, &window.Revision); err != nil {
		return Window{}, err
	}
	window.SourceRef = nullStringPtr(sourceRef)
	window.StartsAt = window.StartsAt.UTC()
	window.EndsAt = window.EndsAt.UTC()
	window.LiveWindowStartsAt = window.LiveWindowStartsAt.UTC()
	window.LiveWindowEndsAt = window.LiveWindowEndsAt.UTC()
	return window, nil
}

func snapshotJSON(snap *LinkedOrderSnapshot) (any, error) {
	if snap == nil {
		return nil, nil
	}
	return json.Marshal(snap)
}

func nullableString(value *string) any {
	if value == nil || *value == "" {
		return nil
	}
	return *value
}

func nullableInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC()
}

func nullStringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}
