CREATE TABLE orders (
    id             TEXT        PRIMARY KEY,
    account_id     TEXT        NOT NULL REFERENCES accounts (id),
    customer_id    TEXT        NOT NULL,
    package_id     TEXT,
    title          TEXT,
    status         TEXT        NOT NULL DEFAULT 'consulting'
        CHECK (status IN ('consulting', 'scheduled', 'shot', 'selected', 'retouching', 'delivered', 'closed', 'cancelled')),
    price          INTEGER     CHECK (price IS NULL OR price >= 0),
    deposit_paid   BOOLEAN     NOT NULL DEFAULT false,
    balance_paid   BOOLEAN     NOT NULL DEFAULT false,
    shot_at        TIMESTAMPTZ,
    delivered_at   TIMESTAMPTZ,
    note           TEXT        CHECK (note IS NULL OR char_length(note) <= 500),
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (account_id, id),
    FOREIGN KEY (account_id, customer_id) REFERENCES customers (account_id, id),
    FOREIGN KEY (account_id, package_id) REFERENCES packages (account_id, id)
);

CREATE INDEX orders_account_customer_created_idx
    ON orders (account_id, customer_id, created_at DESC, id DESC);

CREATE INDEX orders_account_status_created_idx
    ON orders (account_id, status, created_at DESC, id DESC);

CREATE INDEX orders_account_unpaid_idx
    ON orders (account_id, balance_paid, status, created_at DESC, id DESC);

CREATE INDEX orders_account_package_idx
    ON orders (account_id, package_id);
