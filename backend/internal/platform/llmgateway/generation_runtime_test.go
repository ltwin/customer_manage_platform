package llmgateway_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	gw "github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

type generationAdapter struct {
	submitCalls, observeCalls, cancelCalls int
	submit                                 func(context.Context, gw.GenerationCall) (gw.GenerationEvent, error)
	observe                                func(context.Context, gw.GenerationCall) (gw.GenerationEvent, error)
	cancel                                 func(context.Context, gw.GenerationCall) (gw.GenerationEvent, error)
}

func (a *generationAdapter) Submit(ctx context.Context, c gw.GenerationCall) (gw.GenerationEvent, error) {
	a.submitCalls++
	return a.submit(ctx, c)
}
func (a *generationAdapter) Observe(ctx context.Context, c gw.GenerationCall) (gw.GenerationEvent, error) {
	a.observeCalls++
	return a.observe(ctx, c)
}
func (a *generationAdapter) Cancel(ctx context.Context, c gw.GenerationCall) (gw.GenerationEvent, error) {
	a.cancelCalls++
	return a.cancel(ctx, c)
}

func generationRuntime(t *testing.T, f *fixture, a gw.GenerationAdapter) (*gw.GenerationRuntime, gw.PreparedPolicyGeneration, gw.GenerationPolicyDefinition) {
	t.Helper()
	p, d, _ := genPolicyFixture(t, nil)
	r, err := gw.NewGenerationRuntime(gw.GenerationRuntimeConfig{Gateway: f.gateway, ProtectionKey: []byte(strings.Repeat("k", 32)), Adapters: []gw.GenerationAdapterRegistration{{DeploymentID: d.DeploymentID, Version: d.AdapterVersion, Adapter: a}}, Guard: func(context.Context, store.TxAccountScope, gw.GenerationGuardRequest, gw.PreparedPolicyGeneration) error {
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.database.MigrateCreativeJobs(t.Context()); err != nil {
		t.Fatal(err)
	}
	queue, err := f.database.NewJobRuntime(r.Handlers(), nil)
	if err != nil {
		t.Fatal(err)
	}
	r.SetRuntime(queue)
	err = f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		_, err := r.ConfigureDeploymentInTx(t.Context(), tx, d, true, f.now.Add(24*time.Hour), 0)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return r, genPrepared(t, p, newInt64(1000)), d
}
func admitRuntime(t *testing.T, f *fixture, r *gw.GenerationRuntime, p gw.PreparedPolicyGeneration) gw.RequestView {
	t.Helper()
	var view gw.RequestView
	err := f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		var err error
		view, err = r.PrepareInTx(t.Context(), tx, gw.GenerationAdmission{CallerService: "creative_agent", CallerOperationID: uuid.NewString(), CallerGroupID: "g3", GroupLimitMicros: 5000000, GroupTokenLimit: 1000, GroupDeadline: f.now.Add(time.Hour), ConsumerKey: "canvas:g3", Deadline: f.now.Add(time.Hour), Prepared: p}, 1)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return view
}
func generationCompleted(f *fixture) gw.GenerationEvent {
	return gw.GenerationEvent{EventKey: "complete", ProviderRevision: "2", Observation: gw.GenerationObservation{State: gw.GenerationCompleted, TaskID: "private-task", Outputs: []gw.GenerationOutput{{ID: "output-1", Index: 0, Kind: gw.GenerationVideo, Media: &gw.GeneratedMedia{MIME: "video/test", ByteSize: 100, FetchURL: "https://example.invalid/result?signature=secret", ExpiresAt: f.now.Add(time.Hour)}}}}, Usage: map[string]int64{"output_ms": 1000}}
}

func TestGenerationRuntimeSyncAndAsync(t *testing.T) {
	for _, async := range []bool{false, true} {
		name := "sync"
		if async {
			name = "async"
		}
		t.Run(name, func(t *testing.T) {
			f := setupGateway(t, defaultBudget())
			a := &generationAdapter{}
			a.submit = func(context.Context, gw.GenerationCall) (gw.GenerationEvent, error) {
				if async {
					return gw.GenerationEvent{EventKey: "accepted", ProviderRevision: "1", Observation: gw.GenerationObservation{State: gw.GenerationAccepted, TaskID: "private-task"}}, nil
				}
				return generationCompleted(f), nil
			}
			a.observe = func(_ context.Context, c gw.GenerationCall) (gw.GenerationEvent, error) {
				if c.TaskID != "private-task" {
					t.Errorf("task binding: %q", c.TaskID)
				}
				return generationCompleted(f), nil
			}
			r, p, _ := generationRuntime(t, f, a)
			view := admitRuntime(t, f, r, p)
			if _, err := r.AdvanceGeneration(t.Context(), f.a, view.ID); err != nil {
				t.Fatal(err)
			}
			if async {
				f.now = f.now.Add(time.Minute)
				if _, err := r.AdvanceGeneration(t.Context(), f.a, view.ID); err != nil {
					t.Fatal(err)
				}
			}
			result, err := r.GetGeneration(t.Context(), f.a, view.ID)
			if err != nil || result.Request.State != gw.StateSucceeded || len(result.Outputs) != 1 {
				t.Fatalf("result: %+v %v", result, err)
			}
			if result.Outputs[0].Media.FetchURL != "" {
				t.Fatal("public projection leaks location")
			}
			if held, spent := f.reservedMicros(t); held != 0 || spent != 125000 {
				t.Fatalf("ledger: %d %d", held, spent)
			}
			if _, err := r.AdvanceGeneration(t.Context(), f.a, view.ID); err != nil {
				t.Fatal(err)
			}
			if a.submitCalls != 1 {
				t.Fatalf("submit calls: %d", a.submitCalls)
			}
		})
	}
}
func TestGenerationRuntimeLostSubmitNeverResends(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	a := &generationAdapter{submit: func(context.Context, gw.GenerationCall) (gw.GenerationEvent, error) {
		return gw.GenerationEvent{}, errors.New("response lost")
	}}
	r, p, _ := generationRuntime(t, f, a)
	view := admitRuntime(t, f, r, p)
	if _, err := r.AdvanceGeneration(t.Context(), f.a, view.ID); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(10 * time.Minute)
	for range 3 {
		if _, err := r.AdvanceGeneration(t.Context(), f.a, view.ID); err != nil {
			t.Fatal(err)
		}
	}
	result, err := r.GetGeneration(t.Context(), f.a, view.ID)
	if err != nil || !result.VerificationRequired || result.Request.State != gw.StateUnknown {
		t.Fatalf("unknown: %+v %v", result, err)
	}
	if a.submitCalls != 1 {
		t.Fatalf("paid call repeated: %d", a.submitCalls)
	}
	if held, spent := f.reservedMicros(t); held != 1250000 || spent != 0 {
		t.Fatalf("unknown ledger: %d %d", held, spent)
	}
	if n := queryCount(t, f.db, `SELECT count(*) FROM platform_llm_limits`); n != 0 {
		t.Fatalf("unknown released supplier capacity: %d", n)
	}
}

// A submit that failed before sending anything holds neither money nor the
// shared supplier slot, and needs no human to verify it.
func TestGenerationRuntimeUnsentSubmitReleasesHoldAndSlot(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	a := &generationAdapter{submit: func(context.Context, gw.GenerationCall) (gw.GenerationEvent, error) {
		return gw.GenerationEvent{}, fmt.Errorf("%w: reference unreadable", gw.ErrNotDispatched)
	}}
	r, p, _ := generationRuntime(t, f, a)
	view := admitRuntime(t, f, r, p)
	result, err := r.AdvanceGeneration(t.Context(), f.a, view.ID)
	if err != nil || result.VerificationRequired || result.Request.State == gw.StateUnknown {
		t.Fatalf("unsent submit: %+v %v", result, err)
	}
	if held, spent := f.reservedMicros(t); held != 0 || spent != 0 {
		t.Fatalf("unsent ledger: %d %d", held, spent)
	}
	if n := queryCount(t, f.db, `SELECT count(*) FROM platform_llm_limits`); n != 0 {
		t.Fatalf("unsent submit kept supplier capacity: %d", n)
	}
}

func TestGenerationRefreshableSuccessKeepsStableEvidence(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	a := &generationAdapter{}
	completed := func(context.Context, gw.GenerationCall) (gw.GenerationEvent, error) {
		e := generationCompleted(f)
		e.RefreshableMedia = true
		e.Usage = nil
		e.Observation.Outputs[0].Media.FetchURL = "https://example.invalid/refreshed"
		return e, nil
	}
	a.submit = completed
	a.observe = completed
	r, p, _ := generationRuntime(t, f, a)
	root := admitRuntime(t, f, r, p)
	for range 3 {
		progress, err := r.AdvanceGeneration(t.Context(), f.a, root.ID)
		if err != nil {
			t.Fatal(err)
		}
		if progress.VerificationRequired {
			t.Fatal("refreshed expiry treated as conflicting evidence")
		}
		if progress.Request.State != gw.StateSucceeded {
			t.Fatalf("state %s", progress.Request.State)
		}
		f.now = f.now.Add(time.Minute)
	}
	if a.observeCalls < 2 {
		t.Fatal("cost recovery did not observe again")
	}
	if f.count(t, "SELECT count(*) FROM llm_generation_observations WHERE disposition='conflict'") != 0 {
		t.Fatal("false conflict")
	}
	if f.count(t, "SELECT result_revision FROM llm_requests WHERE id='"+root.ID+"'") != 1 {
		t.Fatal("refresh changed result version")
	}
}

func TestGenerationIgnoresExhaustedPlatformQuota(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	if _, err := f.db.Exec(`INSERT INTO platform_llm_limits (limit_key,capacity,active_count,rate_limit,window_seconds,window_start,window_count) VALUES ('generation-test',1,1,1,60,now(),1)`); err != nil {
		t.Fatal(err)
	}
	a := &generationAdapter{submit: func(_ context.Context, c gw.GenerationCall) (gw.GenerationEvent, error) {
		return gw.GenerationEvent{EventKey: "accepted", Observation: gw.GenerationObservation{State: gw.GenerationAccepted, TaskID: c.RequestID}}, nil
	}}
	r, p, _ := generationRuntime(t, f, a)
	for range 3 {
		request := admitRuntime(t, f, r, p)
		progress, err := r.AdvanceGeneration(t.Context(), f.a, request.ID)
		if err != nil || progress.VerificationRequired {
			t.Fatalf("dispatch blocked: %v %+v", err, progress)
		}
		if _, err = r.AdvanceGeneration(t.Context(), f.a, request.ID); err != nil {
			t.Fatal(err)
		}
	}
	if a.submitCalls != 3 {
		t.Fatalf("submissions=%d", a.submitCalls)
	}
	if n := queryCount(t, f.db, `SELECT count(*) FROM llm_attempts WHERE limit_key<>''`); n != 0 {
		t.Fatalf("new quota claims=%d", n)
	}
	if n := queryCount(t, f.db, `SELECT active_count FROM platform_llm_limits WHERE limit_key='generation-test'`); n != 1 {
		t.Fatalf("historical count=%d", n)
	}
}
