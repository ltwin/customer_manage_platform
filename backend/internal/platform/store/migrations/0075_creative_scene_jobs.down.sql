-- A run waiting on a client job has no representation without the state;
-- refuse instead of stranding it.
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM creative_agent_runs WHERE state = 'waiting_client') THEN
  RAISE EXCEPTION 'runs are waiting on client jobs; finish or cancel them before rollback';
 END IF;
END $$;
DROP INDEX creative_agent_run_live;
CREATE INDEX creative_agent_run_live ON creative_agent_runs(account_id,canvas_id)
 WHERE state IN ('queued','running','reconciling','waiting_input','waiting_apply');
ALTER TABLE creative_agent_runs DROP CONSTRAINT creative_agent_runs_state_check;
ALTER TABLE creative_agent_runs ADD CONSTRAINT creative_agent_runs_state_check CHECK (state IN
 ('queued','running','waiting_input','waiting_apply','reconciling',
  'succeeded','partial','failed','cancelled'));
DROP TABLE creative_scene_jobs;
