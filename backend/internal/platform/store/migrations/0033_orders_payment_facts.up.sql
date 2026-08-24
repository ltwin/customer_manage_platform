-- 0033 orders_payment_facts：支付事实字段（dashboard-v2-redesign ITEM-2）。
--
-- amount_paid 已收现金（分，恒有值，默认 0）；outstanding_amount 待收余额（分，
-- NULL = price 未定价，不计入待收合计）；paid_at 收款时刻（NULL = 未知，含存量）。
-- 单向不变量 balance_paid=true ⇒ outstanding_amount=0（DEC-10）以表级 CHECK 恒成立，
-- 服务层推定之外兜住任何直写路径。

ALTER TABLE orders
    ADD COLUMN amount_paid INTEGER NOT NULL DEFAULT 0
        CHECK (amount_paid >= 0),
    ADD COLUMN outstanding_amount INTEGER
        CHECK (outstanding_amount IS NULL OR outstanding_amount >= 0),
    ADD COLUMN paid_at TIMESTAMPTZ;

ALTER TABLE orders
    ADD CONSTRAINT orders_settled_no_outstanding_check
        CHECK (NOT balance_paid OR COALESCE(outstanding_amount, 0) = 0);

-- backfill（一次性，规则冻结于 epic ITEM-2；不含 paid_at——迁移无法知道历史收款时刻，
-- 不发明事实）：
--   已结清（balance_paid=true，含 cancelled 后结清的单）：amount_paid=COALESCE(price,0)、
--     outstanding=0；
--   未结清（balance_paid=false，含 cancelled）：amount_paid=0（列默认已满足）、
--     outstanding=price（price NULL → NULL，不计入待收合计）。
-- 待收/已收的经营口径消费方（dashboard v2）必须联合 status 过滤，与 delivery_due 同模式。
UPDATE orders
   SET amount_paid = COALESCE(price, 0),
       outstanding_amount = 0
 WHERE balance_paid;

UPDATE orders
   SET outstanding_amount = price
 WHERE NOT balance_paid;
