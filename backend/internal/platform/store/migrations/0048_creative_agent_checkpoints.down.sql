DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM creative_agent_checkpoints) THEN
  RAISE EXCEPTION 'recorded agent checkpoints require export before rollback';
 END IF;
END $$;
DROP TABLE creative_checkpoint_content_refs,creative_agent_checkpoints;
