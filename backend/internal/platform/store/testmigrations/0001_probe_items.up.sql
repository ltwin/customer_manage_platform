CREATE TABLE probe_items (
    id         TEXT        PRIMARY KEY,
    account_id TEXT        NOT NULL REFERENCES accounts (id),
    note       TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE probe_item_tags (
    id         TEXT        PRIMARY KEY,
    account_id TEXT        NOT NULL REFERENCES accounts (id),
    item_id    TEXT        NOT NULL REFERENCES probe_items (id),
    handle     TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
