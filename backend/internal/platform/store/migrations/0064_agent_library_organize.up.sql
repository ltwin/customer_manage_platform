-- Per-account cursor signing survives process restarts without exposing secrets.
ALTER TABLE creative_library_settings ADD COLUMN cursor_key text NOT NULL DEFAULT gen_random_uuid()::text;

CREATE TABLE creative_agent_library_calls (
 account_id text NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
 id text NOT NULL,
 tool_step_id text NOT NULL,
 run_id text NOT NULL,
 conversation_id text NOT NULL,
 tool_name text NOT NULL,
 protocol_version integer NOT NULL CHECK (protocol_version=1),
 arguments_hash text NOT NULL,
 cursor_key text NOT NULL DEFAULT gen_random_uuid()::text,
 created_at timestamptz NOT NULL,
 retained_until timestamptz NOT NULL,
 state text NOT NULL CHECK (state IN ('executing','settling','complete')),
 plan jsonb NOT NULL,
 progress jsonb NOT NULL,
 summary jsonb,
 details jsonb NOT NULL DEFAULT '[]',
 PRIMARY KEY(account_id,id),
 UNIQUE(account_id,tool_step_id),
 FOREIGN KEY(account_id,conversation_id) REFERENCES creative_agent_conversations(account_id,id) ON DELETE CASCADE,
 CHECK (octet_length(plan::text)+octet_length(progress::text)+octet_length(details::text)<=8388608),
 CHECK (summary IS NULL OR octet_length(summary::text)<=65536)
);
CREATE INDEX creative_agent_library_calls_conversation ON creative_agent_library_calls(account_id,conversation_id,id);
CREATE INDEX creative_agent_library_calls_retention ON creative_agent_library_calls(retained_until);
