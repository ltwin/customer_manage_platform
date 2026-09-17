-- B2: retained tool output and skill resource bytes, never a host filesystem.
CREATE TABLE creative_agent_context_items (
 id TEXT PRIMARY KEY, account_id TEXT NOT NULL, run_id TEXT NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN ('tool_result','summary','skill_resource')),
 source_step_id_snapshot TEXT,
 source_manifest JSONB NOT NULL,
 payload BYTEA NOT NULL CHECK(octet_length(payload)<=65536),
 digest TEXT NOT NULL CHECK(digest ~ '^[0-9a-f]{64}$'),
 revision BIGINT NOT NULL CHECK(revision>0),
 retained_until TIMESTAMPTZ NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(account_id,id), UNIQUE(account_id,run_id,source_step_id_snapshot)
);
CREATE INDEX creative_context_run ON creative_agent_context_items(account_id,run_id);
CREATE TABLE creative_context_item_content_refs (
 account_id TEXT NOT NULL, item_id TEXT NOT NULL, content_revision_id TEXT NOT NULL,
 PRIMARY KEY(account_id,item_id,content_revision_id)
);
CREATE INDEX creative_context_revision ON creative_context_item_content_refs(account_id,content_revision_id);
