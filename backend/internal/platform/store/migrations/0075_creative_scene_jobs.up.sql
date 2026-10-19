-- scene-agent-tools: work the server hands to an open creative workspace tab
-- (bake a code model, take a screenshot), and the run state that waits for it.
-- Explicit account keys only; no foreign keys, as the other creative tables.
CREATE TABLE creative_scene_jobs (
 id TEXT PRIMARY KEY CHECK (id ~ '^csjob_[0-9a-f-]{36}$'),
 account_id TEXT NOT NULL,
 document_id TEXT NOT NULL,
 kind TEXT NOT NULL CHECK (kind IN ('bake_model','render_view','export_shots')),
 request JSONB NOT NULL CHECK (jsonb_typeof(request)='object'),
 state TEXT NOT NULL DEFAULT 'queued'
  CHECK (state IN ('queued','claimed','succeeded','failed','expired','cancelled')),
 attempt INTEGER NOT NULL DEFAULT 0 CHECK (attempt >= 0),
 claim_token TEXT,
 editor_session_id TEXT,
 lease_until TIMESTAMPTZ,
 deadline TIMESTAMPTZ NOT NULL,
 run_id TEXT,
 tool_call_id TEXT,
 result JSONB CHECK (result IS NULL OR jsonb_typeof(result)='object'),
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 -- A claim is a token, a tab and a lease together, and only while claimed.
 CHECK ((state = 'claimed') = (claim_token IS NOT NULL)),
 CHECK ((claim_token IS NULL) = (editor_session_id IS NULL) AND (claim_token IS NULL) = (lease_until IS NULL)),
 -- Terminal and having a result are the same fact.
 CHECK ((state IN ('succeeded','failed','expired','cancelled')) = (result IS NOT NULL)),
 CHECK ((run_id IS NULL) = (tool_call_id IS NULL))
);
CREATE INDEX creative_scene_jobs_claim ON creative_scene_jobs(account_id, state, created_at);
CREATE INDEX creative_scene_jobs_document ON creative_scene_jobs(account_id, document_id, state, created_at);
CREATE INDEX creative_scene_jobs_deadline ON creative_scene_jobs(state, deadline);
-- One job per tool call: a run replayed from its checkpoint finds the job it
-- already created instead of making a second one.
CREATE UNIQUE INDEX creative_scene_jobs_tool_call ON creative_scene_jobs(account_id, run_id, tool_call_id)
 WHERE run_id IS NOT NULL;

-- Tabs learn about jobs through the account-wide canvas invalidation. Only a new
-- job, a job back in the queue and a finished job concern them: claiming and
-- lease renewal change nothing another tab shows, and would otherwise make
-- every open canvas page re-read.
CREATE TRIGGER creative_scene_jobs_notify_insert AFTER INSERT ON creative_scene_jobs
 FOR EACH ROW EXECUTE FUNCTION notify_creative_canvas_changed('account');
CREATE TRIGGER creative_scene_jobs_notify_state AFTER UPDATE ON creative_scene_jobs
 FOR EACH ROW WHEN (OLD.state IS DISTINCT FROM NEW.state AND NEW.state <> 'claimed')
 EXECUTE FUNCTION notify_creative_canvas_changed('account');

-- A run parked on a client job. It holds no slot and no lease.
ALTER TABLE creative_agent_runs DROP CONSTRAINT creative_agent_runs_state_check;
ALTER TABLE creative_agent_runs ADD CONSTRAINT creative_agent_runs_state_check CHECK (state IN
 ('queued','running','waiting_input','waiting_apply','waiting_client','reconciling',
  'succeeded','partial','failed','cancelled'));
DROP INDEX creative_agent_run_live;
CREATE INDEX creative_agent_run_live ON creative_agent_runs(account_id,canvas_id)
 WHERE state IN ('queued','running','reconciling','waiting_input','waiting_apply','waiting_client');
