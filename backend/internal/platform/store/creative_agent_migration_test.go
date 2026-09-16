package store_test

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// TestAgentConversationMigrationShapeAndLossyRollback pins the 0044 shape and
// proves a rollback can never silently drop the photographer's reading history
// or the evidence of what they authorised to leave the account.
func TestAgentConversationMigrationShapeAndLossyRollback(t *testing.T) {
	url := startPostgres(t)
	if err := store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	tables := []string{
		"creative_agent_conversations", "creative_agent_messages", "creative_message_content_refs",
		"creative_egress_consents", "creative_egress_consent_contents",
	}
	// Every table in this namespace is business data, so every one of them
	// carries an explicit account key: there is no shared infrastructure row here.
	for _, table := range tables {
		var scoped bool
		if err := db.QueryRow(
			`SELECT EXISTS(SELECT 1 FROM information_schema.columns
			 WHERE table_schema='public' AND table_name=$1 AND column_name='account_id')`,
			table).Scan(&scoped); err != nil || !scoped {
			t.Fatalf("table %s is missing or unscoped: %v", table, err)
		}
	}

	// The empty rollback runs first: a refused one leaves the schema version
	// dirty, so the recoverable case has to be proved before the refusal.
	// 0045 (skill foundation) and 0046 (agent runs) sit above this migration;
	// step past both first.
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("rollback agent-runs migration: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("rollback skill-foundation migration: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("rollback with no history must succeed: %v", err)
	}
	for _, table := range tables {
		var exists bool
		if err := db.QueryRow(
			`SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_schema='public' AND table_name=$1)`,
			table).Scan(&exists); err != nil || exists {
			t.Fatalf("table %s survived the rollback: %v", table, err)
		}
	}
	if err := store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}

	if _, err := db.Exec(`INSERT INTO accounts(id,password_hash,status) VALUES('mig-agent','test','active');
		INSERT INTO creative_agent_conversations(id,account_id,project_id,canvas_id,title)
		VALUES('ccco_00000000-0000-4000-8000-000000000001','mig-agent','p','c','会话')`); err != nil {
		t.Fatal(err)
	}
	message := func(id, ordinal, role, step string) error {
		_, err := db.Exec(`INSERT INTO creative_agent_messages
			(id,account_id,conversation_id,ordinal,role,status,schema_version,body,source_step_id)
			VALUES($1,'mig-agent','ccco_00000000-0000-4000-8000-000000000001',$2,$3,'complete',1,'{}'::jsonb,$4)`,
			id, ordinal, role, sql.NullString{String: step, Valid: step != ""})
		return err
	}
	if err := message("ccms_00000000-0000-4000-8000-000000000001", "1", "user", ""); err != nil {
		t.Fatal(err)
	}
	if err := message("ccms_00000000-0000-4000-8000-000000000002", "1", "assistant", ""); err == nil {
		t.Fatal("two messages must never share one position in a conversation")
	}
	if err := message("ccms_00000000-0000-4000-8000-000000000003", "2", "assistant", "ccst_x"); err != nil {
		t.Fatal(err)
	}
	if err := message("ccms_00000000-0000-4000-8000-000000000004", "3", "assistant", "ccst_x"); err == nil {
		t.Fatal("one step result must project into one message per role")
	}
	// A different role may still project the same step, which is how a tool
	// answer and the assistant's words stay two readable records.
	if err := message("ccms_00000000-0000-4000-8000-000000000005", "4", "tool", "ccst_x"); err != nil {
		t.Fatal(err)
	}

	consent := func(id, vendor string) error {
		_, err := db.Exec(`INSERT INTO creative_egress_consents
			(id,account_id,vendor_key,purpose,scope,policy_version)
			VALUES($1,'mig-agent',$2,'creative_assistance','{"mode":"account_library","data_classes":["text"]}'::jsonb,'p-1')`, id, vendor)
		return err
	}
	if err := consent("ccec_00000000-0000-4000-8000-000000000001", "deepseek"); err != nil {
		t.Fatal(err)
	}
	// A vendor key is disclosed to the photographer, so it stays a plain token
	// and can never be a URL that points somewhere else.
	if err := consent("ccec_00000000-0000-4000-8000-000000000002", "https://elsewhere.invalid"); err == nil {
		t.Fatal("a vendor key must not accept an endpoint")
	}
	// The stored scope has to satisfy the published contract: a mode the API
	// declares and at least one data class. Otherwise a row could authorise
	// "everything" by saying nothing.
	for _, scope := range []string{
		`{"mode":"whatever","data_classes":["text"]}`,
		`{"mode":"account_library"}`,
		`{"mode":"account_library","data_classes":[]}`,
		// A scalar where an array belongs: jsonb_array_length raises on this
		// rather than returning NULL, so the CHECK must not reach it.
		`{"mode":"account_library","data_classes":"text"}`,
		`null`, `["text"]`, `"text"`,
	} {
		if _, err := db.Exec(`INSERT INTO creative_egress_consents
			(id,account_id,vendor_key,purpose,scope,policy_version)
			VALUES('ccec_00000000-0000-4000-8000-000000000003','mig-agent','deepseek','creative_assistance',$1::jsonb,'p-1')`,
			scope); err == nil {
			t.Fatalf("scope %s must be refused", scope)
		}
	}

	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("rollback agent-runs migration: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("rollback skill-foundation migration: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err == nil {
		t.Fatal("a rollback that would drop conversation history must be refused")
	} else if !strings.Contains(err.Error(), "export") {
		t.Fatalf("the refusal must say what to do first: %v", err)
	}
}
