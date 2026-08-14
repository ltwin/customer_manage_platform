-- Reverse ITEM-6 S2 ingestion tables; refuse if unresolved quarantine or open epochs remain.

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM plan_assignment_reminder_quarantines WHERE resolved_at IS NULL
    ) THEN
        RAISE EXCEPTION
            'populated plan-assignment-reminder-ingestion down refused: resolve quarantines first';
    END IF;
    IF EXISTS (
        SELECT 1 FROM plan_assignment_reminder_reconcile_epochs WHERE status = 'open'
    ) THEN
        RAISE EXCEPTION
            'populated plan-assignment-reminder-ingestion down refused: close open epochs first';
    END IF;
    IF EXISTS (
        SELECT 1 FROM plan_assignment_reminder_inbox
    ) THEN
        RAISE EXCEPTION
            'populated plan-assignment-reminder-ingestion down refused: delete inbox rows first';
    END IF;
END $$;

DROP TABLE IF EXISTS plan_assignment_reminder_reconcile_epoch_plans;
DROP TABLE IF EXISTS plan_assignment_reminder_reconcile_epochs;
DROP TABLE IF EXISTS plan_assignment_reminder_quarantines;
DROP TABLE IF EXISTS plan_assignment_reminder_inbox;

ALTER TABLE planning_reminder_generation_resolutions
    DROP CONSTRAINT IF EXISTS planning_reminder_generation_resolutions_source_event_check;
ALTER TABLE planning_reminder_generation_resolutions
    DROP CONSTRAINT IF EXISTS planning_reminder_generation_resolutions_mutation_kind_check;
ALTER TABLE planning_reminder_generation_resolutions
    DROP CONSTRAINT IF EXISTS planning_reminder_generation_resolutions_resolution_kind_check;

ALTER TABLE planning_reminder_generation_resolutions
    DROP COLUMN IF EXISTS source_event_id;
ALTER TABLE planning_reminder_generation_resolutions
    DROP COLUMN IF EXISTS mutation_kind;

ALTER TABLE planning_reminder_generation_resolutions
    RENAME COLUMN resolution_kind TO resolution;
ALTER TABLE planning_reminder_generation_resolutions
    RENAME COLUMN resolved_at TO created_at;

ALTER TABLE planning_reminder_generation_resolutions
    ADD CONSTRAINT planning_reminder_generation_resolutions_resolution_check
    CHECK (resolution = 'lifecycle_applied');
