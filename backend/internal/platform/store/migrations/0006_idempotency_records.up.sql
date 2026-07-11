CREATE TABLE idempotency_records (
    account_id      TEXT        NOT NULL REFERENCES accounts (id),
    operation       TEXT        NOT NULL
        CHECK (operation IN ('order.create.v1', 'schedule-slot.create.v1')),
    key             TEXT        NOT NULL,
    request_hash    TEXT        NOT NULL,
    response_status INTEGER,
    response_body   BYTEA,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at      TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (account_id, operation, key),
    CHECK (
        (response_status IS NULL AND response_body IS NULL)
        OR
        (response_status >= 200 AND response_status < 300 AND response_body IS NOT NULL)
    )
);

CREATE INDEX idempotency_records_expires_idx
    ON idempotency_records (expires_at);
