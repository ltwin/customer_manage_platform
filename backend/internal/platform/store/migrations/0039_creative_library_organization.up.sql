-- FND-03: personal library organization; explicit account keys, no foreign keys.
CREATE EXTENSION IF NOT EXISTS pg_trgm;
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
ALTER TABLE creative_asset_search ADD COLUMN normalization_version TEXT NOT NULL DEFAULT '';
CREATE INDEX creative_asset_name ON creative_assets(account_id,normalized_title,id) WHERE deleted_at IS NULL;
CREATE INDEX creative_asset_kind ON creative_assets(account_id,kind,created_at DESC,id DESC) WHERE deleted_at IS NULL;
CREATE INDEX creative_asset_trash ON creative_assets(account_id,deleted_at,id) WHERE deleted_at IS NOT NULL;
ALTER TABLE creative_assets ADD CONSTRAINT creative_asset_purge_window CHECK(purge_after IS NULL OR (deleted_at IS NOT NULL AND purge_after>=deleted_at));
