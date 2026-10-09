package store_test

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func rollbackGenerationHandoff(t *testing.T, url string) {
	t.Helper()
	rollbackStagedUploads(t, url)
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("rollback generation handoff", err)
	}
}
func TestGenerationHandoffMigration(t *testing.T) {
	url := startPostgres(t)
	if err := store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err = db.Exec(`INSERT INTO creative_generation_bindings(account_id,execution_id,request_id,canvas_id,recovery_until) VALUES('a','e','r','c',now())`); err != nil {
		t.Fatal(err)
	}
	rollbackStagedUploads(t, url)
	if err = store.MigrateDownOneForTest(url); err == nil || !strings.Contains(err.Error(), "generation handoff") {
		t.Fatal("lossy handoff rollback allowed", err)
	}
	clean := startPostgres(t)
	if err = store.MigrateUp(clean); err != nil {
		t.Fatal(err)
	}
	rollbackGenerationHandoff(t, clean)
	if err = store.MigrateUp(clean); err != nil {
		t.Fatal(err)
	}
}
