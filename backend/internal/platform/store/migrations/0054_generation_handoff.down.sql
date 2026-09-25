DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM creative_generation_imports) OR EXISTS(SELECT 1 FROM creative_generation_bindings) THEN
  RAISE EXCEPTION 'generation handoff has retained evidence; export and drain before rollback';
 END IF;
END $$;
DROP TABLE creative_generation_imports;
DROP TABLE creative_generation_bindings;

ALTER TABLE llm_generation_requests DROP COLUMN result_verification_required;
