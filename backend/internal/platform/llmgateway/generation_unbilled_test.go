package llmgateway_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	gw "github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func unbilledPolicy(t *testing.T) gw.PreparedPolicyGeneration {
	t.Helper()
	p, _, _ := genPolicyFixture(t, func(d *gw.GenerationPolicyDefinition, target *gw.GenerationPolicyTarget) {
		d.DeploymentID, target.DeploymentID = "unbilled", "unbilled"
		d.Billing, target.Billing = gw.GenerationBillingDisabled, gw.GenerationBillingDisabled
		d.Modes[0].Pricing = []gw.GenerationPriceComponent{}
		target.Usage = nil
	})
	return genPrepared(t, p, newInt64(1000))
}

func unbilledRuntime(t *testing.T, f *fixture, a gw.GenerationAdapter, policies ...gw.PreparedPolicyGeneration) *gw.GenerationRuntime {
	t.Helper()
	var registrations []gw.GenerationAdapterRegistration
	for _, p := range policies {
		d := p.PolicyDefinition()
		registrations = append(registrations, gw.GenerationAdapterRegistration{DeploymentID: d.DeploymentID, Version: d.AdapterVersion, Adapter: a})
	}
	r, err := gw.NewGenerationRuntime(gw.GenerationRuntimeConfig{Gateway: f.gateway, ProtectionKey: []byte(strings.Repeat("k", 32)), Adapters: registrations, Guard: func(context.Context, store.TxAccountScope, gw.GenerationGuardRequest, gw.PreparedPolicyGeneration) error {
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.database.MigrateCreativeJobs(t.Context()); err != nil {
		t.Fatal(err)
	}
	queue, err := f.database.NewJobRuntime(r.Handlers(), nil)
	if err != nil {
		t.Fatal(err)
	}
	r.SetRuntime(queue)
	err = f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		for _, p := range policies {
			if _, e := r.ConfigureDeploymentInTx(t.Context(), tx, p.PolicyDefinition(), true, f.now.Add(48*time.Hour), 0); e != nil {
				return e
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func admitUnbilledRoute(t *testing.T, f *fixture, r *gw.GenerationRuntime, policies ...gw.PreparedPolicyGeneration) gw.RequestView {
	t.Helper()
	var view gw.RequestView
	err := f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		in := gw.GenerationAdmission{CallerService: "creative_agent", CallerOperationID: uuid.NewString(), CallerGroupID: "unbilled", GroupLimitMicros: 1250000, GroupTokenLimit: 1, GroupDeadline: f.now.Add(time.Hour), ConsumerKey: "canvas:unbilled", Deadline: f.now.Add(time.Hour), Prepared: policies[0]}
		for _, p := range policies {
			in.Route = append(in.Route, gw.GenerationRouteCandidate{Prepared: p, ControlRevision: 1})
		}
		var e error
		view, e = r.PrepareRouteInTx(t.Context(), tx, in, 1)
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	return view
}

func assertUnbilledLedger(t *testing.T, f *fixture, request string) {
	t.Helper()
	var state string
	var hold int64
	var actual *int64
	if err := f.db.QueryRow(`SELECT settlement_state,remaining_hold_micros,actual_micros FROM llm_usage_reservations WHERE request_id=$1`, request).Scan(&state, &hold, &actual); err != nil {
		t.Fatal(err)
	}
	if state != "not_applicable" || hold != 0 || actual != nil {
		t.Fatalf("invented money facts: %s %d %v", state, hold, actual)
	}
	var count int
	if err := f.db.QueryRow(`SELECT count(*) FROM llm_usage_measurements WHERE attempt_id IN (SELECT id FROM llm_attempts WHERE request_id=$1)`, request).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("unexpected billing evidence: %d", count)
	}
}

func TestUnbilledGenerationLifecycle(t *testing.T) {
	for _, state := range []gw.GenerationObservationState{gw.GenerationCompleted, gw.GenerationFailed, gw.GenerationCancelled, gw.GenerationRejected, gw.GenerationUnknown} {
		t.Run(string(state), func(t *testing.T) {
			f := setupGateway(t, gw.BudgetPolicy{MonthlyLimitMicros: 1, MonthlyTokenLimit: 1})
			p := unbilledPolicy(t)
			if q := p.Quote(); q.Billing != gw.GenerationBillingDisabled || q.UpperBoundMicros != nil || len(q.Lines) != 0 {
				t.Fatalf("invented price: %+v", q)
			}
			raw, err := p.MarshalSnapshot()
			if err != nil {
				t.Fatal(err)
			}
			p, err = gw.RestorePolicyGeneration(raw, p.Fingerprint())
			if err != nil {
				t.Fatal(err)
			}
			a := &generationAdapter{submit: func(context.Context, gw.GenerationCall) (gw.GenerationEvent, error) {
				if state == gw.GenerationUnknown {
					return gw.GenerationEvent{}, gw.ErrUnavailable
				}
				if state == gw.GenerationRejected {
					return gw.GenerationEvent{EventKey: "rejected", Observation: gw.GenerationObservation{State: state, ErrorCode: "server_error"}}, nil
				}
				e := generationCompleted(f)
				e.Observation.State = state
				if state != gw.GenerationCompleted {
					e.Observation.Outputs = nil
					e.Observation.ErrorCode = "invalid_parameter"
				}
				return e, nil // retain real usage evidence without pricing it
			}}
			r := unbilledRuntime(t, f, a, p)
			view := admitUnbilledRoute(t, f, r, p)
			var progress gw.GenerationProgress
			for range 5 {
				progress, err = r.AdvanceGeneration(t.Context(), f.a, view.ID)
				if err != nil {
					t.Fatal(err)
				}
				f.now = f.now.Add(6 * time.Second)
			}
			want := 1
			if state == gw.GenerationRejected {
				want = 2
			}
			if a.submitCalls != want || a.observeCalls != 0 || progress.BillingReconciliationRequired {
				t.Fatalf("calls=%d progress=%+v", a.submitCalls, progress)
			}
			if state == gw.GenerationUnknown {
				if !progress.VerificationRequired {
					t.Fatal("unknown submission silently passed")
				}
			} else if !progress.Terminal() || progress.VerificationRequired || progress.NextAdvanceAt != nil {
				t.Fatalf("terminal=%+v", progress)
			}
			f.now = f.now.Add(25 * time.Hour)
			if _, err = r.AdvanceGeneration(t.Context(), f.a, view.ID); err != nil {
				t.Fatal(err)
			}
			assertUnbilledLedger(t, f, progress.Request.ID)
			var budgets int
			if err = f.db.QueryRow("SELECT count(*) FROM llm_budgets WHERE account_id='gw-a'").Scan(&budgets); err != nil {
				t.Fatal(err)
			}
			if budgets != 0 {
				t.Fatal("unbilled generation created a budget bucket")
			}
			var attempt string
			if err = f.db.QueryRow("SELECT id FROM llm_attempts WHERE request_id=$1", progress.Request.ID).Scan(&attempt); err != nil {
				t.Fatal(err)
			}
			_, err = f.recordGenerationInvoice(t, progress.Request.ID, generationInvoice(attempt, "unexpected-invoice", 500000))
			if !errors.Is(err, gw.ErrState) {
				t.Fatalf("unbilled call accepted invoice: %v", err)
			}
			if _, err = r.AdvanceGeneration(t.Context(), f.b, view.ID); err == nil {
				t.Fatal("cross-account read")
			}
		})
	}
}

func TestUnbilledFallbackPreservesMeteredHold(t *testing.T) {
	f := setupGateway(t, gw.BudgetPolicy{MonthlyLimitMicros: 1250000, MonthlyTokenLimit: 1})
	policy, _, _ := genPolicyFixture(t, nil)
	primary := genPrepared(t, policy, newInt64(1000))
	backup := unbilledPolicy(t)
	a := &generationAdapter{submit: func(_ context.Context, c gw.GenerationCall) (gw.GenerationEvent, error) {
		e := generationCompleted(f)
		if c.Prepared.PolicyDefinition().Billing == "" {
			e.Observation.State = gw.GenerationFailed
			e.Observation.Outputs = nil
			e.Observation.ErrorCode = "credential_refused"
			e.Usage = nil
		}
		return e, nil
	}}
	r := unbilledRuntime(t, f, a, primary, backup)
	view := admitUnbilledRoute(t, f, r, primary, backup)
	var progress gw.GenerationProgress
	for range 4 {
		var err error
		progress, err = r.AdvanceGeneration(t.Context(), f.a, view.ID)
		if err != nil {
			t.Fatal(err)
		}
		f.now = f.now.Add(6 * time.Second)
	}
	if progress.Request.State != gw.StateSucceeded || a.submitCalls != 2 || progress.BillingReconciliationRequired {
		t.Fatalf("fallback=%+v calls=%d", progress, a.submitCalls)
	}
	held, spent := f.reservedMicros(t)
	if held != 1250000 || spent != 0 {
		t.Fatalf("changed primary ledger %d/%d", held, spent)
	}
	assertUnbilledLedger(t, f, progress.Request.ID)
	var attempt string
	if err := f.db.QueryRow("SELECT id FROM llm_attempts WHERE request_id=$1", view.ID).Scan(&attempt); err != nil {
		t.Fatal(err)
	}
	if _, err := f.recordGenerationInvoice(t, view.ID, generationInvoice(attempt, "primary-invoice", 500000)); err != nil {
		t.Fatal(err)
	}
	held, spent = f.reservedMicros(t)
	if held != 0 || spent != 500000 {
		t.Fatalf("primary settlement changed %d/%d", held, spent)
	}
}

func TestUnbilledGenerationCancellationAndDuplicateWorkers(t *testing.T) {
	for _, cancelBefore := range []bool{true, false} {
		t.Run(map[bool]string{true: "cancel_before_dispatch", false: "concurrent_workers"}[cancelBefore], func(t *testing.T) {
			f := setupGateway(t, defaultBudget())
			p := unbilledPolicy(t)
			var submits atomic.Int32
			a := &countedUnbilledAdapter{submit: func(context.Context, gw.GenerationCall) (gw.GenerationEvent, error) {
				submits.Add(1)
				return generationCompleted(f), nil
			}}
			r := unbilledRuntime(t, f, a, p)
			view := admitUnbilledRoute(t, f, r, p)
			if cancelBefore {
				err := f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error { _, err := r.CancelRouteInTx(t.Context(), tx, view.ID); return err })
				if err != nil {
					t.Fatal(err)
				}
			}
			errs := make(chan error, 2)
			for range 2 {
				go func() { _, err := r.AdvanceGeneration(t.Context(), f.a, view.ID); errs <- err }()
			}
			for range 2 {
				if err := <-errs; err != nil {
					t.Fatal(err)
				}
			}
			want := int32(1)
			if cancelBefore {
				want = 0
			}
			if submits.Load() != want {
				t.Fatalf("submits=%d", submits.Load())
			}
			assertUnbilledLedger(t, f, view.ID)
		})
	}
}

type countedUnbilledAdapter struct {
	submit func(context.Context, gw.GenerationCall) (gw.GenerationEvent, error)
}

func (a *countedUnbilledAdapter) Submit(ctx context.Context, c gw.GenerationCall) (gw.GenerationEvent, error) {
	return a.submit(ctx, c)
}
func (*countedUnbilledAdapter) Observe(context.Context, gw.GenerationCall) (gw.GenerationEvent, error) {
	return gw.GenerationEvent{}, gw.ErrProtocol
}
func (*countedUnbilledAdapter) Cancel(context.Context, gw.GenerationCall) (gw.GenerationEvent, error) {
	return gw.GenerationEvent{}, gw.ErrProtocol
}

func TestUnbilledPolicyCannotDisableMeteredTarget(t *testing.T) {
	_, d, target := genPolicyFixture(t, nil)
	d.Billing = gw.GenerationBillingDisabled
	d.Modes[0].Pricing = []gw.GenerationPriceComponent{}
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = gw.CompileGenerationPolicy(raw, target); !errors.Is(err, gw.ErrCapability) {
		t.Fatalf("disabled untrusted billing: %v", err)
	}
}

func TestUnbilledGroupAPIs(t *testing.T) {
	for _, method := range []string{"lock", "release"} {
		t.Run(method, func(t *testing.T) {
			f := setupGateway(t, defaultBudget())
			p := unbilledPolicy(t)
			r := unbilledRuntime(t, f, &generationAdapter{}, p)
			view := admitUnbilledRoute(t, f, r, p)
			err := f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
				if method == "lock" {
					_, err := f.gateway.LockCallerGroupInTx(t.Context(), tx, "creative_agent", "unbilled")
					return err
				}
				return f.gateway.ReleaseUnusedGroupInTx(t.Context(), tx, "creative_agent", "unbilled")
			})
			if err != nil {
				t.Fatalf("group API rejected valid unbilled group: %v", err)
			}
			assertUnbilledLedger(t, f, view.ID)
			if method == "release" {
				got, err := f.gateway.Get(t.Context(), f.a, view.ID)
				if err != nil || got.State != gw.StateCancelled {
					t.Fatalf("cleanup did not cancel: %+v %v", got, err)
				}
				var consumer string
				if err := f.db.QueryRow("SELECT state FROM llm_result_consumers WHERE request_id=$1", view.ID).Scan(&consumer); err != nil {
					t.Fatal(err)
				}
				if consumer != "abandoned" {
					t.Fatalf("consumer=%s", consumer)
				}
			}
			var buckets int
			if err := f.db.QueryRow("SELECT count(*) FROM llm_budgets WHERE account_id='gw-a'").Scan(&buckets); err != nil {
				t.Fatal(err)
			}
			if buckets != 0 {
				t.Fatal("group API created a dummy budget bucket")
			}
		})
	}
}
