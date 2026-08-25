package store_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func TestAccountAuthMigrationPreservesLegacyAccountAndRollsBack(t *testing.T) {
	url := startPostgres(t)
	if err := store.MigrateStepsForTest(url, 11); err != nil {
		t.Fatalf("migrate legacy schema: %v", err)
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatalf("open legacy database: %v", err)
	}
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `INSERT INTO accounts (id, password_hash) VALUES ('legacy-account', 'legacy-hash')`); err != nil {
		t.Fatalf("insert legacy account: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO customers (id, account_id, display_name, channel)
		VALUES ('legacy-customer', 'legacy-account', '旧客户', 'other')`); err != nil {
		t.Fatalf("insert legacy customer: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy database: %v", err)
	}

	if err := store.MigrateUp(url); err != nil {
		t.Fatalf("migrate auth schema: %v", err)
	}
	if err := store.MigrateUp(url); err != nil {
		t.Fatalf("repeat migrate auth schema: %v", err)
	}
	db, err = sql.Open("pgx", url)
	if err != nil {
		t.Fatalf("reopen migrated database: %v", err)
	}
	defer func() { _ = db.Close() }()

	var accountID, status, legacyHash, credentialHash string
	err = db.QueryRowContext(ctx, `
		SELECT a.id, a.status, a.password_hash, pc.password_hash
		FROM accounts a
		JOIN password_credentials pc ON pc.account_id = a.id
		WHERE a.id = 'legacy-account'`).Scan(&accountID, &status, &legacyHash, &credentialHash)
	if err != nil {
		t.Fatalf("read migrated legacy account: %v", err)
	}
	if accountID != "legacy-account" || status != "legacy_unclaimed" || legacyHash != "legacy-hash" || credentialHash != legacyHash {
		t.Fatalf("migrated legacy account = %q/%q/%q/%q", accountID, status, legacyHash, credentialHash)
	}
	var customerAccountID string
	if err := db.QueryRowContext(ctx, `SELECT account_id FROM customers WHERE id = 'legacy-customer'`).Scan(&customerAccountID); err != nil {
		t.Fatalf("read migrated customer: %v", err)
	}
	if customerAccountID != accountID {
		t.Fatalf("customer account_id = %q, want %q", customerAccountID, accountID)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO accounts (id, password_hash) VALUES ('legacy-other', 'other-hash')`); err != nil {
		t.Fatalf("insert second legacy account: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO account_identities (id, account_id, kind, normalized_value)
		VALUES ('identity-a', 'legacy-account', 'email', 'owner@example.test')`); err != nil {
		t.Fatalf("insert email identity: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO account_identities (id, account_id, kind, normalized_value)
		VALUES ('identity-b', 'legacy-other', 'email', 'owner@example.test')`); err == nil {
		t.Fatal("normalized email identity must be globally unique")
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO auth_action_tokens (selector, account_id, purpose, secret_hash, expires_at)
		VALUES ('selector', 'legacy-account', 'legacy_claim', decode('00', 'hex'), now() + interval '1 hour')`); err == nil {
		t.Fatal("auth action token hash must be exactly 32 bytes")
	}

	if err := db.Close(); err != nil {
		t.Fatalf("close before auth down migration: %v", err)
	}
	for index, label := range []string{
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
		"account profiles", "settings availability", "limiter", "legacy-only auth",
	} {
		if err := store.MigrateDownOneForTest(url); err != nil {
			t.Fatalf("%s down migration (step %d): %v", label, index+1, err)
		}
	}
	db, err = sql.Open("pgx", url)
	if err != nil {
		t.Fatalf("reopen after auth down migration: %v", err)
	}
	defer func() { _ = db.Close() }()
	var authTables int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*) FROM information_schema.tables
		WHERE table_schema = 'public'
		  AND table_name IN ('account_identities', 'password_credentials', 'auth_action_tokens',
		                     'refresh_session_families', 'refresh_session_generations')`).Scan(&authTables); err != nil {
		t.Fatalf("inspect auth down migration: %v", err)
	}
	if authTables != 0 {
		t.Fatalf("auth down migration left %d tables", authTables)
	}
}

func TestAccountAuthDownMigrationRejectsNewStyleAccount(t *testing.T) {
	url := startPostgres(t)
	if err := store.MigrateUp(url); err != nil {
		t.Fatalf("migrate auth schema: %v", err)
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatalf("open migrated database: %v", err)
	}
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO accounts (id, status, password_hash)
		VALUES ('new-account', 'pending_verification', NULL)`); err != nil {
		t.Fatalf("insert new-style account: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close before blocked down migration: %v", err)
	}

	for index, label := range []string{
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
		"account profiles", "settings availability", "limiter",
	} {
		if err := store.MigrateDownOneForTest(url); err != nil {
			t.Fatalf("%s down migration (step %d): %v", label, index+1, err)
		}
	}
	err = store.MigrateDownOneForTest(url)
	if err == nil || !strings.Contains(err.Error(), "auth_schema_down_blocked_new_accounts") {
		t.Fatalf("down migration error = %v, want fail-closed marker", err)
	}
}
