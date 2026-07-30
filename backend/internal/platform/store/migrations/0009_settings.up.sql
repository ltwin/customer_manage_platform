CREATE TABLE settings (
    account_id           TEXT PRIMARY KEY REFERENCES accounts (id),
    timezone             TEXT NOT NULL DEFAULT 'Asia/Shanghai',
    birthday_lead_days   INTEGER NOT NULL DEFAULT 3 CHECK (birthday_lead_days >= 1),
    follow_up_after_days INTEGER NOT NULL DEFAULT 7 CHECK (follow_up_after_days >= 1),
    churn_thresholds     JSONB NOT NULL DEFAULT '[]'::jsonb,
    digest_hour          INTEGER NOT NULL DEFAULT 9 CHECK (digest_hour >= 0 AND digest_hour <= 23),
    telegram_chat_id     TEXT,
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);
