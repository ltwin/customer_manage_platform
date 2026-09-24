package llmgateway_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/platform/jobs"
	gw "github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func TestGenerationRuntimeRejectedUsageCannotAuthorizeRetry(t *testing.T) {
	for _, tc := range []struct {
		name  string
		usage map[string]int64
		retry bool
	}{
		{"positive", map[string]int64{"output_ms": 1000}, false},
		{"negative", map[string]int64{"output_ms": -1}, false},
		{"unexplained", map[string]int64{"other": 1}, false},
		{"verified zero", map[string]int64{"output_ms": 0}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setupGateway(t, defaultBudget())
			event := gw.GenerationEvent{EventKey: "rejection", ProviderRevision: "1", Observation: gw.GenerationObservation{State: gw.GenerationRejected, ErrorCode: "not_accepted"}, Usage: tc.usage}
			a := &generationAdapter{submit: func(context.Context, gw.GenerationCall) (gw.GenerationEvent, error) { return event, nil }}
			r, p, _ := generationRuntime(t, f, a)
			v := admitRuntime(t, f, r, p)
			result, err := r.AdvanceGeneration(t.Context(), f.a, v.ID)
			if err != nil {
				t.Fatal(err)
			}
			if result.Request.RetryEligible != tc.retry {
				t.Fatalf("retry=%v want=%v", result.Request.RetryEligible, tc.retry)
			}
			claim := currentGenerationClaim(t, f, v.ID)
			if err := recordGenerationEvent(t, f, r, claim, event); err != nil {
				t.Fatal(err)
			}
			held, spent := f.reservedMicros(t)
			if tc.retry {
				if held != 0 || spent != 0 {
					t.Fatalf("zero rejection: %d %d", held, spent)
				}
			} else {
				result, err = r.GetGeneration(t.Context(), f.a, v.ID)
				if err != nil || !result.VerificationRequired || result.Request.RetryEligible || held != 1250000 || spent != 0 {
					t.Fatalf("contradiction: %+v %v money=%d/%d", result, err, held, spent)
				}
				if n := queryCount(t, f.db, `SELECT active_count FROM platform_llm_limits WHERE limit_key='generation-test'`); n != 1 {
					t.Fatalf("contradiction released capacity: %d", n)
				}
				if n := queryCount(t, f.db, `SELECT count(*) FROM llm_usage_measurements WHERE account_id='gw-a' AND certainty='known'`); n != 0 {
					t.Fatalf("invented known cost: %d", n)
				}
			}
		})
	}
}

func TestGenerationRuntimeExpiredPreparedIgnoresSupplierLimits(t *testing.T) {
	for _, capacity := range []bool{true, false} {
		name := "rate exhausted"
		if capacity {
			name = "capacity exhausted"
		}
		t.Run(name, func(t *testing.T) {
			f := setupGateway(t, defaultBudget())
			a := &generationAdapter{submit: func(context.Context, gw.GenerationCall) (gw.GenerationEvent, error) {
				return gw.GenerationEvent{}, errors.New("acceptance unknown")
			}}
			r, p, _ := generationRuntime(t, f, a)
			for range 2 {
				v := admitRuntime(t, f, r, p)
				if _, err := r.AdvanceGeneration(t.Context(), f.a, v.ID); err != nil {
					t.Fatal(err)
				}
			}
			v := admitRuntime(t, f, r, p)
			f.now = f.now.Add(2 * time.Hour)
			if !capacity {
				if _, err := f.db.Exec(`UPDATE platform_llm_limits SET active_count=0,window_count=100,window_start=$1 WHERE limit_key='generation-test'`, f.now); err != nil {
					t.Fatal(err)
				}
			}
			result, err := r.AdvanceGeneration(t.Context(), f.a, v.ID)
			if err != nil || result.Request.State != gw.StateCancelled || result.NextAdvanceAt != nil {
				t.Fatalf("expired request not closed: %+v %v", result, err)
			}
			if held, spent := f.reservedMicros(t); held != 2500000 || spent != 0 {
				t.Fatalf("wrong holds released: %d %d", held, spent)
			}
			if a.submitCalls != 2 {
				t.Fatalf("expired request submitted: %d", a.submitCalls)
			}
		})
	}
}

// 实际River dispatcher允许停用账号的维护，普通任务仍取消；维护不得取得新Submit。
func TestGenerationRuntimeInactiveAccountQueueRecovery(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	a := &generationAdapter{submit: func(context.Context, gw.GenerationCall) (gw.GenerationEvent, error) { return acceptedGeneration(), nil }, observe: func(context.Context, gw.GenerationCall) (gw.GenerationEvent, error) {
		return generationCompleted(f), nil
	}}
	r, p, _ := generationRuntime(t, f, a)
	running := admitRuntime(t, f, r, p)
	if _, err := r.AdvanceGeneration(t.Context(), f.a, running.ID); err != nil {
		t.Fatal(err)
	}
	prepared := admitRuntime(t, f, r, p)
	if _, err := f.db.Exec(`UPDATE accounts SET status='pending_verification' WHERE id='gw-a'`); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(time.Minute)
	handlers := append(r.Handlers(), store.JobHandler{Kind: "ordinary", Work: func(context.Context, store.AccountScope, jobs.Request) error {
		t.Error("ordinary job ran for inactive account")
		return nil
	}})
	queue, err := f.database.NewJobRuntime(handlers, nil)
	if err != nil {
		t.Fatal(err)
	}
	r.SetRuntime(queue)
	if err := f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		_, err := jobs.EnqueueInTx(t.Context(), tx.Jobs(queue), jobs.Request{Kind: "ordinary", OperationID: uuid.NewString(), CreatedAt: f.now, Payload: []byte(`{}`)})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	if err := queue.Start(ctx); err != nil {
		t.Fatal(err)
	}
	stop := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := queue.Stop(ctx); err != nil {
			t.Error(err)
		}
	}
	defer stop()
	deadline := time.Now().Add(10 * time.Second)
	completed := false
	for time.Now().Before(deadline) {
		done := queryCount(t, f.db, `SELECT count(*) FROM creative_jobs.river_job WHERE state='completed'`)
		cancelled := queryCount(t, f.db, `SELECT count(*) FROM creative_jobs.river_job WHERE state='cancelled'`)
		if done == 2 && cancelled == 1 {
			completed = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !completed {
		t.Fatal("maintenance did not complete through the real dispatcher")
	}
	stop()
	progress, err := r.GetGeneration(t.Context(), f.a, running.ID)
	if err != nil || progress.Request.State != gw.StateSucceeded {
		t.Fatalf("existing task: %+v %v", progress, err)
	}
	progress, err = r.GetGeneration(t.Context(), f.a, prepared.ID)
	if err != nil || progress.Request.State != gw.StateCancelled {
		t.Fatalf("inactive fresh task: %+v %v", progress, err)
	}
	if a.submitCalls != 1 || a.observeCalls != 1 {
		t.Fatalf("inactive calls: %d/%d", a.submitCalls, a.observeCalls)
	}
	if held, spent := f.reservedMicros(t); held != 0 || spent != 125000 {
		t.Fatalf("inactive recovery ledger: %d/%d", held, spent)
	}
}

func TestGenerationRuntimeRejectedReplayKeepsConflict(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	a := &generationAdapter{submit: func(context.Context, gw.GenerationCall) (gw.GenerationEvent, error) { return acceptedGeneration(), nil }}
	r, p, _ := generationRuntime(t, f, a)
	v := admitRuntime(t, f, r, p)
	if _, err := r.AdvanceGeneration(t.Context(), f.a, v.ID); err != nil {
		t.Fatal(err)
	}
	c := currentGenerationClaim(t, f, v.ID)
	event := gw.GenerationEvent{EventKey: "contradictory-rejection", ProviderRevision: "2", Observation: gw.GenerationObservation{State: gw.GenerationRejected, ErrorCode: "not_accepted"}, Usage: map[string]int64{"output_ms": 0}}
	for range 2 {
		if err := recordGenerationEvent(t, f, r, c, event); err != nil {
			t.Fatal(err)
		}
	}
	result, err := r.GetGeneration(t.Context(), f.a, v.ID)
	if err != nil || !result.VerificationRequired || result.NextAdvanceAt != nil || result.Request.RetryEligible {
		t.Fatalf("replayed conflict restarted recovery: %+v %v", result, err)
	}
}

func TestGenerationRuntimeNormalEventCannotClearIndependentConflict(t *testing.T) {
	for _, replay := range []bool{true, false} {
		name := "new event"
		if replay {
			name = "replay"
		}
		t.Run(name, func(t *testing.T) {
			f := setupGateway(t, defaultBudget())
			a := &generationAdapter{submit: func(context.Context, gw.GenerationCall) (gw.GenerationEvent, error) { return acceptedGeneration(), nil }}
			r, p, _ := generationRuntime(t, f, a)
			v := admitRuntime(t, f, r, p)
			if _, err := r.AdvanceGeneration(t.Context(), f.a, v.ID); err != nil {
				t.Fatal(err)
			}
			c := currentGenerationClaim(t, f, v.ID)
			bad := gw.GenerationEvent{EventKey: "independent-conflict", ProviderRevision: "2", Observation: gw.GenerationObservation{State: gw.GenerationRejected, ErrorCode: "not_accepted"}}
			if err := recordGenerationEvent(t, f, r, c, bad); err != nil {
				t.Fatal(err)
			}
			normal := acceptedGeneration()
			if !replay {
				normal.EventKey, normal.ProviderRevision = "new-normal", "3"
			}
			if err := recordGenerationEvent(t, f, r, c, normal); err != nil {
				t.Fatal(err)
			}
			result, err := r.GetGeneration(t.Context(), f.a, v.ID)
			if err != nil || !result.VerificationRequired || result.NextAdvanceAt != nil {
				t.Fatalf("normal evidence cleared independent conflict: %+v %v", result, err)
			}
			if n := queryCount(t, f.db, `SELECT count(*) FROM llm_generation_observations WHERE disposition='conflict'`); n != 1 {
				t.Fatalf("conflict evidence lost: %d", n)
			}
			if held, spent := f.reservedMicros(t); held != 1250000 || spent != 0 {
				t.Fatalf("conflict recovery changed ledger: %d/%d", held, spent)
			}
			resumeGeneration(t, f, r, v.ID)
			result, err = r.GetGeneration(t.Context(), f.a, v.ID)
			if err != nil || result.VerificationRequired || result.NextAdvanceAt == nil {
				t.Fatalf("trusted resume did not reopen recovery: %+v %v", result, err)
			}
		})
	}
}

func TestGenerationRuntimeCompletedCannotEraseTerminalConflict(t *testing.T) {
	for _, state := range []gw.GenerationObservationState{gw.GenerationCancelled, gw.GenerationFailed, gw.GenerationRejected} {
		t.Run(string(state), func(t *testing.T) {
			f := setupGateway(t, defaultBudget())
			terminal := gw.GenerationEvent{EventKey: "terminal", ProviderRevision: "1", Observation: gw.GenerationObservation{State: state, ErrorCode: "terminal"}}
			if state != gw.GenerationRejected {
				terminal.Observation.TaskID = "private-task"
			}
			a := &generationAdapter{submit: func(context.Context, gw.GenerationCall) (gw.GenerationEvent, error) { return terminal, nil }}
			r, p, _ := generationRuntime(t, f, a)
			v := admitRuntime(t, f, r, p)
			before, err := r.AdvanceGeneration(t.Context(), f.a, v.ID)
			if err != nil {
				t.Fatal(err)
			}
			heldBefore, spentBefore := f.reservedMicros(t)
			wantState := before.Request.State
			if state == gw.GenerationRejected {
				// 迟到反证撤回未受理重试资格，并按未决费用重新保留原上界。
				wantState, heldBefore = gw.StateUnknown, 1250000
			}
			c := currentGenerationClaim(t, f, v.ID)
			for range 2 {
				if err := recordGenerationEvent(t, f, r, c, generationCompleted(f)); err != nil {
					t.Fatal(err)
				}
			}
			result, err := r.GetGeneration(t.Context(), f.a, v.ID)
			if err != nil || !result.VerificationRequired || result.NextAdvanceAt != nil || result.Request.RetryEligible || result.Request.State != wantState || len(result.Outputs) != 0 {
				t.Fatalf("completion erased terminal conflict: before=%+v after=%+v err=%v", before, result, err)
			}
			if n := queryCount(t, f.db, `SELECT count(*) FROM llm_generation_observations WHERE disposition='conflict'`); n != 1 {
				t.Fatalf("terminal conflict not persisted once: %d", n)
			}
			if held, spent := f.reservedMicros(t); held != heldBefore || spent != spentBefore {
				t.Fatalf("terminal conflict changed ledger: %d/%d -> %d/%d", heldBefore, spentBefore, held, spent)
			}
		})
	}
}

func TestGenerationRuntimeRejectedContradictionWithoutUsageReopensSettlement(t *testing.T) {
	for _, budgetAvailable := range []bool{true, false} {
		name := "budget exhausted"
		if budgetAvailable {
			name = "budget available"
		}
		t.Run(name, func(t *testing.T) {
			f := setupGateway(t, defaultBudget())
			rejected := gw.GenerationEvent{EventKey: "rejected", ProviderRevision: "1", Observation: gw.GenerationObservation{State: gw.GenerationRejected, ErrorCode: "not_accepted"}}
			a := &generationAdapter{submit: func(context.Context, gw.GenerationCall) (gw.GenerationEvent, error) { return rejected, nil }}
			r, p, _ := generationRuntime(t, f, a)
			v := admitRuntime(t, f, r, p)
			if _, err := r.AdvanceGeneration(t.Context(), f.a, v.ID); err != nil {
				t.Fatal(err)
			}
			if !budgetAvailable {
				if _, err := f.db.Exec(`UPDATE llm_budgets SET limit_micros=0 WHERE account_id='gw-a'`); err != nil {
					t.Fatal(err)
				}
			}
			c := currentGenerationClaim(t, f, v.ID)
			completed := generationCompleted(f)
			completed.Usage = nil
			for range 2 {
				if err := recordGenerationEvent(t, f, r, c, completed); err != nil {
					t.Fatal(err)
				}
			}
			result, err := r.GetGeneration(t.Context(), f.a, v.ID)
			if err != nil || !result.VerificationRequired || result.NextAdvanceAt != nil || result.Request.State != gw.StateUnknown || result.Request.RetryEligible || result.Request.Settlement != gw.SettlementPartial || len(result.Outputs) != 0 {
				t.Fatalf("unpriced contradiction not quarantined: %+v %v", result, err)
			}
			wantHold := int64(0)
			if budgetAvailable {
				wantHold = 1250000
			}
			if held, spent := f.reservedMicros(t); held != wantHold || spent != 0 {
				t.Fatalf("unknown cost exposure: %d/%d want hold=%d", held, spent, wantHold)
			}
			if n := queryCount(t, f.db, `SELECT count(*) FROM llm_measurement_dispositions WHERE state='disputed'`); n != 1 {
				t.Fatalf("missing or duplicated fee dispute: %d", n)
			}
			f.now = f.now.Add(2 * time.Hour)
			if _, err := r.AdvanceGeneration(t.Context(), f.a, v.ID); err != nil {
				t.Fatal(err)
			}
			if held, spent := f.reservedMicros(t); held != wantHold || spent != 0 || a.submitCalls != 1 {
				t.Fatalf("expiry released disputed exposure or resubmitted: %d/%d calls=%d", held, spent, a.submitCalls)
			}
		})
	}
}
