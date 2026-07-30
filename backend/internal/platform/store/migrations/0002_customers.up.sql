CREATE TABLE customers (
    id                      TEXT        PRIMARY KEY,
    account_id              TEXT        NOT NULL REFERENCES accounts (id),
    display_name            TEXT        NOT NULL,
    real_name               TEXT,
    phone                   TEXT,
    birthday                TEXT,
    channel                 TEXT        NOT NULL CHECK (channel IN ('xiaohongshu', 'douyin', 'weibo', 'referral', 'other')),
    referrer_customer_id    TEXT,
    status                  TEXT        NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'merged', 'archived')),
    merged_into_customer_id TEXT,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (account_id, id),
    FOREIGN KEY (account_id, referrer_customer_id) REFERENCES customers (account_id, id),
    FOREIGN KEY (account_id, merged_into_customer_id) REFERENCES customers (account_id, id)
);

CREATE INDEX customers_account_status_created_idx
    ON customers (account_id, status, created_at DESC);

CREATE INDEX customers_account_channel_idx
    ON customers (account_id, channel);

CREATE TABLE social_identities (
    id          TEXT        PRIMARY KEY,
    account_id  TEXT        NOT NULL REFERENCES accounts (id),
    customer_id TEXT        NOT NULL,
    platform    TEXT        NOT NULL CHECK (platform IN ('wechat', 'qq', 'telegram', 'xiaohongshu', 'douyin', 'weibo', 'other')),
    handle      TEXT        NOT NULL,
    remark      TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    FOREIGN KEY (account_id, customer_id) REFERENCES customers (account_id, id) ON DELETE CASCADE
);

CREATE INDEX social_identities_account_customer_idx
    ON social_identities (account_id, customer_id);

CREATE INDEX social_identities_account_handle_idx
    ON social_identities (account_id, handle);
