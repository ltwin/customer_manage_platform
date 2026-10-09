-- V6-008：上传可以暂存，不指定目标；内容由画布上的 fill_from_upload 放进节点。
ALTER TABLE creative_uploads DROP CONSTRAINT creative_uploads_target_kind_check;
ALTER TABLE creative_uploads ADD CONSTRAINT creative_uploads_target_kind_check CHECK(target_kind IN ('asset','node','staged'));
