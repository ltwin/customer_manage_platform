package idempotency

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/platform/txcap"
)

var errScriptedTransactionOutcome = errors.New("scripted transaction outcome unknown")

type scriptedRunner struct {
	mode  string
	calls atomic.Int32
}

func TestTypedExecuteFramesResourceAndOperation(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	scope, customerID := createTestAccount(t, s, "acct-typed-frame")
	executor := NewExecutor()
	var callbacks atomic.Int32
	callback := probeCreateCallback("typed", customerID, &callbacks)

	request := Request{
		Operation: OperationShootPlanCommand, Key: "typed-frame-key",
		ResourceIdentity: PlanResource("plan-a"), CanonicalBody: []byte(`{"expected_revision":1}`),
	}
	first, err := executor.Execute(ctx, scope, request, callback)
	if err != nil {
		t.Fatalf("typed execute: %v", err)
	}
	replay, err := executor.Execute(ctx, scope, request, callback)
	if err != nil || string(replay.Body) != string(first.Body) || callbacks.Load() != 1 {
		t.Fatalf("typed replay response=%s err=%v callbacks=%d", replay.Body, err, callbacks.Load())
	}
	request.ResourceIdentity = PlanResource("plan-b")
	if _, err := executor.Execute(ctx, scope, request, callback); !errors.Is(err, ErrConflict) {
		t.Fatalf("same operation/key across resource error=%v", err)
	}
	request.Operation = OperationShootPlanTransition
	request.ResourceIdentity = TransitionResource("plan-b")
	if _, err := executor.Execute(ctx, scope, request, callback); err != nil {
		t.Fatalf("different operation may reuse key: %v", err)
	}
	if callbacks.Load() != 2 {
		t.Fatalf("callbacks=%d want 2", callbacks.Load())
	}
}

type capabilityProbeScope struct{ tx store.TxAccountScope }

type capabilityProbeRunner struct {
	scope  store.AccountScope
	begins atomic.Int32
}

func (r *capabilityProbeRunner) Run(
	ctx context.Context,
	_ txcap.ShareTransactionCapability,
	callback func(txcap.LedgerTxView, capabilityProbeScope) error,
) error {
	r.begins.Add(1)
	return r.scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		return callback(tx.IdempotencyLedgerView(), capabilityProbeScope{tx: tx})
	})
}

func TestExecuteInScopeAndCapabilityUseOnePhysicalTransaction(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	scope, customerID := createTestAccount(t, s, "acct-capability-probe")
	executor := NewExecutor()

	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		_, err := executor.ExecuteInScope(ctx, tx, Request{
			Operation: OperationShootPlanBatch, Key: "in-scope-probe",
			ResourceIdentity: BatchResource("plan-a"), CanonicalBody: []byte(`{"candidates":[]}`),
		}, func(bound store.TxAccountScope) (StoredResponse, error) {
			if err := insertProbeOrder(ctx, bound, "ord_in_scope", customerID); err != nil {
				return StoredResponse{}, err
			}
			return StoredResponse{Status: 200, Body: []byte(`{"id":"ord_in_scope"}`)}, nil
		})
		return err
	})
	if err != nil {
		t.Fatalf("execute in scope: %v", err)
	}

	runner := &capabilityProbeRunner{scope: scope}
	capability := txcap.NewShareTransactionCapability(txcap.NewValidatedShareContext("selector-fingerprint"))
	callbackCalls := atomic.Int32{}
	request := Request{
		Operation: OperationExecutionEventVoid, Key: "capability-probe",
		ResourceIdentity: EventVoidResource("plan-a", "event-a"), CanonicalBody: []byte(`{"reason":"mistake"}`),
	}
	callback := func(probe capabilityProbeScope) (StoredResponse, error) {
		callbackCalls.Add(1)
		if err := insertProbeOrder(ctx, probe.tx, "ord_capability", customerID); err != nil {
			return StoredResponse{}, err
		}
		return StoredResponse{Status: 201, Body: []byte(`{"id":"ord_capability"}`)}, nil
	}
	if _, err := ExecuteInCapability(ctx, executor, runner, capability, request, callback); err != nil {
		t.Fatalf("capability execute: %v", err)
	}
	if _, err := ExecuteInCapability(ctx, executor, runner, capability, request, callback); err != nil {
		t.Fatalf("capability replay: %v", err)
	}
	if runner.begins.Load() != 2 || callbackCalls.Load() != 1 {
		t.Fatalf("begins=%d callbacks=%d", runner.begins.Load(), callbackCalls.Load())
	}
	assertCount(t, scope, "orders", 2)
	assertCount(t, scope, "idempotency_records", 2)
}

func (r *scriptedRunner) Run(
	ctx context.Context,
	scope store.AccountScope,
	fn func(store.TxAccountScope) error,
) error {
	if r.calls.Add(1) > 1 {
		return scope.WithTxScope(ctx, fn)
	}
	switch r.mode {
	case "committed-but-error":
		if err := scope.WithTxScope(ctx, fn); err != nil {
			return err
		}
		return errScriptedTransactionOutcome
	case "rolled-back-error":
		return scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
			if err := fn(tx); err != nil {
				return err
			}
			return errScriptedTransactionOutcome
		})
	default:
		return scope.WithTxScope(ctx, fn)
	}
}

func TestOperationValuesAreStable(t *testing.T) {
	if OperationOrderCreate != Operation("order.create.v1") {
		t.Fatalf("order operation = %q", OperationOrderCreate)
	}
	if OperationScheduleSlotCreate != Operation("schedule-slot.create.v1") {
		t.Fatalf("schedule operation = %q", OperationScheduleSlotCreate)
	}
}

func TestExecuteCreateProtocol(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	t.Run("serial and concurrent same hash replay one resource", func(t *testing.T) {
		scope, customerID := createTestAccount(t, s, "acct-replay")
		executor := NewExecutor()
		var callbacks atomic.Int32
		callback := probeCreateCallback("replay", customerID, &callbacks)

		first, err := executor.ExecuteCreate(ctx, scope, OperationOrderCreate, "serial-key", []byte(`{"customer_id":"cus"}`), callback)
		if err != nil {
			t.Fatalf("first execute: %v", err)
		}
		second, err := executor.ExecuteCreate(ctx, scope, OperationOrderCreate, "serial-key", []byte(`{"customer_id":"cus"}`), callback)
		if err != nil {
			t.Fatalf("serial replay: %v", err)
		}
		if string(first.Body) != string(second.Body) || callbacks.Load() != 1 {
			t.Fatalf("serial replay mismatch: first=%s second=%s callbacks=%d", first.Body, second.Body, callbacks.Load())
		}

		const workers = 8
		responses := make(chan StoredResponse, workers)
		errs := make(chan error, workers)
		var wg sync.WaitGroup
		for range workers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				response, err := executor.ExecuteCreate(ctx, scope, OperationOrderCreate, "concurrent-key", []byte(`{"customer_id":"cus","title":"same"}`), callback)
				responses <- response
				errs <- err
			}()
		}
		wg.Wait()
		close(responses)
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatalf("concurrent replay: %v", err)
			}
		}
		var concurrentBody string
		for response := range responses {
			if concurrentBody == "" {
				concurrentBody = string(response.Body)
			}
			if string(response.Body) != concurrentBody {
				t.Fatalf("concurrent responses differ: %s != %s", response.Body, concurrentBody)
			}
		}
		if callbacks.Load() != 2 {
			t.Fatalf("one serial owner + one concurrent owner = 2 callbacks, got %d", callbacks.Load())
		}
		assertCount(t, scope, "orders", 2)
	})

	t.Run("successful key rejects different hash", func(t *testing.T) {
		scope, customerID := createTestAccount(t, s, "acct-conflict")
		executor := NewExecutor()
		var callbacks atomic.Int32
		callback := probeCreateCallback("conflict", customerID, &callbacks)
		if _, err := executor.ExecuteCreate(ctx, scope, OperationOrderCreate, "conflict-key", []byte(`{"title":"first"}`), callback); err != nil {
			t.Fatalf("first execute: %v", err)
		}
		if _, err := executor.ExecuteCreate(ctx, scope, OperationOrderCreate, "conflict-key", []byte(`{"title":"changed"}`), callback); !errors.Is(err, ErrConflict) {
			t.Fatalf("different hash: want conflict, got %v", err)
		}
		if callbacks.Load() != 1 {
			t.Fatalf("conflicting replay must not call callback, got %d", callbacks.Load())
		}
	})

	t.Run("deterministic callback failure leaves no claim", func(t *testing.T) {
		scope, customerID := createTestAccount(t, s, "acct-failure")
		executor := NewExecutor()
		wantErr := errors.New("deterministic create failure")
		failedCalls := atomic.Int32{}
		_, err := executor.ExecuteCreate(ctx, scope, OperationOrderCreate, "retry-key", []byte(`{"title":"retry"}`), func(tx store.TxAccountScope) (StoredResponse, error) {
			failedCalls.Add(1)
			if err := insertProbeOrder(ctx, tx, "ord_failed", customerID); err != nil {
				return StoredResponse{}, err
			}
			return StoredResponse{}, wantErr
		})
		if !errors.Is(err, wantErr) {
			t.Fatalf("deterministic failure: got %v", err)
		}
		assertCount(t, scope, "orders", 0)
		assertCount(t, scope, "idempotency_records", 0)

		var successCalls atomic.Int32
		if _, err := executor.ExecuteCreate(ctx, scope, OperationOrderCreate, "retry-key", []byte(`{"title":"retry"}`), probeCreateCallback("retry", customerID, &successCalls)); err != nil {
			t.Fatalf("same key after deterministic rollback: %v", err)
		}
		if failedCalls.Load() != 1 || successCalls.Load() != 1 {
			t.Fatalf("callback counts failed=%d success=%d", failedCalls.Load(), successCalls.Load())
		}
		assertCount(t, scope, "orders", 1)
		assertCount(t, scope, "idempotency_records", 1)
	})

	t.Run("invalid success response rolls resource back", func(t *testing.T) {
		scope, customerID := createTestAccount(t, s, "acct-response")
		executor := NewExecutor()
		_, err := executor.ExecuteCreate(ctx, scope, OperationOrderCreate, "bad-response", []byte(`{"title":"bad"}`), func(tx store.TxAccountScope) (StoredResponse, error) {
			if err := insertProbeOrder(ctx, tx, "ord_bad_response", customerID); err != nil {
				return StoredResponse{}, err
			}
			return StoredResponse{Status: 201, Body: []byte("not-json")}, nil
		})
		if !errors.Is(err, ErrInvalidResponse) {
			t.Fatalf("invalid response: want ErrInvalidResponse, got %v", err)
		}
		assertCount(t, scope, "orders", 0)
		assertCount(t, scope, "idempotency_records", 0)
	})

	for _, mode := range []string{"committed-but-error", "rolled-back-error"} {
		t.Run(mode+" converges with original key", func(t *testing.T) {
			scope, customerID := createTestAccount(t, s, "acct-"+mode)
			runner := &scriptedRunner{mode: mode}
			executor := newExecutor(runner, time.Now, 24*time.Hour)
			var callbacks atomic.Int32
			callback := probeCreateCallback(mode, customerID, &callbacks)
			_, err := executor.ExecuteCreate(ctx, scope, OperationOrderCreate, "unknown-key", []byte(`{"title":"unknown"}`), callback)
			if !errors.Is(err, errScriptedTransactionOutcome) {
				t.Fatalf("first execute: want scripted error, got %v", err)
			}
			if _, err := executor.ExecuteCreate(ctx, scope, OperationOrderCreate, "unknown-key", []byte(`{"title":"unknown"}`), callback); err != nil {
				t.Fatalf("original key retry: %v", err)
			}
			assertCount(t, scope, "orders", 1)
			assertCount(t, scope, "idempotency_records", 1)
			wantCalls := int32(1)
			if mode == "rolled-back-error" {
				wantCalls = 2
			}
			if callbacks.Load() != wantCalls {
				t.Fatalf("callbacks=%d want=%d", callbacks.Load(), wantCalls)
			}
		})
	}

	t.Run("expired record has one concurrent takeover owner", func(t *testing.T) {
		scope, customerID := createTestAccount(t, s, "acct-expiry")
		now := time.Date(2026, 7, 10, 10, 0, 0, 0, time.UTC)
		executor := newExecutor(accountScopeRunner{}, func() time.Time { return now }, time.Hour)
		var initialCalls atomic.Int32
		if _, err := executor.ExecuteCreate(ctx, scope, OperationOrderCreate, "expiry-key", []byte(`{"version":1}`), probeCreateCallback("expiry-initial", customerID, &initialCalls)); err != nil {
			t.Fatalf("initial execute: %v", err)
		}

		now = now.Add(2 * time.Hour)
		var takeoverCalls atomic.Int32
		callback := probeCreateCallback("expiry-takeover", customerID, &takeoverCalls)
		const workers = 6
		var wg sync.WaitGroup
		errs := make(chan error, workers)
		for range workers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := executor.ExecuteCreate(ctx, scope, OperationOrderCreate, "expiry-key", []byte(`{"version":2}`), callback)
				errs <- err
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatalf("takeover execute: %v", err)
			}
		}
		if initialCalls.Load() != 1 || takeoverCalls.Load() != 1 {
			t.Fatalf("initial=%d takeover=%d", initialCalls.Load(), takeoverCalls.Load())
		}
		assertCount(t, scope, "orders", 2)
		assertCount(t, scope, "idempotency_records", 1)
	})
}

func openTestStore(t *testing.T) *store.Store {
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
		t.Fatalf("start postgres container: %v", err)
	}
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(ctr); err != nil {
			t.Logf("terminate postgres container: %v", err)
		}
	})
	url, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("container connection string: %v", err)
	}
	if err := store.MigrateUp(url); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	s, err := store.Open(ctx, url)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(s.Close)
	return s
}

func createTestAccount(t *testing.T, s *store.Store, accountID string) (store.AccountScope, string) {
	t.Helper()
	ctx := context.Background()
	if err := s.CreateAccount(ctx, accountID, "test-hash"); err != nil {
		t.Fatalf("create account: %v", err)
	}
	scope := s.ScopeFor(auth.AccountContext{AccountID: accountID})
	customerID := "cus_" + accountID
	if err := scope.Insert(ctx, "customers", []string{"id", "display_name", "channel"}, customerID, "测试客户", "other"); err != nil {
		t.Fatalf("seed customer: %v", err)
	}
	return scope, customerID
}

func probeCreateCallback(prefix, customerID string, calls *atomic.Int32) func(store.TxAccountScope) (StoredResponse, error) {
	return func(tx store.TxAccountScope) (StoredResponse, error) {
		sequence := calls.Add(1)
		id := fmt.Sprintf("ord_%s_%d", prefix, sequence)
		if err := insertProbeOrder(context.Background(), tx, id, customerID); err != nil {
			return StoredResponse{}, err
		}
		body, err := json.Marshal(map[string]string{"id": id})
		if err != nil {
			return StoredResponse{}, err
		}
		return StoredResponse{Status: 201, Body: body}, nil
	}
}

func insertProbeOrder(ctx context.Context, tx store.TxAccountScope, id, customerID string) error {
	return tx.Insert(ctx, "orders", []string{"id", "customer_id", "status"}, id, customerID, "consulting")
}

func assertCount(t *testing.T, scope store.AccountScope, table string, want int64) {
	t.Helper()
	got, err := scope.Count(context.Background(), table, "")
	if err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	if got != want {
		t.Fatalf("count %s = %d, want %d", table, got, want)
	}
}
