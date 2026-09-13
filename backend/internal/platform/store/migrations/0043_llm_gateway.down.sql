-- Refuse a lossy rollback once real spend or dispatch evidence exists.
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM llm_attempts WHERE dispatch_state<>'rejected')
  OR EXISTS(SELECT 1 FROM llm_usage_measurements WHERE cost_micros IS NULL OR cost_micros>0)
  OR EXISTS(SELECT 1 FROM llm_budgets WHERE spent_micros>0 OR reserved_micros>0) THEN
  RAISE EXCEPTION 'llm gateway spend evidence requires export before rollback';
 END IF;
END $$;
DROP TABLE platform_llm_limits,llm_result_consumers,llm_settlement_receipts,llm_token_positions,
 llm_cost_positions,llm_measurement_dispositions,llm_usage_measurements,llm_attempts,llm_requests,
 llm_usage_reservations,llm_budgets,llm_call_groups;
