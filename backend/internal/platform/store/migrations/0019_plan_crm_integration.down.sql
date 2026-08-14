DROP VIEW IF EXISTS planning_summary_by_slot;
DROP VIEW IF EXISTS planning_summary_by_order;
DROP VIEW IF EXISTS planning_summary_by_customer;

DROP VIEW IF EXISTS shoot_plan_list_projection;
CREATE VIEW shoot_plan_list_projection AS
SELECT p.account_id,
       p.id,
       p.title,
       p.subject,
       p.status,
       p.planned_look_count,
       p.planned_scene_count,
       p.revision,
       p.execution_fact_revision,
       p.created_at,
       p.updated_at,
       count(s.id)::BIGINT AS planned_shot_count
FROM shoot_plans p
LEFT JOIN shoot_plan_shots s
  ON s.account_id = p.account_id AND s.plan_id = p.id AND s.removed_at IS NULL
GROUP BY p.account_id, p.id;

DROP TABLE IF EXISTS planning_reminder_generation_resolutions;
DROP TABLE IF EXISTS plan_crm_connection_events;
DROP TABLE IF EXISTS plan_schedule_projections;
DROP TABLE IF EXISTS plan_crm_connections;

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
        'plan-ingestion.commit.v1'
    ));
