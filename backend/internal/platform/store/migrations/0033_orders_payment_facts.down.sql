-- 0033 down：丢弃支付事实三列及其数据（与 0032 down 同取舍——降级即放弃本版新增事实，
-- 回到 v1 只有收款布尔标记的世界）。
ALTER TABLE orders
    DROP COLUMN IF EXISTS paid_at,
    DROP COLUMN IF EXISTS outstanding_amount,
    DROP COLUMN IF EXISTS amount_paid;
