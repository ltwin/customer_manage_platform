package httpapi

import (
	"errors"

	"github.com/gin-gonic/gin"
	"github.com/samson/customer-manage-platform/backend/internal/creativeagent"
	"github.com/samson/customer-manage-platform/backend/internal/creativeskill"
)

func registerCreativeAgent(r *gin.RouterGroup, h *handlers) {
	w := ServerInterfaceWrapper{Handler: h, ErrorHandler: func(c *gin.Context, _ error, status int) {
		abortError(c, status, CodeValidationFailed, "请求参数不合法")
	}}
	r.GET("/creative/agent/catalog", w.GetCreativeAgentCatalog)
	r.GET("/creative/agent/skills", w.ListCreativeAgentSkills)
	r.GET("/creative/agent/skills/:id/versions/:version_id", w.GetCreativeAgentSkillVersion)
	r.GET("/creative/canvases/:id/conversations", w.ListCreativeAgentConversations)
	r.POST("/creative/canvases/:id/conversations", w.CreateCreativeAgentConversation)
	r.GET("/creative/conversations/:id/messages", w.ListCreativeAgentMessages)
	r.POST("/creative/conversations/:id/runs", w.CreateCreativeAgentRun)
	r.GET("/creative/agent-runs/:id", w.GetCreativeAgentRun)
	r.POST("/creative/conversations/:id/egress-consents", w.GrantCreativeEgressConsent)
	r.POST("/creative/egress-consents/:id/revoke", w.RevokeCreativeEgressConsent)
}

// creativeAgentError maps the assistant's own refusals. creativeError delegates
// the same cases here, so this function must never call back into it: an error
// added there but not here would recurse until the stack overflows rather than
// degrade to a 500.
func creativeAgentError(c *gin.Context, err error) {
	var busy creativeagent.BusyError
	switch {
	case errors.As(err, &busy):
		// The account has one write slot. Naming the run that holds it turns
		// "try later" into something the photographer can actually open.
		var details ErrorDetails
		if buildErr := details.FromCreativeAgentBusyDetails(
			CreativeAgentBusyDetails{ActiveRunId: busy.ActiveRunID}); buildErr != nil {
			_ = c.Error(buildErr)
			return
		}
		abortErrorWithTypedDetails(c, 429, "creative_agent_busy", "已有一次创作正在进行，完成后再提交", details)
	case errors.Is(err, creativeagent.ErrConsentRevoked):
		abortError(c, 409, "creative_egress_revoked", "该授权已撤销；已发出的内容无法召回")
	case errors.Is(err, creativeagent.ErrEgressRequired):
		abortError(c, 403, "creative_egress_required", "本次提交超出已授权的外发范围")
	case errors.Is(err, creativeagent.ErrBudgetExceeded):
		abortError(c, 429, "creative_llm_budget_exceeded", "本月模型额度已用尽")
	case errors.Is(err, creativeagent.ErrModelCapability):
		abortError(c, 422, "creative_model_capability_missing", "所选模型在本部署暂不可用")
	case errors.Is(err, creativeagent.ErrUnsupportedSegment):
		// The request is well formed and the photographer can act on it by
		// choosing something else, so answering "malformed" would be a lie.
		abortError(c, 422, "creative_segment_unsupported", "本部署暂不能执行该片段")
	case errors.Is(err, creativeagent.ErrRunState):
		abortError(c, 409, "creative_run_state_conflict", "该运行的当前状态不允许这个动作")
	case errors.Is(err, creativeagent.ErrLimit):
		abortError(c, 413, "creative_context_limit", "内容超过本次对话的长度上限")
	case errors.Is(err, creativeagent.ErrNotFound):
		abortError(c, 404, CodeNotFound, "会话、运行或授权不存在")
	default:
		_ = c.Error(err) // Shared middleware records and renders unexpected errors.
	}
}

// creativeSkillError maps the skill store's own refusals. Its error values are
// deliberately distinct from creativemedia's — the two share an object port, so
// a shared sentinel would have the edge answer "upload session not found" about
// a skill file — and that distinction only means something if both are mapped
// here. A skill nobody may see is a 404: refusing across accounts is reported as
// absence, and rendering that as a server fault would be both wrong and a way to
// fill the error log with someone else's guesses.
func creativeSkillError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, creativeskill.ErrNotFound):
		abortError(c, 404, CodeNotFound, "Skill 或版本不存在")
	case errors.Is(err, creativeskill.ErrLimit):
		abortError(c, 413, "creative_skill_limit", "Skill 内容超过本部署的上限")
	case errors.Is(err, creativeskill.ErrContentMismatch):
		// On a read this means a frozen version's object no longer hashes to what
		// it declared (§9). That is a server-side integrity fault the caller can
		// do nothing about, so it is a 5xx — a 4xx would keep it out of the alert
		// path and tell the client to retry something that cannot succeed. The
		// same value means "the bytes contradict the declaration" on the import
		// path, where 409 would be right; that path is the CLI and never reaches
		// an HTTP edge.
		abortError(c, 500, "creative_skill_content_mismatch", "Skill 资源与冻结时的声明不一致")
	case errors.Is(err, creativeskill.ErrResourceUnavailable):
		abortError(c, 503, "creative_skill_unavailable", "Skill 资源暂时不可读，请稍后重试")
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
	// The catalog carries this account's own skill summary since S3, so it is a
	// private directory like the two below and not the deployment-wide document
	// it used to be.
	noStore(c)
	c.JSON(200, value)
}

// noStore marks a private directory answer as uncacheable. The account's own
// skills and the platform catalog it may see are not a public document, and an
// intermediate keeping a copy would serve one account's view to another.
func noStore(c *gin.Context) { c.Header("Cache-Control", "no-store") }

func (h *handlers) ListCreativeAgentSkills(c *gin.Context, p ListCreativeAgentSkillsParams) {
	agent, ok := h.agentService(c)
	if !ok {
		return
	}
	scope, ok := h.creativeScope(c)
	if !ok {
		return
	}
	limit, query, cursor := 20, "", ""
	if p.Limit != nil {
		limit = *p.Limit
	}
	if p.Q != nil {
		query = *p.Q
	}
	if p.Cursor != nil {
		cursor = *p.Cursor
	}
	value, err := agent.ListSkills(c.Request.Context(), scope, query, cursor, limit)
	if err != nil {
		creativeError(c, err)
		return
	}
	noStore(c)
	c.JSON(200, value)
}

func (h *handlers) GetCreativeAgentSkillVersion(c *gin.Context, id string, versionID string) {
	agent, ok := h.agentService(c)
	if !ok {
		return
	}
	scope, ok := h.creativeScope(c)
	if !ok {
		return
	}
	value, err := agent.SkillVersion(c.Request.Context(), scope, id, versionID)
	if err != nil {
		creativeError(c, err)
		return
	}
	noStore(c)
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

// CreateCreativeAgentRun answers 202: the submission was accepted, not
// completed. The run's own resource is where the outcome appears, and a future
// result never rewrites this receipt.
func (h *handlers) CreateCreativeAgentRun(c *gin.Context, id string, _ CreateCreativeAgentRunParams) {
	agent, ok := h.agentService(c)
	if !ok {
		return
	}
	h.creativeWrite(c, "conversation_id", id, agent.CreateRun)
}

func (h *handlers) GetCreativeAgentRun(c *gin.Context, id string) {
	agent, ok := h.agentService(c)
	if !ok {
		return
	}
	scope, ok := h.creativeScope(c)
	if !ok {
		return
	}
	run, err := agent.ReadRun(c.Request.Context(), scope, id)
	if err != nil {
		creativeError(c, err)
		return
	}
	// A run is one account's own work in progress, not a document an
	// intermediate may keep a copy of.
	noStore(c)
	c.JSON(200, run)
}

func (h *handlers) RevokeCreativeEgressConsent(c *gin.Context, id string, _ RevokeCreativeEgressConsentParams) {
	agent, ok := h.agentService(c)
	if !ok {
		return
	}
	h.creativeWrite(c, "consent_id", id, agent.RevokeConsent)
}
