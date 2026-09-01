package ingestion_test

import (
	"context"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/idempotency"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store/storetest"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning/ingestion"
)

func TestMain(m *testing.M) { storetest.Main(m, store.MigrateUp) }

func TestApplicationCreatePreviewReplayAndAbandon(t *testing.T) {
	ctx := context.Background()
	url := storetest.NewURL(t)
	db, err := store.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if err := db.CreateAccount(ctx, "app-ing", "hash"); err != nil {
		t.Fatal(err)
	}
	scope := db.ScopeFor(auth.AccountContext{AccountID: "app-ing"})
	plan, err := shootplanning.NewPostgresRepository().Create(ctx, scope, shootplanning.CreatePlanInput{Title: "应用测试", Subject: "角色"})
	if err != nil {
		t.Fatal(err)
	}
	ingestionRepo := ingestion.NewRepository()
	app := ingestion.NewApplication(ingestionRepo, idempotency.NewExecutor())
	input := ingestion.CreateInput{PlanID: plan.ID, ExpectedPlanRevision: plan.Revision, SourceText: "站姿正面\n\n要带手套"}
	winner, err := app.CreateSession(ctx, scope, "ing-create-1", input)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := app.CreateSession(ctx, scope, "ing-create-1", input)
	if err != nil {
		t.Fatal(err)
	}
	if winner.ID != replay.ID || winner.Revision != replay.Revision {
		t.Fatalf("create replay changed response: winner=%+v replay=%+v", winner, replay)
	}
	preview, err := app.Preview(ctx, scope, "ing-preview-1", ingestion.PreviewInput{PlanID: plan.ID, SessionID: winner.ID, ExpectedSessionRevision: winner.Revision, SourceText: stringPtr("站姿正面\n\n要带手套\n\n侧身")})
	if err != nil {
		t.Fatal(err)
	}
	if preview.Revision != winner.Revision+1 {
		t.Fatalf("preview revision=%d", preview.Revision)
	}
	previewReplay, err := app.Preview(ctx, scope, "ing-preview-1", ingestion.PreviewInput{PlanID: plan.ID, SessionID: winner.ID, ExpectedSessionRevision: winner.Revision, SourceText: stringPtr("站姿正面\n\n要带手套\n\n侧身")})
	if err != nil {
		t.Fatal(err)
	}
	if previewReplay.Revision != preview.Revision {
		t.Fatalf("preview replay changed revision: %d vs %d", previewReplay.Revision, preview.Revision)
	}
	terminal, err := app.Transition(ctx, scope, "ing-abandon-1", ingestion.TransitionInput{PlanID: plan.ID, SessionID: winner.ID, ExpectedSessionRevision: preview.Revision, State: ingestion.SessionAbandoned})
	if err != nil {
		t.Fatal(err)
	}
	if terminal.State != ingestion.SessionAbandoned {
		t.Fatalf("transition state=%s", terminal.State)
	}
	var observation ingestion.PlanBuildObservation
	if err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		var err error
		observation, err = ingestionRepo.GetObservationInScope(ctx, tx, plan.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if observation.Outcome != ingestion.ObservationAbandoned {
		t.Fatalf("abandon must terminally close observation: %+v", observation)
	}
	activeBeforeReplay := observation.ActiveSeconds
	replayed, err := app.Transition(ctx, scope, "ing-abandon-1", ingestion.TransitionInput{PlanID: plan.ID, SessionID: winner.ID, ExpectedSessionRevision: preview.Revision, State: ingestion.SessionAbandoned})
	if err != nil || replayed.State != ingestion.SessionAbandoned {
		t.Fatalf("abandon replay should return stored response: result=%+v err=%v", replayed, err)
	}
	if err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		var err error
		observation, err = ingestionRepo.GetObservationInScope(ctx, tx, plan.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if observation.ActiveSeconds != activeBeforeReplay {
		t.Fatalf("abandon replay must not add active delta: before=%d after=%d", activeBeforeReplay, observation.ActiveSeconds)
	}
}

func TestCombinedCommitReferenceOnlyAndCoreRollbackBoundary(t *testing.T) {
	ctx := context.Background()
	url := storetest.NewURL(t)
	if err := store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if err := db.CreateAccount(ctx, "commit-ing", "hash"); err != nil {
		t.Fatal(err)
	}
	scope := db.ScopeFor(auth.AccountContext{AccountID: "commit-ing"})
	plan, err := shootplanning.NewPostgresRepository().Create(ctx, scope, shootplanning.CreatePlanInput{Title: "combined", Subject: "角色"})
	if err != nil {
		t.Fatal(err)
	}
	executor := idempotency.NewExecutor()
	ingestionRepo := ingestion.NewRepository()
	coreApp, err := shootplanning.NewApplication(shootplanning.NewPostgresRepository(), executor,
		shootplanning.WithIngestionRoutesEnabled(),
		shootplanning.WithPlanReadyObservationSink(ingestion.NewPlanReadyObservationAdapter(ingestionRepo)),
	)
	if err != nil {
		t.Fatal(err)
	}
	app := ingestion.NewApplication(ingestionRepo, executor, ingestion.WithCoreApplication(coreApp))
	session, err := app.CreateSession(ctx, scope, "ref-create-1", ingestion.CreateInput{PlanID: plan.ID, ExpectedPlanRevision: plan.Revision, SourceText: "https://example.com/reference"})
	if err != nil {
		t.Fatal(err)
	}
	ref := session.CandidateSnapshot.ReferenceLinkCandidates[0]
	commit, err := app.Commit(ctx, scope, "ref-commit-1", ingestion.IngestionCommitCanonicalV1{SessionID: session.ID, ExpectedSessionRevision: session.Revision, PlanID: plan.ID, ExpectedPlanRevision: plan.Revision, ReferenceLinkDecisions: []ingestion.ReferenceLinkDecision{{CandidateID: ref.CandidateID, Action: ingestion.DecisionKeep, RawURL: ref.RawURL, TargetKind: "plan"}}})
	if err != nil {
		t.Fatal(err)
	}
	if commit.Session.State != ingestion.SessionCommitted || len(commit.ReferenceLinks) != 1 || commit.PlanBatch != nil {
		t.Fatalf("reference-only commit unexpected: %+v", commit)
	}
	replay, err := app.Commit(ctx, scope, "ref-commit-1", ingestion.IngestionCommitCanonicalV1{SessionID: session.ID, ExpectedSessionRevision: session.Revision, PlanID: plan.ID, ExpectedPlanRevision: plan.Revision, ReferenceLinkDecisions: []ingestion.ReferenceLinkDecision{{CandidateID: ref.CandidateID, Action: ingestion.DecisionKeep, RawURL: ref.RawURL, TargetKind: "plan"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(replay.ReferenceLinks) != 1 || replay.ReferenceLinks[0].ID != commit.ReferenceLinks[0].ID {
		t.Fatalf("commit replay changed link: %+v vs %+v", replay, commit)
	}
	count, err := scope.Count(ctx, "shoot_plan_reference_links", "plan_id = $2", plan.ID)
	if err != nil || count != 1 {
		t.Fatalf("reference-only commit should write one link: count=%d err=%v", count, err)
	}
	second, err := app.CreateSession(ctx, scope, "core-create-1", ingestion.CreateInput{PlanID: plan.ID, ExpectedPlanRevision: plan.Revision, SourceText: "站姿正面"})
	if err != nil {
		t.Fatal(err)
	}
	shotCandidate := second.CandidateSnapshot.ContentCandidates[0]
	title := shotCandidate.Title
	coreCommit, err := app.Commit(ctx, scope, "core-commit-1", ingestion.IngestionCommitCanonicalV1{SessionID: second.ID, ExpectedSessionRevision: second.Revision, PlanID: plan.ID, ExpectedPlanRevision: plan.Revision, ShotDecisions: []ingestion.ShotDecision{{CandidateID: shotCandidate.CandidateID, Action: ingestion.DecisionKeep, ClientRef: "shot-client", Shot: shootplanning.ShotWrite{Title: &title}}}})
	if err != nil {
		t.Fatal(err)
	}
	if coreCommit.PlanBatch == nil || len(coreCommit.PlanBatch.CreatedIDs) != 1 {
		t.Fatalf("core batch branch missing: %+v", coreCommit)
	}
	third, err := app.CreateSession(ctx, scope, "rollback-create-1", ingestion.CreateInput{PlanID: plan.ID, ExpectedPlanRevision: coreCommit.PlanBatch.Revision, SourceText: "另一个镜头"})
	if err != nil {
		t.Fatal(err)
	}
	thirdShot := third.CandidateSnapshot.ContentCandidates[0]
	thirdTitle := thirdShot.Title
	_, err = app.Commit(ctx, scope, "rollback-commit-1", ingestion.IngestionCommitCanonicalV1{SessionID: third.ID, ExpectedSessionRevision: third.Revision, PlanID: plan.ID, ExpectedPlanRevision: coreCommit.PlanBatch.Revision, ShotDecisions: []ingestion.ShotDecision{{CandidateID: thirdShot.CandidateID, Action: ingestion.DecisionKeep, ClientRef: "rollback-shot", Shot: shootplanning.ShotWrite{Title: &thirdTitle}}}, ReferenceLinkDecisions: []ingestion.ReferenceLinkDecision{{CandidateID: "bad-target", Action: ingestion.DecisionKeep, RawURL: "https://example.com/bad", TargetKind: "shot", TargetClientOrIDRef: stringPtr("shot_missing")}}})
	if err == nil {
		t.Fatal("invalid reference target should fail combined commit")
	}
	shotCount, countErr := scope.Count(ctx, "shoot_plan_shots", "plan_id = $2 AND removed_at IS NULL", plan.ID)
	if countErr != nil || shotCount != 1 {
		t.Fatalf("failed combined commit left core shot: count=%d err=%v", shotCount, countErr)
	}
	ready, err := coreApp.TransitionPlan(ctx, scope, "core-ready-1", plan.ID, shootplanning.PlanTransition{ExpectedRevision: coreCommit.PlanBatch.Revision, Kind: shootplanning.TransitionMarkReady})
	if err != nil || ready.Status != shootplanning.PlanStatusReady {
		t.Fatalf("core ready transition failed: result=%+v err=%v", ready, err)
	}
	var readyObservation ingestion.PlanBuildObservation
	if err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		var err error
		readyObservation, err = ingestionRepo.GetObservationInScope(ctx, tx, plan.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if readyObservation.Outcome != ingestion.ObservationFirstReady || readyObservation.FirstReadyAt == nil {
		t.Fatalf("ready sink must terminally mark observation: %+v", readyObservation)
	}
	activeBeforeReadyReplay := readyObservation.ActiveSeconds
	replayedReady, err := coreApp.TransitionPlan(ctx, scope, "core-ready-1", plan.ID, shootplanning.PlanTransition{ExpectedRevision: coreCommit.PlanBatch.Revision, Kind: shootplanning.TransitionMarkReady})
	if err != nil || replayedReady.Status != shootplanning.PlanStatusReady {
		t.Fatalf("ready replay should return stored response: result=%+v err=%v", replayedReady, err)
	}
	if err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		var err error
		readyObservation, err = ingestionRepo.GetObservationInScope(ctx, tx, plan.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if readyObservation.ActiveSeconds != activeBeforeReadyReplay {
		t.Fatalf("ready replay must not add active delta: before=%d after=%d", activeBeforeReadyReplay, readyObservation.ActiveSeconds)
	}
}

func stringPtr(value string) *string { return &value }
