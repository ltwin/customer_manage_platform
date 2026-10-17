-- Scene snapshots have no representation without this kind; refuse instead of dropping them.
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM creative_contents WHERE kind = 'scene') THEN
  RAISE EXCEPTION 'scene documents exist; export or remove them before rollback';
 END IF;
END $$;
ALTER TABLE creative_contents DROP CONSTRAINT creative_contents_kind_check;
ALTER TABLE creative_contents ADD CONSTRAINT creative_contents_kind_check CHECK (kind IN ('text','link','image','video','audio'));
