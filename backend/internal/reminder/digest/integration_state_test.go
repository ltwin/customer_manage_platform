package digest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

type bindingIssuerProbe struct{ calls int }

func (p *bindingIssuerProbe) IssueBindToken(context.Context, store.AccountScope) (BindLink, error) {
	p.calls++
	return BindLink{Token: "opaque"}, nil
}

func TestIntegrationSuspensionGuardsBindingUntilProbeWindow(t *testing.T) {
	now := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: now}
	state := NewIntegrationState(nil).WithClock(clock.Now)
	issuer := &bindingIssuerProbe{}
	guard := NewGuardedBindingIssuer(issuer, state)

	if _, err := guard.IssueBindToken(context.Background(), store.AccountScope{}); err != nil {
		t.Fatalf("initial issue: %v", err)
	}
	state.Suspend(TelegramErrorInvalidAuth, 5*time.Minute)
	if _, err := guard.IssueBindToken(context.Background(), store.AccountScope{}); !errors.Is(err, ErrTelegramIntegrationSuspended) {
		t.Fatalf("suspended issue: got %v", err)
	}
	if issuer.calls != 1 {
		t.Fatalf("suspended guard called delegate: %d", issuer.calls)
	}

	clock.Set(now.Add(5 * time.Minute))
	if _, err := guard.IssueBindToken(context.Background(), store.AccountScope{}); err != nil {
		t.Fatalf("probe-window issue: %v", err)
	}
	if issuer.calls != 2 {
		t.Fatalf("delegate calls after probe window: %d", issuer.calls)
	}
}

func TestPollerInvalidAuthSuspendsSharedIntegration(t *testing.T) {
	source := NewFakeTelegram()
	source.QueueUpdates(nil, &TelegramError{Kind: TelegramErrorInvalidAuth, Code: "401"})
	state := NewIntegrationState(nil)
	poller := NewPoller(source, &recordingUpdateProcessor{}).WithIntegrationState(state)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		poller.Run(ctx)
		close(done)
	}()

	deadline := time.After(time.Second)
	for !state.Suspended() {
		select {
		case <-deadline:
			cancel()
			t.Fatal("poll invalid_auth did not suspend integration")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("suspended poller did not stop on cancellation")
	}
}
