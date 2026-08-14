-- Drop S1 projection tables; refuse if checklist reminders still exist.

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM reminders WHERE type = 'plan_assignment_checklist'
    ) THEN
        RAISE EXCEPTION
            'populated plan-assignment-reminder down refused: delete plan_assignment_checklist reminders first';
    END IF;
END $$;

DROP TABLE IF EXISTS plan_assignment_reminder_members;
DROP TABLE IF EXISTS plan_assignment_reminder_groups;
DROP TABLE IF EXISTS plan_assignment_reminder_sources;

ALTER TABLE reminders
    DROP CONSTRAINT IF EXISTS reminders_plan_assignment_checklist_refs_check;
ALTER TABLE reminders
    DROP CONSTRAINT IF EXISTS reminders_plan_fk;
ALTER TABLE reminders
    DROP COLUMN IF EXISTS plan_id;

ALTER TABLE reminders
    DROP CONSTRAINT reminders_type_check;
ALTER TABLE reminders
    ADD CONSTRAINT reminders_type_check CHECK (type IN (
        'birthday',
        'follow_up',
        'churn',
        'custom'
    ));
