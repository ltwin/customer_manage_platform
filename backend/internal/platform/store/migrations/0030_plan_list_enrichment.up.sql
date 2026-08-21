-- 0030 plan_list_enrichment：列表投影加列（GAP-WS-04）。
-- 订单标题/状态取 plan_crm_connections.linked_order_snapshot（链接时快照，
-- 订单删除后仍可显示，对应原型「订单已取消（保留快照）」语义）；
-- 客户名取当前档案 display_name；捕获统计由 current_outcome_event_id
-- 关联执行事件得出（cleared 结果会把指针置空，因此 FILTER 只见 captured/skipped）；
-- 准备项计数用标量子查询，避免与 shots join 产生笛卡尔积破坏既有计数。

CREATE OR REPLACE VIEW shoot_plan_list_projection AS
SELECT p.account_id,
       p.id,
       p.title,
       p.subject,
       p.status,
       p.planned_look_count,
       p.planned_scene_count,
       p.revision,
       p.execution_fact_revision,
       p.created_at,
       p.updated_at,
       count(s.id)::BIGINT AS planned_shot_count,
       c.customer_id AS crm_customer_id,
       c.order_id AS crm_order_id,
       cu.display_name AS crm_customer_name,
       c.linked_order_snapshot->>'title' AS crm_order_title,
       c.linked_order_snapshot->>'status_at_link' AS crm_order_status_at_link,
       w.starts_at AS window_starts_at,
       w.ends_at AS window_ends_at,
       w.timezone AS window_timezone,
       w.source AS window_source,
       count(e.id) FILTER (WHERE e.result = 'captured')::BIGINT AS captured_count,
       count(e.id) FILTER (WHERE e.result = 'skipped')::BIGINT AS skipped_count,
       (SELECT count(*)
          FROM shoot_plan_readiness_items r
         WHERE r.account_id = p.account_id AND r.plan_id = p.id AND r.removed_at IS NULL
           AND r.requirement = 'required')::BIGINT AS readiness_required_total,
       (SELECT count(*)
          FROM shoot_plan_readiness_items r
         WHERE r.account_id = p.account_id AND r.plan_id = p.id AND r.removed_at IS NULL
           AND r.requirement = 'required' AND r.preflight_status = 'unchecked')::BIGINT AS readiness_required_unchecked
FROM shoot_plans p
LEFT JOIN shoot_plan_shots s
  ON s.account_id = p.account_id AND s.plan_id = p.id AND s.removed_at IS NULL
LEFT JOIN shoot_plan_execution_events e
  ON e.account_id = s.account_id AND e.plan_id = s.plan_id AND e.id = s.current_outcome_event_id
LEFT JOIN plan_crm_connections c
  ON c.account_id = p.account_id AND c.plan_id = p.id
LEFT JOIN customers cu
  ON cu.account_id = c.account_id AND cu.id = c.customer_id
LEFT JOIN shoot_plan_execution_windows w
  ON w.account_id = p.account_id AND w.plan_id = p.id
GROUP BY p.account_id, p.id, c.customer_id, c.order_id, cu.display_name,
         c.linked_order_snapshot, w.starts_at, w.ends_at, w.timezone, w.source;
