-- 0032 orders_delivery_due：交付 SLA 事实（dashboard-v2-redesign ITEM-1）。
--
-- delivery_due_at 是 date-only 的应交付日落库事实，不是时刻：SLA 语义为「拍摄后 N 天交付」，
-- 摄影师看日期不看时刻，date-only 在跨时区/DST 下无歧义（与 reminders.due_date 同模式）。
--
-- delivery_due_is_override 区分自动派生与订单级覆盖：派生值随 shot_at 变更重算，
-- 覆盖值不被冲掉。两者都是落库事实，改 settings.delivery_sla_days 不重写历史订单。

ALTER TABLE settings
    ADD COLUMN delivery_sla_days INTEGER NOT NULL DEFAULT 14
        CHECK (delivery_sla_days >= 1 AND delivery_sla_days <= 180);

ALTER TABLE orders
    ADD COLUMN delivery_due_at DATE,
    ADD COLUMN delivery_due_is_override BOOLEAN NOT NULL DEFAULT false;

-- backfill：存量已有 shot_at 的订单（含 closed）按账号有效时区与有效 SLA 派生应交付日；
-- cancelled 不回填（不进入交付队列）。无 settings 行的账号用默认值 Asia/Shanghai + 14。
UPDATE orders o
   SET delivery_due_at = (
           (o.shot_at AT TIME ZONE COALESCE(s.timezone, 'Asia/Shanghai'))::DATE
           + COALESCE(s.delivery_sla_days, 14)
       )
  FROM accounts a
       LEFT JOIN settings s ON s.account_id = a.id
 WHERE o.account_id = a.id
   AND o.shot_at IS NOT NULL
   AND o.status <> 'cancelled';

CREATE INDEX orders_account_delivery_due_idx
    ON orders (account_id, delivery_due_at, id)
 WHERE delivery_due_at IS NOT NULL;
