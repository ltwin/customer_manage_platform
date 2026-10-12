package store_test

import (
	"database/sql"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func rollbackMediaGC(t *testing.T, url string) {
	t.Helper()
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("rollback creative-media-gc", err)
	}
}

// Existing imports start their clock at the migration, so the first GC pass
// after an upgrade cannot mistake every unfinished import for a stale one; the
// column is only GC's judgement and goes with a rollback.
func TestMediaGCMigrationStampsExistingImportsAndRollsBack(t *testing.T) {
	url := startPostgres(t)
	if err := store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	rollbackMediaGC(t, url)
	if _, err := db.Exec(`INSERT INTO creative_generation_imports(account_id,id,identity_hash,state) VALUES('gc','old','h','stored')`); err != nil {
		t.Fatal(err)
	}
	if err := store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	var fresh bool
	if err := db.QueryRow(`SELECT updated_at>clock_timestamp()-interval '1 minute' FROM creative_generation_imports WHERE id='old'`).Scan(&fresh); err != nil || !fresh {
		t.Fatal("an existing import was not stamped at the migration", err)
	}
	rollbackMediaGC(t, url)
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM information_schema.columns WHERE table_name='creative_generation_imports' AND column_name='updated_at'`).Scan(&n); err != nil || n != 0 {
		t.Fatal("rollback left the column", err)
	}
}
