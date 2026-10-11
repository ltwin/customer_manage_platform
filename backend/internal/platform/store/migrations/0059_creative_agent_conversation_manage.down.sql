-- Archiving and renaming are the photographer's own choices. Dropping them
-- would put archived conversations back in the list and let naming replace a
-- chosen title, so a rollback waits until there are none.
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM creative_agent_conversations
            WHERE archived_at IS NOT NULL OR title_source='manual') THEN
  RAISE EXCEPTION 'archived or renamed conversations exist; restore them before rollback';
 END IF;
END $$;
ALTER TABLE creative_agent_conversations DROP COLUMN archived_at, DROP COLUMN title_source;
