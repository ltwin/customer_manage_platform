-- ITEM-6 S2: assignment Inbox / universal resolution / quarantine / reconcile epoch.
-- Extends planning_reminder_generation_resolutions (0019 stub) to the closed enum.

-- 1) Expand universal generation resolutions ---------------------------------

ALTER TABLE planning_reminder_generation_resolutions
    RENAME COLUMN resolution TO resolution_kind;

ALTER TABLE planning_reminder_generation_resolutions
    RENAME COLUMN created_at TO resolved_at;

ALTER TABLE planning_reminder_generation_resolutions
    DROP CONSTRAINT IF EXISTS planning_reminder_generation_resolutions_resolution_check;

ALTER TABLE planning_reminder_generation_resolutions
    ADD COLUMN mutation_kind TEXT,
    ADD COLUMN source_event_id TEXT;

UPDATE planning_reminder_generation_resolutions r
SET mutation_kind = w.mutation_kind,
    source_event_id = w.source_event_id
FROM planning_reminder_generation_work w
WHERE w.account_id = r.account_id
  AND w.generation = r.generation;

ALTER TABLE planning_reminder_generation_resolutions
    ALTER COLUMN mutation_kind SET NOT NULL;

ALTER TABLE planning_reminder_generation_resolutions
    ADD CONSTRAINT planning_reminder_generation_resolutions_resolution_kind_check
    CHECK (resolution_kind IN (
        'event_applied',
        'rebuild_verified',
        'lifecycle_applied',
        'settings_rebuilt',
        'temporal_applied'
    ));

ALTER TABLE planning_reminder_generation_resolutions
    ADD CONSTRAINT planning_reminder_generation_resolutions_mutation_kind_check
    CHECK (mutation_kind IN (
        'crm_plan_link_changed',
        'crm_order_lifecycle_changed',
        'crm_schedule_changed',
        'settings_timezone_changed',
        'plan_archived',
        'assignment_activated',
        'assignment_revoked',
        'shoot_started'
    ));

ALTER TABLE planning_reminder_generation_resolutions
    ADD CONSTRAINT planning_reminder_generation_resolutions_source_event_check
    CHECK (
        (resolution_kind IN ('event_applied', 'rebuild_verified')
            AND source_event_id IS NOT NULL
            AND mutation_kind IN ('assignment_activated', 'assignment_revoked'))
        OR (resolution_kind NOT IN ('event_applied', 'rebuild_verified')
            AND source_event_id IS NULL
            AND mutation_kind NOT IN ('assignment_activated', 'assignment_revoked'))
    );

-- Matching plan/kind/source_event identity is enforced by writers against the
-- generation work row (PK FK already binds account_id+generation). A composite
-- FK including nullable source_event_id would be MATCH SIMPLE and skip checks.

-- 2) Assignment Inbox (durable consume ack; no body/nickname/token/receipt) ---

CREATE TABLE plan_assignment_reminder_inbox (
    account_id                 TEXT NOT NULL REFERENCES accounts (id),
    source_event_id            TEXT NOT NULL,
    plan_id                    TEXT NOT NULL,
    assignment_id              TEXT NOT NULL,
    assignment_revision        BIGINT NOT NULL CHECK (assignment_revision >= 1),
    account_source_generation  BIGINT NOT NULL CHECK (account_source_generation >= 1),
    event_kind                 TEXT NOT NULL CHECK (event_kind IN ('assignment_activated', 'assignment_revoked')),
    payload_fingerprint        TEXT NOT NULL CHECK (char_length(payload_fingerprint) BETWEEN 1 AND 128),
    resolution_kind            TEXT NOT NULL CHECK (resolution_kind IN ('event_applied', 'rebuild_verified')),
    consumed_at                TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (account_id, source_event_id),
    UNIQUE (account_id, source_event_id),
    FOREIGN KEY (account_id, plan_id) REFERENCES shoot_plans (account_id, id),
    FOREIGN KEY (account_id, assignment_id) REFERENCES share_assignments (account_id, id),
    FOREIGN KEY (account_id, account_source_generation, plan_id, source_event_id, event_kind)
        REFERENCES planning_reminder_generation_work
            (account_id, generation, plan_id, source_event_id, mutation_kind),
    FOREIGN KEY (account_id, source_event_id)
        REFERENCES share_assignment_source_event_v1 (account_id, event_id)
);

CREATE INDEX plan_assignment_reminder_inbox_plan_idx
    ON plan_assignment_reminder_inbox (account_id, plan_id, consumed_at DESC);

-- 3) Durable quarantine (no body; lease/backoff) ------------------------------

CREATE TABLE plan_assignment_reminder_quarantines (
    account_id                 TEXT NOT NULL REFERENCES accounts (id),
    source_event_id            TEXT NOT NULL,
    plan_id                    TEXT NOT NULL,
    assignment_id              TEXT NOT NULL,
    assignment_revision        BIGINT NOT NULL CHECK (assignment_revision >= 1),
    account_source_generation  BIGINT NOT NULL CHECK (account_source_generation >= 1),
    error_code                 TEXT NOT NULL CHECK (char_length(error_code) BETWEEN 1 AND 64),
    payload_fingerprint        TEXT NOT NULL CHECK (char_length(payload_fingerprint) BETWEEN 1 AND 128),
    attempt_count              INTEGER NOT NULL DEFAULT 1 CHECK (attempt_count >= 1),
    next_retry_at              TIMESTAMPTZ NOT NULL,
    lease_owner                TEXT,
    lease_until                TIMESTAMPTZ,
    quarantined_at             TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    resolved_at                TIMESTAMPTZ,
    resolution_kind            TEXT CHECK (
                                   resolution_kind IS NULL
                                   OR resolution_kind IN ('rebuild_verified')
                               ),
    PRIMARY KEY (account_id, source_event_id),
    FOREIGN KEY (account_id, plan_id) REFERENCES shoot_plans (account_id, id),
    FOREIGN KEY (account_id, assignment_id) REFERENCES share_assignments (account_id, id),
    FOREIGN KEY (account_id, source_event_id)
        REFERENCES share_assignment_source_event_v1 (account_id, event_id),
    CHECK (
        (resolved_at IS NULL AND resolution_kind IS NULL)
        OR (resolved_at IS NOT NULL AND resolution_kind IS NOT NULL)
    ),
    CHECK (
        (lease_owner IS NULL AND lease_until IS NULL)
        OR (lease_owner IS NOT NULL AND lease_until IS NOT NULL)
    )
);

CREATE UNIQUE INDEX plan_assignment_reminder_quarantines_unresolved_uniq
    ON plan_assignment_reminder_quarantines (account_id, source_event_id)
    WHERE resolved_at IS NULL;

CREATE INDEX plan_assignment_reminder_quarantines_retry_idx
    ON plan_assignment_reminder_quarantines (account_id, next_retry_at)
    WHERE resolved_at IS NULL;

-- 4) Reconciliation epoch + durable plan work set ----------------------------

CREATE TABLE plan_assignment_reminder_reconcile_epochs (
    epoch_id           TEXT NOT NULL,
    account_id         TEXT NOT NULL REFERENCES accounts (id),
    target_generation  BIGINT NOT NULL CHECK (target_generation >= 0),
    status             TEXT NOT NULL CHECK (status IN ('open', 'complete', 'superseded')),
    created_at         TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    completed_at       TIMESTAMPTZ,
    PRIMARY KEY (account_id, epoch_id),
    UNIQUE (account_id, epoch_id),
    CHECK (
        (status = 'open' AND completed_at IS NULL)
        OR (status IN ('complete', 'superseded') AND completed_at IS NOT NULL)
    )
);

CREATE INDEX plan_assignment_reminder_reconcile_epochs_status_idx
    ON plan_assignment_reminder_reconcile_epochs (account_id, status, created_at DESC);

CREATE TABLE plan_assignment_reminder_reconcile_epoch_plans (
    account_id    TEXT NOT NULL,
    epoch_id      TEXT NOT NULL,
    plan_id       TEXT NOT NULL,
    state         TEXT NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'leased', 'done')),
    lease_owner   TEXT,
    lease_until   TIMESTAMPTZ,
    completed_at  TIMESTAMPTZ,
    PRIMARY KEY (account_id, epoch_id, plan_id),
    FOREIGN KEY (account_id, epoch_id)
        REFERENCES plan_assignment_reminder_reconcile_epochs (account_id, epoch_id),
    FOREIGN KEY (account_id, plan_id) REFERENCES shoot_plans (account_id, id),
    CHECK (
        (state = 'pending' AND lease_owner IS NULL AND lease_until IS NULL AND completed_at IS NULL)
        OR (state = 'leased' AND lease_owner IS NOT NULL AND lease_until IS NOT NULL AND completed_at IS NULL)
        OR (state = 'done' AND completed_at IS NOT NULL)
    )
);

CREATE INDEX plan_assignment_reminder_reconcile_epoch_plans_claim_idx
    ON plan_assignment_reminder_reconcile_epoch_plans (account_id, epoch_id, state, plan_id);
