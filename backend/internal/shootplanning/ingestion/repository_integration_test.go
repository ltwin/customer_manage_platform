package ingestion_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning/ingestion"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestSessionRevisionScopeAndObservationClock(t *testing.T) {
	ctx := context.Background()
	container, err := tcpostgres.Run(ctx, "postgres:17-alpine", tcpostgres.WithDatabase("ingestion_test"), tcpostgres.WithUsername("ingestion_test"), tcpostgres.WithPassword("ingestion_test"), testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(60*time.Second)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(container) })
	url, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if err := db.CreateAccount(ctx, "ing-a", "hash"); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateAccount(ctx, "ing-b", "hash"); err != nil {
		t.Fatal(err)
	}
	scopeA := db.ScopeFor(auth.AccountContext{AccountID: "ing-a"})
	scopeB := db.ScopeFor(auth.AccountContext{AccountID: "ing-b"})
	plan, err := shootplanning.NewPostgresRepository().Create(ctx, scopeA, shootplanning.CreatePlanInput{Title: "摄取测试", Subject: "角色"})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ingestion.Parse(ingestion.ParseInput{SessionID: "pending", FirstSeenSessionRevision: 1, SourceText: "站姿正面"})
	if err != nil {
		t.Fatal(err)
	}
	clock := time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC)
	repo := ingestion.NewRepository(ingestion.WithClock(func() time.Time { return clock }))
	session, err := repo.CreateSession(ctx, scopeA, ingestion.CreateSessionInput{PlanID: plan.ID, ExpectedPlanRevision: plan.Revision, SourceText: "站姿正面", Parsed: parsed})
	if err != nil {
		t.Fatal(err)
	}
	if session.State != ingestion.SessionEditing || session.Revision != 1 {
		t.Fatalf("unexpected session: %+v", session)
	}
	_, err = repo.CreateSession(ctx, scopeA, ingestion.CreateSessionInput{PlanID: plan.ID, ExpectedPlanRevision: plan.Revision, SourceText: "站姿正面", Parsed: parsed})
	if !errors.Is(err, ingestion.ErrEditingSessionExists) {
		t.Fatalf("expected one editing session, got %v", err)
	}
	if _, err := repo.GetSession(ctx, scopeB, plan.ID, session.ID); !errors.Is(err, ingestion.ErrSessionNotFound) {
		t.Fatalf("cross-account get should be 404, got %v", err)
	}
	clock = clock.Add(1 * time.Second)
	preview, err := ingestion.Parse(ingestion.ParseInput{SessionID: session.ID, FirstSeenSessionRevision: 1, SourceText: "站姿正面\n\n侧身"})
	if err != nil {
		t.Fatal(err)
	}
	if err := scopeA.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		locked, err := repo.LockSession(ctx, tx, plan.ID, session.ID)
		if err != nil {
			return err
		}
		_, err = repo.ReplacePreviewInScope(ctx, tx, locked, "站姿正面\n\n侧身", preview)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(301 * time.Second)
	var observation ingestion.PlanBuildObservation
	var accepted bool
	if err := scopeA.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		var err error
		observation, accepted, err = repo.RecordBuildActivityInScope(ctx, tx, ingestion.ActivityFact{PlanID: plan.ID, SessionID: session.ID, TickID: "tick-1", Kind: "preview"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if !accepted || observation.ActiveSeconds != 0 {
		t.Fatalf("first activity should start at zero: %+v accepted=%v", observation, accepted)
	}
	clock = clock.Add(301 * time.Second)
	if err := scopeA.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		var err error
		observation, accepted, err = repo.RecordBuildActivityInScope(ctx, tx, ingestion.ActivityFact{PlanID: plan.ID, SessionID: session.ID, TickID: "tick-2", Kind: "preview"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if !accepted || observation.ActiveSeconds != 300 {
		t.Fatalf("301 seconds must cap at 300: %+v accepted=%v", observation, accepted)
	}
	if err := scopeA.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		var err error
		observation, accepted, err = repo.RecordBuildActivityInScope(ctx, tx, ingestion.ActivityFact{PlanID: plan.ID, SessionID: session.ID, TickID: "tick-2", Kind: "preview"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if accepted || observation.ActiveSeconds != 300 {
		t.Fatalf("duplicate tick must be a no-op: %+v accepted=%v", observation, accepted)
	}
	if err := scopeA.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		locked, err := repo.LockSession(ctx, tx, plan.ID, session.ID)
		if err != nil {
			return err
		}
		_, err = repo.TransitionInScope(ctx, tx, locked, ingestion.SessionAbandoned)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(time.Second)
	if err := scopeA.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		_, _, err := repo.AccumulateAndAbandonInScope(ctx, tx, ingestion.ActivityFact{PlanID: plan.ID, SessionID: session.ID, TickID: "tick-3"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if observation.PostTerminalIngestionCount != 0 {
		t.Fatal("unexpected pre-terminal post count")
	}
}
