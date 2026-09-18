package store_test

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// TestLLMGatewayMigrationShapeAndLossyRollback pins the 0043 ledger shape and
// proves a rollback can never silently drop real spend evidence.
func TestLLMGatewayMigrationShapeAndLossyRollback(t *testing.T) {
	url := startPostgres(t)
	if err := store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	for _, table := range []string{
		"llm_call_groups", "llm_budgets", "llm_usage_reservations", "llm_requests", "llm_attempts",
		"llm_usage_measurements", "llm_measurement_dispositions", "llm_cost_positions",
		"llm_token_positions", "llm_settlement_receipts", "llm_result_consumers", "platform_llm_limits",
	} {
		var exists bool
		if err := db.QueryRow(
			`SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_schema='public' AND table_name=$1)`,
			table).Scan(&exists); err != nil || !exists {
			t.Fatalf("table %s missing after up: %v", table, err)
		}
	}

	// Every business table carries an explicit account key; the shared provider
	// admission record deliberately does not, and stays out of business scope.
	var accountScoped int
	if err := db.QueryRow(`
		SELECT count(*) FROM information_schema.columns
		WHERE table_schema='public' AND column_name='account_id' AND table_name LIKE 'llm\_%'`).Scan(&accountScoped); err != nil {
		t.Fatal(err)
	}
	if accountScoped != 11 {
		t.Fatalf("%d of 11 gateway business tables are account scoped", accountScoped)
	}
	var platformAccountColumns int
	if err := db.QueryRow(`
		SELECT count(*) FROM information_schema.columns
		WHERE table_schema='public' AND table_name='platform_llm_limits' AND column_name='account_id'`).
		Scan(&platformAccountColumns); err != nil {
		t.Fatal(err)
	}
	if platformAccountColumns != 0 {
		t.Fatal("the platform admission record must not carry an account column")
	}

	// ADR-008: explicit key relations only, no database foreign keys.
	var foreignKeys int
	if err := db.QueryRow(`
		SELECT count(*) FROM information_schema.table_constraints
		WHERE table_schema='public' AND constraint_type='FOREIGN KEY'
		  AND (table_name LIKE 'llm\_%' OR table_name='platform_llm_limits')`).Scan(&foreignKeys); err != nil {
		t.Fatal(err)
	}
	if foreignKeys != 0 {
		t.Fatalf("0043 introduced %d foreign keys", foreignKeys)
	}

	// Unknown is NULL, never zero: a known measurement must carry a value.
	insertMeasurement := func(certainty string, cost any) error {
		_, err := db.Exec(`
		INSERT INTO llm_usage_measurements
		 (id,account_id,attempt_id,measurement_key,cost_component,currency,usage_dimensions,price_snapshot,
		  certainty,evidence_kind,evidence_rank,evidence_digest,cost_micros)
		VALUES ('llmu_00000000-0000-4000-8000-000000000001','acc','llma_x','k','output','USD','{}','{}',
		 $1,'derived_from_usage',5,$2,$3)`, certainty, "sha256-"+strings.Repeat("a", 64), cost)
		return err
	}
	if err := insertMeasurement("known", nil); err == nil {
		t.Fatal("a known measurement without any value was accepted")
	}
	if err := insertMeasurement("known", 12); err != nil {
		t.Fatalf("a known measurement with a value must be accepted: %v", err)
	}

	if _, err := db.Exec(`DELETE FROM llm_usage_measurements`); err != nil {
		t.Fatal(err)
	}
	// 0044 (agent conversations) and 0045 (skill foundation) sit above the
	// gateway; step past them first so this test still exercises the gateway's
	// own rollback guard.
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("execution continuation rollback", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("agent control rollback", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("agent checkpoint rollback", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("rollback agent-context migration: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("rollback agent-runs migration: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("rollback skill-foundation migration: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("rollback agent-conversations migration: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("rollback on an empty ledger must succeed: %v", err)
	}
	if err := store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO llm_budgets (account_id,period_start,currency,limit_micros,token_limit,spent_micros)
		VALUES ('acc',date_trunc('month',now())::date,'USD',1000,1000,42)`); err != nil {
		t.Fatal(err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("execution continuation rollback", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("agent control rollback", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("agent checkpoint rollback", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("rollback agent-context migration: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("rollback agent-runs migration: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("rollback skill-foundation migration: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("rollback agent-conversations migration: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err == nil {
		t.Fatal("a rollback that would drop recorded spend must be refused")
	}
}
