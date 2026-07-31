ALTER TABLE accounts
    ADD COLUMN status TEXT NOT NULL DEFAULT 'legacy_unclaimed',
    ALTER COLUMN password_hash DROP NOT NULL;

ALTER TABLE accounts
    ADD CONSTRAINT accounts_status_check
    CHECK (status IN ('pending_verification', 'active', 'legacy_unclaimed'));

CREATE TABLE account_identities (
    id               TEXT        PRIMARY KEY,
    account_id       TEXT        NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    kind             TEXT        NOT NULL CHECK (kind = 'email'),
    normalized_value TEXT        NOT NULL CHECK (octet_length(normalized_value) BETWEEN 3 AND 254),
    verified_at      TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (kind, normalized_value),
    UNIQUE (account_id, kind)
);

CREATE TABLE password_credentials (
    account_id    TEXT        PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
    password_hash TEXT        NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO password_credentials (account_id, password_hash)
SELECT id, password_hash
FROM accounts
WHERE password_hash IS NOT NULL;

CREATE TABLE auth_action_tokens (
    selector    TEXT        PRIMARY KEY,
    account_id  TEXT        NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    purpose     TEXT        NOT NULL CHECK (purpose IN ('email_verification', 'legacy_claim')),
    secret_hash BYTEA       NOT NULL CHECK (octet_length(secret_hash) = 32),
    expires_at  TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (account_id, purpose)
);

CREATE INDEX auth_action_tokens_expiry_idx
    ON auth_action_tokens (expires_at)
    WHERE consumed_at IS NULL;

CREATE TABLE refresh_session_families (
    id                  TEXT        PRIMARY KEY,
    account_id          TEXT        NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    absolute_expires_at TIMESTAMPTZ NOT NULL,
    revoked_at          TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX refresh_session_families_account_idx
    ON refresh_session_families (account_id);

CREATE TABLE refresh_session_generations (
    id                      TEXT        PRIMARY KEY,
    family_id               TEXT        NOT NULL REFERENCES refresh_session_families(id) ON DELETE CASCADE,
    token_hash              BYTEA       NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    idle_expires_at         TIMESTAMPTZ NOT NULL,
    used_at                 TIMESTAMPTZ,
    successor_generation_id TEXT        REFERENCES refresh_session_generations(id),
    replay_until            TIMESTAMPTZ,
    replay_ciphertext       BYTEA,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((replay_until IS NULL) = (replay_ciphertext IS NULL)),
    CHECK (replay_ciphertext IS NULL OR octet_length(replay_ciphertext) >= 29)
);

CREATE UNIQUE INDEX refresh_session_current_generation_idx
    ON refresh_session_generations (family_id)
    WHERE used_at IS NULL;

CREATE INDEX refresh_session_replay_expiry_idx
    ON refresh_session_generations (replay_until)
    WHERE replay_ciphertext IS NOT NULL;
