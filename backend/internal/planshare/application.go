package planshare

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/platform/idempotency"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const sourceRetryAttempts = 3

type Application struct {
	repo        PostgresRepository
	idempotency *idempotency.Executor
	policy      SharePolicyV1
	now         func() time.Time
}

type ApplicationOption func(*Application)

func WithClock(now func() time.Time) ApplicationOption {
	return func(a *Application) { a.now = now }
}

func NewApplication(executor *idempotency.Executor, options ...ApplicationOption) *Application {
	app := &Application{
		repo:        NewPostgresRepository(),
		idempotency: executor,
		policy:      SharePolicyV1{},
		now:         time.Now,
	}
	for _, option := range options {
		option(app)
	}
	return app
}

func (a *Application) clock() time.Time {
	if a == nil || a.now == nil {
		return time.Now().UTC()
	}
	return a.now().UTC()
}

type ManagementQuery struct {
	OfferCursor string
	OfferLimit  int
}

func (a *Application) GetManagementProjection(
	ctx context.Context,
	scope store.AccountScope,
	planID string,
	query ManagementQuery,
) (ShareManagementProjectionV1, error) {
	planID = trimID(planID)
	if planID == "" {
		return ShareManagementProjectionV1{}, validationError("plan id required")
	}
	cursorAt, cursorID, err := decodeOfferCursor(query.OfferCursor)
	if err != nil {
		return ShareManagementProjectionV1{}, err
	}
	now := a.clock()
	var projection ShareManagementProjectionV1
	err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		_, _, archived, err := a.repo.LoadPlanMeta(ctx, tx, planID, false)
		if err != nil {
			return err
		}
		window, err := a.repo.LoadExecutionWindow(ctx, tx, planID)
		if err != nil {
			return err
		}
		views := []ViewLevel{ViewLevelProposal, ViewLevelFull}
		projection.ShareViews = make([]ShareViewProjectionV1, 0, len(views))
		for _, view := range views {
			var viewWindow *ExecutionWindowHint
			if view == ViewLevelFull {
				viewWindow = window
			}
			item := ShareViewProjectionV1{
				ViewLevel:    view,
				ExpiryPolicy: a.policy.Project(view, now, viewWindow),
			}
			latest, err := a.repo.LatestByView(ctx, tx, planID, view)
			if err != nil {
				return err
			}
			if latest != nil {
				eff, reason, endedAt := DeriveEffectiveState(*latest, archived, now)
				item.LatestGeneration = &LatestGenerationProjectionV1{
					ShareID:        latest.ID,
					Generation:     latest.Generation,
					Fingerprint:    latest.Fingerprint,
					EffectiveState: eff,
					EndedReason:    reason,
					IssuedAt:       latest.IssuedAt,
					ExpiresAt:      latest.ExpiresAt,
					EndedAt:        endedAt,
					FirstOpenedAt:  latest.FirstOpenedAt,
					Revision:       latest.Revision,
				}
			}
			projection.ShareViews = append(projection.ShareViews, item)
		}
		offers, next, err := a.repo.ListOffersPage(ctx, tx, planID, query.OfferLimit, cursorAt, cursorID)
		if err != nil {
			return err
		}
		if offers == nil {
			offers = []OnSiteOfferProjectionV1{}
		}
		projection.OnSiteOffers = offers
		projection.OffersNextCursor = next
		return nil
	})
	return projection, err
}

type IssueInput struct {
	ExpectedPlanRevision int64
	ViewLevel            ViewLevel
	SecretCommitment     ShareSecretCommitment
	ExpiresAt            time.Time
	ExpirySource         ExpirySourceV1
	PolicyVersion        string
}

func (a *Application) Issue(
	ctx context.Context,
	scope store.AccountScope,
	key, planID string,
	input IssueInput,
) (ShareIssueResultV1, error) {
	planID = trimID(planID)
	if err := validateIssueInput(input); err != nil || planID == "" || key == "" {
		if err == nil {
			err = validationError("issue input invalid")
		}
		return ShareIssueResultV1{}, err
	}
	canonical, err := marshalIssueCanonical(
		input.ExpectedPlanRevision, input.ViewLevel, input.SecretCommitment,
		input.ExpiresAt, input.ExpirySource, input.PolicyVersion,
	)
	if err != nil {
		return ShareIssueResultV1{}, err
	}
	for attempt := 0; attempt < sourceRetryAttempts; attempt++ {
		response, err := a.idempotency.Execute(ctx, scope, idempotency.Request{
			Operation:        idempotency.OperationPlanShareIssue,
			Key:              key,
			ResourceIdentity: issueResource(planID, input.ViewLevel),
			CanonicalBody:    canonical,
		}, func(tx store.TxAccountScope) (idempotency.StoredResponse, error) {
			result, err := a.issueInScope(ctx, tx, planID, input)
			if err != nil {
				return idempotency.StoredResponse{}, err
			}
			body, err := json.Marshal(result)
			return idempotency.StoredResponse{Status: 201, Body: body}, err
		})
		if errors.Is(err, ErrSourceChangedRetry) {
			continue
		}
		if err != nil {
			return ShareIssueResultV1{}, err
		}
		return decodeIssueResult(response.Body)
	}
	return ShareIssueResultV1{}, ErrSourceChangedRetry
}

func (a *Application) issueInScope(
	ctx context.Context,
	tx store.TxAccountScope,
	planID string,
	input IssueInput,
) (ShareIssueResultV1, error) {
	now := a.clock()
	window, err := a.repo.LoadExecutionWindow(ctx, tx, planID)
	if err != nil {
		return ShareIssueResultV1{}, err
	}
	if err := a.resolveExpiry(input.ViewLevel, input.ExpiresAt, input.ExpirySource, input.PolicyVersion, now, window); err != nil {
		return ShareIssueResultV1{}, err
	}
	var eligibilityEpoch *string
	if input.ViewLevel == ViewLevelFull {
		hint, err := preReadFullEligibility(ctx, tx, planID)
		if err != nil {
			return ShareIssueResultV1{}, err
		}
		if err := lockCRMOrderGraph(ctx, tx, hint); err != nil {
			return ShareIssueResultV1{}, err
		}
		elig, err := recheckFullEligibility(ctx, tx, hint)
		if err != nil {
			return ShareIssueResultV1{}, err
		}
		eligibilityEpoch = &elig.LinkEpochID
	} else {
		if err := lockIDs(ctx, tx, "shoot_plans", []string{planID}); err != nil {
			return ShareIssueResultV1{}, err
		}
	}
	_, revision, archived, err := a.repo.LoadPlanMeta(ctx, tx, planID, true)
	if err != nil {
		return ShareIssueResultV1{}, err
	}
	if archived {
		return ShareIssueResultV1{}, ErrNotFound
	}
	if revision != input.ExpectedPlanRevision {
		return ShareIssueResultV1{}, ErrPlanRevisionConflict
	}
	if active, err := a.repo.FindActiveByView(ctx, tx, planID, input.ViewLevel, true); err != nil {
		return ShareIssueResultV1{}, err
	} else if active != nil {
		return ShareIssueResultV1{}, ErrShareGenerationExists
	}
	genNumber, err := a.repo.NextGenerationNumber(ctx, tx, planID, input.ViewLevel)
	if err != nil {
		return ShareIssueResultV1{}, err
	}
	selector, err := a.newSelector()
	if err != nil {
		return ShareIssueResultV1{}, err
	}
	gen := ShareGeneration{
		ID:                     "sg_" + uuid.NewString(),
		PlanID:                 planID,
		ViewLevel:              input.ViewLevel,
		Generation:             genNumber,
		Selector:               selector,
		SecretCommitment:       input.SecretCommitment,
		Fingerprint:            ShareFingerprint(selector, input.SecretCommitment),
		EligibilityLinkEpochID: eligibilityEpoch,
		State:                  GenerationStateActive,
		ExpiresAt:              input.ExpiresAt.UTC(),
		IssuedAt:               now,
		Revision:               1,
	}
	if err := a.repo.InsertGeneration(ctx, tx, gen); err != nil {
		return ShareIssueResultV1{}, err
	}
	return ShareIssueResultV1{
		ShareID:    gen.ID,
		Selector:   gen.Selector,
		Generation: gen.Generation,
		ViewLevel:  gen.ViewLevel,
		State:      gen.State,
		ExpiresAt:  gen.ExpiresAt,
		Revision:   gen.Revision,
	}, nil
}

type RotateInput struct {
	ExpectedShareRevision int64
	NewSecretCommitment   ShareSecretCommitment
	ExpiresAt             time.Time
	ExpirySource          ExpirySourceV1
	PolicyVersion         string
}

func (a *Application) Rotate(
	ctx context.Context,
	scope store.AccountScope,
	key, planID, shareID string,
	input RotateInput,
) (ShareIssueResultV1, error) {
	planID, shareID = trimID(planID), trimID(shareID)
	if err := validateRotateInput(input); err != nil || planID == "" || shareID == "" || key == "" {
		if err == nil {
			err = validationError("rotate input invalid")
		}
		return ShareIssueResultV1{}, err
	}
	canonical, err := marshalRotateCanonical(
		input.ExpectedShareRevision, input.NewSecretCommitment,
		input.ExpiresAt, input.ExpirySource, input.PolicyVersion,
	)
	if err != nil {
		return ShareIssueResultV1{}, err
	}
	for attempt := 0; attempt < sourceRetryAttempts; attempt++ {
		response, err := a.idempotency.Execute(ctx, scope, idempotency.Request{
			Operation:        idempotency.OperationPlanShareRotate,
			Key:              key,
			ResourceIdentity: generationResource(planID, shareID),
			CanonicalBody:    canonical,
		}, func(tx store.TxAccountScope) (idempotency.StoredResponse, error) {
			result, err := a.rotateInScope(ctx, tx, planID, shareID, input)
			if err != nil {
				return idempotency.StoredResponse{}, err
			}
			body, err := json.Marshal(result)
			return idempotency.StoredResponse{Status: 200, Body: body}, err
		})
		if errors.Is(err, ErrSourceChangedRetry) {
			continue
		}
		if err != nil {
			return ShareIssueResultV1{}, err
		}
		return decodeIssueResult(response.Body)
	}
	return ShareIssueResultV1{}, ErrSourceChangedRetry
}

func (a *Application) rotateInScope(
	ctx context.Context,
	tx store.TxAccountScope,
	planID, shareID string,
	input RotateInput,
) (ShareIssueResultV1, error) {
	now := a.clock()
	current, err := a.repo.FindByID(ctx, tx, planID, shareID, false)
	if err != nil {
		return ShareIssueResultV1{}, err
	}
	window, err := a.repo.LoadExecutionWindow(ctx, tx, planID)
	if err != nil {
		return ShareIssueResultV1{}, err
	}
	if err := a.resolveExpiry(current.ViewLevel, input.ExpiresAt, input.ExpirySource, input.PolicyVersion, now, window); err != nil {
		return ShareIssueResultV1{}, err
	}
	var eligibilityEpoch *string
	if current.ViewLevel == ViewLevelFull {
		hint, err := preReadFullEligibility(ctx, tx, planID)
		if err != nil {
			return ShareIssueResultV1{}, err
		}
		if err := lockCRMOrderGraph(ctx, tx, hint); err != nil {
			return ShareIssueResultV1{}, err
		}
		elig, err := recheckFullEligibility(ctx, tx, hint)
		if err != nil {
			return ShareIssueResultV1{}, err
		}
		eligibilityEpoch = &elig.LinkEpochID
	} else {
		if err := lockIDs(ctx, tx, "shoot_plans", []string{planID}); err != nil {
			return ShareIssueResultV1{}, err
		}
	}
	_, _, archived, err := a.repo.LoadPlanMeta(ctx, tx, planID, true)
	if err != nil {
		return ShareIssueResultV1{}, err
	}
	if archived {
		return ShareIssueResultV1{}, ErrNotFound
	}
	locked, err := a.repo.FindByID(ctx, tx, planID, shareID, true)
	if err != nil {
		return ShareIssueResultV1{}, err
	}
	if locked.State != GenerationStateActive {
		return ShareIssueResultV1{}, ErrShareInvalidState
	}
	if locked.Revision != input.ExpectedShareRevision {
		return ShareIssueResultV1{}, ErrShareStale
	}
	if err := a.repo.MarkRotated(ctx, tx, planID, shareID, input.ExpectedShareRevision, now); err != nil {
		return ShareIssueResultV1{}, err
	}
	genNumber, err := a.repo.NextGenerationNumber(ctx, tx, planID, locked.ViewLevel)
	if err != nil {
		return ShareIssueResultV1{}, err
	}
	selector, err := a.newSelector()
	if err != nil {
		return ShareIssueResultV1{}, err
	}
	gen := ShareGeneration{
		ID:                     "sg_" + uuid.NewString(),
		PlanID:                 planID,
		ViewLevel:              locked.ViewLevel,
		Generation:             genNumber,
		Selector:               selector,
		SecretCommitment:       input.NewSecretCommitment,
		Fingerprint:            ShareFingerprint(selector, input.NewSecretCommitment),
		EligibilityLinkEpochID: eligibilityEpoch,
		State:                  GenerationStateActive,
		ExpiresAt:              input.ExpiresAt.UTC(),
		IssuedAt:               now,
		Revision:               1,
	}
	if locked.ViewLevel == ViewLevelProposal {
		gen.EligibilityLinkEpochID = nil
	}
	if err := a.repo.InsertGeneration(ctx, tx, gen); err != nil {
		return ShareIssueResultV1{}, err
	}
	return ShareIssueResultV1{
		ShareID:    gen.ID,
		Selector:   gen.Selector,
		Generation: gen.Generation,
		ViewLevel:  gen.ViewLevel,
		State:      gen.State,
		ExpiresAt:  gen.ExpiresAt,
		Revision:   gen.Revision,
	}, nil
}

type RevokeInput struct {
	ExpectedShareRevision int64
	PolicyVersion         string
}

func (a *Application) Revoke(
	ctx context.Context,
	scope store.AccountScope,
	key, planID, shareID string,
	input RevokeInput,
) (ShareRevokeResultV1, error) {
	planID, shareID = trimID(planID), trimID(shareID)
	if planID == "" || shareID == "" || key == "" || input.ExpectedShareRevision < 1 {
		return ShareRevokeResultV1{}, validationError("revoke input invalid")
	}
	if input.PolicyVersion == "" {
		input.PolicyVersion = a.policy.Version()
	}
	if input.PolicyVersion != a.policy.Version() {
		return ShareRevokeResultV1{}, validationError("unsupported policy version")
	}
	canonical, err := marshalRevokeCanonical(input.ExpectedShareRevision, input.PolicyVersion)
	if err != nil {
		return ShareRevokeResultV1{}, err
	}
	response, err := a.idempotency.Execute(ctx, scope, idempotency.Request{
		Operation:        idempotency.OperationPlanShareRevoke,
		Key:              key,
		ResourceIdentity: generationResource(planID, shareID),
		CanonicalBody:    canonical,
	}, func(tx store.TxAccountScope) (idempotency.StoredResponse, error) {
		result, err := a.revokeInScope(ctx, tx, planID, shareID, input)
		if err != nil {
			return idempotency.StoredResponse{}, err
		}
		body, err := json.Marshal(result)
		return idempotency.StoredResponse{Status: 200, Body: body}, err
	})
	if err != nil {
		return ShareRevokeResultV1{}, err
	}
	return decodeRevokeResult(response.Body)
}

func (a *Application) revokeInScope(
	ctx context.Context,
	tx store.TxAccountScope,
	planID, shareID string,
	input RevokeInput,
) (ShareRevokeResultV1, error) {
	now := a.clock()
	if err := lockIDs(ctx, tx, "shoot_plans", []string{planID}); err != nil {
		return ShareRevokeResultV1{}, err
	}
	current, err := a.repo.FindByID(ctx, tx, planID, shareID, true)
	if err != nil {
		return ShareRevokeResultV1{}, err
	}
	if current.State != GenerationStateActive {
		return ShareRevokeResultV1{}, ErrShareInvalidState
	}
	if current.Revision != input.ExpectedShareRevision {
		return ShareRevokeResultV1{}, ErrShareStale
	}
	revoked, err := a.repo.MarkRevoked(ctx, tx, planID, shareID, input.ExpectedShareRevision, now)
	if err != nil {
		return ShareRevokeResultV1{}, err
	}
	return ShareRevokeResultV1{
		ShareID:   revoked.ID,
		State:     revoked.State,
		Revision:  revoked.Revision,
		RevokedAt: *revoked.RevokedAt,
	}, nil
}

func (a *Application) resolveExpiry(
	view ViewLevel,
	expiresAt time.Time,
	source ExpirySourceV1,
	policyVersion string,
	commandNow time.Time,
	window *ExecutionWindowHint,
) error {
	if policyVersion != a.policy.Version() {
		return validationError("unsupported policy version")
	}
	switch source.Kind {
	case ExpirySourceExplicit:
		return a.policy.ValidateExplicit(expiresAt, commandNow)
	case ExpirySourceQuotedDefault:
		if source.DefaultQuote == nil {
			return validationError("quoted_default requires default_quote")
		}
		err := a.policy.ValidateQuotedDefault(expiresAt, *source.DefaultQuote, view, commandNow, windowForView(view, window))
		if errors.Is(err, ErrExpiryQuoteExpired) {
			return ExpiryQuoteExpiredError{Refreshed: a.policy.Project(view, commandNow, windowForView(view, window))}
		}
		return err
	default:
		return validationError("invalid expiry_source")
	}
}

func windowForView(view ViewLevel, window *ExecutionWindowHint) *ExecutionWindowHint {
	if view == ViewLevelFull {
		return window
	}
	return nil
}

func (a *Application) newSelector() (string, error) {
	raw := make([]byte, shareSelectorByteLen)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate share selector: %w", err)
	}
	return EncodeShareSelector(raw)
}

func validateIssueInput(input IssueInput) error {
	if input.ExpectedPlanRevision < 1 {
		return validationError("expected_plan_revision invalid")
	}
	if input.ViewLevel != ViewLevelProposal && input.ViewLevel != ViewLevelFull {
		return validationError("view_level invalid")
	}
	if input.SecretCommitment == (ShareSecretCommitment{}) {
		return validationError("secret_commitment required")
	}
	if input.ExpiresAt.IsZero() {
		return validationError("expires_at required")
	}
	if input.PolicyVersion == "" {
		return validationError("policy_version required")
	}
	return nil
}

func validateRotateInput(input RotateInput) error {
	if input.ExpectedShareRevision < 1 {
		return validationError("expected_share_revision invalid")
	}
	if input.NewSecretCommitment == (ShareSecretCommitment{}) {
		return validationError("new_secret_commitment required")
	}
	if input.ExpiresAt.IsZero() {
		return validationError("expires_at required")
	}
	if input.PolicyVersion == "" {
		return validationError("policy_version required")
	}
	return nil
}

func trimID(value string) string {
	return strings.TrimSpace(value)
}
