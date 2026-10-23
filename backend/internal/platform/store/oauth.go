package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/samson/customer-manage-platform/backend/internal/platform/oauth"
)

// The authorization server's tables. Clients, codes and refresh tokens are found
// by key before any account is known, like web refresh sessions
// (auth_session.go); every connection read or write that a photographer drives
// is bounded to their account.

var _ oauth.Store = (*Store)(nil)

const connectionColumns = `c.id, c.account_id, c.client_id, c.client_name, c.host_kind, c.display_name,
 c.created_at, c.last_used_at, c.revoked_at, coalesce(c.revoked_reason, ''), a.status = 'active'`

func scanConnection(row pgx.Row) (oauth.Connection, error) {
	var c oauth.Connection
	err := row.Scan(&c.ID, &c.AccountID, &c.ClientID, &c.ClientName, &c.HostKind, &c.DisplayName,
		&c.CreatedAt, &c.LastUsedAt, &c.RevokedAt, &c.RevokedReason, &c.AccountActive)
	if errors.Is(err, pgx.ErrNoRows) {
		return oauth.Connection{}, oauth.ErrNotFound
	}
	return c, err
}

func (s *Store) Client(ctx context.Context, id string) (oauth.Client, error) {
	var c oauth.Client
	err := s.pool.QueryRow(ctx, `SELECT client_id, registration, client_name, redirect_uris, token_endpoint_auth_method,
 client_secret_hash, metadata, fetched_at FROM oauth_clients WHERE client_id = $1`, id).
		Scan(&c.ID, &c.Registration, &c.Name, &c.RedirectURIs, &c.AuthMethod, &c.SecretHash, &c.Metadata, &c.FetchedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return oauth.Client{}, oauth.ErrNotFound
	}
	if err != nil {
		return oauth.Client{}, fmt.Errorf("read oauth client: %w", err)
	}
	return c, nil
}

// SaveClient inserts a registration, or refreshes a metadata document's copy.
// A registration never overwrites another: its id is fresh.
func (s *Store) SaveClient(ctx context.Context, c oauth.Client) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO oauth_clients (client_id, registration, client_name, redirect_uris,
 token_endpoint_auth_method, client_secret_hash, metadata, fetched_at)
 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
 ON CONFLICT (client_id) DO UPDATE SET client_name = EXCLUDED.client_name, redirect_uris = EXCLUDED.redirect_uris,
  metadata = EXCLUDED.metadata, fetched_at = EXCLUDED.fetched_at
 WHERE oauth_clients.registration = 'cimd' AND EXCLUDED.registration = 'cimd'`,
		c.ID, c.Registration, c.Name, c.RedirectURIs, c.AuthMethod, c.SecretHash, []byte(c.Metadata), c.FetchedAt)
	if err != nil {
		return fmt.Errorf("save oauth client: %w", err)
	}
	return nil
}

func (s *Store) CreateGrant(ctx context.Context, c oauth.Connection, codeHash []byte, code oauth.AuthorizationCode) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin oauth grant: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `INSERT INTO external_agent_connections (id, account_id, client_id, client_name, host_kind,
 display_name, scopes, created_at) VALUES ($1, $2, $3, $4, $5, $6, '{creative}', $7)`,
		c.ID, c.AccountID, c.ClientID, c.ClientName, c.HostKind, c.DisplayName, c.CreatedAt); err != nil {
		return fmt.Errorf("insert external agent connection: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO oauth_authorization_codes (code_hash, client_id, account_id, connection_id,
 redirect_uri, code_challenge, resource, expires_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		codeHash, code.ClientID, code.AccountID, code.ConnectionID, code.RedirectURI, code.CodeChallenge, code.Resource, code.ExpiresAt); err != nil {
		return fmt.Errorf("insert authorization code: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit oauth grant: %w", err)
	}
	return nil
}

// RedeemCode locks the code and its connection, lets the service decide, and
// applies the decision in the same transaction. A decision that also reports a
// refusal is still committed: a replay must stay disconnected.
func (s *Store) RedeemCode(ctx context.Context, codeHash []byte, decide func(oauth.AuthorizationCode, oauth.Connection) (oauth.RedeemOutcome, error)) (oauth.Connection, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return oauth.Connection{}, fmt.Errorf("begin redeem code: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var code oauth.AuthorizationCode
	err = tx.QueryRow(ctx, `SELECT client_id, account_id, connection_id, redirect_uri, code_challenge, resource, expires_at, used_at
 FROM oauth_authorization_codes WHERE code_hash = $1 FOR UPDATE`, codeHash).
		Scan(&code.ClientID, &code.AccountID, &code.ConnectionID, &code.RedirectURI, &code.CodeChallenge, &code.Resource, &code.ExpiresAt, &code.UsedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return oauth.Connection{}, oauth.ErrNotFound
	}
	if err != nil {
		return oauth.Connection{}, fmt.Errorf("lock authorization code: %w", err)
	}
	conn, err := lockConnection(ctx, tx, code.ConnectionID)
	if err != nil {
		return oauth.Connection{}, err
	}
	outcome, refusal := decide(code, conn)
	now := outcome.At
	if outcome.Issue {
		if _, err := tx.Exec(ctx, `UPDATE oauth_authorization_codes SET used_at = $2 WHERE code_hash = $1`, codeHash, now); err != nil {
			return oauth.Connection{}, fmt.Errorf("mark authorization code used: %w", err)
		}
		if err := insertCurrentRefresh(ctx, tx, conn, outcome.Next, now); err != nil {
			return oauth.Connection{}, err
		}
	}
	if outcome.Revoke != "" {
		if conn, err = revokeLocked(ctx, tx, conn, outcome.Revoke, now); err != nil {
			return oauth.Connection{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return oauth.Connection{}, fmt.Errorf("commit redeem code: %w", err)
	}
	return conn, refusal
}

func (s *Store) RedeemRefresh(ctx context.Context, tokenHash []byte, decide func(oauth.RefreshToken, oauth.Connection) (oauth.RedeemOutcome, error)) (oauth.Connection, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return oauth.Connection{}, fmt.Errorf("begin redeem refresh: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var t oauth.RefreshToken
	err = tx.QueryRow(ctx, `SELECT connection_id, account_id, state, last_used_at, absolute_expires_at
 FROM oauth_refresh_tokens WHERE token_hash = $1 FOR UPDATE`, tokenHash).
		Scan(&t.ConnectionID, &t.AccountID, &t.State, &t.LastUsedAt, &t.AbsoluteExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return oauth.Connection{}, oauth.ErrNotFound
	}
	if err != nil {
		return oauth.Connection{}, fmt.Errorf("lock refresh token: %w", err)
	}
	conn, err := lockConnection(ctx, tx, t.ConnectionID)
	if err != nil {
		return oauth.Connection{}, err
	}
	outcome, refusal := decide(t, conn)
	now := outcome.At
	if outcome.Issue {
		if _, err := tx.Exec(ctx, `UPDATE oauth_refresh_tokens SET state = 'rotated', last_used_at = $2 WHERE token_hash = $1`, tokenHash, now); err != nil {
			return oauth.Connection{}, fmt.Errorf("rotate refresh token: %w", err)
		}
		if err := insertCurrentRefresh(ctx, tx, conn, outcome.Next, now); err != nil {
			return oauth.Connection{}, err
		}
	}
	if outcome.Revoke != "" {
		if conn, err = revokeLocked(ctx, tx, conn, outcome.Revoke, now); err != nil {
			return oauth.Connection{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return oauth.Connection{}, fmt.Errorf("commit redeem refresh: %w", err)
	}
	return conn, refusal
}

func lockConnection(ctx context.Context, tx pgx.Tx, id string) (oauth.Connection, error) {
	conn, err := scanConnection(tx.QueryRow(ctx, `SELECT `+connectionColumns+`
 FROM external_agent_connections c JOIN accounts a ON a.id = c.account_id WHERE c.id = $1 FOR UPDATE OF c`, id))
	if err != nil && !errors.Is(err, oauth.ErrNotFound) {
		return oauth.Connection{}, fmt.Errorf("lock external agent connection: %w", err)
	}
	return conn, err
}

func insertCurrentRefresh(ctx context.Context, tx pgx.Tx, conn oauth.Connection, seed oauth.RefreshSeed, now time.Time) error {
	if _, err := tx.Exec(ctx, `INSERT INTO oauth_refresh_tokens (token_hash, connection_id, account_id, state, created_at,
 last_used_at, absolute_expires_at) VALUES ($1, $2, $3, 'current', $4, $4, $5)`,
		seed.Hash, conn.ID, conn.AccountID, now, seed.AbsoluteExpiresAt); err != nil {
		return fmt.Errorf("insert refresh token: %w", err)
	}
	return nil
}

// revokeLocked disconnects a locked connection and voids its whole refresh
// chain. A connection already disconnected keeps its first reason and time.
func revokeLocked(ctx context.Context, tx pgx.Tx, conn oauth.Connection, reason string, now time.Time) (oauth.Connection, error) {
	if conn.RevokedAt == nil {
		if _, err := tx.Exec(ctx, `UPDATE external_agent_connections SET revoked_at = $2, revoked_reason = $3 WHERE id = $1`,
			conn.ID, now, reason); err != nil {
			return oauth.Connection{}, fmt.Errorf("revoke external agent connection: %w", err)
		}
		conn.RevokedAt, conn.RevokedReason = &now, reason
	}
	if _, err := tx.Exec(ctx, `UPDATE oauth_refresh_tokens SET state = 'revoked' WHERE connection_id = $1 AND state <> 'revoked'`, conn.ID); err != nil {
		return oauth.Connection{}, fmt.Errorf("revoke refresh tokens: %w", err)
	}
	return conn, nil
}

func (s *Store) Connection(ctx context.Context, id string) (oauth.Connection, error) {
	conn, err := scanConnection(s.pool.QueryRow(ctx, `SELECT `+connectionColumns+`
 FROM external_agent_connections c JOIN accounts a ON a.id = c.account_id WHERE c.id = $1`, id))
	if err != nil && !errors.Is(err, oauth.ErrNotFound) {
		return oauth.Connection{}, fmt.Errorf("read external agent connection: %w", err)
	}
	return conn, err
}

// TouchConnection is one conditional update without a lock: concurrent calls
// at most write the same minute twice.
func (s *Store) TouchConnection(ctx context.Context, id string, now time.Time) error {
	_, err := s.pool.Exec(ctx, `UPDATE external_agent_connections SET last_used_at = $2
 WHERE id = $1 AND revoked_at IS NULL AND (last_used_at IS NULL OR last_used_at < $2::timestamptz - interval '1 minute')`, id, now)
	if err != nil {
		return fmt.Errorf("touch external agent connection: %w", err)
	}
	return nil
}

func (s *Store) ListConnections(ctx context.Context, accountID string, includeRevoked bool, limit int) ([]oauth.Connection, error) {
	if accountID == "" {
		return nil, ErrEmptyAccountScope
	}
	rows, err := s.pool.Query(ctx, `SELECT `+connectionColumns+`
 FROM external_agent_connections c JOIN accounts a ON a.id = c.account_id
 WHERE c.account_id = $1 AND ($2 OR c.revoked_at IS NULL) ORDER BY c.created_at DESC, c.id DESC LIMIT $3`,
		accountID, includeRevoked, limit)
	if err != nil {
		return nil, fmt.Errorf("list external agent connections: %w", err)
	}
	defer rows.Close()
	out := []oauth.Connection{}
	for rows.Next() {
		c, err := scanConnection(rows)
		if err != nil {
			return nil, fmt.Errorf("scan external agent connection: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list external agent connections: %w", err)
	}
	return out, nil
}

func (s *Store) RevokeConnection(ctx context.Context, accountID, id, reason string, now time.Time) (oauth.Connection, error) {
	if accountID == "" {
		return oauth.Connection{}, ErrEmptyAccountScope
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return oauth.Connection{}, fmt.Errorf("begin revoke connection: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	conn, err := scanConnection(tx.QueryRow(ctx, `SELECT `+connectionColumns+`
 FROM external_agent_connections c JOIN accounts a ON a.id = c.account_id WHERE c.id = $1 AND c.account_id = $2 FOR UPDATE OF c`, id, accountID))
	if errors.Is(err, oauth.ErrNotFound) {
		return oauth.Connection{}, err
	}
	if err != nil {
		return oauth.Connection{}, fmt.Errorf("lock connection to revoke: %w", err)
	}
	if conn, err = revokeLocked(ctx, tx, conn, reason, now); err != nil {
		return oauth.Connection{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return oauth.Connection{}, fmt.Errorf("commit revoke connection: %w", err)
	}
	return conn, nil
}

func (s *Store) AccountActive(ctx context.Context, accountID string) (bool, error) {
	var active bool
	err := s.pool.QueryRow(ctx, `SELECT status = 'active' FROM accounts WHERE id = $1`, accountID).Scan(&active)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read account status: %w", err)
	}
	return active, nil
}
