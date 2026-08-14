ALTER TABLE idempotency_records
    DROP CONSTRAINT idempotency_records_operation_check;
ALTER TABLE idempotency_records
    ADD CONSTRAINT idempotency_records_operation_check CHECK (operation IN (
        'order.create.v1',
        'schedule-slot.create.v1',
        'shoot-plan.create.v1',
        'shoot-plan.command.v1',
        'shoot-plan.transition.v1',
        'shoot-plan.run-session.open.v1',
        'shoot-plan.shot.capture.v1',
        'shoot-plan.execution-event.void.v1',
        'shoot-plan.batch-commit.v1',
        'planning-media.upload.v1',
        'planning-media.binding.create.v1',
        'planning-media.binding.release.v1',
        'planning-media.lease.reserve.v1',
        'planning-media.lease.release.v1',
        'plan-ingestion.create.v1',
        'plan-ingestion.preview.v1',
        'plan-ingestion.transition.v1',
        'plan-ingestion.commit.v1',
        'shoot-plan.crm-link.v1',
        'plan-share.issue.v1',
        'plan-share.rotate.v1',
        'plan-share.revoke.v1'
    ));

CREATE TABLE share_generations (
    id                          TEXT PRIMARY KEY,
    account_id                  TEXT NOT NULL REFERENCES accounts (id),
    plan_id                     TEXT NOT NULL,
    view_level                  TEXT NOT NULL CHECK (view_level IN ('proposal', 'full')),
    generation                  BIGINT NOT NULL CHECK (generation >= 1),
    selector                    TEXT NOT NULL,
    secret_commitment           BYTEA NOT NULL CHECK (octet_length(secret_commitment) = 32),
    fingerprint                 TEXT NOT NULL CHECK (char_length(fingerprint) BETWEEN 1 AND 128),
    eligibility_link_epoch_id   TEXT,
    state                       TEXT NOT NULL CHECK (state IN (
                                    'active', 'rotated', 'revoked', 'eligibility_invalidated'
                                )),
    expires_at                  TIMESTAMPTZ NOT NULL,
    issued_at                   TIMESTAMPTZ NOT NULL,
    rotated_at                  TIMESTAMPTZ,
    revoked_at                  TIMESTAMPTZ,
    invalidated_at              TIMESTAMPTZ,
    first_opened_at             TIMESTAMPTZ,
    revision                    BIGINT NOT NULL DEFAULT 1 CHECK (revision >= 1),
    UNIQUE (account_id, id),
    UNIQUE (selector),
    UNIQUE (account_id, plan_id, view_level, generation),
    FOREIGN KEY (account_id, plan_id) REFERENCES shoot_plans (account_id, id),
    CHECK (
        (view_level = 'proposal' AND eligibility_link_epoch_id IS NULL)
        OR (view_level = 'full' AND eligibility_link_epoch_id IS NOT NULL)
    ),
    CHECK (
        (state = 'active' AND rotated_at IS NULL AND revoked_at IS NULL AND invalidated_at IS NULL)
        OR (state = 'rotated' AND rotated_at IS NOT NULL AND revoked_at IS NULL AND invalidated_at IS NULL)
        OR (state = 'revoked' AND revoked_at IS NOT NULL AND rotated_at IS NULL AND invalidated_at IS NULL)
        OR (state = 'eligibility_invalidated' AND invalidated_at IS NOT NULL
            AND rotated_at IS NULL AND revoked_at IS NULL)
    )
);

CREATE UNIQUE INDEX share_generations_active_plan_view_idx
    ON share_generations (account_id, plan_id, view_level)
    WHERE state = 'active';

CREATE INDEX share_generations_plan_view_generation_idx
    ON share_generations (account_id, plan_id, view_level, generation DESC);

CREATE TABLE share_assignment_offers (
    id               TEXT PRIMARY KEY,
    account_id       TEXT NOT NULL REFERENCES accounts (id),
    plan_id          TEXT NOT NULL,
    assignment_kind  TEXT NOT NULL CHECK (assignment_kind = 'on_site_support'),
    content          TEXT NOT NULL CHECK (char_length(btrim(content)) BETWEEN 1 AND 240),
    state            TEXT NOT NULL CHECK (state IN ('open', 'closed')),
    revision         BIGINT NOT NULL DEFAULT 1 CHECK (revision >= 1),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    closed_at        TIMESTAMPTZ,
    UNIQUE (account_id, id),
    FOREIGN KEY (account_id, plan_id) REFERENCES shoot_plans (account_id, id),
    CHECK (
        (state = 'open' AND closed_at IS NULL)
        OR (state = 'closed' AND closed_at IS NOT NULL)
    )
);

CREATE INDEX share_assignment_offers_plan_created_idx
    ON share_assignment_offers (account_id, plan_id, created_at DESC, id DESC);
