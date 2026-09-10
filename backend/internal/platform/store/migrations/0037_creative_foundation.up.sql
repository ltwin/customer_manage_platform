-- Explicit account keys: these new business tables intentionally have no foreign keys.
CREATE TABLE creative_account_capabilities (
    account_id TEXT PRIMARY KEY,
    read_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    manual_write_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    media_write_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    agent_start_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    node_generate_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    generation_apply_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    gc_delete_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0)
);
CREATE TABLE creative_operation_receipts (
    account_id TEXT NOT NULL,
    operation_id UUID NOT NULL,
    operation_type TEXT NOT NULL,
    client_created_at TIMESTAMPTZ NOT NULL,
    request_hash TEXT NOT NULL CHECK (length(request_hash) = 64),
    http_status INTEGER NOT NULL CHECK (http_status IN (200, 201, 202)),
    response JSONB NOT NULL CHECK (jsonb_typeof(response) = 'object' AND octet_length(response::text) <= 32768),
    result_kind TEXT NOT NULL,
    result_id TEXT,
    result_revision BIGINT CHECK (result_revision > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    retained_until TIMESTAMPTZ NOT NULL CHECK (retained_until > created_at),
    PRIMARY KEY (account_id, operation_id)
);
CREATE INDEX creative_operation_receipts_expiry ON creative_operation_receipts(retained_until, account_id);
