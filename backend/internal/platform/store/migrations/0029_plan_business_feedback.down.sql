DROP TRIGGER IF EXISTS order_price_adjustments_append_only ON order_price_adjustments;
DROP FUNCTION IF EXISTS reject_order_price_adjustment_mutation();
DROP TABLE IF EXISTS order_price_adjustments;
DROP INDEX IF EXISTS planning_business_drafts_latest_idx;
DROP TABLE IF EXISTS planning_business_drafts;
DROP TABLE IF EXISTS planning_business_facts;
DROP TABLE IF EXISTS account_settings_mutation_fences;

ALTER TABLE settings
    DROP COLUMN IF EXISTS planning_business_rule_revision,
    DROP COLUMN IF EXISTS planning_business_rule_overrides;

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
