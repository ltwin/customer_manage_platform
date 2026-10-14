package store_test

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func TestAgentLibraryMigrationRetainsResultsAndAccountOwnership(t *testing.T) {
	url := startPostgres(t)
	if err := store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	_, err = db.Exec(`INSERT INTO accounts(id,password_hash,status) VALUES('journal-a','test','active'),('journal-b','test','active');
 INSERT INTO creative_agent_conversations(id,account_id,project_id,canvas_id,title) VALUES('ccco_00000000-0000-0000-0000-000000000001','journal-a','p','c','测试');
 INSERT INTO creative_agent_library_calls(account_id,id,tool_step_id,run_id,conversation_id,tool_name,protocol_version,arguments_hash,created_at,retained_until,state,plan,progress)
 VALUES('journal-a','result','step','run','ccco_00000000-0000-0000-0000-000000000001','organize_assets',1,'hash',now(),now()+interval '90 days','executing','{"items":[]}','[]')`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE creative_agent_library_calls SET account_id='journal-b'`); err == nil {
		t.Fatal("cross-account conversation accepted")
	}
	if err := store.MigrateDownOneForTest(url); err == nil || !strings.Contains(err.Error(), "must be retained") {
		t.Fatal("lossy rollback", err)
	}
	// A failed migration marks its version dirty. Cascade behavior is independently
	// checkable without forcing that failed migration or dropping saved data.
	if _, err := db.Exec(`DELETE FROM creative_agent_conversations WHERE account_id='journal-a'`); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM creative_agent_library_calls`).Scan(&n); err != nil || n != 0 {
		t.Fatal("conversation cleanup", n, err)
	}
}
func TestAgentLibraryEmptyMigrationCanRollbackAndReapply(t *testing.T) {
	url := startPostgres(t)
	if err := store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal(err)
	}
	if err := store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
}
