-- G3：部署控制、一次性派发、观察证据与输出定位。业务关联均由账号范围服务核验。
CREATE TABLE llm_generation_deployments (
 account_id TEXT NOT NULL, deployment_id TEXT NOT NULL,
 revision BIGINT NOT NULL CHECK(revision>0), enabled BOOLEAN NOT NULL,
 policy_digest TEXT NOT NULL, adapter_version TEXT NOT NULL,
 valid_until TIMESTAMPTZ NOT NULL,
 PRIMARY KEY(account_id,deployment_id)
);
ALTER TABLE llm_generation_requests
 ADD COLUMN control_revision BIGINT NOT NULL DEFAULT 0 CHECK(control_revision>=0),
 ADD COLUMN next_advance_at TIMESTAMPTZ,
 ADD COLUMN verification_required BOOLEAN NOT NULL DEFAULT FALSE;
CREATE INDEX llm_generation_due ON llm_generation_requests(account_id,next_advance_at,request_id)
 WHERE next_advance_at IS NOT NULL;
CREATE TABLE llm_generation_tasks (
 account_id TEXT NOT NULL, request_id TEXT NOT NULL, attempt_id TEXT NOT NULL,
 deployment_id TEXT NOT NULL, adapter_version TEXT NOT NULL,
 limit_key TEXT NOT NULL, task_cipher BYTEA, task_digest TEXT,
 state TEXT NOT NULL DEFAULT 'unknown' CHECK(state IN ('unknown','accepted','running','completed','rejected','failed','cancelled')),
 provider_revision TEXT, progress DOUBLE PRECISION CHECK(progress BETWEEN 0 AND 1),
 epoch BIGINT NOT NULL DEFAULT 1 CHECK(epoch>0), lease_until TIMESTAMPTZ,
 action TEXT NOT NULL CHECK(action IN ('submit','observe','cancel','lookup')),
 failures INTEGER NOT NULL DEFAULT 0 CHECK(failures BETWEEN 0 AND 5),
 cancel_sent BOOLEAN NOT NULL DEFAULT FALSE,
 created_at TIMESTAMPTZ NOT NULL,
 recovery_until TIMESTAMPTZ NOT NULL DEFAULT (clock_timestamp()+interval '24 hours'),
 PRIMARY KEY(account_id,attempt_id),
 CHECK((task_cipher IS NULL)=(task_digest IS NULL))
);
CREATE INDEX llm_generation_task_request ON llm_generation_tasks(account_id,request_id);
CREATE UNIQUE INDEX llm_generation_task_identity ON llm_generation_tasks(account_id,deployment_id,task_digest)
 WHERE task_digest IS NOT NULL;
CREATE TABLE llm_generation_observations (
 account_id TEXT NOT NULL, id TEXT NOT NULL, attempt_id TEXT NOT NULL,
 event_key TEXT NOT NULL CHECK(char_length(event_key) BETWEEN 1 AND 60),
 evidence_digest TEXT NOT NULL,
 evidence_cipher BYTEA NOT NULL CHECK(octet_length(evidence_cipher) BETWEEN 1 AND 1048576),
 claim_epoch BIGINT NOT NULL CHECK(claim_epoch>0),
 disposition TEXT NOT NULL CHECK(disposition IN ('pending','applied','stale','conflict')),
 created_at TIMESTAMPTZ NOT NULL,
 PRIMARY KEY(account_id,id),
 UNIQUE(account_id,attempt_id,event_key,evidence_digest)
);
-- 同事件只允许一份正常证据；不同摘要单独留存冲突，不覆写原始证据。
CREATE UNIQUE INDEX llm_generation_event_identity ON llm_generation_observations(account_id,attempt_id,event_key)
 WHERE disposition<>'conflict';
CREATE TABLE llm_generation_outputs (
 account_id TEXT NOT NULL, request_id TEXT NOT NULL, attempt_id TEXT NOT NULL,
 output_id TEXT NOT NULL, ordinal INTEGER NOT NULL CHECK(ordinal BETWEEN 0 AND 63),
 kind TEXT NOT NULL CHECK(kind IN ('text','image','video','audio')),
 projection JSONB NOT NULL CHECK(jsonb_typeof(projection)='object'),
 location_cipher BYTEA,
 import_state TEXT NOT NULL DEFAULT 'pending' CHECK(import_state IN ('pending','imported','retained')),
 content_revision_id TEXT,
 PRIMARY KEY(account_id,attempt_id,output_id),
 UNIQUE(account_id,attempt_id,ordinal)
);
