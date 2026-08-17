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
        'plan-share.assignment-self-revoke.v1',
        'shoot-plan.business-facts.v1',
        'shoot-plan.business-drafts.generate.v1',
        'shoot-plan.business-draft.decision.v1'
    ));

ALTER TABLE settings
    ADD COLUMN planning_business_rule_overrides JSONB NOT NULL DEFAULT '{}'::jsonb
        CHECK (jsonb_typeof(planning_business_rule_overrides) = 'object'),
    ADD COLUMN planning_business_rule_revision BIGINT NOT NULL DEFAULT 0
        CHECK (planning_business_rule_revision >= 0);

CREATE TABLE account_settings_mutation_fences (
    account_id TEXT PRIMARY KEY REFERENCES accounts (id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);

CREATE TABLE planning_business_facts (
    account_id                 TEXT NOT NULL,
    plan_id                    TEXT NOT NULL,
    rented_location_count      INTEGER CHECK (rented_location_count BETWEEN 0 AND 100),
    assistant_count            INTEGER CHECK (assistant_count BETWEEN 0 AND 100),
    retouched_photo_count      INTEGER CHECK (retouched_photo_count BETWEEN 0 AND 100000),
    estimated_duration_minutes INTEGER CHECK (estimated_duration_minutes BETWEEN 0 AND 10080),
    revision                   BIGINT NOT NULL DEFAULT 1 CHECK (revision >= 1),
    updated_at                 TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (account_id, plan_id),
    FOREIGN KEY (account_id, plan_id) REFERENCES shoot_plans (account_id, id)
);

CREATE TABLE planning_business_drafts (
    id                       TEXT PRIMARY KEY,
    account_id               TEXT NOT NULL,
    plan_id                  TEXT NOT NULL,
    generation_id            TEXT NOT NULL,
    kind                     TEXT NOT NULL CHECK (kind IN ('order_adjustment', 'schedule_duration')),
    source_snapshot          JSONB NOT NULL CHECK (jsonb_typeof(source_snapshot) = 'object'),
    payload                  JSONB NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    terminal_status          TEXT NOT NULL DEFAULT 'fresh' CHECK (terminal_status IN ('fresh', 'applied', 'dismissed')),
    revision                 BIGINT NOT NULL DEFAULT 1 CHECK (revision >= 1),
    superseded_by_draft_id   TEXT,
    applied_adjustment_id    TEXT,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    expires_at               TIMESTAMPTZ NOT NULL,
    applied_at               TIMESTAMPTZ,
    dismissed_at             TIMESTAMPTZ,
    UNIQUE (account_id, id),
    UNIQUE (account_id, plan_id, generation_id, kind),
    FOREIGN KEY (account_id, plan_id) REFERENCES shoot_plans (account_id, id),
    FOREIGN KEY (account_id, superseded_by_draft_id) REFERENCES planning_business_drafts (account_id, id),
    CHECK (expires_at > created_at),
    CHECK (
        (terminal_status = 'fresh' AND applied_at IS NULL AND dismissed_at IS NULL)
        OR (terminal_status = 'applied' AND applied_at IS NOT NULL AND dismissed_at IS NULL)
        OR (terminal_status = 'dismissed' AND dismissed_at IS NOT NULL AND applied_at IS NULL)
    )
);

CREATE INDEX planning_business_drafts_latest_idx
    ON planning_business_drafts (account_id, plan_id, kind, created_at DESC, id DESC);

CREATE TABLE order_price_adjustments (
    id                        TEXT PRIMARY KEY,
    account_id                TEXT NOT NULL,
    order_id                  TEXT NOT NULL,
    plan_id                   TEXT NOT NULL,
    draft_id                  TEXT NOT NULL,
    before_price              INTEGER CHECK (before_price IS NULL OR before_price >= 0),
    after_price               INTEGER NOT NULL CHECK (after_price >= 0),
    calculation_mode          TEXT NOT NULL CHECK (calculation_mode IN ('delta_from_base', 'absolute_target')),
    base_price                INTEGER CHECK (base_price IS NULL OR base_price >= 0),
    lines                     JSONB NOT NULL CHECK (jsonb_typeof(lines) = 'array'),
    warnings                  JSONB NOT NULL CHECK (jsonb_typeof(warnings) = 'array'),
    rule_version              TEXT NOT NULL,
    before_target_fingerprint TEXT NOT NULL CHECK (char_length(before_target_fingerprint) = 64),
    after_target_fingerprint  TEXT NOT NULL CHECK (char_length(after_target_fingerprint) = 64),
    applied_by_account_id     TEXT NOT NULL,
    applied_at                TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (account_id, id),
    UNIQUE (account_id, draft_id),
    FOREIGN KEY (account_id, order_id) REFERENCES orders (account_id, id),
    FOREIGN KEY (account_id, plan_id) REFERENCES shoot_plans (account_id, id),
    FOREIGN KEY (account_id, draft_id) REFERENCES planning_business_drafts (account_id, id),
    FOREIGN KEY (applied_by_account_id) REFERENCES accounts (id),
    CHECK (applied_by_account_id = account_id)
);

CREATE OR REPLACE FUNCTION reject_order_price_adjustment_mutation()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'order_price_adjustments is append-only';
END;
$$;

CREATE TRIGGER order_price_adjustments_append_only
BEFORE UPDATE OR DELETE ON order_price_adjustments
FOR EACH ROW EXECUTE FUNCTION reject_order_price_adjustment_mutation();

