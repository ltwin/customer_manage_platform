-- A frozen version is the only copy of what a run was told to do, and a message
-- skill reference is the evidence of which text a photographer was shown.
-- Refuse a lossy rollback once either exists; export first.
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM creative_skill_versions)
  OR EXISTS(SELECT 1 FROM creative_message_skill_refs) THEN
  RAISE EXCEPTION 'frozen skill versions require export before rollback';
 END IF;
END $$;
DROP TABLE creative_message_skill_refs,creative_skill_imports,
 creative_skill_version_resources,creative_skill_versions,creative_skills;
