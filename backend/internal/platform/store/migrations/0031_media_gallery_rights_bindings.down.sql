-- 回滚到 0017 版本的素材列表投影（丢弃权利快照与有效绑定列）。
-- CREATE OR REPLACE VIEW 不能删列，需先 DROP 再按 0017 定义重建。

DROP VIEW IF EXISTS planning_media_asset_gallery;
CREATE VIEW planning_media_asset_gallery AS
SELECT
    a.account_id,
    a.id,
    a.upload_context_plan_id,
    a.display_name,
    a.state,
    a.current_generation,
    a.revision,
    a.gc_eligible_at,
    a.gc_rule_version,
    a.created_at,
    a.updated_at,
    a.deleted_at,
    r.checksum AS display_checksum
FROM planning_media_assets a
JOIN planning_media_renditions r
  ON r.account_id = a.account_id
 AND r.asset_id = a.id
 AND r.generation = a.current_generation
 AND r.kind = 'display';
