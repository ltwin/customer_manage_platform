-- 已启用或已派发的生成身份不能交还不理解其协议的旧worker。
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM llm_generation_requests WHERE control_revision>0)
 OR EXISTS(SELECT 1 FROM llm_generation_tasks)
 THEN RAISE EXCEPTION 'generation lifecycle data must be drained before rollback'; END IF;
END $$;
DROP TABLE llm_generation_outputs;
DROP TABLE llm_generation_observations;
DROP TABLE llm_generation_tasks;
DROP INDEX llm_generation_due;
ALTER TABLE llm_generation_requests DROP COLUMN control_revision, DROP COLUMN next_advance_at, DROP COLUMN verification_required;
DROP TABLE llm_generation_deployments;
