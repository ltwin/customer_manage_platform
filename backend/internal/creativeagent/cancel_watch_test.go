package creativeagent

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/realtime"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// failingNotifier refuses every subscription, as a stopped listener does.
type failingNotifier struct{}

func (failingNotifier) SubscribeRealtime(context.Context, store.AccountScope, string, string) (<-chan struct{}, func(), error) {
	return nil, nil, errors.New("listener stopped")
}

// hangingNotifier never finishes subscribing, as when the LISTEN connection
// cannot be made: the store keeps retrying until the caller gives up.
type hangingNotifier struct {
	once     sync.Once
	returned chan struct{}
}

func (n *hangingNotifier) SubscribeRealtime(ctx context.Context, _ store.AccountScope, _, _ string) (<-chan struct{}, func(), error) {
	<-ctx.Done()
	n.once.Do(func() { close(n.returned) })
	return nil, nil, ctx.Err()
}

// streamUntilStopped makes the vendor hold the model call open until the
// worker gives up on it, and reports when that happened.
func streamUntilStopped(f *fixture) (entered <-chan struct{}, interrupted <-chan time.Time) {
	in := make(chan struct{})
	out := make(chan time.Time, 1)
	f.vendor.script = func(ctx context.Context, _ llmgateway.ProviderRequest, n int) (llmgateway.Result, error) {
		if n == 1 {
			close(in)
		}
		<-ctx.Done()
		out <- time.Now()
		return llmgateway.Result{}, ctx.Err()
	}
	return in, out
}

func stopWhileStreaming(t *testing.T, f *fixture, limit time.Duration) time.Duration {
	t.Helper()
	c := f.conversation(f.alice, f.canvasID)
	run, err := f.startRun(f.alice, c.ID, f.consent(f.alice, c.ID).ID, InstructionSegment{Type: "text", Text: "整理参考"})
	if err != nil {
		t.Fatal(err)
	}
	entered, interrupted := streamUntilStopped(f)
	worked := make(chan error, 1)
	go func() { worked <- f.work(f.alice, run.ID) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the model call was never made")
	}
	receipt, err := f.service.CancelRun(t.Context(), f.alice, command(t, map[string]any{"run_id": run.ID}))
	if err != nil {
		t.Fatal(err)
	}
	stopped := time.Now()
	var got Run
	if err = json.Unmarshal(receipt.Outcome.Response, &got); err != nil || got.State != RunCancelled {
		t.Fatalf("run=%+v err=%v", got, err)
	}
	var took time.Duration
	select {
	case at := <-interrupted:
		took = at.Sub(stopped)
	case <-time.After(limit):
		t.Fatalf("the model call was still open %v after the stop", limit)
	}
	select {
	case err = <-worked:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the worker did not finish after the stop")
	}
	return took
}

// The worker hears the stop through the run's notifications instead of at the
// next lease renewal, which is ten seconds away.
func TestStopReachesAStreamingCallWithinTwoSeconds(t *testing.T) {
	f := setup(t)
	f.queue()
	f.service.SetNotifier(f.st)
	if took := stopWhileStreaming(t, f, 5*time.Second); took > 2*time.Second {
		t.Fatalf("the model call was interrupted %v after the stop", took)
	}
}

// Without notifications the run is neither held up nor left running: the model
// call goes out as usual and the lease renewal still finds the stop.
func TestStopFallsBackToTheLeaseWithoutNotifications(t *testing.T) {
	hanging := &hangingNotifier{returned: make(chan struct{})}
	for _, tc := range []struct {
		name     string
		notifier realtime.Notifier
	}{
		{"none", nil},
		{"refused", failingNotifier{}},
		{"never ready", hanging},
	} {
		t.Run(tc.name, func(t *testing.T) {
			renewal, retry := leaseRenewal, leaseRetry
			leaseRenewal, leaseRetry = 200*time.Millisecond, 50*time.Millisecond
			t.Cleanup(func() { leaseRenewal, leaseRetry = renewal, retry })
			f := setup(t)
			f.queue()
			if tc.notifier != nil {
				f.service.SetNotifier(tc.notifier)
			}
			stopWhileStreaming(t, f, 5*leaseRenewal)
		})
	}
	select {
	case <-hanging.returned:
	default:
		t.Fatal("a subscription that never became ready outlived its attempt")
	}
}
