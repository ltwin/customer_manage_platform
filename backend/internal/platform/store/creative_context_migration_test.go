package store_test

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func TestAgentContextMigrationRejectsLossyRollback(t *testing.T) {
	url := startPostgres(t)
	if err := store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("generation media facts rollback", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("execution continuation rollback", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("control rollback", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("checkpoint rollback", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal(err)
	}
	if err := store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	for _, table := range []string{"creative_agent_context_items", "creative_context_item_content_refs"} {
		var n int
		if err = db.QueryRow(`SELECT count(*) FROM information_schema.columns WHERE table_name=$1 AND column_name='account_id'`, table).Scan(&n); err != nil || n != 1 {
			t.Fatalf("scoping %s: %v", table, err)
		}
	}
	if _, err = db.Exec(`INSERT INTO creative_agent_context_items(id,account_id,run_id,kind,source_manifest,payload,digest,revision,retained_until) VALUES('ccci_test','scope','run','tool_result','{}','saved',repeat('a',64),1,now()+interval '90 days')`); err != nil {
		t.Fatal(err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("generation media facts rollback", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("execution continuation rollback", err)
	}
	if err = store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("control rollback", err)
	}
	if err = store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("checkpoint rollback", err)
	}
	if err = store.MigrateDownOneForTest(url); err == nil || !strings.Contains(err.Error(), "export") {
		t.Fatalf("lossy rollback: %v", err)
	}
}
