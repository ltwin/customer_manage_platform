package store_test

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func rollbackAssetLibrary(t *testing.T, url string) {
	t.Helper()
	rollbackMediaGC(t, url)
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("rollback creative-asset-library", err)
	}
}

const assetLibraryFixture = `INSERT INTO accounts(id,password_hash,status) VALUES('lib','test','active');
 INSERT INTO creative_contents(id,account_id,kind,created_by_kind) VALUES('content','lib','text','photographer');
 INSERT INTO creative_content_revisions(id,account_id,content_id,sequence,schema_version,payload,rights_declaration_id) VALUES('revision','lib','content',1,1,'{"body":"x"}','rights');
 INSERT INTO creative_assets(id,account_id,kind,title,normalized_title,description,content_id,content_revision_id) VALUES('asset','lib','text','t','t','','content','revision');
 INSERT INTO creative_blob_palettes(account_id,blob_id,algorithm,state) VALUES('lib','blob','oklab-kmeans-v1','failed');
 INSERT INTO creative_asset_group_deletions(id,account_id,group_snapshot,promoted_child_ids,member_asset_ids,hierarchy_revision_after) VALUES('d','lib','{}','{}','{asset}',2)`

func openMigratedDB(t *testing.T) (string, *sql.DB) {
	t.Helper()
	url := startPostgres(t)
	if err := store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	// An application one release behind inserts assets without knowing the column.
	if _, err = db.Exec(assetLibraryFixture); err != nil {
		t.Fatal("old writer rejected", err)
	}
	return url, db
}

// Ratings are the photographer's own judgement, so a rollback waits until
// none are left.
func TestAssetLibraryMigrationRefusesToDropRatings(t *testing.T) {
	url, db := openMigratedDB(t)
	for _, bad := range []string{`UPDATE creative_assets SET rating_halves=0`, `UPDATE creative_assets SET rating_halves=11`} {
		if _, err := db.Exec(bad); err == nil {
			t.Fatal("rating out of range accepted:", bad)
		}
	}
	if _, err := db.Exec(`UPDATE creative_assets SET rating_halves=9`); err != nil {
		t.Fatal(err)
	}
	rollbackMediaGC(t, url)
	if err := store.MigrateDownOneForTest(url); err == nil || !strings.Contains(err.Error(), "rated assets exist") {
		t.Fatal("rollback dropped ratings", err)
	}
}

// Palettes are derived and group-deletion snapshots only serve undo, so they
// go with a rollback; the assets stay and the migration reapplies.
func TestAssetLibraryMigrationRollsBackWithoutRatings(t *testing.T) {
	url, db := openMigratedDB(t)
	rollbackAssetLibrary(t, url)
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM creative_assets WHERE id='asset'`).Scan(&n); err != nil || n != 1 {
		t.Fatal("rollback lost the asset", err)
	}
	if err := store.MigrateUp(url); err != nil {
		t.Fatal("reapply", err)
	}
}
