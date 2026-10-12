-- V6-066–069：资产库第 2 期。只加可空列和新表，旧版本应用照常读写。
-- 评分按半星存成整数 1–10，空值表示未评分。
ALTER TABLE creative_assets ADD COLUMN rating_halves SMALLINT CHECK (rating_halves BETWEEN 1 AND 10);
CREATE INDEX creative_asset_rating ON creative_assets(account_id, rating_halves DESC NULLS LAST, created_at DESC, id DESC)
 WHERE deleted_at IS NULL;

-- 主色是图片文件本身的事实，挂在 blob 上，和素材、画布都无关；不是媒体保留根。
CREATE TABLE creative_blob_palettes (
 account_id TEXT NOT NULL,
 blob_id TEXT NOT NULL,
 algorithm TEXT NOT NULL CHECK (char_length(algorithm) BETWEEN 1 AND 40),
 state TEXT NOT NULL CHECK (state IN ('ready','failed')),
 failure_reason TEXT NOT NULL DEFAULT '' CHECK (char_length(failure_reason) <= 100),
 colors JSONB NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(colors) = 'array'),
 computed_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY (account_id, blob_id)
);

-- 删除分组时的快照，供撤销使用；保留 30 天。
CREATE TABLE creative_asset_group_deletions (
 id TEXT PRIMARY KEY,
 account_id TEXT NOT NULL,
 group_snapshot JSONB NOT NULL CHECK (jsonb_typeof(group_snapshot) = 'object'),
 promoted_child_ids TEXT[] NOT NULL,
 member_asset_ids TEXT[] NOT NULL,
 hierarchy_revision_after BIGINT NOT NULL CHECK (hierarchy_revision_after >= 1),
 restored_at TIMESTAMPTZ,
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 UNIQUE (account_id, id)
);
CREATE INDEX creative_group_deletion_age ON creative_asset_group_deletions(created_at);
