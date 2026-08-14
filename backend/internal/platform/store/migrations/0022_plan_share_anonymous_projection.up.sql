CREATE TABLE share_asset_access_refs (
    id                   TEXT PRIMARY KEY,
    account_id           TEXT NOT NULL REFERENCES accounts (id),
    plan_id              TEXT NOT NULL,
    token_generation_id  TEXT NOT NULL,
    binding_id           TEXT NOT NULL,
    asset_id             TEXT NOT NULL,
    exact_generation     INTEGER NOT NULL CHECK (exact_generation >= 1),
    display_checksum     TEXT NOT NULL CHECK (char_length(display_checksum) BETWEEN 1 AND 128),
    ref                  TEXT NOT NULL,
    state                TEXT NOT NULL CHECK (state IN ('active', 'superseded')),
    created_at           TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    superseded_at        TIMESTAMPTZ,
    UNIQUE (account_id, id),
    UNIQUE (ref),
    UNIQUE (account_id, token_generation_id, binding_id, exact_generation, display_checksum),
    FOREIGN KEY (account_id, plan_id) REFERENCES shoot_plans (account_id, id),
    FOREIGN KEY (account_id, token_generation_id) REFERENCES share_generations (account_id, id),
    CHECK (
        (state = 'active' AND superseded_at IS NULL)
        OR (state = 'superseded' AND superseded_at IS NOT NULL)
    )
);

CREATE INDEX share_asset_access_refs_plan_idx
    ON share_asset_access_refs (account_id, plan_id, token_generation_id);

CREATE TABLE share_interaction_observations (
    id                       TEXT PRIMARY KEY,
    account_id               TEXT NOT NULL REFERENCES accounts (id),
    plan_id                  TEXT NOT NULL,
    token_generation_id      TEXT NOT NULL,
    slot_id                  TEXT,
    execution_window_revision BIGINT,
    kind                     TEXT NOT NULL CHECK (kind IN (
                                'full_open',
                                'plan_feedback',
                                'shot_feedback',
                                'assignment_claim',
                                'assignment_revoke'
                             )),
    source_fact_id           TEXT,
    source_fact_revision     BIGINT,
    idempotency_fingerprint  TEXT,
    policy_version           TEXT NOT NULL CHECK (policy_version = 'v1'),
    occurred_at              TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (account_id, id),
    FOREIGN KEY (account_id, plan_id) REFERENCES shoot_plans (account_id, id),
    FOREIGN KEY (account_id, token_generation_id) REFERENCES share_generations (account_id, id),
    CHECK (
        (kind = 'full_open' AND source_fact_id IS NULL AND source_fact_revision IS NULL)
        OR (kind IN ('plan_feedback', 'shot_feedback', 'assignment_claim', 'assignment_revoke')
            AND source_fact_id IS NOT NULL)
    )
);

-- full_open: at most one observation per token generation
CREATE UNIQUE INDEX share_interaction_observations_full_open_uniq
    ON share_interaction_observations (account_id, token_generation_id)
    WHERE kind = 'full_open';

-- feedback / assignment observations dedupe by source fact
CREATE UNIQUE INDEX share_interaction_observations_source_fact_uniq
    ON share_interaction_observations (account_id, kind, source_fact_id)
    WHERE source_fact_id IS NOT NULL;

CREATE INDEX share_interaction_observations_plan_idx
    ON share_interaction_observations (account_id, plan_id, occurred_at DESC);
