-- Reverse S4 digest intent / calling permit / binding revision additives.

DROP INDEX IF EXISTS delivery_send_attempt_permits_deadline_idx;
DROP INDEX IF EXISTS delivery_send_attempt_permits_one_calling_idx;
DROP TABLE IF EXISTS delivery_send_attempt_permits;

DROP INDEX IF EXISTS plan_assignment_reminder_digest_intents_delivery_idx;
DROP TABLE IF EXISTS plan_assignment_reminder_digest_intents;

ALTER TABLE telegram_deliveries
    DROP CONSTRAINT IF EXISTS telegram_deliveries_account_id_id_unique;

ALTER TABLE telegram_deliveries
    DROP COLUMN IF EXISTS active_intent_revision;

ALTER TABLE settings
    DROP COLUMN IF EXISTS telegram_binding_revision;
