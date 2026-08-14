package planshare

import (
	"context"
	"errors"
	"fmt"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/platform/txcap"
)

// AnonymousProjection is the closed anonymous GET result.
type AnonymousProjection struct {
	Proposal *SharedPlanProposalV1
	Full     *SharedPlanFullV1
}

// AnonymousProjectionDeps are sealed anonymous GET dependencies.
type AnonymousProjectionDeps struct {
	Resolver ShareTokenResolver
	Runner   txcap.TransactionRunner[ShareTxScope]
}

type eligibilityLostError struct {
	fingerprint string
}

func (e eligibilityLostError) Error() string { return "share eligibility lost" }
func (e eligibilityLostError) Unwrap() error { return ErrShareNotFound }

// GetAnonymousProjection resolves a presented token and returns the exact DTO.
func (a *Application) GetAnonymousProjection(
	ctx context.Context,
	deps AnonymousProjectionDeps,
	presentedToken string,
) (AnonymousProjection, error) {
	if deps.Resolver == nil || deps.Runner == nil {
		return AnonymousProjection{}, errors.New("anonymous projection dependencies missing")
	}
	validated, capability, err := deps.Resolver.Resolve(ctx, presentedToken)
	if err != nil {
		if errors.Is(err, ErrShareNotFound) || errors.Is(err, ErrInvalidShareToken) {
			return AnonymousProjection{}, ErrShareNotFound
		}
		return AnonymousProjection{}, err
	}
	_ = validated
	var projection AnonymousProjection
	for attempt := 0; attempt < anonymousProjectionRetries; attempt++ {
		err = deps.Runner.Run(ctx, capability, func(_ txcap.LedgerTxView, scope ShareTxScope) error {
			built, runErr := a.projectAnonymousInScope(ctx, scope)
			if runErr != nil {
				return runErr
			}
			projection = built
			return nil
		})
		if err == nil {
			return projection, nil
		}
		var lost eligibilityLostError
		if errors.As(err, &lost) {
			_ = a.latchEligibilityInvalidated(ctx, deps, capability)
			return AnonymousProjection{}, ErrShareNotFound
		}
		if store.IsSerializationFailure(err) {
			continue
		}
		if errors.Is(err, ErrShareNotFound) || errors.Is(err, ErrNotFound) {
			return AnonymousProjection{}, ErrShareNotFound
		}
		return AnonymousProjection{}, err
	}
	if store.IsSerializationFailure(err) {
		return AnonymousProjection{}, fmt.Errorf("anonymous projection serialization retries exhausted: %w", err)
	}
	return AnonymousProjection{}, err
}

func (a *Application) latchEligibilityInvalidated(
	ctx context.Context,
	deps AnonymousProjectionDeps,
	capability txcap.ShareTransactionCapability,
) error {
	return deps.Runner.Run(ctx, capability, func(_ txcap.LedgerTxView, scope ShareTxScope) error {
		return scope.Generations().LatchEligibilityInvalidated(ctx, a.clock())
	})
}

func (a *Application) projectAnonymousInScope(
	ctx context.Context,
	scope ShareTxScope,
) (AnonymousProjection, error) {
	now := a.clock()
	gen, err := scope.Generations().LoadByValidatedContext(ctx)
	if err != nil {
		return AnonymousProjection{}, ErrShareNotFound
	}
	eff, _, _ := DeriveEffectiveState(gen, false, now)
	if gen.State != GenerationStateActive || eff != EffectiveActive {
		return AnonymousProjection{}, ErrShareNotFound
	}
	var elig ShareEligibility
	if gen.ViewLevel == ViewLevelFull {
		hint, err := scope.Eligibility().PreReadShareEligibility(ctx, txcap.NewValidatedShareContext(gen.Fingerprint))
		if err != nil {
			return AnonymousProjection{}, err
		}
		if gen.EligibilityLinkEpochID != nil {
			hint.LinkEpochID = *gen.EligibilityLinkEpochID
		}
		elig, err = scope.Eligibility().LockAndRecheckShareEligibilityInShare(ctx, hint)
		if err != nil {
			return AnonymousProjection{}, err
		}
		if !elig.Eligible ||
			(gen.EligibilityLinkEpochID != nil && elig.LinkEpochID != *gen.EligibilityLinkEpochID) {
			return AnonymousProjection{}, eligibilityLostError{fingerprint: gen.Fingerprint}
		}
	}
	switch gen.ViewLevel {
	case ViewLevelProposal:
		source, err := scope.PlanSources().ReadProposalSourceInShare(ctx, gen.PlanID)
		if err != nil {
			return AnonymousProjection{}, mapNotFound(err)
		}
		if source.Archived {
			return AnonymousProjection{}, ErrShareNotFound
		}
		moodboard, err := a.upsertMoodboard(ctx, scope, gen, source.PlanID)
		if err != nil {
			return AnonymousProjection{}, err
		}
		if err := scope.Generations().TouchFirstOpenedAt(ctx, now); err != nil {
			return AnonymousProjection{}, err
		}
		proposal := SharedPlanProposalV1{
			ViewLevel:          ViewLevelProposal,
			Title:              source.Title,
			CreativeBrief:      source.CreativeBrief,
			PublicWindow:       source.PublicWindow,
			PublicScale:        source.PublicScale,
			Moodboard:          moodboard,
			ProjectionRevision: source.ProjectionRevision,
		}
		return AnonymousProjection{Proposal: &proposal}, nil
	case ViewLevelFull:
		source, err := scope.PlanSources().ReadFullSourceInShare(ctx, gen.PlanID)
		if err != nil {
			return AnonymousProjection{}, mapNotFound(err)
		}
		if source.Archived {
			return AnonymousProjection{}, ErrShareNotFound
		}
		moodboard, err := a.upsertMoodboard(ctx, scope, gen, source.PlanID)
		if err != nil {
			return AnonymousProjection{}, err
		}
		opportunities := make([]SharedAssignmentOpportunityV1, 0, len(source.Readiness)+8)
		for _, item := range source.Readiness {
			opp := SharedAssignmentOpportunityV1{
				OfferID:                    "readiness:" + item.ReadinessItemID,
				AssignmentKind:             "readiness",
				ReadinessItemID:            &item.ReadinessItemID,
				Content:                    item.Content,
				PreparationLeadDaysPreview: item.PreparationLeadDaysPreview,
				TargetRevision:             item.TargetRevision,
			}
			if active, err := loadActiveAssignmentSummary(ctx, scope, gen.PlanID, AssignmentKindReadiness, &item.ReadinessItemID, nil); err != nil {
				return AnonymousProjection{}, err
			} else if active != nil {
				opp.ActiveAssignment = active
			}
			opportunities = append(opportunities, opp)
		}
		offers, err := listOpenOnSiteOpportunities(ctx, scope, gen.PlanID)
		if err != nil {
			return AnonymousProjection{}, err
		}
		opportunities = append(opportunities, offers...)
		if err := scope.Observations().EnsureFullOpen(ctx, FullOpenObservationInput{
			PlanID:                  gen.PlanID,
			TokenGenerationID:       gen.ID,
			SlotID:                  elig.SlotID,
			ExecutionWindowRevision: elig.WindowRevision,
			PolicyVersion:           a.policy.Version(),
			OccurredAt:              now,
		}); err != nil {
			return AnonymousProjection{}, err
		}
		if err := scope.Generations().TouchFirstOpenedAt(ctx, now); err != nil {
			return AnonymousProjection{}, err
		}
		proposal := SharedPlanProposalV1{
			ViewLevel:          ViewLevelFull,
			Title:              source.Title,
			CreativeBrief:      source.CreativeBrief,
			PublicWindow:       source.PublicWindow,
			PublicScale:        source.PublicScale,
			Moodboard:          moodboard,
			ProjectionRevision: source.ProjectionRevision,
		}
		shots := source.Shots
		if shots == nil {
			shots = []SharedShotV1{}
		}
		full := SharedPlanFullV1{
			SharedPlanProposalV1:    proposal,
			Shots:                   shots,
			AssignmentOpportunities: opportunities,
		}
		return AnonymousProjection{Full: &full}, nil
	default:
		return AnonymousProjection{}, ErrShareNotFound
	}
}

func (a *Application) upsertMoodboard(
	ctx context.Context,
	scope ShareTxScope,
	gen ShareGeneration,
	planID string,
) ([]SharedMoodboardItemV1, error) {
	bindings, err := scope.Media().ListMoodboardBindingsInShare(ctx, planID)
	if err != nil {
		return nil, err
	}
	items := make([]SharedMoodboardItemV1, 0, len(bindings))
	for _, binding := range bindings {
		item, err := scope.SharedAssetRefs().UpsertMoodboardRef(ctx, SharedAssetRefUpsert{
			PlanID:            planID,
			TokenGenerationID: gen.ID,
			Binding:           binding,
		})
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func listOpenOnSiteOpportunities(
	ctx context.Context,
	scope ShareTxScope,
	planID string,
) ([]SharedAssignmentOpportunityV1, error) {
	concrete, ok := scope.(shareTxScope)
	if !ok {
		return nil, errors.New("share scope type unsupported")
	}
	rows, err := concrete.tx.QueryPage(
		ctx,
		"share_assignment_offers",
		"id, content, revision",
		"plan_id = $2 AND state = 'open'",
		[]store.OrderBy{{Column: "created_at", Desc: true}, {Column: "id", Desc: true}},
		100,
		0,
		planID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]SharedAssignmentOpportunityV1, 0)
	for rows.Next() {
		var offerID, content string
		var revision int64
		if err := rows.Scan(&offerID, &content, &revision); err != nil {
			return nil, err
		}
		id := offerID
		opp := SharedAssignmentOpportunityV1{
			OfferID:        offerID,
			AssignmentKind: "on_site_support",
			Content:        content,
			TargetRevision: revision,
		}
		if active, err := loadActiveAssignmentSummary(ctx, scope, planID, AssignmentKindOnSiteSupport, nil, &id); err != nil {
			return nil, err
		} else if active != nil {
			opp.ActiveAssignment = active
		}
		out = append(out, opp)
	}
	return out, rows.Err()
}

func loadActiveAssignmentSummary(
	ctx context.Context,
	scope ShareTxScope,
	planID string,
	kind AssignmentKind,
	readinessItemID, offerID *string,
) (*SharedActiveAssignmentV1, error) {
	row, err := scope.Assignments().FindActiveByTarget(ctx, planID, kind, readinessItemID, offerID)
	if errors.Is(err, ErrAssignmentNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &SharedActiveAssignmentV1{
		ID:                   row.ID,
		ClaimedByDisplayName: row.ClaimedByDisplayName,
		Revision:             row.Revision,
	}, nil
}

func mapNotFound(err error) error {
	if errors.Is(err, ErrNotFound) {
		return ErrShareNotFound
	}
	return err
}
