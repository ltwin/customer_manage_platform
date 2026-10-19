-- The framework checkpoint repeats the conversation about four times, so 1 MiB
-- ended runs whose model context was a quarter full. Matches checkpointMaxBytes.
ALTER TABLE creative_agent_checkpoints DROP CONSTRAINT creative_agent_checkpoints_payload_check;
ALTER TABLE creative_agent_checkpoints ADD CONSTRAINT creative_agent_checkpoints_payload_check
 CHECK (octet_length(payload) > 0 AND octet_length(payload) <= 16777216);
