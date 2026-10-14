package creativeops_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func TestCommandGuardFencesNewEffectsButKeepsReceiptsReadable(t *testing.T) {
	f := setup(t)
	exec := creativeops.Executor{}
	cmd := command("guarded")
	fenced := errors.New("call settling")
	cmd.Guard = func(context.Context, store.TxAccountScope, string, creativeops.Command) error { return fenced }
	if _, err := exec.Run(t.Context(), f.a, operation(), cmd); !errors.Is(err, fenced) {
		t.Fatalf("guard: %v", err)
	}
	if count(t, f, "creative_test_effects") != 0 {
		t.Fatal("guard allowed effect")
	}
	cmd.Guard = nil
	first, err := exec.Run(t.Context(), f.a, operation(), cmd)
	if err != nil {
		t.Fatal(err)
	}
	cmd.Guard = func(context.Context, store.TxAccountScope, string, creativeops.Command) error { return fenced }
	replay, err := exec.Run(t.Context(), f.a, operation(), cmd)
	if err != nil || replay.OperationID != first.OperationID {
		t.Fatalf("receipt replay: %v", err)
	}
	locked, err := exec.LookupLocked(t.Context(), f.a, cmd.OperationID)
	if err != nil || locked.OperationID != cmd.OperationID {
		t.Fatalf("locked receipt: %v", err)
	}
}

func TestLockedLookupWaitsForInFlightCommit(t *testing.T) {
	f := setup(t)
	exec := creativeops.Executor{}
	cmd := command("inflight")
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	cmd.Guard = func(context.Context, store.TxAccountScope, string, creativeops.Command) error {
		close(entered)
		<-release
		return nil
	}
	go func() { _, err := exec.Run(t.Context(), f.a, operation(), cmd); done <- err }()
	<-entered
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	_, err := exec.LookupLocked(ctx, f.a, cmd.OperationID)
	cancel()
	close(release)
	if runErr := <-done; runErr != nil {
		t.Fatal(runErr)
	}
	if err == nil || errors.Is(err, creativeops.ErrNotFound) {
		t.Fatalf("inflight mistaken for absence: %v", err)
	}
	if _, err := exec.LookupLocked(t.Context(), f.a, cmd.OperationID); err != nil {
		t.Fatal(err)
	}
}

func TestLockedCommandLookupRejectsAnotherPayload(t *testing.T) {
	f := setup(t)
	exec := creativeops.Executor{}
	original := command("actual")
	if _, err := exec.Run(t.Context(), f.a, operation(), original); err != nil {
		t.Fatal(err)
	}
	expected := original
	expected.Payload = []byte(`{"marker":"different"}`)
	if _, err := exec.LookupCommandLocked(t.Context(), f.a, operation().Key, expected); !errors.Is(err, creativeops.ErrConflict) {
		t.Fatal("wrong payload receipt accepted", err)
	}
	if _, err := exec.LookupCommandLocked(t.Context(), f.a, "another-operation", original); !errors.Is(err, creativeops.ErrConflict) {
		t.Fatal("wrong operation receipt accepted", err)
	}
	if _, err := exec.LookupCommandLocked(t.Context(), f.a, operation().Key, original); err != nil {
		t.Fatal(err)
	}
}

func TestCommandLookupInTxHoldsAbsenceLockUntilCommit(t *testing.T) {
	f := setup(t)
	exec := creativeops.Executor{}
	cmd := command("absence-fence")
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
			if _, err := exec.LookupCommandInTx(t.Context(), tx, operation().Key, cmd); !errors.Is(err, creativeops.ErrNotFound) {
				return errors.New("expected absent receipt")
			}
			close(entered)
			<-release
			return nil
		})
	}()
	<-entered
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	_, err := exec.Run(ctx, f.a, operation(), cmd)
	cancel()
	close(release)
	if txErr := <-done; txErr != nil {
		t.Fatal(txErr)
	}
	if err == nil || count(t, f, "creative_test_effects") != 0 {
		t.Fatal("absence lock released before transaction ended", err)
	}
	if _, err := exec.Run(t.Context(), f.a, operation(), cmd); err != nil {
		t.Fatal(err)
	}
}
