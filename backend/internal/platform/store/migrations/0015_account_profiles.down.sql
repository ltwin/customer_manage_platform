DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM account_profiles LIMIT 1)
        OR EXISTS (SELECT 1 FROM account_profile_avatar_gc LIMIT 1) THEN
        RAISE EXCEPTION 'account profile tables are not empty; refuse destructive down';
    END IF;
END $$;

DROP TABLE IF EXISTS account_profile_avatar_gc;
DROP TABLE IF EXISTS account_profiles;
