DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM accounts WHERE password_hash IS NULL) THEN
        RAISE EXCEPTION 'auth_schema_down_blocked_new_accounts';
    END IF;
END $$;

DROP TABLE refresh_session_generations;
DROP TABLE refresh_session_families;
DROP TABLE auth_action_tokens;
DROP TABLE password_credentials;
DROP TABLE account_identities;

ALTER TABLE accounts
    DROP CONSTRAINT accounts_status_check,
    DROP COLUMN status,
    ALTER COLUMN password_hash SET NOT NULL;
