-- 路由只选择供应商调用；作品状态仍归画布，费用仍归每次request。
CREATE TABLE llm_generation_routes (
 account_id TEXT NOT NULL, root_request_id TEXT NOT NULL,
 plan BYTEA NOT NULL CHECK(octet_length(plan) <= 60000000),
 strategy_version TEXT NOT NULL DEFAULT 'ordered-v1' CHECK(strategy_version='ordered-v1'),
 max_submissions INTEGER NOT NULL CHECK(max_submissions BETWEEN 2 AND 16),
 plan_digest TEXT NOT NULL, max_cost_micros BIGINT NOT NULL CHECK(max_cost_micros>=0),
 current_request_id TEXT NOT NULL, candidate_index INTEGER NOT NULL DEFAULT 0 CHECK(candidate_index BETWEEN 0 AND 7),
 candidate_attempt INTEGER NOT NULL DEFAULT 1 CHECK(candidate_attempt BETWEEN 1 AND 2),
 ordinal INTEGER NOT NULL DEFAULT 0 CHECK(ordinal BETWEEN 0 AND 15),
 state TEXT NOT NULL DEFAULT 'active' CHECK(state IN ('active','finished','cancelled')),
 next_advance_at TIMESTAMPTZ, final_request_id TEXT,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 PRIMARY KEY(account_id,root_request_id),
 CHECK(state<>'finished' OR final_request_id IS NOT NULL)
);
CREATE INDEX llm_generation_route_due ON llm_generation_routes(account_id,next_advance_at) WHERE state='active';
CREATE TABLE llm_generation_route_calls (
 account_id TEXT NOT NULL, root_request_id TEXT NOT NULL, ordinal INTEGER NOT NULL CHECK(ordinal BETWEEN 0 AND 15),
 request_id TEXT NOT NULL, reason TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(account_id,root_request_id,ordinal), UNIQUE(account_id,request_id)
);
