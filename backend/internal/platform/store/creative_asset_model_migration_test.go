package store_test

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// rollbackCreativeAssetModel undoes 0080 and then 0079; every older
// migration's rollback helper undoes this one first.
func rollbackCreativeAssetModel(t *testing.T, url string) {
	t.Helper()
	rollbackExternalAgentOAuth(t, url)
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("rollback creative-asset-model", err)
	}
}

const modelAssetInsert = `INSERT INTO creative_assets(id,account_id,kind,title,normalized_title,description,content_id,content_revision_id)
VALUES('asset-model','acct','model','柱','柱','','ccnt_model','ccrv_model')`

func TestCreativeAssetModelMigrationAddsKindAndRefusesLossyRollback(t *testing.T) {
	url := startPostgres(t)
	if err := store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	// The clean rollback runs first: a refused one leaves the version dirty.
	rollbackCreativeAssetModel(t, url)
	if _, err = db.Exec(modelAssetInsert); err == nil {
		t.Fatal("model asset accepted before the migration")
	}
	if err = store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(modelAssetInsert); err != nil {
		t.Fatal("model asset rejected", err)
	}
	rollbackExternalAgentOAuth(t, url)
	if err = store.MigrateDownOneForTest(url); err == nil || !strings.Contains(err.Error(), "model assets exist") {
		t.Fatal("lossy model asset rollback allowed", err)
	}
}
