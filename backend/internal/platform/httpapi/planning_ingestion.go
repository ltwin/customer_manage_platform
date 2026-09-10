package httpapi

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning/ingestion"
)

type planningIngestionHandlers struct {
	app          *ingestion.Application
	scopeFactory ScopeFactory
}

func registerPlanningIngestionHandlers(router gin.IRouter, app *ingestion.Application, factory ScopeFactory) {
	h := &planningIngestionHandlers{app: app, scopeFactory: factory}
	router.POST("/shoot-plans/:id/ingestion-sessions", h.create)
	router.GET("/shoot-plans/:id/ingestion-sessions/:sessionId", h.get)
	router.POST("/shoot-plans/:id/ingestion-sessions/:sessionId/preview", h.preview)
	router.POST("/shoot-plans/:id/ingestion-sessions/:sessionId/transition", h.transition)
	router.POST("/shoot-plans/:id/ingestion-sessions/:sessionId/commit", h.commit)
}

func (h *planningIngestionHandlers) scope(c *gin.Context) (store.AccountScope, bool) {
	account, ok := auth.AccountContextFrom(c.Request.Context())
	if !ok {
		abortError(c, http.StatusUnauthorized, CodeUnauthorized, "未认证")
		return store.AccountScope{}, false
	}
	return h.scopeFactory.ScopeFor(account), true
}

type createIngestionBody struct {
	ExpectedPlanRevision   int64                            `json:"expected_plan_revision"`
	SourceText             string                           `json:"source_text"`
	StagedAssetIntentCount int                              `json:"staged_asset_intent_count"`
	StagedAssetIntents     []ingestion.AssetBindingDecision `json:"staged_asset_intents,omitempty"`
}
type previewIngestionBody struct {
	ExpectedSessionRevision int64                                      `json:"expected_session_revision"`
	SourceText              *string                                    `json:"source_text,omitempty"`
	StagedAssetIntentCount  int                                        `json:"staged_asset_intent_count"`
	StagedAssetIntents      []ingestion.AssetBindingDecision           `json:"staged_asset_intents,omitempty"`
	ContentOverrides        []ingestion.ContentCandidateOverride       `json:"content_overrides,omitempty"`
	ReferenceLinkOverrides  []ingestion.ReferenceLinkCandidateOverride `json:"reference_link_overrides,omitempty"`
	ReadinessLinkOverrides  []ingestion.ReadinessLinkCandidateOverride `json:"readiness_link_overrides,omitempty"`
	ReadinessLinkSelections []ingestion.ReadinessLinkSelection         `json:"readiness_link_selections,omitempty"`
}
type transitionIngestionBody struct {
	ExpectedSessionRevision int64                  `json:"expected_session_revision"`
	State                   ingestion.SessionState `json:"state"`
}

func (h *planningIngestionHandlers) create(c *gin.Context) {
	scope, ok := h.scope(c)
	if !ok {
		return
	}
	key := c.GetHeader("Idempotency-Key")
	if key == "" {
		abortError(c, 400, CodeValidationFailed, "缺少幂等键")
		return
	}
	var body createIngestionBody
	if err := decodeStrictJSONBody(c, &body); err != nil || body.ExpectedPlanRevision < 1 || (body.SourceText == "" && len(body.StagedAssetIntents) == 0) || body.StagedAssetIntentCount < 0 || len(body.StagedAssetIntents) > ingestion.MaxStagedAssetIntents {
		abortError(c, 400, CodeValidationFailed, "请求参数不合法")
		return
	}
	session, err := h.app.CreateSession(c, scope, key, ingestion.CreateInput{PlanID: c.Param("id"), ExpectedPlanRevision: body.ExpectedPlanRevision, SourceText: body.SourceText, StagedAssetIntentCount: body.StagedAssetIntentCount, StagedAssetIntents: body.StagedAssetIntents})
	if h.fail(c, err) {
		return
	}
	c.JSON(http.StatusCreated, session)
}
func (h *planningIngestionHandlers) get(c *gin.Context) {
	scope, ok := h.scope(c)
	if !ok {
		return
	}
	session, err := h.app.GetSession(c, scope, c.Param("id"), c.Param("sessionId"))
	if h.fail(c, err) {
		return
	}
	c.JSON(http.StatusOK, session)
}
func (h *planningIngestionHandlers) preview(c *gin.Context) {
	scope, ok := h.scope(c)
	if !ok {
		return
	}
	key := c.GetHeader("Idempotency-Key")
	if key == "" {
		abortError(c, 400, CodeValidationFailed, "缺少幂等键")
		return
	}
	var body previewIngestionBody
	if err := decodeStrictJSONBody(c, &body); err != nil || body.ExpectedSessionRevision < 1 || body.StagedAssetIntentCount < 0 || len(body.StagedAssetIntents) > ingestion.MaxStagedAssetIntents || len(body.ContentOverrides) > ingestion.MaxContentCandidates || len(body.ReferenceLinkOverrides) > ingestion.MaxReferenceLinkCandidates || len(body.ReadinessLinkOverrides) > ingestion.MaxContentCandidates || len(body.ReadinessLinkSelections) > ingestion.MaxContentCandidates {
		abortError(c, 400, CodeValidationFailed, "请求参数不合法")
		return
	}
	session, err := h.app.Preview(c, scope, key, ingestion.PreviewInput{PlanID: c.Param("id"), SessionID: c.Param("sessionId"), ExpectedSessionRevision: body.ExpectedSessionRevision, SourceText: body.SourceText, StagedAssetIntentCount: body.StagedAssetIntentCount, StagedAssetIntents: body.StagedAssetIntents, ContentOverrides: body.ContentOverrides, ReferenceLinkOverrides: body.ReferenceLinkOverrides, ReadinessLinkOverrides: body.ReadinessLinkOverrides, ReadinessLinkSelections: body.ReadinessLinkSelections})
	if h.fail(c, err) {
		return
	}
	c.JSON(http.StatusOK, session)
}
func (h *planningIngestionHandlers) transition(c *gin.Context) {
	scope, ok := h.scope(c)
	if !ok {
		return
	}
	key := c.GetHeader("Idempotency-Key")
	if key == "" {
		abortError(c, 400, CodeValidationFailed, "缺少幂等键")
		return
	}
	var body transitionIngestionBody
	if err := decodeStrictJSONBody(c, &body); err != nil || body.ExpectedSessionRevision < 1 || body.State != ingestion.SessionAbandoned {
		abortError(c, 400, CodeValidationFailed, "请求参数不合法")
		return
	}
	session, err := h.app.Transition(c, scope, key, ingestion.TransitionInput{PlanID: c.Param("id"), SessionID: c.Param("sessionId"), ExpectedSessionRevision: body.ExpectedSessionRevision, State: body.State})
	if h.fail(c, err) {
		return
	}
	c.JSON(http.StatusOK, session)
}

func (h *planningIngestionHandlers) commit(c *gin.Context) {
	scope, ok := h.scope(c)
	if !ok {
		return
	}
	key := c.GetHeader("Idempotency-Key")
	if key == "" {
		abortError(c, http.StatusBadRequest, CodeValidationFailed, "缺少幂等键")
		return
	}
	var body ingestion.IngestionCommitCanonicalV1
	if err := decodeStrictJSONBody(c, &body); err != nil {
		abortError(c, http.StatusBadRequest, CodeValidationFailed, "请求参数不合法")
		return
	}
	body.PlanID, body.SessionID = c.Param("id"), c.Param("sessionId")
	result, err := h.app.Commit(c, scope, key, body)
	if h.fail(c, err) {
		return
	}
	c.JSON(http.StatusOK, result)
}
func (h *planningIngestionHandlers) fail(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, ingestion.ErrSessionNotFound), errors.Is(err, ingestion.ErrPlanNotFound):
		abortError(c, http.StatusNotFound, CodeNotFound, "资源不存在")
	case errors.Is(err, ingestion.ErrSessionRevision), errors.Is(err, ingestion.ErrPlanRevision), errors.Is(err, ingestion.ErrEditingSessionExists):
		abortError(c, http.StatusConflict, "ingestion_revision_conflict", "摄取会话版本已变化，请刷新后重试")
	case errors.Is(err, ingestion.ErrSessionTerminal):
		abortError(c, http.StatusConflict, "ingestion_session_terminal", "摄取会话已结束")
	case errors.Is(err, ingestion.ErrCandidateLimitExceeded):
		abortError(c, http.StatusUnprocessableEntity, "candidate_limit_exceeded", "候选数量超过限制")
	case errors.Is(err, ingestion.ErrCommitInvalid), errors.Is(err, ingestion.ErrReferenceTarget), errors.Is(err, ingestion.ErrReferenceURL), errors.Is(err, ingestion.ErrNoKeptCandidates):
		abortError(c, http.StatusBadRequest, CodeValidationFailed, "提交候选不合法，请刷新后重新确认")
	case errors.Is(err, ingestion.ErrSourceTooLarge):
		abortError(c, http.StatusRequestEntityTooLarge, "payload_too_large", "文本超过大小限制")
	case errors.Is(err, ingestion.ErrInvalidUTF8), errors.Is(err, ingestion.ErrReparseInput):
		abortError(c, http.StatusBadRequest, CodeValidationFailed, "请求参数不合法")
	case errors.Is(err, ingestion.ErrPreviewOverride):
		abortError(c, http.StatusBadRequest, CodeValidationFailed, "摄取编辑覆盖或关联选择不合法")
	default:
		abortError(c, http.StatusInternalServerError, CodeInternal, "内部错误")
	}
	return true
}
