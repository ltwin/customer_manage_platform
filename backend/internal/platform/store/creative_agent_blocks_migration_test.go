package store_test

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// rollbackAgentBlocks steps past 0056; every chain that walks below it starts
// here.
func rollbackAgentBlocks(t *testing.T, url string) {
	t.Helper()
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("rollback creative-agent-blocks", err)
	}
}

func TestAgentBlocksMigrationRefusesLossyRollback(t *testing.T) {
	url := startPostgres(t)
	if err := store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err = db.Exec(`INSERT INTO creative_agent_blocks(id,account_id,run_id,ordinal,type,state,source_key,payload)
VALUES('ccbk_00000000-0000-0000-0000-000000000001','a','ccrn_x',1,'text','complete','step:s:text','{"markdown":"hi"}')`); err != nil {
		t.Fatal(err)
	}
	// Unknown shapes are the registry's job, but the table still refuses rows
	// that cannot be a block at all.
	if _, err = db.Exec(`INSERT INTO creative_agent_blocks(id,account_id,run_id,ordinal,type,state,source_key,payload)
VALUES('ccbk_00000000-0000-0000-0000-000000000002','a','ccrn_x',2,'text','complete','k2','[]')`); err == nil {
		t.Fatal("non-object payload accepted")
	}
	if err = store.MigrateDownOneForTest(url); err == nil || !strings.Contains(err.Error(), "agent blocks exist") {
		t.Fatal("lossy agent blocks rollback allowed", err)
	}
	// 拒绝回滚会把版本标脏，干净回滚换一个库验证：画布通知回到旧 channel。
	clean := startPostgres(t)
	if err = store.MigrateUp(clean); err != nil {
		t.Fatal(err)
	}
	rollbackAgentBlocks(t, clean)
	old, err := sql.Open("pgx", clean)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = old.Close() }()
	var body string
	if err = old.QueryRow(`SELECT prosrc FROM pg_proc WHERE proname='notify_creative_canvas_changed'`).Scan(&body); err != nil || !strings.Contains(body, "creative_canvas_changed") {
		t.Fatal("canvas notify channel not restored", err)
	}
	if err = store.MigrateUp(clean); err != nil {
		t.Fatal(err)
	}
}
