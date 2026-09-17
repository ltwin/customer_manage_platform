CREATE TABLE creative_agent_checkpoints (
 id TEXT PRIMARY KEY CHECK(id ~ '^cccp_[0-9a-f-]{36}$'), account_id TEXT NOT NULL, run_id TEXT NOT NULL,
 runtime_version TEXT NOT NULL, serializer_version TEXT NOT NULL,
 registry_digest TEXT NOT NULL, skill_catalog_digest TEXT NOT NULL,
 execution_epoch BIGINT NOT NULL CHECK(execution_epoch>0), revision BIGINT NOT NULL CHECK(revision>0),
 model_step_id TEXT NOT NULL, journal JSONB NOT NULL,
 payload BYTEA NOT NULL CHECK(octet_length(payload)>0 AND octet_length(payload)<=1048576),
 payload_digest TEXT NOT NULL CHECK(payload_digest ~ '^[0-9a-f]{64}$'),
 created_at TIMESTAMPTZ NOT NULL, updated_at TIMESTAMPTZ NOT NULL, retained_until TIMESTAMPTZ NOT NULL,
 UNIQUE(account_id,id), UNIQUE(account_id,run_id)
);
CREATE TABLE creative_checkpoint_content_refs (
 account_id TEXT NOT NULL, checkpoint_id TEXT NOT NULL, content_revision_id TEXT NOT NULL,
 PRIMARY KEY(account_id,checkpoint_id,content_revision_id)
);
CREATE INDEX creative_checkpoint_revision ON creative_checkpoint_content_refs(account_id,content_revision_id);
