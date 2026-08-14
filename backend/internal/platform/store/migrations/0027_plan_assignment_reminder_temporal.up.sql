-- ITEM-6 S3: temporal invalidation markers for shoot_started frontier.
-- No body / nickname; exact (account, plan, slot, valid_until) uniqueness.

CREATE TABLE plan_assignment_reminder_temporal_invalidations (
    account_id   TEXT NOT NULL REFERENCES accounts (id),
    plan_id      TEXT NOT NULL,
    slot_id      TEXT NOT NULL,
    valid_until  TIMESTAMPTZ NOT NULL,
    generation   BIGINT CHECK (generation IS NULL OR generation >= 1),
    state        TEXT NOT NULL CHECK (state IN ('pending', 'applied')),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    applied_at   TIMESTAMPTZ,
    PRIMARY KEY (account_id, plan_id, slot_id, valid_until),
    UNIQUE (account_id, plan_id, slot_id, valid_until),
    FOREIGN KEY (account_id, plan_id) REFERENCES shoot_plans (account_id, id),
    CHECK (
        (state = 'pending' AND applied_at IS NULL)
        OR (state = 'applied' AND generation IS NOT NULL AND applied_at IS NOT NULL)
    )
);

CREATE INDEX plan_assignment_reminder_temporal_invalidations_pending_idx
    ON plan_assignment_reminder_temporal_invalidations (account_id, state, valid_until)
    WHERE state = 'pending';
