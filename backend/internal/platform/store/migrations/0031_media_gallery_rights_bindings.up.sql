-- 0031 media_gallery_rights_bindings：素材列表投影加当前代权利声明快照与
-- 有效绑定集合（GAP-WS-06/07）。权利声明按 (asset_id, current_generation)
-- 一对一 LEFT JOIN；有效绑定用相关子查询 jsonb_agg 聚合，避免行倍增破坏
-- 以 id 为游标的分页。矩阵裁决仍在服务层（planningmedia/matrix.go）。

CREATE OR REPLACE VIEW planning_media_asset_gallery AS
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
    r.checksum AS display_checksum,
    d.generation AS rights_generation,
    d.source_class AS rights_source_class,
    d.rights_basis AS rights_basis,
    d.license_generation_reference_granted AS rights_generation_granted,
    d.declared_at AS rights_declared_at,
    COALESCE((
        SELECT jsonb_agg(jsonb_build_object(
                  'id', b.id,
                  'asset_id', b.asset_id,
                  'generation', b.generation,
                  'holder_kind', b.holder_kind,
                  'holder_id', b.holder_id,
                  'plan_id', b.plan_id,
                  'purpose', b.purpose,
                  'state', b.state,
                  'revision', b.revision,
                  'created_at', b.created_at,
                  'released_at', b.released_at
              ) ORDER BY b.created_at, b.id)
          FROM planning_media_bindings b
         WHERE b.account_id = a.account_id
           AND b.asset_id = a.id
           AND b.state = 'active'
    ), '[]'::jsonb) AS active_bindings
FROM planning_media_assets a
JOIN planning_media_renditions r
  ON r.account_id = a.account_id
 AND r.asset_id = a.id
 AND r.generation = a.current_generation
 AND r.kind = 'display'
LEFT JOIN planning_media_rights_declarations d
  ON d.account_id = a.account_id
 AND d.asset_id = a.id
 AND d.generation = a.current_generation;
