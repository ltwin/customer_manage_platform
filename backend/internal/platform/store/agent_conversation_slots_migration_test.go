package store_test

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// rollbackAgentConversationSlots undoes 0079 and then 0078; every older
// migration's rollback helper undoes this one first.
func rollbackAgentConversationSlots(t *testing.T, url string) {
	t.Helper()
	rollbackCreativeAssetModel(t, url)
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("rollback agent-conversation-slots", err)
	}
}

func TestAgentConversationSlotsMigrationMovesOccupancyAndRefusesLossyRollback(t *testing.T) {
	url := startPostgres(t)
	if err := store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	run := func(id, conversation string) {
		t.Helper()
		if _, err := db.Exec(`INSERT INTO creative_agent_runs(id,account_id,conversation_id,canvas_id,trigger_message_id,
 egress_consent_id,model_key,model_snapshot,state,claim_token,limits_version,limits_snapshot,deadline_at)
VALUES($1,'acct-a',$2,'cccv_a',$1,'ccec_a','m','{}','queued','tok-'||$1,'v1','{}',now()+interval '5 minutes')`, id, conversation); err != nil {
			t.Fatal(err)
		}
	}
	const first, second = "ccrn_00000000-0000-0000-0000-000000000001", "ccrn_00000000-0000-0000-0000-000000000002"
	holders := func() map[string]string {
		t.Helper()
		rows, err := db.Query(`SELECT account_id||'/'||conversation_id, coalesce(run_id,'') FROM creative_agent_slots`)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = rows.Close() }()
		got := map[string]string{}
		for rows.Next() {
			var key, holder string
			if err := rows.Scan(&key, &holder); err != nil {
				t.Fatal(err)
			}
			got[key] = holder
		}
		return got
	}

	// Before: one row per account, one occupied by a run, one free.
	rollbackAgentConversationSlots(t, url)
	run(first, "ccvs_a1")
	if _, err = db.Exec(`INSERT INTO creative_agent_slots(account_id,run_id,claim_token,acquired_at) VALUES('acct-a',$1,'tok-'||$1,now())`, first); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO creative_agent_slots(account_id) VALUES('acct-b')`); err != nil {
		t.Fatal(err)
	}
	if err = store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	if got := holders(); len(got) != 1 || got["acct-a/ccvs_a1"] != first {
		t.Fatalf("after up: %v; the occupancy must move to its run's conversation and free rows go", got)
	}
	if _, err = db.Exec(`INSERT INTO creative_agent_slots(account_id,conversation_id,run_id,claim_token,acquired_at) VALUES('acct-a','ccvs_a3',$1,'other',now())`, first); err == nil {
		t.Fatal("one run held two slots")
	}

	// A free second conversation folds away; the occupied row survives.
	if _, err = db.Exec(`INSERT INTO creative_agent_slots(account_id,conversation_id) VALUES('acct-a','ccvs_a2')`); err != nil {
		t.Fatal(err)
	}
	rollbackAgentConversationSlots(t, url)
	var holder string
	if err = db.QueryRow(`SELECT run_id FROM creative_agent_slots WHERE account_id='acct-a'`).Scan(&holder); err != nil || holder != first {
		t.Fatalf("after down: %q %v", holder, err)
	}

	// Two occupied conversations have no single row to fold into.
	if err = store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	run(second, "ccvs_a2")
	if _, err = db.Exec(`INSERT INTO creative_agent_slots(account_id,conversation_id,run_id,claim_token,acquired_at) VALUES('acct-a','ccvs_a2',$1,'tok-'||$1,now())`, second); err != nil {
		t.Fatal(err)
	}
	rollbackCreativeAssetModel(t, url)
	err = store.MigrateDownOneForTest(url)
	if err == nil || !strings.Contains(err.Error(), "more than one agent write slot") {
		t.Fatalf("lossy rollback was not refused: %v", err)
	}
}
