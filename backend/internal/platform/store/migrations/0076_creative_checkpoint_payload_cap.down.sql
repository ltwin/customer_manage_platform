-- A larger checkpoint is a run's only resume point; refuse instead of dropping it.
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM creative_agent_checkpoints WHERE octet_length(payload) > 1048576) THEN
  RAISE EXCEPTION 'checkpoints above 1 MiB exist; remove them before rollback';
 END IF;
END $$;
ALTER TABLE creative_agent_checkpoints DROP CONSTRAINT creative_agent_checkpoints_payload_check;
ALTER TABLE creative_agent_checkpoints ADD CONSTRAINT creative_agent_checkpoints_payload_check
 CHECK (octet_length(payload) > 0 AND octet_length(payload) <= 1048576);
