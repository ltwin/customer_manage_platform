package store_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func TestSettingsAvailabilityMigrationBackfillsExistingRowsAndRollsBack(t *testing.T) {
	url := startPostgres(t)
	if err := store.MigrateStepsForTest(url, 13); err != nil {
		t.Fatalf("migrate pre-availability schema: %v", err)
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatalf("open pre-availability database: %v", err)
	}
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `INSERT INTO accounts (id, password_hash) VALUES ('availability-legacy', 'hash')`); err != nil {
		t.Fatalf("insert legacy account: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO settings (account_id, timezone) VALUES ('availability-legacy', 'Asia/Tokyo')`); err != nil {
		t.Fatalf("insert legacy settings: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close pre-availability database: %v", err)
	}

	if err := store.MigrateUp(url); err != nil {
		t.Fatalf("migrate availability schema: %v", err)
	}
	db, err = sql.Open("pgx", url)
	if err != nil {
		t.Fatalf("reopen migrated database: %v", err)
	}
	var raw []byte
	if err := db.QueryRowContext(ctx, `SELECT availability FROM settings WHERE account_id = 'availability-legacy'`).Scan(&raw); err != nil {
		t.Fatalf("read backfilled availability: %v", err)
	}
	var value struct {
		Weekly            map[string]any `json:"weekly"`
		MinOpeningMinutes int            `json:"min_opening_minutes"`
		TurnaroundMinutes int            `json:"turnaround_minutes"`
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("decode backfilled availability: %v", err)
	}
	if len(value.Weekly) != 7 || value.MinOpeningMinutes != 120 || value.TurnaroundMinutes != 60 {
		t.Fatalf("backfilled availability = %s", raw)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close before down migration: %v", err)
	}
	for index, label := range []string{
		"agent-controls", "agent-checkpoints", "agent-context", "agent-runs", "skill-foundation", "agent-conversations", "llm-gateway",
		"creative-canvas-events",
		"creative-media",
		"creative-canvas-commands",
		"creative-library-organization",
		"creative-text-canvas",
		"creative-foundation",
		"creative-workspace",
		"settings-health-tiers",
		"orders-attribution-snapshot",
		"orders-payment-facts",
		"orders-delivery-due",
		"media-gallery-rights-bindings",
		"plan-list-enrichment",
		"plan-business-feedback",
		"plan-assignment-reminder-digest-intent", "plan-assignment-reminder-temporal", "plan-assignment-reminder-ingestion", "plan-assignment-reminders",
		"share assignments", "share feedbacks", "share anonymous projection", "share generations",
		"security attempt budget", "plan crm", "plan ingestion", "planning media", "shoot planning",
		"account profiles", "settings availability",
	} {
		if err := store.MigrateDownOneForTest(url); err != nil {
			t.Fatalf("%s down migration (step %d): %v", label, index+1, err)
		}
	}
	db, err = sql.Open("pgx", url)
	if err != nil {
		t.Fatalf("reopen after down: %v", err)
	}
	defer func() { _ = db.Close() }()
	var columnCount int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*) FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'settings' AND column_name = 'availability'`).Scan(&columnCount); err != nil {
		t.Fatalf("inspect down migration: %v", err)
	}
	if columnCount != 0 {
		t.Fatalf("availability column remains after down migration")
	}
}
