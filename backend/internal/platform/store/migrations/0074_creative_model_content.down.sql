-- Models, model uploads and document-consumed candidates have no representation
-- without these values; refuse instead of dropping them.
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM creative_contents WHERE kind = 'model')
  OR EXISTS (SELECT 1 FROM creative_uploads WHERE declared_kind = 'model')
  OR EXISTS (SELECT 1 FROM creative_upload_candidates WHERE declared_kind = 'model' OR adopted_target_kind = 'document') THEN
  RAISE EXCEPTION 'model content exists; remove it before rollback';
 END IF;
END $$;
ALTER TABLE creative_upload_candidates DROP CONSTRAINT creative_upload_candidates_adopted_target_kind_check;
ALTER TABLE creative_upload_candidates ADD CONSTRAINT creative_upload_candidates_adopted_target_kind_check CHECK (adopted_target_kind IN ('asset','node'));
ALTER TABLE creative_upload_candidates DROP CONSTRAINT creative_upload_candidates_declared_kind_check;
ALTER TABLE creative_upload_candidates ADD CONSTRAINT creative_upload_candidates_declared_kind_check CHECK (declared_kind IN ('image','video','audio'));
ALTER TABLE creative_uploads DROP CONSTRAINT creative_uploads_declared_kind_check;
ALTER TABLE creative_uploads ADD CONSTRAINT creative_uploads_declared_kind_check CHECK (declared_kind IN ('image','video','audio'));
ALTER TABLE creative_contents DROP CONSTRAINT creative_contents_kind_check;
ALTER TABLE creative_contents ADD CONSTRAINT creative_contents_kind_check CHECK (kind IN ('text','link','image','video','audio','scene'));
