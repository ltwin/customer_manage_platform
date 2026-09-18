ALTER TABLE creative_node_executions
 ADD COLUMN resume_token TEXT NOT NULL DEFAULT '' CHECK(length(resume_token)<=200),
 ADD COLUMN next_step_at TIMESTAMPTZ,
 ADD COLUMN result_payload JSONB CHECK(result_payload IS NULL OR jsonb_typeof(result_payload)='object');
