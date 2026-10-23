package httpapi

import (
	"context"
	"errors"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/samson/customer-manage-platform/backend/internal/creativeagent"
	"github.com/samson/customer-manage-platform/backend/internal/creativemcp"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/oauth"
)

// realtimeWatcher is the store's view of live page subscriptions.
type realtimeWatcher interface {
	RealtimeWatching(accountID, kind string) bool
}

// registerMCP mounts the MCP endpoint. Its token check is the authorization
// server's; the account scope comes only from the token's connection.
func registerMCP(r *gin.Engine, h *handlers, agent *creativeagent.Service, timezone AccountTimezoneProvider) error {
	cfg := creativeagent.ExternalConfig{BaseURL: h.publicBaseURL}
	if w, ok := h.scopeFactory.(realtimeWatcher); ok {
		cfg.Watching = func(account string) bool { return w.RealtimeWatching(account, "canvas") }
	}
	if timezone != nil {
		cfg.Timezone = timezone.TimezoneForAccount
	}
	handler, err := creativemcp.New(creativemcp.Options{
		Tools:               agent.ExternalTools(cfg),
		Verify:              h.verifyMCPAccess,
		ResourceMetadataURL: h.oauth.ResourceMetadataURL(),
		Logger:              h.logger,
	})
	if err != nil {
		return err
	}
	serve := gin.WrapH(handler)
	for _, method := range []string{"GET", "POST", "DELETE"} {
		r.Handle(method, "/mcp", serve)
	}
	return nil
}

func (h *handlers) verifyMCPAccess(ctx context.Context, token string) (creativeagent.ExternalPrincipal, time.Time, error) {
	conn, exp, err := h.oauth.VerifyAccess(ctx, token)
	if errors.Is(err, oauth.ErrInvalidToken) {
		return creativeagent.ExternalPrincipal{}, time.Time{}, creativemcp.ErrUnauthorized
	}
	if err != nil {
		return creativeagent.ExternalPrincipal{}, time.Time{}, err
	}
	scope := h.scopeFactory.ScopeFor(auth.AccountContext{AccountID: conn.AccountID})
	return creativeagent.ExternalPrincipal{Scope: scope, ConnectionID: conn.ID, DisplayName: conn.DisplayName, HostKind: conn.HostKind}, exp, nil
}
