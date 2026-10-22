// Package oauth is the authorization server external agents use to reach the
// MCP endpoint (epic creative-mcp-server). It owns client registration, the
// authorization code + PKCE flow, token issue and refresh rotation, and the
// external agent connections a grant creates. It knows nothing of the creative
// modules: they only ever see a connection id and its display name.
package oauth

import (
	"context"
	"encoding/json"
	"time"
)

// Client is a registered OAuth client: dynamically registered, or fetched from
// its Client ID Metadata Document. A client belongs to no account; the same
// connector can be authorized by many.
type Client struct {
	ID           string
	Registration string // cimd | dcr
	Name         string
	RedirectURIs []string
	AuthMethod   string // none | client_secret_post | client_secret_basic
	SecretHash   []byte // confidential clients only
	Metadata     json.RawMessage
	FetchedAt    *time.Time
}

// Connection is one account's authorization of one client: the identity an
// external agent signs its work with and the unit a photographer revokes.
type Connection struct {
	ID            string
	AccountID     string
	ClientID      string
	ClientName    string
	HostKind      string
	DisplayName   string
	CreatedAt     time.Time
	LastUsedAt    *time.Time
	RevokedAt     *time.Time
	RevokedReason string
	// AccountActive is the owning account's status when the row was read;
	// tokens of a disabled account are refused without touching the connection.
	AccountActive bool
}

// Active is a valid connection used within the activity window.
func (c Connection) Active(now time.Time) bool {
	return c.RevokedAt == nil && c.LastUsedAt != nil && now.Sub(*c.LastUsedAt) < ActivityWindow
}

// AuthorizationCode is what the consent page's "allow" hands the client.
type AuthorizationCode struct {
	ClientID      string
	AccountID     string
	ConnectionID  string
	RedirectURI   string
	CodeChallenge string
	Resource      string
	ExpiresAt     time.Time
	UsedAt        *time.Time
}

// RefreshToken is one link of a connection's rotation chain.
type RefreshToken struct {
	ConnectionID      string
	AccountID         string
	State             string // current | rotated | revoked
	LastUsedAt        time.Time
	AbsoluteExpiresAt time.Time
}

// RefreshSeed is a new chain link: the hash of a token handed out once.
type RefreshSeed struct {
	Hash              []byte
	AbsoluteExpiresAt time.Time
}

// RedeemOutcome is what the service decided about a locked code or refresh
// token. The store applies it in the same transaction and commits even when
// the decision also reports an error: a replay must disconnect for good.
type RedeemOutcome struct {
	// At is the service clock's time of the decision; every write uses it.
	At time.Time
	// Issue marks the code used (or the refresh token rotated) and stores Next
	// as the connection's current refresh token.
	Issue bool
	Next  RefreshSeed
	// Revoke disconnects the connection and its whole chain with this reason.
	Revoke string
}

// Store is the authorization server's persistence. Clients, codes and refresh
// tokens are looked up by key across accounts, like web refresh sessions;
// connection listing and revoking are always bounded to one account.
type Store interface {
	Client(ctx context.Context, id string) (Client, error)
	SaveClient(ctx context.Context, c Client) error
	// CreateGrant writes the new connection and its authorization code in one
	// transaction.
	CreateGrant(ctx context.Context, c Connection, codeHash []byte, code AuthorizationCode) error
	RedeemCode(ctx context.Context, codeHash []byte, decide func(AuthorizationCode, Connection) (RedeemOutcome, error)) (Connection, error)
	RedeemRefresh(ctx context.Context, tokenHash []byte, decide func(RefreshToken, Connection) (RedeemOutcome, error)) (Connection, error)
	Connection(ctx context.Context, id string) (Connection, error)
	// TouchConnection records use at most once a minute; a no-op otherwise.
	TouchConnection(ctx context.Context, id string, now time.Time) error
	ListConnections(ctx context.Context, accountID string, includeRevoked bool, limit int) ([]Connection, error)
	// RevokeConnection disconnects one of the account's connections; revoking a
	// revoked one returns it unchanged.
	RevokeConnection(ctx context.Context, accountID, id, reason string, now time.Time) (Connection, error)
	AccountActive(ctx context.Context, accountID string) (bool, error)
}
