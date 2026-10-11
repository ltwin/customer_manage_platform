-- Photographers rename and archive their own conversations.
-- title_source decides whether automatic naming may still write: only a
-- 'default' title is replaced, so a name the photographer chose always wins.
ALTER TABLE creative_agent_conversations
 ADD COLUMN title_source TEXT NOT NULL DEFAULT 'default'
  CHECK(title_source IN ('default','generated','manual')),
 -- Archiving only hides a conversation from the list and stops new runs; its
 -- messages, runs and retention roots stay exactly as they were.
 ADD COLUMN archived_at TIMESTAMPTZ;
-- Before this column the client always created conversations as 新对话, so any
-- other title was written by naming.
UPDATE creative_agent_conversations SET title_source='generated' WHERE title<>'新对话';
