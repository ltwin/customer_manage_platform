-- FND-02: text/link content and actual owning roots only; explicit keys, no foreign keys.
CREATE TABLE creative_contents (
 id TEXT PRIMARY KEY, account_id TEXT NOT NULL,
 kind TEXT NOT NULL CHECK (kind IN ('text','link')),
 created_by_kind TEXT NOT NULL CHECK (created_by_kind IN ('photographer','agent','system')),
 origin_node_id_snapshot TEXT,
 last_sequence BIGINT NOT NULL DEFAULT 0 CHECK(last_sequence>=0),
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(account_id,id)
);
CREATE TABLE creative_rights_declarations (
 id TEXT PRIMARY KEY, account_id TEXT NOT NULL,
 source_class TEXT NOT NULL CHECK (char_length(source_class) BETWEEN 1 AND 80),
 rights_basis TEXT NOT NULL CHECK (char_length(rights_basis) BETWEEN 1 AND 80),
 source_url TEXT CHECK (char_length(source_url)<=4096),
 attribution TEXT NOT NULL DEFAULT '' CHECK (char_length(attribution)<=2000),
 evidence_summary TEXT NOT NULL DEFAULT '' CHECK (char_length(evidence_summary)<=2000),
 declared_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(account_id,id)
);
CREATE TABLE creative_usage_grants (
 id TEXT PRIMARY KEY, account_id TEXT NOT NULL, declaration_id TEXT NOT NULL,
 purpose TEXT NOT NULL CHECK (purpose IN ('display','ai_analysis','generation_reference')),
 evidence JSONB NOT NULL CHECK (jsonb_typeof(evidence)='object'),
 granted_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(), revoked_at TIMESTAMPTZ,
 UNIQUE(account_id,id), CHECK(revoked_at IS NULL OR revoked_at>=granted_at)
);
CREATE UNIQUE INDEX creative_usage_grant_active ON creative_usage_grants(account_id,declaration_id,purpose) WHERE revoked_at IS NULL;
CREATE TABLE creative_content_revisions (
 id TEXT PRIMARY KEY, account_id TEXT NOT NULL, content_id TEXT NOT NULL,
 sequence BIGINT NOT NULL CHECK(sequence>0), schema_version INTEGER NOT NULL CHECK(schema_version>0),
 payload JSONB NOT NULL CHECK(jsonb_typeof(payload)='object'),
 provenance_snapshot JSONB NOT NULL DEFAULT '{}' CHECK(jsonb_typeof(provenance_snapshot)='object'),
 rights_declaration_id TEXT NOT NULL, created_by_run_id TEXT,
 state TEXT NOT NULL DEFAULT 'ready' CHECK(state IN ('ready','deleting','deleted')),
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(account_id,id), UNIQUE(account_id,content_id,sequence)
);
CREATE INDEX creative_revision_rights ON creative_content_revisions(account_id,rights_declaration_id);

CREATE TABLE creative_library_settings (
 account_id TEXT PRIMARY KEY, retention_days INTEGER CHECK(retention_days IN (7,30,90)),
 revision BIGINT NOT NULL DEFAULT 1 CHECK(revision>0),
 hierarchy_revision BIGINT NOT NULL DEFAULT 1 CHECK(hierarchy_revision>0),
 library_revision BIGINT NOT NULL DEFAULT 1 CHECK(library_revision>0)
);
CREATE TABLE creative_assets (
 id TEXT PRIMARY KEY, account_id TEXT NOT NULL, kind TEXT NOT NULL CHECK(kind IN ('text','link')),
 title TEXT NOT NULL CHECK(char_length(title) BETWEEN 1 AND 200), normalized_title TEXT COLLATE "C" NOT NULL,
 description TEXT NOT NULL DEFAULT '' CHECK(char_length(description)<=2000),
 content_id TEXT NOT NULL, content_revision_id TEXT NOT NULL,
 is_favorite BOOLEAN NOT NULL DEFAULT FALSE, source_url TEXT,
 deleted_at TIMESTAMPTZ, purge_after TIMESTAMPTZ,
 revision BIGINT NOT NULL DEFAULT 1 CHECK(revision>0),
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(), updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(account_id,id)
);
CREATE INDEX creative_asset_recent ON creative_assets(account_id,created_at DESC,id DESC) WHERE deleted_at IS NULL;
CREATE INDEX creative_asset_revision_root ON creative_assets(account_id,content_revision_id);
CREATE TABLE creative_projects (
 id TEXT PRIMARY KEY, account_id TEXT NOT NULL, name TEXT NOT NULL CHECK(char_length(name) BETWEEN 1 AND 200),
 archived_at TIMESTAMPTZ, revision BIGINT NOT NULL DEFAULT 1 CHECK(revision>0),
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(), updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(), UNIQUE(account_id,id)
);
CREATE TABLE creative_canvases (
 id TEXT PRIMARY KEY, account_id TEXT NOT NULL, project_id TEXT NOT NULL, name TEXT NOT NULL,
 is_default BOOLEAN NOT NULL DEFAULT TRUE, revision BIGINT NOT NULL DEFAULT 1 CHECK(revision>0),
 topology_revision BIGINT NOT NULL DEFAULT 1 CHECK(topology_revision>0), schema_version INTEGER NOT NULL DEFAULT 1,
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(), updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(), UNIQUE(account_id,id)
);
CREATE UNIQUE INDEX creative_default_canvas ON creative_canvases(account_id,project_id) WHERE is_default;
CREATE TABLE creative_nodes (
 id TEXT PRIMARY KEY, account_id TEXT NOT NULL, canvas_id TEXT NOT NULL,
 type_key TEXT NOT NULL CHECK(type_key IN ('core.text','core.link')), type_version INTEGER NOT NULL DEFAULT 1,
 parent_id TEXT, x DOUBLE PRECISION NOT NULL CHECK(x BETWEEN -10000000 AND 10000000), y DOUBLE PRECISION NOT NULL CHECK(y BETWEEN -10000000 AND 10000000),
 width DOUBLE PRECISION NOT NULL DEFAULT 280 CHECK(width>0 AND width<=100000), height DOUBLE PRECISION NOT NULL DEFAULT 180 CHECK(height>0 AND height<=100000),
 z_order INTEGER NOT NULL DEFAULT 0, title TEXT NOT NULL DEFAULT '' CHECK(char_length(title)<=200), intent TEXT NOT NULL DEFAULT '',
 content_id TEXT, content_revision_id TEXT, source_asset_id_snapshot TEXT,
 config JSONB NOT NULL DEFAULT '{}' CHECK(jsonb_typeof(config)='object'),
 placement_revision BIGINT NOT NULL DEFAULT 1 CHECK(placement_revision>0), data_revision BIGINT NOT NULL DEFAULT 1 CHECK(data_revision>0),
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(), updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(account_id,id), CHECK((content_id IS NULL)=(content_revision_id IS NULL))
);
CREATE INDEX creative_nodes_canvas ON creative_nodes(account_id,canvas_id,id);
CREATE INDEX creative_nodes_revision_root ON creative_nodes(account_id,content_revision_id);
