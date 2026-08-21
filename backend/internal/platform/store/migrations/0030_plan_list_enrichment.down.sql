-- 回滚到 0019 版本的列表投影（丢弃本迁移新增的加列）。
-- CREATE OR REPLACE VIEW 不能删列，需先 DROP 再按 0019 定义重建。

DROP VIEW IF EXISTS shoot_plan_list_projection;
CREATE VIEW shoot_plan_list_projection AS
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
       c.order_id AS crm_order_id
FROM shoot_plans p
LEFT JOIN shoot_plan_shots s
  ON s.account_id = p.account_id AND s.plan_id = p.id AND s.removed_at IS NULL
LEFT JOIN plan_crm_connections c
  ON c.account_id = p.account_id AND c.plan_id = p.id
GROUP BY p.account_id, p.id, c.customer_id, c.order_id;
