-- Routes and HTTP status are durable retry/reconciliation evidence.
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM llm_chat_routes)
    OR EXISTS (SELECT 1 FROM llm_requests WHERE failure_http_status IS NOT NULL) THEN
  RAISE EXCEPTION 'chat failover evidence exists; reconcile it before rollback';
 END IF;
END $$;
ALTER TABLE llm_requests DROP COLUMN failure_http_status;
DROP TABLE llm_chat_routes;
