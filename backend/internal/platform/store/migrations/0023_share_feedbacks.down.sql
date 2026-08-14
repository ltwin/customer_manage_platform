DROP TABLE IF EXISTS share_replay_admissions;
DROP TABLE IF EXISTS share_feedbacks;

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
