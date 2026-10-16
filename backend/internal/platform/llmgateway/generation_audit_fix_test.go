package llmgateway_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	gw "github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

type externalBillingAdapter struct{ *generationAdapter }

func (*externalBillingAdapter) ProvidesUsageEvidence() bool { return false }

func TestExternalBillingTerminalStopsPolling(t *testing.T) {
	for _, state := range []gw.GenerationObservationState{gw.GenerationCompleted, gw.GenerationFailed} {
		t.Run(string(state), func(t *testing.T) {
			f := setupGateway(t, defaultBudget())
			a := &externalBillingAdapter{&generationAdapter{}}
			event := func(context.Context, gw.GenerationCall) (gw.GenerationEvent, error) {
				e := generationCompleted(f)
				e.Usage = nil
				e.Observation.State = state
				if state == gw.GenerationFailed {
					e.Observation.Outputs = nil
					e.Observation.ErrorCode = "server_error"
				}
				return e, nil
			}
			a.submit = event
			a.observe = event
			r, p, _ := generationRuntime(t, f, a)
			request := admitRuntime(t, f, r, p)
			for range 25 {
				progress, err := r.AdvanceGeneration(t.Context(), f.a, request.ID)
				if err != nil {
					t.Fatal(err)
				}
				if progress.NextAdvanceAt != nil || progress.VerificationRequired || progress.ResultVerificationRequired || !progress.BillingReconciliationRequired {
					t.Fatalf("terminal external billing still polling or blocks result: %+v", progress)
				}
				if state == gw.GenerationFailed && progress.Request.FailureClass != "server_error" {
					t.Fatal("billing state replaced failure reason")
				}
				f.now = f.now.Add(time.Hour)
			}
			if a.observeCalls != 0 || a.submitCalls != 1 {
				t.Fatalf("submit=%d observe=%d", a.submitCalls, a.observeCalls)
			}
			held, _ := f.reservedMicros(t)
			if held == 0 {
				t.Fatal("unknown fees released")
			}
			var attempt string
			if err := f.db.QueryRow("SELECT id FROM llm_attempts WHERE request_id=$1", request.ID).Scan(&attempt); err != nil {
				t.Fatal(err)
			}
			if _, err := f.recordGenerationInvoice(t, request.ID, generationInvoice(attempt, "external-invoice", 500000)); err != nil {
				t.Fatal(err)
			}
			progress, err := r.GetGeneration(t.Context(), f.a, request.ID)
			if err != nil || progress.BillingReconciliationRequired || progress.VerificationRequired || progress.Request.Settlement != gw.SettlementSettled {
				t.Fatalf("invoice did not clear billing pending: %+v %v", progress, err)
			}
		})
	}
}

func TestGenerationAdaptersProveConnectionRefusal(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	client, url := server.Client(), server.URL
	server.Close()
	rh, err := gw.NewRunningHubImages(client, url, func() (string, bool) { return "test", true })
	if err != nil {
		t.Fatal(err)
	}
	ai, err := gw.NewAICodeWithImages(client, url, "test")
	if err != nil {
		t.Fatal(err)
	}
	aps, err := gw.AICodeWithImagePolicies("USD")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		adapter gw.GenerationAdapter
		policy  gw.ImagePolicy
	}{{"runninghub", rh, runningHubPolicy(t, "gpt-image-2")}, {"aicodewith", ai, aps[0]}} {
		t.Run(tc.name, func(t *testing.T) {
			req := gw.GenerationRequest{Version: gw.GenerationContractVersion, ModelKey: tc.policy.Definition.Profile.ModelKey, Kind: gw.GenerationImage, Mode: "image", Input: gw.GenerationInput{Prompt: "audit", Parameters: json.RawMessage(`{}`)}}
			facts, e := gw.ReadGenerationFacts(t.Context(), req, imageReferences{})
			if e != nil {
				t.Fatal(e)
			}
			prepared, e := tc.policy.Policy.Prepare(req, facts)
			if e != nil {
				t.Fatal(e)
			}
			_, e = tc.adapter.Submit(t.Context(), gw.GenerationCall{Prepared: prepared.Prepared})
			if !errors.Is(e, gw.ErrNotDispatched) {
				t.Fatalf("refused connection classified as unknown: %v", e)
			}
		})
	}
}

func TestGenerationRouteReturnsPersistedCallState(t *testing.T) {
	a := &generationAdapter{submit: func(context.Context, gw.GenerationCall) (gw.GenerationEvent, error) {
		return gw.GenerationEvent{EventKey: "reject", Observation: gw.GenerationObservation{State: gw.GenerationRejected, ErrorCode: "server_error"}}, nil
	}}
	f, r, root := routeFixture(t, a, 5_000_000)
	progress, err := r.AdvanceGeneration(t.Context(), f.a, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := r.GetGeneration(t.Context(), f.a, progress.Request.ID)
	if err != nil {
		t.Fatal(err)
	}
	if progress.Request.State != stored.Request.State {
		t.Fatalf("projected %s, persisted %s", progress.Request.State, stored.Request.State)
	}
	if progress.Terminal() || progress.Route == nil || progress.Route.State != "active" || progress.Route.RootRequestID != root.ID {
		t.Fatalf("route state confused with call state: %+v", progress)
	}
}

func TestDeploymentLeaseRenewalPreservesFrozenRevision(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	r, _, d := generationRuntime(t, f, &generationAdapter{})
	err := f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		rev, e := r.EnsureDeploymentInTx(t.Context(), tx, d, 30*24*time.Hour)
		if e != nil {
			return e
		}
		if rev != 1 {
			return fmt.Errorf("lease-only renewal changed policy revision: %d", rev)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func preparedAuditImage(t *testing.T) gw.PreparedPolicyGeneration {
	t.Helper()
	p := runningHubPolicy(t, "gpt-image-2")
	req := gw.GenerationRequest{Version: gw.GenerationContractVersion, ModelKey: "gpt-image-2", Kind: gw.GenerationImage, Mode: "image", Input: gw.GenerationInput{Prompt: "audit", Parameters: json.RawMessage(`{}`)}}
	facts, err := gw.ReadGenerationFacts(t.Context(), req, imageReferences{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := p.Policy.Prepare(req, facts)
	if err != nil {
		t.Fatal(err)
	}
	return result.Prepared
}

func TestGenerationTransportFailureBoundaries(t *testing.T) {
	for _, scenario := range []string{"untrusted_certificate", "response_lost", "short_submission_budget"} {
		t.Run(scenario, func(t *testing.T) {
			var received atomic.Int32
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				received.Add(1)
				_, _ = io.Copy(io.Discard, r.Body)
				connection, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				_ = connection.Close()
			}))
			defer server.Close()
			client := server.Client()
			if scenario == "untrusted_certificate" {
				transport := http.DefaultTransport.(*http.Transport).Clone()
				transport.Proxy = nil
				client = &http.Client{Transport: transport}
				defer transport.CloseIdleConnections()
			}
			adapter, err := gw.NewRunningHubImages(client, server.URL, func() (string, bool) { return "test", true })
			if err != nil {
				t.Fatal(err)
			}
			ctx := t.Context()
			if scenario == "short_submission_budget" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, time.Second)
				defer cancel()
			}
			_, err = adapter.Submit(ctx, gw.GenerationCall{Prepared: preparedAuditImage(t)})
			if scenario == "response_lost" {
				if !errors.Is(err, gw.ErrUnavailable) || errors.Is(err, gw.ErrNotDispatched) || received.Load() != 1 {
					t.Fatalf("received request became retryable: %v, received=%d", err, received.Load())
				}
			} else if !errors.Is(err, gw.ErrNotDispatched) || received.Load() != 0 {
				t.Fatalf("pre-submit failure: %v received=%d", err, received.Load())
			}
		})
	}
}

func TestRefusedConnectionRetriesThenUsesBackup(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	client, url := server.Client(), server.URL
	server.Close()
	rh, err := gw.NewRunningHubImages(client, url, func() (string, bool) { return "test", true })
	if err != nil {
		t.Fatal(err)
	}
	prepared := preparedAuditImage(t)
	var f *fixture
	primary, backup := 0, 0
	a := &generationAdapter{submit: func(ctx context.Context, c gw.GenerationCall) (gw.GenerationEvent, error) {
		if c.Prepared.PolicyDefinition().DeploymentID == "deployment-2" {
			backup++
			return generationCompleted(f), nil
		}
		primary++
		return rh.Submit(ctx, gw.GenerationCall{Prepared: prepared})
	}}
	var runtime *gw.GenerationRuntime
	var root gw.RequestView
	f, runtime, root = routeFixture(t, a, 5_000_000)
	var progress gw.GenerationProgress
	for range 6 {
		progress, err = runtime.AdvanceGeneration(t.Context(), f.a, root.ID)
		if err != nil {
			t.Fatal(err)
		}
		f.now = f.now.Add(6 * time.Second)
	}
	if primary != 2 || backup != 1 || !progress.Terminal() || progress.Request.State != gw.StateSucceeded {
		t.Fatalf("primary=%d backup=%d progress=%+v", primary, backup, progress)
	}
	outputs, err := runtime.ImportableOutputs(t.Context(), f.a, root.ID, "creative_agent", "route")
	if err != nil || len(outputs) != 1 {
		t.Fatalf("root handoff: outputs=%d err=%v", len(outputs), err)
	}
	if progress.Route == nil || progress.Route.RootRequestID != root.ID || progress.Route.FinalRequestID != progress.Request.ID {
		t.Fatalf("route identity %+v", progress.Route)
	}
	if _, err = runtime.ImportableOutputs(t.Context(), f.a, progress.Request.ID, "creative_agent", "route"); !errors.Is(err, gw.ErrState) {
		t.Fatalf("call identity accepted at business boundary: %v", err)
	}
	err = f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		allowed, e := runtime.LockPublicationInTx(t.Context(), tx, root.ID)
		if e != nil {
			return e
		}
		if !allowed {
			return errors.New("root publication not allowed")
		}
		return runtime.ReleaseEmptyGenerationInTx(t.Context(), tx, root.ID, "creative_agent", "route")
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestExternalBillingFailureStillRoutes(t *testing.T) {
	a := &externalBillingAdapter{&generationAdapter{submit: func(_ context.Context, c gw.GenerationCall) (gw.GenerationEvent, error) {
		return gw.GenerationEvent{EventKey: "failed", Observation: gw.GenerationObservation{State: gw.GenerationFailed, TaskID: c.AttemptID, ErrorCode: "credential_refused"}}, nil
	}}}
	f, r, root := routeFixture(t, a, 5_000_000)
	for range 5 {
		if _, err := r.AdvanceGeneration(t.Context(), f.a, root.ID); err != nil {
			t.Fatal(err)
		}
		f.now = f.now.Add(6 * time.Second)
	}
	if a.submitCalls != 2 || a.observeCalls != 0 {
		t.Fatalf("submit=%d observe=%d", a.submitCalls, a.observeCalls)
	}
	held, _ := f.reservedMicros(t)
	if held != 2_500_000 {
		t.Fatalf("lost prior hold %d", held)
	}
}

func TestRouteFrozenCostBoundIsEnforced(t *testing.T) {
	a := &generationAdapter{submit: func(context.Context, gw.GenerationCall) (gw.GenerationEvent, error) {
		return gw.GenerationEvent{EventKey: "reject", Observation: gw.GenerationObservation{State: gw.GenerationRejected, ErrorCode: "credential_refused"}}, nil
	}}
	f, r, root := routeFixture(t, a, 5_000_000)
	if _, err := f.db.Exec("UPDATE llm_generation_routes SET max_cost_micros=1250000 WHERE root_request_id=$1", root.ID); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if _, err := r.AdvanceGeneration(t.Context(), f.a, root.ID); err != nil {
			t.Fatal(err)
		}
		f.now = f.now.Add(6 * time.Second)
	}
	if a.submitCalls != 1 {
		t.Fatal("created call exceeding route bound")
	}
}
