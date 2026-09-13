package llmgateway_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// count is the caller-side proof that one step stayed one identity.
func (f *fixture) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if err := f.db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (f *fixture) requestState(t *testing.T, requestID string) (state string, attempts int) {
	t.Helper()
	if err := f.db.QueryRow(`SELECT state,attempts_used FROM llm_requests WHERE id=$1`, requestID).
		Scan(&state, &attempts); err != nil {
		t.Fatal(err)
	}
	return state, attempts
}

func (f *fixture) consumerState(t *testing.T, requestID string) string {
	t.Helper()
	var state string
	if err := f.db.QueryRow(`SELECT state FROM llm_result_consumers WHERE request_id=$1`, requestID).
		Scan(&state); err != nil {
		t.Fatal(err)
	}
	return state
}

func TestResumingAStepReplaysTheResultInsteadOfPayingAgain(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	session := f.callSession(f.a, "run-resume")
	in := llmgateway.CallInput{BindingKey: "run-resume:step-1", Chat: textRequest("chat")}

	first, err := f.gateway.Call(t.Context(), session, in)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if !first.Dispatched || first.AlreadyConsumed {
		t.Fatalf("the first turn must be the one that pays: %+v", first)
	}
	_, spent, usedTokens := f.budget(t, "gw-a")

	// The process restarts and the step resumes with the identity it persisted.
	// Nothing about the turn is regenerated, so nothing new may be paid for.
	second, err := f.gateway.Call(t.Context(), session, in)
	if err != nil {
		t.Fatalf("resumed call: %v", err)
	}
	if second.RequestID != first.RequestID {
		t.Fatalf("a resumed step opened a new request: %s then %s", first.RequestID, second.RequestID)
	}
	if second.Dispatched {
		t.Fatal("a resumed step must read the existing result, not dispatch again")
	}
	if !second.AlreadyConsumed || second.Result.Text != first.Result.Text {
		t.Fatalf("the resumed turn did not replay the same consumed result: %+v", second)
	}
	if calls := f.provider.attempts(); calls != 1 {
		t.Fatalf("the provider was paid %d times for one step", calls)
	}
	if n := f.count(t, `SELECT count(*) FROM llm_requests WHERE account_id='gw-a'`); n != 1 {
		t.Fatalf("one step produced %d request identities", n)
	}
	if n := f.count(t, `SELECT count(*) FROM llm_usage_reservations WHERE account_id='gw-a'`); n != 1 {
		t.Fatalf("one step produced %d reservations", n)
	}
	if _, afterSpent, afterTokens := f.budget(t, "gw-a"); afterSpent != spent || afterTokens != usedTokens {
		t.Fatalf("a replay moved the ledger: %d/%d became %d/%d", spent, usedTokens, afterSpent, afterTokens)
	}
}

func TestConsumeCommitsWithTheCallersOwnWrite(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	session := f.callSession(f.a, "run-consume")
	stepFailed := errors.New("caller could not persist the step")

	// The caller's own row stands in for an agent step and its tool calls: it is
	// written inside the consuming transaction and must share its fate.
	saveStep := func(tx store.TxAccountScope, r llmgateway.Result) error {
		return tx.Insert(t.Context(), "customers",
			[]string{"id", "display_name", "channel"}, "cus-step-1", r.Text, "other")
	}
	in := llmgateway.CallInput{
		BindingKey: "run-consume:step-1",
		Chat:       textRequest("chat"),
		Consume: func(tx store.TxAccountScope, r llmgateway.Result) error {
			if err := saveStep(tx, r); err != nil {
				return err
			}
			return stepFailed
		},
	}
	if _, err := f.gateway.Call(t.Context(), session, in); !errors.Is(err, stepFailed) {
		t.Fatalf("a failed caller write must fail the turn, got %v", err)
	}
	requestID := f.requestID(t, "run-consume")
	if n := f.count(t, `SELECT count(*) FROM customers WHERE id='cus-step-1'`); n != 0 {
		t.Fatal("the caller's row survived a rolled back consumption")
	}
	if state := f.consumerState(t, requestID); state != "pending" {
		t.Fatalf("the result was marked %q although the caller's write rolled back", state)
	}

	// The same step resumes. The paid result is still there, so it is consumed
	// exactly once — together with the caller's row this time.
	in.Consume = saveStep
	outcome, err := f.gateway.Call(t.Context(), session, in)
	if err != nil {
		t.Fatalf("resumed call: %v", err)
	}
	if outcome.AlreadyConsumed {
		t.Fatal("a rolled back consumption must not count as one")
	}
	if outcome.Dispatched || f.provider.attempts() != 1 {
		t.Fatalf("the retry paid again: dispatched=%v calls=%d", outcome.Dispatched, f.provider.attempts())
	}
	if n := f.count(t, `SELECT count(*) FROM customers WHERE id='cus-step-1' AND account_id='gw-a'`); n != 1 {
		t.Fatalf("the caller's row was written %d times", n)
	}
	if state := f.consumerState(t, requestID); state != "consumed" {
		t.Fatalf("consumer state %q", state)
	}
}

func TestARefusedAttemptRetriesOnTheSameIdentity(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	// Only an attempt the provider demonstrably never accepted may run again.
	f.provider.err = &llmgateway.ProviderError{Outcome: llmgateway.OutcomeUnaccepted, Class: "connect_refused"}
	f.provider.observe = func(llmgateway.ProviderRequest) {
		// Invoke holds the provider lock and has already counted this call.
		if f.provider.calls > 1 {
			f.provider.err = nil
		}
	}

	session := f.callSession(f.a, "run-retry")
	outcome, err := f.gateway.Call(t.Context(), session, llmgateway.CallInput{
		BindingKey: "run-retry:step-1",
		Chat:       textRequest("chat"),
	})
	if err != nil {
		t.Fatalf("a refused attempt must be retried by the coordinator: %v", err)
	}
	if calls := f.provider.attempts(); calls != 2 {
		t.Fatalf("provider calls %d", calls)
	}
	state, attempts := f.requestState(t, outcome.RequestID)
	if state != "succeeded" || attempts != 2 {
		t.Fatalf("request state %q after %d attempts", state, attempts)
	}
	if n := f.count(t, `SELECT count(*) FROM llm_requests WHERE account_id='gw-a'`); n != 1 {
		t.Fatalf("the retry opened %d request identities", n)
	}
	if n := f.count(t, `SELECT count(*) FROM llm_usage_reservations WHERE account_id='gw-a'`); n != 1 {
		t.Fatalf("the retry opened %d reservations", n)
	}
	reserved, spent, _ := f.budget(t, "gw-a")
	if reserved != 0 || spent == 0 {
		t.Fatalf("the retried turn settled as reserved=%d spent=%d", reserved, spent)
	}
}

func TestAnUnsettledStepIsNeverSentAgain(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	f.provider.err = &llmgateway.ProviderError{Outcome: llmgateway.OutcomeUnknown, Class: "transport"}

	session := f.callSession(f.a, "run-unknown")
	in := llmgateway.CallInput{BindingKey: "run-unknown:step-1", Chat: textRequest("chat")}
	if _, err := f.gateway.Call(t.Context(), session, in); err == nil {
		t.Fatal("an unknown transport outcome must fail the turn")
	}

	// The provider may already have answered and billed this identity, so the
	// resumed step must refuse rather than find out by sending it again.
	f.provider.err = nil
	_, err := f.gateway.Call(t.Context(), session, in)
	if !errors.Is(err, llmgateway.ErrUnknown) {
		t.Fatalf("an unsettled step must ask for verification, got %v", err)
	}
	if calls := f.provider.attempts(); calls != 1 {
		t.Fatalf("an unsettled step was sent again: %d provider calls", calls)
	}
}

func TestATransientAdmissionRefusalKeepsTheStepResumable(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	// Occupy both shared transports of the test deployment without finishing them.
	var (
		permits []llmgateway.Permit
		chats   []llmgateway.ChatRequest
	)
	for range 2 {
		reservation := f.reserve(t, f.a, uuid.NewString(), turnOptions{})
		view, chat, err := f.prepare(t, f.a, reservation.ID, uuid.NewString(), turnOptions{})
		if err != nil {
			t.Fatal(err)
		}
		permit, err := f.begin(t, f.a, view.ID)
		if err != nil {
			t.Fatal(err)
		}
		permits, chats = append(permits, permit), append(chats, chat)
	}

	session := f.callSession(f.a, "run-busy")
	in := llmgateway.CallInput{BindingKey: "run-busy:step-1", Chat: textRequest("chat")}
	if _, err := f.gateway.Call(t.Context(), session, in); !errors.Is(err, llmgateway.ErrRateLimited) {
		t.Fatalf("a busy deployment must refuse admission, got %v", err)
	}
	requestID := f.requestID(t, "run-busy")
	if state, attempts := f.requestState(t, requestID); state != "prepared" || attempts != 0 {
		t.Fatalf("a transient refusal ended the step as %q after %d attempts", state, attempts)
	}

	// One transport finishes, so the same binding key runs the step it prepared
	// instead of having been cancelled by a condition that clears on its own.
	if _, err := f.gateway.Execute(t.Context(), f.a, limitsOf, permits[0], chats[0], false); err != nil {
		t.Fatal(err)
	}
	outcome, err := f.gateway.Call(t.Context(), session, in)
	if err != nil {
		t.Fatalf("the step must run once admission frees up: %v", err)
	}
	if outcome.RequestID != requestID {
		t.Fatalf("the retry opened a new identity: %s then %s", requestID, outcome.RequestID)
	}
	if n := f.count(t, `SELECT count(*) FROM llm_requests WHERE account_id='gw-a' AND caller_group_id='run-busy'`); n != 1 {
		t.Fatalf("one step produced %d request identities", n)
	}
}

// oneCallPerMinuteCatalog lets exactly one dispatch through per window, so the
// re-dispatch of a proven-unaccepted attempt is refused deterministically.
func oneCallPerMinuteCatalog(t *testing.T) *llmgateway.Catalog {
	t.Helper()
	c, err := llmgateway.NewCatalog("cat-1", "USD", []llmgateway.ModelConfig{{
		ModelKey: "chat", DisplayName: "Chat", Provider: llmgateway.ProviderOpenAICompatible,
		DeploymentKey: "dep-1", BaseURL: "https://example.invalid", CredentialEnv: "TEST_KEY",
		RequestModelID: "vendor-model", AcceptedModelIDs: []string{"vendor-model"},
		Capability: llmgateway.Capability{ToolCalling: true, ContextTokens: 1000, MaxOutputTokens: 500},
		Price:      usablePrice(), LimitKey: "dep-1", Concurrency: 2, RateLimitPerMin: 1,
		QueryCapability: "none", CancelCapability: "none", Enabled: true,
	}})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestARefusedPreparationLeavesTheBindingKeyUsable(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	session := f.callSession(f.a, "run-prepare")

	// Capability is only checked while preparing, so this reaches the gateway
	// with a hold already computed for it.
	over := textRequest("chat")
	over.OutputLimit = 600 // the deployment allows 500
	in := llmgateway.CallInput{BindingKey: "run-prepare:step-1", Chat: over}
	if _, err := f.gateway.Call(t.Context(), session, in); !errors.Is(err, llmgateway.ErrCapability) {
		t.Fatalf("an over-capability request must be refused, got %v", err)
	}

	// A refused preparation must leave nothing behind: a reservation this binding
	// key can never claim would make the step permanently unrunnable.
	if n := f.count(t, `SELECT count(*) FROM llm_usage_reservations WHERE account_id='gw-a'`); n != 0 {
		t.Fatalf("a refused preparation left %d reservations behind", n)
	}
	if n := f.count(t, `SELECT count(*) FROM llm_requests WHERE account_id='gw-a'`); n != 0 {
		t.Fatalf("a refused preparation left %d requests behind", n)
	}
	if reserved, _, _ := f.budget(t, "gw-a"); reserved != 0 {
		t.Fatalf("a refused preparation held %d micros", reserved)
	}

	// The caller fixes what was refused and runs the same step.
	in.Chat = textRequest("chat")
	outcome, err := f.gateway.Call(t.Context(), session, in)
	if err != nil {
		t.Fatalf("the corrected step must run on the same binding key: %v", err)
	}
	if !outcome.Dispatched || outcome.Result.Text != okResult().Text {
		t.Fatalf("outcome %+v", outcome)
	}
}

func TestATerminalRefusalEndsTheRequestAndReturnsTheHold(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	session := f.callSession(f.a, "run-expired")
	base := f.now
	var once sync.Once
	session.Limits = func(tx store.TxAccountScope) llmgateway.LimitView {
		// The request deadline passes between preparing the turn and taking its
		// permit. Unlike a shared limit or a ceiling, that is terminal: this
		// identity can never dispatch, so its hold must come back now.
		once.Do(func() { f.now = base.Add(2 * time.Hour) })
		return tx.LLMLimitView()
	}

	outcome, err := f.gateway.Call(t.Context(), session, llmgateway.CallInput{
		BindingKey: "run-expired:step-1",
		Chat:       textRequest("chat"),
	})
	if !errors.Is(err, llmgateway.ErrDeadline) {
		t.Fatalf("an expired request must be refused, got %v", err)
	}
	if outcome.RequestID == "" {
		t.Fatal("a caller that has to follow up needs the request id back")
	}
	if state, attempts := f.requestState(t, outcome.RequestID); state != "cancelled" || attempts != 0 {
		t.Fatalf("a turn that can never dispatch was left %q after %d attempts", state, attempts)
	}
	if reserved, spent, _ := f.budget(t, "gw-a"); reserved != 0 || spent != 0 {
		t.Fatalf("the hold was not returned: reserved=%d spent=%d", reserved, spent)
	}
	if state := f.consumerState(t, outcome.RequestID); state != "abandoned" {
		t.Fatalf("the retention claim of a cancelled request is %q", state)
	}
	if calls := f.provider.attempts(); calls != 0 {
		t.Fatalf("nothing may be sent, got %d provider calls", calls)
	}
}

func TestARefusedRedispatchLeavesTheStepResumable(t *testing.T) {
	f := setupGatewayWith(t, defaultBudget(), oneCallPerMinuteCatalog(t))
	session := f.callSession(f.a, "run-redispatch")
	in := llmgateway.CallInput{BindingKey: "run-redispatch:step-1", Chat: textRequest("chat")}

	// The first attempt is provably unaccepted, which puts the step back in line
	// for re-admission — but the deployment's one call per window is now spent,
	// so the re-dispatch is refused.
	f.provider.err = &llmgateway.ProviderError{Outcome: llmgateway.OutcomeUnaccepted, Class: "connect_refused"}
	if _, err := f.gateway.Call(t.Context(), session, in); !errors.Is(err, llmgateway.ErrRateLimited) {
		t.Fatalf("the re-dispatch must be refused by the shared limit, got %v", err)
	}

	// Re-admission and the dispatch intent rolled back together, so the step is
	// still waiting to be re-admitted rather than holding a rebuilt reservation
	// no later call can use.
	requestID := f.requestID(t, "run-redispatch")
	var (
		reservationState string
		retryEligible    bool
	)
	if err := f.db.QueryRow(
		`SELECT settlement_state,retry_eligible FROM llm_usage_reservations WHERE request_id=$1`, requestID).
		Scan(&reservationState, &retryEligible); err != nil {
		t.Fatal(err)
	}
	if reservationState != "released" || !retryEligible {
		t.Fatalf("the reservation was left %q (retry_eligible=%v) with no attempt to go with it",
			reservationState, retryEligible)
	}

	// The window rolls over and the same step runs, instead of failing forever on
	// "reservation is not released for retry".
	f.provider.err = nil
	f.now = f.now.Add(2 * time.Minute)
	outcome, err := f.gateway.Call(t.Context(), session, in)
	if err != nil {
		t.Fatalf("the step must still be resumable: %v", err)
	}
	if outcome.RequestID != requestID {
		t.Fatalf("the resumed step opened a new identity: %s then %s", requestID, outcome.RequestID)
	}
	if state, attempts := f.requestState(t, requestID); state != "succeeded" || attempts != 2 {
		t.Fatalf("request state %q after %d attempts", state, attempts)
	}
	if reserved, spent, _ := f.budget(t, "gw-a"); reserved != 0 || spent == 0 {
		t.Fatalf("the step settled as reserved=%d spent=%d", reserved, spent)
	}
}

func TestACeilingRefusalLeavesTheStepRecoverable(t *testing.T) {
	f := setupGatewayWith(t, defaultBudget(), oneCallPerMinuteCatalog(t))
	chat := textRequest("chat")
	// The hold is derived with the same recipe the coordinator uses, so a group
	// ceiling of exactly one hold leaves room for this step and nothing else.
	bound := usablePrice().UpperBound()
	stepHold := llmgateway.CostMicros(llmgateway.EstimateInputTokens(chat), bound.InputUncached) +
		llmgateway.CostMicros(int64(chat.OutputLimit), bound.Output)

	session := f.callSession(f.a, "run-budget")
	session.GroupLimitMicros = stepHold
	in := llmgateway.CallInput{BindingKey: "run-budget:step-1", Chat: chat}

	// One proven-unaccepted attempt gives the hold back; the re-dispatch is
	// refused by the one call per window, which leaves the step waiting.
	f.provider.err = &llmgateway.ProviderError{Outcome: llmgateway.OutcomeUnaccepted, Class: "connect_refused"}
	if _, err := f.gateway.Call(t.Context(), session, in); !errors.Is(err, llmgateway.ErrRateLimited) {
		t.Fatalf("setup: %v", err)
	}
	requestID := f.requestID(t, "run-budget")

	// Another task of the same group takes the freed ceiling in the meantime.
	competitor := f.reserve(t, f.a, uuid.NewString(), turnOptions{group: "run-budget", inputBound: 1, outputLimit: 256})
	f.provider.err = nil
	f.now = f.now.Add(2 * time.Minute)
	if _, err := f.gateway.Call(t.Context(), session, in); !errors.Is(err, llmgateway.ErrBudget) {
		t.Fatalf("the re-admission must report the ceiling, got %v", err)
	}

	// A ceiling someone else is occupying is not a reason to end the step: it
	// keeps its identity and stays exactly as re-admissible as it was.
	state, attempts := f.requestState(t, requestID)
	if state != "prepared" || attempts != 1 {
		t.Fatalf("a temporary ceiling ended the step as %q after %d attempts", state, attempts)
	}
	var (
		reservationState string
		retryEligible    bool
	)
	if err := f.db.QueryRow(
		`SELECT settlement_state,retry_eligible FROM llm_usage_reservations WHERE request_id=$1`, requestID).
		Scan(&reservationState, &retryEligible); err != nil {
		t.Fatal(err)
	}
	if reservationState != "released" || !retryEligible {
		t.Fatalf("the reservation was left %q (retry_eligible=%v)", reservationState, retryEligible)
	}

	// The other task finishes and the step runs on its original identity.
	if err := f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		return f.gateway.ReleaseReservationInTx(t.Context(), tx, competitor.ID)
	}); err != nil {
		t.Fatal(err)
	}
	outcome, err := f.gateway.Call(t.Context(), session, in)
	if err != nil {
		t.Fatalf("the step must be recoverable once the ceiling frees up: %v", err)
	}
	if outcome.RequestID != requestID {
		t.Fatalf("the recovered step opened a new identity: %s then %s", requestID, outcome.RequestID)
	}
	if state, attempts := f.requestState(t, requestID); state != "succeeded" || attempts != 2 {
		t.Fatalf("request state %q after %d attempts", state, attempts)
	}
}

func TestALostDispatchRaceNeverAbandonsAPaidResult(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	in := llmgateway.CallInput{BindingKey: "run-race:step-1", Chat: textRequest("chat")}
	stepFailed := errors.New("caller could not persist the step")

	// The loser is held at the point where it is about to take its permit: it has
	// already seen the request as prepared, exactly as a second worker resuming
	// the same step would.
	atBarrier, release := make(chan struct{}), make(chan struct{})
	loser := f.callSession(f.a, "run-race")
	var once sync.Once
	loser.Limits = func(tx store.TxAccountScope) llmgateway.LimitView {
		once.Do(func() {
			close(atBarrier)
			<-release
		})
		return tx.LLMLimitView()
	}

	loserDone := make(chan error, 1)
	go func() {
		_, err := f.gateway.Call(t.Context(), loser, in)
		loserDone <- err
	}()
	<-atBarrier

	// The winner produces and pays for the result; only its own persistence
	// fails, so the result is complete and still waiting to be consumed.
	winner := in
	winner.Consume = func(store.TxAccountScope, llmgateway.Result) error { return stepFailed }
	if _, err := f.gateway.Call(t.Context(), f.callSession(f.a, "run-race"), winner); !errors.Is(err, stepFailed) {
		t.Fatalf("winner: %v", err)
	}
	close(release)
	if err := <-loserDone; !errors.Is(err, llmgateway.ErrState) {
		t.Fatalf("the loser must report the state it lost to, got %v", err)
	}

	requestID := f.requestID(t, "run-race")
	if state := f.consumerState(t, requestID); state != "pending" {
		t.Fatalf("the loser marked a paid result %q; it can never be consumed again", state)
	}
	// The paid result is still there to be consumed by whoever comes back for it.
	outcome, err := f.gateway.Call(t.Context(), f.callSession(f.a, "run-race"), in)
	if err != nil {
		t.Fatalf("the paid result must still be consumable: %v", err)
	}
	if outcome.AlreadyConsumed || outcome.Dispatched || outcome.Result.Text != okResult().Text {
		t.Fatalf("outcome %+v", outcome)
	}
	if calls := f.provider.attempts(); calls != 1 {
		t.Fatalf("the race paid %d times", calls)
	}
}

// requestID reads back the single request one caller group prepared.
func (f *fixture) requestID(t *testing.T, group string) string {
	t.Helper()
	var id string
	if err := f.db.QueryRow(
		`SELECT id FROM llm_requests WHERE account_id='gw-a' AND caller_group_id=$1`, group).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
