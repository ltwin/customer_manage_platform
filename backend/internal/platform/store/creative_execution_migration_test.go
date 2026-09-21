package store_test

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func TestExecutionContinuationMigrationPreservesRecoveryEvidence(t *testing.T) {
	url := startPostgres(t)
	if err := store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("generation media facts rollback", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err = db.Exec(`INSERT INTO creative_node_executions(id,account_id,canvas_id,node_id,action_key,executor_version,operation_id,request_hash,state,deadline,target_data_revision,draft_revision,prompt_snapshot,read_set,caller_kind,publish_operation_id,result_operation_id) VALUES('e','a','c','n','internal.text-compose','1','o','hash','queued',now()+interval '1 hour',1,1,'{}','[]','photographer','p','r')`); err != nil {
		t.Fatal(err)
	}
	if err = store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	var token string
	var absent bool
	if err = db.QueryRow(`SELECT resume_token,result_payload IS NULL AND next_step_at IS NULL FROM creative_node_executions WHERE id='e'`).Scan(&token, &absent); err != nil || token != "" || !absent {
		t.Fatalf("existing execution changed: %q %v %v", token, absent, err)
	}
	if _, err = db.Exec(`UPDATE creative_node_executions SET result_payload='{"body":"retained"}' WHERE id='e'`); err != nil {
		t.Fatal(err)
	}
	if err = store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("generation media facts rollback", err)
	}
	if err = store.MigrateDownOneForTest(url); err == nil || !strings.Contains(err.Error(), "export") {
		t.Fatalf("lossy rollback: %v", err)
	}
}
