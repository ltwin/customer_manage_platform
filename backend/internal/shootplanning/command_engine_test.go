package shootplanning

import (
	"encoding/json"
	"testing"
	"time"
)

func TestPlanCommandCanonicalUsesOpenAPITopLevelShape(t *testing.T) {
	t.Parallel()
	title := "  镜头一  "
	canonical, err := marshalPlanCommand(7, UpsertShotCommand{Shot: ShotWrite{
		Title: &title,
		Scene: Optional[string]{Specified: true, Null: true},
	}})
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(canonical, &body); err != nil {
		t.Fatal(err)
	}
	if _, exists := body["payload"]; exists {
		t.Fatalf("canonical command must not contain legacy payload wrapper: %s", canonical)
	}
	if body["operation"] != "upsert_shot" || body["expected_revision"] != float64(7) {
		t.Fatalf("unexpected command envelope: %s", canonical)
	}
	shot, ok := body["shot"].(map[string]any)
	if !ok || shot["title"] != "镜头一" {
		t.Fatalf("shot canonicalization failed: %s", canonical)
	}
	if value, exists := shot["scene"]; !exists || value != nil {
		t.Fatalf("explicit null must be preserved: %s", canonical)
	}
}

func TestExecutionFactJSONMatchesClosedResultAndVoidVariants(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	resultJSON, err := json.Marshal(ExecutionFact{
		Kind: ExecutionFactResult, ID: "event-1", PlanID: "plan-1", ShotID: "shot-1",
		Sequence: 1, PlanRevision: 2, Result: ShotResultCaptured, CheckedAt: now,
		CaptureMode: CaptureModeLive, Revision: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(resultJSON, &result); err != nil {
		t.Fatal(err)
	}
	if _, exists := result["voided_at"]; exists {
		t.Fatalf("result variant leaked void field: %s", resultJSON)
	}
	voidJSON, err := json.Marshal(ExecutionFact{
		Kind: ExecutionFactVoid, ID: "void-1", PlanID: "plan-1", ShotID: "shot-1",
		TargetEventID: "event-1", Sequence: 2, Reason: "误记", VoidedAt: now, Revision: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	var void map[string]any
	if err := json.Unmarshal(voidJSON, &void); err != nil {
		t.Fatal(err)
	}
	if _, exists := void["checked_at"]; exists {
		t.Fatalf("void variant leaked result field: %s", voidJSON)
	}
}

func TestPlanTransitionCanonicalUsesPayloadUnion(t *testing.T) {
	t.Parallel()
	acknowledgement := ArchiveAcknowledgementRegistryV1("planning-share-v1")
	canonical, err := marshalPlanTransition(PlanTransition{
		ExpectedRevision: 3, Kind: TransitionArchive, ArchiveAcknowledgement: &acknowledgement,
	})
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(canonical, &body); err != nil {
		t.Fatal(err)
	}
	if _, exists := body["acknowledgement"]; exists {
		t.Fatalf("transition must not contain legacy acknowledgement field: %s", canonical)
	}
	payload, ok := body["payload"].(map[string]any)
	if !ok || payload["version"] != "planning-share-v1" {
		t.Fatalf("archive payload mismatch: %s", canonical)
	}
}

func TestNormalizeShotWriteDistinguishesOmittedAndExplicitNull(t *testing.T) {
	t.Parallel()
	previous := "旧场景"
	current := normalizedShotWrite{scene: &previous}
	omitted, err := normalizeShotWrite(current, ShotWrite{})
	if err != nil {
		t.Fatal(err)
	}
	if omitted.scene == nil || *omitted.scene != previous {
		t.Fatalf("omitted field changed: %+v", omitted.scene)
	}
	cleared, err := normalizeShotWrite(current, ShotWrite{Scene: Optional[string]{Specified: true, Null: true}})
	if err != nil {
		t.Fatal(err)
	}
	if cleared.scene != nil {
		t.Fatalf("explicit null did not clear scene: %+v", cleared.scene)
	}
}

func TestPreparePlanBatchRejectsDuplicateClientRefsAndIsDeterministic(t *testing.T) {
	t.Parallel()
	title := "镜头"
	readinessTitle := "服装"
	input := PlanBatchInput{PlanID: "spl_test", ExpectedPlanRevision: 1, Candidates: []PlanBatchCandidate{
		CreateShotBatchCandidate{ClientRef: "shot-1", Shot: ShotWrite{Title: &title}},
		CreateReadinessBatchCandidate{ClientRef: "ready-1", Item: ReadinessWrite{Title: &readinessTitle}},
		LinkReadinessBatchCandidate{ShotRef: "shot-1", ReadinessRef: "ready-1"},
	}}
	first, err := PreparePlanBatch(input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := PreparePlanBatch(input)
	if err != nil {
		t.Fatal(err)
	}
	if string(first.canonical) != string(second.canonical) {
		t.Fatalf("canonical batch is not deterministic:\n%s\n%s", first.canonical, second.canonical)
	}
	title = "caller mutated after prepare"
	preparedShot := first.input.Candidates[0].(CreateShotBatchCandidate)
	if preparedShot.Shot.Title == nil || *preparedShot.Shot.Title != "镜头" {
		t.Fatalf("prepared batch retained caller-owned pointer: %+v", preparedShot.Shot.Title)
	}
	input.Candidates[1] = CreateReadinessBatchCandidate{ClientRef: "shot-1", Item: ReadinessWrite{Title: &readinessTitle}}
	if _, err := PreparePlanBatch(input); err == nil {
		t.Fatal("duplicate cross-kind client ref accepted")
	}
}
