package planshare_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/customer"
	"github.com/samson/customer-manage-platform/backend/internal/order"
	"github.com/samson/customer-manage-platform/backend/internal/planshare"
	"github.com/samson/customer-manage-platform/backend/internal/platform/httpapi"
	"github.com/samson/customer-manage-platform/backend/internal/platform/idempotency"
	"github.com/samson/customer-manage-platform/backend/internal/platform/securitybudget"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning/crm"
)

func TestAnonymousShareProjectionUniform404AndBoundaries(t *testing.T) {
	ctx := context.Background()
	db := openShareStore(t)
	scope := createShareAccount(t, db, "share-anon-acct")
	clock := newAtomicClock(time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC))
	app := planshare.NewApplication(idempotency.NewExecutor(), planshare.WithClock(clock.Now))
	repo := shootplanning.NewPostgresRepository()
	engine := crm.NewEngine()
	customers := customer.NewService(customer.NewPostgresRepository())
	orders := order.NewService(order.NewPostgresRepository())

	plan, err := repo.Create(ctx, scope, shootplanning.CreatePlanInput{Title: "匿名投影", Subject: "主体"})
	if err != nil {
		t.Fatal(err)
	}
	cust := createShareCustomer(t, customers, scope, "阿晚")
	title := "和服"
	status := order.StatusScheduled
	linked, err := orders.Create(ctx, scope, order.CreateInput{
		CreationMode: order.CreationModeNew,
		CustomerID:   cust.ID,
		Title:        &title,
		Status:       &status,
	})
	if err != nil {
		t.Fatal(err)
	}
	outcome := applyShareCRM(t, scope, engine, plan.ID, plan.Revision, crm.Command{
		Kind: crm.KindLinkOrder, OrderID: &linked.ID,
	})

	proposalSecret := mustSecret(t)
	proposal := issueOK(t, app, scope, "anon-issue-proposal", plan.ID, planshare.IssueInput{
		ExpectedPlanRevision: outcome.Revision,
		ViewLevel:            planshare.ViewLevelProposal,
		SecretCommitment:     planshare.CommitShareSecret(proposalSecret),
		ExpiresAt:            clock.Now().Add(2 * time.Hour),
		ExpirySource:         planshare.ExpirySourceV1{Kind: planshare.ExpirySourceExplicit},
		PolicyVersion:        planshare.PolicyVersionV1,
	})
	proposalWire, err := planshare.ComposeShareTokenWire(proposal.Selector, proposalSecret)
	if err != nil {
		t.Fatal(err)
	}

	fullSecret := mustSecret(t)
	full := issueOK(t, app, scope, "anon-issue-full", plan.ID, planshare.IssueInput{
		ExpectedPlanRevision: outcome.Revision,
		ViewLevel:            planshare.ViewLevelFull,
		SecretCommitment:     planshare.CommitShareSecret(fullSecret),
		ExpiresAt:            clock.Now().Add(3 * time.Hour),
		ExpirySource:         planshare.ExpirySourceV1{Kind: planshare.ExpirySourceExplicit},
		PolicyVersion:        planshare.PolicyVersionV1,
	})
	fullWire, err := planshare.ComposeShareTokenWire(full.Selector, fullSecret)
	if err != nil {
		t.Fatal(err)
	}

	resolver := planshare.NewResolver(planshare.StoreTokenLookup{Store: db}, planshare.DefaultTrustedCapabilityFactory{})
	runner := planshare.StoreShareTransactionRunner{Store: db}
	deps := planshare.AnonymousProjectionDeps{Resolver: resolver, Runner: runner}

	probe := planshare.NewCountingQueryProbe()
	proposalProj, err := app.GetAnonymousProjection(planshare.WithQueryProbe(ctx, probe), deps, proposalWire)
	if err != nil || proposalProj.Proposal == nil || proposalProj.Full != nil {
		t.Fatalf("proposal projection = %+v err=%v", proposalProj, err)
	}
	for _, table := range []string{"shoot_plan_shots", "share_assignment_offers", "orders", "customers"} {
		if probe.Count(table) != 0 {
			t.Fatalf("proposal must not query %s (count=%d)", table, probe.Count(table))
		}
	}
	raw, _ := json.Marshal(proposalProj.Proposal)
	body := string(raw)
	for _, leak := range []string{"price", "cost", "labor", "notes", "secret_commitment"} {
		if strings.Contains(strings.ToLower(body), leak) {
			t.Fatalf("proposal leaked %q: %s", leak, body)
		}
	}

	fullProj, err := app.GetAnonymousProjection(ctx, deps, fullWire)
	if err != nil || fullProj.Full == nil {
		t.Fatalf("full projection = %+v err=%v", fullProj, err)
	}
	if fullProj.Full.Shots == nil || fullProj.Full.AssignmentOpportunities == nil {
		t.Fatal("full shots/opportunities must be non-nil")
	}

	var openCount int64
	if err := scope.ScalarAggregate(ctx, "share_interaction_observations", store.AggregateCount, "id",
		"token_generation_id = $2 AND kind = 'full_open'", full.ShareID).Scan(&openCount); err != nil {
		t.Fatal(err)
	}
	if openCount != 1 {
		t.Fatalf("full_open count=%d", openCount)
	}
	if _, err := app.GetAnonymousProjection(ctx, deps, fullWire); err != nil {
		t.Fatal(err)
	}
	if err := scope.ScalarAggregate(ctx, "share_interaction_observations", store.AggregateCount, "id",
		"token_generation_id = $2 AND kind = 'full_open'", full.ShareID).Scan(&openCount); err != nil {
		t.Fatal(err)
	}
	if openCount != 1 {
		t.Fatalf("full_open must stay unique, count=%d", openCount)
	}

	for _, token := range []string{"sp1.bad", proposalWire + "x"} {
		if _, err := app.GetAnonymousProjection(ctx, deps, token); err != planshare.ErrShareNotFound {
			t.Fatalf("invalid token err=%v", err)
		}
	}

	// eligibility lost → uniform 404
	unlinked := applyShareCRM(t, scope, engine, plan.ID, outcome.Revision, crm.Command{Kind: crm.KindUnlinkOrder})
	if _, err := app.GetAnonymousProjection(ctx, deps, fullWire); err != planshare.ErrShareNotFound {
		t.Fatalf("ineligible full err=%v", err)
	}

	revokeOK(t, app, scope, "anon-revoke-proposal", plan.ID, proposal.ShareID, planshare.RevokeInput{
		ExpectedShareRevision: proposal.Revision,
		PolicyVersion:         planshare.PolicyVersionV1,
	})
	if _, err := app.GetAnonymousProjection(ctx, deps, proposalWire); err != planshare.ErrShareNotFound {
		t.Fatalf("revoked proposal err=%v", err)
	}

	// expired active generation → uniform 404
	expSecret := mustSecret(t)
	expIssued := issueOK(t, app, scope, "anon-issue-exp", plan.ID, planshare.IssueInput{
		ExpectedPlanRevision: unlinked.Revision,
		ViewLevel:            planshare.ViewLevelProposal,
		SecretCommitment:     planshare.CommitShareSecret(expSecret),
		ExpiresAt:            clock.Now().Add(time.Hour),
		ExpirySource:         planshare.ExpirySourceV1{Kind: planshare.ExpirySourceExplicit},
		PolicyVersion:        planshare.PolicyVersionV1,
	})
	expWire, err := planshare.ComposeShareTokenWire(expIssued.Selector, expSecret)
	if err != nil {
		t.Fatal(err)
	}
	clock.Set(clock.Now().Add(2 * time.Hour))
	if _, err := app.GetAnonymousProjection(ctx, deps, expWire); err != planshare.ErrShareNotFound {
		t.Fatalf("expired proposal err=%v", err)
	}
}

func TestAnonymousConcurrentGETIdempotent(t *testing.T) {
	ctx := context.Background()
	db := openShareStore(t)
	scope := createShareAccount(t, db, "share-anon-conc")
	clock := newAtomicClock(time.Date(2026, 8, 14, 13, 0, 0, 0, time.UTC))
	app := planshare.NewApplication(idempotency.NewExecutor(), planshare.WithClock(clock.Now))
	repo := shootplanning.NewPostgresRepository()
	plan, err := repo.Create(ctx, scope, shootplanning.CreatePlanInput{Title: "并发", Subject: "主体"})
	if err != nil {
		t.Fatal(err)
	}
	secret := mustSecret(t)
	issued := issueOK(t, app, scope, "conc-issue", plan.ID, planshare.IssueInput{
		ExpectedPlanRevision: plan.Revision,
		ViewLevel:            planshare.ViewLevelProposal,
		SecretCommitment:     planshare.CommitShareSecret(secret),
		ExpiresAt:            clock.Now().Add(2 * time.Hour),
		ExpirySource:         planshare.ExpirySourceV1{Kind: planshare.ExpirySourceExplicit},
		PolicyVersion:        planshare.PolicyVersionV1,
	})
	wire, err := planshare.ComposeShareTokenWire(issued.Selector, secret)
	if err != nil {
		t.Fatal(err)
	}
	deps := planshare.AnonymousProjectionDeps{
		Resolver: planshare.NewResolver(planshare.StoreTokenLookup{Store: db}, planshare.DefaultTrustedCapabilityFactory{}),
		Runner:   planshare.StoreShareTransactionRunner{Store: db},
	}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := app.GetAnonymousProjection(ctx, deps, wire)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestAnonymousHTTPHeadersAndPathCanary(t *testing.T) {
	ctx := context.Background()
	db := openShareStore(t)
	scope := createShareAccount(t, db, "share-anon-http")
	clock := newAtomicClock(time.Date(2026, 8, 14, 14, 0, 0, 0, time.UTC))
	app := planshare.NewApplication(idempotency.NewExecutor(), planshare.WithClock(clock.Now))
	repo := shootplanning.NewPostgresRepository()
	plan, err := repo.Create(ctx, scope, shootplanning.CreatePlanInput{Title: "HTTP", Subject: "主体"})
	if err != nil {
		t.Fatal(err)
	}
	secret := mustSecret(t)
	issued := issueOK(t, app, scope, "http-issue", plan.ID, planshare.IssueInput{
		ExpectedPlanRevision: plan.Revision,
		ViewLevel:            planshare.ViewLevelProposal,
		SecretCommitment:     planshare.CommitShareSecret(secret),
		ExpiresAt:            clock.Now().Add(2 * time.Hour),
		ExpirySource:         planshare.ExpirySourceV1{Kind: planshare.ExpirySourceExplicit},
		PolicyVersion:        planshare.PolicyVersionV1,
	})
	wire, err := planshare.ComposeShareTokenWire(issued.Selector, secret)
	if err != nil {
		t.Fatal(err)
	}
	resolver := planshare.NewResolver(planshare.StoreTokenLookup{Store: db}, planshare.DefaultTrustedCapabilityFactory{})
	runner := planshare.StoreShareTransactionRunner{Store: db}
	logBuf := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(logBuf, nil))
	router := httpapi.NewRouter(httpapi.RouterDeps{
		Logger: logger,
		DB:     db,
		AnonymousShare: httpapi.AnonymousShareDeps{
			App:        app,
			Resolver:   resolver,
			Runner:     runner,
			ReadBudget: fakeReadBudget{allowed: true},
			IPDigest:   fakeIPDigest{},
			Now:        clock.Now,
		},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/shared/plans/"+wire, nil)
	req.RemoteAddr = "203.0.113.10:443"
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Cache-Control") != "private, no-store" ||
		rec.Header().Get("Referrer-Policy") != "no-referrer" ||
		!strings.Contains(rec.Header().Get("Content-Security-Policy"), "default-src 'none'") {
		t.Fatalf("headers=%v", rec.Header())
	}
	logged := logBuf.String()
	if strings.Contains(logged, wire) {
		t.Fatalf("token canary in logs: %s", logged)
	}
}

func TestFailClosedIPDigestWithoutKey(t *testing.T) {
	resolver := planshare.FailClosedIPDigestResolver{}
	_, err := resolver.ResolveTokenIP(context.Background(), "1.2.3.4", "fp", time.Now())
	if err != securitybudget.ErrUnavailable {
		t.Fatalf("err=%v", err)
	}
}

func TestAnonymousProjectionArchiveIndependent404(t *testing.T) {
	ctx := context.Background()
	db := openShareStore(t)
	scope := createShareAccount(t, db, "share-anon-archive")
	clock := newAtomicClock(time.Date(2026, 8, 14, 14, 0, 0, 0, time.UTC))
	app := planshare.NewApplication(idempotency.NewExecutor(), planshare.WithClock(clock.Now))
	repo := shootplanning.NewPostgresRepository()
	plan, err := repo.Create(ctx, scope, shootplanning.CreatePlanInput{Title: "归档投影", Subject: "主体"})
	if err != nil {
		t.Fatal(err)
	}
	secret := mustSecret(t)
	issued := issueOK(t, app, scope, "archive-proj-issue", plan.ID, planshare.IssueInput{
		ExpectedPlanRevision: plan.Revision,
		ViewLevel:            planshare.ViewLevelProposal,
		SecretCommitment:     planshare.CommitShareSecret(secret),
		ExpiresAt:            clock.Now().Add(2 * time.Hour),
		ExpirySource:         planshare.ExpirySourceV1{Kind: planshare.ExpirySourceExplicit},
		PolicyVersion:        planshare.PolicyVersionV1,
	})
	wire, err := planshare.ComposeShareTokenWire(issued.Selector, secret)
	if err != nil {
		t.Fatal(err)
	}
	deps := planshare.AnonymousProjectionDeps{
		Resolver: planshare.NewResolver(planshare.StoreTokenLookup{Store: db}, planshare.DefaultTrustedCapabilityFactory{}),
		Runner:   planshare.StoreShareTransactionRunner{Store: db},
	}
	if _, err := app.GetAnonymousProjection(ctx, deps, wire); err != nil {
		t.Fatalf("pre-archive projection: %v", err)
	}
	if _, err := scope.Update(ctx, "shoot_plans", "archived_at = $2", "id = $3", clock.Now(), plan.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := app.GetAnonymousProjection(ctx, deps, wire); err != planshare.ErrShareNotFound {
		t.Fatalf("archived projection err=%v", err)
	}
}

func mustSecret(t *testing.T) []byte {
	t.Helper()
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		t.Fatal(err)
	}
	return secret
}

type fakeReadBudget struct{ allowed bool }

func (f fakeReadBudget) ConsumeReadOuter(
	context.Context,
	securitybudget.DigestCandidates,
	time.Time,
) (time.Duration, bool, error) {
	if !f.allowed {
		return time.Minute, false, nil
	}
	return 0, true, nil
}

type fakeIPDigest struct{}

func (fakeIPDigest) ResolveTokenIP(
	context.Context, string, string, time.Time,
) (securitybudget.DigestCandidates, error) {
	sum := sha256.Sum256([]byte("fake-ip-digest"))
	return securitybudget.DigestCandidates{
		Current: securitybudget.DigestIdentity{
			Version: securitybudget.DigestVersionV1,
			HMAC:    sum[:],
		},
	}, nil
}
