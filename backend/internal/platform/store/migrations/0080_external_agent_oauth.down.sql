-- Connection ids are authorship in business tables; dropping the rows would
-- leave signatures nobody can resolve. Refuse instead and fix forward.
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM external_agent_connections) THEN
  RAISE EXCEPTION 'external agent connections exist; fix forward instead of rolling back';
 END IF;
END $$;
DROP TABLE oauth_refresh_tokens;
DROP TABLE oauth_authorization_codes;
DROP TABLE external_agent_connections;
DROP TABLE oauth_clients;
