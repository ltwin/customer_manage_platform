-- Dropping reasoning loses no conversation content: later requests send an
-- empty reasoning instead, which the vendor accepts.
ALTER TABLE creative_agent_messages DROP COLUMN reasoning;
