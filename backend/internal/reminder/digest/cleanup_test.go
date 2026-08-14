package digest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

type cleanupProbeRepository struct {
	claim       AttemptClaim
	releaseSeen chan cleanupProbe
}

type cleanupProbe struct {
	err         error
	deadline    time.Time
	hasDeadline bool
}

type finalizeUnknownRepository struct {
	claim          AttemptClaim
	current        Delivery
	applyThenError bool
	finalizeCalls  int
}

func (r *finalizeUnknownRepository) ClaimDueAttempt(
	context.Context,
	store.AccountScope,
	time.Time,
	time.Time,
) (AttemptClaim, ClaimDueOutcome, error) {
	return r.claim, ClaimDueClaimed, nil
}

func (r *finalizeUnknownRepository) ReloadClaim(
	context.Context,
	store.AccountScope,
	string,
	string,
) (Delivery, error) {
	return r.current, nil
}

func (*finalizeUnknownRepository) AssertCallableClaim(
	context.Context,
	store.AccountScope,
	AttemptClaim,
	time.Time,
	time.Time,
) (bool, error) {
	return true, nil
}

func (r *finalizeUnknownRepository) FinalizeAttempt(
	context.Context,
	store.AccountScope,
	AttemptClaim,
	AttemptResult,
	time.Time,
) (FinalizeOutcome, error) {
	r.finalizeCalls++
	if r.finalizeCalls == 1 {
		if r.applyThenError {
			r.current.Status = DeliveryStatusSent
			r.current.ClaimID = ""
			r.current.LeaseUntil = nil
		}
		return FinalizeStale, errors.New("commit outcome unknown")
	}
	r.current.Status = DeliveryStatusSent
	r.current.ClaimID = ""
	r.current.LeaseUntil = nil
	return FinalizeApplied, nil
}

func (*finalizeUnknownRepository) ReleaseClaim(
	context.Context,
	store.AccountScope,
	AttemptClaim,
	ClaimRelease,
	time.Time,
) (FinalizeOutcome, error) {
	return FinalizeStale, nil
}

func (r *cleanupProbeRepository) ClaimDueAttempt(context.Context, store.AccountScope, time.Time, time.Time) (AttemptClaim, ClaimDueOutcome, error) {
	return r.claim, ClaimDueClaimed, nil
}

func (r *cleanupProbeRepository) ReloadClaim(context.Context, store.AccountScope, string, string) (Delivery, error) {
	return r.claim.Delivery, nil
}

func (*cleanupProbeRepository) AssertCallableClaim(context.Context, store.AccountScope, AttemptClaim, time.Time, time.Time) (bool, error) {
	return false, nil
}

func (*cleanupProbeRepository) FinalizeAttempt(context.Context, store.AccountScope, AttemptClaim, AttemptResult, time.Time) (FinalizeOutcome, error) {
	return FinalizeApplied, nil
}

func (r *cleanupProbeRepository) ReleaseClaim(ctx context.Context, _ store.AccountScope, _ AttemptClaim, _ ClaimRelease, _ time.Time) (FinalizeOutcome, error) {
	deadline, ok := ctx.Deadline()
	r.releaseSeen <- cleanupProbe{err: ctx.Err(), deadline: deadline, hasDeadline: ok}
	return FinalizeApplied, nil
}

type cancelingRecipientResolver struct{ cancel context.CancelFunc }

func (r cancelingRecipientResolver) ResolveCurrent(context.Context, store.ScopedAccount) (RecipientOutcome, error) {
	r.cancel()
	return RecipientOutcome{}, context.Canceled
}

func TestDeliverySenderPreSendCancelUsesIndependentBoundedCleanupContext(t *testing.T) {
	now := time.Now().UTC()
	lease := now.Add(claimLease)
	repo := &cleanupProbeRepository{
		claim: AttemptClaim{
			ClaimID:    "claim-cleanup",
			LeaseUntil: lease,
			Delivery: Delivery{
				ID:         "del-cleanup",
				Status:     DeliveryStatusPending,
				ClaimID:    "claim-cleanup",
				LeaseUntil: &lease,
			},
		},
		releaseSeen: make(chan cleanupProbe, 1),
	}
	ctx, cancel := context.WithCancel(context.Background())
	sender := newTestSender(
		repo,
		NewRecipientGate(),
		cancelingRecipientResolver{cancel: cancel},
		fixedMessageBuilder{text: "not reached"},
		NewFakeTelegram(),
	).WithClock(func() time.Time { return now })
	processed, err := sender.SendNext(ctx, store.ScopedAccount{AccountID: "acc-cleanup"})
	if err != nil || !processed {
		t.Fatalf("SendNext: processed=%v err=%v", processed, err)
	}
	select {
	case probe := <-repo.releaseSeen:
		if probe.err != nil {
			t.Fatalf("cleanup inherited canceled attempt context: %v", probe.err)
		}
		if !probe.hasDeadline || time.Until(probe.deadline) > cleanupTimeout || time.Until(probe.deadline) <= 0 {
			t.Fatalf("cleanup deadline is not independently bounded: deadline=%v ok=%v", probe.deadline, probe.hasDeadline)
		}
	case <-time.After(time.Second):
		t.Fatal("pre-send cancel did not release claim")
	}
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatalf("test parent context was not canceled: %v", ctx.Err())
	}
}

func TestDeliverySenderRepairsResultCommitUnknownWithoutResending(t *testing.T) {
	for _, tt := range []struct {
		name           string
		applyThenError bool
		wantFinalizes  int
	}{
		{name: "commit applied but response unknown", applyThenError: true, wantFinalizes: 1},
		{name: "commit not applied then idempotent rewrite", wantFinalizes: 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Now().UTC()
			lease := now.Add(claimLease)
			delivery := Delivery{
				ID: "del-result-repair", Status: DeliveryStatusPending,
				ClaimID: "claim-result-repair", LeaseUntil: &lease,
			}
			repo := &finalizeUnknownRepository{
				claim: AttemptClaim{
					Delivery: delivery, ClaimID: delivery.ClaimID, LeaseUntil: lease,
				},
				current: delivery, applyThenError: tt.applyThenError,
			}
			telegram := NewFakeTelegram()
			sender := newTestSender(
				repo,
				NewRecipientGate(),
				fixedRecipientResolver{outcome: RecipientOutcome{Kind: RecipientCurrent, ChatID: "chat"}},
				fixedMessageBuilder{text: "fixed"},
				telegram,
			).WithClock(func() time.Time { return now })

			processed, err := sender.SendNext(context.Background(), store.ScopedAccount{AccountID: "acc"})
			if err != nil || !processed {
				t.Fatalf("SendNext: processed=%v err=%v", processed, err)
			}
			if repo.finalizeCalls != tt.wantFinalizes || repo.current.Status != DeliveryStatusSent {
				t.Fatalf("repair: calls=%d current=%+v", repo.finalizeCalls, repo.current)
			}
			if calls := telegram.SendCalls(); len(calls) != 1 {
				t.Fatalf("result repair resent Telegram: %d calls", len(calls))
			}
		})
	}
}

// staleThenAppliedRepository returns (FinalizeStale, nil) on the first finalize
// while the claim is still the current pending owner with a live lease, then
// (FinalizeApplied, nil) on the idempotent retry. The old finalize treated the
// first stale+nil as success and skipped repair (REV-006).
type staleThenAppliedRepository struct {
	claim         AttemptClaim
	current       Delivery
	finalizeCalls int
}

func (r *staleThenAppliedRepository) ClaimDueAttempt(
	context.Context, store.AccountScope, time.Time, time.Time,
) (AttemptClaim, ClaimDueOutcome, error) {
	return r.claim, ClaimDueClaimed, nil
}

func (r *staleThenAppliedRepository) ReloadClaim(
	context.Context, store.AccountScope, string, string,
) (Delivery, error) {
	return r.current, nil
}

func (*staleThenAppliedRepository) AssertCallableClaim(
	context.Context, store.AccountScope, AttemptClaim, time.Time, time.Time,
) (bool, error) {
	return true, nil
}

func (r *staleThenAppliedRepository) FinalizeAttempt(
	context.Context, store.AccountScope, AttemptClaim, AttemptResult, time.Time,
) (FinalizeOutcome, error) {
	r.finalizeCalls++
	if r.finalizeCalls == 1 {
		return FinalizeStale, nil
	}
	r.current.Status = DeliveryStatusSent
	r.current.ClaimID = ""
	r.current.LeaseUntil = nil
	return FinalizeApplied, nil
}

func (*staleThenAppliedRepository) ReleaseClaim(
	context.Context, store.AccountScope, AttemptClaim, ClaimRelease, time.Time,
) (FinalizeOutcome, error) {
	return FinalizeStale, nil
}

func TestDeliverySenderRetriesStaleFinalizeWhileClaimStillOwnsDelivery(t *testing.T) {
	now := time.Now().UTC()
	lease := now.Add(claimLease)
	delivery := Delivery{
		ID: "del-stale-nil", Status: DeliveryStatusPending,
		ClaimID: "claim-stale-nil", LeaseUntil: &lease,
	}
	repo := &staleThenAppliedRepository{
		claim:   AttemptClaim{Delivery: delivery, ClaimID: delivery.ClaimID, LeaseUntil: lease},
		current: delivery,
	}
	telegram := NewFakeTelegram()
	sender := newTestSender(
		repo,
		NewRecipientGate(),
		fixedRecipientResolver{outcome: RecipientOutcome{Kind: RecipientCurrent, ChatID: "chat"}},
		fixedMessageBuilder{text: "fixed"},
		telegram,
	).WithClock(func() time.Time { return now })

	processed, err := sender.SendNext(context.Background(), store.ScopedAccount{AccountID: "acc"})
	if err != nil || !processed {
		t.Fatalf("SendNext: processed=%v err=%v", processed, err)
	}
	if repo.finalizeCalls != 2 {
		t.Fatalf("stale+nil finalize was treated as success without repair: calls=%d", repo.finalizeCalls)
	}
	if repo.current.Status != DeliveryStatusSent {
		t.Fatalf("delivery not repaired to sent: %+v", repo.current)
	}
	if calls := telegram.SendCalls(); len(calls) != 1 {
		t.Fatalf("stale finalize repair resent Telegram: %d calls", len(calls))
	}
}
