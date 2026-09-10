-- 第二批模块 DDL 草案：先加载 content-media-schema.sql；不接入生产迁移器。
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE TABLE creative_library_settings (
 account_id TEXT PRIMARY KEY, retention_days INTEGER CHECK(retention_days IN (7,30,90)),
 revision BIGINT NOT NULL DEFAULT 1 CHECK(revision>0),
 hierarchy_revision BIGINT NOT NULL DEFAULT 1 CHECK(hierarchy_revision>0),
 library_revision BIGINT NOT NULL DEFAULT 1 CHECK(library_revision>0)
);
CREATE TABLE creative_assets (
 id TEXT PRIMARY KEY, account_id TEXT NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN ('image','text','link','video','audio')),
 title TEXT NOT NULL CHECK(char_length(title) BETWEEN 1 AND 200),
 normalized_title TEXT COLLATE "C" NOT NULL,
 description TEXT NOT NULL DEFAULT '' CHECK(char_length(description)<=2000),
 content_id TEXT NOT NULL, content_revision_id TEXT NOT NULL,
 is_favorite BOOLEAN NOT NULL DEFAULT FALSE, source_url TEXT CHECK(char_length(source_url)<=4096),
 deleted_at TIMESTAMPTZ, purge_after TIMESTAMPTZ,
 revision BIGINT NOT NULL DEFAULT 1 CHECK(revision>0),
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(), updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(account_id,id), CHECK(purge_after IS NULL OR (deleted_at IS NOT NULL AND purge_after>=deleted_at))
);
CREATE INDEX creative_asset_recent ON creative_assets(account_id,created_at DESC,id DESC) WHERE deleted_at IS NULL;
CREATE INDEX creative_asset_name ON creative_assets(account_id,normalized_title,id) WHERE deleted_at IS NULL;
CREATE INDEX creative_asset_kind ON creative_assets(account_id,kind,created_at DESC,id DESC) WHERE deleted_at IS NULL;
CREATE INDEX creative_asset_revision_root ON creative_assets(account_id,content_revision_id);
CREATE INDEX creative_asset_purge_due ON creative_assets(purge_after,account_id,id) WHERE deleted_at IS NOT NULL AND purge_after IS NOT NULL;
CREATE TABLE creative_asset_groups (
 id TEXT PRIMARY KEY, account_id TEXT NOT NULL, parent_id TEXT,
 name TEXT NOT NULL CHECK(char_length(name) BETWEEN 1 AND 80), position INTEGER NOT NULL CHECK(position>=0),
 revision BIGINT NOT NULL DEFAULT 1 CHECK(revision>0), UNIQUE(account_id,id), CHECK(parent_id IS NULL OR parent_id<>id)
);
CREATE INDEX creative_group_tree ON creative_asset_groups(account_id,parent_id,position,id);
CREATE TABLE creative_asset_group_members (
 account_id TEXT NOT NULL,asset_id TEXT NOT NULL,group_id TEXT NOT NULL,position INTEGER NOT NULL DEFAULT 0 CHECK(position>=0),
 PRIMARY KEY(account_id,asset_id,group_id)
);
CREATE INDEX creative_group_member_lookup ON creative_asset_group_members(account_id,group_id,asset_id);
CREATE TABLE creative_tag_categories (
 id TEXT PRIMARY KEY,account_id TEXT NOT NULL,name TEXT NOT NULL CHECK(char_length(name) BETWEEN 1 AND 80),
 position INTEGER NOT NULL CHECK(position>=0),revision BIGINT NOT NULL DEFAULT 1 CHECK(revision>0),UNIQUE(account_id,id)
);
CREATE TABLE creative_tags (
 id TEXT PRIMARY KEY,account_id TEXT NOT NULL,name TEXT NOT NULL CHECK(char_length(name) BETWEEN 1 AND 80),
 normalized_name TEXT NOT NULL CHECK(char_length(normalized_name) BETWEEN 1 AND 80),
 color TEXT NOT NULL CHECK(color ~ '^#[a-fA-F0-9]{6}$'),category_id TEXT,
 revision BIGINT NOT NULL DEFAULT 1 CHECK(revision>0),UNIQUE(account_id,id),UNIQUE(account_id,normalized_name)
);
CREATE INDEX creative_tag_category ON creative_tags(account_id,category_id);
CREATE TABLE creative_asset_tags (
 account_id TEXT NOT NULL,asset_id TEXT NOT NULL,tag_id TEXT NOT NULL,PRIMARY KEY(account_id,asset_id,tag_id)
);
CREATE INDEX creative_asset_tag_lookup ON creative_asset_tags(account_id,tag_id,asset_id);
CREATE TABLE creative_asset_search (
 account_id TEXT NOT NULL,asset_id TEXT NOT NULL,normalized_text TEXT NOT NULL,
 asset_revision BIGINT NOT NULL CHECK(asset_revision>0),PRIMARY KEY(account_id,asset_id)
);
CREATE INDEX creative_search_trgm ON creative_asset_search USING gin(normalized_text gin_trgm_ops);
CREATE TABLE creative_projects (
 id TEXT PRIMARY KEY,account_id TEXT NOT NULL,name TEXT NOT NULL CHECK(char_length(name) BETWEEN 1 AND 200),
 archived_at TIMESTAMPTZ,revision BIGINT NOT NULL DEFAULT 1 CHECK(revision>0),
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),UNIQUE(account_id,id)
);
CREATE INDEX creative_project_list ON creative_projects(account_id,updated_at DESC,id DESC);
CREATE TABLE creative_canvases (
 id TEXT PRIMARY KEY,account_id TEXT NOT NULL,project_id TEXT NOT NULL,name TEXT NOT NULL,
 is_default BOOLEAN NOT NULL DEFAULT TRUE,revision BIGINT NOT NULL DEFAULT 1 CHECK(revision>0),
 topology_revision BIGINT NOT NULL DEFAULT 1 CHECK(topology_revision>0),schema_version INTEGER NOT NULL DEFAULT 1 CHECK(schema_version>0),
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),UNIQUE(account_id,id)
);
CREATE UNIQUE INDEX creative_canvas_default ON creative_canvases(account_id,project_id) WHERE is_default;
CREATE TABLE creative_graph_identities (
 object_id TEXT PRIMARY KEY,account_id TEXT NOT NULL,canvas_id TEXT NOT NULL,
 object_kind TEXT NOT NULL CHECK(object_kind IN ('node','edge','input')),is_live BOOLEAN NOT NULL,
 effect_heads JSONB NOT NULL DEFAULT '{}' CHECK(jsonb_typeof(effect_heads)='object'),
 last_placement_revision BIGINT NOT NULL DEFAULT 1 CHECK(last_placement_revision>0),
 last_data_revision BIGINT NOT NULL DEFAULT 1 CHECK(last_data_revision>0),last_revision BIGINT NOT NULL DEFAULT 1 CHECK(last_revision>0),
 UNIQUE(account_id,object_id)
);
CREATE INDEX creative_graph_identity_canvas ON creative_graph_identities(account_id,canvas_id);
CREATE TABLE creative_nodes (
 id TEXT PRIMARY KEY,account_id TEXT NOT NULL,canvas_id TEXT NOT NULL,type_key TEXT NOT NULL,type_version INTEGER NOT NULL CHECK(type_version>0),parent_id TEXT,
 x DOUBLE PRECISION NOT NULL CHECK(x>'-Infinity'::float8 AND x<'Infinity'::float8),
 y DOUBLE PRECISION NOT NULL CHECK(y>'-Infinity'::float8 AND y<'Infinity'::float8),
 width DOUBLE PRECISION NOT NULL CHECK(width>0 AND width<'Infinity'::float8),
 height DOUBLE PRECISION NOT NULL CHECK(height>0 AND height<'Infinity'::float8),z_order INTEGER NOT NULL DEFAULT 0,
 title TEXT NOT NULL DEFAULT '' CHECK(char_length(title)<=200),intent TEXT NOT NULL DEFAULT '' CHECK(char_length(intent)<=2000),
 content_id TEXT,content_revision_id TEXT,source_asset_id_snapshot TEXT,source_name_snapshot TEXT,
 config JSONB NOT NULL DEFAULT '{}' CHECK(jsonb_typeof(config)='object'),
 placement_revision BIGINT NOT NULL DEFAULT 1 CHECK(placement_revision>0),data_revision BIGINT NOT NULL DEFAULT 1 CHECK(data_revision>0),
 UNIQUE(account_id,id),CHECK(parent_id IS NULL OR parent_id<>id),CHECK((content_id IS NULL)=(content_revision_id IS NULL)),
 CHECK(type_key<>'core.group' OR content_id IS NULL)
);
CREATE INDEX creative_nodes_canvas ON creative_nodes(account_id,canvas_id,id);
CREATE INDEX creative_nodes_parent ON creative_nodes(account_id,canvas_id,parent_id);
CREATE INDEX creative_nodes_revision_root ON creative_nodes(account_id,content_revision_id) WHERE content_revision_id IS NOT NULL;
CREATE TABLE creative_edges (
 id TEXT PRIMARY KEY,account_id TEXT NOT NULL,canvas_id TEXT NOT NULL,source_node_id TEXT NOT NULL,source_port TEXT NOT NULL,
 target_node_id TEXT NOT NULL,target_port TEXT NOT NULL,role TEXT NOT NULL,target_ordinal INTEGER NOT NULL CHECK(target_ordinal>=0),
 revision BIGINT NOT NULL DEFAULT 1 CHECK(revision>0),UNIQUE(account_id,id),CHECK(source_node_id<>target_node_id),
 UNIQUE(account_id,canvas_id,source_node_id,source_port,target_node_id,target_port,role)
);
CREATE INDEX creative_edges_source ON creative_edges(account_id,canvas_id,source_node_id);
CREATE INDEX creative_edges_target ON creative_edges(account_id,canvas_id,target_node_id);
CREATE TABLE creative_node_inputs (
 id TEXT PRIMARY KEY,account_id TEXT NOT NULL,canvas_id TEXT NOT NULL,node_id TEXT NOT NULL,slot_key TEXT NOT NULL,
 position INTEGER NOT NULL CHECK(position>=0),source_node_id TEXT,content_id TEXT,content_revision_id TEXT,role TEXT NOT NULL,
 revision BIGINT NOT NULL DEFAULT 1 CHECK(revision>0),UNIQUE(account_id,id),UNIQUE(account_id,node_id,slot_key,position),
 CHECK((content_id IS NULL)=(content_revision_id IS NULL)),
 CHECK((source_node_id IS NOT NULL AND content_id IS NULL) OR (source_node_id IS NULL AND content_id IS NOT NULL)),
 CHECK(source_node_id IS NULL OR source_node_id<>node_id)
);
CREATE INDEX creative_input_dynamic ON creative_node_inputs(account_id,canvas_id,source_node_id) WHERE source_node_id IS NOT NULL;
CREATE INDEX creative_input_revision_root ON creative_node_inputs(account_id,content_revision_id) WHERE content_revision_id IS NOT NULL;
CREATE TABLE creative_changes (
 id TEXT PRIMARY KEY,account_id TEXT NOT NULL,operation_id TEXT NOT NULL,request_hash TEXT NOT NULL,
 aggregate_kind TEXT NOT NULL,aggregate_id TEXT NOT NULL,canvas_id TEXT,change_group_id TEXT NOT NULL,
 actor_kind TEXT NOT NULL CHECK(actor_kind IN ('photographer','agent','system')),run_id TEXT,
 base_revision BIGINT NOT NULL CHECK(base_revision>=0),result_revision BIGINT NOT NULL CHECK(result_revision>base_revision),
 preconditions JSONB NOT NULL CHECK(jsonb_typeof(preconditions)='object'),
 commands JSONB NOT NULL CHECK(jsonb_typeof(commands)='array'),before_after JSONB NOT NULL CHECK(jsonb_typeof(before_after)='object'),
 response JSONB NOT NULL CHECK(jsonb_typeof(response)='object'),undo_of TEXT,expires_at TIMESTAMPTZ NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),UNIQUE(account_id,id),UNIQUE(account_id,operation_id)
);
CREATE INDEX creative_change_group ON creative_changes(account_id,change_group_id);
CREATE INDEX creative_change_canvas ON creative_changes(account_id,canvas_id,result_revision);
CREATE INDEX creative_change_due ON creative_changes(expires_at,account_id,id);
CREATE TABLE creative_change_content_refs (
 account_id TEXT NOT NULL,change_id TEXT NOT NULL,content_revision_id TEXT NOT NULL,PRIMARY KEY(account_id,change_id,content_revision_id)
);
CREATE INDEX creative_change_revision_root ON creative_change_content_refs(account_id,content_revision_id);
