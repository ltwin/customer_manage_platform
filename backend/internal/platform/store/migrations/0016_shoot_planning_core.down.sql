DROP VIEW IF EXISTS shoot_plan_list_projection;
ALTER TABLE idempotency_records
    DROP CONSTRAINT IF EXISTS idempotency_records_operation_check;
ALTER TABLE idempotency_records
    ADD CONSTRAINT idempotency_records_operation_check
    CHECK (operation IN ('order.create.v1', 'schedule-slot.create.v1'));
DROP TABLE IF EXISTS planning_archive_capability_state;
DROP TABLE IF EXISTS planning_reminder_generation_work;
DROP TABLE IF EXISTS planning_reminder_account_generations;
DROP TABLE IF EXISTS shoot_plan_finalization_snapshots;
DROP TABLE IF EXISTS shoot_plan_execution_event_voids;
ALTER TABLE IF EXISTS shoot_plan_shots DROP CONSTRAINT IF EXISTS shoot_plan_shots_current_outcome_fk;
DROP TABLE IF EXISTS shoot_plan_execution_events;
DROP TABLE IF EXISTS shoot_plan_run_sessions;
DROP TABLE IF EXISTS shoot_plan_execution_windows;
DROP TABLE IF EXISTS shoot_plan_shot_readiness_links;
DROP TABLE IF EXISTS shoot_plan_readiness_items;
DROP TABLE IF EXISTS shoot_plan_shots;
DROP TABLE IF EXISTS shoot_plans;
