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
	// Named rather than counted: the point of this test is that 0039 itself
	// rolls back and rebuilds, so it has to land on 0038 no matter how many
	// migrations have since been stacked above. Four bare steps silently
	// stopped short once 0043 and 0044 arrived, and the test kept passing
	// because the backfill is driven by normalization_version rather than by
	// migration depth.
	rollbackTo0038(t, url)
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
		rollbackTo0038(t, url)
		var n int
		if err = db.QueryRow(`SELECT count(*) FROM creative_assets WHERE id='asset'`).Scan(&n); err != nil || n != 1 {
			t.Fatal("rollback lost preexisting asset", err)
		}
	}
}

// rollbackTo0038 names every migration it steps past. Adding one above without
// listing it here makes this test stop short of 0039 while still passing, which
// is exactly what happened between 0043 and 0045.
func rollbackTo0038(t *testing.T, url string) {
	t.Helper()
	for _, label := range []string{
		"0050 execution-continuations", "0049 agent-controls", "0048 agent-checkpoints", "0047 agent-context", "0046 agent-runs", "0045 skill-foundation", "0044 agent-conversations",
		"0043 llm-gateway",
		"0042 creative-canvas-events", "0041 creative-media", "0040 creative-canvas-commands",
		"0039 creative-library-organization",
	} {
		if err := store.MigrateDownOneForTest(url); err != nil {
			t.Fatalf("rollback %s: %v", label, err)
		}
	}
}
