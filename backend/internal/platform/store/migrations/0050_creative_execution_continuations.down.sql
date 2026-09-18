DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM creative_node_executions WHERE resume_token<>'' OR next_step_at IS NOT NULL OR result_payload IS NOT NULL) THEN
  RAISE EXCEPTION 'export creative execution continuation evidence before rollback';
 END IF;
END $$;
ALTER TABLE creative_node_executions DROP COLUMN resume_token,DROP COLUMN next_step_at,DROP COLUMN result_payload;
