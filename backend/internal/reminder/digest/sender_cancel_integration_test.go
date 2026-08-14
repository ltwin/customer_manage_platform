package digest_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/reminder/digest"
	telegramapi "github.com/samson/customer-manage-platform/backend/internal/reminder/digest/telegram"
)

// This test wires the real DeliverySender to the production telegram.Client
// over a transport that returns the parent context's error, proving the
// send-started cancel path end-to-end without a Fake sender (REV-001).

type cancelTransport struct {
	started chan struct{}
	once    bool
}

func (t *cancelTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if !t.once {
		t.once = true
		close(t.started)
	}
	<-r.Context().Done()
	return nil, r.Context().Err()
}

type cancelSenderRepo struct {
	claim         digest.AttemptClaim
	current       digest.Delivery
	finalizeCalls int
	releaseCalls  int
}

func (r *cancelSenderRepo) ClaimDueAttempt(
	context.Context, store.AccountScope, time.Time, time.Time,
) (digest.AttemptClaim, digest.ClaimDueOutcome, error) {
	return r.claim, digest.ClaimDueClaimed, nil
}

func (r *cancelSenderRepo) ReloadClaim(
	context.Context, store.AccountScope, string, string,
) (digest.Delivery, error) {
	return r.current, nil
}

func (*cancelSenderRepo) AssertCallableClaim(
	context.Context, store.AccountScope, digest.AttemptClaim, time.Time, time.Time,
) (bool, error) {
	return true, nil
}

func (r *cancelSenderRepo) FinalizeAttempt(
	context.Context, store.AccountScope, digest.AttemptClaim, digest.AttemptResult, time.Time,
) (digest.FinalizeOutcome, error) {
	r.finalizeCalls++
	return digest.FinalizeApplied, nil
}

func (r *cancelSenderRepo) ReleaseClaim(
	context.Context, store.AccountScope, digest.AttemptClaim, digest.ClaimRelease, time.Time,
) (digest.FinalizeOutcome, error) {
	r.releaseCalls++
	return digest.FinalizeApplied, nil
}

type currentRecipientResolver struct{ chatID string }

func (r currentRecipientResolver) ResolveCurrent(
	context.Context, store.ScopedAccount,
) (digest.RecipientOutcome, error) {
	return digest.RecipientOutcome{Kind: digest.RecipientCurrent, ChatID: r.chatID}, nil
}

type fixedTextBuilder struct{ text string }

func (b fixedTextBuilder) Build(context.Context, store.AccountScope, digest.Delivery) (string, error) {
	return b.text, nil
}

type fixedCallAuthorizer struct {
	chatID string
	text   string
}

func (a fixedCallAuthorizer) BeginCurrentCall(
	context.Context, store.AccountScope, digest.AttemptClaim,
) (digest.CallStartPermit, error) {
	return digest.CallStartPermit{
		AttemptID:            "att_cancel",
		IntentRevision:       1,
		StartDeadline:        time.Now().UTC().Add(5 * time.Second),
		MonotonicStartBudget: 5 * time.Second,
		PayloadText:          a.text,
		RecipientChatID:      a.chatID,
	}, nil
}

func TestDeliverySenderWithProductionClientKeepsClaimOnSendStartedCancel(t *testing.T) {
	now := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	lease := now.Add(30 * time.Second)
	delivery := digest.Delivery{
		ID:         "del-prod-cancel",
		Status:     digest.DeliveryStatusPending,
		ClaimID:    "claim-prod-cancel",
		LeaseUntil: &lease,
	}
	repo := &cancelSenderRepo{
		claim:   digest.AttemptClaim{Delivery: delivery, ClaimID: delivery.ClaimID, LeaseUntil: lease},
		current: delivery,
	}

	transport := &cancelTransport{started: make(chan struct{})}
	client := telegramapi.NewClient("secret", "https://example.invalid", &http.Client{Transport: transport})

	recipients := currentRecipientResolver{chatID: "chat"}
	messages := fixedTextBuilder{text: "digest body"}
	sender := digest.NewDeliverySender(
		repo,
		digest.NewRecipientGate(),
		recipients,
		messages,
		client,
	).WithClock(func() time.Time { return now }).
		WithCallAuthorizer(fixedCallAuthorizer{chatID: "chat", text: "digest body"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	type result struct {
		processed bool
		err       error
	}
	done := make(chan result, 1)
	account := store.ScopedAccount{AccountID: "acc-prod-cancel"}
	go func() {
		processed, err := sender.SendNext(ctx, account)
		done <- result{processed: processed, err: err}
	}()

	select {
	case <-transport.started:
	case <-time.After(2 * time.Second):
		t.Fatal("production send did not start")
	}
	cancel()

	select {
	case got := <-done:
		if got.err != nil || !got.processed {
			t.Fatalf("send-started cancel not absorbed: processed=%v err=%v", got.processed, got.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("SendNext did not return after cancel")
	}

	if repo.finalizeCalls != 0 {
		t.Fatalf("send-started cancel burned budget via finalize: calls=%d", repo.finalizeCalls)
	}
	if repo.releaseCalls != 0 {
		t.Fatalf("send-started cancel cleared claim via release: calls=%d", repo.releaseCalls)
	}
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatalf("parent context was not canceled: %v", ctx.Err())
	}
}
