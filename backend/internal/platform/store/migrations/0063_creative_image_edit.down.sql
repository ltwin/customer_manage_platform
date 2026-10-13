-- 回滚前先停受理并让候选终结；未采用的结果 blob 失去持有方后由 GC 按「从未计额度」回收，
-- 已采用的结果由节点版本持有，不依赖本表。
DROP TABLE creative_image_edit_renders;
