package store_test

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// rollbackSceneDocument is the head of the rollback chain: every older
// migration's rollback helper undoes this one first.
func rollbackSceneDocument(t *testing.T, url string) {
	t.Helper()
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("rollback creative-scene-document", err)
	}
}

func TestSceneDocumentMigrationAddsKindAndRefusesLossyRollback(t *testing.T) {
	url := startPostgres(t)
	if err := store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	insert := `INSERT INTO creative_contents(id,account_id,kind,created_by_kind) VALUES($1,'acct','scene','photographer')`

	// The clean rollback runs first: a refused one leaves the version dirty.
	rollbackSceneDocument(t, url)
	if _, err = db.Exec(insert, "ccnt_old"); err == nil {
		t.Fatal("scene kind accepted before the migration")
	}
	if err = store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(insert, "ccnt_scene"); err != nil {
		t.Fatal("scene kind rejected", err)
	}
	if err = store.MigrateDownOneForTest(url); err == nil || !strings.Contains(err.Error(), "scene documents exist") {
		t.Fatal("lossy scene rollback allowed", err)
	}
}
