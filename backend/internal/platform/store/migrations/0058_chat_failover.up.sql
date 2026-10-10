-- A logical chat turn owns multiple independently metered requests.
-- Failed requests retain their accounting evidence; only a complete result is consumed.
CREATE TABLE llm_chat_routes (
 account_id TEXT NOT NULL,
 root_request_id TEXT NOT NULL,
 deployments JSONB NOT NULL CHECK(jsonb_typeof(deployments)='array'),
 final_request_id TEXT,
 final_binding_key TEXT,
 state TEXT NOT NULL DEFAULT 'active' CHECK(state IN ('active','succeeded','exhausted')),
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(account_id,root_request_id),
 CHECK((state='succeeded')=(final_request_id IS NOT NULL AND final_binding_key IS NOT NULL))
);
ALTER TABLE llm_requests ADD COLUMN failure_http_status INTEGER;
