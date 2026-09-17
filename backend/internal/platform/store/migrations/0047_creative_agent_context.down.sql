DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM creative_agent_context_items) THEN
  RAISE EXCEPTION 'recorded agent context requires export before rollback';
 END IF;
END $$;
DROP TABLE creative_context_item_content_refs, creative_agent_context_items;
