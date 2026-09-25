-- G4：生成执行绑定与可恢复的对象导入；全部身份带账号范围。
CREATE TABLE creative_generation_bindings (
 account_id TEXT NOT NULL, execution_id TEXT NOT NULL, request_id TEXT NOT NULL,
 canvas_id TEXT NOT NULL, next_step_at TIMESTAMPTZ,
 epoch BIGINT NOT NULL DEFAULT 0, lease_until TIMESTAMPTZ,
 failures INTEGER NOT NULL DEFAULT 0 CHECK(failures BETWEEN 0 AND 5),
 verification_required BOOLEAN NOT NULL DEFAULT FALSE,
 recovery_until TIMESTAMPTZ NOT NULL,
 PRIMARY KEY(account_id,execution_id), UNIQUE(account_id,request_id)
);
CREATE INDEX creative_generation_due ON creative_generation_bindings(account_id,next_step_at) WHERE next_step_at IS NOT NULL;
CREATE TABLE creative_generation_imports (
 account_id TEXT NOT NULL, id TEXT NOT NULL, identity_hash TEXT NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('writing','stored','ready')),
 reserved_bytes BIGINT NOT NULL DEFAULT 0 CHECK(reserved_bytes>=0),
 epoch BIGINT NOT NULL DEFAULT 1, lease_until TIMESTAMPTZ,
 original_blob TEXT NOT NULL DEFAULT '', display_blob TEXT NOT NULL DEFAULT '', original_version TEXT NOT NULL DEFAULT '', display_version TEXT NOT NULL DEFAULT '',
 verified JSONB NOT NULL DEFAULT '{}', content_revision_id TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(account_id,id),
 CHECK(state<>'ready' OR content_revision_id<>'')
);

ALTER TABLE llm_generation_requests ADD COLUMN result_verification_required BOOLEAN NOT NULL DEFAULT FALSE;
UPDATE llm_generation_requests r SET result_verification_required=TRUE
 WHERE r.verification_required AND EXISTS (
 SELECT 1 FROM llm_generation_observations o JOIN llm_generation_tasks t
 ON t.account_id=o.account_id AND t.attempt_id=o.attempt_id
 WHERE t.account_id=r.account_id AND t.request_id=r.request_id AND o.disposition='conflict');
