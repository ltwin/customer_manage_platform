-- Shared document foundation: revision history, reference holders, trash and
-- per-canvas assistant conversations for every document type on the canvas.
ALTER TABLE creative_documents
 ADD COLUMN created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 ADD COLUMN updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 ADD COLUMN trashed_at TIMESTAMPTZ,
 ADD COLUMN unit_revs JSONB NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(unit_revs)='object');
CREATE INDEX creative_documents_list ON creative_documents(account_id, updated_at DESC, id DESC);
CREATE INDEX creative_documents_type_list ON creative_documents(account_id, type_key, updated_at DESC, id DESC);

CREATE TABLE creative_document_revisions (
 account_id TEXT NOT NULL, document_id TEXT NOT NULL,
 revision BIGINT NOT NULL CHECK (revision > 0),
 content_revision_id TEXT NOT NULL,
 title TEXT NOT NULL CHECK (char_length(title) <= 200),
 actor_kind TEXT NOT NULL CHECK (actor_kind IN ('photographer','agent','system')),
 run_id TEXT,
 restored_from BIGINT CHECK (restored_from IS NULL OR restored_from > 0),
 changed_units TEXT[] NOT NULL DEFAULT '{}',
 annotated_units TEXT[] NOT NULL DEFAULT '{}',
 removed_units TEXT[] NOT NULL DEFAULT '{}',
 unit_revs JSONB NOT NULL CHECK (jsonb_typeof(unit_revs)='object'),
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY (account_id, document_id, revision),
 CHECK (actor_kind <> 'agent' OR run_id IS NOT NULL)
);
CREATE INDEX creative_document_revisions_run ON creative_document_revisions(account_id, document_id, run_id, revision DESC);
CREATE INDEX creative_document_revisions_root ON creative_document_revisions(account_id, content_revision_id);

CREATE TABLE creative_document_content_refs (
 account_id TEXT NOT NULL, document_id TEXT NOT NULL, revision BIGINT NOT NULL,
 content_revision_id TEXT NOT NULL,
 PRIMARY KEY (account_id, document_id, revision, content_revision_id)
);
CREATE INDEX creative_document_content_refs_root ON creative_document_content_refs(account_id, content_revision_id);

CREATE TABLE creative_document_conversations (
 account_id TEXT NOT NULL, document_id TEXT NOT NULL, canvas_id TEXT NOT NULL,
 conversation_id TEXT NOT NULL,
 updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY (account_id, document_id, canvas_id)
);

-- Existing documents get one system revision equal to their head.
UPDATE creative_documents SET unit_revs = CASE WHEN type_key = 'internal.document'
 THEN jsonb_build_object('meta', revision, 'body', revision)
 ELSE jsonb_build_object('meta', revision) END;
INSERT INTO creative_document_revisions(account_id, document_id, revision, content_revision_id, title, actor_kind, changed_units, unit_revs)
SELECT account_id, id, revision, content_revision_id, title, 'system', '{*}', unit_revs FROM creative_documents;
