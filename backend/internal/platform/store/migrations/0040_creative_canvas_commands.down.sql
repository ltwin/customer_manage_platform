-- Refuse a lossy rollback after new graph features have been used.
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM creative_changes) OR EXISTS(SELECT 1 FROM creative_node_executions) OR EXISTS(SELECT 1 FROM creative_documents) OR EXISTS(SELECT 1 FROM creative_node_prompt_drafts) THEN
  RAISE EXCEPTION 'creative canvas data requires export/restore before rollback';
 END IF;
END $$;
DROP TABLE creative_content_required_grants,creative_node_version_counters,creative_graph_migration_state,creative_documents,creative_execution_refs,creative_node_executions,creative_node_version_input_refs,creative_node_versions,creative_node_prompt_refs,creative_node_prompt_drafts,creative_change_content_refs,creative_changes,creative_graph_identities,creative_node_inputs,creative_edges;
ALTER TABLE creative_nodes DROP COLUMN selected_version_id, DROP COLUMN document_id, DROP COLUMN status_revision, DROP COLUMN active_execution_id, DROP COLUMN latest_execution_id;
ALTER TABLE creative_nodes ADD CONSTRAINT creative_nodes_type_key_check CHECK(type_key IN ('core.text','core.link'));
