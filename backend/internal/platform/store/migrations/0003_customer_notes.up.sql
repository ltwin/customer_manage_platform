CREATE TABLE customer_notes (
    id          TEXT        PRIMARY KEY,
    account_id  TEXT        NOT NULL REFERENCES accounts (id),
    customer_id TEXT        NOT NULL,
    content     TEXT        NOT NULL CHECK (char_length(content) BETWEEN 1 AND 500),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    FOREIGN KEY (account_id, customer_id) REFERENCES customers (account_id, id) ON DELETE CASCADE
);

CREATE INDEX customer_notes_account_customer_created_idx
    ON customer_notes (account_id, customer_id, created_at DESC, id DESC);
