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
        'plan-share.revoke.v1',
        'plan-share.feedback-disposition.v1',
        'plan-share.feedback-plan-create.v1',
        'plan-share.feedback-shot-create.v1',
        'plan-share.offer-create.v1',
        'plan-share.offer-close.v1',
        'plan-share.assignment-photographer-revoke.v1',
        'plan-share.assignment-claim.v1',
        'plan-share.assignment-self-revoke.v1'
    ));

CREATE TABLE share_assignments (
    id                              TEXT PRIMARY KEY,
    account_id                      TEXT NOT NULL REFERENCES accounts (id),
    plan_id                         TEXT NOT NULL,
    token_generation_id             TEXT NOT NULL,
    assignment_kind                 TEXT NOT NULL CHECK (assignment_kind IN ('readiness', 'on_site_support')),
    readiness_item_id               TEXT,
    offer_id                        TEXT,
    content_snapshot                TEXT NOT NULL CHECK (char_length(content_snapshot) BETWEEN 1 AND 2000),
    claimed_by_display_name         TEXT NOT NULL CHECK (char_length(claimed_by_display_name) BETWEEN 1 AND 40),
    preparation_lead_days_snapshot  INTEGER CHECK (
                                        preparation_lead_days_snapshot IS NULL
                                        OR (preparation_lead_days_snapshot >= 0 AND preparation_lead_days_snapshot <= 365)
                                    ),
    lead_rule_version               TEXT CHECK (
                                        lead_rule_version IS NULL
                                        OR char_length(lead_rule_version) BETWEEN 1 AND 64
                                    ),
    status                          TEXT NOT NULL CHECK (status IN ('active', 'revoked')),
    claim_receipt_commitment        BYTEA NOT NULL CHECK (octet_length(claim_receipt_commitment) = 32),
    revision                        BIGINT NOT NULL DEFAULT 1 CHECK (revision >= 1),
    claimed_at                      TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    revoked_at                      TIMESTAMPTZ,
    revoked_by                      TEXT CHECK (revoked_by IS NULL OR revoked_by IN ('anonymous', 'photographer')),
    UNIQUE (account_id, id),
    FOREIGN KEY (account_id, plan_id) REFERENCES shoot_plans (account_id, id),
    FOREIGN KEY (account_id, token_generation_id) REFERENCES share_generations (account_id, id),
    FOREIGN KEY (account_id, offer_id) REFERENCES share_assignment_offers (account_id, id),
    CHECK (
        (assignment_kind = 'readiness'
            AND readiness_item_id IS NOT NULL
            AND offer_id IS NULL
            AND preparation_lead_days_snapshot IS NOT NULL
            AND lead_rule_version IS NOT NULL)
        OR (assignment_kind = 'on_site_support'
            AND offer_id IS NOT NULL
            AND readiness_item_id IS NULL
            AND preparation_lead_days_snapshot IS NULL
            AND lead_rule_version IS NULL)
    ),
    CHECK (
        (status = 'active' AND revoked_at IS NULL AND revoked_by IS NULL)
        OR (status = 'revoked' AND revoked_at IS NOT NULL AND revoked_by IS NOT NULL)
    )
);

CREATE UNIQUE INDEX share_assignments_active_target_uniq
    ON share_assignments (
        account_id,
        plan_id,
        assignment_kind,
        COALESCE(readiness_item_id, ''),
        COALESCE(offer_id, '')
    )
    WHERE status = 'active';

CREATE INDEX share_assignments_plan_claimed_idx
    ON share_assignments (account_id, plan_id, claimed_at DESC, id DESC);

CREATE INDEX share_assignments_active_readiness_idx
    ON share_assignments (account_id, plan_id, readiness_item_id)
    WHERE status = 'active' AND assignment_kind = 'readiness';

CREATE TABLE share_assignment_source_event_v1 (
    event_id                        TEXT NOT NULL,
    account_id                      TEXT NOT NULL REFERENCES accounts (id),
    plan_id                         TEXT NOT NULL,
    assignment_id                   TEXT NOT NULL,
    assignment_revision             BIGINT NOT NULL CHECK (assignment_revision >= 1),
    account_source_generation       BIGINT NOT NULL CHECK (account_source_generation >= 1),
    event_kind                      TEXT NOT NULL CHECK (event_kind IN ('assignment_activated', 'assignment_revoked')),
    assignment_kind                 TEXT NOT NULL CHECK (assignment_kind IN ('readiness', 'on_site_support')),
    readiness_item_id               TEXT,
    preparation_lead_days_snapshot  INTEGER CHECK (
                                        preparation_lead_days_snapshot IS NULL
                                        OR (preparation_lead_days_snapshot >= 0 AND preparation_lead_days_snapshot <= 365)
                                    ),
    lead_rule_version               TEXT CHECK (
                                        lead_rule_version IS NULL
                                        OR char_length(lead_rule_version) BETWEEN 1 AND 64
                                    ),
    content_fingerprint             TEXT NOT NULL CHECK (char_length(content_fingerprint) BETWEEN 1 AND 128),
    occurred_at                     TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    source_version                  INTEGER NOT NULL DEFAULT 1 CHECK (source_version = 1),
    PRIMARY KEY (account_id, event_id),
    UNIQUE (account_id, account_source_generation),
    UNIQUE (account_id, account_source_generation, plan_id, event_id, event_kind),
    UNIQUE (account_id, plan_id, assignment_id, assignment_revision, event_kind),
    FOREIGN KEY (account_id, plan_id) REFERENCES shoot_plans (account_id, id),
    FOREIGN KEY (account_id, assignment_id) REFERENCES share_assignments (account_id, id),
    CHECK (
        (assignment_kind = 'readiness'
            AND readiness_item_id IS NOT NULL
            AND preparation_lead_days_snapshot IS NOT NULL
            AND lead_rule_version IS NOT NULL)
        OR (assignment_kind = 'on_site_support'
            AND readiness_item_id IS NULL
            AND preparation_lead_days_snapshot IS NULL
            AND lead_rule_version IS NULL)
    )
);

CREATE INDEX share_assignment_source_event_v1_plan_idx
    ON share_assignment_source_event_v1 (account_id, plan_id, occurred_at DESC);

ALTER TABLE share_assignment_source_event_v1
    ADD CONSTRAINT fk_planshare_assignment_event_to_planning_reminder_work
    FOREIGN KEY (account_id, account_source_generation, plan_id, event_id, event_kind)
    REFERENCES planning_reminder_generation_work
        (account_id, generation, plan_id, source_event_id, mutation_kind)
    DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE planning_reminder_generation_work
    ADD CONSTRAINT fk_planning_reminder_assignment_work_to_planshare_event
    FOREIGN KEY (account_id, generation, plan_id, source_event_id, mutation_kind)
    REFERENCES share_assignment_source_event_v1
        (account_id, account_source_generation, plan_id, event_id, event_kind)
    DEFERRABLE INITIALLY DEFERRED;
