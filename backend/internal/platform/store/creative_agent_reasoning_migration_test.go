package store_test

import (
	"database/sql"
	"fmt"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func rollbackAgentReasoning(t *testing.T, url string) {
	t.Helper()
	rollbackChatFailover(t, url)
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("rollback creative-agent-reasoning", err)
	}
}

func TestAgentReasoningOnlyBelongsToAssistantTurns(t *testing.T) {
	url := startPostgres(t)
	if err := store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	ordinal := 0
	insert := func(role string, reasoning any) error {
		ordinal++
		_, err := db.Exec(`INSERT INTO creative_agent_messages(id,account_id,conversation_id,ordinal,role,status,schema_version,body,reasoning)
VALUES($1,'a','ccco_00000000-0000-0000-0000-000000000001',$2,$3,'complete',2,'{"blocks":[]}',$4)`,
			fmt.Sprintf("ccms_00000000-0000-0000-0000-%012d", ordinal), ordinal, role, reasoning)
		return err
	}
	if err = insert("assistant", "先看画布"); err != nil {
		t.Fatal(err)
	}
	if err = insert("user", "不该有"); err == nil {
		t.Fatal("reasoning accepted on a user message")
	}
	if err = insert("assistant", ""); err == nil {
		t.Fatal("empty reasoning accepted; absent reasoning is NULL")
	}
	rollbackAgentReasoning(t, url)
	if err = store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
}
