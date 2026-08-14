-- Transactional down only. Never use NoTransaction.
-- Writer exclusion is SHARE table locks, not migrator advisory lock.

LOCK TABLE planning_reminder_generation_work IN SHARE MODE;
LOCK TABLE share_assignment_source_event_v1 IN SHARE MODE;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM share_assignment_source_event_v1
        UNION ALL
        SELECT 1 FROM planning_reminder_generation_work
        WHERE mutation_kind IN ('assignment_activated', 'assignment_revoked')
    ) THEN
        RAISE EXCEPTION
            'populated planning-share assignment down refused: reset/restore required before dropping reciprocal event schema';
    END IF;
END $$;

ALTER TABLE planning_reminder_generation_work
    DROP CONSTRAINT fk_planning_reminder_assignment_work_to_planshare_event;

ALTER TABLE share_assignment_source_event_v1
    DROP CONSTRAINT fk_planshare_assignment_event_to_planning_reminder_work;

DROP TABLE share_assignment_source_event_v1;
DROP TABLE share_assignments;

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
        'plan-share.feedback-shot-create.v1'
    ));
