package planningreminder

import (
	"context"
	"errors"
	"testing"
)

func TestLockedFenceRejectsDuplicateAndDescendingFacts(t *testing.T) {
	t.Parallel()
	var generation int64
	view := BindTrustedFenceTxView(TrustedFenceOperations{
		Lock: func(context.Context) error { return nil },
		Reserve: func(context.Context, MutationFact) (int64, error) {
			generation++
			return generation, nil
		},
		Apply: func(context.Context, int64) error { return nil },
	})
	locked, err := view.LockCurrentAccount(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	fact := MutationFact{PlanID: "plan-b", MutationKind: MutationPlanArchived}
	if _, err := locked.ReserveGeneration(context.Background(), fact); err != nil {
		t.Fatal(err)
	}
	if _, err := locked.ReserveGeneration(context.Background(), fact); !errors.Is(err, ErrDuplicateFact) {
		t.Fatalf("duplicate fact error = %v", err)
	}
	if _, err := locked.ReserveGeneration(context.Background(), MutationFact{PlanID: "plan-a", MutationKind: MutationPlanArchived}); !errors.Is(err, ErrFactOrder) {
		t.Fatalf("descending fact error = %v", err)
	}
}

func TestMutationFactSourceEventClosedRule(t *testing.T) {
	t.Parallel()
	source := "assignment-event"
	tests := []struct {
		fact MutationFact
		ok   bool
	}{
		{fact: MutationFact{PlanID: "plan", MutationKind: MutationPlanArchived}, ok: true},
		{fact: MutationFact{PlanID: "plan", MutationKind: MutationPlanArchived, SourceEventID: &source}},
		{fact: MutationFact{PlanID: "plan", MutationKind: MutationAssignmentActivated, SourceEventID: &source}, ok: true},
		{fact: MutationFact{PlanID: "plan", MutationKind: MutationAssignmentRevoked}},
	}
	for _, tt := range tests {
		err := ValidateMutationFact(tt.fact)
		if (err == nil) != tt.ok {
			t.Fatalf("ValidateMutationFact(%+v) error=%v ok=%v", tt.fact, err, tt.ok)
		}
	}
}
