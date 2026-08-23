DROP INDEX IF EXISTS orders_account_delivery_due_idx;

ALTER TABLE orders
    DROP COLUMN IF EXISTS delivery_due_is_override,
    DROP COLUMN IF EXISTS delivery_due_at;

ALTER TABLE settings DROP COLUMN IF EXISTS delivery_sla_days;
