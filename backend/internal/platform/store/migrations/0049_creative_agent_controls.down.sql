DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM creative_agent_runs WHERE waiting_token IS NOT NULL OR source_run_id IS NOT NULL) THEN
  RAISE EXCEPTION 'export creative run control/recovery evidence before rollback';
 END IF;
END $$;
DROP INDEX creative_agent_run_maintenance;
ALTER TABLE creative_agent_runs DROP COLUMN source_summary,DROP COLUMN source_run_id,DROP COLUMN wait_reason,DROP COLUMN waiting_token;
