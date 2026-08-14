package planshare

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/platform/idempotency"
	"github.com/samson/customer-manage-platform/backend/internal/platform/securitybudget"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/platform/txcap"
)

type FeedbackListQuery struct {
	Cursor string
	Limit  int
}

type CreatePlanFeedbackInput struct {
	ExpectedProjectionRevision int64
	AuthorDisplayName          string
	Content                    string
	PolicyVersion              string
}

type CreateShotFeedbackInput struct {
	ShotRef              string
	ExpectedShotRevision int64
	AuthorDisplayName    string
	Content              string
	PolicyVersion        string
}

type DispositionInput struct {
	ExpectedFeedbackRevision int64
	Disposition              FeedbackDisposition
}

type AnonymousFeedbackDeps struct {
	Resolver       ShareTokenResolver
	Runner         txcap.TransactionRunner[ShareTxScope]
	MutationBudget AnonymousMutationBudget
	MutationDigest MutationIPDigestResolver
	SourceIP       string
}

func (a *Application) ListFeedback(
	ctx context.Context,
	scope store.AccountScope,
	planID string,
	query FeedbackListQuery,
) (FeedbackManagementPageV1, error) {
	planID = trimID(planID)
	if planID == "" {
		return FeedbackManagementPageV1{}, validationError("plan id required")
	}
	cursorAt, cursorID, err := decodeFeedbackCursor(query.Cursor)
	if err != nil {
		return FeedbackManagementPageV1{}, err
	}
	limit := query.Limit
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 100 {
		return FeedbackManagementPageV1{}, validationError("limit out of range")
	}
	var page FeedbackManagementPageV1
	err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if _, _, _, err := a.repo.LoadPlanMeta(ctx, tx, planID, false); err != nil {
			return err
		}
		items, next, err := a.repo.ListFeedbackPage(ctx, tx, planID, limit, cursorAt, cursorID)
		if err != nil {
			return err
		}
		if items == nil {
			items = []FeedbackManagementItemV1{}
		}
		page.Items = items
		page.NextCursor = next
		return nil
	})
	return page, err
}

func (a *Application) SetFeedbackDisposition(
	ctx context.Context,
	scope store.AccountScope,
	key, planID, feedbackID string,
	input DispositionInput,
) (FeedbackDispositionResultV1, error) {
	planID = trimID(planID)
	feedbackID = trimID(feedbackID)
	if planID == "" || feedbackID == "" || key == "" {
		return FeedbackDispositionResultV1{}, validationError("disposition input invalid")
	}
	if input.Disposition != FeedbackDispositionAdopted && input.Disposition != FeedbackDispositionIgnored {
		return FeedbackDispositionResultV1{}, validationError("disposition must be adopted or ignored")
	}
	if input.ExpectedFeedbackRevision < 1 {
		return FeedbackDispositionResultV1{}, validationError("expected_feedback_revision required")
	}
	canonical, err := marshalDispositionCanonical(input.ExpectedFeedbackRevision, input.Disposition)
	if err != nil {
		return FeedbackDispositionResultV1{}, err
	}
	response, err := a.idempotency.Execute(ctx, scope, idempotency.Request{
		Operation:        idempotency.OperationPlanShareFeedbackDisposition,
		Key:              key,
		ResourceIdentity: FeedbackResource(planID, feedbackID),
		CanonicalBody:    canonical,
	}, func(tx store.TxAccountScope) (idempotency.StoredResponse, error) {
		result, err := a.dispositionInScope(ctx, tx, planID, feedbackID, input)
		if err != nil {
			return idempotency.StoredResponse{}, err
		}
		body, err := json.Marshal(result)
		return idempotency.StoredResponse{Status: 200, Body: body}, err
	})
	if err != nil {
		return FeedbackDispositionResultV1{}, err
	}
	var result FeedbackDispositionResultV1
	if err := json.Unmarshal(response.Body, &result); err != nil {
		return FeedbackDispositionResultV1{}, fmt.Errorf("decode feedback disposition replay: %w", err)
	}
	return result, nil
}

func (a *Application) dispositionInScope(
	ctx context.Context,
	tx store.TxAccountScope,
	planID, feedbackID string,
	input DispositionInput,
) (FeedbackDispositionResultV1, error) {
	if _, _, archived, err := a.repo.LoadPlanMeta(ctx, tx, planID, true); err != nil {
		return FeedbackDispositionResultV1{}, err
	} else if archived {
		return FeedbackDispositionResultV1{}, ErrNotFound
	}
	store := shareFeedbackStore{tx: tx}
	current, err := store.LockByID(ctx, planID, feedbackID)
	if err != nil {
		return FeedbackDispositionResultV1{}, err
	}
	if current.Revision != input.ExpectedFeedbackRevision {
		return FeedbackDispositionResultV1{}, ErrFeedbackStale
	}
	updated, err := store.UpdateDisposition(ctx, planID, feedbackID, input.ExpectedFeedbackRevision, input.Disposition, a.clock())
	if err != nil {
		return FeedbackDispositionResultV1{}, err
	}
	return FeedbackDispositionResultV1{
		FeedbackID:     updated.ID,
		Disposition:    updated.Disposition,
		Revision:       updated.Revision,
		DeepLinkTarget: deepLinkFromRow(updated),
	}, nil
}

func (a *Application) CreateAnonymousPlanFeedback(
	ctx context.Context,
	deps AnonymousFeedbackDeps,
	key, presentedToken string,
	input CreatePlanFeedbackInput,
) (FeedbackCreateResultV1, error) {
	return a.createAnonymousFeedback(ctx, deps, key, presentedToken, anonymousFeedbackCommand{
		operation:       idempotency.OperationPlanShareFeedbackPlanCreate,
		planInput:       &input,
		requireFull:     false,
		targetKind:      FeedbackTargetPlan,
		observationKind: "plan_feedback",
	})
}

func (a *Application) CreateAnonymousShotFeedback(
	ctx context.Context,
	deps AnonymousFeedbackDeps,
	key, presentedToken, shotRef string,
	input CreateShotFeedbackInput,
) (FeedbackCreateResultV1, error) {
	input.ShotRef = trimID(shotRef)
	if input.ShotRef == "" {
		return FeedbackCreateResultV1{}, validationError("shot ref required")
	}
	return a.createAnonymousFeedback(ctx, deps, key, presentedToken, anonymousFeedbackCommand{
		operation:       idempotency.OperationPlanShareFeedbackShotCreate,
		shotInput:       &input,
		requireFull:     true,
		targetKind:      FeedbackTargetShot,
		observationKind: "shot_feedback",
	})
}

type anonymousFeedbackCommand struct {
	operation       idempotency.Operation
	planInput       *CreatePlanFeedbackInput
	shotInput       *CreateShotFeedbackInput
	requireFull     bool
	targetKind      FeedbackTargetKind
	observationKind string
}

func (a *Application) createAnonymousFeedback(
	ctx context.Context,
	deps AnonymousFeedbackDeps,
	key, presentedToken string,
	cmd anonymousFeedbackCommand,
) (FeedbackCreateResultV1, error) {
	if deps.Resolver == nil || deps.Runner == nil {
		return FeedbackCreateResultV1{}, errors.New("anonymous feedback dependencies missing")
	}
	if key == "" || presentedToken == "" {
		return FeedbackCreateResultV1{}, validationError("anonymous feedback input invalid")
	}
	now := a.clock()
	if deps.MutationBudget == nil || deps.MutationDigest == nil || deps.SourceIP == "" {
		return FeedbackCreateResultV1{}, securitybudget.ErrUnavailable
	}
	// Outer IP always counts before resolver.
	if _, err := ConsumeAnonymousMutationOuterIP(ctx, deps.MutationBudget, deps.MutationDigest, deps.SourceIP, now); err != nil {
		return FeedbackCreateResultV1{}, err
	}
	_, capability, err := deps.Resolver.Resolve(ctx, presentedToken)
	if err != nil {
		return FeedbackCreateResultV1{}, ErrShareNotFound
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
		return FeedbackCreateResultV1{}, mapAnonErr(err)
	}
	eff, _, _ := DeriveEffectiveState(gen, false, now)
	if gen.State != GenerationStateActive || eff != EffectiveActive {
		return FeedbackCreateResultV1{}, ErrShareNotFound
	}
	if cmd.requireFull && gen.ViewLevel != ViewLevelFull {
		return FeedbackCreateResultV1{}, ErrShareNotFound
	}

	if _, err := ConsumeAnonymousMutationOuterAfterGrant(
		ctx, deps.MutationBudget, deps.MutationDigest, deps.SourceIP, gen.ID, now,
	); err != nil {
		return FeedbackCreateResultV1{}, err
	}

	author, content, policyVersion, expectedRev, resource, typedBody, err := a.normalizeAnonymousFeedback(cmd, gen)
	if err != nil {
		return FeedbackCreateResultV1{}, err
	}
	frameBytes, frame, err := BuildCanonicalAnonymousMutationFrameV1Bytes(string(cmd.operation), resource, typedBody)
	if err != nil {
		return FeedbackCreateResultV1{}, err
	}
	fingerprint := ExactFrameFingerprint(frameBytes)
	keyDigest := IdempotencyKeyDigest(key)

	var hasAdmission bool
	err = deps.Runner.Run(ctx, capability, func(_ txcap.LedgerTxView, scope ShareTxScope) error {
		ok, lookupErr := scope.ReplayAdmissions().HasExactUnexpired(
			ctx, gen.ID, string(cmd.operation), keyDigest, fingerprint, now,
		)
		if lookupErr != nil {
			return lookupErr
		}
		hasAdmission = ok
		return nil
	})
	if err != nil {
		return FeedbackCreateResultV1{}, mapAnonErr(err)
	}
	if !hasAdmission {
		if _, err := ConsumeAnonymousMutationBusinessQuota(
			ctx, deps.MutationBudget, deps.MutationDigest, deps.SourceIP, gen.ID, now,
		); err != nil {
			return FeedbackCreateResultV1{}, err
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
				Operation:        cmd.operation,
				Key:              key,
				ResourceIdentity: resource,
				CanonicalBody:    typedBody,
				ExactFrameHash:   frame.FrameHash,
			},
			func(scope ShareTxScope) (idempotency.StoredResponse, error) {
				result, cbErr := a.createFeedbackInShareScope(
					ctx, scope, gen, cmd, author, content, policyVersion, expectedRev, frame,
				)
				if cbErr != nil {
					return idempotency.StoredResponse{}, cbErr
				}
				body, marshalErr := json.Marshal(result)
				return idempotency.StoredResponse{Status: 201, Body: body}, marshalErr
			},
		)
		if errors.Is(execErr, ErrSourceChangedRetry) {
			continue
		}
		if execErr != nil {
			return FeedbackCreateResultV1{}, mapAnonErr(execErr)
		}
		var result FeedbackCreateResultV1
		if err := json.Unmarshal(response.Body, &result); err != nil {
			return FeedbackCreateResultV1{}, fmt.Errorf("decode feedback create replay: %w", err)
		}
		return result, nil
	}
	return FeedbackCreateResultV1{}, ErrSourceChangedRetry
}

func (a *Application) normalizeAnonymousFeedback(
	cmd anonymousFeedbackCommand,
	gen ShareGeneration,
) (author, content, policyVersion string, expectedRev int64, resource idempotency.ResourceIdentity, typedBody []byte, err error) {
	policyVersion = PolicyVersionV1
	switch {
	case cmd.planInput != nil:
		if cmd.planInput.PolicyVersion != "" && cmd.planInput.PolicyVersion != PolicyVersionV1 {
			return "", "", "", 0, idempotency.ResourceIdentity{}, nil, validationError("policy_version invalid")
		}
		author, err = normalizeAuthorDisplayName(cmd.planInput.AuthorDisplayName)
		if err != nil {
			return "", "", "", 0, idempotency.ResourceIdentity{}, nil, err
		}
		if err = rejectUnknownControlRunes(author); err != nil {
			return "", "", "", 0, idempotency.ResourceIdentity{}, nil, err
		}
		content, err = normalizeFeedbackContent(cmd.planInput.Content)
		if err != nil {
			return "", "", "", 0, idempotency.ResourceIdentity{}, nil, err
		}
		if err = rejectUnknownControlRunes(content); err != nil {
			return "", "", "", 0, idempotency.ResourceIdentity{}, nil, err
		}
		if cmd.planInput.ExpectedProjectionRevision < 1 {
			return "", "", "", 0, idempotency.ResourceIdentity{}, nil, validationError("expected_projection_revision required")
		}
		expectedRev = cmd.planInput.ExpectedProjectionRevision
		resource = AnonymousPlanFeedbackResource(gen.PlanID, gen.ID)
		typedBody, err = marshalCanonicalJSON(planFeedbackCanonicalV1{
			AuthorDisplayName:          author,
			Content:                    content,
			ExpectedProjectionRevision: expectedRev,
			PolicyVersion:              policyVersion,
		})
	case cmd.shotInput != nil:
		if cmd.shotInput.PolicyVersion != "" && cmd.shotInput.PolicyVersion != PolicyVersionV1 {
			return "", "", "", 0, idempotency.ResourceIdentity{}, nil, validationError("policy_version invalid")
		}
		author, err = normalizeAuthorDisplayName(cmd.shotInput.AuthorDisplayName)
		if err != nil {
			return "", "", "", 0, idempotency.ResourceIdentity{}, nil, err
		}
		if err = rejectUnknownControlRunes(author); err != nil {
			return "", "", "", 0, idempotency.ResourceIdentity{}, nil, err
		}
		content, err = normalizeFeedbackContent(cmd.shotInput.Content)
		if err != nil {
			return "", "", "", 0, idempotency.ResourceIdentity{}, nil, err
		}
		if err = rejectUnknownControlRunes(content); err != nil {
			return "", "", "", 0, idempotency.ResourceIdentity{}, nil, err
		}
		if cmd.shotInput.ExpectedShotRevision < 1 {
			return "", "", "", 0, idempotency.ResourceIdentity{}, nil, validationError("expected_shot_revision required")
		}
		expectedRev = cmd.shotInput.ExpectedShotRevision
		resource = AnonymousShotFeedbackResource(gen.PlanID, gen.ID, cmd.shotInput.ShotRef)
		typedBody, err = marshalCanonicalJSON(shotFeedbackCanonicalV1{
			AuthorDisplayName:    author,
			Content:              content,
			ExpectedShotRevision: expectedRev,
			PolicyVersion:        policyVersion,
		})
	default:
		err = validationError("feedback command incomplete")
	}
	return author, content, policyVersion, expectedRev, resource, typedBody, err
}

func (a *Application) createFeedbackInShareScope(
	ctx context.Context,
	scope ShareTxScope,
	gen ShareGeneration,
	cmd anonymousFeedbackCommand,
	author, content, policyVersion string,
	expectedRev int64,
	frame CanonicalAnonymousMutationFrameV1,
) (FeedbackCreateResultV1, error) {
	now := a.clock()
	concrete, ok := scope.(shareTxScope)
	if !ok {
		return FeedbackCreateResultV1{}, errors.New("share scope type unsupported")
	}
	current, err := scope.Generations().LoadByValidatedContext(ctx)
	if err != nil {
		return FeedbackCreateResultV1{}, ErrShareNotFound
	}
	if current.ID != gen.ID || current.State != GenerationStateActive {
		return FeedbackCreateResultV1{}, ErrShareNotFound
	}
	eff, _, _ := DeriveEffectiveState(current, false, now)
	if eff != EffectiveActive {
		return FeedbackCreateResultV1{}, ErrShareNotFound
	}

	var elig ShareEligibility
	var revision int64
	var archived bool
	if current.ViewLevel == ViewLevelFull {
		hint, err := preReadFullEligibility(ctx, concrete.tx, current.PlanID)
		if err != nil {
			if errors.Is(err, ErrFullViewNotEligible) {
				return FeedbackCreateResultV1{}, ErrShareNotFound
			}
			return FeedbackCreateResultV1{}, err
		}
		if current.EligibilityLinkEpochID != nil && hint.LinkEpochID != *current.EligibilityLinkEpochID {
			return FeedbackCreateResultV1{}, ErrShareNotFound
		}
		if err := lockCRMOrderGraph(ctx, concrete.tx, hint); err != nil {
			return FeedbackCreateResultV1{}, mapAnonErr(err)
		}
		rechecked, err := recheckFullEligibility(ctx, concrete.tx, hint)
		if err != nil {
			if errors.Is(err, ErrSourceChangedRetry) {
				return FeedbackCreateResultV1{}, err
			}
			if errors.Is(err, ErrFullViewNotEligible) {
				return FeedbackCreateResultV1{}, ErrShareNotFound
			}
			return FeedbackCreateResultV1{}, err
		}
		elig = ShareEligibility{
			LinkEpochID: rechecked.LinkEpochID,
			Eligible:    rechecked.Eligible,
			ObservedAt:  now,
		}
		if rechecked.SlotID != "" {
			slot := rechecked.SlotID
			elig.SlotID = &slot
		}
		_, revision, archived, err = (PostgresRepository{}).LoadPlanMeta(ctx, concrete.tx, current.PlanID, false)
		if err != nil {
			return FeedbackCreateResultV1{}, mapAnonErr(err)
		}
	} else {
		_, revision, archived, err = (PostgresRepository{}).LoadPlanMeta(ctx, concrete.tx, current.PlanID, true)
		if err != nil {
			return FeedbackCreateResultV1{}, mapAnonErr(err)
		}
	}
	if archived {
		return FeedbackCreateResultV1{}, ErrShareNotFound
	}
	var lockedGenID string
	if err := concrete.tx.QueryRowForUpdate(ctx, "share_generations", "id",
		"fingerprint = $2 AND state = 'active'", current.Fingerprint).Scan(&lockedGenID); err != nil {
		return FeedbackCreateResultV1{}, ErrShareNotFound
	}

	var targetID *string
	var deepLinkKind DeepLinkKind
	var deepLinkShotID *string
	switch cmd.targetKind {
	case FeedbackTargetPlan:
		if revision != expectedRev {
			return FeedbackCreateResultV1{}, ErrPlanRevisionConflict
		}
		deepLinkKind = DeepLinkFeedbackSection
	case FeedbackTargetShot:
		shotID := cmd.shotInput.ShotRef
		if err := lockShotAndCheckRevision(ctx, concrete.tx, current.PlanID, shotID, expectedRev); err != nil {
			return FeedbackCreateResultV1{}, err
		}
		targetID = &shotID
		deepLinkKind = DeepLinkShot
		deepLinkShotID = &shotID
	default:
		return FeedbackCreateResultV1{}, validationError("target kind invalid")
	}

	feedbackID := "sfb_" + uuid.NewString()
	row, err := scope.Feedback().Insert(ctx, ShareFeedbackInsert{
		ID:                feedbackID,
		PlanID:            current.PlanID,
		TokenGenerationID: current.ID,
		TargetKind:        cmd.targetKind,
		TargetID:          targetID,
		TargetRevision:    expectedRev,
		AuthorDisplayName: author,
		Content:           content,
		DeepLinkKind:      deepLinkKind,
		DeepLinkShotID:    deepLinkShotID,
		CreatedAt:         now,
	})
	if err != nil {
		return FeedbackCreateResultV1{}, err
	}
	if err := scope.Observations().EnsureFeedback(ctx, FeedbackObservationInput{
		PlanID:            current.PlanID,
		TokenGenerationID: current.ID,
		Kind:              cmd.observationKind,
		SourceFactID:      row.ID,
		PolicyVersion:     policyVersion,
		OccurredAt:        now,
		SlotID:            elig.SlotID,
		WindowRevision:    elig.WindowRevision,
	}); err != nil {
		return FeedbackCreateResultV1{}, err
	}
	if err := scope.ReplayAdmissions().RecordForCurrentLedgerClaim(ctx, frame); err != nil {
		return FeedbackCreateResultV1{}, err
	}
	return FeedbackCreateResultV1{
		FeedbackID: row.ID,
		TargetKind: row.TargetKind,
		TargetRef:  row.TargetID,
		Revision:   row.Revision,
		CreatedAt:  row.CreatedAt,
	}, nil
}

func lockShotAndCheckRevision(
	ctx context.Context,
	tx store.TxAccountScope,
	planID, shotID string,
	expectedRev int64,
) error {
	var revision int64
	err := tx.QueryRowForUpdate(ctx, "shoot_plan_shots", "revision",
		"plan_id = $2 AND id = $3 AND removed_at IS NULL", planID, shotID).Scan(&revision)
	if errors.Is(err, store.ErrNoRows) {
		return ErrShareNotFound
	}
	if err != nil {
		return err
	}
	if revision != expectedRev {
		return ErrShotRevisionConflict
	}
	return nil
}

func mapAnonErr(err error) error {
	if errors.Is(err, ErrNotFound) || errors.Is(err, store.ErrNoRows) {
		return ErrShareNotFound
	}
	return err
}

func deepLinkFromRow(row ShareFeedbackRow) FeedbackDeepLinkTargetV1 {
	return FeedbackDeepLinkTargetV1{Kind: row.DeepLinkKind, ShotID: row.DeepLinkShotID}
}

type planFeedbackCanonicalV1 struct {
	AuthorDisplayName          string `json:"author_display_name"`
	Content                    string `json:"content"`
	ExpectedProjectionRevision int64  `json:"expected_projection_revision"`
	PolicyVersion              string `json:"policy_version"`
}

type shotFeedbackCanonicalV1 struct {
	AuthorDisplayName    string `json:"author_display_name"`
	Content              string `json:"content"`
	ExpectedShotRevision int64  `json:"expected_shot_revision"`
	PolicyVersion        string `json:"policy_version"`
}

type dispositionCanonicalV1 struct {
	Disposition              FeedbackDisposition `json:"disposition"`
	ExpectedFeedbackRevision int64               `json:"expected_feedback_revision"`
}

func marshalDispositionCanonical(expected int64, disposition FeedbackDisposition) ([]byte, error) {
	return marshalCanonicalJSON(dispositionCanonicalV1{
		Disposition:              disposition,
		ExpectedFeedbackRevision: expected,
	})
}
