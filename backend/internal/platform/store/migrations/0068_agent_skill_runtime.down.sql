ALTER TABLE creative_agent_runs DROP COLUMN skill_discovery;
DROP TABLE creative_run_skill_searches;
DROP TABLE creative_run_skills;
-- Refuse rollback if new descriptions cannot fit; never truncate published data.
ALTER TABLE creative_skills DROP CONSTRAINT creative_skills_description_check;
ALTER TABLE creative_skills ADD CHECK (char_length(description) BETWEEN 1 AND 500);
ALTER TABLE creative_skill_versions DROP CONSTRAINT creative_skill_versions_description_snapshot_check;
ALTER TABLE creative_skill_versions ADD CHECK (char_length(description_snapshot) BETWEEN 1 AND 500);
ALTER TABLE creative_skills DROP CONSTRAINT creative_skills_slug_check;
ALTER TABLE creative_skills ADD CHECK (slug ~ '^[a-z][a-z0-9-]{0,63}$');
