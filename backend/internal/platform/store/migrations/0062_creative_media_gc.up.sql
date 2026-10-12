-- V6-075：媒体 GC。生成导入补最后进展时间，GC 据此识别 lease 已过期且长期无进展的
-- 写入中 / 已存储导入并重置；已有行从迁移时刻起计时，不会被立即判为陈旧。
ALTER TABLE creative_generation_imports ADD COLUMN updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp();
