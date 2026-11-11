ALTER TABLE creative_contents DROP CONSTRAINT creative_contents_kind_check;
ALTER TABLE creative_contents ADD CONSTRAINT creative_contents_kind_check
    CHECK (kind IN ('text','link','image','video','audio','scene','model','material','character'));
ALTER TABLE creative_assets DROP CONSTRAINT creative_assets_kind_check;
ALTER TABLE creative_assets ADD CONSTRAINT creative_assets_kind_check
    CHECK (kind IN ('text','link','image','video','audio','model','material','character'));
ALTER TABLE creative_content_revision_refs DROP CONSTRAINT creative_content_revision_refs_role_check;
ALTER TABLE creative_content_revision_refs ADD CONSTRAINT creative_content_revision_refs_role_check
    CHECK (role IN ('texture','character'));

ALTER TABLE creative_asset_search
    ADD COLUMN character_ip_key TEXT,
    ADD COLUMN character_ip_name TEXT,
    ADD COLUMN character_work TEXT;
CREATE INDEX creative_asset_search_character_ip_key ON creative_asset_search(account_id, character_ip_key)
    WHERE character_ip_key IS NOT NULL;
CREATE INDEX creative_asset_search_character_ip_name ON creative_asset_search(account_id, character_ip_name)
    WHERE character_ip_name IS NOT NULL;
CREATE INDEX creative_asset_search_character_work ON creative_asset_search(account_id, character_work)
    WHERE character_work IS NOT NULL;
