-- A thinking model's reasoning for one assistant turn. It is model context
-- only: a thinking deployment needs it back on later requests, and it is never
-- returned to the page.
ALTER TABLE creative_agent_messages ADD COLUMN reasoning text,
 ADD CONSTRAINT creative_agent_messages_reasoning CHECK (
  reasoning IS NULL OR (role = 'assistant' AND octet_length(reasoning) BETWEEN 1 AND 1048576));
