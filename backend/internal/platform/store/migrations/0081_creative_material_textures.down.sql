-- Materials and texture holders have no representation without this
-- migration; refuse instead of dropping them.
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM creative_contents WHERE kind = 'material')
  OR EXISTS (SELECT 1 FROM creative_assets WHERE kind = 'material')
  OR EXISTS (SELECT 1 FROM creative_content_revision_refs) THEN
  RAISE EXCEPTION 'material content or texture holders exist; remove them before rollback';
 END IF;
END $$;
DROP TABLE creative_content_revision_refs;
ALTER TABLE creative_assets DROP CONSTRAINT creative_assets_kind_check;
ALTER TABLE creative_assets ADD CONSTRAINT creative_assets_kind_check CHECK (kind IN ('text','link','image','video','audio','model'));
ALTER TABLE creative_contents DROP CONSTRAINT creative_contents_kind_check;
ALTER TABLE creative_contents ADD CONSTRAINT creative_contents_kind_check CHECK (kind IN ('text','link','image','video','audio','scene','model'));
