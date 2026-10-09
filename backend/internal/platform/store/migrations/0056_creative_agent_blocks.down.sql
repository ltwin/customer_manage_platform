DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM creative_agent_blocks) THEN
  RAISE EXCEPTION 'agent blocks exist; drain them before rollback';
 END IF;
END $$;
CREATE OR REPLACE FUNCTION notify_creative_canvas_changed() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE row_data jsonb; owner_id text; target_id text;
BEGIN
 IF TG_OP = 'DELETE' THEN row_data := to_jsonb(OLD); ELSE row_data := to_jsonb(NEW); END IF;
 IF TG_OP = 'UPDATE' AND OLD IS NOT DISTINCT FROM NEW THEN RETURN NULL; END IF;
 owner_id := row_data->>'account_id';
 IF TG_TABLE_NAME = 'accounts' THEN owner_id := row_data->>'id'; END IF;
 IF TG_ARGV[0] = 'canvas' THEN target_id := row_data->>'canvas_id'; END IF;
 IF TG_TABLE_NAME = 'creative_canvases' THEN target_id := row_data->>'id'; END IF;
 PERFORM pg_notify('creative_canvas_changed', json_build_object('Account', owner_id, 'Canvas', target_id)::text);
 RETURN NULL;
END;
$$;
DROP TRIGGER creative_run_events_notify ON creative_run_events;
DROP FUNCTION notify_creative_run_event();
ALTER TABLE creative_agent_runs DROP COLUMN waiting_block_id, DROP COLUMN next_block_ordinal;
DROP TABLE creative_agent_blocks;
