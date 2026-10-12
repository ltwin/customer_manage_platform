-- 进展时间只服务 GC 判定，回滚后 GC 不再重置陈旧导入，可以直接丢弃。
ALTER TABLE creative_generation_imports DROP COLUMN updated_at;
