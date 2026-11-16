-- agent-tool-background: one row per accepted long-running tool step. The row
-- owns foreground/background ownership, result publication and completion
-- notice delivery; execution, cost and effects stay with the source records
-- (node executions, image edit renders, scene client jobs). Explicit account
-- keys only; no foreign keys, as in 0044.
CREATE TABLE creative_agent_tool_tasks (
 id TEXT PRIMARY KEY CHECK(id ~ '^cctask_[0-9a-f-]{36}$'),
 account_id TEXT NOT NULL,
 conversation_id TEXT NOT NULL,
 canvas_id TEXT NOT NULL,
 source_run_id TEXT NOT NULL,
 tool_step_id TEXT NOT NULL,
 tool_key TEXT NOT NULL CHECK(tool_key IN ('generate_node','compress_image','write_model','render_view','export_shots')),
 -- creative_node_executions.id, creative_image_edit_renders.id or
 -- creative_scene_jobs.id, by tool_key.
 source_id TEXT NOT NULL,
 mode TEXT NOT NULL CHECK(mode IN ('foreground','background')),
 state TEXT NOT NULL DEFAULT 'running' CHECK(state IN ('running','succeeded','failed','cancelled','expired')),
 revision BIGINT NOT NULL DEFAULT 1 CHECK(revision>0),
 result JSONB,
 finished_at TIMESTAMPTZ,
 -- The source run's model and main Skill version: an automatic run inherits
 -- them, and they group notices for one wake-up.
 model_key TEXT NOT NULL,
 skill_version_id TEXT,
 notification_state TEXT NOT NULL DEFAULT 'none' CHECK(notification_state IN ('none','pending','delivered','suppressed')),
 notification_message_id TEXT,
 -- The model step whose dispatch carried the notice. With notification_state
 -- this is the only delivery record; a model step's snapshot is replay input.
 delivery_model_step_id TEXT,
 wake_after TIMESTAMPTZ,
 wake_skip_reason TEXT CHECK(wake_skip_reason IN ('source_stopped','source_failed','chain_limit','egress','credits','unavailable')),
 wake_run_id TEXT,
 -- Due finishing work. Source triggers set it; the finisher clears it.
 next_check_at TIMESTAMPTZ,
 stop_requested_at TIMESTAMPTZ,
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(account_id,id),
 UNIQUE(account_id,tool_step_id),
 UNIQUE(account_id,tool_key,source_id),
 -- render_view has no background form.
 CHECK(tool_key<>'render_view' OR mode='foreground'),
 -- A terminal task has its result and time; a running one has neither.
 CHECK((state='running')=(result IS NULL AND finished_at IS NULL)),
 -- Only a finished background task carries a notice.
 CHECK(notification_state='none' OR (mode='background' AND state<>'running')),
 CHECK((notification_state IN ('pending','delivered'))=(notification_message_id IS NOT NULL)),
 CHECK((notification_state='delivered')=(delivery_model_step_id IS NOT NULL)),
 -- Wake-up bookkeeping belongs to a pending notice, and a skipped notice has no
 -- wake time.
 CHECK(notification_state='pending' OR (wake_after IS NULL AND wake_skip_reason IS NULL)),
 CHECK(wake_after IS NULL OR wake_skip_reason IS NULL)
);
-- The finisher and the wake worker scan across accounts; each item is then
-- handled in its own account scope.
CREATE INDEX creative_agent_tool_tasks_due ON creative_agent_tool_tasks(next_check_at,id)
 WHERE next_check_at IS NOT NULL;
CREATE INDEX creative_agent_tool_tasks_wake ON creative_agent_tool_tasks(wake_after,id)
 WHERE wake_after IS NOT NULL;
CREATE INDEX creative_agent_tool_tasks_pending ON creative_agent_tool_tasks(account_id,conversation_id,finished_at,id)
 WHERE notification_state='pending';
-- Stopping a run freezes its foreground tasks.
CREATE INDEX creative_agent_tool_tasks_run ON creative_agent_tool_tasks(account_id,source_run_id)
 WHERE state='running';

-- Source triggers only mark the task due and wake the finisher. They never
-- read or write runs, conversations or messages, never decide an outcome and
-- never raise; a source without a task row (an old path, or the photographer's
-- own work) is left alone. Lock order: source row, then task row.
CREATE FUNCTION mark_creative_agent_tool_task() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 UPDATE creative_agent_tool_tasks SET next_check_at = clock_timestamp()
  WHERE account_id = NEW.account_id AND source_id = NEW.id
   AND tool_key = ANY(TG_ARGV) AND state = 'running';
 IF FOUND THEN
  PERFORM pg_notify('creative_agent_tool_tasks', '');
 END IF;
 RETURN NULL;
END;
$$;
CREATE TRIGGER creative_node_executions_tool_task AFTER UPDATE ON creative_node_executions
 FOR EACH ROW WHEN (OLD.state IS DISTINCT FROM NEW.state OR OLD.apply_state IS DISTINCT FROM NEW.apply_state)
 EXECUTE FUNCTION mark_creative_agent_tool_task('generate_node');
CREATE TRIGGER creative_image_edit_renders_tool_task AFTER UPDATE ON creative_image_edit_renders
 FOR EACH ROW WHEN (OLD.state IS DISTINCT FROM NEW.state)
 EXECUTE FUNCTION mark_creative_agent_tool_task('compress_image');
CREATE TRIGGER creative_scene_jobs_tool_task AFTER UPDATE ON creative_scene_jobs
 FOR EACH ROW WHEN (OLD.state IS DISTINCT FROM NEW.state)
 EXECUTE FUNCTION mark_creative_agent_tool_task('write_model','render_view','export_shots');

-- Cards follow their task through the canvas invalidation tabs already
-- listen to.
CREATE TRIGGER creative_agent_tool_tasks_notify AFTER INSERT OR UPDATE OF revision ON creative_agent_tool_tasks
 FOR EACH ROW EXECUTE FUNCTION notify_creative_canvas_changed('canvas');

-- An automatic run started by a background task's completion.
ALTER TABLE creative_agent_runs
 ADD COLUMN trigger_kind TEXT CHECK(trigger_kind IS NULL OR trigger_kind='background_tool');

-- A model step keeps the notices it was assembled with (replay input, not a
-- delivery record) and, on the final step, the conversation ordinal up to
-- which this run may still pick notices up.
ALTER TABLE creative_agent_steps
 ADD COLUMN background_notification_ids JSONB CHECK(background_notification_ids IS NULL OR jsonb_typeof(background_notification_ids)='array'),
 ADD COLUMN background_notification_through_ordinal BIGINT CHECK(background_notification_through_ordinal IS NULL OR background_notification_through_ordinal>0);
