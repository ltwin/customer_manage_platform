-- 评分是摄影师自己打的，回滚会丢掉，所以有评分时拒绝回滚。
-- 主色是派生数据、删除快照只服务撤销，可以直接丢弃。
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM creative_assets WHERE rating_halves IS NOT NULL) THEN
  RAISE EXCEPTION 'rated assets exist; clear ratings before rollback';
 END IF;
END $$;
DROP TABLE creative_asset_group_deletions;
DROP TABLE creative_blob_palettes;
DROP INDEX creative_asset_rating;
ALTER TABLE creative_assets DROP COLUMN rating_halves;
