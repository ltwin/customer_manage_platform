DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM creative_uploads WHERE target_kind='staged') THEN
  RAISE EXCEPTION 'staged uploads exist; drain them before rollback';
 END IF;
END $$;
ALTER TABLE creative_uploads DROP CONSTRAINT creative_uploads_target_kind_check;
ALTER TABLE creative_uploads ADD CONSTRAINT creative_uploads_target_kind_check CHECK(target_kind IN ('asset','node'));
