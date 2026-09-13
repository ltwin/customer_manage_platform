package llmgateway_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func (f *fixture) recoveryTurn(t *testing.T, scope store.AccountScope) (llmgateway.Permit, llmgateway.ChatRequest) {
	t.Helper()
	r := f.reserve(t, scope, uuid.NewString(), turnOptions{})
	v, c, err := f.prepare(t, scope, r.ID, uuid.NewString(), turnOptions{})
	if err != nil {
		t.Fatal(err)
	}
	p, err := f.begin(t, scope, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	return p, c
}
func (f *fixture) activeLLMSlots(t *testing.T) int {
	t.Helper()
	var n int
	if err := f.db.QueryRow(`SELECT active_count FROM platform_llm_limits WHERE limit_key='dep-1'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
func TestOrphanedDispatchRecoveryFencesPermitsAndRestoresCapacity(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	p, c := f.recoveryTurn(t, f.a)
	f.recoveryTurn(t, f.a)
	if n, e := f.gateway.RecoverExpired(t.Context(), f.a, 100); e != nil || n != 0 {
		t.Fatalf("live=%d %v", n, e)
	}
	f.now = f.now.Add(4 * time.Minute)
	if n, e := f.gateway.RecoverExpired(t.Context(), f.b, 100); e != nil || n != 0 {
		t.Fatalf("other account=%d %v", n, e)
	}
	if n, e := f.gateway.RecoverExpired(t.Context(), f.a, 100); e != nil || n != 2 {
		t.Fatalf("recovery=%d %v", n, e)
	}
	if n, e := f.gateway.RecoverExpired(t.Context(), f.a, 100); e != nil || n != 0 {
		t.Fatalf("repeat=%d %v", n, e)
	}
	if n := f.activeLLMSlots(t); n != 0 {
		t.Fatalf("slots=%d", n)
	}
	u, e := f.gateway.GetUsage(t.Context(), f.a, p.RequestID)
	if e != nil {
		t.Fatal(e)
	}
	if u.HoldMicros == 0 || u.HoldTokens == 0 || u.Settlement != llmgateway.SettlementUnknown {
		t.Fatalf("hold lost: %+v", u)
	}
	if _, e = f.gateway.Execute(t.Context(), f.a, limitsOf, p, c, false); !errors.Is(e, llmgateway.ErrConflict) {
		t.Fatalf("stale permit=%v", e)
	}
	if f.provider.attempts() != 0 {
		t.Fatal("stale permit sent")
	}
	if _, _, e = f.turn(t, f.b, turnOptions{}); e != nil {
		t.Fatalf("other account blocked: %v", e)
	}
	if e = f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		_, e := f.gateway.VerifyUnacceptedInTx(t.Context(), tx, p.RequestID, "operator-confirmed-no-send")
		return e
	}); e != nil {
		t.Fatal(e)
	}
}
func TestExpiredPermitCannotStartBeforeSweep(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	p, c := f.recoveryTurn(t, f.a)
	f.now = f.now.Add(2 * time.Minute)
	if _, e := f.gateway.Execute(t.Context(), f.a, limitsOf, p, c, false); !errors.Is(e, llmgateway.ErrConflict) {
		t.Fatal(e)
	}
	if f.provider.attempts() != 0 {
		t.Fatal("expired permit sent")
	}
}
func TestResultPersistenceFailureBecomesRecoverableUnknown(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	p, c := f.recoveryTurn(t, f.a)
	// A database failure rolls back the same first finish transaction that a
	// persistence timeout aborts, without depending on a 30-second wall clock.
	_, e := f.db.Exec(`CREATE FUNCTION reject_llm_success() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.state='succeeded' THEN RAISE EXCEPTION 'forced persistence failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_llm_success BEFORE UPDATE ON llm_requests FOR EACH ROW EXECUTE FUNCTION reject_llm_success()`)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.gateway.Execute(t.Context(), f.a, limitsOf, p, c, false); e == nil {
		t.Fatal("expected persistence error")
	}
	if _, e = f.db.Exec(`DROP TRIGGER reject_llm_success ON llm_requests`); e != nil {
		t.Fatal(e)
	}
	f.now = f.now.Add(4 * time.Minute)
	if n, e := f.gateway.RecoverExpired(t.Context(), f.a, 100); e != nil || n != 1 {
		t.Fatalf("recovery=%d %v", n, e)
	}
	v, e := f.gateway.Get(t.Context(), f.a, p.RequestID)
	if e != nil {
		t.Fatal(e)
	}
	if v.State != llmgateway.StateUnknown || f.activeLLMSlots(t) != 0 {
		t.Fatalf("state=%s slots=%d", v.State, f.activeLLMSlots(t))
	}
	if f.provider.attempts() != 1 {
		t.Fatal("resent paid request")
	}
}
func TestLateResultAfterRecoveryDoesNotReleaseAnotherSlot(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	p, c := f.recoveryTurn(t, f.a)
	f.provider.observe = func(llmgateway.ProviderRequest) {
		f.now = f.now.Add(4 * time.Minute)
		if n, e := f.gateway.RecoverExpired(t.Context(), f.a, 100); e != nil || n != 1 {
			t.Fatalf("recovery=%d %v", n, e)
		}
		f.recoveryTurn(t, f.b)
	}
	if _, e := f.gateway.Execute(t.Context(), f.a, limitsOf, p, c, false); e != nil {
		t.Fatal(e)
	}
	v, e := f.gateway.Get(t.Context(), f.a, p.RequestID)
	if e != nil {
		t.Fatal(e)
	}
	if v.State != llmgateway.StateSucceeded || v.Result == nil {
		t.Fatal("lost late result")
	}
	if n := f.activeLLMSlots(t); n != 1 {
		t.Fatalf("another slot released: %d", n)
	}
}

func TestRecoverySweepPagesThroughInactiveAccounts(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	f.recoveryTurn(t, f.a)
	f.recoveryTurn(t, f.b)
	if _, e := f.db.Exec(`UPDATE accounts SET status='pending_verification' WHERE id='gw-b'`); e != nil {
		t.Fatal(e)
	}
	f.now = f.now.Add(4 * time.Minute)
	cursor, e := f.gateway.SweepExpiredDispatches(t.Context(), f.database, "", 1)
	if e != nil {
		t.Fatal(e)
	}
	if cursor != "gw-a" || f.activeLLMSlots(t) != 1 {
		t.Fatalf("first page=%q slots=%d", cursor, f.activeLLMSlots(t))
	}
	cursor, e = f.gateway.SweepExpiredDispatches(t.Context(), f.database, cursor, 1)
	if e != nil {
		t.Fatal(e)
	}
	if cursor != "gw-b" || f.activeLLMSlots(t) != 0 {
		t.Fatalf("inactive account page=%q slots=%d", cursor, f.activeLLMSlots(t))
	}
	cursor, e = f.gateway.SweepExpiredDispatches(t.Context(), f.database, cursor, 1)
	if e != nil || cursor != "" {
		t.Fatalf("page wrap=%q %v", cursor, e)
	}
}
