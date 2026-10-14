package llmgateway_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func retryFixture(t *testing.T) (*fixture, *llmgateway.Catalog, *[]time.Duration) {
	t.Helper()
	primary, backup := routedDeployment("primary", "first"), routedDeployment("backup", "second")
	primary.RateLimitPerMin, backup.RateLimitPerMin = 100, 100
	backup.Price.Peak.Output *= 2
	backup.Price.OffPeak.Output *= 2
	backup.Price.Version = "backup-price"
	catalog, err := llmgateway.NewRoutedCatalog("retry-test", "USD", []llmgateway.Model{routedModel("primary", "backup")}, []llmgateway.Deployment{primary, backup})
	if err != nil {
		t.Fatal(err)
	}
	f := setupGatewayWith(t, defaultBudget(), catalog)
	var waits []time.Duration
	configureRetry(t, f, catalog, func(ctx context.Context, delay time.Duration) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		waits = append(waits, delay)
		f.now = f.now.Add(delay)
		return nil
	})
	return f, catalog, &waits
}

func configureRetry(t *testing.T, f *fixture, catalog *llmgateway.Catalog, wait func(context.Context, time.Duration) error) {
	t.Helper()
	var err error
	f.gateway, err = llmgateway.New(llmgateway.Config{
		Catalog: catalog, Providers: map[llmgateway.ProviderKey]llmgateway.Provider{llmgateway.ProviderOpenAICompatible: f.provider},
		Credential: func(string) (string, bool) { return "test", true }, Clock: func() time.Time { return f.now },
		DefaultBudget: defaultBudget(), ChatRetry: true, RetryWait: wait,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestChatRetryPriorityAndIndependentAccounting(t *testing.T) {
	for _, tc := range []struct {
		name            string
		failures, calls int
		status          int
		outcome         llmgateway.Outcome
		class           string
		wantErr         error
	}{
		{name: "recovers on third attempt", failures: 2, calls: 3, status: 503, outcome: llmgateway.OutcomeUnknown, class: "server_error"},
		{name: "falls back after five retries", failures: 6, calls: 7, status: 502, outcome: llmgateway.OutcomeUnknown, class: "server_error"},
		{name: "all deployments exhausted", failures: 12, calls: 12, status: 500, outcome: llmgateway.OutcomeUnknown, class: "server_error", wantErr: llmgateway.ErrProvidersExhausted},
		{name: "timeout retries", failures: 1, calls: 2, outcome: llmgateway.OutcomeUnknown, class: "timeout"},
		{name: "rate limit retries", failures: 1, calls: 2, status: 429, outcome: llmgateway.OutcomeUnaccepted, class: "admission_refused"},
		{name: "unexpected terminal status retries", failures: 1, calls: 2, outcome: llmgateway.OutcomeProtocol, class: "response_status"},
		{name: "credential skips vendor", failures: 1, calls: 2, status: 401, outcome: llmgateway.OutcomeRejected, class: "invalid_request"},
		{name: "bad request stops", failures: 1, calls: 1, status: 400, outcome: llmgateway.OutcomeRejected, class: "invalid_request", wantErr: llmgateway.ErrRejected},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, catalog, waits := retryFixture(t)
			var routes []string
			f.provider.observe = func(req llmgateway.ProviderRequest) {
				routes = append(routes, req.Model.DeploymentKey)
				f.provider.err = nil
				if len(routes) <= tc.failures {
					f.provider.err = &llmgateway.ProviderError{Outcome: tc.outcome, Class: tc.class, StatusCode: tc.status}
				}
			}
			session := f.callSession(f.a, "retry-run")
			consumed := 0
			in := llmgateway.CallInput{BindingKey: "retry-turn", Chat: textRequest("chat"), Consume: func(store.TxAccountScope, llmgateway.Result) error { consumed++; return nil }}
			result, err := f.gateway.Call(t.Context(), session, in)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("error %v, want %v", err, tc.wantErr)
			}
			if len(routes) != tc.calls {
				t.Fatalf("calls %v", routes)
			}
			for i, route := range routes {
				want := "primary"
				if i >= 6 || tc.status == 401 && i > 0 {
					want = "backup"
				}
				if route != want {
					t.Fatalf("routes %v", routes)
				}
			}
			if len(*waits) != tc.calls-1 {
				t.Fatalf("waits %v", *waits)
			}
			for _, delay := range *waits {
				if delay != 5*time.Second {
					t.Fatalf("delay %v", delay)
				}
			}
			if n := f.count(t, "SELECT count(*) FROM llm_requests WHERE account_id='gw-a' AND state IN ('unknown','dispatching','streaming')"); n != 0 {
				t.Fatalf("%d requests still block the UI", n)
			}
			if tc.outcome == llmgateway.OutcomeUnknown {
				n := f.count(t, "SELECT count(*) FROM llm_usage_reservations WHERE account_id='gw-a' AND settlement_state='unknown' AND remaining_hold_micros>0")
				if n != tc.failures {
					t.Fatalf("lost unknown billing evidence: %d", n)
				}
			}
			if tc.wantErr == nil {
				if consumed != 1 {
					t.Fatalf("consumed %d times", consumed)
				}
				view, err := f.gateway.RequestForBinding(t.Context(), f.a, session.CallerService, in.BindingKey)
				if err != nil || view.ID != result.RequestID || view.State != llmgateway.StateSucceeded {
					t.Fatalf("binding %+v %v", view, err)
				}
			}
			// A process restart reuses the route and never purchases attempts again.
			configureRetry(t, f, catalog, func(context.Context, time.Duration) error { t.Fatal("replayed completed route waited"); return nil })
			replayed, err := f.gateway.Call(t.Context(), session, in)
			if !errors.Is(err, tc.wantErr) || f.provider.attempts() != tc.calls {
				t.Fatalf("replay err=%v calls=%d", err, f.provider.attempts())
			}
			if tc.wantErr == nil && (!replayed.AlreadyConsumed || consumed != 1) {
				t.Fatal("replay consumed twice")
			}
		})
	}
}

func TestChatRetryCancellationAndFinalAdmission(t *testing.T) {
	for _, cancelWait := range []bool{true, false} {
		t.Run(fmt.Sprint(cancelWait), func(t *testing.T) {
			f, catalog, _ := retryFixture(t)
			f.provider.err = &llmgateway.ProviderError{Outcome: llmgateway.OutcomeUnknown, Class: "server_error", StatusCode: 503}
			refused := errors.New("consent revoked")
			configureRetry(t, f, catalog, func(context.Context, time.Duration) error {
				if cancelWait {
					return context.Canceled
				}
				f.now = f.now.Add(5 * time.Second)
				return nil
			})
			session := f.callSession(f.a, "guard-run")
			admissions := 0
			session.Admit = func(store.TxAccountScope) error {
				admissions++
				if admissions > 1 {
					return refused
				}
				return nil
			}
			_, err := f.gateway.Call(t.Context(), session, llmgateway.CallInput{BindingKey: "guard-turn", Chat: textRequest("chat")})
			want := refused
			if cancelWait {
				want = context.Canceled
			}
			if !errors.Is(err, want) || f.provider.attempts() != 1 {
				t.Fatalf("err %v calls %d", err, f.provider.attempts())
			}
		})
	}
}

func TestEveryNewChatStartsAtHighestPriority(t *testing.T) {
	f, _, _ := retryFixture(t)
	var routes []string
	f.provider.observe = func(req llmgateway.ProviderRequest) {
		routes = append(routes, req.Model.DeploymentKey)
		f.provider.err = nil
		if req.Model.DeploymentKey == "primary" {
			f.provider.err = &llmgateway.ProviderError{Outcome: llmgateway.OutcomeUnknown, Class: "server_error", StatusCode: 503}
		}
	}
	for i := range 2 {
		_, err := f.gateway.Call(t.Context(), f.callSession(f.a, fmt.Sprintf("run-%d", i)), llmgateway.CallInput{BindingKey: fmt.Sprintf("turn-%d", i), Chat: textRequest("chat")})
		if err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"primary", "primary", "primary", "primary", "primary", "primary", "backup"}
	if !reflect.DeepEqual(routes, append(append([]string{}, want...), want...)) {
		t.Fatalf("routes %v", routes)
	}
}

func TestConcurrentChatRetryBindingDoesNotDispatchTwice(t *testing.T) {
	f, _, _ := retryFixture(t)
	started, release := make(chan struct{}), make(chan struct{})
	f.provider.observe = func(llmgateway.ProviderRequest) { close(started); <-release }
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	session := f.callSession(f.a, "concurrent-run")
	in := llmgateway.CallInput{BindingKey: "same-turn", Chat: textRequest("chat")}
	done := make(chan error, 1)
	go func() { _, err := f.gateway.Call(ctx, session, in); done <- err }()
	select {
	case <-started:
	case <-ctx.Done():
		close(release)
		t.Fatal(ctx.Err())
	}
	_, duplicateErr := f.gateway.Call(ctx, session, in)
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !errors.Is(duplicateErr, llmgateway.ErrUnknown) || f.provider.attempts() != 1 {
		t.Fatalf("duplicate err %v calls %d", duplicateErr, f.provider.attempts())
	}
}

func TestChatRetryReplacesFailedStreamPreview(t *testing.T) {
	f, _, _ := retryFixture(t)
	sink := &recordingSink{}
	f.provider.observe = func(req llmgateway.ProviderRequest) {
		f.provider.err = nil
		if f.provider.calls == 1 {
			req.Sink.Delta(llmgateway.DeltaText, "未完成的旧回答")
			f.provider.err = &llmgateway.ProviderError{Outcome: llmgateway.OutcomeUnknown, Class: "incomplete_stream"}
			return
		}
		req.Sink.Delta(llmgateway.DeltaText, "完整的新回答")
	}
	_, err := f.gateway.Call(t.Context(), f.callSession(f.a, "stream-run"), llmgateway.CallInput{
		BindingKey: "stream-turn", Chat: textRequest("chat"), Stream: true, Sink: sink,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(sink.resets) != 1 {
		t.Fatalf("resets=%d", len(sink.resets))
	}
}

// Failed attempts keep their holds, so a later attempt can stop fitting. The
// route then ends with the budget as its reason instead of staying active.
func TestChatRetryStopsWhenBudgetCannotHoldAnotherAttempt(t *testing.T) {
	f, _, _ := retryFixture(t)
	probe := f.callSession(f.a, "probe-run")
	if _, err := f.gateway.Call(t.Context(), probe, llmgateway.CallInput{BindingKey: "probe-turn", Chat: textRequest("chat")}); err != nil {
		t.Fatal(err)
	}
	hold := f.count(t, "SELECT initial_reserved_tokens FROM llm_usage_reservations WHERE caller_group_id='probe-run'")
	f.provider.err = &llmgateway.ProviderError{Outcome: llmgateway.OutcomeUnknown, Class: "server_error", StatusCode: 503}
	session := f.callSession(f.a, "budget-run")
	session.GroupTokenLimit = int64(3 * hold)
	in := llmgateway.CallInput{BindingKey: "budget-turn", Chat: textRequest("chat")}
	_, err := f.gateway.Call(t.Context(), session, in)
	if !errors.Is(err, llmgateway.ErrProvidersExhausted) || !errors.Is(err, llmgateway.ErrBudget) {
		t.Fatalf("err %v", err)
	}
	if calls := f.provider.attempts(); calls != 1+3 {
		t.Fatalf("calls %d", calls)
	}
	if n := f.count(t, "SELECT count(*) FROM llm_chat_routes WHERE account_id='gw-a' AND state='exhausted'"); n != 1 {
		t.Fatalf("route left active: %d", n)
	}
	if _, err := f.gateway.Call(t.Context(), session, in); !errors.Is(err, llmgateway.ErrProvidersExhausted) || f.provider.attempts() != 4 {
		t.Fatalf("replay err=%v calls=%d", err, f.provider.attempts())
	}
}

// startInterruptedRoute leaves an active route after one failed attempt, as a
// worker that stopped during the retry wait would.
func startInterruptedRoute(t *testing.T, f *fixture, catalog *llmgateway.Catalog) (llmgateway.CallSession, llmgateway.CallInput) {
	t.Helper()
	f.provider.err = &llmgateway.ProviderError{Outcome: llmgateway.OutcomeUnknown, Class: "server_error", StatusCode: 503}
	configureRetry(t, f, catalog, func(context.Context, time.Duration) error { return context.Canceled })
	session := f.callSession(f.a, "replay-run")
	in := llmgateway.CallInput{BindingKey: "replay-turn", Chat: textRequest("chat")}
	if _, err := f.gateway.Call(t.Context(), session, in); !errors.Is(err, context.Canceled) || f.provider.attempts() != 1 {
		t.Fatalf("err %v calls %d", err, f.provider.attempts())
	}
	f.provider.err = nil
	f.now = f.now.Add(5 * time.Second)
	return session, in
}

func TestChatRouteReplaySkipsWithdrawnDeployment(t *testing.T) {
	f, catalog, _ := retryFixture(t)
	session, in := startInterruptedRoute(t, f, catalog)
	backup := routedDeployment("backup", "second")
	backup.RateLimitPerMin = 100
	backup.Price.Peak.Output *= 2
	backup.Price.OffPeak.Output *= 2
	backup.Price.Version = "backup-price"
	withdrawn, err := llmgateway.NewRoutedCatalog("retry-test", "USD", []llmgateway.Model{routedModel("backup")}, []llmgateway.Deployment{backup})
	if err != nil {
		t.Fatal(err)
	}
	var routes []string
	f.provider.observe = func(req llmgateway.ProviderRequest) { routes = append(routes, req.Model.DeploymentKey) }
	configureRetry(t, f, withdrawn, func(context.Context, time.Duration) error { return nil })
	if _, err := f.gateway.Call(t.Context(), session, in); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(routes, []string{"backup"}) {
		t.Fatalf("routes %v", routes)
	}
}

func TestChatRouteWithNoAdmittableDeploymentIsUnavailable(t *testing.T) {
	f, catalog, _ := retryFixture(t)
	session, in := startInterruptedRoute(t, f, catalog)
	primary, backup := routedDeployment("primary", "first"), routedDeployment("backup", "second")
	primary.Enabled, primary.DisabledReason = false, "未配置 PRIMARY_KEY"
	backup.Enabled, backup.DisabledReason = false, "未配置 BACKUP_KEY"
	disabled, err := llmgateway.NewRoutedCatalog("retry-test", "USD", []llmgateway.Model{routedModel("primary", "backup")}, []llmgateway.Deployment{primary, backup})
	if err != nil {
		t.Fatal(err)
	}
	configureRetry(t, f, disabled, func(context.Context, time.Duration) error { return nil })
	_, err = f.gateway.Call(t.Context(), session, in)
	if !errors.Is(err, llmgateway.ErrProvidersExhausted) || !errors.Is(err, llmgateway.ErrUnavailable) || f.provider.attempts() != 1 {
		t.Fatalf("err %v calls %d", err, f.provider.attempts())
	}
}

// A superseded attempt is failed for the turn but its money question is open;
// trusted evidence releases the hold without reviving the request.
func TestSupersededUnknownAttemptCanBeVerified(t *testing.T) {
	f, _, _ := retryFixture(t)
	f.provider.observe = func(llmgateway.ProviderRequest) {
		f.provider.err = nil
		if f.provider.calls == 1 {
			f.provider.err = &llmgateway.ProviderError{Outcome: llmgateway.OutcomeUnknown, Class: "timeout"}
		}
	}
	session := f.callSession(f.a, "verify-run")
	if _, err := f.gateway.Call(t.Context(), session, llmgateway.CallInput{BindingKey: "verify-turn", Chat: textRequest("chat")}); err != nil {
		t.Fatal(err)
	}
	var rootID string
	var status *int
	if err := f.db.QueryRow(`SELECT id,failure_http_status FROM llm_requests WHERE account_id='gw-a' AND state='failed'`).Scan(&rootID, &status); err != nil {
		t.Fatal(err)
	}
	if status != nil {
		t.Fatalf("timeout stored HTTP status %d", *status)
	}
	var view llmgateway.RequestView
	err := f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		var err error
		view, err = f.gateway.VerifyUnacceptedInTx(t.Context(), tx, rootID, "vendor-console")
		return err
	})
	if err != nil || view.State != llmgateway.StateFailed || view.RetryEligible {
		t.Fatalf("verify %+v %v", view, err)
	}
	if n := f.count(t, "SELECT count(*) FROM llm_usage_reservations WHERE account_id='gw-a' AND request_id=$1 AND remaining_hold_micros=0 AND settlement_state='released'", rootID); n != 1 {
		t.Fatal("verified hold not released")
	}
}
