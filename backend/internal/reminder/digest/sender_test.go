package digest

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

type fixedRecipientResolver struct {
	outcome RecipientOutcome
	after   func()
}

func (r fixedRecipientResolver) ResolveCurrent(context.Context, store.ScopedAccount) (RecipientOutcome, error) {
	if r.after != nil {
		r.after()
	}
	return r.outcome, nil
}

type fixedMessageBuilder struct{ text string }

func (b fixedMessageBuilder) Build(context.Context, store.AccountScope, Delivery) (string, error) {
	return b.text, nil
}

type stubCallAuthorizer struct {
	messages   MessageBuilder
	recipients RecipientResolver
}

func (a stubCallAuthorizer) BeginCurrentCall(
	ctx context.Context,
	scope store.AccountScope,
	claim AttemptClaim,
) (CallStartPermit, error) {
	text, err := a.messages.Build(ctx, scope, claim.Delivery)
	if err != nil {
		return CallStartPermit{}, err
	}
	outcome, err := a.recipients.ResolveCurrent(ctx, store.ScopedAccount{AccountID: scope.AccountID(), Scope: scope})
	if err != nil {
		return CallStartPermit{}, err
	}
	switch outcome.Kind {
	case RecipientMissing, RecipientIntegrity:
		return CallStartPermit{}, errRecipientMissingIntent
	case RecipientCurrent:
	default:
		return CallStartPermit{}, errors.New("unknown recipient outcome")
	}
	return CallStartPermit{
		AttemptID:            "att_stub",
		IntentRevision:       1,
		StartDeadline:        time.Now().UTC().Add(5 * time.Second),
		MonotonicStartBudget: 5 * time.Second,
		PayloadText:          text,
		RecipientChatID:      outcome.ChatID,
	}, nil
}

func newTestSender(
	repository DeliveryRepository,
	gate RecipientGate,
	recipients RecipientResolver,
	messages MessageBuilder,
	telegram TelegramSender,
) *DeliverySender {
	return NewDeliverySender(repository, gate, recipients, messages, telegram).
		WithCallAuthorizer(stubCallAuthorizer{messages: messages, recipients: recipients})
}

type barrierRecipientResolver struct {
	firstResolved chan struct{}
	releaseFirst  chan struct{}
	mu            sync.Mutex
	calls         int
}

func (r *barrierRecipientResolver) ResolveCurrent(
	context.Context,
	store.ScopedAccount,
) (RecipientOutcome, error) {
	r.mu.Lock()
	r.calls++
	call := r.calls
	r.mu.Unlock()
	if call == 1 {
		close(r.firstResolved)
		<-r.releaseFirst
	}
	return RecipientOutcome{Kind: RecipientCurrent, ChatID: "chat-current"}, nil
}

type reclaimProbeRepository struct {
	DeliveryRepository
	mu            sync.Mutex
	claims        int
	secondClaimed chan struct{}
}

func (r *reclaimProbeRepository) ClaimDueAttempt(
	ctx context.Context,
	scope store.AccountScope,
	now time.Time,
	leaseUntil time.Time,
) (AttemptClaim, ClaimDueOutcome, error) {
	claim, outcome, err := r.DeliveryRepository.ClaimDueAttempt(ctx, scope, now, leaseUntil)
	if err == nil && outcome == ClaimDueClaimed {
		r.mu.Lock()
		r.claims++
		if r.claims == 2 {
			close(r.secondClaimed)
		}
		r.mu.Unlock()
	}
	return claim, outcome, err
}

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Set(now time.Time) {
	c.mu.Lock()
	c.now = now
	c.mu.Unlock()
}

func TestDeliverySenderFakeFlowClaimsFencesSendsAndFinalizes(t *testing.T) {
	_, account := startDigestPostgres(t)
	now := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	insertDelivery(t, account.Scope, "del-send", "ack-send", now)
	repo := NewPostgresDeliveryRepository()
	telegram := NewFakeTelegram()
	sender := newTestSender(
		repo,
		NewRecipientGate(),
		fixedRecipientResolver{outcome: RecipientOutcome{Kind: RecipientCurrent, ChatID: "chat-current"}},
		fixedMessageBuilder{text: "绑定成功"},
		telegram,
	).WithClock(func() time.Time { return now })

	processed, err := sender.SendNext(context.Background(), account)
	if err != nil || !processed {
		t.Fatalf("SendNext: processed=%v err=%v", processed, err)
	}
	calls := telegram.SendCalls()
	if len(calls) != 1 || calls[0].ChatID != "chat-current" || calls[0].Text != "绑定成功" {
		t.Fatalf("fake send calls: %+v", calls)
	}
	delivery, err := repo.ReloadClaim(context.Background(), account.Scope, "del-send", "")
	if err != nil || delivery.Status != DeliveryStatusSent || delivery.ClaimID != "" {
		t.Fatalf("delivery not finalized: %+v err=%v", delivery, err)
	}
}

func TestDeliverySenderCallBoundaryFenceReleasesWithoutSendingOrBudget(t *testing.T) {
	_, account := startDigestPostgres(t)
	start := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: start}
	insertDelivery(t, account.Scope, "del-fence", "ack-fence", start)
	repo := NewPostgresDeliveryRepository()
	telegram := NewFakeTelegram()
	sender := newTestSender(
		repo,
		NewRecipientGate(),
		fixedRecipientResolver{
			outcome: RecipientOutcome{Kind: RecipientCurrent, ChatID: "chat-current"},
			after:   func() { clock.Set(start.Add(9 * time.Second)) },
		},
		fixedMessageBuilder{text: "must not send"},
		telegram,
	).WithClock(clock.Now)

	processed, err := sender.SendNext(context.Background(), account)
	if err != nil || !processed {
		t.Fatalf("SendNext: processed=%v err=%v", processed, err)
	}
	if calls := telegram.SendCalls(); len(calls) != 0 {
		t.Fatalf("expired call boundary invoked Telegram: %+v", calls)
	}
	delivery, err := repo.ReloadClaim(context.Background(), account.Scope, "del-fence", "")
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if delivery.Attempts != 0 || delivery.ClaimID != "" || delivery.Status != DeliveryStatusPending {
		t.Fatalf("pre-send fence burned budget or retained claim: %+v", delivery)
	}
}

func TestDeliverySenderExpiredOwnerDoesNotSendAfterAnotherClaimantReclaims(t *testing.T) {
	_, account := startDigestPostgres(t)
	start := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: start}
	insertDelivery(t, account.Scope, "del-reclaim-fence", "ack-reclaim-fence", start)
	repo := &reclaimProbeRepository{
		DeliveryRepository: NewPostgresDeliveryRepository(),
		secondClaimed:      make(chan struct{}),
	}
	gate := NewRecipientGate()
	recipients := &barrierRecipientResolver{
		firstResolved: make(chan struct{}),
		releaseFirst:  make(chan struct{}),
	}
	telegramA := NewFakeTelegram()
	telegramB := NewFakeTelegram()
	senderA := newTestSender(repo, gate, recipients, fixedMessageBuilder{text: "A must not send"}, telegramA).
		WithClock(clock.Now)
	senderB := newTestSender(repo, gate, recipients, fixedMessageBuilder{text: "B sends once"}, telegramB).
		WithClock(clock.Now)

	aDone := make(chan error, 1)
	go func() {
		_, err := senderA.SendNext(context.Background(), account)
		aDone <- err
	}()
	select {
	case <-recipients.firstResolved:
	case <-time.After(time.Second):
		t.Fatal("claimant A did not reach recipient barrier")
	}

	clock.Set(start.Add(claimLease + time.Second))
	bDone := make(chan error, 1)
	go func() {
		_, err := senderB.SendNext(context.Background(), account)
		bDone <- err
	}()
	select {
	case <-repo.secondClaimed:
	case <-time.After(time.Second):
		t.Fatal("claimant B did not reclaim the expired lease")
	}
	close(recipients.releaseFirst)

	for name, done := range map[string]<-chan error{"A": aDone, "B": bDone} {
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("claimant %s: %v", name, err)
			}
		case <-time.After(time.Second):
			t.Fatalf("claimant %s did not finish", name)
		}
	}
	if calls := telegramA.SendCalls(); len(calls) != 0 {
		t.Fatalf("expired claimant A invoked Telegram: %+v", calls)
	}
	if calls := telegramB.SendCalls(); len(calls) != 1 {
		t.Fatalf("reclaiming claimant B calls: got %d, want 1", len(calls))
	}
	delivery, err := repo.ReloadClaim(context.Background(), account.Scope, "del-reclaim-fence", "")
	if err != nil || delivery.Status != DeliveryStatusSent {
		t.Fatalf("reclaimed delivery not finalized: status=%s err=%v", delivery.Status, err)
	}
}

func TestDeliverySenderRecipientMissingBacksOffWithoutSendingOrBudget(t *testing.T) {
	_, account := startDigestPostgres(t)
	now := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	insertDelivery(t, account.Scope, "del-missing", "ack-missing", now)
	repo := NewPostgresDeliveryRepository()
	telegram := NewFakeTelegram()
	sender := newTestSender(
		repo,
		NewRecipientGate(),
		fixedRecipientResolver{outcome: RecipientOutcome{Kind: RecipientMissing}},
		fixedMessageBuilder{text: "must not send"},
		telegram,
	).WithClock(func() time.Time { return now })

	processed, err := sender.SendNext(context.Background(), account)
	if err != nil || !processed {
		t.Fatalf("SendNext: processed=%v err=%v", processed, err)
	}
	delivery, err := repo.ReloadClaim(context.Background(), account.Scope, "del-missing", "")
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if delivery.Attempts != 0 || delivery.ClaimID != "" || !delivery.NextAttemptAt.Equal(now.Add(5*time.Minute)) {
		t.Fatalf("recipient backoff mismatch: %+v", delivery)
	}
	if calls := telegram.SendCalls(); len(calls) != 0 {
		t.Fatalf("missing recipient invoked Telegram: %+v", calls)
	}
}
