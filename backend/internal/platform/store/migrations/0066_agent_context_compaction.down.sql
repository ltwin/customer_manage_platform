DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM creative_agent_compactions)
  OR EXISTS (SELECT 1 FROM creative_agent_runs WHERE kind <> 'chat' OR compaction_state IS NOT NULL)
  OR EXISTS (SELECT 1 FROM creative_run_inputs WHERE input_role IN ('compaction','skill_index'))
  OR EXISTS (SELECT 1 FROM creative_agent_steps WHERE kind = 'compaction') THEN
  RAISE EXCEPTION 'agent context compactions must be retained or explicitly removed before rollback';
 END IF;
END $$;
ALTER TABLE creative_agent_steps
 DROP COLUMN context_composition,
 DROP COLUMN input_tokens,
 DROP COLUMN input_image_count,
 DROP COLUMN input_text_bytes;
ALTER TABLE creative_agent_steps DROP CONSTRAINT creative_agent_steps_kind_check;
ALTER TABLE creative_agent_steps ADD CONSTRAINT creative_agent_steps_kind_check
 CHECK (kind IN ('model','tool','check'));
ALTER TABLE creative_run_inputs DROP CONSTRAINT creative_run_inputs_input_role_check;
ALTER TABLE creative_run_inputs ADD CONSTRAINT creative_run_inputs_input_role_check
 CHECK (input_role IN ('history','instruction','selection','upstream','attachment','tool_result'));
ALTER TABLE creative_agent_runs
 DROP CONSTRAINT creative_agent_runs_compaction_pair_check,
 DROP COLUMN compaction_plan,
 DROP COLUMN compaction_state,
 DROP COLUMN kind;
DROP TABLE creative_agent_compactions;
