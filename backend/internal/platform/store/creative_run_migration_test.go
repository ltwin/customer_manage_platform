package store_test

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// TestAgentRunMigrationShapeAndLossyRollback pins the 0046 shape and proves a
// rollback can never silently drop the record of what was sent to a vendor,
// what it produced, and what it may still cost.
func TestAgentRunMigrationShapeAndLossyRollback(t *testing.T) {
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
		"creative_agent_runs", "creative_agent_slots", "creative_run_inputs",
		"creative_run_skill_refs", "creative_agent_steps", "creative_run_events",
	}
	// Every table here is business data, including the slot: one write slot per
	// account is an account fact, not shared infrastructure.
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
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("generation media facts rollback", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("execution continuation rollback", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("agent control rollback", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("agent checkpoint rollback", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("agent context rollback", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("rollback with no runs must succeed: %v", err)
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

	const runID = "ccrn_00000000-0000-4000-8000-000000000001"
	if _, err := db.Exec(`INSERT INTO accounts(id,password_hash,status) VALUES('mig-run','test','active')`); err != nil {
		t.Fatal(err)
	}
	run := func(id, state string, finished any) error {
		_, err := db.Exec(`INSERT INTO creative_agent_runs
			(id,account_id,conversation_id,canvas_id,trigger_message_id,egress_consent_id,
			 model_key,model_snapshot,state,claim_token,limits_version,limits_snapshot,
			 deadline_at,created_at,finished_at)
			VALUES($1,'mig-run','ccco_1','cccv_1',$2,'ccec_1','m','{}'::jsonb,$3,'tok','v1','{}'::jsonb,
			       clock_timestamp()+interval '5 minutes',clock_timestamp(),$4)`,
			id, "ccms_"+id, state, finished)
		return err
	}
	if err := run(runID, "queued", nil); err != nil {
		t.Fatal(err)
	}
	// Terminal and finished are the same fact stated twice; the column pair may
	// never disagree in either direction.
	if err := run("ccrn_00000000-0000-4000-8000-000000000002", "succeeded", nil); err == nil {
		t.Fatal("a terminal run must carry its end time")
	}
	if err := run("ccrn_00000000-0000-4000-8000-000000000003", "running", "now()"); err == nil {
		t.Fatal("a run that is still working must not carry an end time")
	}
	// One run per trigger message: a retried creation replays instead of
	// starting a second run against the same words.
	if _, err := db.Exec(`INSERT INTO creative_agent_runs
		(id,account_id,conversation_id,canvas_id,trigger_message_id,egress_consent_id,
		 model_key,model_snapshot,state,claim_token,limits_version,limits_snapshot,deadline_at)
		VALUES('ccrn_00000000-0000-4000-8000-000000000004','mig-run','ccco_1','cccv_1',$1,'ccec_1',
		       'm','{}'::jsonb,'queued','tok','v1','{}'::jsonb,clock_timestamp()+interval '5 minutes')`,
		"ccms_"+runID); err == nil {
		t.Fatal("two runs must never share one trigger message")
	}
	// Only a run with a live worker holds a lease. A queued one has none, which
	// is what keeps "the slot is held" and "someone is working" separable.
	if _, err := db.Exec(`UPDATE creative_agent_runs SET lease_until=clock_timestamp() WHERE id=$1`, runID); err == nil {
		t.Fatal("a queued run must not hold a worker lease")
	}

	// A half-filled slot is one nobody can release.
	if _, err := db.Exec(`INSERT INTO creative_agent_slots(account_id,run_id) VALUES('mig-run',$1)`, runID); err == nil {
		t.Fatal("an occupied slot must carry its claim token and time")
	}
	if _, err := db.Exec(`INSERT INTO creative_agent_slots(account_id,run_id,claim_token,acquired_at)
		VALUES('mig-run',$1,'tok',clock_timestamp())`, runID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO creative_agent_slots(account_id) VALUES('mig-run')`); err == nil {
		t.Fatal("an account has exactly one write slot")
	}

	// A model step never belongs to another model answer, and one gateway
	// request is claimed by at most one step.
	step := func(id, ordinal, kind, parent, index, request any) error {
		_, err := db.Exec(`INSERT INTO creative_agent_steps
			(id,account_id,run_id,ordinal,kind,state,execution_epoch,input_hash,input,
			 parent_model_step_id,tool_call_index,llm_request_id)
			VALUES($1,'mig-run',$2,$3,$4,'prepared',1,'h','{}'::jsonb,$5,$6,$7)`,
			id, runID, ordinal, kind, parent, index, request)
		return err
	}
	if err := step("ccst_00000000-0000-4000-8000-000000000001", 1, "model", nil, nil, "llmr_1"); err != nil {
		t.Fatal(err)
	}
	if err := step("ccst_00000000-0000-4000-8000-000000000002", 2, "model", nil, nil, "llmr_1"); err == nil {
		t.Fatal("two steps must never claim one gateway request")
	}
	if err := step("ccst_00000000-0000-4000-8000-000000000003", 2, "model",
		"ccst_00000000-0000-4000-8000-000000000001", 0, nil); err == nil {
		t.Fatal("a model step is never a tool call of another step")
	}
	if err := step("ccst_00000000-0000-4000-8000-000000000004", 2, "tool",
		"ccst_00000000-0000-4000-8000-000000000001", 0, nil); err != nil {
		t.Fatal(err)
	}
	if err := step("ccst_00000000-0000-4000-8000-000000000005", 3, "tool",
		"ccst_00000000-0000-4000-8000-000000000001", 0, nil); err == nil {
		t.Fatal("one model answer must expand into one tool call per index")
	}

	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("generation media facts rollback", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("execution continuation rollback", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("agent control rollback", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("agent checkpoint rollback", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("agent context rollback", err)
	}
	if err := store.MigrateDownOneForTest(url); err == nil {
		t.Fatal("a rollback that would drop recorded runs must be refused")
	} else if !strings.Contains(err.Error(), "export") {
		t.Fatalf("the refusal must say what to do first: %v", err)
	}
}
