package store_test

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// rollbackModelContent undoes 0075 and then 0074; every older migration's
// rollback helper undoes this one first.
func rollbackModelContent(t *testing.T, url string) {
	t.Helper()
	rollbackSceneJobs(t, url)
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("rollback creative-model-content", err)
	}
}

const modelUploadInsert = `INSERT INTO creative_uploads(id,account_id,operation_id,rights_declaration_id,publish_operation_id,declared_kind,declared_mime,declared_size,reserved_bytes,original_name,staging_key,expires_at,target_kind,target_snapshot)
VALUES('up-model','acct',gen_random_uuid(),'decl',gen_random_uuid(),'model','model/gltf-binary',8,8,'m.glb','stage/up-model',now()+interval '1 day','staged','{"kind":"staged"}')`

func TestModelContentMigrationAddsKindsAndRefusesLossyRollback(t *testing.T) {
	url := startPostgres(t)
	if err := store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err = db.Exec(`INSERT INTO accounts(id,password_hash,status) VALUES('acct','test','active')`); err != nil {
		t.Fatal(err)
	}
	inserts := []string{
		`INSERT INTO creative_contents(id,account_id,kind,created_by_kind) VALUES('ccnt_model','acct','model','photographer')`,
		modelUploadInsert,
		`INSERT INTO creative_upload_candidates(id,account_id,upload_id,state,target_snapshot,declared_kind,expires_at,adopted_target_kind,adopted_target_id,adopted_target_revision)
VALUES('cand-doc','acct','up-model','applied','{}','model',now()+interval '1 day','document','cwdoc_x',2)`,
	}

	// The clean rollback runs first: a refused one leaves the version dirty.
	rollbackModelContent(t, url)
	for _, q := range inserts[:2] {
		if _, err = db.Exec(q); err == nil {
			t.Fatal("model value accepted before the migration", q)
		}
	}
	if err = store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	for _, q := range inserts {
		if _, err = db.Exec(q); err != nil {
			t.Fatal("model value rejected", q, err)
		}
	}
	rollbackSceneJobs(t, url)
	if err = store.MigrateDownOneForTest(url); err == nil || !strings.Contains(err.Error(), "model content exists") {
		t.Fatal("lossy model rollback allowed", err)
	}
}

// Each kind of model data blocks the rollback on its own.
func TestModelContentRollbackRefusesEachKindOfModelData(t *testing.T) {
	tests := []struct {
		name string
		seed string
	}{
		{"content", `INSERT INTO creative_contents(id,account_id,kind,created_by_kind) VALUES('ccnt_model','acct','model','photographer')`},
		{"upload", modelUploadInsert},
		{"document candidate", `INSERT INTO creative_uploads(id,account_id,operation_id,rights_declaration_id,publish_operation_id,declared_kind,declared_mime,declared_size,reserved_bytes,original_name,staging_key,expires_at,target_kind,target_snapshot)
VALUES('up-png','acct',gen_random_uuid(),'decl',gen_random_uuid(),'image','image/png',8,8,'t.png','stage/up-png',now()+interval '1 day','staged','{"kind":"staged"}');
INSERT INTO creative_upload_candidates(id,account_id,upload_id,state,target_snapshot,declared_kind,expires_at,adopted_target_kind,adopted_target_id,adopted_target_revision)
VALUES('cand-doc','acct','up-png','applied','{}','image',now()+interval '1 day','document','cwdoc_x',2)`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := startPostgres(t)
			if err := store.MigrateUp(url); err != nil {
				t.Fatal(err)
			}
			db, err := sql.Open("pgx", url)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = db.Close() }()
			if _, err = db.Exec(`INSERT INTO accounts(id,password_hash,status) VALUES('acct','test','active');` + tt.seed); err != nil {
				t.Fatal(err)
			}
			rollbackSceneJobs(t, url)
			if err = store.MigrateDownOneForTest(url); err == nil || !strings.Contains(err.Error(), "model content exists") {
				t.Fatal("lossy model rollback allowed", err)
			}
		})
	}
}
