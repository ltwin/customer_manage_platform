package ingestion

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/shootplanning"
)

func TestProjectStage1EvidenceFixedFixtureIsStableAndPassesThresholds(t *testing.T) {
	base := time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC)
	input := Stage1EvidenceInput{
		WindowID:      "fixture-window-1",
		BuildRevision: "fixture-build-1",
		SampleDecisions: []Stage1SampleDecision{
			{PlanID: "plan-05", Disposition: Stage1SampleInclude, OwnerReviewRef: "owner-review-05"},
			{PlanID: "plan-02", Disposition: Stage1SampleInclude, OwnerReviewRef: "owner-review-02"},
			{PlanID: "plan-04", Disposition: Stage1SampleInclude, OwnerReviewRef: "owner-review-04"},
			{PlanID: "plan-01", Disposition: Stage1SampleInclude, OwnerReviewRef: "owner-review-01"},
			{PlanID: "plan-03", Disposition: Stage1SampleInclude, OwnerReviewRef: "owner-review-03"},
		},
		RunSessions: []shootplanning.RunModeSession{
			{ID: "run-03-unknown", PlanID: "plan-03", CaptureMode: shootplanning.CaptureModeUnknown},
			{ID: "run-01", PlanID: "plan-01", CaptureMode: shootplanning.CaptureModeLive},
			{ID: "run-01-retry", PlanID: "plan-01", CaptureMode: shootplanning.CaptureModeLive},
			{ID: "run-02", PlanID: "plan-02", CaptureMode: shootplanning.CaptureModeLive},
			{ID: "run-04", PlanID: "plan-04", CaptureMode: shootplanning.CaptureModeUnknown},
			{ID: "run-05", PlanID: "plan-05", CaptureMode: shootplanning.CaptureModeLive},
		},
		Observations: []PlanBuildObservation{
			{PlanID: "plan-01", Outcome: ObservationFirstReady, ActiveSeconds: 240, IdleRuleVersion: 1, FirstIngestionAt: base, LastActivityAt: base},
			{PlanID: "plan-02", Outcome: ObservationFirstReady, ActiveSeconds: 480, IdleRuleVersion: 1, FirstIngestionAt: base, LastActivityAt: base},
			{PlanID: "plan-03", Outcome: ObservationFirstReady, ActiveSeconds: 600, IdleRuleVersion: 1, FirstIngestionAt: base, LastActivityAt: base},
			{PlanID: "plan-04", Outcome: ObservationFirstReady, ActiveSeconds: 900, IdleRuleVersion: 1, FirstIngestionAt: base, LastActivityAt: base},
			{PlanID: "plan-05", Outcome: ObservationAbandoned, ActiveSeconds: 300, IdleRuleVersion: 1, FirstIngestionAt: base, LastActivityAt: base},
		},
	}
	first, err := ProjectStage1Evidence(input)
	if err != nil {
		t.Fatal(err)
	}
	if first.Report.Status != "passed" || !first.Report.CohortComplete {
		t.Fatalf("fixture should pass without approval: %+v", first.Report)
	}
	if first.Report.G1.DistinctLivePlanCount != 3 || !first.Report.G1.ThresholdMet {
		t.Fatalf("unexpected G1: %+v", first.Report.G1)
	}
	if first.Report.G2.MedianActiveSeconds == nil || *first.Report.G2.MedianActiveSeconds != 540 {
		t.Fatalf("unexpected G2 median: %+v", first.Report.G2)
	}
	if first.Report.G2.AbandonRatio == nil || *first.Report.G2.AbandonRatio != 0.2 {
		t.Fatalf("unexpected abandon ratio: %+v", first.Report.G2)
	}
	if first.Report.ProjectionSHA256 == "" || first.SHA256 == "" {
		t.Fatal("stable hashes must be present")
	}

	input.RunSessions = append([]shootplanning.RunModeSession(nil), input.RunSessions[1:]...)
	input.Observations = append(append([]PlanBuildObservation(nil), input.Observations[3:]...), input.Observations[:3]...)
	second, err := ProjectStage1Evidence(input)
	if err != nil {
		t.Fatal(err)
	}
	if first.SHA256 != second.SHA256 || !bytes.Equal(first.CanonicalJSON, second.CanonicalJSON) {
		t.Fatalf("same facts must produce stable canonical hash: %s vs %s", first.SHA256, second.SHA256)
	}
	var decoded map[string]any
	if err := json.Unmarshal(first.CanonicalJSON, &decoded); err != nil {
		t.Fatal(err)
	}
	if _, approved := decoded["approval"]; approved {
		t.Fatal("projector must not create an approval decision")
	}
}

func TestProjectStage1EvidenceKeepsExcludedSamplesOutOfAutomaticPass(t *testing.T) {
	input := Stage1EvidenceInput{
		WindowID:      "window",
		BuildRevision: "build",
		SampleDecisions: []Stage1SampleDecision{
			{PlanID: "p1", Disposition: Stage1SampleInclude, OwnerReviewRef: "r1"},
			{PlanID: "p2", Disposition: Stage1SampleInclude, OwnerReviewRef: "r2"},
			{PlanID: "p3", Disposition: Stage1SampleInclude, OwnerReviewRef: "r3"},
			{PlanID: "p4", Disposition: Stage1SampleInclude, OwnerReviewRef: "r4"},
			{PlanID: "p5", Disposition: Stage1SampleExclude, ExclusionReason: "network_failure", OwnerReviewRef: "r5"},
		},
		Observations: []PlanBuildObservation{
			{PlanID: "p1", Outcome: ObservationFirstReady, ActiveSeconds: 1, IdleRuleVersion: 1},
			{PlanID: "p2", Outcome: ObservationFirstReady, ActiveSeconds: 1, IdleRuleVersion: 1},
			{PlanID: "p3", Outcome: ObservationFirstReady, ActiveSeconds: 1, IdleRuleVersion: 1},
			{PlanID: "p4", Outcome: ObservationFirstReady, ActiveSeconds: 1, IdleRuleVersion: 1},
		},
	}
	artifact, err := ProjectStage1Evidence(input)
	if err != nil {
		t.Fatal(err)
	}
	if artifact.Report.Status != "insufficient" || artifact.Report.CohortComplete {
		t.Fatalf("excluded sample must keep owner gate incomplete: %+v", artifact.Report)
	}
	if len(artifact.Report.Exclusions) != 1 || artifact.Report.Exclusions[0].Reason != "network_failure" {
		t.Fatalf("exclusion reason missing: %+v", artifact.Report.Exclusions)
	}
}
