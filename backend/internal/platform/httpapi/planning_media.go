package httpapi

import (
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/samson/customer-manage-platform/backend/internal/planningmedia"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/idempotency"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning"
)

type planningMediaHandlers struct {
	app          *planningmedia.Application
	scopeFactory ScopeFactory
}

const maxPlanningMediaMultipartBytes = planningmedia.MaxUploadBytes + 2*1024*1024

func registerPlanningMediaHandlers(router gin.IRouter, app *planningmedia.Application, factory ScopeFactory) {
	h := &planningMediaHandlers{app: app, scopeFactory: factory}
	router.GET("/shoot-plans/:id/assets", h.list)
	router.POST("/shoot-plans/:id/assets", h.upload)
	router.POST("/shoot-plans/:id/assets/:assetId/bindings", h.bind)
	router.DELETE("/shoot-plans/:id/assets/:assetId/bindings/:bindingId", h.release)
	router.GET("/shoot-plans/:id/assets/:assetId/content", h.content)
}

func (h *planningMediaHandlers) scope(c *gin.Context) (store.AccountScope, bool) {
	account, ok := auth.AccountContextFrom(c.Request.Context())
	if !ok {
		abortError(c, http.StatusUnauthorized, CodeUnauthorized, "未认证")
		return store.AccountScope{}, false
	}
	return creativeLegacyScope(c, h.scopeFactory.ScopeFor(account)), true
}
func (h *planningMediaHandlers) list(c *gin.Context) {
	scope, ok := h.scope(c)
	if !ok {
		return
	}
	limit := 40
	if raw := c.Query("page_size"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			abortError(c, 400, CodeValidationFailed, "请求参数不合法")
			return
		}
		if value < 1 || value > 100 {
			abortError(c, 400, CodeValidationFailed, "请求参数不合法")
			return
		}
		limit = value
	}
	result, err := h.app.ListForPlan(c, scope, c.Param("id"), c.Query("cursor"), limit)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
func (h *planningMediaHandlers) upload(c *gin.Context) {
	scope, ok := h.scope(c)
	if !ok {
		return
	}
	key := c.GetHeader("Idempotency-Key")
	if key == "" {
		abortError(c, 400, CodeValidationFailed, "缺少幂等键")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxPlanningMediaMultipartBytes)
	if err := c.Request.ParseMultipartForm(1 << 20); err != nil {
		abortError(c, 400, CodeValidationFailed, "上传内容不合法")
		return
	}
	if c.Request.MultipartForm != nil {
		defer func() { _ = c.Request.MultipartForm.RemoveAll() }()
	}
	file, header, err := c.Request.FormFile("image")
	if err != nil {
		abortError(c, 400, CodeValidationFailed, "缺少图片")
		return
	}
	defer func() { _ = file.Close() }()
	spool, err := planningmedia.NewUploadSpool(c, file, c.PostForm("declared_media_type"))
	if errors.Is(err, planningmedia.ErrImageSizeInvalid) {
		abortError(c, 413, "payload_too_large", "图片过大")
		return
	}
	if errors.Is(err, planningmedia.ErrImageFormatInvalid) || err != nil {
		abortError(c, 415, "unsupported_media_type", "图片格式不支持或与声明不一致")
		return
	}
	defer func() { _ = spool.Close() }()
	var rights planningmedia.RightsDeclarationInput
	if err := decodeStrictJSON([]byte(c.PostForm("rights")), &rights); err != nil {
		abortError(c, 400, CodeValidationFailed, "权利声明不合法")
		return
	}
	revision, err := strconv.ParseInt(c.PostForm("expected_plan_revision"), 10, 64)
	if err != nil || revision < 1 {
		abortError(c, 400, CodeValidationFailed, "请求参数不合法")
		return
	}
	result, err := h.app.Upload(c, scope, key, planningmedia.UploadInput{PlanID: c.Param("id"), ExpectedPlanRevision: revision, DisplayName: header.Filename, Content: spool, Rights: rights, IntendedPurpose: planningmedia.Purpose(c.PostForm("intended_purpose"))})
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}
func (h *planningMediaHandlers) bind(c *gin.Context) {
	scope, ok := h.scope(c)
	if !ok {
		return
	}
	key := c.GetHeader("Idempotency-Key")
	if key == "" {
		abortError(c, http.StatusBadRequest, CodeValidationFailed, "缺少幂等键")
		return
	}
	var input planningmedia.CreateBindingInput
	if err := decodeStrictJSONBody(c, &input); err != nil {
		abortError(c, 400, CodeValidationFailed, "请求参数不合法")
		return
	}
	input.PlanID = c.Param("id")
	input.AssetID = c.Param("assetId")
	result, err := h.app.CreateBinding(c, scope, key, input)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
func (h *planningMediaHandlers) release(c *gin.Context) {
	scope, ok := h.scope(c)
	if !ok {
		return
	}
	key := c.GetHeader("Idempotency-Key")
	if key == "" {
		abortError(c, http.StatusBadRequest, CodeValidationFailed, "缺少幂等键")
		return
	}
	var body struct {
		ExpectedPlanRevision    int64 `json:"expected_plan_revision"`
		ExpectedAssetRevision   int64 `json:"expected_asset_revision"`
		ExpectedBindingRevision int64 `json:"expected_binding_revision"`
	}
	if err := decodeStrictJSONBody(c, &body); err != nil {
		abortError(c, 400, CodeValidationFailed, "请求参数不合法")
		return
	}
	result, err := h.app.ReleaseBinding(c, scope, key, c.Param("id"), c.Param("assetId"), c.Param("bindingId"), body.ExpectedPlanRevision, body.ExpectedAssetRevision, body.ExpectedBindingRevision)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
func (h *planningMediaHandlers) content(c *gin.Context) {
	scope, ok := h.scope(c)
	if !ok {
		return
	}
	result, err := h.app.OpenDisplay(c, scope, c.Param("id"), c.Param("assetId"), c.Query("v"))
	if err != nil {
		h.fail(c, err)
		return
	}
	defer func() { _ = result.Close() }()
	c.Header("Content-Type", result.MediaType)
	c.Header("Content-Length", strconv.FormatInt(result.Size, 10))
	c.Header("ETag", strconv.Quote(result.Checksum))
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.DataFromReader(http.StatusOK, result.Size, result.MediaType, result.Reader, nil)
}
func decodeStrictJSONBody(c *gin.Context, destination any) error {
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, 1<<20))
	if err != nil {
		return err
	}
	return decodeStrictJSON(body, destination)
}
func (h *planningMediaHandlers) fail(c *gin.Context, err error) {
	if abortLegacyWriteError(c, err) {
		return
	}
	switch {
	case errors.Is(err, planningmedia.ErrNotFound):
		abortError(c, 404, CodeNotFound, "资源不存在")
	case errors.Is(err, planningmedia.ErrAssetCorrupt):
		abortError(c, http.StatusServiceUnavailable, "asset_corrupt", "素材完整性校验失败")
	case errors.Is(err, planningmedia.ErrAssetReferenceStale):
		abortError(c, 409, "asset_reference_stale", "素材引用已过期")
	case errors.Is(err, planningmedia.ErrAssetRevisionConflict), errors.Is(err, planningmedia.ErrBindingAlreadyReleased), errors.Is(err, planningmedia.ErrBindingAlreadyActive), errors.Is(err, planningmedia.ErrAssetGCPending), errors.Is(err, planningmedia.ErrAssetState), errors.Is(err, planningmedia.ErrPlanArchived), errors.Is(err, planningmedia.ErrPlanReopenRequired):
		abortError(c, http.StatusConflict, stringErrorCode(err), "素材状态或版本已变化，请刷新后重试")
	case errors.Is(err, planningmedia.ErrHolderAuthorizationRequired):
		abortError(c, http.StatusServiceUnavailable, "holder_authorization_unavailable", "素材授权服务暂不可用")
	case errors.Is(err, planningmedia.ErrPurposeNotPermitted), errors.Is(err, planningmedia.ErrRightsCombinationInvalid), errors.Is(err, planningmedia.ErrGenerationGrantRequired):
		abortError(c, 400, stringErrorCode(err), "素材来源或用途不允许")
	case errors.Is(err, idempotency.ErrValidation):
		abortError(c, http.StatusBadRequest, CodeValidationFailed, "幂等键或请求不合法")
	case errors.Is(err, planningmedia.ErrValidation):
		abortError(c, http.StatusBadRequest, CodeValidationFailed, "请求参数不合法")
	case errors.Is(err, shootplanning.ErrPlanNotFound), errors.Is(err, shootplanning.ErrShotNotFound):
		abortError(c, http.StatusNotFound, CodeNotFound, "资源不存在")
	case errors.Is(err, shootplanning.ErrPlanRevisionConflict):
		abortError(c, http.StatusConflict, "plan_revision_conflict", "策划版本已变化，请刷新后重试")
	default:
		_ = c.Error(err)
	}
}
func stringErrorCode(err error) string {
	if errors.Is(err, planningmedia.ErrAssetRevisionConflict) {
		return "asset_revision_conflict"
	}
	if errors.Is(err, planningmedia.ErrBindingAlreadyActive) {
		return "binding_already_active"
	}
	if errors.Is(err, planningmedia.ErrBindingAlreadyReleased) {
		return "binding_already_released"
	}
	if errors.Is(err, planningmedia.ErrAssetGCPending) {
		return "asset_gc_in_progress"
	}
	if errors.Is(err, planningmedia.ErrAssetState) {
		return "asset_state_invalid"
	}
	if errors.Is(err, planningmedia.ErrPlanArchived) {
		return "plan_archived"
	}
	if errors.Is(err, planningmedia.ErrPlanReopenRequired) {
		return "plan_reopen_required"
	}
	if errors.Is(err, planningmedia.ErrGenerationGrantRequired) {
		return "generation_reference_grant_required"
	}
	if errors.Is(err, planningmedia.ErrPurposeNotPermitted) {
		return "purpose_not_permitted"
	}
	return "rights_combination_invalid"
}
