package shootplanning

import (
	"errors"
	"testing"
)

func TestTransitionPlanState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		from       PlanStatus
		transition PlanTransitionKind
		facts      TransitionFacts
		want       PlanStatus
		wantErr    error
	}{
		{name: "draft to ready", from: PlanStatusDraft, transition: TransitionMarkReady, facts: TransitionFacts{RequiredReadinessComplete: true}, want: PlanStatusReady},
		{name: "required readiness blocks ready", from: PlanStatusDraft, transition: TransitionMarkReady, wantErr: ErrReadinessIncomplete},
		{name: "ready to in progress", from: PlanStatusReady, transition: TransitionStart, want: PlanStatusInProgress},
		{name: "complete requires current shots", from: PlanStatusInProgress, transition: TransitionComplete, facts: TransitionFacts{AllCurrentShotsComplete: true}, wantErr: ErrShotsIncomplete},
		{name: "complete requires all outcomes", from: PlanStatusInProgress, transition: TransitionComplete, facts: TransitionFacts{HasCurrentShots: true}, wantErr: ErrShotsIncomplete},
		{name: "in progress to complete", from: PlanStatusInProgress, transition: TransitionComplete, facts: TransitionFacts{HasCurrentShots: true, AllCurrentShotsComplete: true}, want: PlanStatusCompleted},
		{name: "completed to reopen", from: PlanStatusCompleted, transition: TransitionReopen, want: PlanStatusInProgress},
		{name: "draft can archive", from: PlanStatusDraft, transition: TransitionArchive, want: PlanStatusArchived},
		{name: "archived is immutable", from: PlanStatusArchived, transition: TransitionReopen, wantErr: ErrArchivedReadOnly},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := TransitionPlanState(tt.from, tt.transition, tt.facts)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("TransitionPlanState() error = %v, want %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("TransitionPlanState() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStructuralMutationDemotesInvalidReadyPlan(t *testing.T) {
	t.Parallel()

	if got := StatusAfterStructuralMutation(PlanStatusReady, false); got != PlanStatusDraft {
		t.Fatalf("StatusAfterStructuralMutation() = %q, want draft", got)
	}
	if got := StatusAfterStructuralMutation(PlanStatusInProgress, false); got != PlanStatusInProgress {
		t.Fatalf("in_progress must not be demoted, got %q", got)
	}
}
