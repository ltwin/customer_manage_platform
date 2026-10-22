package oauth

import (
	"context"
	"time"
)

// connectionListLimit bounds the connections page: an account has a handful.
const connectionListLimit = 50

// ListConnections returns the account's newest connections first.
func (s *Service) ListConnections(ctx context.Context, accountID string, includeRevoked bool) ([]Connection, error) {
	return s.store.ListConnections(ctx, accountID, includeRevoked, connectionListLimit)
}

// Revoke disconnects one of the account's connections: its refresh chain is
// void at once and its next MCP request is refused.
func (s *Service) Revoke(ctx context.Context, accountID, id string) (Connection, error) {
	conn, err := s.store.RevokeConnection(ctx, accountID, id, "user", s.now().UTC())
	if err == nil {
		s.logger.InfoContext(ctx, "oauth connection revoked", "connection_id", id)
	}
	return conn, err
}

// Now is the service clock, for callers that judge activity.
func (s *Service) Now() time.Time { return s.now() }
