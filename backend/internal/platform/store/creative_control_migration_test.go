package store_test

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func TestAgentControlMigrationRefusesLossyRollback(t *testing.T) {
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
	_, err = db.Exec(`INSERT INTO creative_agent_runs(id,account_id,conversation_id,canvas_id,trigger_message_id,egress_consent_id,model_key,model_snapshot,state,claim_token,limits_version,limits_snapshot,deadline_at,waiting_token)
 VALUES('ccrn_00000000-0000-4000-8000-000000000001','a','c','canvas','m','consent','model','{}','waiting_input','token','v','{}',now()+interval '1 hour','wait')`)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("generation media facts rollback", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("execution continuation rollback", err)
	}
	if err = store.MigrateDownOneForTest(url); err == nil || !strings.Contains(err.Error(), "export") {
		t.Fatalf("lossy rollback: %v", err)
	}
}
