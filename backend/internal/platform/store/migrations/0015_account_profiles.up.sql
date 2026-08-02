CREATE TABLE account_profiles (
    account_id        TEXT PRIMARY KEY REFERENCES accounts (id),
    display_name      TEXT,
    profile_revision  BIGINT NOT NULL DEFAULT 0 CHECK (profile_revision >= 0),
    avatar_revision   BIGINT NOT NULL DEFAULT 0 CHECK (avatar_revision >= 0),
    avatar_version    TEXT,
    avatar_object_id  TEXT,
    avatar_media_type TEXT,
    avatar_size       BIGINT,
    avatar_updated_at TIMESTAMPTZ,
    updated_at        TIMESTAMPTZ NOT NULL,
    CONSTRAINT account_profiles_avatar_pointer_complete_check CHECK (
        (
            avatar_version IS NULL
            AND avatar_object_id IS NULL
            AND avatar_media_type IS NULL
            AND avatar_size IS NULL
            AND avatar_updated_at IS NULL
        )
        OR
        (
            avatar_version IS NOT NULL
            AND avatar_object_id IS NOT NULL
            AND avatar_media_type IS NOT NULL
            AND avatar_size IS NOT NULL
            AND avatar_updated_at IS NOT NULL
            AND avatar_version ~ '^sha256-[0-9a-f]{64}$'
            AND avatar_object_id ~ '^[0-9a-f]{32}$'
            AND avatar_media_type IN ('image/jpeg', 'image/png', 'image/webp')
            AND avatar_size > 0
            AND avatar_size <= 5242880
        )
    ),
    CONSTRAINT account_profiles_display_name_nonempty_check CHECK (
        display_name IS NULL OR length(btrim(display_name)) > 0
    )
);

CREATE TABLE account_profile_avatar_gc (
    account_id         TEXT        NOT NULL REFERENCES accounts (id),
    avatar_version     TEXT        NOT NULL CHECK (avatar_version ~ '^sha256-[0-9a-f]{64}$'),
    avatar_object_id   TEXT        NOT NULL CHECK (avatar_object_id ~ '^[0-9a-f]{32}$'),
    not_before         TIMESTAMPTZ NOT NULL,
    next_attempt_at    TIMESTAMPTZ NOT NULL,
    attempts           INTEGER     NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    last_error_class   TEXT,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (account_id, avatar_object_id)
);

CREATE INDEX account_profile_avatar_gc_due_idx
    ON account_profile_avatar_gc (account_id, next_attempt_at, avatar_object_id);
