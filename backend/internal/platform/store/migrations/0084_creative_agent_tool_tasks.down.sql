-- Tasks and their notices are the record of what ran in the background and
-- what the assistant was told; refuse instead of dropping them.
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM creative_agent_tool_tasks)
  OR EXISTS (SELECT 1 FROM creative_agent_runs WHERE trigger_kind IS NOT NULL)
  OR EXISTS (SELECT 1 FROM creative_agent_steps WHERE background_notification_ids IS NOT NULL OR background_notification_through_ordinal IS NOT NULL) THEN
  RAISE EXCEPTION 'agent tool tasks exist; use a forward migration';
 END IF;
END $$;
ALTER TABLE creative_agent_steps
 DROP COLUMN background_notification_through_ordinal,
 DROP COLUMN background_notification_ids;
ALTER TABLE creative_agent_runs DROP COLUMN trigger_kind;
DROP TRIGGER creative_scene_jobs_tool_task ON creative_scene_jobs;
DROP TRIGGER creative_image_edit_renders_tool_task ON creative_image_edit_renders;
DROP TRIGGER creative_node_executions_tool_task ON creative_node_executions;
DROP FUNCTION mark_creative_agent_tool_task();
DROP TABLE creative_agent_tool_tasks;
