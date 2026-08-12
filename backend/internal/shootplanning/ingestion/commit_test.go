package ingestion_test

import (
	"errors"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/shootplanning"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning/ingestion"
)

func TestPrepareIngestionCommitCoreAndReferenceBranches(t *testing.T) {
	shotTitle := "站姿正面"
	prepared, err := ingestion.PrepareIngestionCommit(ingestion.IngestionCommitCanonicalV1{
		SessionID: "ing_s1", PlanID: "spl_1", ExpectedSessionRevision: 1, ExpectedPlanRevision: 2,
		ShotDecisions:          []ingestion.ShotDecision{{CandidateID: "c-shot", Action: ingestion.DecisionKeep, ClientRef: "shot-client", Shot: shootplanning.ShotWrite{Title: &shotTitle}}},
		ReferenceLinkDecisions: []ingestion.ReferenceLinkDecision{{CandidateID: "c-link", Action: ingestion.DecisionKeep, RawURL: "https://example.com/ref", TargetKind: "plan"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Core == nil || len(prepared.References) != 1 {
		t.Fatalf("expected core + reference preparations: %+v", prepared)
	}
	if err := ingestion.ValidateReferenceURL("ftp://example.com/ref"); !errors.Is(err, ingestion.ErrReferenceURL) {
		t.Fatalf("ftp should be rejected: %v", err)
	}
	if _, err := ingestion.PrepareIngestionCommit(ingestion.IngestionCommitCanonicalV1{SessionID: "ing_s1", PlanID: "spl_1", ExpectedSessionRevision: 1, ExpectedPlanRevision: 2, ShotDecisions: []ingestion.ShotDecision{{CandidateID: "discard", Action: ingestion.DecisionDiscard}}}); !errors.Is(err, ingestion.ErrNoKeptCandidates) {
		t.Fatalf("all discard should fail: %v", err)
	}
	referenceOnly, err := ingestion.PrepareIngestionCommit(ingestion.IngestionCommitCanonicalV1{SessionID: "ing_s1", PlanID: "spl_1", ExpectedSessionRevision: 1, ExpectedPlanRevision: 2, ReferenceLinkDecisions: []ingestion.ReferenceLinkDecision{{CandidateID: "c-link", Action: ingestion.DecisionKeep, RawURL: "https://example.com/ref", TargetKind: "plan"}}})
	if err != nil || referenceOnly.Core != nil {
		t.Fatalf("reference-only branch must not create core batch: %+v %v", referenceOnly, err)
	}
}
