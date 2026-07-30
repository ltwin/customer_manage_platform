CREATE TABLE schedule_slots (
    id         TEXT        PRIMARY KEY,
    account_id TEXT        NOT NULL REFERENCES accounts (id),
    start_at   TIMESTAMPTZ NOT NULL,
    end_at     TIMESTAMPTZ NOT NULL,
    type       TEXT        NOT NULL CHECK (type IN ('shoot', 'hold', 'busy')),
    order_id   TEXT,
    note       TEXT        CHECK (note IS NULL OR char_length(note) <= 500),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (account_id, id),
    CHECK (end_at > start_at),
    CHECK (
        (type = 'shoot' AND order_id IS NOT NULL)
        OR
        (type IN ('hold', 'busy') AND order_id IS NULL)
    ),
    FOREIGN KEY (account_id, order_id) REFERENCES orders (account_id, id)
);

CREATE INDEX schedule_slots_account_range_idx
    ON schedule_slots (account_id, start_at, end_at, id);

CREATE UNIQUE INDEX schedule_slots_account_shoot_order_uidx
    ON schedule_slots (account_id, order_id)
    WHERE type = 'shoot' AND order_id IS NOT NULL;
