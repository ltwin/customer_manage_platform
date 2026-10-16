-- Published version bytes are immutable. Only metadata capacity changes.
ALTER TABLE creative_skills DROP CONSTRAINT creative_skills_description_check;
ALTER TABLE creative_skills ADD CHECK (char_length(description) BETWEEN 1 AND 1024);
ALTER TABLE creative_skill_versions DROP CONSTRAINT creative_skill_versions_description_snapshot_check;
ALTER TABLE creative_skill_versions ADD CHECK (char_length(description_snapshot) BETWEEN 1 AND 1024);
CREATE TABLE creative_run_skills (
 account_id TEXT NOT NULL, run_id TEXT NOT NULL,
 skill_id TEXT NOT NULL, skill_version_id TEXT NOT NULL,
 digest TEXT NOT NULL CHECK (digest ~ '^[0-9a-f]{64}$'),
 source TEXT NOT NULL CHECK (source IN ('explicit','history','catalog','search')),
 metadata JSONB NOT NULL, snapshot JSONB,
 activated_at TIMESTAMPTZ, created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 CHECK ((activated_at IS NULL) = (snapshot IS NULL)),
 PRIMARY KEY (account_id,run_id,skill_version_id)
);
CREATE INDEX creative_run_skills_history ON creative_run_skills(account_id,run_id,activated_at) WHERE activated_at IS NOT NULL;
-- The initial candidate page must also survive a restart when it was empty.
ALTER TABLE creative_agent_runs ADD COLUMN skill_discovery JSONB;
CREATE TABLE creative_run_skill_searches (
 account_id TEXT NOT NULL, run_id TEXT NOT NULL, step_id TEXT NOT NULL,
 result JSONB NOT NULL, PRIMARY KEY(account_id,run_id,step_id)
);
-- Syntax is validated by the version-specific importer; permit standard Unicode names.
ALTER TABLE creative_skills DROP CONSTRAINT creative_skills_slug_check;
ALTER TABLE creative_skills ADD CHECK (char_length(slug) BETWEEN 1 AND 64);
