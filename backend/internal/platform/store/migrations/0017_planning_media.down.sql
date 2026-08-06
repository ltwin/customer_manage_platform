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
        'shoot-plan.batch-commit.v1'
    ));

DROP TABLE IF EXISTS planning_media_object_inventory;
DROP TABLE IF EXISTS planning_media_read_pins;
DROP TABLE IF EXISTS planning_media_leases;
DROP VIEW IF EXISTS planning_media_shot_access_refs CASCADE;
DROP TABLE IF EXISTS planning_media_bindings;
DROP VIEW IF EXISTS planning_media_asset_gallery CASCADE;
DROP TABLE IF EXISTS planning_media_renditions;
ALTER TABLE IF EXISTS planning_media_assets DROP CONSTRAINT IF EXISTS planning_media_assets_current_generation_fk;
DROP TABLE IF EXISTS planning_media_generations;
DROP TABLE IF EXISTS planning_media_rights_declarations;
DROP TABLE IF EXISTS planning_media_assets;
