package planshare_test

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestFeedbackS5ProposalFullIdempotencyDisposition(t *testing.T) {
	ctx := context.Background()
	db := openShareStore(t)
	scope := createShareAccount(t, db, "share-feedback-s5")
	clock := newAtomicClock(time.Date(2026, 8, 14, 15, 0, 0, 0, time.UTC))
	app := planshare.NewApplication(idempotency.NewExecutor(), planshare.WithClock(clock.Now))
	repo := shootplanning.NewPostgresRepository()
	planningApp, err := shootplanning.NewApplication(repo, idempotency.NewExecutor())
	if err != nil {
		t.Fatal(err)
	}
	engine := crm.NewEngine()
	customers := customer.NewService(customer.NewPostgresRepository())
	orders := order.NewService(order.NewPostgresRepository())

	plan, err := repo.Create(ctx, scope, shootplanning.CreatePlanInput{Title: "反馈策划", Subject: "主体"})
	if err != nil {
		t.Fatal(err)
	}
	title := "镜头A"
	mutated, err := planningApp.ApplyPlanCommand(ctx, scope, "fb-shot-create", plan.ID, plan.Revision,
		shootplanning.UpsertShotCommand{Shot: shootplanning.ShotWrite{Title: &title}})
	if err != nil {
		t.Fatal(err)
	}
	plan.Revision = mutated.Revision

	var shotID string
	var shotRev int64
	err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		return tx.QueryRow(ctx, "shoot_plan_shots", "id, revision", "plan_id = $2 AND removed_at IS NULL", plan.ID).
			Scan(&shotID, &shotRev)
	})
	if err != nil {
		t.Fatal(err)
	}

	cust := createShareCustomer(t, customers, scope, "反馈客户")
	orderTitle := "成片"
	status := order.StatusScheduled
	linked, err := orders.Create(ctx, scope, order.CreateInput{
		CreationMode: order.CreationModeNew,
		CustomerID:   cust.ID,
		Title:        &orderTitle,
		Status:       &status,
	})
	if err != nil {
		t.Fatal(err)
	}
	outcome := applyShareCRM(t, scope, engine, plan.ID, plan.Revision, crm.Command{
		Kind: crm.KindLinkOrder, OrderID: &linked.ID,
	})
	plan.Revision = outcome.Revision

	proposalSecret := mustSecret(t)
	proposal := issueOK(t, app, scope, "fb-issue-proposal", plan.ID, planshare.IssueInput{
		ExpectedPlanRevision: plan.Revision,
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
	full := issueOK(t, app, scope, "fb-issue-full", plan.ID, planshare.IssueInput{
		ExpectedPlanRevision: plan.Revision,
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
	deps := planshare.AnonymousFeedbackDeps{
		Resolver:       resolver,
		Runner:         runner,
		MutationBudget: fakeMutationBudget{allowed: true},
		MutationDigest: fakeMutationDigest{},
		SourceIP:       "203.0.113.50",
	}

	created, err := app.CreateAnonymousPlanFeedback(ctx, deps, "fb-plan-key-1", proposalWire, planshare.CreatePlanFeedbackInput{
		ExpectedProjectionRevision: plan.Revision,
		AuthorDisplayName:          "  ",
		Content:                    "整案建议",
		PolicyVersion:              planshare.PolicyVersionV1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.TargetKind != planshare.FeedbackTargetPlan || created.Revision != 1 {
		t.Fatalf("created=%+v", created)
	}
	replay, err := app.CreateAnonymousPlanFeedback(ctx, deps, "fb-plan-key-1", proposalWire, planshare.CreatePlanFeedbackInput{
		ExpectedProjectionRevision: plan.Revision,
		AuthorDisplayName:          "  ",
		Content:                    "整案建议",
		PolicyVersion:              planshare.PolicyVersionV1,
	})
	if err != nil || replay.FeedbackID != created.FeedbackID {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	_, err = app.CreateAnonymousPlanFeedback(ctx, deps, "fb-plan-key-1", proposalWire, planshare.CreatePlanFeedbackInput{
		ExpectedProjectionRevision: plan.Revision,
		AuthorDisplayName:          "别名",
		Content:                    "整案建议",
		PolicyVersion:              planshare.PolicyVersionV1,
	})
	if !errors.Is(err, idempotency.ErrConflict) {
		t.Fatalf("conflict err=%v", err)
	}

	_, err = app.CreateAnonymousShotFeedback(ctx, deps, "fb-shot-proposal", proposalWire, shotID, planshare.CreateShotFeedbackInput{
		ExpectedShotRevision: shotRev,
		Content:              "proposal 不该写 shot",
		PolicyVersion:        planshare.PolicyVersionV1,
	})
	if !errors.Is(err, planshare.ErrShareNotFound) {
		t.Fatalf("proposal shot feedback want 404, err=%v", err)
	}

	shotFB, err := app.CreateAnonymousShotFeedback(ctx, deps, "fb-shot-full", fullWire, shotID, planshare.CreateShotFeedbackInput{
		ExpectedShotRevision: shotRev,
		AuthorDisplayName:    "客户甲",
		Content:              "镜头建议",
		PolicyVersion:        planshare.PolicyVersionV1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if shotFB.TargetKind != planshare.FeedbackTargetShot || deref(shotFB.TargetRef) != shotID {
		t.Fatalf("shotFB=%+v", shotFB)
	}
	_, err = app.CreateAnonymousShotFeedback(ctx, deps, "fb-shot-stale", fullWire, shotID, planshare.CreateShotFeedbackInput{
		ExpectedShotRevision: shotRev + 1,
		Content:              "过期 revision",
		PolicyVersion:        planshare.PolicyVersionV1,
	})
	if !errors.Is(err, planshare.ErrShotRevisionConflict) {
		t.Fatalf("stale shot want conflict, err=%v", err)
	}

	_, err = app.CreateAnonymousPlanFeedback(ctx, deps, "fb-html", fullWire, planshare.CreatePlanFeedbackInput{
		ExpectedProjectionRevision: plan.Revision,
		Content:                    "<b>x</b>",
		PolicyVersion:              planshare.PolicyVersionV1,
	})
	if !errors.Is(err, planshare.ErrValidation) {
		t.Fatalf("html want validation, err=%v", err)
	}
	_, err = app.CreateAnonymousPlanFeedback(ctx, deps, "fb-toolong", fullWire, planshare.CreatePlanFeedbackInput{
		ExpectedProjectionRevision: plan.Revision,
		Content:                    strings.Repeat("啊", 2001),
		PolicyVersion:              planshare.PolicyVersionV1,
	})
	if !errors.Is(err, planshare.ErrValidation) {
		t.Fatalf("overlong want validation, err=%v", err)
	}

	page, err := app.ListFeedback(ctx, scope, plan.ID, planshare.FeedbackListQuery{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) < 2 {
		t.Fatalf("page items=%d", len(page.Items))
	}
	for i := 1; i < len(page.Items); i++ {
		prev, cur := page.Items[i-1], page.Items[i]
		if prev.CreatedAt.Before(cur.CreatedAt) ||
			(prev.CreatedAt.Equal(cur.CreatedAt) && prev.FeedbackID <= cur.FeedbackID) {
			t.Fatalf("cursor order unstable: %+v then %+v", prev, cur)
		}
	}
	first := page.Items[0]
	cursor := base64.RawURLEncoding.EncodeToString([]byte(first.CreatedAt.UTC().Format(time.RFC3339Nano) + "|" + first.FeedbackID))
	cursorPage, err := app.ListFeedback(ctx, scope, plan.ID, planshare.FeedbackListQuery{Limit: 1, Cursor: cursor})
	if err != nil {
		t.Fatal(err)
	}
	if len(cursorPage.Items) != 1 || cursorPage.Items[0].FeedbackID == first.FeedbackID {
		t.Fatalf("cursor page=%+v", cursorPage)
	}

	disp, err := app.SetFeedbackDisposition(ctx, scope, "fb-disp-1", plan.ID, shotFB.FeedbackID, planshare.DispositionInput{
		ExpectedFeedbackRevision: shotFB.Revision,
		Disposition:              planshare.FeedbackDispositionAdopted,
	})
	if err != nil {
		t.Fatal(err)
	}
	if disp.Disposition != planshare.FeedbackDispositionAdopted || disp.Revision != 2 {
		t.Fatalf("disp=%+v", disp)
	}
	if disp.DeepLinkTarget.Kind != planshare.DeepLinkShot || deref(disp.DeepLinkTarget.ShotID) != shotID {
		t.Fatalf("deep link=%+v", disp.DeepLinkTarget)
	}
	var afterTitle string
	var afterRev int64
	err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		return tx.QueryRow(ctx, "shoot_plan_shots", "title, revision", "id = $2", shotID).
			Scan(&afterTitle, &afterRev)
	})
	if err != nil {
		t.Fatal(err)
	}
	if afterTitle != title || afterRev != shotRev {
		t.Fatalf("adopt mutated core shot title=%q rev=%d", afterTitle, afterRev)
	}

	router := httpapi.NewRouter(httpapi.RouterDeps{
		Logger: slog.Default(),
		AnonymousShare: httpapi.AnonymousShareDeps{
			App:            app,
			Resolver:       resolver,
			Runner:         runner,
			MutationBudget: fakeMutationBudget{allowed: true},
			MutationDigest: fakeMutationDigest{},
			PublicBaseURL:  "https://app.example.invalid",
			Now:            clock.Now,
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/shared/plans/"+proposalWire+"/shots/"+shotID+"/feedback",
		strings.NewReader(`{"expected_shot_revision":1,"content":"x","policy_version":"v1"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "http-proposal-shot")
	req.Header.Set("Origin", "https://app.example.invalid")
	req.RemoteAddr = "203.0.113.50:443"
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("proposal shot http status=%d body=%s", rec.Code, rec.Body.String())
	}

	rateDeps := planshare.AnonymousFeedbackDeps{
		Resolver:       resolver,
		Runner:         runner,
		MutationBudget: fakeMutationBudget{allowed: false, retry: 12 * time.Second},
		MutationDigest: fakeMutationDigest{},
		SourceIP:       "203.0.113.50",
	}
	_, err = app.CreateAnonymousPlanFeedback(ctx, rateDeps, "app-rate-key", fullWire, planshare.CreatePlanFeedbackInput{
		ExpectedProjectionRevision: plan.Revision,
		Content:                    "限流",
		PolicyVersion:              planshare.PolicyVersionV1,
	})
	if !errors.Is(err, planshare.ErrAnonymousMutationRateLimited) {
		t.Fatalf("app rate err=%v", err)
	}

	rateRouter := httpapi.NewRouter(httpapi.RouterDeps{
		Logger: slog.Default(),
		AnonymousShare: httpapi.AnonymousShareDeps{
			App:            app,
			Resolver:       resolver,
			Runner:         runner,
			MutationBudget: fakeMutationBudget{allowed: false, retry: 12 * time.Second},
			MutationDigest: fakeMutationDigest{},
			PublicBaseURL:  "https://app.example.invalid",
			Now:            clock.Now,
		},
	})
	rateReq := httptest.NewRequest(http.MethodPost, "/api/v1/shared/plans/"+fullWire+"/feedback",
		strings.NewReader(fmt.Sprintf(
			`{"expected_projection_revision":%d,"content":"限流","policy_version":"v1"}`, plan.Revision,
		)))
	rateReq.Header.Set("Content-Type", "application/json")
	rateReq.Header.Set("Idempotency-Key", "http-rate-key-xxxxxxxx")
	rateReq.Header.Set("Origin", "https://app.example.invalid")
	rateReq.RemoteAddr = "203.0.113.50:443"
	rateRec := httptest.NewRecorder()
	rateRouter.ServeHTTP(rateRec, rateReq)
	if rateRec.Code != http.StatusTooManyRequests || rateRec.Header().Get("Retry-After") == "" {
		t.Fatalf("rate status=%d retry=%q body=%s", rateRec.Code, rateRec.Header().Get("Retry-After"), rateRec.Body.String())
	}
}

type fakeMutationBudget struct {
	allowed bool
	retry   time.Duration
}

func (f fakeMutationBudget) ConsumeMutationOuterIP(context.Context, securitybudget.DigestCandidates, time.Time) (time.Duration, bool, error) {
	return f.retry, f.allowed, nil
}
func (f fakeMutationBudget) ConsumeMutationOuterTokenGenerationIP(context.Context, securitybudget.DigestCandidates, time.Time) (time.Duration, bool, error) {
	return f.retry, f.allowed, nil
}
func (f fakeMutationBudget) ConsumeMutationOuterTokenGeneration(context.Context, securitybudget.DigestCandidates, time.Time) (time.Duration, bool, error) {
	return f.retry, f.allowed, nil
}
func (f fakeMutationBudget) ConsumeMutationBusinessTokenGenerationIP(context.Context, securitybudget.DigestCandidates, time.Time) (time.Duration, bool, error) {
	return f.retry, f.allowed, nil
}
func (f fakeMutationBudget) ConsumeMutationBusinessTokenGeneration(context.Context, securitybudget.DigestCandidates, time.Time) (time.Duration, bool, error) {
	return f.retry, f.allowed, nil
}

type fakeMutationDigest struct{}

func (fakeMutationDigest) ResolveMutationIP(context.Context, string, time.Time) (securitybudget.DigestCandidates, error) {
	return securitybudget.DigestCandidates{
		Current: securitybudget.DigestIdentity{Version: securitybudget.DigestVersionV1, HMAC: make([]byte, 32)},
	}, nil
}
func (fakeMutationDigest) ResolveMutationTokenGenerationIP(context.Context, string, string, time.Time) (securitybudget.DigestCandidates, error) {
	return securitybudget.DigestCandidates{
		Current: securitybudget.DigestIdentity{Version: securitybudget.DigestVersionV1, HMAC: make([]byte, 32)},
	}, nil
}
func (fakeMutationDigest) ResolveMutationTokenGeneration(context.Context, string, time.Time) (securitybudget.DigestCandidates, error) {
	return securitybudget.DigestCandidates{
		Current: securitybudget.DigestIdentity{Version: securitybudget.DigestVersionV1, HMAC: make([]byte, 32)},
	}, nil
}
