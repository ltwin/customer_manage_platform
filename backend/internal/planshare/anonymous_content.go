package planshare

import (
	"context"
	"errors"
	"fmt"

	"github.com/samson/customer-manage-platform/backend/internal/planningmedia"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/platform/txcap"
)

// AnonymousContentDeps are sealed anonymous content dependencies.
type AnonymousContentDeps struct {
	Resolver ShareTokenResolver
	Runner   txcap.TransactionRunner[ShareTxScope]
	Media    *planningmedia.Application
}

// IssueAnonymousContentPermit rechecks grant/ref/binding and issues a
// process-local ContentPermit. Callers open the stream via planningmedia
// OpenDisplayWithPermit without holding AccountScope.
func (a *Application) IssueAnonymousContentPermit(
	ctx context.Context,
	deps AnonymousContentDeps,
	presentedToken, ref, checksum string,
) (planningmedia.ContentPermit, error) {
	if deps.Resolver == nil || deps.Runner == nil || deps.Media == nil {
		return planningmedia.ContentPermit{}, errors.New("anonymous content dependencies missing")
	}
	if ref == "" || checksum == "" {
		return planningmedia.ContentPermit{}, ErrShareNotFound
	}
	_, capability, err := deps.Resolver.Resolve(ctx, presentedToken)
	if err != nil {
		if errors.Is(err, ErrShareNotFound) || errors.Is(err, ErrInvalidShareToken) {
			return planningmedia.ContentPermit{}, ErrShareNotFound
		}
		return planningmedia.ContentPermit{}, err
	}
	var permit planningmedia.ContentPermit
	for attempt := 0; attempt < anonymousProjectionRetries; attempt++ {
		permit = planningmedia.ContentPermit{}
		err = deps.Runner.Run(ctx, capability, func(_ txcap.LedgerTxView, scope ShareTxScope) error {
			issued, runErr := a.issueContentPermitInScope(ctx, scope, ref, checksum)
			if runErr != nil {
				return runErr
			}
			permit = issued
			return nil
		})
		if err == nil {
			if finErr := deps.Media.FinalizeShareDisplayPermit(permit); finErr != nil {
				deps.Media.DiscardShareDisplayPermit(permit)
				return planningmedia.ContentPermit{}, finErr
			}
			return permit, nil
		}
		if permit.ID() != "" {
			deps.Media.DiscardShareDisplayPermit(permit)
		}
		var lost eligibilityLostError
		if errors.As(err, &lost) {
			_ = a.latchEligibilityInvalidated(ctx, AnonymousProjectionDeps{
				Resolver: deps.Resolver,
				Runner:   deps.Runner,
			}, capability)
			return planningmedia.ContentPermit{}, ErrShareNotFound
		}
		if store.IsSerializationFailure(err) {
			continue
		}
		if errors.Is(err, planningmedia.ErrAssetCorrupt) {
			return planningmedia.ContentPermit{}, err
		}
		if errors.Is(err, ErrShareNotFound) || errors.Is(err, ErrNotFound) {
			return planningmedia.ContentPermit{}, ErrShareNotFound
		}
		return planningmedia.ContentPermit{}, err
	}
	if store.IsSerializationFailure(err) {
		return planningmedia.ContentPermit{}, fmt.Errorf("anonymous content serialization retries exhausted: %w", err)
	}
	return planningmedia.ContentPermit{}, err
}

func (a *Application) issueContentPermitInScope(
	ctx context.Context,
	scope ShareTxScope,
	ref, checksum string,
) (planningmedia.ContentPermit, error) {
	now := a.clock()
	gen, err := scope.Generations().LoadByValidatedContext(ctx)
	if err != nil {
		return planningmedia.ContentPermit{}, ErrShareNotFound
	}
	eff, _, _ := DeriveEffectiveState(gen, false, now)
	if gen.State != GenerationStateActive || eff != EffectiveActive {
		return planningmedia.ContentPermit{}, ErrShareNotFound
	}
	if gen.ViewLevel == ViewLevelFull {
		hint, err := scope.Eligibility().PreReadShareEligibility(ctx, txcap.NewValidatedShareContext(gen.Fingerprint))
		if err != nil {
			return planningmedia.ContentPermit{}, err
		}
		if gen.EligibilityLinkEpochID != nil {
			hint.LinkEpochID = *gen.EligibilityLinkEpochID
		}
		elig, err := scope.Eligibility().LockAndRecheckShareEligibilityInShare(ctx, hint)
		if err != nil {
			return planningmedia.ContentPermit{}, err
		}
		if !elig.Eligible ||
			(gen.EligibilityLinkEpochID != nil && elig.LinkEpochID != *gen.EligibilityLinkEpochID) {
			return planningmedia.ContentPermit{}, eligibilityLostError{fingerprint: gen.Fingerprint}
		}
	}

	source, err := scope.PlanSources().ReadProposalSourceInShare(ctx, gen.PlanID)
	if err != nil {
		return planningmedia.ContentPermit{}, mapNotFound(err)
	}
	if source.Archived {
		return planningmedia.ContentPermit{}, ErrShareNotFound
	}

	row, err := scope.SharedAssetRefs().LoadActiveByRef(ctx, gen.ID, ref)
	if err != nil {
		return planningmedia.ContentPermit{}, mapNotFound(err)
	}
	if row.PlanID != gen.PlanID || row.DisplayChecksum != checksum {
		return planningmedia.ContentPermit{}, ErrShareNotFound
	}

	return scope.Media().IssueDisplayPermitInShare(ctx, SharedDisplayPermitRequest{
		PlanID:          row.PlanID,
		BindingID:       row.BindingID,
		AssetID:         row.AssetID,
		ExactGeneration: row.ExactGeneration,
		DisplayChecksum: row.DisplayChecksum,
	})
}
