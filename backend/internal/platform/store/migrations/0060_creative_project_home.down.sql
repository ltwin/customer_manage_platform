-- 新列只是派生数据（最近编辑时间、规范化名称），回滚不丢摄影师的内容。
CREATE OR REPLACE FUNCTION notify_creative_canvas_changed() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE row_data jsonb; owner_id text; target_id text;
BEGIN
 IF TG_OP = 'DELETE' THEN row_data := to_jsonb(OLD); ELSE row_data := to_jsonb(NEW); END IF;
 IF TG_OP = 'UPDATE' AND OLD IS NOT DISTINCT FROM NEW THEN RETURN NULL; END IF;
 owner_id := row_data->>'account_id';
 IF TG_TABLE_NAME = 'accounts' THEN owner_id := row_data->>'id'; END IF;
 IF TG_ARGV[0] = 'canvas' THEN target_id := row_data->>'canvas_id'; END IF;
 IF TG_TABLE_NAME = 'creative_canvases' THEN target_id := row_data->>'id'; END IF;
 PERFORM pg_notify('creative_realtime', json_build_object('Kind','canvas','Account', owner_id, 'ID', target_id)::text);
 RETURN NULL;
END;
$$;
DROP INDEX creative_execution_live;
DROP INDEX creative_agent_run_live;
DROP INDEX creative_project_name;
DROP INDEX creative_project_edited;
ALTER TABLE creative_projects DROP COLUMN normalized_name, DROP COLUMN edited_at;
