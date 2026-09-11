package store_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// TestSettingsHealthTiersMigrationDefaultsExistingRowsAndRollsBack 锁住 0035：
// 存量 settings 行由列默认直接获得原型口径（1.2/2/3.5 倍 + 120 天基线，无 backfill）；
// down 迁移丢弃该列（参数可重录，丢列无数据保全义务）。
func TestSettingsHealthTiersMigrationDefaultsExistingRowsAndRollsBack(t *testing.T) {
	url := startPostgres(t)
	if err := store.MigrateStepsForTest(url, 34); err != nil {
		t.Fatalf("migrate pre-health-tiers schema: %v", err)
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatalf("open pre-health-tiers database: %v", err)
	}
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `INSERT INTO accounts (id, password_hash) VALUES ('health-legacy', 'hash')`); err != nil {
		t.Fatalf("insert legacy account: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO settings (account_id, timezone) VALUES ('health-legacy', 'Asia/Tokyo')`); err != nil {
		t.Fatalf("insert legacy settings: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close pre-health-tiers database: %v", err)
	}

	if err := store.MigrateUp(url); err != nil {
		t.Fatalf("migrate health-tiers schema: %v", err)
	}
	db, err = sql.Open("pgx", url)
	if err != nil {
		t.Fatalf("reopen migrated database: %v", err)
	}
	var raw []byte
	if err := db.QueryRowContext(ctx, `SELECT health_tiers FROM settings WHERE account_id = 'health-legacy'`).Scan(&raw); err != nil {
		t.Fatalf("read defaulted health_tiers: %v", err)
	}
	var value struct {
		SleepingRatio       float64 `json:"sleeping_ratio"`
		AtRiskRatio         float64 `json:"at_risk_ratio"`
		LostRatio           float64 `json:"lost_ratio"`
		FallbackCadenceDays int     `json:"fallback_cadence_days"`
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("decode defaulted health_tiers: %v", err)
	}
	if value.SleepingRatio != 1.2 || value.AtRiskRatio != 2 || value.LostRatio != 3.5 || value.FallbackCadenceDays != 120 {
		t.Fatalf("defaulted health_tiers = %s", raw)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close before down migration: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("rollback creative-canvas-events migration: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("rollback creative-media migration: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("rollback creative-canvas-commands migration: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("rollback creative-library-organization migration: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("rollback creative-text-canvas migration: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("rollback creative-foundation migration: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("rollback creative-workspace migration: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("health-tiers down migration: %v", err)
	}
	db, err = sql.Open("pgx", url)
	if err != nil {
		t.Fatalf("reopen after down: %v", err)
	}
	defer func() { _ = db.Close() }()
	var columnCount int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*) FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'settings' AND column_name = 'health_tiers'`).Scan(&columnCount); err != nil {
		t.Fatalf("inspect down migration: %v", err)
	}
	if columnCount != 0 {
		t.Fatal("health_tiers column remains after down migration")
	}
}
