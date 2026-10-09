package store_test

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// rollbackStagedUploads steps past 0055; every chain that walks below it
// starts here.
func rollbackStagedUploads(t *testing.T, url string) {
	t.Helper()
	rollbackAgentBlocks(t, url)
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("rollback creative-staged-uploads", err)
	}
}

func TestStagedUploadMigrationRefusesLossyRollback(t *testing.T) {
	url := startPostgres(t)
	if err := store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	_, err = db.Exec(`INSERT INTO accounts(id,password_hash,status) VALUES('staged-a','test','active');
INSERT INTO creative_uploads(id,account_id,operation_id,rights_declaration_id,publish_operation_id,declared_kind,declared_mime,declared_size,reserved_bytes,original_name,staging_key,expires_at,target_kind,target_snapshot)
VALUES('up-staged','staged-a',gen_random_uuid(),'decl',gen_random_uuid(),'image','image/png',8,8,'a.png','stage/up-staged',now()+interval '1 day','staged','{"kind":"staged"}')`)
	if err != nil {
		t.Fatal("staged target accepted after 0055", err)
	}
	rollbackAgentBlocks(t, url)
	if err = store.MigrateDownOneForTest(url); err == nil || !strings.Contains(err.Error(), "staged uploads exist") {
		t.Fatal("lossy staged rollback allowed", err)
	}
	// 拒绝回滚会把版本标脏，干净回滚换一个库验证。
	clean := startPostgres(t)
	if err = store.MigrateUp(clean); err != nil {
		t.Fatal(err)
	}
	rollbackStagedUploads(t, clean)
	old, err := sql.Open("pgx", clean)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = old.Close() }()
	if _, err = old.Exec(`INSERT INTO accounts(id,password_hash,status) VALUES('staged-a','test','active');
INSERT INTO creative_uploads(id,account_id,operation_id,rights_declaration_id,publish_operation_id,declared_kind,declared_mime,declared_size,reserved_bytes,original_name,staging_key,expires_at,target_kind,target_snapshot)
VALUES('up-old','staged-a',gen_random_uuid(),'decl',gen_random_uuid(),'image','image/png',8,8,'a.png','stage/up-old',now()+interval '1 day','staged','{"kind":"staged"}')`); err == nil {
		t.Fatal("staged target accepted below 0055")
	}
	if err = store.MigrateUp(clean); err != nil {
		t.Fatal(err)
	}
}
