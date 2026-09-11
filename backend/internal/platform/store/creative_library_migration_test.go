package store_test

import (
	"database/sql"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary/textindex"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func TestCreativeLibraryMigrationBackfillsAndCanRebuildAfterRollback(t *testing.T) {
	url := startPostgres(t)
	if err := store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal(err)
	} // 0042
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal(err)
	} // 0041
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal(err)
	} // 0040
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal(err)
	} // 0039 -> 0038 fixture
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	// Existing FND-02 asset; no new organizing tables required by this fixture.
	_, err = db.Exec(`INSERT INTO accounts(id,password_hash,status) VALUES('backfill','test','active');
 INSERT INTO creative_library_settings(account_id) VALUES('backfill');
 INSERT INTO creative_contents(id,account_id,kind,created_by_kind) VALUES('content','backfill','text','photographer');
 INSERT INTO creative_content_revisions(id,account_id,content_id,sequence,schema_version,payload,rights_declaration_id) VALUES('revision','backfill','content',1,1,'{"body":"窗边 ＡＩ"}','rights');
 INSERT INTO creative_assets(id,account_id,kind,title,normalized_title,description,content_id,content_revision_id) VALUES('asset','backfill','text','ＡＩ 灵感','old','描述','content','revision');`)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = store.MigrateUp(url); err != nil {
			t.Fatal(err)
		}
		var title, text, version string
		err = db.QueryRow(`SELECT a.normalized_title,s.normalized_text,s.normalization_version FROM creative_assets a JOIN creative_asset_search s ON s.account_id=a.account_id AND s.asset_id=a.id WHERE a.id='asset'`).Scan(&title, &text, &version)
		if err != nil || title != "ai 灵感" || version != textindex.Version || text == "" {
			t.Fatalf("backfill %q %q %q %v", title, text, version, err)
		}
		if err = store.MigrateUp(url); err != nil {
			t.Fatal("idempotent", err)
		}
		if err = store.MigrateDownOneForTest(url); err != nil {
			t.Fatal(err)
		} // 0042
		if err = store.MigrateDownOneForTest(url); err != nil {
			t.Fatal(err)
		} // 0041
		if err = store.MigrateDownOneForTest(url); err != nil {
			t.Fatal(err)
		} // 0040
		if err = store.MigrateDownOneForTest(url); err != nil {
			t.Fatal("rollback", err)
		}
		var n int
		if err = db.QueryRow(`SELECT count(*) FROM creative_assets WHERE id='asset'`).Scan(&n); err != nil || n != 1 {
			t.Fatal("rollback lost preexisting asset", err)
		}
	}
}
