package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/oapi-codegen/nullable"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/oauth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func registerAgentConnections(r *gin.RouterGroup, h *handlers) {
	w := ServerInterfaceWrapper{Handler: h, ErrorHandler: func(c *gin.Context, _ error, status int) {
		abortError(c, status, CodeValidationFailed, "请求参数不合法")
	}}
	r.GET("/creative/agent-connections", w.ListExternalAgentConnections)
	r.POST("/creative/agent-connections/:id/revoke", w.RevokeExternalAgentConnection)
	r.POST("/oauth/authorize/decision", w.DecideOAuthAuthorization)
}

func connectionView(c oauth.Connection, now func() time.Time) ExternalAgentConnection {
	return ExternalAgentConnection{Id: c.ID, DisplayName: c.DisplayName, HostKind: ExternalAgentConnectionHostKind(c.HostKind),
		CreatedAt: c.CreatedAt, LastUsedAt: nullableTime(c.LastUsedAt), RevokedAt: nullableTime(c.RevokedAt), Active: c.Active(now())}
}

func nullableTime(t *time.Time) nullable.Nullable[time.Time] {
	if t == nil {
		return nullable.NewNullNullable[time.Time]()
	}
	return nullable.NewNullableWithValue(t.UTC())
}

// agentConnectionAccount checks the capability the operation needs and names
// the signed-in account. Connections are an authorization-server record, so
// the account id is passed on rather than a business scope.
func (h *handlers) agentConnectionAccount(c *gin.Context, capability string) (string, bool) {
	if h.oauth == nil {
		abortError(c, http.StatusNotFound, CodeNotFound, "外部助手暂未开放")
		return "", false
	}
	scope, ok := h.creativeScope(c)
	if !ok {
		return "", false
	}
	err := scope.WithTxScope(c.Request.Context(), func(tx store.TxAccountScope) error {
		return tx.RequireCreativeCapability(c.Request.Context(), capability)
	})
	if err != nil {
		creativeError(c, err)
		return "", false
	}
	ac, _ := auth.AccountContextFrom(c.Request.Context())
	return ac.AccountID, true
}

func (h *handlers) ListExternalAgentConnections(c *gin.Context, params ListExternalAgentConnectionsParams) {
	account, ok := h.agentConnectionAccount(c, "creative_read")
	if !ok {
		return
	}
	includeRevoked := params.IncludeRevoked != nil && *params.IncludeRevoked
	conns, err := h.oauth.ListConnections(c.Request.Context(), account, includeRevoked)
	if err != nil {
		_ = c.Error(err)
		return
	}
	out := ExternalAgentConnectionList{McpUrl: h.oauth.Resource(), Items: []ExternalAgentConnection{}}
	for _, conn := range conns {
		view := connectionView(conn, h.oauth.Now)
		if params.Active != nil && *params.Active && !view.Active {
			continue
		}
		out.Items = append(out.Items, view)
	}
	c.JSON(http.StatusOK, out)
}

func (h *handlers) RevokeExternalAgentConnection(c *gin.Context, id string) {
	account, ok := h.agentConnectionAccount(c, "manual_write")
	if !ok {
		return
	}
	conn, err := h.oauth.Revoke(c.Request.Context(), account, id)
	switch {
	case errors.Is(err, oauth.ErrNotFound):
		abortError(c, http.StatusNotFound, CodeNotFound, "连接不存在")
	case err != nil:
		_ = c.Error(err)
	default:
		c.JSON(http.StatusOK, connectionView(conn, h.oauth.Now))
	}
}
