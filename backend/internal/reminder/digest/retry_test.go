package digest

import (
	"context"
	"testing"
	"time"
)

func TestDeliveryFailureBudgetUsesOneFiveThirtyThenFailedAtFour(t *testing.T) {
	_, account := startDigestPostgres(t)
	repo := NewPostgresDeliveryRepository()
	now := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	insertDelivery(t, account.Scope, "del-retry", "ack-retry", now)
	wantDelays := []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 0}

	for failure := 1; failure <= 4; failure++ {
		claim, outcome, err := repo.ClaimDueAttempt(context.Background(), account.Scope, now, now.Add(claimLease))
		if err != nil || outcome != ClaimDueClaimed {
			t.Fatalf("claim failure %d: outcome=%s err=%v", failure, outcome, err)
		}
		finalized, err := repo.FinalizeAttempt(context.Background(), account.Scope, claim,
			AttemptResult{ErrorKind: TelegramErrorNetwork}, now.Add(time.Second))
		if err != nil || finalized != FinalizeApplied {
			t.Fatalf("finalize failure %d: outcome=%s err=%v", failure, finalized, err)
		}
		delivery, err := repo.ReloadClaim(context.Background(), account.Scope, "del-retry", "")
		if err != nil {
			t.Fatalf("reload failure %d: %v", failure, err)
		}
		if delivery.Attempts != failure {
			t.Fatalf("failure %d attempts=%d", failure, delivery.Attempts)
		}
		if failure == 4 {
			if delivery.Status != DeliveryStatusFailed {
				t.Fatalf("fourth budget failure status=%s", delivery.Status)
			}
			continue
		}
		wantNext := now.Add(time.Second).Add(wantDelays[failure-1])
		if delivery.Status != DeliveryStatusPending || !delivery.NextAttemptAt.Equal(wantNext) {
			t.Fatalf("failure %d next/status: %+v wantNext=%v", failure, delivery, wantNext)
		}
		now = wantNext
	}
}

func TestDeliveryRateLimitUsesLaterRetryAfter(t *testing.T) {
	_, account := startDigestPostgres(t)
	repo := NewPostgresDeliveryRepository()
	now := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	insertDelivery(t, account.Scope, "del-rate", "ack-rate", now)
	claim, _, err := repo.ClaimDueAttempt(context.Background(), account.Scope, now, now.Add(claimLease))
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if _, err := repo.FinalizeAttempt(context.Background(), account.Scope, claim,
		AttemptResult{ErrorKind: TelegramErrorRateLimited, RetryAfter: 17 * time.Minute}, now); err != nil {
		t.Fatalf("finalize rate limit: %v", err)
	}
	delivery, err := repo.ReloadClaim(context.Background(), account.Scope, "del-rate", "")
	if err != nil || !delivery.NextAttemptAt.Equal(now.Add(17*time.Minute)) {
		t.Fatalf("rate limit retry_at=%v err=%v", delivery.NextAttemptAt, err)
	}
}

func TestDeliverySenderInvalidAuthDoesNotBurnFailureBudget(t *testing.T) {
	_, account := startDigestPostgres(t)
	now := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	insertDelivery(t, account.Scope, "del-auth", "ack-auth", now)
	repo := NewPostgresDeliveryRepository()
	telegram := NewFakeTelegram()
	telegram.QueueSendError(&TelegramError{Kind: TelegramErrorInvalidAuth, Code: "401"})
	state := NewIntegrationState(nil).WithClock(func() time.Time { return now })
	sender := newTestSender(repo, NewRecipientGate(),
		fixedRecipientResolver{outcome: RecipientOutcome{Kind: RecipientCurrent, ChatID: "chat"}},
		fixedMessageBuilder{text: "ack"}, telegram).
		WithClock(func() time.Time { return now }).
		WithIntegrationState(state)
	processed, err := sender.SendNext(context.Background(), account)
	if err != nil || !processed {
		t.Fatalf("SendNext: processed=%v err=%v", processed, err)
	}
	delivery, err := repo.ReloadClaim(context.Background(), account.Scope, "del-auth", "")
	if err != nil || delivery.Attempts != 0 || delivery.Status != DeliveryStatusPending || delivery.ClaimID != "" {
		t.Fatalf("invalid auth burned budget/claim: %+v err=%v", delivery, err)
	}
	if !state.Suspended() {
		t.Fatal("send invalid_auth did not suspend integration")
	}
}

type cancelOnSendTelegram struct {
	started chan struct{}
	cancel  context.CancelFunc
}

func (f cancelOnSendTelegram) SendMessage(ctx context.Context, _, _ string) (MessageRef, error) {
	close(f.started)
	f.cancel()
	<-ctx.Done()
	return MessageRef{}, ctx.Err()
}

func TestDeliverySenderSendStartedCancellationLeavesClaimForRepairOrLease(t *testing.T) {
	_, account := startDigestPostgres(t)
	now := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	insertDelivery(t, account.Scope, "del-cancel-send", "ack-cancel-send", now)
	repo := NewPostgresDeliveryRepository()
	ctx, cancel := context.WithCancel(context.Background())
	telegram := cancelOnSendTelegram{started: make(chan struct{}), cancel: cancel}
	sender := newTestSender(repo, NewRecipientGate(),
		fixedRecipientResolver{outcome: RecipientOutcome{Kind: RecipientCurrent, ChatID: "chat"}},
		fixedMessageBuilder{text: "ack"}, telegram).WithClock(func() time.Time { return now })
	processed, err := sender.SendNext(ctx, account)
	if err != nil || !processed {
		t.Fatalf("SendNext: processed=%v err=%v", processed, err)
	}
	delivery, err := repo.ReloadClaim(context.Background(), account.Scope, "del-cancel-send", "")
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if delivery.Attempts != 0 || delivery.ClaimID == "" || delivery.LeaseUntil == nil {
		t.Fatalf("send-started cancel was immediately retried or burned: %+v", delivery)
	}
}
