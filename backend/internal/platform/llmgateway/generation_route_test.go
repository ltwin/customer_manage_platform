package llmgateway_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/platform/jobs"
	gw "github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func TestGenerationRoutePriorityAndRecovery(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		unknown    bool
		calls      int
	}{
		{"retry_then_backup", "server_error", false, 3},
		{"credentials_skip", "credential_refused", false, 2},
		{"invalid_stops", "invalid_parameter", false, 1},
		{"moderation_stops", "content_rejected", false, 1},
		{"unknown_never_resends", "", true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setupGateway(t, defaultBudget())
			var sent []string
			a := &generationAdapter{submit: func(_ context.Context, c gw.GenerationCall) (gw.GenerationEvent, error) {
				d := c.Prepared.PolicyDefinition().DeploymentID
				sent = append(sent, d)
				if d == "deployment-2" {
					return generationCompleted(f), nil
				}
				if tc.unknown {
					return gw.GenerationEvent{}, errors.New("lost submit answer")
				}
				return gw.GenerationEvent{EventKey: "submit", Observation: gw.GenerationObservation{State: gw.GenerationRejected, ErrorCode: tc.code}}, nil
			}}
			_, p, d := generationRuntime(t, f, a)
			_, d2, target := genPolicyFixture(t, nil)
			d2.DeploymentID, target.DeploymentID = "deployment-2", "deployment-2"
			p2 := genPrepared(t, compilePolicy(t, d2, target), newInt64(1000))
			r, err := gw.NewGenerationRuntime(gw.GenerationRuntimeConfig{Gateway: f.gateway, ProtectionKey: []byte(strings.Repeat("k", 32)), Adapters: []gw.GenerationAdapterRegistration{
				{DeploymentID: d.DeploymentID, Version: d.AdapterVersion, Adapter: a},
				{DeploymentID: d2.DeploymentID, Version: d2.AdapterVersion, Adapter: a},
			}, Guard: func(context.Context, store.TxAccountScope, gw.GenerationGuardRequest, gw.PreparedPolicyGeneration) error {
				return nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			q, err := f.database.NewJobRuntime(r.Handlers(), nil)
			if err != nil {
				t.Fatal(err)
			}
			r.SetRuntime(q)
			var view gw.RequestView
			err = f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
				if _, e := r.ConfigureDeploymentInTx(t.Context(), tx, d2, true, f.now.Add(time.Hour), 0); e != nil {
					return e
				}
				in := gw.GenerationAdmission{CallerService: "creative_agent", CallerOperationID: uuid.NewString(), CallerGroupID: "route", GroupLimitMicros: 5000000, GroupDeadline: f.now.Add(time.Hour), ConsumerKey: "route", Deadline: f.now.Add(time.Hour), Prepared: p,
					Route: []gw.GenerationRouteCandidate{{Prepared: p, ControlRevision: 1}, {Prepared: p2, ControlRevision: 1}}}
				var e error
				view, e = r.PrepareRouteInTx(t.Context(), tx, in, 1)
				return e
			})
			if err != nil {
				t.Fatal(err)
			}
			var progress gw.GenerationProgress
			for range 8 {
				progress, err = r.AdvanceGeneration(t.Context(), f.a, view.ID)
				if err != nil {
					t.Fatal(err)
				}
				f.now = f.now.Add(6 * time.Second)
			}
			if len(sent) != tc.calls {
				t.Fatalf("submissions=%v want %d", sent, tc.calls)
			}
			if tc.calls > 1 && (sent[len(sent)-1] != "deployment-2" || progress.Request.State != gw.StateSucceeded) {
				t.Fatalf("route=%v progress=%+v", sent, progress)
			}
			if tc.unknown && !progress.VerificationRequired {
				t.Fatal("lost submit must require verification")
			}
			if _, err = r.AdvanceGeneration(t.Context(), f.b, view.ID); err == nil {
				t.Fatal("cross-account route readable")
			}
		})
	}
}

func routeFixture(t *testing.T, a gw.GenerationAdapter, limit int64, guards ...gw.GenerationGuard) (*fixture, *gw.GenerationRuntime, gw.RequestView) {
	t.Helper()
	f := setupGateway(t, defaultBudget())
	_, p, d := generationRuntime(t, f, a)
	p2, d2, _ := genPolicyFixture(t, func(d *gw.GenerationPolicyDefinition, target *gw.GenerationPolicyTarget) {
		d.DeploymentID = "deployment-2"
		target.DeploymentID = d.DeploymentID
	})
	r, err := gw.NewGenerationRuntime(gw.GenerationRuntimeConfig{Gateway: f.gateway, ProtectionKey: []byte(strings.Repeat("k", 32)), Adapters: []gw.GenerationAdapterRegistration{
		{DeploymentID: d.DeploymentID, Version: d.AdapterVersion, Adapter: a},
		{DeploymentID: d2.DeploymentID, Version: d2.AdapterVersion, Adapter: a},
	}, Guard: func(ctx context.Context, tx store.TxAccountScope, request gw.GenerationGuardRequest, prepared gw.PreparedPolicyGeneration) error {
		if len(guards) > 0 {
			return guards[0](ctx, tx, request, prepared)
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	q, err := f.database.NewJobRuntime(r.Handlers(), nil)
	if err != nil {
		t.Fatal(err)
	}
	r.SetRuntime(q)
	var root gw.RequestView
	err = f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		if _, e := r.ConfigureDeploymentInTx(t.Context(), tx, d2, true, f.now.Add(time.Hour), 0); e != nil {
			return e
		}
		var e error
		root, e = r.PrepareRouteInTx(t.Context(), tx, gw.GenerationAdmission{CallerService: "creative_agent", CallerOperationID: uuid.NewString(), CallerGroupID: "route", GroupLimitMicros: limit, GroupDeadline: f.now.Add(time.Hour), ConsumerKey: "route", Deadline: f.now.Add(time.Hour), Prepared: p, Route: []gw.GenerationRouteCandidate{{Prepared: p, ControlRevision: 1}, {Prepared: genPrepared(t, p2, newInt64(1000)), ControlRevision: 1}}}, 1)
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	return f, r, root
}

func TestGenerationRouteFailedTaskKeepsUnknownCost(t *testing.T) {
	a := &generationAdapter{}
	a.submit = func(_ context.Context, c gw.GenerationCall) (gw.GenerationEvent, error) {
		return gw.GenerationEvent{EventKey: "failed", Observation: gw.GenerationObservation{State: gw.GenerationFailed, TaskID: c.AttemptID, ErrorCode: "credential_refused"}}, nil
	}
	a.observe = a.submit
	f, r, root := routeFixture(t, a, 5_000_000)
	for range 4 {
		_, err := r.AdvanceGeneration(t.Context(), f.a, root.ID)
		if err != nil {
			t.Fatal(err)
		}
		f.now = f.now.Add(6 * time.Second)
	}
	if a.submitCalls != 2 {
		t.Fatalf("calls=%d", a.submitCalls)
	}
	held, spent := f.reservedMicros(t)
	if held != 2_500_000 || spent != 0 {
		t.Fatalf("unknown holds=%d spent=%d", held, spent)
	}
	if f.count(t, "SELECT count(*) FROM llm_generation_route_calls WHERE account_id='gw-a'") != 2 {
		t.Fatal("missing separately metered calls")
	}
}

func TestGenerationRouteBudgetRefusalDoesNotReleasePriorCost(t *testing.T) {
	a := &generationAdapter{submit: func(_ context.Context, c gw.GenerationCall) (gw.GenerationEvent, error) {
		return gw.GenerationEvent{EventKey: "failed", Observation: gw.GenerationObservation{State: gw.GenerationFailed, TaskID: c.AttemptID, ErrorCode: "credential_refused"}}, nil
	}}
	f, r, root := routeFixture(t, a, 1_250_000)
	for range 3 {
		_, err := r.AdvanceGeneration(t.Context(), f.a, root.ID)
		if err != nil {
			t.Fatal(err)
		}
		f.now = f.now.Add(6 * time.Second)
	}
	if a.submitCalls != 1 {
		t.Fatal("second call exceeded budget")
	}
	held, _ := f.reservedMicros(t)
	if held != 1_250_000 {
		t.Fatalf("lost hold %d", held)
	}
}

func TestGenerationRouteCancelStopsFallback(t *testing.T) {
	a := &generationAdapter{submit: func(context.Context, gw.GenerationCall) (gw.GenerationEvent, error) {
		return gw.GenerationEvent{EventKey: "submit", Observation: gw.GenerationObservation{State: gw.GenerationRejected, ErrorCode: "credential_refused"}}, nil
	}}
	f, r, root := routeFixture(t, a, 5_000_000)
	if _, err := r.AdvanceGeneration(t.Context(), f.a, root.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error { _, err := r.CancelRouteInTx(t.Context(), tx, root.ID); return err }); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(time.Minute)
	if _, err := r.AdvanceGeneration(t.Context(), f.a, root.ID); err != nil {
		t.Fatal(err)
	}
	if a.submitCalls != 1 {
		t.Fatal("cancelled route started fallback")
	}
}

func TestGenerationRouteConcurrentAdvanceSubmitsOnce(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	a := &generationAdapter{submit: func(_ context.Context, c gw.GenerationCall) (gw.GenerationEvent, error) {
		close(entered)
		<-release
		return gw.GenerationEvent{EventKey: "accepted", Observation: gw.GenerationObservation{State: gw.GenerationAccepted, TaskID: c.AttemptID}}, nil
	}}
	f, r, root := routeFixture(t, a, 5_000_000)
	result := make(chan error, 1)
	go func() { _, err := r.AdvanceGeneration(t.Context(), f.a, root.ID); result <- err }()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("submit not reached")
	}
	_, err := r.AdvanceGeneration(t.Context(), f.a, root.ID)
	close(release)
	if first := <-result; first != nil {
		t.Fatal(first)
	}
	if err != nil {
		t.Fatal(err)
	}
	if a.submitCalls != 1 {
		t.Fatal("duplicate dispatch")
	}
}

func TestGenerationRoutePerCallJobDisabledBeforeDispatch(t *testing.T) {
	a := &generationAdapter{submit: func(_ context.Context, c gw.GenerationCall) (gw.GenerationEvent, error) {
		return gw.GenerationEvent{EventKey: "accepted", Observation: gw.GenerationObservation{State: gw.GenerationAccepted, TaskID: c.AttemptID}}, nil
	}}
	f, r, root := routeFixture(t, a, 5_000_000)
	if err := f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		_, err := tx.Update(t.Context(), "llm_generation_deployments", "enabled=FALSE", "deployment_id IN (SELECT deployment_id FROM llm_generation_requests WHERE account_id=$1 AND request_id=$2)", root.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]any{"request_id": root.ID})
	if err := r.Handlers()[0].Work(t.Context(), f.a, jobs.Request{Payload: payload}); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if _, err := r.AdvanceGeneration(t.Context(), f.a, root.ID); err != nil {
			t.Fatal(err)
		}
		f.now = f.now.Add(6 * time.Second)
	}
	if a.submitCalls != 1 {
		t.Fatalf("backup calls=%d", a.submitCalls)
	}
	if f.count(t, "SELECT count(*) FROM llm_generation_route_calls WHERE ordinal=1") != 1 {
		t.Fatal("backup not selected")
	}
}

func TestGenerationRouteStopAfterSwitch(t *testing.T) {
	a := &generationAdapter{submit: func(context.Context, gw.GenerationCall) (gw.GenerationEvent, error) {
		return gw.GenerationEvent{EventKey: "submit", Observation: gw.GenerationObservation{State: gw.GenerationRejected, ErrorCode: "credential_refused"}}, nil
	}}
	pause := errors.New("caller paused")
	f, r, root := routeFixture(t, a, 5_000_000, func(ctx context.Context, tx store.TxAccountScope, call gw.GenerationGuardRequest, p gw.PreparedPolicyGeneration) error {
		if p.PolicyDefinition().DeploymentID == "deployment-2" {
			selected, err := tx.Exists(ctx, "llm_generation_routes", "root_request_id=$2 AND current_request_id=$3", call.RootRequestID, call.Call.ID)
			if err != nil {
				return err
			}
			if selected {
				return pause
			}
		}
		return nil
	})
	if _, err := r.AdvanceGeneration(t.Context(), f.a, root.ID); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(6 * time.Second)
	_, err := r.AdvanceGeneration(t.Context(), f.a, root.ID)
	if !errors.Is(err, pause) {
		t.Fatalf("expected queued backup: %v", err)
	}
	var current string
	if err = f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		var e error
		e = tx.QueryRow(t.Context(), "llm_generation_routes", "current_request_id", "root_request_id=$2", root.ID).Scan(&current)
		if e != nil {
			return e
		}
		stopped, e := r.StopUndispatchedInTx(t.Context(), tx, root.ID)
		if e == nil && !stopped {
			t.Error("backup not stopped")
		}
		return e
	}); err != nil {
		t.Fatal(err)
	}
	if current == root.ID {
		t.Fatal("did not switch")
	}
	f.now = f.now.Add(2 * time.Minute)
	if _, err = r.AdvanceGeneration(t.Context(), f.a, current); !errors.Is(err, gw.ErrState) {
		t.Fatalf("business API accepted deployment call ID: %v", err)
	}
	payload, _ := json.Marshal(map[string]string{"request_id": current})
	if err = r.Handlers()[0].Work(t.Context(), f.a, jobs.Request{Payload: payload}); err != nil {
		t.Fatal(err)
	}
	if _, err = r.AdvanceGeneration(t.Context(), f.a, root.ID); err != nil {
		t.Fatal(err)
	}
	if a.submitCalls != 1 {
		t.Fatal("stopped backup submitted")
	}
}

func TestGenerationRouteExplicitKeyAudit(t *testing.T) {
	f, _, root := routeFixture(t, &generationAdapter{}, 5_000_000)
	query, err := os.ReadFile("../../../../docs/dev/creative-generation-route-audit.sql")
	if err != nil {
		t.Fatal(err)
	}
	audit := func(want bool) {
		t.Helper()
		rows, e := f.db.Query(string(query))
		if e != nil {
			t.Fatal(e)
		}
		defer func() { _ = rows.Close() }()
		if rows.Next() != want {
			t.Fatalf("audit anomaly expected %v", want)
		}
		if e = rows.Err(); e != nil {
			t.Fatal(e)
		}
	}
	audit(false)
	// Raw SQL deliberately bypasses the scoped writer, proving the read-only audit catches corruption.
	if _, err = f.db.Exec("UPDATE llm_generation_routes SET current_request_id='foreign-request' WHERE root_request_id=$1", root.ID); err != nil {
		t.Fatal(err)
	}
	audit(true)
}

func TestGenerationRouteCancelRacesWithFallback(t *testing.T) {
	for round := range 3 {
		t.Run(fmt.Sprint(round), func(t *testing.T) {
			a := &generationAdapter{submit: func(context.Context, gw.GenerationCall) (gw.GenerationEvent, error) {
				return gw.GenerationEvent{EventKey: "submit", Observation: gw.GenerationObservation{State: gw.GenerationRejected, ErrorCode: "credential_refused"}}, nil
			}}
			f, r, root := routeFixture(t, a, 5_000_000)
			if _, err := r.AdvanceGeneration(t.Context(), f.a, root.ID); err != nil {
				t.Fatal(err)
			}
			f.now = f.now.Add(6 * time.Second)
			start := make(chan struct{})
			results := make(chan error, 2)
			go func() { <-start; _, err := r.AdvanceGeneration(t.Context(), f.a, root.ID); results <- err }()
			go func() {
				<-start
				results <- f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error { _, err := r.CancelRouteInTx(t.Context(), tx, root.ID); return err })
			}()
			close(start)
			for range 2 {
				if err := <-results; err != nil {
					t.Fatal(err)
				}
			}
			calls := a.submitCalls
			if calls > 2 {
				t.Fatalf("unbounded calls %d", calls)
			}
			for range 3 {
				f.now = f.now.Add(time.Minute)
				if _, err := r.AdvanceGeneration(t.Context(), f.a, root.ID); err != nil {
					t.Fatal(err)
				}
			}
			if a.submitCalls != calls {
				t.Fatal("cancelled route created another submission")
			}
			if f.count(t, "SELECT count(*) FROM llm_generation_routes WHERE state='cancelled'") != 1 {
				t.Fatal("cancel lost to fallback")
			}
		})
	}
}
