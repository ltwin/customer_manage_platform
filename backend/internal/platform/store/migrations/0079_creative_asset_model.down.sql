-- Model assets have no representation without this value; refuse instead of
-- dropping them.
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM creative_assets WHERE kind = 'model') THEN
  RAISE EXCEPTION 'model assets exist; remove them before rollback';
 END IF;
END $$;
ALTER TABLE creative_assets DROP CONSTRAINT creative_assets_kind_check;
ALTER TABLE creative_assets ADD CONSTRAINT creative_assets_kind_check CHECK (kind IN ('text','link','image','video','audio'));
