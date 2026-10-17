-- scene-document-shell: a scene document stores its snapshot as content of kind 'scene'.
ALTER TABLE creative_contents DROP CONSTRAINT creative_contents_kind_check;
ALTER TABLE creative_contents ADD CONSTRAINT creative_contents_kind_check CHECK (kind IN ('text','link','image','video','audio','scene'));
