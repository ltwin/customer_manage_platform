ALTER TABLE creative_agent_runs
 ADD COLUMN waiting_token TEXT,
 ADD COLUMN wait_reason TEXT,
 ADD COLUMN source_run_id TEXT,
 ADD COLUMN source_summary JSONB;
CREATE INDEX creative_agent_run_maintenance ON creative_agent_runs(account_id,updated_at,id)
 WHERE finished_at IS NULL OR settlement_state IN ('unknown','pending');
