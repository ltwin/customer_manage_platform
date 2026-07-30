CREATE UNIQUE INDEX settings_telegram_chat_id_unique
    ON settings (telegram_chat_id)
    WHERE telegram_chat_id IS NOT NULL;

CREATE TABLE telegram_bind_tokens (
    account_id  TEXT PRIMARY KEY REFERENCES accounts (id) ON DELETE CASCADE,
    token_hash  BYTEA NOT NULL CHECK (octet_length(token_hash) = 32),
    expires_at  TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE telegram_deliveries (
    id                    TEXT PRIMARY KEY,
    account_id            TEXT NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    source                TEXT NOT NULL CHECK (source IN ('daily', 'command', 'binding_ack')),
    source_key            TEXT NOT NULL,
    message_kind          TEXT NOT NULL CHECK (message_kind IN ('digest', 'temporary_unavailable', 'binding_ack')),
    target_local_date     DATE,
    timezone_at_enqueue   TEXT,
    status                TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sent', 'failed', 'superseded')),
    attempts              INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0 AND attempts <= 4),
    next_attempt_at       TIMESTAMPTZ NOT NULL,
    sent_at               TIMESTAMPTZ,
    last_error_code       TEXT,
    claim_id              TEXT,
    lease_until           TIMESTAMPTZ,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (account_id, source, source_key),
    CHECK ((claim_id IS NULL) = (lease_until IS NULL)),
    CHECK (
        (message_kind = 'binding_ack' AND target_local_date IS NULL AND timezone_at_enqueue IS NULL)
        OR
        (message_kind <> 'binding_ack' AND target_local_date IS NOT NULL AND timezone_at_enqueue IS NOT NULL)
    )
);

CREATE INDEX telegram_deliveries_due_idx
    ON telegram_deliveries (account_id, next_attempt_at, id)
    WHERE status = 'pending';
