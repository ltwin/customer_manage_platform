package store_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const (
	toolTaskSceneJob = `INSERT INTO creative_scene_jobs(id,account_id,document_id,kind,request,deadline)
VALUES('csjob_00000000-0000-4000-8000-0000000000a1','acct','cwdoc_x','bake_model','{}',now()+interval '30 minutes')`
	toolTaskRender = `INSERT INTO creative_image_edit_renders(account_id,id,operation_id,request_hash,canvas_id,node_id,source_revision_id,action,parameters,encoder_policy,caller_kind,state,expires_at)
VALUES('acct','render-1','op-1','h','canvas','node','rev','compress','{"quality":80}','image-edit-v1','agent','queued',clock_timestamp()+interval '2 minutes')`
	toolTaskExecution = `INSERT INTO creative_node_executions(id,account_id,canvas_id,node_id,action_key,executor_version,operation_id,request_hash,state,deadline,target_data_revision,draft_revision,prompt_snapshot,read_set,caller_kind,publish_operation_id,result_operation_id)
VALUES('exec-1','acct','canvas','n','internal.text-compose','1','o','hash','running',now()+interval '1 hour',1,1,'{}','[]','agent','p','r')`
)

// toolTask inserts a running task over source id for tool.
func toolTask(t *testing.T, db *sql.DB, n, tool, source, mode string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO creative_agent_tool_tasks(id,account_id,conversation_id,canvas_id,source_run_id,tool_step_id,tool_key,source_id,mode,model_key)
VALUES($1,'acct','ccco_x','canvas','ccrn_x',$2,$3,$4,$5,'model')`, "cctask_00000000-0000-4000-8000-00000000000"+n, "ccst_"+n, tool, source, mode)
	if err != nil {
		t.Fatal(tool, err)
	}
}

func dueAt(t *testing.T, db *sql.DB, n string) sql.NullTime {
	t.Helper()
	var due sql.NullTime
	if err := db.QueryRow(`SELECT next_check_at FROM creative_agent_tool_tasks WHERE id=$1`, "cctask_00000000-0000-4000-8000-00000000000"+n).Scan(&due); err != nil {
		t.Fatal(err)
	}
	return due
}

// rollbackAgentToolTasks undoes 0084.
func rollbackAgentToolTasks(t *testing.T, url string) {
	t.Helper()
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("rollback creative-agent-tool-tasks", err)
	}
}

func TestCreativeAgentToolTasksMigration(t *testing.T) {
	url, db := openMaterialTextures(t)
	// The clean rollback runs first: a refused one leaves the version dirty.
	rollbackAgentToolTasks(t, url)
	if _, err := db.Exec(`SELECT 1 FROM creative_agent_tool_tasks`); err == nil {
		t.Fatal("task table survived rollback")
	}
	if _, err := db.Exec(`SELECT trigger_kind FROM creative_agent_runs`); err == nil {
		t.Fatal("trigger_kind survived rollback")
	}
	if err := store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}

	for _, q := range []string{toolTaskSceneJob, toolTaskRender, toolTaskExecution} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(q, err)
		}
	}
	toolTask(t, db, "1", "write_model", "csjob_00000000-0000-4000-8000-0000000000a1", "foreground")
	toolTask(t, db, "2", "compress_image", "render-1", "background")
	toolTask(t, db, "3", "generate_node", "exec-1", "foreground")

	// One task per tool step, and per source.
	if _, err := db.Exec(`INSERT INTO creative_agent_tool_tasks(id,account_id,conversation_id,canvas_id,source_run_id,tool_step_id,tool_key,source_id,mode,model_key)
VALUES('cctask_00000000-0000-4000-8000-000000000009','acct','ccco_x','canvas','ccrn_x','ccst_1','generate_node','other','foreground','model')`); err == nil {
		t.Fatal("second task for one tool step accepted")
	}
	if _, err := db.Exec(`INSERT INTO creative_agent_tool_tasks(id,account_id,conversation_id,canvas_id,source_run_id,tool_step_id,tool_key,source_id,mode,model_key)
VALUES('cctask_00000000-0000-4000-8000-000000000009','acct','ccco_x','canvas','ccrn_x','ccst_9','generate_node','exec-1','foreground','model')`); err == nil {
		t.Fatal("second task for one source accepted")
	}
	// Another account's keys do not collide.
	if _, err := db.Exec(`INSERT INTO creative_agent_tool_tasks(id,account_id,conversation_id,canvas_id,source_run_id,tool_step_id,tool_key,source_id,mode,model_key)
VALUES('cctask_00000000-0000-4000-8000-000000000009','other','ccco_x','canvas','ccrn_x','ccst_1','generate_node','exec-1','foreground','model')`); err != nil {
		t.Fatal("another account's task refused", err)
	}
	for _, bad := range []string{
		// render_view has no background form.
		`INSERT INTO creative_agent_tool_tasks(id,account_id,conversation_id,canvas_id,source_run_id,tool_step_id,tool_key,source_id,mode,model_key)
VALUES('cctask_00000000-0000-4000-8000-00000000000a','acct','ccco_x','canvas','ccrn_x','ccst_a','render_view','job-x','background','model')`,
		// A terminal state needs its result and time.
		`UPDATE creative_agent_tool_tasks SET state='succeeded' WHERE id='cctask_00000000-0000-4000-8000-000000000001'`,
		// Foreground tasks carry no notice.
		`UPDATE creative_agent_tool_tasks SET state='succeeded',result='{}',finished_at=now(),notification_state='suppressed' WHERE id='cctask_00000000-0000-4000-8000-000000000001'`,
		// A pending notice names its message.
		`UPDATE creative_agent_tool_tasks SET state='succeeded',result='{}',finished_at=now(),notification_state='pending' WHERE id='cctask_00000000-0000-4000-8000-000000000002'`,
		// A skipped notice has no wake time.
		`UPDATE creative_agent_tool_tasks SET state='succeeded',result='{}',finished_at=now(),notification_state='pending',notification_message_id='m',wake_after=now(),wake_skip_reason='credits' WHERE id='cctask_00000000-0000-4000-8000-000000000002'`,
		`INSERT INTO creative_agent_runs(id,account_id,conversation_id,canvas_id,trigger_message_id,egress_consent_id,model_key,model_snapshot,state,claim_token,limits_version,limits_snapshot,deadline_at,trigger_kind)
VALUES('ccrn_00000000-0000-4000-8000-0000000000b1','acct','c','canvas','m','consent','model','{}','queued','token','v','{}',now()+interval '1 hour','manual')`,
	} {
		if _, err := db.Exec(bad); err == nil {
			t.Fatal("constraint missing for", bad)
		}
	}

	// A source's state change marks its running task due; nothing else does.
	if dueAt(t, db, "1").Valid || dueAt(t, db, "2").Valid || dueAt(t, db, "3").Valid {
		t.Fatal("task due before any source change")
	}
	listener := listenToolTasks(t, url)
	if _, err := db.Exec(`UPDATE creative_scene_jobs SET attempt=attempt+1 WHERE account_id='acct'`); err != nil {
		t.Fatal(err)
	}
	if dueAt(t, db, "1").Valid {
		t.Fatal("a change other than state marked the task due")
	}
	for _, q := range []string{
		`UPDATE creative_scene_jobs SET state='claimed',claim_token='x',editor_session_id='tab',lease_until=now()+interval '1 minute' WHERE account_id='acct'`,
		`UPDATE creative_image_edit_renders SET state='ready' WHERE account_id='acct'`,
		`UPDATE creative_node_executions SET apply_state='applied' WHERE account_id='acct'`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(q, err)
		}
	}
	for _, n := range []string{"1", "2", "3"} {
		if !dueAt(t, db, n).Valid {
			t.Fatalf("task %s not marked due by its source", n)
		}
	}
	listener.expect(t)
	// The other account's task over a same-named source stays untouched.
	var otherDue sql.NullTime
	if err := db.QueryRow(`SELECT next_check_at FROM creative_agent_tool_tasks WHERE account_id='other'`).Scan(&otherDue); err != nil || otherDue.Valid {
		t.Fatalf("another account's task marked due: %v %v", otherDue, err)
	}
	// A finished task is no longer marked.
	if _, err := db.Exec(`UPDATE creative_agent_tool_tasks SET state='succeeded',result='{}',finished_at=now(),next_check_at=NULL WHERE id='cctask_00000000-0000-4000-8000-000000000001'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE creative_scene_jobs SET state='succeeded',claim_token=NULL,editor_session_id=NULL,lease_until=NULL,result='{}' WHERE account_id='acct'`); err != nil {
		t.Fatal(err)
	}
	if dueAt(t, db, "1").Valid {
		t.Fatal("finished task marked due")
	}

	if err := store.MigrateDownOneForTest(url); err == nil || !strings.Contains(err.Error(), "agent tool tasks exist") {
		t.Fatalf("lossy rollback accepted: %v", err)
	}
}

type toolTaskListener struct{ conn *pgx.Conn }

func listenToolTasks(t *testing.T, url string) toolTaskListener {
	t.Helper()
	conn, err := pgx.Connect(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	if _, err = conn.Exec(context.Background(), "LISTEN creative_agent_tool_tasks"); err != nil {
		t.Fatal(err)
	}
	return toolTaskListener{conn: conn}
}

func (l toolTaskListener) expect(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := l.conn.WaitForNotification(ctx); err != nil {
		t.Fatal("no creative_agent_tool_tasks notification", err)
	}
}
