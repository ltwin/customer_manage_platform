package shootplanning

import "errors"

type PlanTransitionKind string

const (
	TransitionMarkReady PlanTransitionKind = "mark_ready"
	TransitionStart     PlanTransitionKind = "start"
	TransitionComplete  PlanTransitionKind = "complete"
	TransitionReopen    PlanTransitionKind = "reopen"
	TransitionArchive   PlanTransitionKind = "archive"
)

var (
	ErrInvalidPlanTransition = errors.New("invalid_plan_transition")
	ErrReadinessIncomplete   = errors.New("readiness_incomplete")
	ErrShotsIncomplete       = errors.New("shots_incomplete")
	ErrArchivedReadOnly      = errors.New("archived_read_only")
)

type TransitionFacts struct {
	RequiredReadinessComplete bool
	HasCurrentShots           bool
	AllCurrentShotsComplete   bool
}

func TransitionPlanState(from PlanStatus, transition PlanTransitionKind, facts TransitionFacts) (PlanStatus, error) {
	if from == PlanStatusArchived {
		return "", ErrArchivedReadOnly
	}

	switch transition {
	case TransitionMarkReady:
		if from != PlanStatusDraft {
			return "", ErrInvalidPlanTransition
		}
		if !facts.RequiredReadinessComplete {
			return "", ErrReadinessIncomplete
		}
		return PlanStatusReady, nil
	case TransitionStart:
		if from != PlanStatusReady {
			return "", ErrInvalidPlanTransition
		}
		return PlanStatusInProgress, nil
	case TransitionComplete:
		if from != PlanStatusInProgress {
			return "", ErrInvalidPlanTransition
		}
		if !facts.HasCurrentShots || !facts.AllCurrentShotsComplete {
			return "", ErrShotsIncomplete
		}
		return PlanStatusCompleted, nil
	case TransitionReopen:
		if from != PlanStatusCompleted {
			return "", ErrInvalidPlanTransition
		}
		return PlanStatusInProgress, nil
	case TransitionArchive:
		switch from {
		case PlanStatusDraft, PlanStatusReady, PlanStatusInProgress, PlanStatusCompleted:
			return PlanStatusArchived, nil
		default:
			return "", ErrInvalidPlanTransition
		}
	default:
		return "", ErrInvalidPlanTransition
	}
}

func StatusAfterStructuralMutation(status PlanStatus, requiredReadinessComplete bool) PlanStatus {
	if status == PlanStatusReady && !requiredReadinessComplete {
		return PlanStatusDraft
	}
	return status
}
