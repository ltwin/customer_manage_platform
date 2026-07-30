CREATE TABLE packages (
    id                 TEXT        PRIMARY KEY,
    account_id         TEXT        NOT NULL REFERENCES accounts (id),
    name               TEXT        NOT NULL CHECK (char_length(name) >= 1),
    shoot_type         TEXT        NOT NULL CHECK (shoot_type IN ('portrait', 'cosplay', 'other')),
    pricing_mode       TEXT        NOT NULL CHECK (pricing_mode IN ('per_duration', 'per_photo', 'fixed')),
    base_price         INTEGER     NOT NULL CHECK (base_price >= 0),
    duration_minutes   INTEGER     CHECK (duration_minutes IS NULL OR duration_minutes >= 0),
    shot_count_min     INTEGER     CHECK (shot_count_min IS NULL OR shot_count_min >= 0),
    shot_count_max     INTEGER     CHECK (shot_count_max IS NULL OR shot_count_max >= 0),
    raw_delivery_count INTEGER     CHECK (raw_delivery_count IS NULL OR raw_delivery_count >= 0),
    retouch_count      INTEGER     CHECK (retouch_count IS NULL OR retouch_count >= 0),
    note               TEXT        CHECK (note IS NULL OR char_length(note) <= 500),
    status             TEXT        NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'archived')),
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (account_id, id),
    CHECK (shot_count_min IS NULL OR shot_count_max IS NULL OR shot_count_min <= shot_count_max)
);

CREATE INDEX packages_account_status_created_idx
    ON packages (account_id, status, created_at DESC, id DESC);
