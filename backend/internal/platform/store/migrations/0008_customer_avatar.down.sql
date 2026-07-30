DROP TABLE avatar_reconciliation_checkpoint;
DROP TABLE avatar_object_gc;

ALTER TABLE customers
    DROP CONSTRAINT customers_avatar_pointer_complete_check,
    DROP COLUMN avatar_updated_at,
    DROP COLUMN avatar_size,
    DROP COLUMN avatar_media_type,
    DROP COLUMN avatar_object_id,
    DROP COLUMN avatar_version,
    DROP COLUMN avatar_revision;
