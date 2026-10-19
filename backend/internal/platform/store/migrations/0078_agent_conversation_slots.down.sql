-- Back to one slot per account. Two occupied conversations of one account have
-- no single row to fold into, and dropping either would strand its run with no
-- slot to release. Refuse and fix forward instead.
DO $$ BEGIN
 IF EXISTS (
  SELECT 1 FROM creative_agent_slots WHERE run_id IS NOT NULL
  GROUP BY account_id HAVING count(*) > 1) THEN
  RAISE EXCEPTION 'an account holds more than one agent write slot; fix forward instead of rolling back';
 END IF;
END $$;
DROP INDEX creative_agent_slots_run;
-- Keep the occupied row if there is one; the free ones are interchangeable.
DELETE FROM creative_agent_slots s
USING (
 SELECT account_id, conversation_id,
  row_number() OVER (PARTITION BY account_id ORDER BY run_id IS NULL, conversation_id) AS n
 FROM creative_agent_slots
) k
WHERE k.account_id = s.account_id AND k.conversation_id = s.conversation_id AND k.n > 1;
ALTER TABLE creative_agent_slots DROP CONSTRAINT creative_agent_slots_pkey;
ALTER TABLE creative_agent_slots DROP COLUMN conversation_id;
ALTER TABLE creative_agent_slots ADD PRIMARY KEY (account_id);
