-- FND-07 skill foundation: a skill's authoritative content moves out of the
-- binary into the database, so platform maintenance no longer needs a release.
-- Explicit account keys only; no foreign keys. Cross-table ownership is guarded
-- by services, exactly as 0044 does for conversations.
CREATE TABLE creative_skills (
 id TEXT PRIMARY KEY CHECK(id ~ '^ccsk_[0-9a-f-]{36}$'), account_id TEXT NOT NULL,
 -- The publishing account decides origin and a client never writes it: only the
 -- trusted admin entry point creates a platform skill.
 origin TEXT NOT NULL CHECK(origin IN ('platform','account')),
 -- Discovery name, unique per author. Renaming keeps the id, because a slug is
 -- never an execution identity.
 slug TEXT NOT NULL CHECK(slug ~ '^[a-z][a-z0-9-]{0,63}$'),
 display_name TEXT NOT NULL CHECK(char_length(display_name) BETWEEN 1 AND 120),
 description TEXT NOT NULL CHECK(char_length(description) BETWEEN 1 AND 500),
 -- The version this author currently recommends. Messages and runs fix their
 -- own version id and never re-read this pointer.
 current_version_id TEXT,
 -- Limits new executions without touching any content digest.
 availability TEXT NOT NULL DEFAULT 'active' CHECK(availability IN ('active','disabled')),
 revision BIGINT NOT NULL DEFAULT 1 CHECK(revision>0),
 -- Version numbers are handed out under this row's lock, never MAX()+1.
 next_version_number INT NOT NULL DEFAULT 1 CHECK(next_version_number>0),
 -- Reserved for a future marketplace release transaction and forced empty until
 -- one exists. A published-to-the-market pointer and the author's own current
 -- pointer answer different questions and must never share a column.
 marketplace_version_id TEXT CHECK(marketplace_version_id IS NULL),
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(account_id,id),
 UNIQUE(account_id,slug)
);
-- Serves the directory query, which the picker issues on every keystroke:
-- one account's startable skills, newest first, paged by (updated_at,id).
-- The search term is matched inside that set rather than by this index.
CREATE INDEX creative_skill_directory
 ON creative_skills(account_id,updated_at DESC,id DESC)
 WHERE current_version_id IS NOT NULL AND availability='active';

CREATE TABLE creative_skill_versions (
 id TEXT PRIMARY KEY CHECK(id ~ '^ccsv_[0-9a-f-]{36}$'), account_id TEXT NOT NULL,
 skill_id TEXT NOT NULL,
 version_number INT NOT NULL CHECK(version_number>0),
 -- The document format of this version, which is not its version number.
 schema_version INT NOT NULL CHECK(schema_version>0),
 -- What this version called itself. A history message renders the name shown
 -- then, not whatever the skill happens to be called today.
 display_name_snapshot TEXT NOT NULL CHECK(char_length(display_name_snapshot) BETWEEN 1 AND 120),
 description_snapshot TEXT NOT NULL CHECK(char_length(description_snapshot) BETWEEN 1 AND 500),
 instructions TEXT NOT NULL CHECK(char_length(instructions)>0),
 manifest JSONB NOT NULL,
 -- Computed by the server over the whole frozen content. A digest supplied by
 -- the importer is only ever compared against this one, never stored as it.
 digest TEXT NOT NULL CHECK(digest ~ '^[0-9a-f]{64}$'),
 -- Reserved like marketplace_version_id above and written by nothing yet: the
 -- editing baseline only becomes a fact once drafts exist. Read it as always
 -- NULL rather than as a lineage that happens to be missing.
 based_on_version_id TEXT,
 -- Execution control is the one mutable part of a version and is deliberately
 -- outside the digest: switching a version off must not change the answer to
 -- "which text ran".
 execution_status TEXT NOT NULL DEFAULT 'active' CHECK(execution_status IN ('active','disabled')),
 disabled_at TIMESTAMPTZ,
 disabled_reason TEXT CHECK(disabled_reason IS NULL OR char_length(disabled_reason) BETWEEN 1 AND 500),
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(account_id,id),
 UNIQUE(account_id,skill_id,version_number),
 CHECK(CASE WHEN execution_status='disabled' THEN disabled_at IS NOT NULL
            ELSE disabled_at IS NULL AND disabled_reason IS NULL END)
);

-- The declared files of one frozen version. Undeclared paths do not exist: a
-- run asks by declared path and there is no directory underneath.
CREATE TABLE creative_skill_version_resources (
 account_id TEXT NOT NULL, skill_version_id TEXT NOT NULL,
 path TEXT NOT NULL CHECK(char_length(path) BETWEEN 1 AND 255),
 mime TEXT NOT NULL CHECK(char_length(mime) BETWEEN 1 AND 127),
 byte_size BIGINT NOT NULL CHECK(byte_size>0),
 sha256 TEXT NOT NULL CHECK(sha256 ~ '^[0-9a-f]{64}$'),
 -- The exact object this version reads, and a server-internal address rather
 -- than a reusable storage credential: the API never returns these columns.
 storage_driver TEXT NOT NULL CHECK(storage_driver IN ('local','oss')),
 bucket TEXT NOT NULL,
 object_key TEXT NOT NULL CHECK(char_length(object_key)>0),
 object_version TEXT NOT NULL CHECK(char_length(object_version)>0),
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(account_id,skill_version_id,path)
);

-- Object upload and the database do not share a transaction, so an import is a
-- bounded record of one attempt rather than an implicit side effect.
CREATE TABLE creative_skill_imports (
 id TEXT PRIMARY KEY CHECK(id ~ '^ccsi_[0-9a-f-]{36}$'), account_id TEXT NOT NULL,
 -- One operation is one import: a retry replays its result, while the same
 -- operation carrying different bytes is reported as a conflict.
 operation_id TEXT NOT NULL CHECK(char_length(operation_id) BETWEEN 1 AND 128),
 request_hash TEXT NOT NULL CHECK(request_hash ~ '^[0-9a-f]{64}$'),
 target_skill_id TEXT NOT NULL,
 -- Empty for the import that creates the skill itself.
 expected_skill_revision BIGINT CHECK(expected_skill_revision>0),
 state TEXT NOT NULL CHECK(state IN ('preparing','ready','finalized','failed','expired')),
 -- The whole verified intent, not only the skill's manifest: origin, slug,
 -- names, instructions, manifest, resource declarations and the activation
 -- decision. Object upload and this database cannot share a transaction, so
 -- the intent has to survive between BeginImport and FinalizeImport.
 verified_manifest JSONB,
 -- The objects this import committed to owning, and the only record reclamation
 -- has. It is not a complete census: a crash between a successful upload and
 -- the row update leaves an orphan only a prefix scan could find, and the
 -- object port has no way to enumerate a prefix. Reclamation therefore deletes
 -- what is listed here; the residue waits for FND-10's general collection.
 staged_objects JSONB NOT NULL DEFAULT '[]'::jsonb CHECK(jsonb_typeof(staged_objects)='array'),
 result_version_id TEXT,
 expires_at TIMESTAMPTZ NOT NULL,
 revision BIGINT NOT NULL DEFAULT 1 CHECK(revision>0),
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(account_id,id),
 UNIQUE(account_id,operation_id),
 CHECK((state='finalized') = (result_version_id IS NOT NULL))
);
-- Serves the reclamation sweep, which asks for the least recently touched
-- candidates of one account. Finalized imports are excluded because they are
-- the bulk of the table and are never candidates; the sweep orders by
-- updated_at so that an import it cannot delete yields its place to the ones
-- behind it instead of holding the head of every future page.
CREATE INDEX creative_skill_import_reclaim
 ON creative_skill_imports(account_id,updated_at) WHERE state<>'finalized';

-- What a message actually showed. Registering the fact is all this phase needs:
-- skill versions are never physically deleted yet, so nothing reads these as a
-- retention root until FND-10 opens general collection. The account here is the
-- one reading the message; the publishing account is recorded beside it as a
-- value, so resolving a platform skill never needs another account's scope.
CREATE TABLE creative_message_skill_refs (
 account_id TEXT NOT NULL, message_id TEXT NOT NULL,
 segment_ordinal INT NOT NULL CHECK(segment_ordinal>=0),
 skill_id TEXT NOT NULL, skill_version_id TEXT NOT NULL,
 skill_owner_account_id TEXT NOT NULL,
 digest TEXT NOT NULL CHECK(digest ~ '^[0-9a-f]{64}$'),
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(account_id,message_id,segment_ordinal)
);
