-- One write slot per conversation instead of one per account. A conversation's
-- turns are sequential by nature, but one photographer working in a second
-- conversation or another project must not wait for the first to finish. Two
-- runs touching the same canvas are kept apart by its revisions, not by this.
ALTER TABLE creative_agent_slots ADD COLUMN conversation_id TEXT;
-- An occupied slot moves to the conversation of the run holding it. A free
-- account row is nobody's; the first run of each conversation creates its own.
UPDATE creative_agent_slots s SET conversation_id = r.conversation_id
FROM creative_agent_runs r
WHERE r.account_id = s.account_id AND r.id = s.run_id;
DELETE FROM creative_agent_slots WHERE conversation_id IS NULL;
ALTER TABLE creative_agent_slots ALTER COLUMN conversation_id SET NOT NULL;
ALTER TABLE creative_agent_slots DROP CONSTRAINT creative_agent_slots_pkey;
ALTER TABLE creative_agent_slots ADD PRIMARY KEY (account_id, conversation_id);
-- A run holds at most one slot; releases and takeovers find it by the run.
CREATE UNIQUE INDEX creative_agent_slots_run
 ON creative_agent_slots(account_id, run_id) WHERE run_id IS NOT NULL;
