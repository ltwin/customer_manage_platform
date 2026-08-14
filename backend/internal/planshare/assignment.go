package planshare

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/platform/idempotency"
	"github.com/samson/customer-manage-platform/backend/internal/platform/securitybudget"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/platform/txcap"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning/planningreminder"
)

const (
	offerContentMinRunes        = 1
	offerContentMaxRunes        = 240
	platformDefaultLeadDays     = 3
	leadRuleReadinessItemV1     = "readiness-item-v1"
	leadRulePlatformDefaultV1   = "platform-default-v1"
	assignmentObservationClaim  = "assignment_claim"
	assignmentObservationRevoke = "assignment_revoke"
)

type AssignmentListQuery struct {
	Cursor string
	Limit  int
}

type CreateOfferInput struct {
	ExpectedPlanRevision int64
	AssignmentKind       AssignmentKind
	Content              string
	PolicyVersion        string
}

type CloseOfferInput struct {
	ExpectedOfferRevision int64
	PolicyVersion         string
}

type PhotographerRevokeAssignmentInput struct {
	ExpectedAssignmentRevision int64
	PolicyVersion              string
}

type ClaimAssignmentTarget struct {
	Kind            AssignmentTargetKind `json:"kind"`
	ReadinessItemID *string              `json:"readiness_item_id,omitempty"`
	OfferID         *string              `json:"offer_id,omitempty"`
}

type ClaimAssignmentInput struct {
	Target                 ClaimAssignmentTarget
	ExpectedTargetRevision int64
	ClaimedByDisplayName   string
	ClaimReceiptCommitment ClaimReceiptCommitment
	PolicyVersion          string
}

type SelfRevokeAssignmentInput struct {
	ExpectedAssignmentRevision int64
	ClaimReceiptHash           ClaimReceiptCommitment
	PolicyVersion              string
}

type AnonymousAssignmentDeps struct {
	Resolver       ShareTokenResolver
	Runner         txcap.TransactionRunner[ShareTxScope]
	MutationBudget AnonymousMutationBudget
	MutationDigest MutationIPDigestResolver
	SourceIP       string
}

func (a *Application) ListAssignments(
	ctx context.Context,
	scope store.AccountScope,
	planID string,
	query AssignmentListQuery,
) (AssignmentManagementPageV1, error) {
	planID = trimID(planID)
	if planID == "" {
		return AssignmentManagementPageV1{}, validationError("plan id required")
	}
	cursorAt, cursorID, err := decodeAssignmentCursor(query.Cursor)
	if err != nil {
		return AssignmentManagementPageV1{}, err
	}
	limit := query.Limit
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 100 {
		return AssignmentManagementPageV1{}, validationError("limit out of range")
	}
	var page AssignmentManagementPageV1
	err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if _, _, _, err := a.repo.LoadPlanMeta(ctx, tx, planID, false); err != nil {
			return err
		}
		items, next, err := a.repo.ListAssignmentsPage(ctx, tx, planID, limit, cursorAt, cursorID)
		if err != nil {
			return err
		}
		if items == nil {
			items = []AssignmentManagementItemV1{}
		}
		page.Items = items
		page.NextCursor = next
		return nil
	})
	return page, err
}

func (a *Application) CreateOnSiteOffer(
	ctx context.Context,
	scope store.AccountScope,
	key, planID string,
	input CreateOfferInput,
) (OfferMutationResultV1, error) {
	planID = trimID(planID)
	if planID == "" || key == "" {
		return OfferMutationResultV1{}, validationError("offer create input invalid")
	}
	if input.AssignmentKind != AssignmentKindOnSiteSupport {
		return OfferMutationResultV1{}, validationError("assignment_kind must be on_site_support")
	}
	content, err := normalizeOfferContent(input.Content)
	if err != nil {
		return OfferMutationResultV1{}, err
	}
	if input.ExpectedPlanRevision < 1 {
		return OfferMutationResultV1{}, validationError("expected_plan_revision required")
	}
	policyVersion := input.PolicyVersion
	if policyVersion == "" {
		policyVersion = a.policy.Version()
	}
	if policyVersion != a.policy.Version() {
		return OfferMutationResultV1{}, validationError("policy_version invalid")
	}
	canonical, err := marshalCanonicalJSON(offerCreateCanonicalV1{
		AssignmentKind:       string(AssignmentKindOnSiteSupport),
		Content:              content,
		ExpectedPlanRevision: input.ExpectedPlanRevision,
		PolicyVersion:        policyVersion,
	})
	if err != nil {
		return OfferMutationResultV1{}, err
	}
	response, err := a.idempotency.Execute(ctx, scope, idempotency.Request{
		Operation:        idempotency.OperationPlanShareOfferCreate,
		Key:              key,
		ResourceIdentity: idempotency.OfferCollectionResource(planID),
		CanonicalBody:    canonical,
	}, func(tx store.TxAccountScope) (idempotency.StoredResponse, error) {
		result, err := a.createOfferInScope(ctx, tx, planID, input.ExpectedPlanRevision, content)
		if err != nil {
			return idempotency.StoredResponse{}, err
		}
		body, err := json.Marshal(result)
		return idempotency.StoredResponse{Status: 201, Body: body}, err
	})
	if err != nil {
		return OfferMutationResultV1{}, err
	}
	var result OfferMutationResultV1
	if err := json.Unmarshal(response.Body, &result); err != nil {
		return OfferMutationResultV1{}, fmt.Errorf("decode offer create replay: %w", err)
	}
	return result, nil
}

func (a *Application) CloseOnSiteOffer(
	ctx context.Context,
	scope store.AccountScope,
	key, planID, offerID string,
	input CloseOfferInput,
) (OfferMutationResultV1, error) {
	planID = trimID(planID)
	offerID = trimID(offerID)
	if planID == "" || offerID == "" || key == "" {
		return OfferMutationResultV1{}, validationError("offer close input invalid")
	}
	if input.ExpectedOfferRevision < 1 {
		return OfferMutationResultV1{}, validationError("expected_offer_revision required")
	}
	policyVersion := input.PolicyVersion
	if policyVersion == "" {
		policyVersion = a.policy.Version()
	}
	if policyVersion != a.policy.Version() {
		return OfferMutationResultV1{}, validationError("policy_version invalid")
	}
	canonical, err := marshalCanonicalJSON(offerCloseCanonicalV1{
		ExpectedOfferRevision: input.ExpectedOfferRevision,
		PolicyVersion:         policyVersion,
	})
	if err != nil {
		return OfferMutationResultV1{}, err
	}
	response, err := a.idempotency.Execute(ctx, scope, idempotency.Request{
		Operation:        idempotency.OperationPlanShareOfferClose,
		Key:              key,
		ResourceIdentity: idempotency.OfferResource(planID, offerID),
		CanonicalBody:    canonical,
	}, func(tx store.TxAccountScope) (idempotency.StoredResponse, error) {
		result, err := a.closeOfferInScope(ctx, tx, planID, offerID, input.ExpectedOfferRevision)
		if err != nil {
			return idempotency.StoredResponse{}, err
		}
		body, err := json.Marshal(result)
		return idempotency.StoredResponse{Status: 200, Body: body}, err
	})
	if err != nil {
		return OfferMutationResultV1{}, err
	}
	var result OfferMutationResultV1
	if err := json.Unmarshal(response.Body, &result); err != nil {
		return OfferMutationResultV1{}, fmt.Errorf("decode offer close replay: %w", err)
	}
	return result, nil
}

func (a *Application) PhotographerRevokeAssignment(
	ctx context.Context,
	scope store.AccountScope,
	key, planID, assignmentID string,
	input PhotographerRevokeAssignmentInput,
) (AssignmentMutationResultV1, error) {
	planID = trimID(planID)
	assignmentID = trimID(assignmentID)
	if planID == "" || assignmentID == "" || key == "" {
		return AssignmentMutationResultV1{}, validationError("assignment revoke input invalid")
	}
	if input.ExpectedAssignmentRevision < 1 {
		return AssignmentMutationResultV1{}, validationError("expected_assignment_revision required")
	}
	policyVersion := input.PolicyVersion
	if policyVersion == "" {
		policyVersion = a.policy.Version()
	}
	if policyVersion != a.policy.Version() {
		return AssignmentMutationResultV1{}, validationError("policy_version invalid")
	}
	canonical, err := marshalCanonicalJSON(assignmentRevokeCanonicalV1{
		ExpectedAssignmentRevision: input.ExpectedAssignmentRevision,
		PolicyVersion:              policyVersion,
	})
	if err != nil {
		return AssignmentMutationResultV1{}, err
	}
	response, err := a.idempotency.Execute(ctx, scope, idempotency.Request{
		Operation:        idempotency.OperationPlanShareAssignmentPhotographerRevoke,
		Key:              key,
		ResourceIdentity: idempotency.AssignmentResource(planID, assignmentID),
		CanonicalBody:    canonical,
	}, func(tx store.TxAccountScope) (idempotency.StoredResponse, error) {
		result, err := a.revokeAssignmentInBearerScope(
			ctx, tx, planID, assignmentID, input.ExpectedAssignmentRevision, policyVersion,
		)
		if err != nil {
			return idempotency.StoredResponse{}, err
		}
		body, err := json.Marshal(result)
		return idempotency.StoredResponse{Status: 200, Body: body}, err
	})
	if err != nil {
		return AssignmentMutationResultV1{}, err
	}
	var result AssignmentMutationResultV1
	if err := json.Unmarshal(response.Body, &result); err != nil {
		return AssignmentMutationResultV1{}, fmt.Errorf("decode assignment revoke replay: %w", err)
	}
	return result, nil
}

func (a *Application) ClaimAnonymousAssignment(
	ctx context.Context,
	deps AnonymousAssignmentDeps,
	key, presentedToken string,
	input ClaimAssignmentInput,
) (AssignmentClaimResultV1, error) {
	if deps.Resolver == nil || deps.Runner == nil {
		return AssignmentClaimResultV1{}, errors.New("anonymous assignment dependencies missing")
	}
	if key == "" || presentedToken == "" {
		return AssignmentClaimResultV1{}, validationError("anonymous claim input invalid")
	}
	now := a.clock()
	if deps.MutationBudget == nil || deps.MutationDigest == nil || deps.SourceIP == "" {
		return AssignmentClaimResultV1{}, securitybudget.ErrUnavailable
	}
	if _, err := ConsumeAnonymousMutationOuterIP(ctx, deps.MutationBudget, deps.MutationDigest, deps.SourceIP, now); err != nil {
		return AssignmentClaimResultV1{}, err
	}
	_, capability, err := deps.Resolver.Resolve(ctx, presentedToken)
	if err != nil {
		return AssignmentClaimResultV1{}, ErrShareNotFound
	}

	var gen ShareGeneration
	err = deps.Runner.Run(ctx, capability, func(_ txcap.LedgerTxView, scope ShareTxScope) error {
		loaded, loadErr := scope.Generations().LoadByValidatedContext(ctx)
		if loadErr != nil {
			return loadErr
		}
		gen = loaded
		return nil
	})
	if err != nil {
		return AssignmentClaimResultV1{}, mapAnonErr(err)
	}
	eff, _, _ := DeriveEffectiveState(gen, false, now)
	if gen.State != GenerationStateActive || eff != EffectiveActive || gen.ViewLevel != ViewLevelFull {
		return AssignmentClaimResultV1{}, ErrShareNotFound
	}
	if _, err := ConsumeAnonymousMutationOuterAfterGrant(
		ctx, deps.MutationBudget, deps.MutationDigest, deps.SourceIP, gen.ID, now,
	); err != nil {
		return AssignmentClaimResultV1{}, err
	}

	normalized, resource, typedBody, err := a.normalizeClaimInput(gen, input)
	if err != nil {
		return AssignmentClaimResultV1{}, err
	}
	frameBytes, frame, err := BuildCanonicalAnonymousMutationFrameV1Bytes(
		string(idempotency.OperationPlanShareAssignmentClaim), resource, typedBody,
	)
	if err != nil {
		return AssignmentClaimResultV1{}, err
	}
	fingerprint := ExactFrameFingerprint(frameBytes)
	keyDigest := IdempotencyKeyDigest(key)

	var hasAdmission bool
	err = deps.Runner.Run(ctx, capability, func(_ txcap.LedgerTxView, scope ShareTxScope) error {
		ok, lookupErr := scope.ReplayAdmissions().HasExactUnexpired(
			ctx, gen.ID, string(idempotency.OperationPlanShareAssignmentClaim), keyDigest, fingerprint, now,
		)
		if lookupErr != nil {
			return lookupErr
		}
		hasAdmission = ok
		return nil
	})
	if err != nil {
		return AssignmentClaimResultV1{}, mapAnonErr(err)
	}
	if !hasAdmission {
		if _, err := ConsumeAnonymousMutationBusinessQuota(
			ctx, deps.MutationBudget, deps.MutationDigest, deps.SourceIP, gen.ID, now,
		); err != nil {
			return AssignmentClaimResultV1{}, err
		}
	}

	frame.IdempotencyKey = key
	frame.TokenGenerationID = gen.ID
	frame.ExactFrameFingerprint = fingerprint

	for attempt := 0; attempt < sourceRetryAttempts; attempt++ {
		response, execErr := idempotency.ExecuteInCapability(
			ctx,
			a.idempotency,
			deps.Runner,
			capability,
			idempotency.Request{
				Operation:        idempotency.OperationPlanShareAssignmentClaim,
				Key:              key,
				ResourceIdentity: resource,
				CanonicalBody:    typedBody,
				ExactFrameHash:   frame.FrameHash,
			},
			func(scope ShareTxScope) (idempotency.StoredResponse, error) {
				result, cbErr := a.claimInShareScope(ctx, scope, gen, normalized, frame)
				if cbErr != nil {
					return idempotency.StoredResponse{}, cbErr
				}
				body, marshalErr := json.Marshal(result)
				return idempotency.StoredResponse{Status: 201, Body: body}, marshalErr
			},
		)
		if errors.Is(execErr, ErrSourceChangedRetry) || store.IsSerializationFailure(execErr) {
			continue
		}
		if execErr != nil {
			return AssignmentClaimResultV1{}, mapAnonErr(execErr)
		}
		var result AssignmentClaimResultV1
		if err := json.Unmarshal(response.Body, &result); err != nil {
			return AssignmentClaimResultV1{}, fmt.Errorf("decode assignment claim replay: %w", err)
		}
		return result, nil
	}
	return AssignmentClaimResultV1{}, ErrSourceChangedRetry
}

func (a *Application) SelfRevokeAnonymousAssignment(
	ctx context.Context,
	deps AnonymousAssignmentDeps,
	key, presentedToken, assignmentRef string,
	rawReceipt string,
	input SelfRevokeAssignmentInput,
) (AssignmentMutationResultV1, error) {
	if deps.Resolver == nil || deps.Runner == nil {
		return AssignmentMutationResultV1{}, errors.New("anonymous assignment dependencies missing")
	}
	assignmentRef = trimID(assignmentRef)
	if key == "" || presentedToken == "" || assignmentRef == "" {
		return AssignmentMutationResultV1{}, validationError("anonymous self-revoke input invalid")
	}
	now := a.clock()
	if deps.MutationBudget == nil || deps.MutationDigest == nil || deps.SourceIP == "" {
		return AssignmentMutationResultV1{}, securitybudget.ErrUnavailable
	}

	parsed, err := ParseClaimReceipt(rawReceipt)
	if err != nil {
		return AssignmentMutationResultV1{}, ErrShareNotFound
	}
	receiptHash := CommitClaimReceipt(parsed.Secret)
	for i := range parsed.Secret {
		parsed.Secret[i] = 0
	}

	if _, err := ConsumeAnonymousMutationOuterIP(ctx, deps.MutationBudget, deps.MutationDigest, deps.SourceIP, now); err != nil {
		return AssignmentMutationResultV1{}, err
	}
	_, capability, err := deps.Resolver.Resolve(ctx, presentedToken)
	if err != nil {
		return AssignmentMutationResultV1{}, ErrShareNotFound
	}

	var gen ShareGeneration
	err = deps.Runner.Run(ctx, capability, func(_ txcap.LedgerTxView, scope ShareTxScope) error {
		loaded, loadErr := scope.Generations().LoadByValidatedContext(ctx)
		if loadErr != nil {
			return loadErr
		}
		gen = loaded
		return nil
	})
	if err != nil {
		return AssignmentMutationResultV1{}, mapAnonErr(err)
	}
	eff, _, _ := DeriveEffectiveState(gen, false, now)
	if gen.State != GenerationStateActive || eff != EffectiveActive || gen.ViewLevel != ViewLevelFull {
		return AssignmentMutationResultV1{}, ErrShareNotFound
	}
	if _, err := ConsumeAnonymousMutationOuterAfterGrant(
		ctx, deps.MutationBudget, deps.MutationDigest, deps.SourceIP, gen.ID, now,
	); err != nil {
		return AssignmentMutationResultV1{}, err
	}

	if input.ExpectedAssignmentRevision < 1 {
		return AssignmentMutationResultV1{}, validationError("expected_assignment_revision required")
	}
	policyVersion := input.PolicyVersion
	if policyVersion == "" {
		policyVersion = a.policy.Version()
	}
	if policyVersion != a.policy.Version() {
		return AssignmentMutationResultV1{}, validationError("policy_version invalid")
	}
	resource := idempotency.AssignmentResource(gen.PlanID, assignmentRef)
	typedBody, err := marshalCanonicalJSON(assignmentSelfRevokeCanonicalV1{
		ClaimReceiptHash:           base64.RawURLEncoding.EncodeToString(receiptHash[:]),
		ExpectedAssignmentRevision: input.ExpectedAssignmentRevision,
		PolicyVersion:              policyVersion,
	})
	if err != nil {
		return AssignmentMutationResultV1{}, err
	}
	frameBytes, frame, err := BuildCanonicalAnonymousMutationFrameV1Bytes(
		string(idempotency.OperationPlanShareAssignmentSelfRevoke), resource, typedBody,
	)
	if err != nil {
		return AssignmentMutationResultV1{}, err
	}
	fingerprint := ExactFrameFingerprint(frameBytes)
	keyDigest := IdempotencyKeyDigest(key)

	var hasAdmission bool
	err = deps.Runner.Run(ctx, capability, func(_ txcap.LedgerTxView, scope ShareTxScope) error {
		ok, lookupErr := scope.ReplayAdmissions().HasExactUnexpired(
			ctx, gen.ID, string(idempotency.OperationPlanShareAssignmentSelfRevoke), keyDigest, fingerprint, now,
		)
		if lookupErr != nil {
			return lookupErr
		}
		hasAdmission = ok
		return nil
	})
	if err != nil {
		return AssignmentMutationResultV1{}, mapAnonErr(err)
	}
	if !hasAdmission {
		if _, err := ConsumeAnonymousMutationBusinessQuota(
			ctx, deps.MutationBudget, deps.MutationDigest, deps.SourceIP, gen.ID, now,
		); err != nil {
			return AssignmentMutationResultV1{}, err
		}
	}

	frame.IdempotencyKey = key
	frame.TokenGenerationID = gen.ID
	frame.ExactFrameFingerprint = fingerprint

	for attempt := 0; attempt < sourceRetryAttempts; attempt++ {
		response, execErr := idempotency.ExecuteInCapability(
			ctx,
			a.idempotency,
			deps.Runner,
			capability,
			idempotency.Request{
				Operation:        idempotency.OperationPlanShareAssignmentSelfRevoke,
				Key:              key,
				ResourceIdentity: resource,
				CanonicalBody:    typedBody,
				ExactFrameHash:   frame.FrameHash,
			},
			func(scope ShareTxScope) (idempotency.StoredResponse, error) {
				result, cbErr := a.selfRevokeInShareScope(
					ctx, scope, gen, assignmentRef, input.ExpectedAssignmentRevision, receiptHash, policyVersion, frame,
				)
				if cbErr != nil {
					return idempotency.StoredResponse{}, cbErr
				}
				body, marshalErr := json.Marshal(result)
				return idempotency.StoredResponse{Status: 200, Body: body}, marshalErr
			},
		)
		if errors.Is(execErr, ErrSourceChangedRetry) || store.IsSerializationFailure(execErr) {
			continue
		}
		if execErr != nil {
			return AssignmentMutationResultV1{}, mapAnonErr(execErr)
		}
		var result AssignmentMutationResultV1
		if err := json.Unmarshal(response.Body, &result); err != nil {
			return AssignmentMutationResultV1{}, fmt.Errorf("decode assignment self-revoke replay: %w", err)
		}
		return result, nil
	}
	return AssignmentMutationResultV1{}, ErrSourceChangedRetry
}

// ReadinessRemovalGuard is the production planshare implementation of core's
// shootplanning.ReadinessRemovalGuard.
type ReadinessRemovalGuard struct{}

func (ReadinessRemovalGuard) AssertRemovableInScope(
	ctx context.Context,
	tx store.TxAccountScope,
	planID, readinessID string,
) error {
	exists, err := shareAssignmentStore{tx: tx}.ExistsActiveReadiness(ctx, planID, readinessID)
	if err != nil {
		return err
	}
	if exists {
		return shootplanning.ErrReadinessAssignmentActive
	}
	return nil
}

func (a *Application) createOfferInScope(
	ctx context.Context,
	tx store.TxAccountScope,
	planID string,
	expectedPlanRevision int64,
	content string,
) (OfferMutationResultV1, error) {
	_, revision, archived, err := a.repo.LoadPlanMeta(ctx, tx, planID, true)
	if err != nil {
		return OfferMutationResultV1{}, err
	}
	if archived {
		return OfferMutationResultV1{}, ErrNotFound
	}
	if revision != expectedPlanRevision {
		return OfferMutationResultV1{}, ErrPlanRevisionConflict
	}
	id := "sao_" + uuid.NewString()
	now := a.clock()
	if err := tx.Insert(ctx, "share_assignment_offers",
		[]string{"id", "plan_id", "assignment_kind", "content", "state", "revision", "created_at"},
		id, planID, string(AssignmentKindOnSiteSupport), content, "open", int64(1), now,
	); err != nil {
		return OfferMutationResultV1{}, err
	}
	return OfferMutationResultV1{OfferID: id, State: "open", Revision: 1}, nil
}

func (a *Application) closeOfferInScope(
	ctx context.Context,
	tx store.TxAccountScope,
	planID, offerID string,
	expectedRevision int64,
) (OfferMutationResultV1, error) {
	if _, _, archived, err := a.repo.LoadPlanMeta(ctx, tx, planID, true); err != nil {
		return OfferMutationResultV1{}, err
	} else if archived {
		return OfferMutationResultV1{}, ErrNotFound
	}
	var state string
	var revision int64
	err := tx.QueryRowForUpdate(ctx, "share_assignment_offers",
		"state, revision", "plan_id = $2 AND id = $3", planID, offerID,
	).Scan(&state, &revision)
	if errors.Is(err, store.ErrNoRows) {
		return OfferMutationResultV1{}, ErrOfferNotFound
	}
	if err != nil {
		return OfferMutationResultV1{}, err
	}
	if revision != expectedRevision {
		return OfferMutationResultV1{}, ErrOfferStale
	}
	if state == "closed" {
		return OfferMutationResultV1{OfferID: offerID, State: "closed", Revision: revision}, nil
	}
	active, err := tx.Exists(ctx, "share_assignments",
		"plan_id = $2 AND offer_id = $3 AND status = 'active'", planID, offerID)
	if err != nil {
		return OfferMutationResultV1{}, err
	}
	if active {
		return OfferMutationResultV1{}, ErrAssignmentActive
	}
	now := a.clock()
	n, err := tx.Update(ctx, "share_assignment_offers",
		"state = 'closed', closed_at = $2, revision = revision + 1",
		"plan_id = $3 AND id = $4 AND state = 'open' AND revision = $5",
		now, planID, offerID, expectedRevision,
	)
	if err != nil {
		return OfferMutationResultV1{}, err
	}
	if n == 0 {
		return OfferMutationResultV1{}, ErrOfferStale
	}
	return OfferMutationResultV1{OfferID: offerID, State: "closed", Revision: expectedRevision + 1}, nil
}

func (a *Application) revokeAssignmentInBearerScope(
	ctx context.Context,
	tx store.TxAccountScope,
	planID, assignmentID string,
	expectedRevision int64,
	policyVersion string,
) (AssignmentMutationResultV1, error) {
	if _, _, archived, err := a.repo.LoadPlanMeta(ctx, tx, planID, true); err != nil {
		return AssignmentMutationResultV1{}, err
	} else if archived {
		return AssignmentMutationResultV1{}, ErrNotFound
	}
	fence := tx.PlanningReminderFence()
	locked, err := fence.LockCurrentAccount(ctx)
	if err != nil {
		return AssignmentMutationResultV1{}, err
	}
	stores := assignmentMutationStores{
		assignments:  shareAssignmentStore{tx: tx},
		events:       shareAssignmentSourceEventStore{tx: tx},
		observations: shareObservationStore{tx: tx},
		tx:           tx,
	}
	return revokeAssignmentKernel(ctx, locked, stores, assignmentRevokeCommand{
		PlanID:            planID,
		AssignmentID:      assignmentID,
		ExpectedRevision:  expectedRevision,
		RevokedBy:         AssignmentRevokedByPhotographer,
		PolicyVersion:     policyVersion,
		RequireReceipt:    false,
		TokenGenerationID: "",
		Now:               a.clock(),
	})
}

type normalizedClaim struct {
	Kind                   AssignmentKind
	ReadinessItemID        *string
	OfferID                *string
	ExpectedTargetRevision int64
	ClaimedByDisplayName   string
	ClaimReceiptCommitment ClaimReceiptCommitment
	PolicyVersion          string
	TargetRef              string
}

func (a *Application) normalizeClaimInput(
	gen ShareGeneration,
	input ClaimAssignmentInput,
) (normalizedClaim, idempotency.ResourceIdentity, []byte, error) {
	policyVersion := input.PolicyVersion
	if policyVersion == "" {
		policyVersion = a.policy.Version()
	}
	if policyVersion != a.policy.Version() {
		return normalizedClaim{}, idempotency.ResourceIdentity{}, nil, validationError("policy_version invalid")
	}
	if input.ExpectedTargetRevision < 1 {
		return normalizedClaim{}, idempotency.ResourceIdentity{}, nil, validationError("expected_target_revision required")
	}
	author, err := normalizeAuthorDisplayName(input.ClaimedByDisplayName)
	if err != nil {
		return normalizedClaim{}, idempotency.ResourceIdentity{}, nil, err
	}
	if err := rejectUnknownControlRunes(author); err != nil {
		return normalizedClaim{}, idempotency.ResourceIdentity{}, nil, err
	}
	var kind AssignmentKind
	var readinessID, offerID *string
	var targetKind, targetID string
	switch input.Target.Kind {
	case AssignmentTargetReadiness:
		id := trimID(stringPtrValue(input.Target.ReadinessItemID))
		if id == "" || input.Target.OfferID != nil {
			return normalizedClaim{}, idempotency.ResourceIdentity{}, nil, validationError("readiness target invalid")
		}
		kind = AssignmentKindReadiness
		readinessID = &id
		targetKind = "readiness"
		targetID = id
	case AssignmentTargetOnSiteSupport:
		id := trimID(stringPtrValue(input.Target.OfferID))
		if id == "" || input.Target.ReadinessItemID != nil {
			return normalizedClaim{}, idempotency.ResourceIdentity{}, nil, validationError("offer target invalid")
		}
		kind = AssignmentKindOnSiteSupport
		offerID = &id
		targetKind = "on_site_support"
		targetID = id
	default:
		return normalizedClaim{}, idempotency.ResourceIdentity{}, nil, validationError("target kind invalid")
	}
	resource := AssignmentClaimResource(gen.PlanID, gen.ID, targetKind, targetID)
	typedBody, err := marshalCanonicalJSON(assignmentClaimCanonicalV1{
		ClaimReceiptCommitment: base64.RawURLEncoding.EncodeToString(input.ClaimReceiptCommitment[:]),
		ClaimedByDisplayName:   author,
		ExpectedTargetRevision: input.ExpectedTargetRevision,
		PolicyVersion:          policyVersion,
		Target: claimTargetCanonicalV1{
			Kind:            string(input.Target.Kind),
			OfferID:         offerID,
			ReadinessItemID: readinessID,
		},
	})
	if err != nil {
		return normalizedClaim{}, idempotency.ResourceIdentity{}, nil, err
	}
	return normalizedClaim{
		Kind:                   kind,
		ReadinessItemID:        readinessID,
		OfferID:                offerID,
		ExpectedTargetRevision: input.ExpectedTargetRevision,
		ClaimedByDisplayName:   author,
		ClaimReceiptCommitment: input.ClaimReceiptCommitment,
		PolicyVersion:          policyVersion,
		TargetRef:              targetID,
	}, resource, typedBody, nil
}

func (a *Application) claimInShareScope(
	ctx context.Context,
	scope ShareTxScope,
	gen ShareGeneration,
	claim normalizedClaim,
	frame CanonicalAnonymousMutationFrameV1,
) (AssignmentClaimResultV1, error) {
	concrete, ok := scope.(shareTxScope)
	if !ok {
		return AssignmentClaimResultV1{}, errors.New("share scope type unsupported")
	}
	now := a.clock()
	current, err := scope.Generations().LoadByValidatedContext(ctx)
	if err != nil || current.ID != gen.ID || current.State != GenerationStateActive {
		return AssignmentClaimResultV1{}, ErrShareNotFound
	}
	eff, _, _ := DeriveEffectiveState(current, false, now)
	if eff != EffectiveActive || current.ViewLevel != ViewLevelFull {
		return AssignmentClaimResultV1{}, ErrShareNotFound
	}

	locked, err := scope.PlanningReminderFence().LockCurrentAccount(ctx)
	if err != nil {
		return AssignmentClaimResultV1{}, err
	}

	hint, err := preReadFullEligibility(ctx, concrete.tx, current.PlanID)
	if err != nil {
		if errors.Is(err, ErrFullViewNotEligible) {
			return AssignmentClaimResultV1{}, ErrShareNotFound
		}
		return AssignmentClaimResultV1{}, err
	}
	if current.EligibilityLinkEpochID != nil && hint.LinkEpochID != *current.EligibilityLinkEpochID {
		return AssignmentClaimResultV1{}, ErrShareNotFound
	}
	if err := lockCRMOrderGraph(ctx, concrete.tx, hint); err != nil {
		return AssignmentClaimResultV1{}, mapAnonErr(err)
	}
	rechecked, err := recheckFullEligibility(ctx, concrete.tx, hint)
	if err != nil {
		if errors.Is(err, ErrFullViewNotEligible) {
			return AssignmentClaimResultV1{}, ErrShareNotFound
		}
		return AssignmentClaimResultV1{}, err
	}
	_ = rechecked

	_, _, archived, err := (PostgresRepository{}).LoadPlanMeta(ctx, concrete.tx, current.PlanID, true)
	if err != nil {
		return AssignmentClaimResultV1{}, mapAnonErr(err)
	}
	if archived {
		return AssignmentClaimResultV1{}, ErrShareNotFound
	}

	// Lock token generation row after plan.
	var lockedGenID string
	if err := concrete.tx.QueryRowForUpdate(ctx, "share_generations", "id", "id = $2", current.ID).Scan(&lockedGenID); err != nil {
		return AssignmentClaimResultV1{}, ErrShareNotFound
	}

	prepared, err := lockAndPrepareClaimTarget(ctx, concrete.tx, current.PlanID, claim)
	if err != nil {
		return AssignmentClaimResultV1{}, err
	}

	// Confirm no active assignment yet (lock target path) before reserve.
	if _, err := scope.Assignments().LockActiveByTarget(
		ctx, current.PlanID, claim.Kind, claim.ReadinessItemID, claim.OfferID,
	); err == nil {
		return AssignmentClaimResultV1{}, ErrAssignmentAlreadyClaimed
	} else if !errors.Is(err, ErrAssignmentNotFound) {
		return AssignmentClaimResultV1{}, err
	}

	eventID := "sae_" + uuid.NewString()
	generation, err := locked.ReserveGeneration(ctx, planningreminder.MutationFact{
		PlanID:        current.PlanID,
		MutationKind:  planningreminder.MutationAssignmentActivated,
		SourceEventID: &eventID,
	})
	if err != nil {
		return AssignmentClaimResultV1{}, err
	}

	assignmentID := "saa_" + uuid.NewString()
	row, err := scope.Assignments().Insert(ctx, ShareAssignmentInsert{
		ID:                          assignmentID,
		PlanID:                      current.PlanID,
		TokenGenerationID:           current.ID,
		AssignmentKind:              claim.Kind,
		ReadinessItemID:             claim.ReadinessItemID,
		OfferID:                     claim.OfferID,
		ContentSnapshot:             prepared.Content,
		ClaimedByDisplayName:        claim.ClaimedByDisplayName,
		PreparationLeadDaysSnapshot: prepared.LeadDays,
		LeadRuleVersion:             prepared.LeadRuleVersion,
		ClaimReceiptCommitment:      claim.ClaimReceiptCommitment,
		ClaimedAt:                   now,
	})
	if err != nil {
		return AssignmentClaimResultV1{}, err
	}

	fingerprint := contentFingerprint(prepared.Content)
	if err := scope.AssignmentSourceEvents().Insert(ctx, ShareAssignmentSourceEventInsert{
		EventID:                     eventID,
		PlanID:                      current.PlanID,
		AssignmentID:                row.ID,
		AssignmentRevision:          row.Revision,
		AccountSourceGeneration:     generation,
		EventKind:                   string(planningreminder.MutationAssignmentActivated),
		AssignmentKind:              claim.Kind,
		ReadinessItemID:             claim.ReadinessItemID,
		PreparationLeadDaysSnapshot: prepared.LeadDays,
		LeadRuleVersion:             prepared.LeadRuleVersion,
		ContentFingerprint:          fingerprint,
		OccurredAt:                  now,
	}); err != nil {
		return AssignmentClaimResultV1{}, err
	}

	rev := row.Revision
	if err := scope.Observations().EnsureAssignment(ctx, AssignmentObservationInput{
		PlanID:             current.PlanID,
		TokenGenerationID:  current.ID,
		Kind:               assignmentObservationClaim,
		SourceFactID:       eventID,
		SourceFactRevision: &rev,
		PolicyVersion:      claim.PolicyVersion,
		OccurredAt:         now,
	}); err != nil {
		return AssignmentClaimResultV1{}, err
	}
	if err := scope.ReplayAdmissions().RecordForCurrentLedgerClaim(ctx, frame); err != nil {
		return AssignmentClaimResultV1{}, err
	}
	// Leave generation work pending for the reminder fence consumer (ITEM-6 S2).
	return AssignmentClaimResultV1{
		AssignmentID:   row.ID,
		AssignmentKind: row.AssignmentKind,
		TargetRef:      claim.TargetRef,
		Status:         row.Status,
		Revision:       row.Revision,
		ClaimedAt:      row.ClaimedAt,
	}, nil
}

func (a *Application) selfRevokeInShareScope(
	ctx context.Context,
	scope ShareTxScope,
	gen ShareGeneration,
	assignmentID string,
	expectedRevision int64,
	receiptHash ClaimReceiptCommitment,
	policyVersion string,
	frame CanonicalAnonymousMutationFrameV1,
) (AssignmentMutationResultV1, error) {
	concrete, ok := scope.(shareTxScope)
	if !ok {
		return AssignmentMutationResultV1{}, errors.New("share scope type unsupported")
	}
	now := a.clock()
	current, err := scope.Generations().LoadByValidatedContext(ctx)
	if err != nil || current.ID != gen.ID || current.State != GenerationStateActive {
		return AssignmentMutationResultV1{}, ErrShareNotFound
	}
	eff, _, _ := DeriveEffectiveState(current, false, now)
	if eff != EffectiveActive || current.ViewLevel != ViewLevelFull {
		return AssignmentMutationResultV1{}, ErrShareNotFound
	}
	locked, err := scope.PlanningReminderFence().LockCurrentAccount(ctx)
	if err != nil {
		return AssignmentMutationResultV1{}, err
	}
	hint, err := preReadFullEligibility(ctx, concrete.tx, current.PlanID)
	if err != nil {
		if errors.Is(err, ErrFullViewNotEligible) {
			return AssignmentMutationResultV1{}, ErrShareNotFound
		}
		return AssignmentMutationResultV1{}, err
	}
	if err := lockCRMOrderGraph(ctx, concrete.tx, hint); err != nil {
		return AssignmentMutationResultV1{}, mapAnonErr(err)
	}
	if _, err := recheckFullEligibility(ctx, concrete.tx, hint); err != nil {
		if errors.Is(err, ErrFullViewNotEligible) {
			return AssignmentMutationResultV1{}, ErrShareNotFound
		}
		return AssignmentMutationResultV1{}, err
	}
	if _, _, archived, err := (PostgresRepository{}).LoadPlanMeta(ctx, concrete.tx, current.PlanID, true); err != nil {
		return AssignmentMutationResultV1{}, mapAnonErr(err)
	} else if archived {
		return AssignmentMutationResultV1{}, ErrShareNotFound
	}
	var lockedGenID string
	if err := concrete.tx.QueryRowForUpdate(ctx, "share_generations", "id", "id = $2", current.ID).Scan(&lockedGenID); err != nil {
		return AssignmentMutationResultV1{}, ErrShareNotFound
	}

	stores := assignmentMutationStores{
		assignments:  scope.Assignments(),
		events:       scope.AssignmentSourceEvents(),
		observations: scope.Observations(),
		admissions:   scope.ReplayAdmissions(),
		tx:           concrete.tx,
	}
	result, err := revokeAssignmentKernel(ctx, locked, stores, assignmentRevokeCommand{
		PlanID:            current.PlanID,
		AssignmentID:      assignmentID,
		ExpectedRevision:  expectedRevision,
		RevokedBy:         AssignmentRevokedByAnonymous,
		PolicyVersion:     policyVersion,
		RequireReceipt:    true,
		ReceiptHash:       receiptHash,
		TokenGenerationID: current.ID,
		Frame:             &frame,
		Now:               now,
	})
	if errors.Is(err, ErrAssignmentNotFound) || errors.Is(err, ErrAssignmentStale) {
		return AssignmentMutationResultV1{}, ErrShareNotFound
	}
	return result, err
}

type assignmentMutationStores struct {
	assignments  ShareAssignmentStore
	events       ShareAssignmentSourceEventStore
	observations ShareObservationStore
	admissions   ShareReplayAdmissionStore
	tx           store.TxAccountScope
}

type assignmentRevokeCommand struct {
	PlanID            string
	AssignmentID      string
	ExpectedRevision  int64
	RevokedBy         AssignmentRevokedBy
	PolicyVersion     string
	RequireReceipt    bool
	ReceiptHash       ClaimReceiptCommitment
	TokenGenerationID string
	Frame             *CanonicalAnonymousMutationFrameV1
	Now               time.Time
}

func revokeAssignmentKernel(
	ctx context.Context,
	locked planningreminder.LockedFenceTx,
	stores assignmentMutationStores,
	cmd assignmentRevokeCommand,
) (AssignmentMutationResultV1, error) {
	current, err := stores.assignments.LockByID(ctx, cmd.PlanID, cmd.AssignmentID)
	if err != nil {
		return AssignmentMutationResultV1{}, err
	}
	if current.Status == AssignmentStatusRevoked {
		if current.RevokedAt == nil {
			return AssignmentMutationResultV1{}, ErrAssignmentStale
		}
		// Material revoke already applied; exact ledger replay should not reach
		// here. Treat unexpected hits as stale rather than re-reserving.
		return AssignmentMutationResultV1{}, ErrAssignmentStale
	}
	if current.Revision != cmd.ExpectedRevision {
		return AssignmentMutationResultV1{}, ErrAssignmentStale
	}
	if cmd.RequireReceipt {
		if !subtleReceiptMatch(cmd.ReceiptHash, current.ClaimReceiptCommitment) {
			return AssignmentMutationResultV1{}, ErrShareNotFound
		}
	}

	eventID := "sae_" + uuid.NewString()
	generation, err := locked.ReserveGeneration(ctx, planningreminder.MutationFact{
		PlanID:        cmd.PlanID,
		MutationKind:  planningreminder.MutationAssignmentRevoked,
		SourceEventID: &eventID,
	})
	if err != nil {
		return AssignmentMutationResultV1{}, err
	}
	updated, err := stores.assignments.RevokeCAS(
		ctx, cmd.PlanID, cmd.AssignmentID, cmd.ExpectedRevision, cmd.RevokedBy, cmd.Now,
	)
	if err != nil {
		return AssignmentMutationResultV1{}, err
	}
	tokenGenerationID := cmd.TokenGenerationID
	if tokenGenerationID == "" {
		tokenGenerationID = current.TokenGenerationID
	}
	if err := stores.events.Insert(ctx, ShareAssignmentSourceEventInsert{
		EventID:                     eventID,
		PlanID:                      cmd.PlanID,
		AssignmentID:                updated.ID,
		AssignmentRevision:          updated.Revision,
		AccountSourceGeneration:     generation,
		EventKind:                   string(planningreminder.MutationAssignmentRevoked),
		AssignmentKind:              updated.AssignmentKind,
		ReadinessItemID:             updated.ReadinessItemID,
		PreparationLeadDaysSnapshot: updated.PreparationLeadDaysSnapshot,
		LeadRuleVersion:             updated.LeadRuleVersion,
		ContentFingerprint:          contentFingerprint(updated.ContentSnapshot),
		OccurredAt:                  cmd.Now,
	}); err != nil {
		return AssignmentMutationResultV1{}, err
	}
	rev := updated.Revision
	if err := stores.observations.EnsureAssignment(ctx, AssignmentObservationInput{
		PlanID:             cmd.PlanID,
		TokenGenerationID:  tokenGenerationID,
		Kind:               assignmentObservationRevoke,
		SourceFactID:       eventID,
		SourceFactRevision: &rev,
		PolicyVersion:      cmd.PolicyVersion,
		OccurredAt:         cmd.Now,
	}); err != nil {
		return AssignmentMutationResultV1{}, err
	}
	if cmd.Frame != nil && stores.admissions != nil {
		if err := stores.admissions.RecordForCurrentLedgerClaim(ctx, *cmd.Frame); err != nil {
			return AssignmentMutationResultV1{}, err
		}
	}
	// Leave generation work pending for the reminder fence consumer (ITEM-6 S2).
	if updated.RevokedAt == nil {
		return AssignmentMutationResultV1{}, errors.New("revoked assignment missing revoked_at")
	}
	return AssignmentMutationResultV1{
		AssignmentID: updated.ID,
		Status:       updated.Status,
		Revision:     updated.Revision,
		RevokedAt:    *updated.RevokedAt,
	}, nil
}

type preparedClaimTarget struct {
	Content         string
	LeadDays        *int
	LeadRuleVersion *string
}

func lockAndPrepareClaimTarget(
	ctx context.Context,
	tx store.TxAccountScope,
	planID string,
	claim normalizedClaim,
) (preparedClaimTarget, error) {
	switch claim.Kind {
	case AssignmentKindReadiness:
		var content string
		var lead sql.NullInt64
		var revision int64
		err := tx.QueryRowForUpdate(ctx, "shoot_plan_readiness_items",
			"requirement, default_preparation_lead_days, revision",
			"plan_id = $2 AND id = $3 AND removed_at IS NULL",
			planID, *claim.ReadinessItemID,
		).Scan(&content, &lead, &revision)
		if errors.Is(err, store.ErrNoRows) {
			return preparedClaimTarget{}, ErrShareNotFound
		}
		if err != nil {
			return preparedClaimTarget{}, err
		}
		if revision != claim.ExpectedTargetRevision {
			return preparedClaimTarget{}, ErrShareNotFound
		}
		leadDays, leadRule := resolveLeadSnapshot(lead)
		return preparedClaimTarget{Content: content, LeadDays: &leadDays, LeadRuleVersion: &leadRule}, nil
	case AssignmentKindOnSiteSupport:
		var content, state string
		var revision int64
		err := tx.QueryRowForUpdate(ctx, "share_assignment_offers",
			"content, state, revision",
			"plan_id = $2 AND id = $3",
			planID, *claim.OfferID,
		).Scan(&content, &state, &revision)
		if errors.Is(err, store.ErrNoRows) {
			return preparedClaimTarget{}, ErrShareNotFound
		}
		if err != nil {
			return preparedClaimTarget{}, err
		}
		if state != "open" || revision != claim.ExpectedTargetRevision {
			return preparedClaimTarget{}, ErrShareNotFound
		}
		return preparedClaimTarget{Content: content}, nil
	default:
		return preparedClaimTarget{}, validationError("assignment kind invalid")
	}
}

func resolveLeadSnapshot(lead sql.NullInt64) (int, string) {
	if lead.Valid {
		return int(lead.Int64), leadRuleReadinessItemV1
	}
	return platformDefaultLeadDays, leadRulePlatformDefaultV1
}

func contentFingerprint(content string) string {
	sum := sha256.Sum256([]byte(content))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func subtleReceiptMatch(presented, stored ClaimReceiptCommitment) bool {
	return subtle.ConstantTimeCompare(presented[:], stored[:]) == 1
}

func normalizeOfferContent(raw string) (string, error) {
	if strings.ContainsAny(raw, "<>") || looksLikeHTML(raw) {
		return "", validationError("content must be plain text")
	}
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", validationError("content required")
	}
	n := utf8.RuneCountInString(trimmed)
	if n < offerContentMinRunes || n > offerContentMaxRunes {
		return "", validationError("content length out of range")
	}
	return trimmed, nil
}

func stringPtrValue(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func AssignmentClaimResource(planID, generationID, targetKind, targetID string) idempotency.ResourceIdentity {
	return idempotency.AssignmentClaimResource(planID, TupleV1(generationID, targetKind, targetID))
}

type offerCreateCanonicalV1 struct {
	AssignmentKind       string `json:"assignment_kind"`
	Content              string `json:"content"`
	ExpectedPlanRevision int64  `json:"expected_plan_revision"`
	PolicyVersion        string `json:"policy_version"`
}

type offerCloseCanonicalV1 struct {
	ExpectedOfferRevision int64  `json:"expected_offer_revision"`
	PolicyVersion         string `json:"policy_version"`
}

type assignmentRevokeCanonicalV1 struct {
	ExpectedAssignmentRevision int64  `json:"expected_assignment_revision"`
	PolicyVersion              string `json:"policy_version"`
}

type claimTargetCanonicalV1 struct {
	Kind            string  `json:"kind"`
	OfferID         *string `json:"offer_id,omitempty"`
	ReadinessItemID *string `json:"readiness_item_id,omitempty"`
}

type assignmentClaimCanonicalV1 struct {
	ClaimReceiptCommitment string                 `json:"claim_receipt_commitment"`
	ClaimedByDisplayName   string                 `json:"claimed_by_display_name"`
	ExpectedTargetRevision int64                  `json:"expected_target_revision"`
	PolicyVersion          string                 `json:"policy_version"`
	Target                 claimTargetCanonicalV1 `json:"target"`
}

type assignmentSelfRevokeCanonicalV1 struct {
	ClaimReceiptHash           string `json:"claim_receipt_hash"`
	ExpectedAssignmentRevision int64  `json:"expected_assignment_revision"`
	PolicyVersion              string `json:"policy_version"`
}
