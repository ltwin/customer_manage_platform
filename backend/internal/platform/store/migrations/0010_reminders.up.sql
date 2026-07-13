CREATE TABLE reminders (
    id          TEXT PRIMARY KEY,
    account_id  TEXT NOT NULL REFERENCES accounts (id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    type        TEXT NOT NULL CHECK (type IN ('birthday', 'follow_up', 'churn', 'custom')),
    customer_id TEXT,
    order_id    TEXT,
    due_date    DATE NOT NULL,
    content     TEXT NOT NULL CHECK (char_length(content) > 0),
    status      TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'done', 'dismissed')),
    dedup_key   TEXT NOT NULL,
    UNIQUE (account_id, id),
    UNIQUE (account_id, dedup_key),
    FOREIGN KEY (account_id, customer_id) REFERENCES customers (account_id, id)
);

CREATE INDEX reminders_account_status_due_idx
    ON reminders (account_id, status, due_date ASC, id ASC);

CREATE INDEX reminders_account_customer_idx
    ON reminders (account_id, customer_id);

CREATE INDEX reminders_account_order_idx
    ON reminders (account_id, order_id)
    WHERE order_id IS NOT NULL;

CREATE TABLE reminder_scan_state (
    account_id     TEXT PRIMARY KEY REFERENCES accounts (id),
    last_scan_date DATE NOT NULL
);
