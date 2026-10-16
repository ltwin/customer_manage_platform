package store_test

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store/storetest"
)

func rollbackGenerationBillingOptional(t *testing.T, url string) {
	t.Helper()
	rollbackProviderLimits(t, url)
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("rollback generation-billing-optional", err)
	}
}

func TestGenerationBillingOptionalMigration(t *testing.T) {
	url := storetest.NewURL(t)
	rollbackGenerationBillingOptional(t, url)
	if err := store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	_, err = db.Exec(`INSERT INTO llm_usage_reservations(id,account_id,caller_service,caller_operation_id,caller_group_id,group_limit_micros,group_token_limit,budget_period,currency,initial_reserved_micros,initial_reserved_tokens,remaining_hold_micros,remaining_hold_tokens,settlement_state,price_version,expires_at)
 VALUES('llmv_00000000-0000-0000-0000-000000000001','a','test','00000000-0000-0000-0000-000000000001','g',1,1,'2026-10-01','USD',0,0,0,0,'not_applicable','v',now())`)
	if err != nil {
		t.Fatal(err)
	}
	for _, assignment := range []string{"actual_micros=0", "actual_tokens=0", "remaining_hold_micros=1", "initial_reserved_micros=1"} {
		if _, err = db.Exec("UPDATE llm_usage_reservations SET " + assignment); err == nil {
			t.Fatalf("unbilled record accepted %s", assignment)
		}
	}
	rollbackProviderLimits(t, url)
	if err = store.MigrateDownOneForTest(url); err == nil || !strings.Contains(err.Error(), "unbilled generation calls exist") {
		t.Fatalf("lossy downgrade allowed: %v", err)
	}
}
