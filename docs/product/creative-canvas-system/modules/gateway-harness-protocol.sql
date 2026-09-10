-- 第三批协议探针字段投影：仅供隔离测试库，不是完整模块/生产迁移。
-- 全部业务表无外键；实际服务的归属/权限/载荷验证见模块文档。
CREATE TABLE llm_budgets (
 account_id TEXT NOT NULL, period_start DATE NOT NULL, currency TEXT NOT NULL,
 limit_micros BIGINT NOT NULL CHECK(limit_micros>=0), reserved_micros BIGINT NOT NULL DEFAULT 0 CHECK(reserved_micros>=0),
 spent_micros BIGINT NOT NULL DEFAULT 0 CHECK(spent_micros>=0), revision BIGINT NOT NULL DEFAULT 1 CHECK(revision>0),
 PRIMARY KEY(account_id,period_start,currency)
);
CREATE TABLE llm_usage_reservations (
 id TEXT PRIMARY KEY, account_id TEXT NOT NULL, caller_service TEXT NOT NULL, caller_operation_id TEXT NOT NULL,
 request_id TEXT, budget_period DATE NOT NULL, currency TEXT NOT NULL,
 initial_reserved_micros BIGINT NOT NULL CHECK(initial_reserved_micros>=0),
 remaining_hold_micros BIGINT NOT NULL CHECK(remaining_hold_micros>=0),
 hold_generation BIGINT NOT NULL DEFAULT 1 CHECK(hold_generation>0),
 UNIQUE(account_id,id), UNIQUE(account_id,caller_service,caller_operation_id), UNIQUE(account_id,request_id)
);
CREATE TABLE llm_requests (
 id TEXT PRIMARY KEY, account_id TEXT NOT NULL, caller_service TEXT NOT NULL, caller_operation_id TEXT NOT NULL,
 request_hash TEXT NOT NULL, state TEXT NOT NULL CHECK(state IN ('prepared','dispatching','streaming','succeeded','failed','cancelled','unknown')),
 cancel_requested_at TIMESTAMPTZ, result_payload JSONB, result_hash TEXT,
 UNIQUE(account_id,id), UNIQUE(account_id,caller_service,caller_operation_id),
 CHECK(state<>'succeeded' OR (result_payload IS NOT NULL AND result_hash IS NOT NULL))
);
CREATE TABLE llm_attempts (
 id TEXT PRIMARY KEY, account_id TEXT NOT NULL, request_id TEXT NOT NULL,
 attempt_number INTEGER NOT NULL CHECK(attempt_number>0), permit_digest TEXT NOT NULL,
 dispatch_state TEXT NOT NULL CHECK(dispatch_state IN ('dispatching','streaming','succeeded','rejected','unknown')),
 reservation_generation BIGINT NOT NULL DEFAULT 1 CHECK(reservation_generation>0),
 UNIQUE(account_id,id), UNIQUE(account_id,request_id,attempt_number), UNIQUE(permit_digest)
);
CREATE UNIQUE INDEX llm_single_pending_attempt ON llm_attempts(account_id,request_id)
 WHERE dispatch_state IN ('dispatching','streaming','unknown');
CREATE TABLE llm_usage_measurements (
 id TEXT PRIMARY KEY, account_id TEXT NOT NULL, attempt_id TEXT NOT NULL, measurement_key TEXT NOT NULL,
 cost_component TEXT NOT NULL, currency TEXT NOT NULL, supersedes_id TEXT,
 cost_micros BIGINT NOT NULL CHECK(cost_micros>=0),
 UNIQUE(account_id,id), UNIQUE(account_id,attempt_id,measurement_key)
);
CREATE TABLE llm_cost_positions (
 account_id TEXT NOT NULL, attempt_id TEXT NOT NULL, cost_component TEXT NOT NULL, currency TEXT NOT NULL,
 current_measurement_id TEXT NOT NULL, booked_cost_micros BIGINT NOT NULL CHECK(booked_cost_micros>=0),
 revision BIGINT NOT NULL CHECK(revision>0), PRIMARY KEY(account_id,attempt_id,cost_component,currency)
);
CREATE TABLE llm_settlement_receipts (
 account_id TEXT NOT NULL, attempt_id TEXT NOT NULL, cost_component TEXT NOT NULL, currency TEXT NOT NULL,
 position_revision BIGINT NOT NULL CHECK(position_revision>0), measurement_id TEXT NOT NULL,
 old_value BIGINT NOT NULL, new_value BIGINT NOT NULL, PRIMARY KEY(account_id,attempt_id,cost_component,currency,position_revision)
);
CREATE TABLE creative_agent_runs (
 id TEXT PRIMARY KEY, account_id TEXT NOT NULL, state TEXT NOT NULL
 CHECK(state IN ('queued','running','waiting_input','waiting_apply','reconciling','succeeded','partial','failed','cancelled')),
 execution_epoch BIGINT NOT NULL CHECK(execution_epoch>0), claim_token TEXT,
 cancel_requested_at TIMESTAMPTZ, lease_until TIMESTAMPTZ, deadline_at TIMESTAMPTZ NOT NULL,
 last_event_seq BIGINT NOT NULL DEFAULT 0 CHECK(last_event_seq>=0), pruned_through_seq BIGINT NOT NULL DEFAULT 0,
 UNIQUE(account_id,id), CHECK(pruned_through_seq BETWEEN 0 AND last_event_seq)
);
CREATE TABLE creative_agent_slots (
 account_id TEXT PRIMARY KEY, run_id TEXT, claim_token TEXT,
 CHECK((run_id IS NULL)=(claim_token IS NULL))
);
CREATE TABLE creative_run_events (
 account_id TEXT NOT NULL, run_id TEXT NOT NULL, seq BIGINT NOT NULL CHECK(seq>0), event_type TEXT NOT NULL,
 payload JSONB NOT NULL DEFAULT '{}', created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(account_id,run_id,seq)
);
CREATE TABLE llm_result_consumers (
 account_id TEXT NOT NULL, request_id TEXT NOT NULL, caller_service TEXT NOT NULL, consumer_key TEXT NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('pending','consumed','abandoned')),
 PRIMARY KEY(account_id,request_id,caller_service,consumer_key)
);
CREATE TABLE creative_agent_steps (
 id TEXT PRIMARY KEY, account_id TEXT NOT NULL, run_id TEXT NOT NULL, parent_model_step_id TEXT NOT NULL,
 tool_call_index INTEGER NOT NULL CHECK(tool_call_index>=0), input_hash TEXT NOT NULL,
 UNIQUE(account_id,parent_model_step_id,tool_call_index)
);
