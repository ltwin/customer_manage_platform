package store_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// rollbackSceneJobs undoes 0077 through 0075; every older migration's
// rollback helper undoes this one first.
func rollbackSceneJobs(t *testing.T, url string) {
	t.Helper()
	rollbackCheckpointPayloadCap(t, url)
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("rollback creative-scene-jobs", err)
	}
}

const sceneJobInsert = `INSERT INTO creative_scene_jobs(id,account_id,document_id,kind,request,deadline)
VALUES('csjob_00000000-0000-4000-8000-000000000001','acct','cwdoc_x','render_view','{"preset":"overview","mode":"clay"}',now()+interval '30 minutes')`

const waitingClientRun = `INSERT INTO creative_agent_runs(id,account_id,conversation_id,canvas_id,trigger_message_id,egress_consent_id,model_key,model_snapshot,state,claim_token,limits_version,limits_snapshot,deadline_at,waiting_token)
VALUES('ccrn_00000000-0000-4000-8000-000000000001','acct','c','canvas','m','consent','model','{}','waiting_client','token','v','{}',now()+interval '1 hour','wait')`

func TestSceneJobsMigrationAddsTableAndRefusesLossyRollback(t *testing.T) {
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
	rollbackSceneJobs(t, url)
	if _, err = db.Exec(sceneJobInsert); err == nil {
		t.Fatal("scene job accepted before the migration")
	}
	if err = store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(sceneJobInsert); err != nil {
		t.Fatal("scene job rejected", err)
	}
	if _, err = db.Exec(waitingClientRun); err != nil {
		t.Fatal("waiting_client run rejected", err)
	}
	rollbackCheckpointPayloadCap(t, url)
	if err = store.MigrateDownOneForTest(url); err == nil || !strings.Contains(err.Error(), "waiting on client jobs") {
		t.Fatal("lossy rollback allowed", err)
	}
}

// A claim is a token, a tab and a lease together; a result means terminal.
func TestSceneJobsRowInvariants(t *testing.T) {
	url := startPostgres(t)
	if err := store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err = db.Exec(sceneJobInsert); err != nil {
		t.Fatal(err)
	}
	const id = "csjob_00000000-0000-4000-8000-000000000001"
	bad := map[string]string{
		"claimed without token":    `UPDATE creative_scene_jobs SET state='claimed' WHERE id=$1`,
		"token without lease":      `UPDATE creative_scene_jobs SET state='claimed', claim_token='t', editor_session_id='tab_0123456789abcdef' WHERE id=$1`,
		"terminal without result":  `UPDATE creative_scene_jobs SET state='succeeded' WHERE id=$1`,
		"result while queued":      `UPDATE creative_scene_jobs SET result='{}' WHERE id=$1`,
		"run without tool call":    `UPDATE creative_scene_jobs SET run_id='ccrn_x' WHERE id=$1`,
		"unknown kind":             `UPDATE creative_scene_jobs SET kind='bake' WHERE id=$1`,
		"negative attempt":         `UPDATE creative_scene_jobs SET attempt=-1 WHERE id=$1`,
		"request is not an object": `UPDATE creative_scene_jobs SET request='[]' WHERE id=$1`,
	}
	for name, q := range bad {
		if _, err := db.Exec(q, id); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if _, err := db.Exec(`UPDATE creative_scene_jobs SET state='claimed', claim_token='t', editor_session_id='tab_0123456789abcdef', lease_until=now()+interval '30 seconds' WHERE id=$1`, id); err != nil {
		t.Fatal("valid claim rejected", err)
	}
	dup := `INSERT INTO creative_scene_jobs(id,account_id,document_id,kind,request,deadline,run_id,tool_call_id)
VALUES($1,'acct','cwdoc_x','bake_model','{}',now()+interval '30 minutes','ccrn_r','step-1')`
	if _, err := db.Exec(dup, "csjob_00000000-0000-4000-8000-000000000002"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(dup, "csjob_00000000-0000-4000-8000-000000000003"); err == nil {
		t.Fatal("a second job for the same tool call accepted")
	}
}

// Other tabs hear about a new job, one back in the queue and a finished one;
// claiming and renewing the lease stay silent.
func TestSceneJobsNotifyOnlyVisibleChanges(t *testing.T) {
	url := startPostgres(t)
	if err := store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	listener, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close(context.Background()) }()
	if _, err = listener.Exec(ctx, "LISTEN creative_realtime"); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	notified := func() bool {
		t.Helper()
		wait, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
		defer cancel()
		n, err := listener.WaitForNotification(wait)
		if errors.Is(err, context.DeadlineExceeded) {
			return false
		}
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(n.Payload, `"Kind" : "canvas"`) || !strings.Contains(n.Payload, `"Account" : "acct"`) {
			t.Fatalf("payload %s", n.Payload)
		}
		return true
	}
	const id = "csjob_00000000-0000-4000-8000-000000000001"
	steps := []struct {
		name   string
		query  string
		notify bool
	}{
		{"created", sceneJobInsert, true},
		{"claimed", `UPDATE creative_scene_jobs SET state='claimed', claim_token='t', editor_session_id='tab_0123456789abcdef', lease_until=now()+interval '30 seconds' WHERE id='` + id + `'`, false},
		{"lease renewed", `UPDATE creative_scene_jobs SET lease_until=now()+interval '60 seconds', updated_at=clock_timestamp() WHERE id='` + id + `'`, false},
		{"back in the queue", `UPDATE creative_scene_jobs SET state='queued', attempt=1, claim_token=NULL, editor_session_id=NULL, lease_until=NULL WHERE id='` + id + `'`, true},
		{"cancelled", `UPDATE creative_scene_jobs SET state='cancelled', result='{"error":{"code":"cancelled"}}' WHERE id='` + id + `'`, true},
		{"deleted", `DELETE FROM creative_scene_jobs WHERE id='` + id + `'`, false},
	}
	for _, s := range steps {
		if _, err := db.Exec(s.query); err != nil {
			t.Fatal(s.name, err)
		}
		if got := notified(); got != s.notify {
			t.Fatalf("%s: notified=%v, want %v", s.name, got, s.notify)
		}
	}
}
