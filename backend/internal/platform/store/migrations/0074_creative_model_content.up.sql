-- scene-procedural-geometry: model content (GLB baked from geometry code), its
-- staged uploads, and candidates consumed by a scene document command.
ALTER TABLE creative_contents DROP CONSTRAINT creative_contents_kind_check;
ALTER TABLE creative_contents ADD CONSTRAINT creative_contents_kind_check CHECK (kind IN ('text','link','image','video','audio','scene','model'));
ALTER TABLE creative_uploads DROP CONSTRAINT creative_uploads_declared_kind_check;
ALTER TABLE creative_uploads ADD CONSTRAINT creative_uploads_declared_kind_check CHECK (declared_kind IN ('image','video','audio','model'));
ALTER TABLE creative_upload_candidates DROP CONSTRAINT creative_upload_candidates_declared_kind_check;
ALTER TABLE creative_upload_candidates ADD CONSTRAINT creative_upload_candidates_declared_kind_check CHECK (declared_kind IN ('image','video','audio','model'));
ALTER TABLE creative_upload_candidates DROP CONSTRAINT creative_upload_candidates_adopted_target_kind_check;
ALTER TABLE creative_upload_candidates ADD CONSTRAINT creative_upload_candidates_adopted_target_kind_check CHECK (adopted_target_kind IN ('asset','node','document'));
