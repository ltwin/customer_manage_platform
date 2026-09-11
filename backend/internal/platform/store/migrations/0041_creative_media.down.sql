-- Refuse a lossy rollback once media has been uploaded or bound.
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM creative_uploads) OR EXISTS(SELECT 1 FROM creative_blobs) OR EXISTS(SELECT 1 FROM creative_contents WHERE kind IN ('image','video','audio')) THEN
  RAISE EXCEPTION 'creative media data requires export/restore before rollback';
 END IF;
END $$;
DROP TABLE creative_media_quotas,creative_revision_holds,creative_blob_read_pins,creative_upload_candidates,creative_upload_parts,creative_uploads,creative_content_objects,creative_blobs;
ALTER TABLE creative_assets DROP CONSTRAINT creative_assets_kind_check;
ALTER TABLE creative_assets ADD CONSTRAINT creative_assets_kind_check CHECK(kind IN ('text','link'));
ALTER TABLE creative_contents DROP CONSTRAINT creative_contents_kind_check;
ALTER TABLE creative_contents ADD CONSTRAINT creative_contents_kind_check CHECK (kind IN ('text','link'));
