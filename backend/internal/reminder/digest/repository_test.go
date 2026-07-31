package digest

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func startDigestPostgres(t *testing.T) (*store.Store, store.ScopedAccount) {
	t.Helper()
	ctx := context.Background()
	ctr, err := tcpostgres.Run(ctx, "postgres:17-alpine",
		tcpostgres.WithDatabase("crm_test"),
		tcpostgres.WithUsername("crm_test"),
		tcpostgres.WithPassword("crm_test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(60*time.Second)),
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
		t.Fatalf("migrate: %v", err)
	}
	db, err := store.Open(ctx, url)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(db.Close)
	if err := db.CreateAccount(ctx, "acc_digest", "hash"); err != nil {
		t.Fatalf("create account: %v", err)
	}
	activateLegacyTestAccount(t, db, "acc_digest")
	accounts, err := db.AccountScopes(ctx)
	if err != nil || len(accounts) != 1 {
		t.Fatalf("account scopes: len=%d err=%v", len(accounts), err)
	}
	return db, accounts[0]
}

func activateLegacyTestAccount(t *testing.T, db *store.Store, id string) {
	t.Helper()
	mail := &activationMailSender{}
	service := auth.NewService(
		db,
		auth.NewTokenIssuer("digest-test-account-activation-root"),
		auth.WithAuthMailSender(mail),
		auth.WithAttemptLimiter(db),
		auth.WithPublicBaseURL("https://digest.test"),
	)
	result, err := service.BeginLegacyClaim(context.Background(), id+"@digest.test", false)
	if err != nil || result.State != auth.LegacyClaimReady || mail.wire == "" {
		t.Fatalf("begin legacy test-account activation: state=%s err=%v", result.State, err)
	}
	if _, err := service.VerifyEmail(context.Background(), mail.wire, auth.ClientMeta{SourceIP: netip.MustParseAddr("127.0.0.1")}); err != nil {
		t.Fatalf("verify legacy test-account activation: %v", err)
	}
}

type activationMailSender struct {
	wire string
}

func (s *activationMailSender) Send(_ context.Context, mail auth.AuthMail) (auth.MailReceipt, error) {
	actionURL, err := url.Parse(mail.ActionURL)
	if err != nil {
		return auth.MailReceipt{}, fmt.Errorf("parse activation action URL: %w", err)
	}
	fragment, err := url.ParseQuery(actionURL.Fragment)
	if err != nil {
		return auth.MailReceipt{}, fmt.Errorf("parse activation action fragment: %w", err)
	}
	s.wire = fragment.Get("token")
	return auth.MailReceipt{ProviderMessageID: "digest-test-activation", AcceptedAt: time.Now().UTC()}, nil
}

func insertDelivery(t *testing.T, scope store.AccountScope, id, sourceKey string, now time.Time) {
	t.Helper()
	if err := scope.Insert(context.Background(), "telegram_deliveries", []string{
		"id", "source", "source_key", "message_kind", "status", "next_attempt_at",
	}, id, string(DeliverySourceBindingAck), sourceKey, string(MessageKindBindingAck), string(DeliveryStatusPending), now); err != nil {
		t.Fatalf("insert delivery: %v", err)
	}
}

func TestPostgresDeliveryRepositoryClaimFenceFinalizeAndStaleWriter(t *testing.T) {
	_, account := startDigestPostgres(t)
	now := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	insertDelivery(t, account.Scope, "del-1", "ack-1", now)
	repo := NewPostgresDeliveryRepository()

	claim, outcome, err := repo.ClaimDueAttempt(context.Background(), account.Scope, now, now.Add(30*time.Second))
	if err != nil || outcome != ClaimDueClaimed {
		t.Fatalf("first claim: outcome=%s err=%v", outcome, err)
	}
	if claim.Delivery.ID != "del-1" || claim.ClaimID == "" || !claim.LeaseUntil.Equal(now.Add(30*time.Second)) {
		t.Fatalf("claim mismatch: %+v", claim)
	}
	_, outcome, err = repo.ClaimDueAttempt(context.Background(), account.Scope, now, now.Add(30*time.Second))
	if err != nil || outcome != ClaimDueNone {
		t.Fatalf("second current claim must see no due work: outcome=%s err=%v", outcome, err)
	}
	callable, err := repo.AssertCallableClaim(context.Background(), account.Scope, claim, now, now.Add(12*time.Second))
	if err != nil || !callable {
		t.Fatalf("current claim must be callable: callable=%v err=%v", callable, err)
	}
	finalized, err := repo.FinalizeAttempt(context.Background(), account.Scope, claim, AttemptResult{Sent: true}, now.Add(time.Second))
	if err != nil || finalized != FinalizeApplied {
		t.Fatalf("finalize success: outcome=%s err=%v", finalized, err)
	}
	finalized, err = repo.FinalizeAttempt(context.Background(), account.Scope, claim, AttemptResult{ErrorKind: TelegramErrorNetwork}, now.Add(2*time.Second))
	if err != nil || finalized != FinalizeStale {
		t.Fatalf("stale writer must be a no-op: outcome=%s err=%v", finalized, err)
	}
	loaded, err := repo.ReloadClaim(context.Background(), account.Scope, claim.Delivery.ID, claim.ClaimID)
	if err != nil || loaded.Status != DeliveryStatusSent {
		t.Fatalf("terminal delivery was overwritten: status=%s err=%v", loaded.Status, err)
	}
}

func TestPostgresDeliveryRepositoryRecoversCommittedClaimWhenOutcomeIsUnknown(t *testing.T) {
	_, account := startDigestPostgres(t)
	now := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	insertDelivery(t, account.Scope, "del-claim-unknown", "ack-claim-unknown", now)
	repo := NewPostgresDeliveryRepository()
	repo.afterClaim = func() error { return errors.New("commit outcome unknown") }

	claim, outcome, err := repo.ClaimDueAttempt(context.Background(), account.Scope, now, now.Add(claimLease))
	if err != nil || outcome != ClaimDueClaimed {
		t.Fatalf("recover committed claim: outcome=%s err=%v", outcome, err)
	}
	if claim.ClaimID == "" || claim.Delivery.ID != "del-claim-unknown" {
		t.Fatalf("recovered claim mismatch: %+v", claim)
	}
	loaded, err := repo.ReloadClaim(context.Background(), account.Scope, claim.Delivery.ID, claim.ClaimID)
	if err != nil || loaded.ClaimID != claim.ClaimID {
		t.Fatalf("recovered claim is not durable: loaded=%+v err=%v", loaded, err)
	}
}

func TestPostgresDeliveryRepositoryReleaseDoesNotBurnFailureBudget(t *testing.T) {
	_, account := startDigestPostgres(t)
	now := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	insertDelivery(t, account.Scope, "del-release", "ack-release", now)
	repo := NewPostgresDeliveryRepository()
	claim, outcome, err := repo.ClaimDueAttempt(context.Background(), account.Scope, now, now.Add(30*time.Second))
	if err != nil || outcome != ClaimDueClaimed {
		t.Fatalf("claim: outcome=%s err=%v", outcome, err)
	}
	next := now.Add(5 * time.Minute)
	released, err := repo.ReleaseClaim(context.Background(), account.Scope, claim, ClaimRelease{
		ErrorCode:     "recipient_missing",
		NextAttemptAt: next,
	}, now.Add(time.Second))
	if err != nil || released != FinalizeApplied {
		t.Fatalf("release: outcome=%s err=%v", released, err)
	}
	loaded, err := repo.ReloadClaim(context.Background(), account.Scope, claim.Delivery.ID, claim.ClaimID)
	if err != nil {
		t.Fatalf("reload released delivery: %v", err)
	}
	if loaded.Attempts != 0 || loaded.ClaimID != "" || !loaded.NextAttemptAt.Equal(next) {
		t.Fatalf("release burned budget or retained claim: %+v", loaded)
	}
}

func TestPostgresDeliveryRepositorySupersededClaimRejectsLateResult(t *testing.T) {
	_, account := startDigestPostgres(t)
	now := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	insertDelivery(t, account.Scope, "del-superseded", "ack-superseded", now)
	repo := NewPostgresDeliveryRepository()
	claim, outcome, err := repo.ClaimDueAttempt(context.Background(), account.Scope, now, now.Add(30*time.Second))
	if err != nil || outcome != ClaimDueClaimed {
		t.Fatalf("claim: outcome=%s err=%v", outcome, err)
	}
	if _, err := account.Scope.Update(context.Background(), "telegram_deliveries",
		"status = $2, claim_id = NULL, lease_until = NULL, updated_at = $3",
		"id = $4", string(DeliveryStatusSuperseded), now.Add(time.Second), claim.Delivery.ID); err != nil {
		t.Fatalf("supersede delivery: %v", err)
	}
	finalized, err := repo.FinalizeAttempt(context.Background(), account.Scope, claim, AttemptResult{Sent: true}, now.Add(2*time.Second))
	if err != nil || finalized != FinalizeStale {
		t.Fatalf("late result must not overwrite superseded: outcome=%s err=%v", finalized, err)
	}
	delivery, err := repo.ReloadClaim(context.Background(), account.Scope, claim.Delivery.ID, claim.ClaimID)
	if err != nil || delivery.Status != DeliveryStatusSuperseded {
		t.Fatalf("superseded terminal was overwritten: status=%s err=%v", delivery.Status, err)
	}
}
