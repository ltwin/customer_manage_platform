package httpapi

import (
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/oapi-codegen/nullable"

	"github.com/samson/customer-manage-platform/backend/internal/planningmedia"
	"github.com/samson/customer-manage-platform/backend/internal/planshare"
	"github.com/samson/customer-manage-platform/backend/internal/platform/idempotency"
	"github.com/samson/customer-manage-platform/backend/internal/platform/securitybudget"
	"github.com/samson/customer-manage-platform/backend/internal/platform/txcap"
)

const anonymousShareCSP = "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'"

// AnonymousShareDeps is the sealed dependency surface for anonymous share
// routes. It must never carry ScopeFactory / AccountScope.
type AnonymousShareDeps struct {
	App            *planshare.Application
	Resolver       planshare.ShareTokenResolver
	Runner         txcap.TransactionRunner[planshare.ShareTxScope]
	PlanningMedia  *planningmedia.Application
	ReadBudget     planshare.AnonymousReadBudget
	IPDigest       planshare.ClientIPDigestResolver
	MutationBudget planshare.AnonymousMutationBudget
	MutationDigest planshare.MutationIPDigestResolver
	PublicBaseURL  string
	Now            func() time.Time
}

type anonymousShareRouteDeps = AnonymousShareDeps

func setAnonymousShareHeaders(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	c.Header("Referrer-Policy", "no-referrer")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Security-Policy", anonymousShareCSP)
}

func (h *handlers) GetSharedPlan(c *gin.Context, token string) {
	setAnonymousShareHeaders(c)
	deps := h.anonymousShare
	if deps.App == nil || deps.Resolver == nil || deps.Runner == nil {
		_ = c.Error(errors.New("anonymous share route dependencies missing"))
		return
	}
	now := time.Now().UTC()
	if deps.Now != nil {
		now = deps.Now().UTC()
	}

	validated, _, err := deps.Resolver.Resolve(c.Request.Context(), token)
	if err != nil {
		abortError(c, http.StatusNotFound, CodeNotFound, "资源不存在")
		return
	}
	meta := clientMeta(c, h.trustedProxyCIDRs)
	sourceIP := ""
	if meta.SourceIP.IsValid() {
		sourceIP = meta.SourceIP.String()
	}
	if deps.ReadBudget != nil || deps.IPDigest != nil {
		if deps.ReadBudget == nil || deps.IPDigest == nil || sourceIP == "" {
			abortError(c, http.StatusServiceUnavailable, "service_unavailable", "服务暂时不可用")
			return
		}
		retryAfter, budgetErr := planshare.ConsumeAnonymousReadOuter(
			c.Request.Context(),
			deps.ReadBudget,
			deps.IPDigest,
			sourceIP,
			validated.SelectorFingerprint(),
			now,
		)
		if errors.Is(budgetErr, planshare.ErrAnonymousReadRateLimited) {
			seconds := int64(retryAfter.Seconds())
			if seconds < 1 {
				seconds = 1
			}
			c.Header("Retry-After", strconv.FormatInt(seconds, 10))
			abortError(c, http.StatusTooManyRequests, CodeRateLimited, "请求过于频繁，请稍后重试")
			return
		}
		if budgetErr != nil {
			if errors.Is(budgetErr, securitybudget.ErrUnavailable) ||
				errors.Is(budgetErr, securitybudget.ErrInvalidCandidates) {
				abortError(c, http.StatusServiceUnavailable, "service_unavailable", "服务暂时不可用")
				return
			}
			_ = c.Error(budgetErr)
			return
		}
	}

	result, err := deps.App.GetAnonymousProjection(c.Request.Context(), planshare.AnonymousProjectionDeps{
		Resolver: deps.Resolver,
		Runner:   deps.Runner,
	}, token)
	if err != nil {
		if errors.Is(err, planshare.ErrShareNotFound) || errors.Is(err, planshare.ErrNotFound) {
			abortError(c, http.StatusNotFound, CodeNotFound, "资源不存在")
			return
		}
		_ = c.Error(err)
		return
	}
	if result.Full != nil {
		c.JSON(http.StatusOK, result.Full)
		return
	}
	if result.Proposal != nil {
		c.JSON(http.StatusOK, result.Proposal)
		return
	}
	abortError(c, http.StatusNotFound, CodeNotFound, "资源不存在")
}

func (h *handlers) GetSharedPlanAssetContent(c *gin.Context, token string, ref string, params GetSharedPlanAssetContentParams) {
	setAnonymousShareHeaders(c)
	deps := h.anonymousShare
	if deps.App == nil || deps.Resolver == nil || deps.Runner == nil || deps.PlanningMedia == nil {
		_ = c.Error(errors.New("anonymous share content dependencies missing"))
		return
	}
	now := time.Now().UTC()
	if deps.Now != nil {
		now = deps.Now().UTC()
	}

	validated, _, err := deps.Resolver.Resolve(c.Request.Context(), token)
	if err != nil {
		abortError(c, http.StatusNotFound, CodeNotFound, "资源不存在")
		return
	}
	meta := clientMeta(c, h.trustedProxyCIDRs)
	sourceIP := ""
	if meta.SourceIP.IsValid() {
		sourceIP = meta.SourceIP.String()
	}
	if deps.ReadBudget != nil || deps.IPDigest != nil {
		if deps.ReadBudget == nil || deps.IPDigest == nil || sourceIP == "" {
			abortError(c, http.StatusServiceUnavailable, "service_unavailable", "服务暂时不可用")
			return
		}
		retryAfter, budgetErr := planshare.ConsumeAnonymousReadOuter(
			c.Request.Context(),
			deps.ReadBudget,
			deps.IPDigest,
			sourceIP,
			validated.SelectorFingerprint(),
			now,
		)
		if errors.Is(budgetErr, planshare.ErrAnonymousReadRateLimited) {
			seconds := int64(retryAfter.Seconds())
			if seconds < 1 {
				seconds = 1
			}
			c.Header("Retry-After", strconv.FormatInt(seconds, 10))
			abortError(c, http.StatusTooManyRequests, CodeRateLimited, "请求过于频繁，请稍后重试")
			return
		}
		if budgetErr != nil {
			if errors.Is(budgetErr, securitybudget.ErrUnavailable) ||
				errors.Is(budgetErr, securitybudget.ErrInvalidCandidates) {
				abortError(c, http.StatusServiceUnavailable, "service_unavailable", "服务暂时不可用")
				return
			}
			_ = c.Error(budgetErr)
			return
		}
	}

	checksum := params.V
	permit, err := deps.App.IssueAnonymousContentPermit(
		c.Request.Context(),
		planshare.AnonymousContentDeps{
			Resolver: deps.Resolver,
			Runner:   deps.Runner,
			Media:    deps.PlanningMedia,
		},
		token,
		ref,
		checksum,
	)
	if err != nil {
		if errors.Is(err, planningmedia.ErrAssetCorrupt) {
			abortError(c, http.StatusServiceUnavailable, "service_unavailable", "服务暂时不可用")
			return
		}
		if errors.Is(err, planshare.ErrShareNotFound) || errors.Is(err, planshare.ErrNotFound) {
			abortError(c, http.StatusNotFound, CodeNotFound, "资源不存在")
			return
		}
		_ = c.Error(err)
		return
	}
	stream, err := deps.PlanningMedia.OpenDisplayWithPermit(c.Request.Context(), permit)
	if err != nil {
		if errors.Is(err, planningmedia.ErrAssetCorrupt) {
			abortError(c, http.StatusServiceUnavailable, "service_unavailable", "服务暂时不可用")
			return
		}
		if errors.Is(err, planningmedia.ErrNotFound) ||
			errors.Is(err, planningmedia.ErrAssetGCPending) ||
			errors.Is(err, planningmedia.ErrAssetReferenceStale) {
			abortError(c, http.StatusNotFound, CodeNotFound, "资源不存在")
			return
		}
		_ = c.Error(err)
		return
	}
	defer func() { _ = stream.Close() }()

	c.Header("Content-Type", stream.MediaType)
	c.Header("Content-Length", strconv.FormatInt(stream.Size, 10))
	c.Header("ETag", strconv.Quote(stream.Checksum))
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Disposition", "inline")
	c.Status(http.StatusOK)
	_, _ = io.Copy(c.Writer, stream.Reader)
}

func (h *handlers) CreateSharedPlanFeedback(c *gin.Context, token string, params CreateSharedPlanFeedbackParams) {
	h.createSharedFeedback(c, token, params.IdempotencyKey, "", true)
}

func (h *handlers) CreateSharedShotFeedback(c *gin.Context, token string, shotRef string, params CreateSharedShotFeedbackParams) {
	h.createSharedFeedback(c, token, params.IdempotencyKey, shotRef, false)
}

func (h *handlers) ClaimSharedAssignment(c *gin.Context, token string, params ClaimSharedAssignmentParams) {
	setAnonymousShareHeaders(c)
	deps := h.anonymousShare
	if deps.App == nil || deps.Resolver == nil || deps.Runner == nil {
		_ = c.Error(errors.New("anonymous share assignment dependencies missing"))
		return
	}
	if deps.PublicBaseURL == "" || c.GetHeader("Origin") != deps.PublicBaseURL {
		abortError(c, http.StatusForbidden, "origin_forbidden", "来源不被允许")
		return
	}
	meta := clientMeta(c, h.trustedProxyCIDRs)
	sourceIP := ""
	if meta.SourceIP.IsValid() {
		sourceIP = meta.SourceIP.String()
	}
	var body SharedAssignmentClaimInputV1
	if err := decodeStrictRequest(c, &body,
		"target", "expected_target_revision", "claim_receipt_commitment",
	); err != nil {
		abortError(c, http.StatusBadRequest, CodeValidationFailed, "请求参数不合法")
		return
	}
	commitment, err := decodeReceiptCommitment(body.ClaimReceiptCommitment)
	if err != nil {
		abortError(c, http.StatusBadRequest, CodeValidationFailed, "请求参数不合法")
		return
	}
	author := ""
	if body.ClaimedByDisplayName != nil {
		author = *body.ClaimedByDisplayName
	}
	policy := ""
	if body.PolicyVersion != nil {
		policy = string(*body.PolicyVersion)
	}
	input := planshare.ClaimAssignmentInput{
		Target: planshare.ClaimAssignmentTarget{
			Kind:            planshare.AssignmentTargetKind(body.Target.Kind),
			ReadinessItemID: body.Target.ReadinessItemId,
			OfferID:         body.Target.OfferId,
		},
		ExpectedTargetRevision: body.ExpectedTargetRevision,
		ClaimedByDisplayName:   author,
		ClaimReceiptCommitment: commitment,
		PolicyVersion:          policy,
	}
	result, err := deps.App.ClaimAnonymousAssignment(
		c.Request.Context(),
		planshare.AnonymousAssignmentDeps{
			Resolver:       deps.Resolver,
			Runner:         deps.Runner,
			MutationBudget: deps.MutationBudget,
			MutationDigest: deps.MutationDigest,
			SourceIP:       sourceIP,
		},
		params.IdempotencyKey,
		token,
		input,
	)
	if h.abortAnonymousFeedbackError(c, err) {
		return
	}
	c.JSON(http.StatusCreated, AssignmentClaimResultV1{
		AssignmentId:   result.AssignmentID,
		AssignmentKind: AssignmentClaimResultV1AssignmentKind(result.AssignmentKind),
		TargetRef:      result.TargetRef,
		Status:         AssignmentClaimResultV1Status(result.Status),
		Revision:       result.Revision,
		ClaimedAt:      result.ClaimedAt,
	})
}

func (h *handlers) SelfRevokeSharedAssignment(
	c *gin.Context,
	token string,
	assignmentRef string,
	params SelfRevokeSharedAssignmentParams,
) {
	setAnonymousShareHeaders(c)
	deps := h.anonymousShare
	if deps.App == nil || deps.Resolver == nil || deps.Runner == nil {
		_ = c.Error(errors.New("anonymous share assignment dependencies missing"))
		return
	}
	if deps.PublicBaseURL == "" || c.GetHeader("Origin") != deps.PublicBaseURL {
		abortError(c, http.StatusForbidden, "origin_forbidden", "来源不被允许")
		return
	}
	meta := clientMeta(c, h.trustedProxyCIDRs)
	sourceIP := ""
	if meta.SourceIP.IsValid() {
		sourceIP = meta.SourceIP.String()
	}
	var body SharedAssignmentSelfRevokeInputV1
	if err := decodeStrictRequest(c, &body, "expected_assignment_revision", "claim_receipt"); err != nil {
		abortError(c, http.StatusBadRequest, CodeValidationFailed, "请求参数不合法")
		return
	}
	policy := ""
	if body.PolicyVersion != nil {
		policy = string(*body.PolicyVersion)
	}
	result, err := deps.App.SelfRevokeAnonymousAssignment(
		c.Request.Context(),
		planshare.AnonymousAssignmentDeps{
			Resolver:       deps.Resolver,
			Runner:         deps.Runner,
			MutationBudget: deps.MutationBudget,
			MutationDigest: deps.MutationDigest,
			SourceIP:       sourceIP,
		},
		params.IdempotencyKey,
		token,
		assignmentRef,
		body.ClaimReceipt,
		planshare.SelfRevokeAssignmentInput{
			ExpectedAssignmentRevision: body.ExpectedAssignmentRevision,
			PolicyVersion:              policy,
		},
	)
	body.ClaimReceipt = ""
	if h.abortAnonymousFeedbackError(c, err) {
		return
	}
	c.JSON(http.StatusOK, AssignmentMutationResultV1{
		AssignmentId: result.AssignmentID,
		Status:       AssignmentMutationResultV1Status(result.Status),
		Revision:     result.Revision,
		RevokedAt:    result.RevokedAt,
	})
}

func decodeReceiptCommitment(wire string) (planshare.ClaimReceiptCommitment, error) {
	raw, err := base64.RawURLEncoding.DecodeString(wire)
	if err != nil || len(raw) != 32 {
		return planshare.ClaimReceiptCommitment{}, errors.New("invalid receipt commitment")
	}
	var out planshare.ClaimReceiptCommitment
	copy(out[:], raw)
	return out, nil
}

func (h *handlers) createSharedFeedback(c *gin.Context, token, idempotencyKey, shotRef string, planTarget bool) {
	setAnonymousShareHeaders(c)
	deps := h.anonymousShare
	if deps.App == nil || deps.Resolver == nil || deps.Runner == nil {
		_ = c.Error(errors.New("anonymous share feedback dependencies missing"))
		return
	}
	if deps.PublicBaseURL == "" || c.GetHeader("Origin") != deps.PublicBaseURL {
		abortError(c, http.StatusForbidden, "origin_forbidden", "来源不被允许")
		return
	}
	now := time.Now().UTC()
	if deps.Now != nil {
		now = deps.Now().UTC()
	}
	_ = now
	meta := clientMeta(c, h.trustedProxyCIDRs)
	sourceIP := ""
	if meta.SourceIP.IsValid() {
		sourceIP = meta.SourceIP.String()
	}
	feedbackDeps := planshare.AnonymousFeedbackDeps{
		Resolver:       deps.Resolver,
		Runner:         deps.Runner,
		MutationBudget: deps.MutationBudget,
		MutationDigest: deps.MutationDigest,
		SourceIP:       sourceIP,
	}

	var result planshare.FeedbackCreateResultV1
	var err error
	if planTarget {
		var body SharedPlanFeedbackCreateInputV1
		if decodeErr := decodeStrictRequest(c, &body,
			"expected_projection_revision", "content", "policy_version",
		); decodeErr != nil || !body.PolicyVersion.Valid() {
			abortError(c, http.StatusBadRequest, CodeValidationFailed, "请求参数不合法")
			return
		}
		author := ""
		if body.AuthorDisplayName != nil {
			author = *body.AuthorDisplayName
		}
		result, err = deps.App.CreateAnonymousPlanFeedback(c.Request.Context(), feedbackDeps, idempotencyKey, token, planshare.CreatePlanFeedbackInput{
			ExpectedProjectionRevision: body.ExpectedProjectionRevision,
			AuthorDisplayName:          author,
			Content:                    body.Content,
			PolicyVersion:              string(body.PolicyVersion),
		})
	} else {
		var body SharedShotFeedbackCreateInputV1
		if decodeErr := decodeStrictRequest(c, &body,
			"expected_shot_revision", "content", "policy_version",
		); decodeErr != nil || !body.PolicyVersion.Valid() {
			abortError(c, http.StatusBadRequest, CodeValidationFailed, "请求参数不合法")
			return
		}
		author := ""
		if body.AuthorDisplayName != nil {
			author = *body.AuthorDisplayName
		}
		result, err = deps.App.CreateAnonymousShotFeedback(c.Request.Context(), feedbackDeps, idempotencyKey, token, shotRef, planshare.CreateShotFeedbackInput{
			ExpectedShotRevision: body.ExpectedShotRevision,
			AuthorDisplayName:    author,
			Content:              body.Content,
			PolicyVersion:        string(body.PolicyVersion),
		})
	}
	if h.abortAnonymousFeedbackError(c, err) {
		return
	}
	out := FeedbackCreateResultV1{
		FeedbackId: result.FeedbackID,
		TargetKind: FeedbackCreateResultV1TargetKind(result.TargetKind),
		Revision:   result.Revision,
		CreatedAt:  result.CreatedAt,
	}
	if result.TargetRef != nil {
		out.TargetRef = nullable.NewNullableWithValue(*result.TargetRef)
	}
	c.JSON(http.StatusCreated, out)
}

func (h *handlers) abortAnonymousFeedbackError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, planshare.ErrAnonymousMutationRateLimited) {
		seconds := int64(1)
		var limited planshare.MutationRateLimitedError
		if errors.As(err, &limited) {
			seconds = int64(limited.RetryAfter.Seconds())
			if seconds < 1 {
				seconds = 1
			}
		}
		c.Header("Retry-After", strconv.FormatInt(seconds, 10))
		abortError(c, http.StatusTooManyRequests, CodeRateLimited, "请求过于频繁，请稍后重试")
		return true
	}
	if errors.Is(err, securitybudget.ErrUnavailable) || errors.Is(err, securitybudget.ErrInvalidCandidates) {
		abortError(c, http.StatusServiceUnavailable, "service_unavailable", "服务暂时不可用")
		return true
	}
	if errors.Is(err, planshare.ErrShareNotFound) || errors.Is(err, planshare.ErrNotFound) {
		abortError(c, http.StatusNotFound, CodeNotFound, "资源不存在")
		return true
	}
	if errors.Is(err, planshare.ErrPlanRevisionConflict) {
		abortError(c, http.StatusConflict, CodePlanRevisionConflict, "策划版本已变化，请刷新后重试")
		return true
	}
	if errors.Is(err, planshare.ErrShotRevisionConflict) {
		abortError(c, http.StatusConflict, "shot_revision_conflict", "镜头版本已变化，请刷新后重试")
		return true
	}
	if errors.Is(err, planshare.ErrAssignmentAlreadyClaimed) {
		abortError(c, http.StatusConflict, "assignment_already_claimed", "该目标已被认领")
		return true
	}
	if errors.Is(err, planshare.ErrAssignmentStale) {
		abortError(c, http.StatusConflict, "assignment_stale", "认领版本已变化，请刷新后重试")
		return true
	}
	if errors.Is(err, idempotency.ErrConflict) {
		abortError(c, http.StatusConflict, CodeIdempotencyConflict, "幂等键已被其他请求使用")
		return true
	}
	if errors.Is(err, planshare.ErrValidation) || errors.Is(err, idempotency.ErrValidation) {
		abortError(c, http.StatusBadRequest, CodeValidationFailed, "请求参数不合法")
		return true
	}
	_ = c.Error(err)
	return true
}
