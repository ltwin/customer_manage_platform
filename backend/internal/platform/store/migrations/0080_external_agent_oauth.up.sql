-- External agents (epic creative-mcp-server C1): the OAuth authorization
-- server's clients, codes and refresh tokens, and the connections they grant.
-- Only platform/oauth reads these tables; business tables keep a connection id
-- as authorship and never join back here.

CREATE TABLE oauth_clients (
 client_id TEXT PRIMARY KEY,
 registration TEXT NOT NULL CHECK (registration IN ('cimd','dcr')),
 client_name TEXT NOT NULL CHECK (length(client_name) BETWEEN 1 AND 100),
 redirect_uris TEXT[] NOT NULL CHECK (cardinality(redirect_uris) BETWEEN 1 AND 5),
 token_endpoint_auth_method TEXT NOT NULL CHECK (token_endpoint_auth_method IN ('none','client_secret_post','client_secret_basic')),
 client_secret_hash BYTEA,
 metadata JSONB NOT NULL CHECK (jsonb_typeof(metadata) = 'object' AND octet_length(metadata::text) <= 5120),
 fetched_at TIMESTAMPTZ,
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 -- A metadata document cannot carry a secret; a confidential client always has one.
 CHECK (registration = 'dcr' OR (token_endpoint_auth_method = 'none' AND fetched_at IS NOT NULL)),
 CHECK ((token_endpoint_auth_method = 'none') = (client_secret_hash IS NULL))
);

CREATE TABLE external_agent_connections (
 id TEXT PRIMARY KEY CHECK (id LIKE 'cxcon\_%'),
 account_id TEXT NOT NULL REFERENCES accounts(id),
 client_id TEXT NOT NULL,
 client_name TEXT NOT NULL CHECK (length(client_name) BETWEEN 1 AND 100),
 host_kind TEXT NOT NULL CHECK (host_kind IN ('claude','claude_code','codex','chatgpt','other')),
 display_name TEXT NOT NULL CHECK (length(display_name) BETWEEN 1 AND 100),
 scopes TEXT[] NOT NULL CHECK (scopes = '{creative}'),
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 last_used_at TIMESTAMPTZ,
 revoked_at TIMESTAMPTZ,
 revoked_reason TEXT CHECK (revoked_reason IN ('user','refresh_replay','account_disabled')),
 CHECK ((revoked_at IS NULL) = (revoked_reason IS NULL))
);
CREATE INDEX external_agent_connections_account_idx
 ON external_agent_connections (account_id, revoked_at, last_used_at DESC);

CREATE TABLE oauth_authorization_codes (
 code_hash BYTEA PRIMARY KEY,
 client_id TEXT NOT NULL,
 account_id TEXT NOT NULL REFERENCES accounts(id),
 connection_id TEXT NOT NULL REFERENCES external_agent_connections(id),
 redirect_uri TEXT NOT NULL,
 code_challenge TEXT NOT NULL CHECK (length(code_challenge) BETWEEN 43 AND 128),
 resource TEXT NOT NULL,
 expires_at TIMESTAMPTZ NOT NULL,
 used_at TIMESTAMPTZ
);
CREATE INDEX oauth_authorization_codes_expiry_idx ON oauth_authorization_codes (expires_at);

CREATE TABLE oauth_refresh_tokens (
 token_hash BYTEA PRIMARY KEY,
 connection_id TEXT NOT NULL REFERENCES external_agent_connections(id),
 account_id TEXT NOT NULL REFERENCES accounts(id),
 state TEXT NOT NULL CHECK (state IN ('current','rotated','revoked')),
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 last_used_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 absolute_expires_at TIMESTAMPTZ NOT NULL
);
CREATE UNIQUE INDEX oauth_refresh_tokens_one_current
 ON oauth_refresh_tokens (connection_id) WHERE state = 'current';
CREATE INDEX oauth_refresh_tokens_connection_idx ON oauth_refresh_tokens (connection_id);

-- The workspace shows "<host> connected" while a connection was used in the last
-- five minutes. Pages hear about a new connection, a revoke and a connection
-- turning active; the once-a-minute touch while active stays silent, and pages
-- expire the indicator themselves.
CREATE TRIGGER external_agent_connections_notify AFTER INSERT ON external_agent_connections
 FOR EACH ROW EXECUTE FUNCTION notify_creative_canvas_changed('account');
CREATE TRIGGER external_agent_connections_change_notify AFTER UPDATE ON external_agent_connections
 FOR EACH ROW WHEN (
  OLD.revoked_at IS DISTINCT FROM NEW.revoked_at
  OR (NEW.last_used_at IS NOT NULL AND (OLD.last_used_at IS NULL OR NEW.last_used_at - OLD.last_used_at > interval '5 minutes'))
 ) EXECUTE FUNCTION notify_creative_canvas_changed('account');
