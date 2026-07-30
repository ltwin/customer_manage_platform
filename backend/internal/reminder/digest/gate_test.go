package digest

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestRecipientGateGivesQueuedRebindPriorityOverLaterSend(t *testing.T) {
	gate := NewRecipientGate()
	active := make(chan struct{})
	release := make(chan struct{})
	order := make(chan string, 2)

	go func() {
		_ = gate.WithSend(context.Background(), "acc-1", func(context.Context) error {
			close(active)
			<-release
			return nil
		})
	}()
	<-active

	var started sync.WaitGroup
	started.Add(1)
	go func() {
		started.Done()
		_ = gate.WithRebind(context.Background(), "acc-1", func(context.Context) error {
			order <- "rebind"
			return nil
		})
	}()
	started.Wait()
	go func() {
		_ = gate.WithSend(context.Background(), "acc-1", func(context.Context) error {
			order <- "send"
			return nil
		})
	}()
	close(release)

	if got := <-order; got != "rebind" {
		t.Fatalf("queued rebind must run before a later send, got %q", got)
	}
	if got := <-order; got != "send" {
		t.Fatalf("later send must run after rebind, got %q", got)
	}
}

func TestRecipientGateWaitIsCancellationAware(t *testing.T) {
	gate := NewRecipientGate()
	active := make(chan struct{})
	release := make(chan struct{})
	go func() {
		_ = gate.WithSend(context.Background(), "acc-1", func(context.Context) error {
			close(active)
			<-release
			return nil
		})
	}()
	<-active

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := gate.WithRebind(ctx, "acc-1", func(context.Context) error {
		t.Fatal("canceled waiter must not enter the critical section")
		return nil
	})
	close(release)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context cancellation, got %v", err)
	}
}

func TestRecipientGateDoesNotBlockDifferentAccounts(t *testing.T) {
	gate := NewRecipientGate()
	active := make(chan struct{})
	release := make(chan struct{})
	go func() {
		_ = gate.WithSend(context.Background(), "acc-1", func(context.Context) error {
			close(active)
			<-release
			return nil
		})
	}()
	<-active

	done := make(chan struct{})
	go func() {
		_ = gate.WithSend(context.Background(), "acc-2", func(context.Context) error {
			close(done)
			return nil
		})
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("one account gate blocked an unrelated account")
	}
	close(release)
}
