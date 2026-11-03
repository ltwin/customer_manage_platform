-- scene-pbr-textures (epic scene-editor-3d C5c1): a material slot can be saved
-- into the asset library, and a revision can hold the images it textures with.
ALTER TABLE creative_contents DROP CONSTRAINT creative_contents_kind_check;
ALTER TABLE creative_contents ADD CONSTRAINT creative_contents_kind_check CHECK (kind IN ('text','link','image','video','audio','scene','model','material'));
ALTER TABLE creative_assets DROP CONSTRAINT creative_assets_kind_check;
ALTER TABLE creative_assets ADD CONSTRAINT creative_assets_kind_check CHECK (kind IN ('text','link','image','video','audio','model','material'));
-- A revision holding another revision: a material or a library model copy
-- holding its textures. Derived from the holder's payload (which stays the
-- source of truth), written with the holder and never changed.
CREATE TABLE creative_content_revision_refs (
 account_id TEXT NOT NULL, holder_revision_id TEXT NOT NULL, content_revision_id TEXT NOT NULL,
 role TEXT NOT NULL CHECK (role IN ('texture')),
 PRIMARY KEY (account_id, holder_revision_id, content_revision_id),
 CHECK (holder_revision_id <> content_revision_id),
 FOREIGN KEY (account_id, holder_revision_id) REFERENCES creative_content_revisions(account_id, id) ON DELETE CASCADE,
 FOREIGN KEY (account_id, content_revision_id) REFERENCES creative_content_revisions(account_id, id)
);
CREATE INDEX creative_content_revision_refs_held ON creative_content_revision_refs(account_id, content_revision_id);
