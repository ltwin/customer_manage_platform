-- 存在生成行时禁止破坏性回退（§19.8）：不允许旧二进制接管它无法解读的任务。
-- 空库回退供迁移形状测试使用；生产回退先关闭新生成准入并清空待核实数据。
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM llm_requests WHERE request_kind='generation') THEN
  RAISE EXCEPTION 'generation rows exist; destructive down migration is forbidden';
 END IF;
END $$;
ALTER TABLE llm_cost_positions DROP CONSTRAINT llm_cost_positions_cost_component_check;
ALTER TABLE llm_cost_positions ADD CONSTRAINT llm_cost_positions_cost_component_check
 CHECK(cost_component IN ('input_cached','input_uncached','output'));
ALTER TABLE llm_usage_measurements DROP CONSTRAINT llm_usage_measurements_cost_component_check;
ALTER TABLE llm_usage_measurements ADD CONSTRAINT llm_usage_measurements_cost_component_check
 CHECK(cost_component IN ('input_cached','input_uncached','output','budget_total_tokens'));
DROP TABLE llm_generation_deployment_blocks;
DROP TABLE llm_generation_requests;
ALTER TABLE llm_requests DROP CONSTRAINT llm_requests_output_limit_check;
ALTER TABLE llm_requests ADD CONSTRAINT llm_requests_output_limit_check CHECK(output_limit>0);
ALTER TABLE llm_requests DROP COLUMN request_kind;
