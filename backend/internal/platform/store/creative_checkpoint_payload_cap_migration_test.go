package store_test

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// rollbackCheckpointPayloadCap is the head of the rollback chain: every older
// migration's rollback helper undoes this one first.
func rollbackCheckpointPayloadCap(t *testing.T, url string) {
	t.Helper()
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("rollback creative-checkpoint-payload-cap", err)
	}
}

// A 2 MiB payload: over the old 1 MiB cap, under the new 16 MiB one.
const largeCheckpointInsert = `INSERT INTO creative_agent_checkpoints(id,account_id,run_id,runtime_version,serializer_version,registry_digest,skill_catalog_digest,execution_epoch,revision,model_step_id,journal,payload,payload_digest,created_at,updated_at,retained_until)
VALUES('cccp_00000000-0000-4000-8000-000000000001','acct','ccrn_x','r','s','g','k',1,1,'ccst_x','[]',decode(repeat('00',2097152),'hex'),repeat('0',64),now(),now(),now()+interval '1 day')`

func TestCheckpointPayloadCapMigrationRaisesCapAndRefusesLossyRollback(t *testing.T) {
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
	rollbackCheckpointPayloadCap(t, url)
	if _, err = db.Exec(largeCheckpointInsert); err == nil {
		t.Fatal("checkpoint above 1 MiB accepted before the migration")
	}
	if err = store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(largeCheckpointInsert); err != nil {
		t.Fatal("checkpoint above 1 MiB rejected", err)
	}
	if err = store.MigrateDownOneForTest(url); err == nil || !strings.Contains(err.Error(), "checkpoints above 1 MiB exist") {
		t.Fatal("lossy rollback allowed", err)
	}
}
