package shootplanning

import (
	"errors"
	"fmt"
	"sort"
)

var ErrInvalidExecutionTimeline = errors.New("invalid execution timeline")

// ReplayCurrentOutcome validates and replays append-only result/void facts.
// Supersedes links are intentionally not consulted: server sequence is the only
// ordering source and cleared projects to an unset outcome.
func ReplayCurrentOutcome(facts []ExecutionFact) (CurrentOutcome, error) {
	ordered := append([]ExecutionFact(nil), facts...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Sequence < ordered[j].Sequence })

	results := make(map[string]ExecutionFact)
	voided := make(map[string]struct{})
	var previousSequence int64
	for i, fact := range ordered {
		if fact.ID == "" || fact.Sequence <= 0 || (i > 0 && fact.Sequence == previousSequence) {
			return CurrentOutcome{}, ErrInvalidExecutionTimeline
		}
		previousSequence = fact.Sequence
		switch fact.Kind {
		case ExecutionFactResult:
			if fact.Result != ShotResultCaptured && fact.Result != ShotResultSkipped && fact.Result != ShotResultCleared {
				return CurrentOutcome{}, ErrInvalidExecutionTimeline
			}
			if _, exists := results[fact.ID]; exists {
				return CurrentOutcome{}, ErrInvalidExecutionTimeline
			}
			results[fact.ID] = fact
		case ExecutionFactVoid:
			target, exists := results[fact.TargetEventID]
			if !exists || target.Sequence >= fact.Sequence {
				return CurrentOutcome{}, fmt.Errorf("%w: void target must be an earlier result", ErrInvalidExecutionTimeline)
			}
			if _, exists := voided[fact.TargetEventID]; exists {
				return CurrentOutcome{}, fmt.Errorf("%w: result already voided", ErrInvalidExecutionTimeline)
			}
			voided[fact.TargetEventID] = struct{}{}
		default:
			return CurrentOutcome{}, ErrInvalidExecutionTimeline
		}
	}

	var latest ExecutionFact
	found := false
	for _, fact := range ordered {
		if fact.Kind != ExecutionFactResult {
			continue
		}
		if _, isVoided := voided[fact.ID]; isVoided {
			continue
		}
		if !found || fact.Sequence > latest.Sequence {
			latest = fact
			found = true
		}
	}
	if !found || latest.Result == ShotResultCleared {
		return CurrentOutcome{}, nil
	}
	return CurrentOutcome{
		Set:         true,
		EventID:     latest.ID,
		Result:      latest.Result,
		SkipReason:  latest.SkipReason,
		CheckedAt:   latest.CheckedAt,
		CaptureMode: latest.CaptureMode,
	}, nil
}
