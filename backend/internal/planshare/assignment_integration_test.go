package planshare_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/customer"
	"github.com/samson/customer-manage-platform/backend/internal/order"
	"github.com/samson/customer-manage-platform/backend/internal/planshare"
	"github.com/samson/customer-manage-platform/backend/internal/platform/idempotency"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning/crm"
)

func TestAssignmentS6ClaimRevokeConcurrencyGuardWiring(t *testing.T) {
	ctx := context.Background()
	db := openShareStore(t)
	scope := createShareAccount(t, db, "share-assign-s6")
	clock := newAtomicClock(time.Date(2026, 8, 14, 16, 0, 0, 0, time.UTC))
	app := planshare.NewApplication(idempotency.NewExecutor(), planshare.WithClock(clock.Now))
	repo := shootplanning.NewPostgresRepository()
	planningApp, err := shootplanning.NewApplication(
		repo,
		idempotency.NewExecutor(),
		shootplanning.WithArchiveImpactPolicy(shootplanning.PlanningShareArchiveImpactPolicyV1{}),
		shootplanning.WithReadinessRemovalGuard(planshare.ReadinessRemovalGuard{}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := shootplanning.NewApplication(
		repo,
		idempotency.NewExecutor(),
		shootplanning.WithArchiveImpactPolicy(shootplanning.PlanningShareArchiveImpactPolicyV1{}),
	); !errors.Is(err, shootplanning.ErrReadinessGuardWiringMismatch) {
		t.Fatalf("disabled guard with planning-share-v1 err=%v", err)
	}

	engine := crm.NewEngine()
	customers := customer.NewService(customer.NewPostgresRepository())
	orders := order.NewService(order.NewPostgresRepository())

	plan, err := repo.Create(ctx, scope, shootplanning.CreatePlanInput{Title: "认领策划", Subject: "主体"})
	if err != nil {
		t.Fatal(err)
	}
	readyTitle := "妆造准备"
	category := "styling"
	requirement := "required"
	preflight := "unchecked"
	hint := "customer"
	mutated, err := planningApp.ApplyPlanCommand(ctx, scope, "asgn-ready-create", plan.ID, plan.Revision,
		shootplanning.UpsertReadinessCommand{Item: shootplanning.ReadinessWrite{
			Category: &category, Title: &readyTitle, Requirement: &requirement,
			PreflightStatus: &preflight, ResponsibilityHint: &hint,
		}})
	if err != nil {
		t.Fatal(err)
	}
	plan.Revision = mutated.Revision

	var readinessID string
	var readinessRev int64
	err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		return tx.QueryRow(ctx, "shoot_plan_readiness_items",
			"id, revision", "plan_id = $2 AND removed_at IS NULL", plan.ID,
		).Scan(&readinessID, &readinessRev)
	})
	if err != nil {
		t.Fatal(err)
	}

	cust := createShareCustomer(t, customers, scope, "认领客户")
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

	offer, err := app.CreateOnSiteOffer(ctx, scope, "offer-create-1", plan.ID, planshare.CreateOfferInput{
		ExpectedPlanRevision: plan.Revision,
		AssignmentKind:       planshare.AssignmentKindOnSiteSupport,
		Content:              "现场协助布光",
		PolicyVersion:        planshare.PolicyVersionV1,
	})
	if err != nil {
		t.Fatal(err)
	}

	fullSecret := mustSecret(t)
	full := issueOK(t, app, scope, "asgn-issue-full", plan.ID, planshare.IssueInput{
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
	proposalSecret := mustSecret(t)
	proposal := issueOK(t, app, scope, "asgn-issue-proposal", plan.ID, planshare.IssueInput{
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

	resolver := planshare.NewResolver(planshare.StoreTokenLookup{Store: db}, planshare.DefaultTrustedCapabilityFactory{})
	runner := planshare.StoreShareTransactionRunner{Store: db}
	deps := planshare.AnonymousAssignmentDeps{
		Resolver:       resolver,
		Runner:         runner,
		MutationBudget: fakeMutationBudget{allowed: true},
		MutationDigest: fakeMutationDigest{},
		SourceIP:       "203.0.113.60",
	}

	// proposal claim is uniform 404
	if _, err := app.ClaimAnonymousAssignment(ctx, deps, "claim-proposal", proposalWire, planshare.ClaimAssignmentInput{
		Target: planshare.ClaimAssignmentTarget{
			Kind:    planshare.AssignmentTargetOnSiteSupport,
			OfferID: &offer.OfferID,
		},
		ExpectedTargetRevision: offer.Revision,
		ClaimedByDisplayName:   "客户甲",
		ClaimReceiptCommitment: mustReceiptCommitment(t),
		PolicyVersion:          planshare.PolicyVersionV1,
	}); !errors.Is(err, planshare.ErrShareNotFound) {
		t.Fatalf("proposal claim err=%v", err)
	}

	receiptSecretA := mustSecret(t)
	var mu sync.Mutex
	var successCount, alreadyCount int
	var claimed planshare.AssignmentClaimResultV1
	var wg sync.WaitGroup
	wg.Add(2)
	runClaim := func(key, name string, secret []byte) {
		defer wg.Done()
		result, err := app.ClaimAnonymousAssignment(ctx, deps, key, fullWire, planshare.ClaimAssignmentInput{
			Target: planshare.ClaimAssignmentTarget{
				Kind:    planshare.AssignmentTargetOnSiteSupport,
				OfferID: &offer.OfferID,
			},
			ExpectedTargetRevision: offer.Revision,
			ClaimedByDisplayName:   name,
			ClaimReceiptCommitment: planshare.CommitClaimReceipt(secret),
			PolicyVersion:          planshare.PolicyVersionV1,
		})
		mu.Lock()
		defer mu.Unlock()
		switch {
		case err == nil:
			successCount++
			claimed = result
		case errors.Is(err, planshare.ErrAssignmentAlreadyClaimed):
			alreadyCount++
		default:
			t.Errorf("unexpected concurrent claim err=%v", err)
		}
	}
	go runClaim("claim-offer-a", "客户甲", receiptSecretA)
	go runClaim("claim-offer-b", "客户乙", mustSecret(t))
	wg.Wait()
	if successCount != 1 || alreadyCount != 1 {
		t.Fatalf("concurrent claim success=%d already=%d claimed=%+v", successCount, alreadyCount, claimed)
	}
	if claimed.Revision != 1 || claimed.Status != planshare.AssignmentStatusActive {
		t.Fatalf("claim revision/status = %+v", claimed)
	}

	if _, err := app.CloseOnSiteOffer(ctx, scope, "offer-close-active", plan.ID, offer.OfferID, planshare.CloseOfferInput{
		ExpectedOfferRevision: offer.Revision,
		PolicyVersion:         planshare.PolicyVersionV1,
	}); !errors.Is(err, planshare.ErrAssignmentActive) {
		t.Fatalf("close with active assignment err=%v", err)
	}

	revoked, err := app.PhotographerRevokeAssignment(ctx, scope, "photo-revoke-1", plan.ID, claimed.AssignmentID,
		planshare.PhotographerRevokeAssignmentInput{
			ExpectedAssignmentRevision: claimed.Revision,
			PolicyVersion:              planshare.PolicyVersionV1,
		})
	if err != nil {
		t.Fatal(err)
	}
	if revoked.Revision != 2 || revoked.Status != planshare.AssignmentStatusRevoked {
		t.Fatalf("photographer revoke = %+v", revoked)
	}
	replay, err := app.PhotographerRevokeAssignment(ctx, scope, "photo-revoke-1", plan.ID, claimed.AssignmentID,
		planshare.PhotographerRevokeAssignmentInput{
			ExpectedAssignmentRevision: claimed.Revision,
			PolicyVersion:              planshare.PolicyVersionV1,
		})
	if err != nil {
		t.Fatal(err)
	}
	if replay.Revision != 2 {
		t.Fatalf("replay bumped revision: %+v", replay)
	}

	reclaim, err := app.ClaimAnonymousAssignment(ctx, deps, "claim-offer-reclaim", fullWire, planshare.ClaimAssignmentInput{
		Target: planshare.ClaimAssignmentTarget{
			Kind:    planshare.AssignmentTargetOnSiteSupport,
			OfferID: &offer.OfferID,
		},
		ExpectedTargetRevision: offer.Revision,
		ClaimedByDisplayName:   "客户丙",
		ClaimReceiptCommitment: mustReceiptCommitment(t),
		PolicyVersion:          planshare.PolicyVersionV1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if reclaim.AssignmentID == claimed.AssignmentID || reclaim.Revision != 1 {
		t.Fatalf("reclaim should new id/rev1: %+v old=%s", reclaim, claimed.AssignmentID)
	}

	receiptSecretReady := mustSecret(t)
	readyClaim, err := app.ClaimAnonymousAssignment(ctx, deps, "claim-ready-1", fullWire, planshare.ClaimAssignmentInput{
		Target: planshare.ClaimAssignmentTarget{
			Kind:            planshare.AssignmentTargetReadiness,
			ReadinessItemID: &readinessID,
		},
		ExpectedTargetRevision: readinessRev,
		ClaimedByDisplayName:   "客户丁",
		ClaimReceiptCommitment: planshare.CommitClaimReceipt(receiptSecretReady),
		PolicyVersion:          planshare.PolicyVersionV1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if readyClaim.Revision != 1 {
		t.Fatalf("readiness claim = %+v", readyClaim)
	}
	if _, err := planningApp.ApplyPlanCommand(ctx, scope, "remove-ready-blocked", plan.ID, plan.Revision,
		shootplanning.RemoveReadinessCommand{ReadinessID: readinessID}); !errors.Is(err, shootplanning.ErrReadinessAssignmentActive) {
		t.Fatalf("remove readiness err=%v", err)
	}

	wireReceipt, err := planshare.ComposeClaimReceiptWire(receiptSecretReady)
	if err != nil {
		t.Fatal(err)
	}
	selfRevoked, err := app.SelfRevokeAnonymousAssignment(ctx, deps, "self-revoke-1", fullWire, readyClaim.AssignmentID, wireReceipt,
		planshare.SelfRevokeAssignmentInput{
			ExpectedAssignmentRevision: readyClaim.Revision,
			PolicyVersion:              planshare.PolicyVersionV1,
		})
	if err != nil {
		t.Fatal(err)
	}
	if selfRevoked.Revision != 2 {
		t.Fatalf("self revoke = %+v", selfRevoked)
	}

	// rotate full token; old receipt + new token still works for another assignment
	newSecret := mustSecret(t)
	rotated, err := app.Rotate(ctx, scope, "rotate-full-asgn", plan.ID, full.ShareID, planshare.RotateInput{
		ExpectedShareRevision: full.Revision,
		NewSecretCommitment:   planshare.CommitShareSecret(newSecret),
		ExpiresAt:             clock.Now().Add(4 * time.Hour),
		ExpirySource:          planshare.ExpirySourceV1{Kind: planshare.ExpirySourceExplicit},
		PolicyVersion:         planshare.PolicyVersionV1,
	})
	if err != nil {
		t.Fatal(err)
	}
	newWire, err := planshare.ComposeShareTokenWire(rotated.Selector, newSecret)
	if err != nil {
		t.Fatal(err)
	}
	readyClaim2, err := app.ClaimAnonymousAssignment(ctx, deps, "claim-ready-2", newWire, planshare.ClaimAssignmentInput{
		Target: planshare.ClaimAssignmentTarget{
			Kind:            planshare.AssignmentTargetReadiness,
			ReadinessItemID: &readinessID,
		},
		ExpectedTargetRevision: readinessRev,
		ClaimedByDisplayName:   "客户戊",
		ClaimReceiptCommitment: planshare.CommitClaimReceipt(receiptSecretReady),
		PolicyVersion:          planshare.PolicyVersionV1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.SelfRevokeAnonymousAssignment(ctx, deps, "self-revoke-rotated", newWire, readyClaim2.AssignmentID, wireReceipt,
		planshare.SelfRevokeAssignmentInput{
			ExpectedAssignmentRevision: readyClaim2.Revision,
			PolicyVersion:              planshare.PolicyVersionV1,
		}); err != nil {
		t.Fatalf("rotate then self-revoke with original receipt: %v", err)
	}

	page, err := app.ListAssignments(ctx, scope, plan.ID, planshare.AssignmentListQuery{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) < 3 {
		t.Fatalf("expected active+revoked history, got %d", len(page.Items))
	}
	for _, item := range page.Items {
		if item.AssignmentID == "" || item.ContentSnapshot == "" || item.ClaimedByDisplayName == "" {
			t.Fatalf("allowlist incomplete: %+v", item)
		}
	}
}

func mustReceiptCommitment(t *testing.T) planshare.ClaimReceiptCommitment {
	t.Helper()
	return planshare.CommitClaimReceipt(mustSecret(t))
}
