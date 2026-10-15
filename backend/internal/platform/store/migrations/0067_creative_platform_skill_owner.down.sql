-- Which account used to hold the platform catalog is not recorded anywhere, so
-- moving it back would be a guess. Rolling back is only allowed while the
-- reserved owner holds nothing.
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM creative_skills WHERE account_id='platform')
  OR EXISTS (SELECT 1 FROM creative_skill_imports WHERE account_id='platform') THEN
  RAISE EXCEPTION 'platform skills must be moved to an account explicitly before rollback';
 END IF;
END $$;
