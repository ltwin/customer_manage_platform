package llmgateway_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func TestCallStagesJoinBusinessAdmissionAndConsumption(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	session := f.callSession(f.a, "generation-stages")
	session.Deadline = session.Deadline.Add(123 * time.Nanosecond)
	session.Admit = func(tx store.TxAccountScope) error { return tx.RequireCreativeCapability(t.Context(), "manual_write") }
	key := "execution-one"
	chat := textRequest("chat")
	rejected := errors.New("business admission rolled back")
	prepare := func(tx store.TxAccountScope) (llmgateway.RequestView, error) {
		return f.gateway.PrepareCallInTx(t.Context(), tx, session, key, chat)
	}
	err := f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		if _, err := prepare(tx); err != nil {
			return err
		}
		if err := tx.Insert(t.Context(), "customers", []string{"id", "display_name", "channel"}, "cus-generation-admission", "受理", "other"); err != nil {
			return err
		}
		return rejected
	})
	if !errors.Is(err, rejected) {
		t.Fatal(err)
	}
	if f.count(t, `SELECT count(*) FROM llm_requests`)+f.count(t, `SELECT count(*) FROM llm_usage_reservations`)+f.count(t, `SELECT count(*) FROM customers WHERE id='cus-generation-admission'`) != 0 {
		t.Fatal("rollback left a request, reservation, or business row")
	}
	var prepared llmgateway.RequestView
	if err = f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error { var err error; prepared, err = prepare(tx); return err }); err != nil {
		t.Fatal(err)
	}
	if f.provider.attempts() != 0 {
		t.Fatal("admission triggered a provider dispatch")
	}
	progress, err := f.gateway.AdvanceCall(t.Context(), session, key, chat, false)
	if err != nil || progress.Request.ID != prepared.ID || progress.Request.State != llmgateway.StateSucceeded || !progress.Dispatched {
		t.Fatalf("stage progress: %+v %v", progress, err)
	}
	if f.consumerState(t, prepared.ID) != "pending" {
		t.Fatal("advance released result retention before consumption")
	}
	writes := 0
	save := func(tx store.TxAccountScope, result llmgateway.Result) error {
		writes++
		return tx.Insert(t.Context(), "customers", []string{"id", "display_name", "channel"}, "cus-generation-result", result.Text, "other")
	}
	if err = f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		_, err := f.gateway.ConsumeCallInTx(t.Context(), tx, session, key, func(result llmgateway.Result) error {
			if err := save(tx, result); err != nil {
				return err
			}
			return rejected
		})
		return err
	}); !errors.Is(err, rejected) {
		t.Fatal(err)
	}
	if f.consumerState(t, prepared.ID) != "pending" || f.count(t, `SELECT count(*) FROM customers WHERE id='cus-generation-result'`) != 0 {
		t.Fatal("consumption rollback was not atomic")
	}
	progress, err = f.gateway.AdvanceCall(t.Context(), session, key, chat, false)
	if err != nil || progress.Dispatched || f.provider.attempts() != 1 {
		t.Fatalf("resume dispatched again: %+v %v", progress, err)
	}
	for i := 0; i < 2; i++ {
		if err = f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
			result, err := f.gateway.ConsumeCallInTx(t.Context(), tx, session, key, func(result llmgateway.Result) error { return save(tx, result) })
			if err == nil && result.AlreadyConsumed != (i == 1) {
				t.Fatal("consumption replay identity mismatch")
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	if writes != 2 || f.provider.attempts() != 1 {
		t.Fatalf("consumptions=%d dispatches=%d", writes, f.provider.attempts())
	}
}

func TestCallStagesDispatchOnlyOneAttemptAndNeverResendUnknown(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		t.Run(map[bool]string{false: "proven_unaccepted", true: "acceptance_unknown"}[unknown], func(t *testing.T) {
			f := setupGateway(t, defaultBudget())
			session := f.callSession(f.a, "one-step")
			session.Admit = func(store.TxAccountScope) error { return nil }
			chat := textRequest("chat")
			key := "execution-one"
			if err := f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
				_, err := f.gateway.PrepareCallInTx(t.Context(), tx, session, key, chat)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			outcome := llmgateway.OutcomeUnaccepted
			if unknown {
				outcome = llmgateway.OutcomeUnknown
			}
			f.provider.err = &llmgateway.ProviderError{Outcome: outcome, Class: "test_evidence"}
			first, err := f.gateway.AdvanceCall(t.Context(), session, key, chat, false)
			if unknown && !errors.Is(err, llmgateway.ErrUnknown) || !unknown && err != nil {
				t.Fatal(err)
			}
			if f.provider.attempts() != 1 {
				t.Fatal("one stage invocation dispatched more than once")
			}
			f.provider.err = nil
			second, err := f.gateway.AdvanceCall(t.Context(), session, key, chat, false)
			if second.Request.ID != first.Request.ID {
				t.Fatal("resume changed the logical request")
			}
			if unknown {
				if !errors.Is(err, llmgateway.ErrUnknown) || f.provider.attempts() != 1 {
					t.Fatal("unknown outcome was dispatched again", err)
				}
			} else if err != nil || f.provider.attempts() != 2 || second.Request.State != llmgateway.StateSucceeded {
				t.Fatal("unaccepted request could not retry in a later stage", err)
			}
		})
	}
}

func TestCallStagesRefuseChangedIdentityAndRevokedAuthority(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	session := f.callSession(f.a, "stable")
	session.Admit = func(store.TxAccountScope) error { return nil }
	chat := textRequest("chat")
	key := "stable-execution"
	if _, err := f.gateway.AdvanceCall(t.Context(), session, key, chat, false); !errors.Is(err, llmgateway.ErrNotFound) {
		t.Fatal("unprepared request was created implicitly", err)
	}
	if err := f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		_, err := f.gateway.PrepareCallInTx(t.Context(), tx, session, key, chat)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"group", "model", "deadline", "payload", "account", "guard"} {
		t.Run(field, func(t *testing.T) {
			changed := session
			input := textRequest("chat")
			want := llmgateway.ErrConflict
			switch field {
			case "group":
				changed.CallerGroupID = "other"
			case "model":
				changed.ModelKey = "another"
				input.ModelKey = "another"
			case "deadline":
				changed.Deadline = changed.Deadline.Add(time.Second)
			case "payload":
				input.Messages[0].Blocks[0].Text = "篡改后的输入"
			case "account":
				changed.Scope = f.b
				want = llmgateway.ErrNotFound
			case "guard":
				changed.Admit = nil
				want = llmgateway.ErrValidation
			}
			if _, err := f.gateway.AdvanceCall(t.Context(), changed, key, input, false); !errors.Is(err, want) {
				t.Fatalf("want=%v got=%v", want, err)
			}
		})
	}
	if f.provider.attempts() != 0 {
		t.Fatal("invalid stage invocation dispatched a request")
	}
	denied := errors.New("business admission revoked")
	session.Admit = func(store.TxAccountScope) error { return denied }
	if _, err := f.gateway.AdvanceCall(t.Context(), session, key, chat, false); !errors.Is(err, denied) {
		t.Fatal(err)
	}
	if f.provider.attempts() != 0 || f.count(t, `SELECT count(*) FROM llm_attempts`) != 0 {
		t.Fatal("denied admission still created a dispatch intent")
	}
	if err := f.b.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		_, err := f.gateway.PrepareCallInTx(t.Context(), tx, session, key, chat)
		return err
	}); !errors.Is(err, llmgateway.ErrValidation) {
		t.Fatal("admission accepted a cross-account transaction", err)
	}
	if err := f.b.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		_, err := f.gateway.ConsumeCallInTx(t.Context(), tx, session, key, func(llmgateway.Result) error {
			t.Fatal("cross-account consumption invoked the save callback")
			return nil
		})
		return err
	}); !errors.Is(err, llmgateway.ErrValidation) {
		t.Fatal("consumption accepted a cross-account transaction", err)
	}
}

func TestCallStagesReturnCompensatedStateAndSerializeDispatch(t *testing.T) {
	for _, expire := range []bool{false, true} {
		t.Run(map[bool]string{false: "concurrent_dispatch", true: "deadline_compensation"}[expire], func(t *testing.T) {
			f := setupGateway(t, defaultBudget())
			session := f.callSession(f.a, "dispatch")
			session.Admit = func(store.TxAccountScope) error { return nil }
			key := "one"
			chat := textRequest("chat")
			if err := f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
				_, err := f.gateway.PrepareCallInTx(t.Context(), tx, session, key, chat)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			if expire {
				f.now = f.now.Add(2 * time.Hour)
				progress, err := f.gateway.AdvanceCall(t.Context(), session, key, chat, false)
				if !errors.Is(err, llmgateway.ErrDeadline) || progress.Request.State != llmgateway.StateCancelled || progress.Request.Settlement != llmgateway.SettlementReleased || f.provider.attempts() != 0 {
					t.Fatalf("compensated state=%+v error=%v", progress, err)
				}
				return
			}
			var wait sync.WaitGroup
			errs := make([]error, 2)
			for i := range 2 {
				wait.Add(1)
				go func() {
					defer wait.Done()
					_, errs[i] = f.gateway.AdvanceCall(t.Context(), session, key, chat, false)
				}()
			}
			wait.Wait()
			for _, err := range errs {
				if err != nil && !errors.Is(err, llmgateway.ErrState) && !errors.Is(err, llmgateway.ErrUnknown) {
					t.Fatal(err)
				}
			}
			if f.provider.attempts() != 1 {
				t.Fatal("concurrent advances dispatched more than once")
			}
			progress, err := f.gateway.AdvanceCall(t.Context(), session, key, chat, false)
			if err != nil || progress.Request.State != llmgateway.StateSucceeded || progress.Dispatched {
				t.Fatal("completed result could not be replayed", err)
			}
		})
	}
}

func TestCallStagesReplayAfterDeploymentRemoval(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	session := f.callSession(f.a, "offline-replay")
	session.Admit = func(store.TxAccountScope) error { return nil }
	key := "one"
	chat := textRequest("chat")
	if err := f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		_, err := f.gateway.PrepareCallInTx(t.Context(), tx, session, key, chat)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	first, err := f.gateway.AdvanceCall(t.Context(), session, key, chat, false)
	if err != nil {
		t.Fatal(err)
	}
	_, spent, tokens := f.budget(t, "gw-a")
	// 模拟重启时移除部署：历史结果和消费不依赖当前可用模型或供应商凭证。
	catalog, err := llmgateway.NewCatalog("empty", "USD", nil)
	if err != nil {
		t.Fatal(err)
	}
	offline, err := llmgateway.New(llmgateway.Config{Catalog: catalog, DefaultBudget: defaultBudget(), Clock: func() time.Time { return f.now }})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		replay, err := offline.PrepareCallInTx(t.Context(), tx, session, key, chat)
		if err == nil && replay.ID != first.Request.ID {
			t.Fatal("resume created a new identity")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	replay, err := offline.AdvanceCall(t.Context(), session, key, chat, false)
	if err != nil || replay.Dispatched || replay.Request.Result == nil || replay.Request.Result.Text != first.Request.Result.Text {
		t.Fatalf("offline replay: %+v %v", replay, err)
	}
	observed, err := offline.RequestForBinding(t.Context(), f.a, session.CallerService, key)
	if err != nil || observed.ID != first.Request.ID {
		t.Fatal("read-only observation failed", err)
	}
	if err = f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		_, err := offline.ConsumeCallInTx(t.Context(), tx, session, key, func(result llmgateway.Result) error {
			return tx.Insert(t.Context(), "customers", []string{"id", "display_name", "channel"}, "cus-offline", result.Text, "other")
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, afterSpent, afterTokens := f.budget(t, "gw-a"); afterSpent != spent || afterTokens != tokens {
		t.Fatal("replaying a historical result changed the ledger")
	}
	if f.provider.attempts() != 1 {
		t.Fatal("offline read dispatched again")
	}
}

func TestCallStagesConcurrentAdmissionChecksActualBinding(t *testing.T) {
	for _, field := range []string{"group", "deadline"} {
		t.Run(field, func(t *testing.T) {
			f := setupGateway(t, defaultBudget())
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			first := f.callSession(f.a, "first")
			first.Admit = func(store.TxAccountScope) error { return nil }
			winner := first
			if field == "group" {
				winner.CallerGroupID = "winner"
			} else {
				winner.Deadline = winner.Deadline.Add(-time.Minute)
			}
			entered, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			var wait sync.WaitGroup
			defer func() { cancel(); unblock(); wait.Wait() }()
			// 此回调发生在首次查无绑定之后，用屏障固定另一个受理事务的提交顺序。
			first.EstimateInputTokens = func(llmgateway.ChatRequest) int64 {
				close(entered)
				select {
				case <-release:
				case <-ctx.Done():
				}
				return 0
			}
			result := make(chan error, 1)
			wait.Add(1)
			go func() {
				defer wait.Done()
				result <- f.a.WithTxScope(ctx, func(tx store.TxAccountScope) error {
					// 该行代表需要随受理失败一起回滚的业务事实。
					if err := tx.Insert(ctx, "customers", []string{"id", "display_name", "channel"}, "cus-losing-admission", "不应受理", "other"); err != nil {
						return err
					}
					_, err := f.gateway.PrepareCallInTx(ctx, tx, first, "shared-key", textRequest("chat"))
					return err
				})
			}()
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal("concurrent admission did not reach the barrier")
			}
			var accepted llmgateway.RequestView
			if err := f.a.WithTxScope(ctx, func(tx store.TxAccountScope) error {
				var err error
				accepted, err = f.gateway.PrepareCallInTx(ctx, tx, winner, "shared-key", textRequest("chat"))
				return err
			}); err != nil {
				t.Fatal(err)
			}
			unblock()
			if err := <-result; !errors.Is(err, llmgateway.ErrConflict) {
				t.Fatalf("conflicting admission did not roll back: %v", err)
			}
			if f.count(t, `SELECT count(*) FROM customers WHERE id='cus-losing-admission'`) != 0 || f.count(t, `SELECT count(*) FROM llm_requests`) != 1 || f.count(t, `SELECT count(*) FROM llm_usage_reservations`) != 1 {
				t.Fatal("concurrent admission left invalid business rows or reservations")
			}
			observed, err := f.gateway.RequestForBinding(ctx, f.a, winner.CallerService, "shared-key")
			if err != nil || observed.ID != accepted.ID || observed.GroupID != winner.CallerGroupID || !observed.Deadline.Equal(winner.Deadline) {
				t.Fatalf("committed binding changed: %+v %v", observed, err)
			}
			if f.provider.attempts() != 0 {
				t.Fatal("admission race triggered a dispatch")
			}
		})
	}
}
