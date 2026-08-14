package planshare_test

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/customer"
	"github.com/samson/customer-manage-platform/backend/internal/order"
	"github.com/samson/customer-manage-platform/backend/internal/planshare"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/idempotency"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning/crm"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestBearerShareIssueRotateRevokeAndPolicy(t *testing.T) {
	ctx := context.Background()
	db := openShareStore(t)
	scope := createShareAccount(t, db, "share-bearer-acct")
	clock := newAtomicClock(time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC))
	app := planshare.NewApplication(idempotency.NewExecutor(), planshare.WithClock(clock.Now))
	repo := shootplanning.NewPostgresRepository()
	engine := crm.NewEngine()
	customers := customer.NewService(customer.NewPostgresRepository())
	orders := order.NewService(order.NewPostgresRepository())

	plan, err := repo.Create(ctx, scope, shootplanning.CreatePlanInput{Title: "分享策划", Subject: "主体"})
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
	epochBefore := deref(outcome.Connection.LinkEpochID)

	proposal := issueOK(t, app, scope, "issue-proposal", plan.ID, planshare.IssueInput{
		ExpectedPlanRevision: outcome.Revision,
		ViewLevel:            planshare.ViewLevelProposal,
		SecretCommitment:     mustCommitment(t),
		ExpiresAt:            clock.Now().Add(2 * time.Hour),
		ExpirySource:         planshare.ExpirySourceV1{Kind: planshare.ExpirySourceExplicit},
		PolicyVersion:        planshare.PolicyVersionV1,
	})
	full := issueOK(t, app, scope, "issue-full", plan.ID, planshare.IssueInput{
		ExpectedPlanRevision: outcome.Revision,
		ViewLevel:            planshare.ViewLevelFull,
		SecretCommitment:     mustCommitment(t),
		ExpiresAt:            clock.Now().Add(3 * time.Hour),
		ExpirySource:         planshare.ExpirySourceV1{Kind: planshare.ExpirySourceExplicit},
		PolicyVersion:        planshare.PolicyVersionV1,
	})
	if proposal.Selector == "" || full.Selector == "" || proposal.Selector == full.Selector {
		t.Fatalf("selectors invalid: %+v %+v", proposal, full)
	}

	_, err = app.Issue(ctx, scope, "issue-proposal-again", plan.ID, planshare.IssueInput{
		ExpectedPlanRevision: outcome.Revision,
		ViewLevel:            planshare.ViewLevelProposal,
		SecretCommitment:     mustCommitment(t),
		ExpiresAt:            clock.Now().Add(2 * time.Hour),
		ExpirySource:         planshare.ExpirySourceV1{Kind: planshare.ExpirySourceExplicit},
		PolicyVersion:        planshare.PolicyVersionV1,
	})
	if !errors.Is(err, planshare.ErrShareGenerationExists) {
		t.Fatalf("same-view issue err = %v", err)
	}

	proj, err := app.GetManagementProjection(ctx, scope, plan.ID, planshare.ManagementQuery{OfferLimit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(proj.ShareViews) != 2 ||
		proj.ShareViews[0].ViewLevel != planshare.ViewLevelProposal ||
		proj.ShareViews[1].ViewLevel != planshare.ViewLevelFull {
		t.Fatalf("share views = %+v", proj.ShareViews)
	}
	if proj.OnSiteOffers == nil {
		t.Fatal("offers must be non-nil empty slice")
	}
	raw, err := json.Marshal(proj)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	for _, leak := range []string{proposal.Selector, full.Selector, "secret_commitment", "sp1."} {
		if leak != "" && strings.Contains(body, leak) {
			t.Fatalf("projection leaked %q: %s", leak, body)
		}
	}

	// quoted default: t0 read → t1 mutate within TTL; exact replay keeps selector.
	t0proj, err := app.GetManagementProjection(ctx, scope, plan.ID, planshare.ManagementQuery{})
	if err != nil {
		t.Fatal(err)
	}
	proposalPolicy := t0proj.ShareViews[0].ExpiryPolicy
	quote := proposalPolicy.DefaultQuote
	rotatedCommitment := mustCommitment(t)
	clock.Set(clock.Now().Add(5 * time.Minute))
	rotated := rotateOK(t, app, scope, "rotate-proposal", plan.ID, proposal.ShareID, planshare.RotateInput{
		ExpectedShareRevision: proposal.Revision,
		NewSecretCommitment:   rotatedCommitment,
		ExpiresAt:             proposalPolicy.ResolvedDefaultExpiresAt,
		ExpirySource: planshare.ExpirySourceV1{
			Kind:         planshare.ExpirySourceQuotedDefault,
			DefaultQuote: &quote,
		},
		PolicyVersion: planshare.PolicyVersionV1,
	})
	replay, err := app.Rotate(ctx, scope, "rotate-proposal", plan.ID, proposal.ShareID, planshare.RotateInput{
		ExpectedShareRevision: proposal.Revision,
		NewSecretCommitment:   rotatedCommitment,
		ExpiresAt:             proposalPolicy.ResolvedDefaultExpiresAt,
		ExpirySource: planshare.ExpirySourceV1{
			Kind:         planshare.ExpirySourceQuotedDefault,
			DefaultQuote: &quote,
		},
		PolicyVersion: planshare.PolicyVersionV1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if replay.Selector != rotated.Selector || replay.ShareID != rotated.ShareID {
		t.Fatalf("replay diverged: %+v vs %+v", replay, rotated)
	}

	// expired quote on a fresh plan → 409 expiry_quote_expired
	fresh, err := repo.Create(ctx, scope, shootplanning.CreatePlanInput{Title: "报价过期", Subject: "主体"})
	if err != nil {
		t.Fatal(err)
	}
	clock.Set(time.Date(2026, 8, 14, 11, 0, 0, 0, time.UTC))
	freshProj, err := app.GetManagementProjection(ctx, scope, fresh.ID, planshare.ManagementQuery{})
	if err != nil {
		t.Fatal(err)
	}
	freshQuote := freshProj.ShareViews[0].ExpiryPolicy.DefaultQuote
	clock.Set(freshQuote.ValidUntil.Add(time.Second))
	_, err = app.Issue(ctx, scope, "quote-expired", fresh.ID, planshare.IssueInput{
		ExpectedPlanRevision: fresh.Revision,
		ViewLevel:            planshare.ViewLevelProposal,
		SecretCommitment:     mustCommitment(t),
		ExpiresAt:            freshProj.ShareViews[0].ExpiryPolicy.ResolvedDefaultExpiresAt,
		ExpirySource: planshare.ExpirySourceV1{
			Kind:         planshare.ExpirySourceQuotedDefault,
			DefaultQuote: &freshQuote,
		},
		PolicyVersion: planshare.PolicyVersionV1,
	})
	var expired planshare.ExpiryQuoteExpiredError
	if !errors.As(err, &expired) {
		t.Fatalf("expired quote err = %v", err)
	}

	// full without order link → 409
	plan2, err := repo.Create(ctx, scope, shootplanning.CreatePlanInput{Title: "无资格", Subject: "主体"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = app.Issue(ctx, scope, "full-ineligible", plan2.ID, planshare.IssueInput{
		ExpectedPlanRevision: plan2.Revision,
		ViewLevel:            planshare.ViewLevelFull,
		SecretCommitment:     mustCommitment(t),
		ExpiresAt:            clock.Now().Add(2 * time.Hour),
		ExpirySource:         planshare.ExpirySourceV1{Kind: planshare.ExpirySourceExplicit},
		PolicyVersion:        planshare.PolicyVersionV1,
	})
	if !errors.Is(err, planshare.ErrFullViewNotEligible) {
		t.Fatalf("ineligible err = %v", err)
	}

	// unlink/relink mints new epoch; new full issue succeeds with new generation.
	clock.Set(time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC))
	_ = revokeOK(t, app, scope, "revoke-full", plan.ID, full.ShareID, planshare.RevokeInput{
		ExpectedShareRevision: full.Revision,
		PolicyVersion:         planshare.PolicyVersionV1,
	})
	unlinked := applyShareCRM(t, scope, engine, plan.ID, outcome.Revision, crm.Command{Kind: crm.KindUnlinkOrder})
	relinked := applyShareCRM(t, scope, engine, plan.ID, unlinked.Revision, crm.Command{
		Kind: crm.KindLinkOrder, OrderID: &linked.ID,
	})
	if deref(relinked.Connection.LinkEpochID) == "" || deref(relinked.Connection.LinkEpochID) == epochBefore {
		t.Fatalf("relink must mint new epoch: before=%s after=%s", epochBefore, deref(relinked.Connection.LinkEpochID))
	}
	reissued := issueOK(t, app, scope, "full-after-relink", plan.ID, planshare.IssueInput{
		ExpectedPlanRevision: relinked.Revision,
		ViewLevel:            planshare.ViewLevelFull,
		SecretCommitment:     mustCommitment(t),
		ExpiresAt:            clock.Now().Add(2 * time.Hour),
		ExpirySource:         planshare.ExpirySourceV1{Kind: planshare.ExpirySourceExplicit},
		PolicyVersion:        planshare.PolicyVersionV1,
	})
	if reissued.Generation <= full.Generation {
		t.Fatalf("reissue generation = %d old = %d", reissued.Generation, full.Generation)
	}
}

func issueOK(t *testing.T, app *planshare.Application, scope store.AccountScope, key, planID string, input planshare.IssueInput) planshare.ShareIssueResultV1 {
	t.Helper()
	result, err := app.Issue(context.Background(), scope, key, planID, input)
	if err != nil {
		t.Fatalf("issue %s: %v", key, err)
	}
	return result
}

func rotateOK(t *testing.T, app *planshare.Application, scope store.AccountScope, key, planID, shareID string, input planshare.RotateInput) planshare.ShareIssueResultV1 {
	t.Helper()
	result, err := app.Rotate(context.Background(), scope, key, planID, shareID, input)
	if err != nil {
		t.Fatalf("rotate %s: %v", key, err)
	}
	return result
}

func revokeOK(t *testing.T, app *planshare.Application, scope store.AccountScope, key, planID, shareID string, input planshare.RevokeInput) planshare.ShareRevokeResultV1 {
	t.Helper()
	result, err := app.Revoke(context.Background(), scope, key, planID, shareID, input)
	if err != nil {
		t.Fatalf("revoke %s: %v", key, err)
	}
	return result
}

func mustCommitment(t *testing.T) planshare.ShareSecretCommitment {
	t.Helper()
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		t.Fatal(err)
	}
	return sha256.Sum256(secret)
}

func openShareStore(t *testing.T) *store.Store {
	t.Helper()
	ctx := context.Background()
	ctr, err := tcpostgres.Run(ctx, "postgres:17-alpine",
		tcpostgres.WithDatabase("plan_share_test"),
		tcpostgres.WithUsername("plan_share_test"),
		tcpostgres.WithPassword("plan_share_test"),
		testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(60*time.Second)),
	)
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(ctr) })
	url, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}
	if err := store.MigrateUp(url); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	db, err := store.Open(ctx, url)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(db.Close)
	return db
}

func createShareAccount(t *testing.T, db *store.Store, id string) store.AccountScope {
	t.Helper()
	if err := db.CreateAccount(context.Background(), id, "test-hash"); err != nil {
		t.Fatal(err)
	}
	return db.ScopeFor(auth.AccountContext{AccountID: id})
}

func createShareCustomer(t *testing.T, customers *customer.Service, scope store.AccountScope, name string) customer.Customer {
	t.Helper()
	created, err := customers.Create(context.Background(), scope, customer.CreateInput{
		DisplayName: name,
		Channel:     customer.ChannelOther,
		Identities:  []customer.IdentityInput{{Platform: customer.PlatformOther, Handle: name}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return created
}

func applyShareCRM(t *testing.T, scope store.AccountScope, engine *crm.Engine, planID string, revision int64, command crm.Command) crm.ApplyOutcome {
	t.Helper()
	var outcome crm.ApplyOutcome
	err := scope.WithTxScope(context.Background(), func(tx store.TxAccountScope) error {
		var applyErr error
		outcome, applyErr = engine.ApplyCommandInScope(context.Background(), tx, planID, revision, command)
		return applyErr
	})
	if err != nil {
		t.Fatal(err)
	}
	return outcome
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

type atomicClock struct {
	now atomic.Value
}

func newAtomicClock(t time.Time) *atomicClock {
	c := &atomicClock{}
	c.Set(t)
	return c
}

func (c *atomicClock) Now() time.Time {
	return c.now.Load().(time.Time)
}

func (c *atomicClock) Set(t time.Time) {
	c.now.Store(t.UTC())
}
