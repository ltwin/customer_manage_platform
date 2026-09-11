-- Commit-time invalidation, not a persistent event log. Never include content.
CREATE FUNCTION notify_creative_canvas_changed() RETURNS trigger LANGUAGE plpgsql AS $$
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
CREATE TRIGGER creative_canvases_notify AFTER INSERT OR UPDATE OR DELETE ON creative_canvases
 FOR EACH ROW EXECUTE FUNCTION notify_creative_canvas_changed('account');
CREATE TRIGGER creative_nodes_notify AFTER INSERT OR UPDATE OR DELETE ON creative_nodes
 FOR EACH ROW EXECUTE FUNCTION notify_creative_canvas_changed('canvas');
CREATE TRIGGER creative_edges_notify AFTER INSERT OR UPDATE OR DELETE ON creative_edges
 FOR EACH ROW EXECUTE FUNCTION notify_creative_canvas_changed('canvas');
CREATE TRIGGER creative_node_inputs_notify AFTER INSERT OR UPDATE OR DELETE ON creative_node_inputs
 FOR EACH ROW EXECUTE FUNCTION notify_creative_canvas_changed('canvas');
CREATE TRIGGER creative_changes_notify AFTER INSERT OR UPDATE OR DELETE ON creative_changes
 FOR EACH ROW EXECUTE FUNCTION notify_creative_canvas_changed('canvas');
CREATE TRIGGER creative_node_prompt_drafts_notify AFTER INSERT OR UPDATE OR DELETE ON creative_node_prompt_drafts
 FOR EACH ROW EXECUTE FUNCTION notify_creative_canvas_changed('canvas');
CREATE TRIGGER creative_node_prompt_refs_notify AFTER INSERT OR UPDATE OR DELETE ON creative_node_prompt_refs
 FOR EACH ROW EXECUTE FUNCTION notify_creative_canvas_changed('canvas');
CREATE TRIGGER creative_node_versions_notify AFTER INSERT OR UPDATE OR DELETE ON creative_node_versions
 FOR EACH ROW EXECUTE FUNCTION notify_creative_canvas_changed('canvas');
CREATE TRIGGER creative_node_executions_notify AFTER INSERT OR UPDATE OR DELETE ON creative_node_executions
 FOR EACH ROW EXECUTE FUNCTION notify_creative_canvas_changed('canvas');
CREATE TRIGGER creative_projects_notify AFTER INSERT OR UPDATE OR DELETE ON creative_projects
 FOR EACH ROW EXECUTE FUNCTION notify_creative_canvas_changed('account');
CREATE TRIGGER creative_documents_notify AFTER INSERT OR UPDATE OR DELETE ON creative_documents
 FOR EACH ROW EXECUTE FUNCTION notify_creative_canvas_changed('account');
CREATE TRIGGER creative_usage_grants_notify AFTER INSERT OR UPDATE OR DELETE ON creative_usage_grants
 FOR EACH ROW EXECUTE FUNCTION notify_creative_canvas_changed('account');
CREATE TRIGGER creative_content_revisions_notify AFTER INSERT OR UPDATE OR DELETE ON creative_content_revisions
 FOR EACH ROW EXECUTE FUNCTION notify_creative_canvas_changed('account');
CREATE TRIGGER creative_content_required_grants_notify AFTER INSERT OR UPDATE OR DELETE ON creative_content_required_grants
 FOR EACH ROW EXECUTE FUNCTION notify_creative_canvas_changed('account');
CREATE TRIGGER accounts_creative_notify AFTER UPDATE OF status OR DELETE ON accounts
 FOR EACH ROW EXECUTE FUNCTION notify_creative_canvas_changed('account');
