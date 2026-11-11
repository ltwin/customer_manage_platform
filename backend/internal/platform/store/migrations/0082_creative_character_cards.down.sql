DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM creative_contents WHERE kind = 'character')
        OR EXISTS (SELECT 1 FROM creative_assets WHERE kind = 'character')
        OR EXISTS (SELECT 1 FROM creative_content_revision_refs WHERE role = 'character') THEN
        RAISE EXCEPTION 'character content, assets or references exist; use a forward migration';
    END IF;
END $$;

ALTER TABLE creative_asset_search
    DROP COLUMN character_ip_key,
    DROP COLUMN character_ip_name,
    DROP COLUMN character_work;
ALTER TABLE creative_content_revision_refs DROP CONSTRAINT creative_content_revision_refs_role_check;
ALTER TABLE creative_content_revision_refs ADD CONSTRAINT creative_content_revision_refs_role_check
    CHECK (role IN ('texture'));
ALTER TABLE creative_assets DROP CONSTRAINT creative_assets_kind_check;
ALTER TABLE creative_assets ADD CONSTRAINT creative_assets_kind_check
    CHECK (kind IN ('text','link','image','video','audio','model','material'));
ALTER TABLE creative_contents DROP CONSTRAINT creative_contents_kind_check;
ALTER TABLE creative_contents ADD CONSTRAINT creative_contents_kind_check
    CHECK (kind IN ('text','link','image','video','audio','scene','model','material'));
