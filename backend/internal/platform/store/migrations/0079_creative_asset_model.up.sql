-- scene-model-library: a model revision can be saved into the asset library.
ALTER TABLE creative_assets DROP CONSTRAINT creative_assets_kind_check;
ALTER TABLE creative_assets ADD CONSTRAINT creative_assets_kind_check CHECK (kind IN ('text','link','image','video','audio','model'));
