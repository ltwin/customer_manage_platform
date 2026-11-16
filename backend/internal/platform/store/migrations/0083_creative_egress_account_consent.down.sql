-- Account-level consents are the only evidence of what each photographer
-- agreed to; refuse instead of dropping them.
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM creative_egress_consents WHERE conversation_id IS NULL)
  OR EXISTS (SELECT 1 FROM creative_egress_consent_states) THEN
  RAISE EXCEPTION 'account-level egress consents exist; use a forward migration';
 END IF;
END $$;
DROP TABLE creative_egress_consent_states;
DROP INDEX creative_egress_account_consent_current;
ALTER TABLE creative_egress_consents DROP COLUMN superseded_at;
