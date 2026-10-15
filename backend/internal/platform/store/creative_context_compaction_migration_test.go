package store_test

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// rollbackContextCompaction undoes the newer migrations first, then this one;
// every older migration's rollback helper starts here.
func rollbackContextCompaction(t *testing.T, url string) {
	t.Helper()
	rollbackPlatformSkillOwner(t, url)
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("rollback agent-context-compaction", err)
	}
}

// A checkpoint is owned by its account and conversation, at most one per run,
// and a summary is never empty. Rolling back with checkpoints present would
// silently drop what later runs were told, so it is refused.
func TestContextCompactionMigrationKeepsCheckpointsOwned(t *testing.T) {
	url := startPostgres(t)
	if err := store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	const conversation = "ccco_00000000-0000-0000-0000-000000000001"
	if _, err = db.Exec(`INSERT INTO accounts(id,password_hash,status) VALUES('compact-a','test','active'),('compact-b','test','active')`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO creative_agent_conversations(id,account_id,project_id,canvas_id,title) VALUES($1,'compact-a','p','c','测试')`, conversation); err != nil {
		t.Fatal(err)
	}
	insert := func(account, id, run, summary string) error {
		_, err := db.Exec(`INSERT INTO creative_agent_compactions(account_id,id,conversation_id,run_id,covers_through_ordinal,summary,trigger,model_key,tokens_before,tokens_after,created_at)
 VALUES($1,$2,$3,$4,5,$5,'auto','m',100,10,now())`, account, id, conversation, run, summary)
		return err
	}
	if err = insert("compact-a", "cccp_00000000-0000-0000-0000-000000000001", "run-1", "摘要"); err != nil {
		t.Fatal(err)
	}
	if insert("compact-a", "cccp_00000000-0000-0000-0000-000000000002", "run-1", "摘要") == nil {
		t.Fatal("a second checkpoint for one run was accepted")
	}
	if insert("compact-a", "cccp_00000000-0000-0000-0000-000000000003", "run-2", "") == nil {
		t.Fatal("an empty summary was accepted")
	}
	if insert("compact-b", "cccp_00000000-0000-0000-0000-000000000004", "run-3", "摘要") == nil {
		t.Fatal("a checkpoint was attached to another account's conversation")
	}
	if _, err = db.Exec(`UPDATE creative_agent_compactions SET invalidated_at=now()`); err == nil {
		t.Fatal("an invalidation without a reason was accepted")
	}
	rollbackPlatformSkillOwner(t, url)
	if err := store.MigrateDownOneForTest(url); err == nil || !strings.Contains(err.Error(), "must be retained") {
		t.Fatal("lossy rollback", err)
	}
}

func TestContextCompactionEmptyMigrationRollsBackAndReapplies(t *testing.T) {
	url := startPostgres(t)
	if err := store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	rollbackContextCompaction(t, url)
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var present bool
	if err := db.QueryRow(`SELECT to_regclass('creative_agent_compactions') IS NOT NULL`).Scan(&present); err != nil || present {
		t.Fatal("compaction table survived rollback", err)
	}
	if err := store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
}
