package httpapi

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/oapi-codegen/nullable"

	"github.com/samson/customer-manage-platform/backend/internal/planshare"
	"github.com/samson/customer-manage-platform/backend/internal/platform/idempotency"
	shootplanningapi "github.com/samson/customer-manage-platform/backend/internal/shootplanning/httpcontract"
)

const (
	CodeShareGenerationExists = "share_generation_exists"
	CodeFullViewNotEligible   = "full_view_not_eligible"
	CodeExpiryQuoteExpired    = "expiry_quote_expired"
	CodeExpiryQuoteStale      = "expiry_quote_stale"
	CodeShareStale            = "share_stale"
	CodeShareInvalidState     = "share_invalid_state"
)

func (h *shootPlanningHandlers) GetShootPlanShares(
	c *gin.Context,
	id shootplanningapi.Id,
	params shootplanningapi.GetShootPlanSharesParams,
) {
	scope, ok := h.scope(c)
	if !ok {
		return
	}
	if h.planShare == nil {
		_ = c.Error(errors.New("plan share route dependencies missing"))
		return
	}
	if !onlyQueryParameters(c, "offerCursor", "offerLimit") {
		abortShootPlanningValidation(c)
		return
	}
	query := planshare.ManagementQuery{OfferLimit: 50}
	if params.OfferCursor != nil {
		query.OfferCursor = *params.OfferCursor
	}
	if params.OfferLimit != nil {
		query.OfferLimit = *params.OfferLimit
	}
	if query.OfferLimit < 1 || query.OfferLimit > 100 {
		abortShootPlanningValidation(c)
		return
	}
	result, err := h.planShare.GetManagementProjection(c.Request.Context(), scope, id, query)
	if h.abortShareError(c, err) {
		return
	}
	c.JSON(http.StatusOK, toShareManagementAPI(result))
}

func (h *shootPlanningHandlers) IssueShootPlanShare(
	c *gin.Context,
	id shootplanningapi.Id,
	params shootplanningapi.IssueShootPlanShareParams,
) {
	scope, ok := h.scope(c)
	if !ok {
		return
	}
	if h.planShare == nil {
		_ = c.Error(errors.New("plan share route dependencies missing"))
		return
	}
	var body shootplanningapi.ShareIssueInputV1
	if err := decodeStrictRequest(c, &body,
		"expected_plan_revision", "view_level", "secret_commitment", "expires_at", "expiry_source", "policy_version",
	); err != nil || !body.ViewLevel.Valid() || !body.PolicyVersion.Valid() {
		abortShootPlanningValidation(c)
		return
	}
	commitment, err := planshare.DecodeSecretCommitment(body.SecretCommitment)
	if err != nil {
		abortShootPlanningValidation(c)
		return
	}
	source, err := decodeExpirySource(body.ExpirySource)
	if err != nil {
		abortShootPlanningValidation(c)
		return
	}
	result, err := h.planShare.Issue(c.Request.Context(), scope, params.IdempotencyKey, id, planshare.IssueInput{
		ExpectedPlanRevision: body.ExpectedPlanRevision,
		ViewLevel:            planshare.ViewLevel(body.ViewLevel),
		SecretCommitment:     commitment,
		ExpiresAt:            body.ExpiresAt,
		ExpirySource:         source,
		PolicyVersion:        string(body.PolicyVersion),
	})
	if h.abortShareError(c, err) {
		return
	}
	c.JSON(http.StatusCreated, toShareIssueAPI(result))
}

func (h *shootPlanningHandlers) RotateShootPlanShare(
	c *gin.Context,
	id shootplanningapi.Id,
	shareID string,
	params shootplanningapi.RotateShootPlanShareParams,
) {
	scope, ok := h.scope(c)
	if !ok {
		return
	}
	if h.planShare == nil {
		_ = c.Error(errors.New("plan share route dependencies missing"))
		return
	}
	var body shootplanningapi.ShareRotateInputV1
	if err := decodeStrictRequest(c, &body,
		"expected_share_revision", "new_secret_commitment", "expires_at", "expiry_source", "policy_version",
	); err != nil || !body.PolicyVersion.Valid() {
		abortShootPlanningValidation(c)
		return
	}
	commitment, err := planshare.DecodeSecretCommitment(body.NewSecretCommitment)
	if err != nil {
		abortShootPlanningValidation(c)
		return
	}
	source, err := decodeExpirySource(body.ExpirySource)
	if err != nil {
		abortShootPlanningValidation(c)
		return
	}
	result, err := h.planShare.Rotate(c.Request.Context(), scope, params.IdempotencyKey, id, shareID, planshare.RotateInput{
		ExpectedShareRevision: body.ExpectedShareRevision,
		NewSecretCommitment:   commitment,
		ExpiresAt:             body.ExpiresAt,
		ExpirySource:          source,
		PolicyVersion:         string(body.PolicyVersion),
	})
	if h.abortShareError(c, err) {
		return
	}
	c.JSON(http.StatusOK, toShareIssueAPI(result))
}

func (h *shootPlanningHandlers) RevokeShootPlanShare(
	c *gin.Context,
	id shootplanningapi.Id,
	shareID string,
	params shootplanningapi.RevokeShootPlanShareParams,
) {
	scope, ok := h.scope(c)
	if !ok {
		return
	}
	if h.planShare == nil {
		_ = c.Error(errors.New("plan share route dependencies missing"))
		return
	}
	var body shootplanningapi.ShareRevokeInputV1
	if err := decodeStrictRequest(c, &body, "expected_share_revision", "policy_version"); err != nil || !body.PolicyVersion.Valid() {
		abortShootPlanningValidation(c)
		return
	}
	result, err := h.planShare.Revoke(c.Request.Context(), scope, params.IdempotencyKey, id, shareID, planshare.RevokeInput{
		ExpectedShareRevision: body.ExpectedShareRevision,
		PolicyVersion:         string(body.PolicyVersion),
	})
	if h.abortShareError(c, err) {
		return
	}
	c.JSON(http.StatusOK, shootplanningapi.ShareRevokeResultV1{
		ShareId:   result.ShareID,
		State:     shootplanningapi.ShareRevokeResultV1State(result.State),
		Revision:  result.Revision,
		RevokedAt: result.RevokedAt,
	})
}

func (h *shootPlanningHandlers) GetShootPlanFeedback(
	c *gin.Context,
	id shootplanningapi.Id,
	params shootplanningapi.GetShootPlanFeedbackParams,
) {
	scope, ok := h.scope(c)
	if !ok {
		return
	}
	if h.planShare == nil {
		_ = c.Error(errors.New("plan share route dependencies missing"))
		return
	}
	if !onlyQueryParameters(c, "cursor", "limit") {
		abortShootPlanningValidation(c)
		return
	}
	query := planshare.FeedbackListQuery{Limit: 50}
	if params.Cursor != nil {
		query.Cursor = *params.Cursor
	}
	if params.Limit != nil {
		query.Limit = *params.Limit
	}
	if query.Limit < 1 || query.Limit > 100 {
		abortShootPlanningValidation(c)
		return
	}
	page, err := h.planShare.ListFeedback(c.Request.Context(), scope, id, query)
	if h.abortShareError(c, err) {
		return
	}
	c.JSON(http.StatusOK, toFeedbackPageAPI(page))
}

func (h *shootPlanningHandlers) SetShootPlanFeedbackDisposition(
	c *gin.Context,
	id shootplanningapi.Id,
	feedbackID string,
	params shootplanningapi.SetShootPlanFeedbackDispositionParams,
) {
	scope, ok := h.scope(c)
	if !ok {
		return
	}
	if h.planShare == nil {
		_ = c.Error(errors.New("plan share route dependencies missing"))
		return
	}
	var body shootplanningapi.FeedbackDispositionInputV1
	if err := decodeStrictRequest(c, &body, "expected_feedback_revision", "disposition"); err != nil ||
		(body.Disposition != shootplanningapi.FeedbackDispositionInputV1DispositionAdopted &&
			body.Disposition != shootplanningapi.FeedbackDispositionInputV1DispositionIgnored) {
		abortShootPlanningValidation(c)
		return
	}
	result, err := h.planShare.SetFeedbackDisposition(
		c.Request.Context(), scope, params.IdempotencyKey, id, feedbackID, planshare.DispositionInput{
			ExpectedFeedbackRevision: body.ExpectedFeedbackRevision,
			Disposition:              planshare.FeedbackDisposition(body.Disposition),
		},
	)
	if h.abortShareError(c, err) {
		return
	}
	c.JSON(http.StatusOK, toFeedbackDispositionAPI(result))
}

func (h *shootPlanningHandlers) GetShootPlanAssignments(
	c *gin.Context,
	id shootplanningapi.Id,
	params shootplanningapi.GetShootPlanAssignmentsParams,
) {
	scope, ok := h.scope(c)
	if !ok {
		return
	}
	if h.planShare == nil {
		_ = c.Error(errors.New("plan share route dependencies missing"))
		return
	}
	if !onlyQueryParameters(c, "cursor", "limit") {
		abortShootPlanningValidation(c)
		return
	}
	query := planshare.AssignmentListQuery{Limit: 50}
	if params.Cursor != nil {
		query.Cursor = *params.Cursor
	}
	if params.Limit != nil {
		query.Limit = *params.Limit
	}
	if query.Limit < 1 || query.Limit > 100 {
		abortShootPlanningValidation(c)
		return
	}
	page, err := h.planShare.ListAssignments(c.Request.Context(), scope, id, query)
	if h.abortShareError(c, err) {
		return
	}
	c.JSON(http.StatusOK, toAssignmentPageAPI(page))
}

func (h *shootPlanningHandlers) RevokeShootPlanAssignment(
	c *gin.Context,
	id shootplanningapi.Id,
	assignmentID string,
	params shootplanningapi.RevokeShootPlanAssignmentParams,
) {
	scope, ok := h.scope(c)
	if !ok {
		return
	}
	if h.planShare == nil {
		_ = c.Error(errors.New("plan share route dependencies missing"))
		return
	}
	var body shootplanningapi.AssignmentPhotographerRevokeInputV1
	if err := decodeStrictRequest(c, &body, "expected_assignment_revision"); err != nil {
		abortShootPlanningValidation(c)
		return
	}
	policy := ""
	if body.PolicyVersion != nil {
		policy = string(*body.PolicyVersion)
	}
	result, err := h.planShare.PhotographerRevokeAssignment(
		c.Request.Context(), scope, params.IdempotencyKey, id, assignmentID,
		planshare.PhotographerRevokeAssignmentInput{
			ExpectedAssignmentRevision: body.ExpectedAssignmentRevision,
			PolicyVersion:              policy,
		},
	)
	if h.abortShareError(c, err) {
		return
	}
	c.JSON(http.StatusOK, toAssignmentMutationAPI(result))
}

func (h *shootPlanningHandlers) CreateShootPlanAssignmentOffer(
	c *gin.Context,
	id shootplanningapi.Id,
	params shootplanningapi.CreateShootPlanAssignmentOfferParams,
) {
	scope, ok := h.scope(c)
	if !ok {
		return
	}
	if h.planShare == nil {
		_ = c.Error(errors.New("plan share route dependencies missing"))
		return
	}
	var body shootplanningapi.AssignmentOfferCreateInputV1
	if err := decodeStrictRequest(c, &body, "expected_plan_revision", "assignment_kind", "content"); err != nil ||
		body.AssignmentKind != shootplanningapi.AssignmentOfferCreateInputV1AssignmentKindOnSiteSupport {
		abortShootPlanningValidation(c)
		return
	}
	policy := ""
	if body.PolicyVersion != nil {
		policy = string(*body.PolicyVersion)
	}
	result, err := h.planShare.CreateOnSiteOffer(
		c.Request.Context(), scope, params.IdempotencyKey, id, planshare.CreateOfferInput{
			ExpectedPlanRevision: body.ExpectedPlanRevision,
			AssignmentKind:       planshare.AssignmentKindOnSiteSupport,
			Content:              body.Content,
			PolicyVersion:        policy,
		},
	)
	if h.abortShareError(c, err) {
		return
	}
	c.JSON(http.StatusCreated, toOfferMutationAPI(result))
}

func (h *shootPlanningHandlers) CloseShootPlanAssignmentOffer(
	c *gin.Context,
	id shootplanningapi.Id,
	offerID string,
	params shootplanningapi.CloseShootPlanAssignmentOfferParams,
) {
	scope, ok := h.scope(c)
	if !ok {
		return
	}
	if h.planShare == nil {
		_ = c.Error(errors.New("plan share route dependencies missing"))
		return
	}
	var body shootplanningapi.AssignmentOfferCloseInputV1
	if err := decodeStrictRequest(c, &body, "expected_offer_revision"); err != nil {
		abortShootPlanningValidation(c)
		return
	}
	policy := ""
	if body.PolicyVersion != nil {
		policy = string(*body.PolicyVersion)
	}
	result, err := h.planShare.CloseOnSiteOffer(
		c.Request.Context(), scope, params.IdempotencyKey, id, offerID, planshare.CloseOfferInput{
			ExpectedOfferRevision: body.ExpectedOfferRevision,
			PolicyVersion:         policy,
		},
	)
	if h.abortShareError(c, err) {
		return
	}
	c.JSON(http.StatusOK, toOfferMutationAPI(result))
}

func (h *shootPlanningHandlers) abortShareError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	var quoteExpired planshare.ExpiryQuoteExpiredError
	if errors.As(err, &quoteExpired) {
		details, detailsErr := expiryQuoteExpiredDetails(quoteExpired.Refreshed)
		if detailsErr != nil {
			_ = c.Error(detailsErr)
			return true
		}
		abortErrorWithTypedDetails(c, http.StatusConflict, CodeExpiryQuoteExpired,
			"默认过期报价已过期，请刷新后重新确认", details)
		return true
	}
	conflicts := []struct {
		err     error
		code    string
		message string
	}{
		{planshare.ErrShareGenerationExists, CodeShareGenerationExists, "该视角已有有效分享链接，请轮换"},
		{planshare.ErrFullViewNotEligible, CodeFullViewNotEligible, "当前 CRM 关联不满足完整分享资格"},
		{planshare.ErrExpiryQuoteStale, CodeExpiryQuoteStale, "默认过期报价已失效，请刷新后重新确认"},
		{planshare.ErrShareStale, CodeShareStale, "分享版本已变化，请刷新后重试"},
		{planshare.ErrShareInvalidState, CodeShareInvalidState, "分享状态不允许此操作"},
		{planshare.ErrPlanRevisionConflict, CodePlanRevisionConflict, "策划版本已变化，请刷新后重试"},
		{idempotency.ErrConflict, CodeIdempotencyConflict, "幂等键已被其他请求使用"},
		{planshare.ErrFeedbackStale, "feedback_stale", "反馈版本已变化，请刷新后重试"},
		{planshare.ErrShotRevisionConflict, "shot_revision_conflict", "镜头版本已变化，请刷新后重试"},
		{planshare.ErrAssignmentAlreadyClaimed, "assignment_already_claimed", "该目标已被认领"},
		{planshare.ErrAssignmentActive, "assignment_active", "仍有有效认领，请先撤销"},
		{planshare.ErrAssignmentStale, "assignment_stale", "认领版本已变化，请刷新后重试"},
		{planshare.ErrOfferStale, "offer_stale", "邀约版本已变化，请刷新后重试"},
	}
	for _, item := range conflicts {
		if errors.Is(err, item.err) {
			abortError(c, http.StatusConflict, item.code, item.message)
			return true
		}
	}
	if errors.Is(err, planshare.ErrNotFound) ||
		errors.Is(err, planshare.ErrFeedbackNotFound) ||
		errors.Is(err, planshare.ErrAssignmentNotFound) ||
		errors.Is(err, planshare.ErrOfferNotFound) {
		abortError(c, http.StatusNotFound, CodeNotFound, "资源不存在")
		return true
	}
	if errors.Is(err, planshare.ErrValidation) || errors.Is(err, planshare.ErrExpiryOutOfRange) ||
		errors.Is(err, idempotency.ErrValidation) {
		abortShootPlanningValidation(c)
		return true
	}
	return h.abortError(c, err)
}

func decodeExpirySource(source shootplanningapi.ExpirySourceV1) (planshare.ExpirySourceV1, error) {
	if explicit, err := source.AsExpirySourceExplicitV1(); err == nil && explicit.Kind == shootplanningapi.Explicit {
		return planshare.ExpirySourceV1{Kind: planshare.ExpirySourceExplicit}, nil
	}
	quoted, err := source.AsExpirySourceQuotedDefaultV1()
	if err != nil || quoted.Kind != shootplanningapi.QuotedDefault {
		return planshare.ExpirySourceV1{}, validationFailed()
	}
	quote := planshare.DefaultQuoteV1{
		EvaluatedAt:   quoted.DefaultQuote.EvaluatedAt.UTC(),
		ValidUntil:    quoted.DefaultQuote.ValidUntil.UTC(),
		ViewLevel:     planshare.ViewLevel(quoted.DefaultQuote.ViewLevel),
		PolicyVersion: string(quoted.DefaultQuote.PolicyVersion),
	}
	if quoted.DefaultQuote.ExecutionWindowRevision.IsSpecified() && !quoted.DefaultQuote.ExecutionWindowRevision.IsNull() {
		value, err := quoted.DefaultQuote.ExecutionWindowRevision.Get()
		if err != nil {
			return planshare.ExpirySourceV1{}, err
		}
		quote.ExecutionWindowRevision = &value
	}
	return planshare.ExpirySourceV1{Kind: planshare.ExpirySourceQuotedDefault, DefaultQuote: &quote}, nil
}

func validationFailed() error {
	return planshare.ValidationError{Message: "expiry_source invalid"}
}

func toShareIssueAPI(result planshare.ShareIssueResultV1) shootplanningapi.ShareIssueResultV1 {
	return shootplanningapi.ShareIssueResultV1{
		ShareId:    result.ShareID,
		Selector:   result.Selector,
		Generation: result.Generation,
		ViewLevel:  shootplanningapi.ShareViewLevel(result.ViewLevel),
		State:      shootplanningapi.ShareIssueResultV1State(result.State),
		ExpiresAt:  result.ExpiresAt,
		Revision:   result.Revision,
	}
}

func toShareManagementAPI(result planshare.ShareManagementProjectionV1) shootplanningapi.ShareManagementProjectionV1 {
	out := shootplanningapi.ShareManagementProjectionV1{
		ShareViews:   make([]shootplanningapi.ShareViewProjectionV1, 0, len(result.ShareViews)),
		OnSiteOffers: make([]shootplanningapi.OnSiteOfferProjectionV1, 0, len(result.OnSiteOffers)),
	}
	if result.OffersNextCursor != nil {
		out.OffersNextCursor = nullable.NewNullableWithValue(*result.OffersNextCursor)
	}
	for _, view := range result.ShareViews {
		item := shootplanningapi.ShareViewProjectionV1{
			ViewLevel:    shootplanningapi.ShareViewLevel(view.ViewLevel),
			ExpiryPolicy: toExpiryPolicyAPI(view.ExpiryPolicy),
		}
		if view.LatestGeneration != nil {
			latest := toLatestGenerationAPI(*view.LatestGeneration)
			item.LatestGeneration = nullable.NewNullableWithValue(latest)
		}
		out.ShareViews = append(out.ShareViews, item)
	}
	for _, offer := range result.OnSiteOffers {
		item := shootplanningapi.OnSiteOfferProjectionV1{
			OfferId:        offer.OfferID,
			AssignmentKind: shootplanningapi.OnSiteOfferProjectionV1AssignmentKind(offer.AssignmentKind),
			Content:        offer.Content,
			State:          shootplanningapi.OnSiteOfferProjectionV1State(offer.State),
			Revision:       offer.Revision,
			CreatedAt:      offer.CreatedAt,
		}
		if offer.ClosedAt != nil {
			item.ClosedAt = nullable.NewNullableWithValue(*offer.ClosedAt)
		}
		if offer.ActiveAssignmentID != nil {
			item.ActiveAssignmentId = nullable.NewNullableWithValue(*offer.ActiveAssignmentID)
		}
		out.OnSiteOffers = append(out.OnSiteOffers, item)
	}
	return out
}

func toLatestGenerationAPI(latest planshare.LatestGenerationProjectionV1) shootplanningapi.LatestShareGenerationProjectionV1 {
	out := shootplanningapi.LatestShareGenerationProjectionV1{
		ShareId:        latest.ShareID,
		Generation:     latest.Generation,
		Fingerprint:    latest.Fingerprint,
		EffectiveState: shootplanningapi.LatestShareGenerationProjectionV1EffectiveState(latest.EffectiveState),
		IssuedAt:       latest.IssuedAt,
		ExpiresAt:      latest.ExpiresAt,
		Revision:       latest.Revision,
	}
	if latest.EndedReason != nil {
		out.EndedReason = nullable.NewNullableWithValue(*latest.EndedReason)
	}
	if latest.EndedAt != nil {
		out.EndedAt = nullable.NewNullableWithValue(*latest.EndedAt)
	}
	if latest.FirstOpenedAt != nil {
		out.FirstOpenedAt = nullable.NewNullableWithValue(*latest.FirstOpenedAt)
	}
	return out
}

func toExpiryPolicyAPI(policy planshare.ExpiryPolicyProjectionV1) shootplanningapi.ExpiryPolicyProjectionV1 {
	quote := shootplanningapi.DefaultQuoteV1{
		EvaluatedAt:   policy.DefaultQuote.EvaluatedAt,
		ValidUntil:    policy.DefaultQuote.ValidUntil,
		ViewLevel:     shootplanningapi.ShareViewLevel(policy.DefaultQuote.ViewLevel),
		PolicyVersion: shootplanningapi.DefaultQuoteV1PolicyVersion(policy.DefaultQuote.PolicyVersion),
	}
	if policy.DefaultQuote.ExecutionWindowRevision != nil {
		quote.ExecutionWindowRevision = nullable.NewNullableWithValue(*policy.DefaultQuote.ExecutionWindowRevision)
	}
	return shootplanningapi.ExpiryPolicyProjectionV1{
		ViewLevel:                shootplanningapi.ShareViewLevel(policy.ViewLevel),
		MinExpiresAt:             policy.MinExpiresAt,
		MaxExpiresAt:             policy.MaxExpiresAt,
		ResolvedDefaultExpiresAt: policy.ResolvedDefaultExpiresAt,
		PolicyVersion:            shootplanningapi.ExpiryPolicyProjectionV1PolicyVersion(policy.PolicyVersion),
		DefaultQuote:             quote,
	}
}

func expiryQuoteExpiredDetails(policy planshare.ExpiryPolicyProjectionV1) (ErrorDetails, error) {
	details := ExpiryQuoteExpiredDetails{
		RefreshedExpiryPolicy: ExpiryPolicyProjectionV1{
			ViewLevel:                ShareViewLevel(policy.ViewLevel),
			MinExpiresAt:             policy.MinExpiresAt,
			MaxExpiresAt:             policy.MaxExpiresAt,
			ResolvedDefaultExpiresAt: policy.ResolvedDefaultExpiresAt,
			PolicyVersion:            ExpiryPolicyProjectionV1PolicyVersion(policy.PolicyVersion),
			DefaultQuote: DefaultQuoteV1{
				EvaluatedAt:   policy.DefaultQuote.EvaluatedAt,
				ValidUntil:    policy.DefaultQuote.ValidUntil,
				ViewLevel:     ShareViewLevel(policy.DefaultQuote.ViewLevel),
				PolicyVersion: DefaultQuoteV1PolicyVersion(policy.DefaultQuote.PolicyVersion),
			},
		},
	}
	if policy.DefaultQuote.ExecutionWindowRevision != nil {
		details.RefreshedExpiryPolicy.DefaultQuote.ExecutionWindowRevision =
			nullable.NewNullableWithValue(*policy.DefaultQuote.ExecutionWindowRevision)
	}
	var union ErrorDetails
	if err := union.FromExpiryQuoteExpiredDetails(details); err != nil {
		return ErrorDetails{}, err
	}
	return union, nil
}

func toFeedbackPageAPI(page planshare.FeedbackManagementPageV1) shootplanningapi.FeedbackManagementPageV1 {
	out := shootplanningapi.FeedbackManagementPageV1{
		Items: make([]shootplanningapi.FeedbackManagementItemV1, 0, len(page.Items)),
	}
	if page.NextCursor != nil {
		out.NextCursor = nullable.NewNullableWithValue(*page.NextCursor)
	}
	for _, item := range page.Items {
		apiItem := shootplanningapi.FeedbackManagementItemV1{
			FeedbackId:        item.FeedbackID,
			AuthorDisplayName: item.AuthorDisplayName,
			Content:           item.Content,
			Disposition:       shootplanningapi.FeedbackManagementItemV1Disposition(item.Disposition),
			Revision:          item.Revision,
			CreatedAt:         item.CreatedAt,
			Target:            toFeedbackTargetAPI(item.Target),
			DeepLinkTarget:    toFeedbackDeepLinkAPI(item.DeepLinkTarget),
		}
		if item.DispositionAt != nil {
			apiItem.DispositionAt = nullable.NewNullableWithValue(*item.DispositionAt)
		}
		out.Items = append(out.Items, apiItem)
	}
	return out
}

func toFeedbackDispositionAPI(result planshare.FeedbackDispositionResultV1) shootplanningapi.FeedbackDispositionResultV1 {
	return shootplanningapi.FeedbackDispositionResultV1{
		FeedbackId:     result.FeedbackID,
		Disposition:    shootplanningapi.FeedbackDispositionResultV1Disposition(result.Disposition),
		Revision:       result.Revision,
		DeepLinkTarget: toFeedbackDeepLinkAPI(result.DeepLinkTarget),
	}
}

func toFeedbackTargetAPI(target planshare.FeedbackTargetV1) shootplanningapi.FeedbackTargetV1 {
	var out shootplanningapi.FeedbackTargetV1
	switch target.Kind {
	case planshare.FeedbackTargetPlan:
		_ = out.FromFeedbackTargetV10(shootplanningapi.FeedbackTargetV10{
			Kind: shootplanningapi.Plan,
		})
	case planshare.FeedbackTargetShot:
		shotID := ""
		if target.ShotID != nil {
			shotID = *target.ShotID
		}
		_ = out.FromFeedbackTargetV11(shootplanningapi.FeedbackTargetV11{
			Kind:   shootplanningapi.FeedbackTargetV11KindShot,
			ShotId: shotID,
		})
	}
	return out
}

func toFeedbackDeepLinkAPI(link planshare.FeedbackDeepLinkTargetV1) shootplanningapi.FeedbackDeepLinkTargetV1 {
	var out shootplanningapi.FeedbackDeepLinkTargetV1
	switch link.Kind {
	case planshare.DeepLinkFeedbackSection:
		_ = out.FromFeedbackDeepLinkTargetV10(shootplanningapi.FeedbackDeepLinkTargetV10{
			Kind: shootplanningapi.FeedbackSection,
		})
	case planshare.DeepLinkShot:
		shotID := ""
		if link.ShotID != nil {
			shotID = *link.ShotID
		}
		_ = out.FromFeedbackDeepLinkTargetV11(shootplanningapi.FeedbackDeepLinkTargetV11{
			Kind:   shootplanningapi.FeedbackDeepLinkTargetV11KindShot,
			ShotId: shotID,
		})
	}
	return out
}

func toAssignmentPageAPI(page planshare.AssignmentManagementPageV1) shootplanningapi.AssignmentManagementPageV1 {
	out := shootplanningapi.AssignmentManagementPageV1{
		Items: make([]shootplanningapi.AssignmentManagementItemV1, 0, len(page.Items)),
	}
	if page.NextCursor != nil {
		out.NextCursor = nullable.NewNullableWithValue(*page.NextCursor)
	}
	for _, item := range page.Items {
		apiItem := shootplanningapi.AssignmentManagementItemV1{
			AssignmentId:         item.AssignmentID,
			AssignmentKind:       shootplanningapi.AssignmentManagementItemV1AssignmentKind(item.AssignmentKind),
			ContentSnapshot:      item.ContentSnapshot,
			ClaimedByDisplayName: item.ClaimedByDisplayName,
			Status:               shootplanningapi.AssignmentManagementItemV1Status(item.Status),
			Revision:             item.Revision,
			ClaimedAt:            item.ClaimedAt,
			Target: shootplanningapi.AssignmentTargetV1{
				Kind:            shootplanningapi.AssignmentTargetV1Kind(item.Target.Kind),
				ReadinessItemId: item.Target.ReadinessItemID,
				OfferId:         item.Target.OfferID,
			},
			DeepLinkTarget: shootplanningapi.AssignmentDeepLinkTargetV1{
				Kind:            shootplanningapi.AssignmentDeepLinkTargetV1Kind(item.DeepLinkTarget.Kind),
				ReadinessItemId: item.DeepLinkTarget.ReadinessItemID,
				OfferId:         item.DeepLinkTarget.OfferID,
			},
		}
		if item.PreparationLeadDaysSnapshot != nil {
			apiItem.PreparationLeadDaysSnapshot = nullable.NewNullableWithValue(*item.PreparationLeadDaysSnapshot)
		}
		if item.LeadRuleVersion != nil {
			apiItem.LeadRuleVersion = nullable.NewNullableWithValue(*item.LeadRuleVersion)
		}
		if item.RevokedAt != nil {
			apiItem.RevokedAt = nullable.NewNullableWithValue(*item.RevokedAt)
		}
		if item.RevokedBy != nil {
			apiItem.RevokedBy = nullable.NewNullableWithValue(
				shootplanningapi.AssignmentManagementItemV1RevokedBy(*item.RevokedBy),
			)
		}
		out.Items = append(out.Items, apiItem)
	}
	return out
}

func toAssignmentMutationAPI(result planshare.AssignmentMutationResultV1) shootplanningapi.AssignmentMutationResultV1 {
	return shootplanningapi.AssignmentMutationResultV1{
		AssignmentId: result.AssignmentID,
		Status:       shootplanningapi.AssignmentMutationResultV1Status(result.Status),
		Revision:     result.Revision,
		RevokedAt:    result.RevokedAt,
	}
}

func toOfferMutationAPI(result planshare.OfferMutationResultV1) shootplanningapi.OfferMutationResultV1 {
	return shootplanningapi.OfferMutationResultV1{
		OfferId:  result.OfferID,
		State:    shootplanningapi.OfferMutationResultV1State(result.State),
		Revision: result.Revision,
	}
}
