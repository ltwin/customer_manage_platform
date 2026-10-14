DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM creative_agent_library_calls) THEN
  RAISE EXCEPTION 'agent library organize results must be retained or explicitly removed before rollback';
 END IF;
END $$;
DROP TABLE creative_agent_library_calls;
ALTER TABLE creative_library_settings DROP COLUMN cursor_key;
