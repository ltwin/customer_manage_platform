package llmgateway_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	gw "github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func acceptedGeneration() gw.GenerationEvent {
	return gw.GenerationEvent{EventKey: "accepted", ProviderRevision: "1", Observation: gw.GenerationObservation{State: gw.GenerationAccepted, TaskID: "private-task"}}
}
func currentGenerationClaim(t *testing.T, f *fixture, id string) gw.GenerationClaim {
	t.Helper()
	c := gw.GenerationClaim{RequestID: id}
	if err := f.db.QueryRow(`SELECT attempt_id,epoch FROM llm_generation_tasks WHERE account_id='gw-a' AND request_id=$1 ORDER BY created_at DESC LIMIT 1`, id).Scan(&c.AttemptID, &c.Epoch); err != nil {
		t.Fatal(err)
	}
	return c
}
func recordGenerationEvent(t *testing.T, f *fixture, r *gw.GenerationRuntime, c gw.GenerationClaim, e gw.GenerationEvent) error {
	t.Helper()
	return f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error { return r.RecordGenerationObservationInTx(t.Context(), tx, c, e) })
}
func resumeGeneration(t *testing.T, f *fixture, r *gw.GenerationRuntime, id string) {
	t.Helper()
	if err := f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error { return r.ResumeVerificationInTx(t.Context(), tx, id) }); err != nil {
		t.Fatal(err)
	}
}

func TestGenerationRuntimeRestartUsesOriginalAdapterAndTask(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	a := &generationAdapter{submit: func(context.Context, gw.GenerationCall) (gw.GenerationEvent, error) { return acceptedGeneration(), nil }}
	r, p, d := generationRuntime(t, f, a)
	view := admitRuntime(t, f, r, p)
	if _, err := r.AdvanceGeneration(t.Context(), f.a, view.ID); err != nil {
		t.Fatal(err)
	}
	// 新目录停用不取消历史查询能力；重建进程从密文恢复原task，不调用Submit。
	if err := f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		_, err := r.ConfigureDeploymentInTx(t.Context(), tx, d, false, f.now.Add(time.Hour), 1)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	b := &generationAdapter{observe: func(_ context.Context, c gw.GenerationCall) (gw.GenerationEvent, error) {
		if c.TaskID != "private-task" || c.Prepared.Fingerprint() != p.Fingerprint() {
			t.Errorf("restored wrong identity")
		}
		return generationCompleted(f), nil
	}}
	rebuilt, err := gw.NewGenerationRuntime(gw.GenerationRuntimeConfig{Gateway: f.gateway, ProtectionKey: []byte(strings.Repeat("k", 32)), Adapters: []gw.GenerationAdapterRegistration{{DeploymentID: d.DeploymentID, Version: d.AdapterVersion, LimitKey: "generation-test", Concurrency: 2, RateLimit: 100, Adapter: b}}, Guard: func(context.Context, store.TxAccountScope, gw.GenerationGuardRequest, gw.PreparedPolicyGeneration) error {
		return gw.ErrCancelled
	}})
	if err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(time.Minute)
	if _, err := rebuilt.AdvanceGeneration(t.Context(), f.a, view.ID); err != nil {
		t.Fatal(err)
	}
	if a.submitCalls != 1 || b.submitCalls != 0 || b.observeCalls != 1 {
		t.Fatal("recovery resubmitted or failed to observe")
	}
	var cipher, projection []byte
	if err := f.db.QueryRow(`SELECT task_cipher FROM llm_generation_tasks WHERE account_id='gw-a' AND request_id=$1`, view.ID).Scan(&cipher); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(cipher), "private-task") {
		t.Fatal("task stored in plaintext")
	}
	if err := f.db.QueryRow(`SELECT location_cipher,projection FROM llm_generation_outputs WHERE account_id='gw-a' AND request_id=$1`, view.ID).Scan(&cipher, &projection); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(cipher), "signature") || strings.Contains(string(projection), "signature") {
		t.Fatal("output location leaked")
	}
	if _, err := rebuilt.GetGeneration(t.Context(), f.b, view.ID); !errors.Is(err, gw.ErrNotFound) {
		t.Fatalf("foreign account result: %v", err)
	}
}

func TestGenerationRuntimeOldEpochOnlyRecordsMoney(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	a := &generationAdapter{submit: func(context.Context, gw.GenerationCall) (gw.GenerationEvent, error) { return acceptedGeneration(), nil }}
	r, p, _ := generationRuntime(t, f, a)
	view := admitRuntime(t, f, r, p)
	if _, err := r.AdvanceGeneration(t.Context(), f.a, view.ID); err != nil {
		t.Fatal(err)
	}
	old := currentGenerationClaim(t, f, view.ID)
	if _, err := f.db.Exec(`UPDATE llm_generation_tasks SET epoch=epoch+1 WHERE account_id='gw-a' AND request_id=$1`, view.ID); err != nil {
		t.Fatal(err)
	}
	e := generationCompleted(f)
	if err := recordGenerationEvent(t, f, r, old, e); err != nil {
		t.Fatal(err)
	}
	progress, err := r.GetGeneration(t.Context(), f.a, view.ID)
	if err != nil || len(progress.Outputs) != 0 || progress.Request.State != gw.StateStreaming {
		t.Fatalf("stale epoch changed business: %+v %v", progress, err)
	}
	if held, spent := f.reservedMicros(t); held != 1250000 || spent != 125000 {
		t.Fatalf("stale money evidence lost: %d %d", held, spent)
	}
	c := currentGenerationClaim(t, f, view.ID)
	// 新租约核实同一事件，允许stale证据推进业务而不重复入账。
	if err := recordGenerationEvent(t, f, r, c, e); err != nil {
		t.Fatal(err)
	}
	progress, err = r.GetGeneration(t.Context(), f.a, view.ID)
	if err != nil || len(progress.Outputs) != 1 || progress.Request.State != gw.StateSucceeded {
		t.Fatalf("current completion: %+v %v", progress, err)
	}
	// 同一证据重放与更旧的运行状态均不能回退结果或再次记账。
	if err := recordGenerationEvent(t, f, r, c, e); err != nil {
		t.Fatal(err)
	}
	late := acceptedGeneration()
	late.EventKey = "late-running"
	if err := recordGenerationEvent(t, f, r, c, late); err != nil {
		t.Fatal(err)
	}
	if held, spent := f.reservedMicros(t); held != 0 || spent != 125000 {
		t.Fatalf("replay changed money: %d %d", held, spent)
	}
	if n := queryCount(t, f.db, `SELECT count(*) FROM llm_generation_outputs WHERE account_id='gw-a'`); n != 1 {
		t.Fatalf("output replay: %d", n)
	}
}

func TestGenerationRuntimeConflictingEventIsQuarantined(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	a := &generationAdapter{submit: func(context.Context, gw.GenerationCall) (gw.GenerationEvent, error) { return acceptedGeneration(), nil }}
	r, p, _ := generationRuntime(t, f, a)
	view := admitRuntime(t, f, r, p)
	if _, err := r.AdvanceGeneration(t.Context(), f.a, view.ID); err != nil {
		t.Fatal(err)
	}
	c := currentGenerationClaim(t, f, view.ID)
	e := generationCompleted(f)
	e.EventKey = "accepted"
	if err := recordGenerationEvent(t, f, r, c, e); err != nil {
		t.Fatal(err)
	}
	result, err := r.GetGeneration(t.Context(), f.a, view.ID)
	if err != nil || !result.VerificationRequired || len(result.Outputs) != 0 {
		t.Fatalf("conflict: %+v %v", result, err)
	}
	if held, spent := f.reservedMicros(t); held != 1250000 || spent != 0 {
		t.Fatalf("conflicting evidence booked: %d %d", held, spent)
	}
	if n := queryCount(t, f.db, `SELECT count(*) FROM llm_generation_observations WHERE account_id='gw-a' AND event_key='accepted'`); n != 2 {
		t.Fatalf("conflict evidence lost: %d", n)
	}
	if err := recordGenerationEvent(t, f, r, c, e); err != nil {
		t.Fatal(err)
	}
	if n := queryCount(t, f.db, `SELECT count(*) FROM llm_generation_observations WHERE account_id='gw-a'`); n != 2 {
		t.Fatalf("conflict replay duplicated: %d", n)
	}
}

func TestGenerationRuntimeCancellationKeepsFeeRecovery(t *testing.T) {
	for _, completed := range []bool{false, true} {
		name := "cancelled without fee"
		if completed {
			name = "completion races cancel"
		}
		t.Run(name, func(t *testing.T) {
			f := setupGateway(t, defaultBudget())
			a := &generationAdapter{submit: func(context.Context, gw.GenerationCall) (gw.GenerationEvent, error) { return acceptedGeneration(), nil }}
			cancelled := gw.GenerationEvent{EventKey: "cancel", ProviderRevision: "2", Observation: gw.GenerationObservation{State: gw.GenerationCancelled, TaskID: "private-task", ErrorCode: "cancelled"}}
			a.cancel = func(context.Context, gw.GenerationCall) (gw.GenerationEvent, error) {
				if completed {
					return generationCompleted(f), nil
				}
				return cancelled, nil
			}
			a.observe = func(context.Context, gw.GenerationCall) (gw.GenerationEvent, error) {
				e := cancelled
				e.EventKey = "fee"
				e.ProviderRevision = "3"
				e.Usage = map[string]int64{"output_ms": 1000}
				return e, nil
			}
			r, p, _ := generationRuntime(t, f, a)
			view := admitRuntime(t, f, r, p)
			if _, err := r.AdvanceGeneration(t.Context(), f.a, view.ID); err != nil {
				t.Fatal(err)
			}
			if err := f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
				_, err := r.CancelGenerationInTx(t.Context(), tx, view.ID)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			f.now = f.now.Add(time.Minute)
			result, err := r.AdvanceGeneration(t.Context(), f.a, view.ID)
			if err != nil || result.Request.State != gw.StateCancelled {
				t.Fatalf("cancel: %+v %v", result, err)
			}
			if !completed {
				if held, spent := f.reservedMicros(t); held != 1250000 || spent != 0 {
					t.Fatalf("cancel invented zero fee: %d %d", held, spent)
				}
				f.now = f.now.Add(time.Minute)
				if _, err := r.AdvanceGeneration(t.Context(), f.a, view.ID); err != nil {
					t.Fatal(err)
				}
			}
			if held, spent := f.reservedMicros(t); held != 0 || spent != 125000 {
				t.Fatalf("fee recovery: %d %d", held, spent)
			}
			if a.submitCalls != 1 || a.cancelCalls != 1 {
				t.Fatalf("calls: submit=%d cancel=%d", a.submitCalls, a.cancelCalls)
			}
			if n := queryCount(t, f.db, `SELECT active_count FROM platform_llm_limits WHERE limit_key='generation-test'`); n != 0 {
				t.Fatalf("terminal capacity not released: %d", n)
			}
		})
	}
}

func TestGenerationRuntimeVerifiedRejectionRearmsOnce(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	a := &generationAdapter{}
	a.submit = func(context.Context, gw.GenerationCall) (gw.GenerationEvent, error) {
		if a.submitCalls == 1 {
			return gw.GenerationEvent{EventKey: "rejected", ProviderRevision: "1", Observation: gw.GenerationObservation{State: gw.GenerationRejected, ErrorCode: "not_accepted"}}, nil
		}
		return generationCompleted(f), nil
	}
	r, p, _ := generationRuntime(t, f, a)
	view := admitRuntime(t, f, r, p)
	result, err := r.AdvanceGeneration(t.Context(), f.a, view.ID)
	if err != nil || !result.Request.RetryEligible {
		t.Fatalf("rejection: %+v %v", result, err)
	}
	if held, spent := f.reservedMicros(t); held != 0 || spent != 0 {
		t.Fatalf("rejection fee: %d %d", held, spent)
	}
	f.now = f.now.Add(time.Minute)
	result, err = r.AdvanceGeneration(t.Context(), f.a, view.ID)
	if err != nil || result.Request.State != gw.StateSucceeded {
		t.Fatalf("retry: %+v %v", result, err)
	}
	if a.submitCalls != 2 {
		t.Fatalf("attempts: %d", a.submitCalls)
	}
	if held, spent := f.reservedMicros(t); held != 0 || spent != 125000 {
		t.Fatalf("retry ledger: %d %d", held, spent)
	}
}

func TestGenerationRuntimeDeploymentStopAndConsentGuard(t *testing.T) {
	for _, stop := range []bool{true, false} {
		name := "consent revoked"
		if stop {
			name = "deployment stopped"
		}
		t.Run(name, func(t *testing.T) {
			f := setupGateway(t, defaultBudget())
			a := &generationAdapter{}
			r, p, d := generationRuntime(t, f, a)
			view := admitRuntime(t, f, r, p)
			if stop {
				if err := f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
					_, err := r.ConfigureDeploymentInTx(t.Context(), tx, d, false, f.now.Add(time.Hour), 1)
					return err
				}); err != nil {
					t.Fatal(err)
				}
			} else {
				var err error
				r, err = gw.NewGenerationRuntime(gw.GenerationRuntimeConfig{Gateway: f.gateway, ProtectionKey: []byte(strings.Repeat("k", 32)), Adapters: []gw.GenerationAdapterRegistration{{DeploymentID: d.DeploymentID, Version: d.AdapterVersion, LimitKey: "generation-test", Concurrency: 2, RateLimit: 100, Adapter: a}}, Guard: func(context.Context, store.TxAccountScope, gw.GenerationGuardRequest, gw.PreparedPolicyGeneration) error {
					return gw.ErrCancelled
				}})
				if err != nil {
					t.Fatal(err)
				}
			}
			result, err := r.AdvanceGeneration(t.Context(), f.a, view.ID)
			if err != nil || result.Request.State != gw.StateCancelled {
				t.Fatalf("guard result: %+v %v", result, err)
			}
			if a.submitCalls != 0 {
				t.Fatal("guard dispatched")
			}
			if held, spent := f.reservedMicros(t); held != 0 || spent != 0 {
				t.Fatalf("guard hold leak: %d %d", held, spent)
			}
		})
	}
}

func TestGenerationRuntimeConcurrentAdvanceOnlySubmitsOnce(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	entered, release := make(chan struct{}), make(chan struct{})
	a := &generationAdapter{submit: func(ctx context.Context, _ gw.GenerationCall) (gw.GenerationEvent, error) {
		close(entered)
		select {
		case <-release:
			return generationCompleted(f), nil
		case <-ctx.Done():
			return gw.GenerationEvent{}, ctx.Err()
		}
	}}
	r, p, _ := generationRuntime(t, f, a)
	view := admitRuntime(t, f, r, p)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := r.AdvanceGeneration(ctx, f.a, view.ID); done <- err }()
	select {
	case <-entered:
	case err := <-done:
		t.Fatalf("first submit: %v", err)
	}
	var wg sync.WaitGroup
	errs := make([]error, 3)
	for i := range errs {
		wg.Add(1)
		go func() { defer wg.Done(); _, errs[i] = r.AdvanceGeneration(ctx, f.a, view.ID) }()
	}
	wg.Wait()
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if a.submitCalls != 1 {
		t.Fatalf("concurrent submits: %d", a.submitCalls)
	}
}

func TestGenerationRuntimeDueScanRepairsDiscardedDelivery(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	r, p, _ := generationRuntime(t, f, &generationAdapter{})
	view := admitRuntime(t, f, r, p)
	if _, err := f.db.Exec(`UPDATE creative_jobs.river_job SET state='discarded',finalized_at=now()`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := r.SweepGenerationDue(t.Context(), f.database, "", 100); err != nil {
			t.Fatal(err)
		}
	}
	if n := queryCount(t, f.db, `SELECT count(*) FROM creative_jobs.river_job WHERE state='available'`); n != 1 {
		t.Fatalf("due scan did not dedupe recovery: %d", n)
	}
	var args []byte
	if err := f.db.QueryRow(`SELECT args FROM creative_jobs.river_job WHERE state='available'`).Scan(&args); err != nil {
		t.Fatal(err)
	}
	if !json.Valid(args) || !strings.Contains(string(args), view.ID) {
		t.Fatalf("wrong recovery identity: %s", args)
	}
}

// 已知任务身份的查询失败只退避不判核实，直到恢复窗口用尽；任务跑得久、
// 查询接口偶发抖动都不能把生成永久停住。
func TestGenerationRuntimeObserveRetriesKnownTaskAndChatRecoveryIsolation(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	a := &generationAdapter{submit: func(context.Context, gw.GenerationCall) (gw.GenerationEvent, error) { return acceptedGeneration(), nil }, observe: func(context.Context, gw.GenerationCall) (gw.GenerationEvent, error) {
		return gw.GenerationEvent{}, errors.New("temporary")
	}}
	r, p, _ := generationRuntime(t, f, a)
	view := admitRuntime(t, f, r, p)
	if _, err := r.AdvanceGeneration(t.Context(), f.a, view.ID); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(5 * time.Minute)
	if changed, err := f.gateway.RecoverExpired(t.Context(), f.a, 10); err != nil || changed != 0 {
		t.Fatalf("chat recovery touched generation: %d %v", changed, err)
	}
	for range 6 {
		f.now = f.now.Add(2 * time.Minute)
		if _, err := r.AdvanceGeneration(t.Context(), f.a, view.ID); err != nil {
			t.Fatal(err)
		}
	}
	progress, err := r.GetGeneration(t.Context(), f.a, view.ID)
	if err != nil || progress.VerificationRequired || a.observeCalls != 6 {
		t.Fatalf("known task stopped retrying: calls=%d %+v %v", a.observeCalls, progress, err)
	}
	if next := progress.NextAdvanceAt; next == nil || next.Sub(f.now) > time.Minute {
		t.Fatalf("backoff not capped: %v", next)
	}
	if n := queryCount(t, f.db, `SELECT active_count FROM platform_llm_limits WHERE limit_key='generation-test'`); n != 1 {
		t.Fatalf("unresolved task lost capacity: %d", n)
	}
	a.observe = func(context.Context, gw.GenerationCall) (gw.GenerationEvent, error) {
		return generationCompleted(f), nil
	}
	f.now = f.now.Add(2 * time.Minute)
	if progress, err := r.AdvanceGeneration(t.Context(), f.a, view.ID); err != nil || progress.Request.State != gw.StateSucceeded {
		t.Fatalf("recovered provider not observed: %+v %v", progress, err)
	}
}

// 恢复窗口用尽后才转人工核实，受信恢复可以继续查询同一任务。
func TestGenerationRuntimeObserveRecoveryWindowRequiresVerification(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	a := &generationAdapter{submit: func(context.Context, gw.GenerationCall) (gw.GenerationEvent, error) { return acceptedGeneration(), nil }, observe: func(context.Context, gw.GenerationCall) (gw.GenerationEvent, error) {
		return gw.GenerationEvent{}, errors.New("temporary")
	}}
	// 与真实适配器一致：取消只是再查一次同一任务。
	a.cancel = func(ctx context.Context, c gw.GenerationCall) (gw.GenerationEvent, error) { return a.observe(ctx, c) }
	r, p, _ := generationRuntime(t, f, a)
	view := admitRuntime(t, f, r, p)
	if _, err := r.AdvanceGeneration(t.Context(), f.a, view.ID); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(25 * time.Hour)
	if _, err := r.AdvanceGeneration(t.Context(), f.a, view.ID); err != nil {
		t.Fatal(err)
	}
	progress, err := r.GetGeneration(t.Context(), f.a, view.ID)
	if err != nil || !progress.VerificationRequired {
		t.Fatalf("expired recovery window kept polling: %+v %v", progress, err)
	}
	a.observe = func(context.Context, gw.GenerationCall) (gw.GenerationEvent, error) {
		return generationCompleted(f), nil
	}
	resumeGeneration(t, f, r, view.ID)
	// 生成期限已过，迟到的完成只作终态与费用证据，不再作为可交付结果。
	if progress, err := r.AdvanceGeneration(t.Context(), f.a, view.ID); err != nil || !progress.Request.Terminal() {
		t.Fatalf("manual recovery: %+v %v", progress, err)
	}
}

// 提交后持久化失败等价于worker在提交窗口消失；租约到期只能核实，不能重发。
func TestGenerationRuntimeLostPersistenceFencesSubmit(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	a := &generationAdapter{submit: func(context.Context, gw.GenerationCall) (gw.GenerationEvent, error) {
		e := generationCompleted(f)
		e.Observation.Outputs[0].Media.MIME = "video/not-supported"
		return e, nil
	}}
	r, p, _ := generationRuntime(t, f, a)
	view := admitRuntime(t, f, r, p)
	if _, err := r.AdvanceGeneration(t.Context(), f.a, view.ID); !errors.Is(err, gw.ErrProtocol) {
		t.Fatalf("expected lost persistence: %v", err)
	}
	f.now = f.now.Add(4 * time.Minute)
	result, err := r.AdvanceGeneration(t.Context(), f.a, view.ID)
	if err != nil || !result.VerificationRequired || result.Request.State != gw.StateUnknown {
		t.Fatalf("lease recovery: %+v %v", result, err)
	}
	if a.submitCalls != 1 {
		t.Fatalf("resubmitted: %d", a.submitCalls)
	}
	if held, spent := f.reservedMicros(t); held != 1250000 || spent != 0 {
		t.Fatalf("lost response changed money: %d %d", held, spent)
	}
}

type lookupGenerationAdapter struct {
	generationAdapter
	lookups int
}

func (a *lookupGenerationAdapter) LookupSubmission(_ context.Context, c gw.GenerationCall) (gw.GenerationEvent, error) {
	a.lookups++
	if c.AttemptID == "" || c.TaskID != "" {
		return gw.GenerationEvent{}, gw.ErrProtocol
	}
	return acceptedGeneration(), nil
}
func TestGenerationRuntimeUnknownSubmissionLookup(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	a := &lookupGenerationAdapter{}
	a.submit = func(context.Context, gw.GenerationCall) (gw.GenerationEvent, error) {
		return gw.GenerationEvent{}, errors.New("lost acceptance")
	}
	a.observe = func(context.Context, gw.GenerationCall) (gw.GenerationEvent, error) {
		return generationCompleted(f), nil
	}
	r, p, _ := generationRuntime(t, f, a)
	view := admitRuntime(t, f, r, p)
	for range 3 {
		if _, err := r.AdvanceGeneration(t.Context(), f.a, view.ID); err != nil {
			t.Fatal(err)
		}
		f.now = f.now.Add(time.Minute)
	}
	result, err := r.GetGeneration(t.Context(), f.a, view.ID)
	if err != nil || result.Request.State != gw.StateSucceeded || a.lookups != 1 || a.submitCalls != 1 || a.observeCalls != 1 {
		t.Fatalf("lookup recovery: %+v %v calls=%d/%d/%d", result, err, a.submitCalls, a.lookups, a.observeCalls)
	}
}

func TestGenerationRuntimeOutputSuccessDoesNotInventFee(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		name := "usage missing"
		if invalid {
			name = "invalid usage"
		}
		t.Run(name, func(t *testing.T) {
			f := setupGateway(t, defaultBudget())
			a := &generationAdapter{submit: func(context.Context, gw.GenerationCall) (gw.GenerationEvent, error) {
				e := generationCompleted(f)
				if invalid {
					e.Usage = map[string]int64{"output_ms": -1}
				} else {
					e.Usage = nil
				}
				return e, nil
			}}
			r, p, _ := generationRuntime(t, f, a)
			view := admitRuntime(t, f, r, p)
			result, err := r.AdvanceGeneration(t.Context(), f.a, view.ID)
			if err != nil || result.Request.State != gw.StateSucceeded || result.Request.Settlement == gw.SettlementSettled || len(result.Outputs) != 1 {
				t.Fatalf("output/fee independence: %+v %v", result, err)
			}
			if held, spent := f.reservedMicros(t); held != 1250000 || spent != 0 {
				t.Fatalf("unknown fee lost hold: %d %d", held, spent)
			}
			if invalid {
				if n := queryCount(t, f.db, `SELECT count(*) FROM llm_generation_deployment_blocks WHERE account_id='gw-a'`); n != 1 {
					t.Fatalf("invalid usage did not block: %d", n)
				}
			}
		})
	}
}
