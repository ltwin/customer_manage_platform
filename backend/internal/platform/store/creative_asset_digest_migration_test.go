package store_test

import (
	"database/sql"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func rollbackAssetDigestLookup(t *testing.T, url string) {
	t.Helper()
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("rollback asset-digest-lookup", err)
	}
}

// The digest index only speeds up the import check; it holds nothing a
// rollback would lose.
func TestAssetDigestMigrationRollsBackAndReapplies(t *testing.T) {
	url := startPostgres(t)
	if err := store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	indexed := func() bool {
		var ok bool
		if err := db.QueryRow(`SELECT to_regclass('creative_blob_digest') IS NOT NULL`).Scan(&ok); err != nil {
			t.Fatal(err)
		}
		return ok
	}
	if !indexed() {
		t.Fatal("digest index missing")
	}
	rollbackAssetDigestLookup(t, url)
	if indexed() {
		t.Fatal("digest index survived rollback")
	}
	if err := store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
}
