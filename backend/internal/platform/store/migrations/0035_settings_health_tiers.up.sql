-- 0035 settings_health_tiers：客户健康度分层参数组（customer-health-tiers）。
--
-- 三档 ratio 阈值（严格递增，由服务层校验）+ 仅 1 次拍摄客户的通用节奏基线天数。
-- JSONB 单组存储与 churn_thresholds/availability 同模式；存量行由列默认直接获得
-- 原型口径（1.2/2/3.5 倍 + 120 天），无 backfill 需要。

ALTER TABLE settings
    ADD COLUMN health_tiers JSONB NOT NULL DEFAULT '{
      "sleeping_ratio": 1.2,
      "at_risk_ratio": 2,
      "lost_ratio": 3.5,
      "fallback_cadence_days": 120
    }'::jsonb;
