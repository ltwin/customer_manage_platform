package store_test

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// rollbackCreativeMaterialTextures is the head of the rollback chain: every
// older migration's rollback helper undoes this one first.
func rollbackCreativeMaterialTextures(t *testing.T, url string) {
	t.Helper()
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("rollback creative-material-textures", err)
	}
}

const (
	materialContentInsert = `INSERT INTO creative_contents(id,account_id,kind,created_by_kind) VALUES('ccnt_mat','acct','material','photographer')`
	materialRowsInsert    = `INSERT INTO creative_contents(id,account_id,kind,created_by_kind) VALUES('ccnt_img','acct','image','photographer');
INSERT INTO creative_content_revisions(id,account_id,content_id,sequence,schema_version,payload,rights_declaration_id) VALUES
 ('ccrv_mat','acct','ccnt_mat',1,1,'{"material":{}}','rights'),
 ('ccrv_img','acct','ccnt_img',1,1,'{}','rights')`
	materialRefInsert   = `INSERT INTO creative_content_revision_refs(account_id,holder_revision_id,content_revision_id,role) VALUES('acct','ccrv_mat','ccrv_img','texture')`
	materialAssetInsert = `INSERT INTO creative_assets(id,account_id,kind,title,normalized_title,description,content_id,content_revision_id)
VALUES('asset-mat','acct','material','砖墙','砖墙','','ccnt_mat','ccrv_mat')`
)

func openMaterialTextures(t *testing.T) (string, *sql.DB) {
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
	return url, db
}

func TestCreativeMaterialTexturesMigrationAddsKindAndHolderTable(t *testing.T) {
	url, db := openMaterialTextures(t)
	// The clean rollback runs first: a refused one leaves the version dirty.
	rollbackCreativeMaterialTextures(t, url)
	if _, err := db.Exec(materialContentInsert); err == nil {
		t.Fatal("material content accepted before the migration")
	}
	if _, err := db.Exec(`SELECT 1 FROM creative_content_revision_refs`); err == nil {
		t.Fatal("holder table survived rollback")
	}
	if err := store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{materialContentInsert, materialRowsInsert, materialRefInsert, materialAssetInsert} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(q, err)
		}
	}

	rejected := []struct{ name, query string }{
		{"unknown role", `INSERT INTO creative_content_revision_refs(account_id,holder_revision_id,content_revision_id,role) VALUES('acct','ccrv_img','ccrv_mat','thumbnail')`},
		{"same pair twice", materialRefInsert},
		{"holder that does not exist", `INSERT INTO creative_content_revision_refs(account_id,holder_revision_id,content_revision_id,role) VALUES('acct','ccrv_gone','ccrv_img','texture')`},
		{"held revision that does not exist", `INSERT INTO creative_content_revision_refs(account_id,holder_revision_id,content_revision_id,role) VALUES('acct','ccrv_mat','ccrv_gone','texture')`},
		{"held revision of another account", `INSERT INTO creative_content_revision_refs(account_id,holder_revision_id,content_revision_id,role) VALUES('other','ccrv_mat','ccrv_img','texture')`},
		{"revision holding itself", `INSERT INTO creative_content_revision_refs(account_id,holder_revision_id,content_revision_id,role) VALUES('acct','ccrv_img','ccrv_img','texture')`},
	}
	for _, r := range rejected {
		if _, err := db.Exec(r.query); err == nil {
			t.Fatalf("%s: accepted", r.name)
		}
	}

	// The image cannot be removed from under its holder; the holder's rows go
	// with the holder.
	if _, err := db.Exec(`DELETE FROM creative_content_revisions WHERE id='ccrv_img'`); err == nil {
		t.Fatal("held revision deleted while its holder exists")
	}
	if _, err := db.Exec(`DELETE FROM creative_assets WHERE id='asset-mat'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM creative_content_revisions WHERE id='ccrv_mat'`); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM creative_content_revision_refs`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("holder rows outlived the holder: %d %v", n, err)
	}
}

// Each of these alone would be lost by a rollback, so each alone refuses it.
func TestCreativeMaterialTexturesMigrationRefusesLossyRollback(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup []string
		clear string
	}{
		{"material content", []string{materialContentInsert}, ""},
		{"material asset", []string{materialContentInsert, materialRowsInsert, materialAssetInsert}, `UPDATE creative_contents SET kind='image' WHERE id='ccnt_mat'`},
		// A library model copy holds its textures without any material row.
		{"holder row", []string{materialContentInsert, materialRowsInsert, materialRefInsert}, `UPDATE creative_contents SET kind='model' WHERE id='ccnt_mat'`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			url, db := openMaterialTextures(t)
			for _, q := range tc.setup {
				if _, err := db.Exec(q); err != nil {
					t.Fatal(q, err)
				}
			}
			if tc.clear != "" {
				if _, err := db.Exec(tc.clear); err != nil {
					t.Fatal(err)
				}
			}
			if err := store.MigrateDownOneForTest(url); err == nil || !strings.Contains(err.Error(), "material content or texture holders exist") {
				t.Fatal("lossy rollback allowed", err)
			}
		})
	}
}
