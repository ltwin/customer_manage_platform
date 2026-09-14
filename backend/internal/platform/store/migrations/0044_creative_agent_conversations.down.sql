-- Messages are the photographer's permanent reading record and consents are the
-- evidence of what was authorised to leave the account. Refuse a lossy rollback
-- once either exists; export first.
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM creative_agent_messages)
  OR EXISTS(SELECT 1 FROM creative_egress_consents) THEN
  RAISE EXCEPTION 'agent conversation history requires export before rollback';
 END IF;
END $$;
DROP TABLE creative_egress_consent_contents,creative_egress_consents,
 creative_message_content_refs,creative_agent_messages,creative_agent_conversations;
