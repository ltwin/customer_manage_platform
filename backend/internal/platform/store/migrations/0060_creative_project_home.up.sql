-- V6-061–064：首页与项目页。
-- edited_at 是「最近编辑」：画布内容提交与改名推进它，归档、恢复不动它。
-- 它带默认值、normalized_name 是生成列，旧版本应用只写 id、name 也能插入。
ALTER TABLE creative_projects ADD COLUMN edited_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp();
UPDATE creative_projects p SET edited_at = GREATEST(p.updated_at, c.updated_at)
 FROM creative_canvases c WHERE c.account_id=p.account_id AND c.project_id=p.id AND c.is_default;
-- 与资产库标题同一套规范化（NFKC、小写、去首尾空白），排序按码位。
ALTER TABLE creative_projects ADD COLUMN normalized_name TEXT COLLATE "C"
 GENERATED ALWAYS AS (btrim(lower(normalize(name, NFKC)))) STORED;
CREATE INDEX creative_project_edited ON creative_projects(account_id,edited_at DESC,id DESC);
CREATE INDEX creative_project_name ON creative_projects(account_id,normalized_name,id);
-- 项目卡片的进行中状态只读活跃的那一小部分。
CREATE INDEX creative_agent_run_live ON creative_agent_runs(account_id,canvas_id)
 WHERE state IN ('queued','running','reconciling','waiting_input','waiting_apply');
CREATE INDEX creative_execution_live ON creative_node_executions(account_id,canvas_id)
 WHERE state IN ('queued','running','reconciling');

-- 画布每次提交都会推进 edited_at。项目行的通知是账号级的（ID 为空 = 所有画布重读），
-- 所以只改了 edited_at 的更新不发通知。
CREATE OR REPLACE FUNCTION notify_creative_canvas_changed() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE row_data jsonb; owner_id text; target_id text;
BEGIN
 IF TG_OP = 'DELETE' THEN row_data := to_jsonb(OLD); ELSE row_data := to_jsonb(NEW); END IF;
 IF TG_OP = 'UPDATE' AND OLD IS NOT DISTINCT FROM NEW THEN RETURN NULL; END IF;
 IF TG_OP = 'UPDATE' AND TG_TABLE_NAME = 'creative_projects'
    AND to_jsonb(OLD) - 'edited_at' = to_jsonb(NEW) - 'edited_at' THEN RETURN NULL; END IF;
 owner_id := row_data->>'account_id';
 IF TG_TABLE_NAME = 'accounts' THEN owner_id := row_data->>'id'; END IF;
 IF TG_ARGV[0] = 'canvas' THEN target_id := row_data->>'canvas_id'; END IF;
 IF TG_TABLE_NAME = 'creative_canvases' THEN target_id := row_data->>'id'; END IF;
 PERFORM pg_notify('creative_realtime', json_build_object('Kind','canvas','Account', owner_id, 'ID', target_id)::text);
 RETURN NULL;
END;
$$;
