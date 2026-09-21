package store_test

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func TestAgentCheckpointMigrationPreservesRecoveryEvidence(t *testing.T) {
	url := startPostgres(t)
	if err := store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("generation media facts rollback", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("execution continuation rollback", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("control rollback", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal(err)
	}
	if err := store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	for _, table := range []string{"creative_agent_checkpoints", "creative_checkpoint_content_refs"} {
		var n int
		if err = db.QueryRow(`SELECT count(*) FROM information_schema.columns WHERE table_name=$1 AND column_name='account_id'`, table).Scan(&n); err != nil || n != 1 {
			t.Fatalf("scope %s: %v", table, err)
		}
	}
	if _, err = db.Exec(`INSERT INTO creative_agent_checkpoints(id,account_id,run_id,runtime_version,serializer_version,registry_digest,skill_catalog_digest,execution_epoch,revision,model_step_id,journal,payload,payload_digest,created_at,updated_at,retained_until)
 VALUES('cccp_00000000-0000-4000-8000-000000000001','a','r','eino','gob',repeat('a',64),repeat('b',64),1,1,'m','[]','payload',repeat('c',64),now(),now(),now()+interval '90 days')`); err != nil {
		t.Fatal(err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("generation media facts rollback", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("execution continuation rollback", err)
	}
	if err = store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("control rollback", err)
	}
	if err = store.MigrateDownOneForTest(url); err == nil || !strings.Contains(err.Error(), "export") {
		t.Fatalf("lossy rollback: %v", err)
	}
}
