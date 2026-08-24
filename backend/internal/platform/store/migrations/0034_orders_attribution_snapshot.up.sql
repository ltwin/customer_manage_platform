-- dashboard-v2-redesign ITEM-3 归因快照：下单时固化渠道与拍摄类型（DEC-5）。
-- channel_snapshot 恒有值；DEFAULT 'other' 仅兜底直写 SQL（测试便利缺省），
-- 生产写路径（order 仓库创建）恒显式提供下单时渠道。存量订单 best-effort
-- 回填：按当前客户渠道/套系类型写入，可能与真实下单时不一致（epic 遗留风险 4）。
ALTER TABLE orders
    ADD COLUMN channel_snapshot TEXT NOT NULL DEFAULT 'other'
        CHECK (channel_snapshot IN ('xiaohongshu', 'douyin', 'weibo', 'referral', 'other')),
    ADD COLUMN shoot_type_snapshot TEXT
        CHECK (shoot_type_snapshot IS NULL OR shoot_type_snapshot IN ('portrait', 'cosplay', 'other'));

UPDATE orders AS ord
SET channel_snapshot = cust.channel
FROM customers AS cust
WHERE cust.id = ord.customer_id
  AND cust.account_id = ord.account_id;

UPDATE orders AS ord
SET shoot_type_snapshot = pkg.shoot_type
FROM packages AS pkg
WHERE pkg.id = ord.package_id
  AND pkg.account_id = ord.account_id;
