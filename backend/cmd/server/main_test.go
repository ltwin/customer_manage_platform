package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestWaitForRunnerUsesCallerDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	started := time.Now()
	err := waitForRunner(ctx, make(chan struct{}))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waitForRunner: want deadline exceeded, got %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("waitForRunner exceeded bounded wait: %v", elapsed)
	}
}

func TestWaitForRunnerReturnsWhenRunnerStops(t *testing.T) {
	done := make(chan struct{})
	close(done)
	if err := waitForRunner(context.Background(), done); err != nil {
		t.Fatalf("waitForRunner: %v", err)
	}
}

func TestServerLifecycleBoundsListenerErrorWhenRunnerDoesNotStop(t *testing.T) {
	listenerErr := errors.New("listener failed")
	serverResult := make(chan error, 1)
	serverResult <- listenerErr
	cancelCalled := false
	shutdownCalled := false
	lifecycle := serverLifecycle{
		timeout:      10 * time.Millisecond,
		cancel:       func() { cancelCalled = true },
		shutdown:     func(context.Context) error { shutdownCalled = true; return nil },
		serverResult: serverResult,
		runnerDone:   make(chan struct{}),
	}

	started := time.Now()
	err := lifecycle.wait(context.Background())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("wait: want deadline exceeded, got %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("listener error path exceeded bounded wait: %v", elapsed)
	}
	if !cancelCalled {
		t.Fatal("listener error path must cancel the runner context")
	}
	if shutdownCalled {
		t.Fatal("listener error path must not shut down an already stopped HTTP server")
	}
}

func TestServerLifecycleBoundsSignalShutdownWhenRunnerDoesNotStop(t *testing.T) {
	rootCtx, cancelRoot := context.WithCancel(context.Background())
	cancelRoot()
	cancelCalled := false
	shutdownCalled := false
	lifecycle := serverLifecycle{
		timeout:      10 * time.Millisecond,
		cancel:       func() { cancelCalled = true },
		shutdown:     func(context.Context) error { shutdownCalled = true; return nil },
		serverResult: make(chan error),
		runnerDone:   make(chan struct{}),
	}

	started := time.Now()
	err := lifecycle.wait(rootCtx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("wait: want deadline exceeded, got %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("signal path exceeded bounded wait: %v", elapsed)
	}
	if !cancelCalled {
		t.Fatal("signal path must cancel the runner context")
	}
	if !shutdownCalled {
		t.Fatal("signal path must call HTTP Shutdown")
	}
}
