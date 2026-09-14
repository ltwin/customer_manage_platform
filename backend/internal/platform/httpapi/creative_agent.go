package httpapi

import (
	"errors"

	"github.com/gin-gonic/gin"
	"github.com/samson/customer-manage-platform/backend/internal/creativeagent"
)

func registerCreativeAgent(r *gin.RouterGroup, h *handlers) {
	w := ServerInterfaceWrapper{Handler: h, ErrorHandler: func(c *gin.Context, _ error, status int) {
		abortError(c, status, CodeValidationFailed, "请求参数不合法")
	}}
	r.GET("/creative/agent/catalog", w.GetCreativeAgentCatalog)
	r.GET("/creative/canvases/:id/conversations", w.ListCreativeAgentConversations)
	r.POST("/creative/canvases/:id/conversations", w.CreateCreativeAgentConversation)
	r.GET("/creative/conversations/:id/messages", w.ListCreativeAgentMessages)
	r.POST("/creative/conversations/:id/egress-consents", w.GrantCreativeEgressConsent)
	r.POST("/creative/egress-consents/:id/revoke", w.RevokeCreativeEgressConsent)
}

// creativeAgentError maps the assistant's own refusals. creativeError delegates
// the same cases here, so this function must never call back into it: an error
// added there but not here would recurse until the stack overflows rather than
// degrade to a 500.
func creativeAgentError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, creativeagent.ErrConsentRevoked):
		abortError(c, 409, "creative_egress_revoked", "该授权已撤销；已发出的内容无法召回")
	case errors.Is(err, creativeagent.ErrLimit):
		abortError(c, 413, "creative_context_limit", "内容超过本次对话的长度上限")
	case errors.Is(err, creativeagent.ErrNotFound):
		abortError(c, 404, CodeNotFound, "会话或授权不存在")
	default:
		_ = c.Error(err) // Shared middleware records and renders unexpected errors.
	}
}

func (h *handlers) agentService(c *gin.Context) (*creativeagent.Service, bool) {
	if h.creativeAgent == nil {
		abortError(c, 503, "creative_dependency_unavailable", "创作助手暂不可用")
		return nil, false
	}
	return h.creativeAgent, true
}

func (h *handlers) GetCreativeAgentCatalog(c *gin.Context) {
	agent, ok := h.agentService(c)
	if !ok {
		return
	}
	scope, ok := h.creativeScope(c)
	if !ok {
		return
	}
	value, err := agent.Catalog(c.Request.Context(), scope)
	if err != nil {
		creativeError(c, err)
		return
	}
	c.JSON(200, value)
}

func (h *handlers) ListCreativeAgentConversations(c *gin.Context, id string, p ListCreativeAgentConversationsParams) {
	agent, ok := h.agentService(c)
	if !ok {
		return
	}
	scope, ok := h.creativeScope(c)
	if !ok {
		return
	}
	limit, cursor := 30, ""
	if p.Limit != nil {
		limit = *p.Limit
	}
	if p.Cursor != nil {
		cursor = *p.Cursor
	}
	value, err := agent.ListConversations(c.Request.Context(), scope, id, limit, cursor)
	if err != nil {
		creativeError(c, err)
		return
	}
	c.JSON(200, value)
}

func (h *handlers) CreateCreativeAgentConversation(c *gin.Context, id string, _ CreateCreativeAgentConversationParams) {
	agent, ok := h.agentService(c)
	if !ok {
		return
	}
	h.creativeWrite(c, "canvas_id", id, agent.CreateConversation)
}

func (h *handlers) ListCreativeAgentMessages(c *gin.Context, id string, p ListCreativeAgentMessagesParams) {
	agent, ok := h.agentService(c)
	if !ok {
		return
	}
	scope, ok := h.creativeScope(c)
	if !ok {
		return
	}
	limit, before := 30, ""
	if p.Limit != nil {
		limit = *p.Limit
	}
	if p.BeforeOrdinal != nil {
		before = *p.BeforeOrdinal
	}
	value, err := agent.ListMessages(c.Request.Context(), scope, id, before, limit)
	if err != nil {
		creativeError(c, err)
		return
	}
	c.JSON(200, value)
}

func (h *handlers) GrantCreativeEgressConsent(c *gin.Context, id string, _ GrantCreativeEgressConsentParams) {
	agent, ok := h.agentService(c)
	if !ok {
		return
	}
	h.creativeWrite(c, "conversation_id", id, agent.GrantConsent)
}

func (h *handlers) RevokeCreativeEgressConsent(c *gin.Context, id string, _ RevokeCreativeEgressConsentParams) {
	agent, ok := h.agentService(c)
	if !ok {
		return
	}
	h.creativeWrite(c, "consent_id", id, agent.RevokeConsent)
}
