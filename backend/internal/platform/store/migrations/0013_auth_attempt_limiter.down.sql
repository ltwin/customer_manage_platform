DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM auth_action_tokens WHERE purpose = 'password_reset') THEN
        RAISE EXCEPTION 'auth_hardening_down_blocked_password_reset_tokens';
    END IF;
END $$;

ALTER TABLE auth_action_tokens
    DROP CONSTRAINT auth_action_tokens_purpose_check,
    ADD CONSTRAINT auth_action_tokens_purpose_check
    CHECK (purpose IN ('email_verification', 'legacy_claim'));

DROP TABLE auth_attempt_budgets;
