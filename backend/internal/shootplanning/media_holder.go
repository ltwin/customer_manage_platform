package shootplanning

import (
	"context"
	"errors"

	"github.com/samson/customer-manage-platform/backend/internal/planningmedia"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// MediaHolderAuthorizer is the only bridge that interprets ShootPlan lifecycle
// for planningmedia. The media module never queries core planning tables.
type MediaHolderAuthorizer struct{ repo PostgresRepository }

func NewMediaHolderAuthorizer(repo PostgresRepository) MediaHolderAuthorizer {
	return MediaHolderAuthorizer{repo: repo}
}

func (a MediaHolderAuthorizer) AuthorizeMediaHolderInScope(ctx context.Context, tx store.TxAccountScope, req planningmedia.HolderRequest) (planningmedia.HolderProof, error) {
	plan, err := a.repo.LockPlan(ctx, tx, req.PlanID)
	if err != nil {
		return planningmedia.HolderProof{}, err
	}
	if req.ExpectedPlanRevision > 0 && plan.Revision != req.ExpectedPlanRevision {
		return planningmedia.HolderProof{}, ErrPlanRevisionConflict
	}
	if req.Mutation == planningmedia.MutationRead {
		if plan.Status == PlanStatusArchived {
			return planningmedia.HolderProof{}, planningmedia.ErrPlanArchived
		}
	} else {
		if plan.Status == PlanStatusArchived {
			return planningmedia.HolderProof{}, planningmedia.ErrPlanArchived
		}
		if plan.Status == PlanStatusCompleted {
			return planningmedia.HolderProof{}, planningmedia.ErrPlanReopenRequired
		}
	}
	switch req.Kind {
	case planningmedia.HolderPlan:
		if req.HolderID != req.PlanID {
			return planningmedia.HolderProof{}, ErrPlanNotFound
		}
	case planningmedia.HolderShot:
		var removedAt any
		err := tx.QueryRow(ctx, "shoot_plan_shots", "removed_at", "id = $2 AND plan_id = $3", req.HolderID, req.PlanID).Scan(&removedAt)
		if errors.Is(err, store.ErrNoRows) {
			return planningmedia.HolderProof{}, ErrShotNotFound
		}
		if err != nil {
			return planningmedia.HolderProof{}, err
		}
		if removedAt != nil && req.Mutation != planningmedia.MutationRelease {
			return planningmedia.HolderProof{}, ErrShotNotFound
		}
	default:
		return planningmedia.HolderProof{}, ErrShotNotFound
	}
	return planningmedia.NewHolderProof(tx.AccountID(), plan.ID, req.HolderID, req.Kind, plan.Revision, string(plan.Status)), nil
}
