DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM creative_document_revisions WHERE actor_kind <> 'system')
  OR EXISTS (SELECT 1 FROM creative_documents WHERE trashed_at IS NOT NULL) THEN
  RAISE EXCEPTION 'document history or trash must be retained or explicitly removed before rollback';
 END IF;
END $$;
DROP TABLE creative_document_conversations;
DROP TABLE creative_document_content_refs;
DROP TABLE creative_document_revisions;
DROP INDEX creative_documents_type_list;
DROP INDEX creative_documents_list;
ALTER TABLE creative_documents
 DROP COLUMN unit_revs,
 DROP COLUMN trashed_at,
 DROP COLUMN updated_at,
 DROP COLUMN created_at;
