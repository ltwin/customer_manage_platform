-- FND-07 milestone A: agent conversations, durable messages and egress consent.
-- Explicit account keys only; no foreign keys. Cross-table ownership is guarded by services.
-- Runs, steps, slots and events arrive with the next migration; this one owns the
-- reading surface a photographer keeps after any run is long finished.
CREATE TABLE creative_agent_conversations (
 id TEXT PRIMARY KEY CHECK(id ~ '^ccco_[0-9a-f-]{36}$'), account_id TEXT NOT NULL,
 project_id TEXT NOT NULL, canvas_id TEXT NOT NULL,
 title TEXT NOT NULL CHECK(char_length(title) BETWEEN 1 AND 120),
 revision BIGINT NOT NULL DEFAULT 1 CHECK(revision>0),
 -- Ordinals are handed out under the conversation row lock, never MAX()+1.
 next_message_ordinal BIGINT NOT NULL DEFAULT 1 CHECK(next_message_ordinal>0),
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(account_id,id)
);
CREATE INDEX creative_conversation_canvas
 ON creative_agent_conversations(account_id,canvas_id,updated_at DESC,id DESC);

CREATE TABLE creative_agent_messages (
 id TEXT PRIMARY KEY CHECK(id ~ '^ccms_[0-9a-f-]{36}$'), account_id TEXT NOT NULL,
 conversation_id TEXT NOT NULL,
 ordinal BIGINT NOT NULL CHECK(ordinal>0),
 run_id TEXT,
 role TEXT NOT NULL CHECK(role IN ('user','assistant','tool','system')),
 status TEXT NOT NULL CHECK(status IN ('streaming','complete','interrupted')),
 schema_version INT NOT NULL CHECK(schema_version>0),
 body JSONB NOT NULL,
 revision BIGINT NOT NULL DEFAULT 1 CHECK(revision>0),
 source_step_id TEXT,
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(account_id,id),
 UNIQUE(account_id,conversation_id,ordinal)
);
-- One step result projects into one message per role; a retried consumer replays
-- the original row instead of appending a second copy of the same answer.
CREATE UNIQUE INDEX creative_message_source_step
 ON creative_agent_messages(account_id,source_step_id,role) WHERE source_step_id IS NOT NULL;
CREATE INDEX creative_message_run ON creative_agent_messages(account_id,run_id) WHERE run_id IS NOT NULL;

-- A message is a retention root: the content it shows stays readable after the
-- run payload expires. Identifiers hidden inside body JSON are not roots.
CREATE TABLE creative_message_content_refs (
 account_id TEXT NOT NULL, message_id TEXT NOT NULL, content_revision_id TEXT NOT NULL,
 role TEXT NOT NULL CHECK(role IN ('input','attachment','result')),
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(account_id,message_id,content_revision_id,role)
);
CREATE INDEX creative_message_ref_revision
 ON creative_message_content_refs(account_id,content_revision_id);

CREATE TABLE creative_egress_consents (
 id TEXT PRIMARY KEY CHECK(id ~ '^ccec_[0-9a-f-]{36}$'), account_id TEXT NOT NULL,
 -- The company that receives the bytes, not the wire protocol and not one
 -- model: switching between two models of the same vendor must not re-prompt.
 vendor_key TEXT NOT NULL CHECK(vendor_key ~ '^[a-z][a-z0-9_-]{0,63}$'),
 purpose TEXT NOT NULL CHECK(purpose IN ('creative_assistance')),
 conversation_id TEXT,
 -- Scope stores the mode and data classes only. Approved resources live in the
 -- companion table so a scope string can never smuggle in an identifier.
 -- coalesce, because a missing key makes jsonb_typeof NULL and a CHECK only
 -- rejects FALSE. The length test is wrapped in CASE rather than left to the
 -- right of an AND: PostgreSQL does not promise boolean short-circuiting, and
 -- jsonb_array_length raises on a scalar instead of returning NULL.
 scope JSONB NOT NULL CHECK(
  coalesce(scope->>'mode','') IN ('selected_revisions','account_library')
  AND (CASE WHEN jsonb_typeof(scope->'data_classes')='array'
            THEN jsonb_array_length(scope->'data_classes')>0 ELSE false END)
 ),
 policy_version TEXT NOT NULL CHECK(char_length(policy_version) BETWEEN 1 AND 64),
 revision BIGINT NOT NULL DEFAULT 1 CHECK(revision>0),
 granted_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 revoked_at TIMESTAMPTZ,
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(account_id,id)
);
CREATE INDEX creative_consent_conversation
 ON creative_egress_consents(account_id,conversation_id,granted_at DESC)
 WHERE conversation_id IS NOT NULL AND revoked_at IS NULL;

-- An approval record, not a retention root: once the revision is cleaned up the
-- row still locates what was approved but cannot restore the content.
CREATE TABLE creative_egress_consent_contents (
 account_id TEXT NOT NULL, consent_id TEXT NOT NULL, content_revision_id TEXT NOT NULL,
 approved_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(account_id,consent_id,content_revision_id)
);
