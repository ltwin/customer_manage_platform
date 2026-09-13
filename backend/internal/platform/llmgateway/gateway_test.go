package llmgateway_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store/storetest"
)

func TestMain(m *testing.M) { storetest.Main(m, store.MigrateUp) }

// fakeProvider stands in for one deployment so admission, dispatch and ledger
// behaviour can be driven deterministically. Wire-protocol fidelity is proven
// separately against the two real protocol adapters.
type fakeProvider struct {
	mu      sync.Mutex
	calls   int
	result  llmgateway.Result
	err     error
	observe func(llmgateway.ProviderRequest)
}

func (p *fakeProvider) Key() llmgateway.ProviderKey { return llmgateway.ProviderOpenAICompatible }

func (p *fakeProvider) Invoke(_ context.Context, r llmgateway.ProviderRequest) (llmgateway.Result, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	if p.observe != nil {
		p.observe(r)
	}
	if p.err != nil {
		return llmgateway.Result{}, p.err
	}
	return p.result, nil
}

func (p *fakeProvider) attempts() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

type fixture struct {
	db       *sql.DB
	a, b     store.AccountScope
	gateway  *llmgateway.Service
	provider *fakeProvider
	now      time.Time
}

func ptr[T any](v T) *T { return &v }

func okResult() llmgateway.Result {
	return llmgateway.Result{
		Text:              "三个方向",
		FinishReason:      llmgateway.FinishStop,
		Model:             "vendor-model",
		ProviderRequestID: "req-1",
		Usage: llmgateway.UsageEvidence{
			InputTotalTokens:  ptr(int64(1000)),
			InputCachedTokens: ptr(int64(400)),
			OutputTokens:      ptr(int64(200)),
		},
	}
}

func limitsOf(tx store.TxAccountScope) llmgateway.LimitView { return tx.LLMLimitView() }

func setupGateway(t *testing.T, budget llmgateway.BudgetPolicy) *fixture {
	t.Helper()
	return setupGatewayWith(t, budget, testCatalog(t, usablePrice()))
}

func setupGatewayWith(t *testing.T, budget llmgateway.BudgetPolicy, catalog *llmgateway.Catalog) *fixture {
	t.Helper()
	url := storetest.NewURL(t)
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`INSERT INTO accounts(id,password_hash,status) VALUES ('gw-a','test','active'),('gw-b','test','active')`); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.Context(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)

	f := &fixture{db: db, provider: &fakeProvider{result: okResult()}, now: time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)}
	f.gateway, err = llmgateway.New(llmgateway.Config{
		Catalog:       catalog,
		Providers:     map[llmgateway.ProviderKey]llmgateway.Provider{llmgateway.ProviderOpenAICompatible: f.provider},
		Credential:    func(string) (string, bool) { return "secret-value", true },
		Clock:         func() time.Time { return f.now },
		DefaultBudget: budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	f.a = st.ScopeFor(auth.AccountContext{AccountID: "gw-a"})
	f.b = st.ScopeFor(auth.AccountContext{AccountID: "gw-b"})
	return f
}

func defaultBudget() llmgateway.BudgetPolicy {
	return llmgateway.BudgetPolicy{MonthlyLimitMicros: 10_000_000, MonthlyTokenLimit: 1_000_000}
}

type turnOptions struct {
	group       string
	groupMicros int64
	groupTokens int64
	inputBound  int64
	outputLimit int
	deadline    time.Time
}

func (f *fixture) defaults(o turnOptions) turnOptions {
	if o.group == "" {
		o.group = "run-1"
	}
	if o.groupMicros == 0 {
		o.groupMicros = 5_000_000
	}
	if o.groupTokens == 0 {
		o.groupTokens = 500_000
	}
	if o.inputBound == 0 {
		o.inputBound = 1200
	}
	if o.outputLimit == 0 {
		o.outputLimit = 256
	}
	if o.deadline.IsZero() {
		o.deadline = f.now.Add(time.Hour)
	}
	return o
}

func (f *fixture) reserve(t *testing.T, scope store.AccountScope, operationID string, o turnOptions) llmgateway.Reservation {
	t.Helper()
	reservation, err := f.tryReserve(t, scope, operationID, o)
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	return reservation
}

func (f *fixture) tryReserve(t *testing.T, scope store.AccountScope, operationID string, o turnOptions) (llmgateway.Reservation, error) {
	t.Helper()
	o = f.defaults(o)
	// A real caller bounds the reservation with the same estimator the request
	// will be checked against, so the hold always covers what it dispatches.
	chat := textRequest("chat")
	chat.OutputLimit = o.outputLimit
	if estimate := llmgateway.EstimateInputTokens(chat); estimate > o.inputBound {
		o.inputBound = estimate
	}
	var reservation llmgateway.Reservation
	err := scope.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		var err error
		reservation, err = f.gateway.ReserveInTx(t.Context(), tx, llmgateway.ReserveInput{
			CallerService:        "creative_agent",
			CallerOperationID:    operationID,
			CallerGroupID:        o.group,
			GroupLimitMicros:     o.groupMicros,
			GroupTokenLimit:      o.groupTokens,
			GroupDeadline:        o.deadline,
			ModelKey:             "chat",
			InputTokenUpperBound: o.inputBound,
			OutputLimit:          o.outputLimit,
			ExpiresAt:            o.deadline,
		})
		return err
	})
	return reservation, err
}

func (f *fixture) prepare(t *testing.T, scope store.AccountScope, reservationID, operationID string, o turnOptions) (llmgateway.RequestView, llmgateway.ChatRequest, error) {
	t.Helper()
	o = f.defaults(o)
	chat := textRequest("chat")
	chat.OutputLimit = o.outputLimit
	var view llmgateway.RequestView
	err := scope.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		var err error
		view, err = f.gateway.PrepareInTx(t.Context(), tx, llmgateway.PrepareInput{
			CallerService:     "creative_agent",
			CallerOperationID: operationID,
			CallerGroupID:     o.group,
			ReservationID:     reservationID,
			ConsumerKey:       "step:" + operationID,
			Chat:              chat,
			Deadline:          o.deadline,
		})
		return err
	})
	return view, chat, err
}

func (f *fixture) begin(t *testing.T, scope store.AccountScope, requestID string) (llmgateway.Permit, error) {
	t.Helper()
	var permit llmgateway.Permit
	err := scope.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		var err error
		permit, err = f.gateway.BeginDispatchInTx(t.Context(), tx, tx.LLMLimitView(), requestID)
		return err
	})
	return permit, err
}

// turn drives one full gateway turn the way a caller would.
func (f *fixture) turn(t *testing.T, scope store.AccountScope, o turnOptions) (llmgateway.RequestView, llmgateway.Result, error) {
	t.Helper()
	reservation, err := f.tryReserve(t, scope, uuid.NewString(), o)
	if err != nil {
		return llmgateway.RequestView{}, llmgateway.Result{}, err
	}
	view, chat, err := f.prepare(t, scope, reservation.ID, uuid.NewString(), o)
	if err != nil {
		return view, llmgateway.Result{}, err
	}
	permit, err := f.begin(t, scope, view.ID)
	if err != nil {
		return view, llmgateway.Result{}, err
	}
	result, err := f.gateway.Execute(t.Context(), scope, limitsOf, permit, chat, false)
	return view, result, err
}

func (f *fixture) budget(t *testing.T, account string) (reserved, spent, usedTokens int64) {
	t.Helper()
	err := f.db.QueryRow(
		`SELECT reserved_micros,spent_micros,used_tokens FROM llm_budgets WHERE account_id=$1`, account).
		Scan(&reserved, &spent, &usedTokens)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return reserved, spent, usedTokens
}

func TestReserveReplayNeverBooksBudgetTwice(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	operation := uuid.NewString()

	first := f.reserve(t, f.a, operation, turnOptions{})
	reserved, _, _ := f.budget(t, "gw-a")
	second := f.reserve(t, f.a, operation, turnOptions{})
	afterReplay, _, _ := f.budget(t, "gw-a")

	if first.ID != second.ID || !second.Replayed {
		t.Fatalf("replay must return the original reservation: %+v vs %+v", first, second)
	}
	if reserved != afterReplay {
		t.Fatalf("replay moved the budget from %d to %d", reserved, afterReplay)
	}
	if reserved != first.HoldMicros {
		t.Fatalf("reserved %d does not match the hold %d", reserved, first.HoldMicros)
	}
}

func TestInitialReservationIsClaimedByExactlyOneRequest(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	reservation := f.reserve(t, f.a, uuid.NewString(), turnOptions{})

	if _, _, err := f.prepare(t, f.a, reservation.ID, uuid.NewString(), turnOptions{}); err != nil {
		t.Fatalf("first claim: %v", err)
	}
	_, _, err := f.prepare(t, f.a, reservation.ID, uuid.NewString(), turnOptions{})
	if !errors.Is(err, llmgateway.ErrConflict) {
		t.Fatalf("a second request must not claim the same reservation: %v", err)
	}
}

func TestSuccessfulTurnBooksDisjointComponentsAndClearsHold(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	view, result, err := f.turn(t, f.a, turnOptions{})
	if err != nil {
		t.Fatalf("turn: %v", err)
	}
	if result.Text != "三个方向" {
		t.Fatalf("result %+v", result)
	}

	rate := usablePrice().RateAt(f.now)
	want := llmgateway.CostMicros(400, rate.InputCached) +
		llmgateway.CostMicros(600, rate.InputUncached) +
		llmgateway.CostMicros(200, rate.Output)

	reserved, spent, usedTokens := f.budget(t, "gw-a")
	if spent != want {
		t.Fatalf("booked %d micros, want %d (cached+uncached+output, never overlapping)", spent, want)
	}
	if reserved != 0 {
		t.Fatalf("a fully settled turn must hold nothing, got %d", reserved)
	}
	if usedTokens != 1200 {
		t.Fatalf("token ledger %d, want input_total+output without re-adding cached", usedTokens)
	}

	stored, err := f.gateway.Get(t.Context(), f.a, view.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.State != llmgateway.StateSucceeded || stored.Settlement != llmgateway.SettlementSettled {
		t.Fatalf("state %s settlement %s", stored.State, stored.Settlement)
	}

	usage, err := f.gateway.GetUsage(t.Context(), f.a, view.ID)
	if err != nil {
		t.Fatal(err)
	}
	if usage.BookedMicros != want || usage.HoldMicros != 0 || usage.OpenEvidence != 0 {
		t.Fatalf("usage report %+v", usage)
	}
}

func TestMissingUsageKeepsResultUsableAndMoneyOpen(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	result := okResult()
	result.Usage = llmgateway.UsageEvidence{}
	f.provider.result = result

	view, _, err := f.turn(t, f.a, turnOptions{})
	if err != nil {
		t.Fatalf("a complete answer without usage must still succeed: %v", err)
	}
	stored, err := f.gateway.Get(t.Context(), f.a, view.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.State != llmgateway.StateSucceeded || stored.Result == nil {
		t.Fatalf("the known result must stay readable: %+v", stored)
	}
	if stored.Settlement == llmgateway.SettlementSettled {
		t.Fatal("settlement must not claim to be complete without usage evidence")
	}
	reserved, spent, _ := f.budget(t, "gw-a")
	if spent != 0 || reserved == 0 {
		t.Fatalf("unknown usage must keep the hold instead of booking zero: reserved=%d spent=%d", reserved, spent)
	}
}

func TestVerifiedRejectionReadmitsOnlyWhenBudgetIsFree(t *testing.T) {
	// The month bucket holds exactly one conservative reservation, so A and B
	// really compete for the same money.
	bound := usablePrice().UpperBound()
	oneHold := llmgateway.CostMicros(100, bound.InputUncached) + llmgateway.CostMicros(256, bound.Output)
	f := setupGateway(t, llmgateway.BudgetPolicy{MonthlyLimitMicros: oneHold, MonthlyTokenLimit: 1_000_000})
	small := turnOptions{groupMicros: oneHold, groupTokens: 1_000_000, inputBound: 100, outputLimit: 256}

	// A dispatches and the provider reliably refuses admission: zero cost.
	f.provider.err = &llmgateway.ProviderError{Outcome: llmgateway.OutcomeUnaccepted, Class: "rate_limited", StatusCode: 429}
	reservationA := f.reserve(t, f.a, uuid.NewString(), small)
	viewA, chatA, err := f.prepare(t, f.a, reservationA.ID, uuid.NewString(), small)
	if err != nil {
		t.Fatal(err)
	}
	permitA, err := f.begin(t, f.a, viewA.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.gateway.Execute(t.Context(), f.a, limitsOf, permitA, chatA, false); err == nil {
		t.Fatal("an unaccepted dispatch must surface an error")
	}
	stored, err := f.gateway.Get(t.Context(), f.a, viewA.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.State != llmgateway.StatePrepared || !stored.RetryEligible {
		t.Fatalf("a proven non-acceptance must stay re-admissible: %+v", stored)
	}
	reserved, spent, _ := f.budget(t, "gw-a")
	if reserved != 0 || spent != 0 {
		t.Fatalf("a zero-cost rejection must free the hold: reserved=%d spent=%d", reserved, spent)
	}

	// B takes the whole budget while A is released.
	f.provider.err = nil
	reservationB := f.reserve(t, f.a, uuid.NewString(),
		turnOptions{group: "run-2", groupMicros: oneHold, groupTokens: 1_000_000, inputBound: 100, outputLimit: 256})
	if reserved, _, _ := f.budget(t, "gw-a"); reserved != oneHold {
		t.Fatalf("the competitor must hold the whole bucket: reserved=%d want=%d", reserved, oneHold)
	}

	// A may not re-arm while the budget is occupied.
	err = f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		_, err := f.gateway.RearmInTx(t.Context(), tx, viewA.ID, 1, 0, 0)
		return err
	})
	if !errors.Is(err, llmgateway.ErrBudget) {
		t.Fatalf("re-admission must fail while the budget is taken, got %v", err)
	}

	// B gives the money back through its own port, and only then may A rebuild
	// its original hold under a new generation.
	if err := f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		return f.gateway.ReleaseReservationInTx(t.Context(), tx, reservationB.ID)
	}); err != nil {
		t.Fatalf("releasing the competitor's unclaimed hold: %v", err)
	}
	if reserved, spent, _ := f.budget(t, "gw-a"); reserved != 0 || spent != 0 {
		t.Fatalf("a released hold must leave the bucket empty: reserved=%d spent=%d", reserved, spent)
	}
	var rearmed llmgateway.Reservation
	err = f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		var err error
		rearmed, err = f.gateway.RearmInTx(t.Context(), tx, viewA.ID, 1, 0, 0)
		return err
	})
	if err != nil {
		t.Fatalf("re-admission after the budget was freed: %v", err)
	}
	if rearmed.HoldGeneration != 2 || rearmed.HoldMicros != reservationA.HoldMicros {
		t.Fatalf("re-arm must rebuild the original bound under a new generation: %+v", rearmed)
	}
	if err := f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		_, err := f.gateway.RearmInTx(t.Context(), tx, viewA.ID, 1, 0, 0)
		return err
	}); !errors.Is(err, llmgateway.ErrState) && !errors.Is(err, llmgateway.ErrConflict) {
		t.Fatalf("a stale generation must not re-arm again: %v", err)
	}
}

func TestAttemptsAreBoundedAndPermitIsSingleUse(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	f.provider.err = &llmgateway.ProviderError{Outcome: llmgateway.OutcomeUnaccepted, Class: "rate_limited", StatusCode: 429}

	reservation := f.reserve(t, f.a, uuid.NewString(), turnOptions{})
	view, chat, err := f.prepare(t, f.a, reservation.ID, uuid.NewString(), turnOptions{})
	if err != nil {
		t.Fatal(err)
	}
	permit, err := f.begin(t, f.a, view.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.gateway.Execute(t.Context(), f.a, limitsOf, permit, chat, false); err == nil {
		t.Fatal("want dispatch error")
	}
	calls := f.provider.attempts()

	// Replaying the same permit must not produce a second real attempt.
	_, err = f.gateway.Execute(t.Context(), f.a, limitsOf, permit, chat, false)
	if !errors.Is(err, llmgateway.ErrConflict) {
		t.Fatalf("a consumed permit must not dispatch again: %v", err)
	}
	if f.provider.attempts() != calls {
		t.Fatalf("the provider was called %d times after a permit replay", f.provider.attempts())
	}
}

func TestCancelBeforeDispatchIsAtomicAndAfterDispatchKeepsMoneyOpen(t *testing.T) {
	f := setupGateway(t, defaultBudget())

	reservation := f.reserve(t, f.a, uuid.NewString(), turnOptions{})
	view, _, err := f.prepare(t, f.a, reservation.ID, uuid.NewString(), turnOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		_, err := f.gateway.CancelInTx(t.Context(), tx, view.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	cancelled, err := f.gateway.Get(t.Context(), f.a, view.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.State != llmgateway.StateCancelled {
		t.Fatalf("state %s", cancelled.State)
	}
	if reserved, _, _ := f.budget(t, "gw-a"); reserved != 0 {
		t.Fatalf("cancelling before dispatch must release the hold, got %d", reserved)
	}
	if _, err := f.begin(t, f.a, view.ID); err == nil {
		t.Fatal("a cancelled request must not dispatch")
	}
	if f.provider.attempts() != 0 {
		t.Fatal("cancelling before dispatch must not reach the provider")
	}

	// A request that is already unknown may not be turned into a free cancel.
	f.provider.err = &llmgateway.ProviderError{Outcome: llmgateway.OutcomeUnknown, Class: "timeout"}
	second := f.reserve(t, f.a, uuid.NewString(), turnOptions{})
	live, chat, err := f.prepare(t, f.a, second.ID, uuid.NewString(), turnOptions{})
	if err != nil {
		t.Fatal(err)
	}
	permit, err := f.begin(t, f.a, live.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.gateway.Execute(t.Context(), f.a, limitsOf, permit, chat, false); err == nil {
		t.Fatal("want unknown error")
	}
	if err := f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		_, err := f.gateway.CancelInTx(t.Context(), tx, live.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	after, err := f.gateway.Get(t.Context(), f.a, live.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.State != llmgateway.StateUnknown || !after.CancelRequested {
		t.Fatalf("an unknown result must stay unknown after cancel: %+v", after)
	}
	if after.Settlement != llmgateway.SettlementUnknown {
		t.Fatalf("settlement %s must stay unknown until it is verified", after.Settlement)
	}
	if reserved, _, _ := f.budget(t, "gw-a"); reserved == 0 {
		t.Fatal("an unknown attempt must keep its hold")
	}
}

func TestUnknownCanOnlyBeClearedByVerifiedEvidence(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	f.provider.err = &llmgateway.ProviderError{Outcome: llmgateway.OutcomeUnknown, Class: "timeout"}

	reservation := f.reserve(t, f.a, uuid.NewString(), turnOptions{})
	view, chat, err := f.prepare(t, f.a, reservation.ID, uuid.NewString(), turnOptions{})
	if err != nil {
		t.Fatal(err)
	}
	permit, err := f.begin(t, f.a, view.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.gateway.Execute(t.Context(), f.a, limitsOf, permit, chat, false); err == nil {
		t.Fatal("want unknown error")
	}
	// An unknown attempt still occupies the request, so nothing re-dispatches.
	if _, err := f.begin(t, f.a, view.ID); err == nil {
		t.Fatal("unknown must block a new dispatch")
	}

	if err := f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		_, err := f.gateway.VerifyUnacceptedInTx(t.Context(), tx, view.ID, "operator:deepseek-support-ticket-1")
		return err
	}); err != nil {
		t.Fatalf("verification: %v", err)
	}
	verified, err := f.gateway.Get(t.Context(), f.a, view.ID)
	if err != nil {
		t.Fatal(err)
	}
	if verified.State != llmgateway.StatePrepared || !verified.RetryEligible {
		t.Fatalf("verified non-acceptance must re-open admission: %+v", verified)
	}
	if reserved, spent, _ := f.budget(t, "gw-a"); reserved != 0 || spent != 0 {
		t.Fatalf("verified zero cost must free the hold: reserved=%d spent=%d", reserved, spent)
	}
}

func TestSequentialCostCorrectionsSettleFromTheCurrentValue(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	view, _, err := f.turn(t, f.a, turnOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var attemptID string
	if err := f.db.QueryRow(`SELECT id FROM llm_attempts WHERE account_id='gw-a' AND request_id=$1`, view.ID).Scan(&attemptID); err != nil {
		t.Fatal(err)
	}
	_, spentBefore, _ := f.budget(t, "gw-a")

	// Same-ranked invoices only carry an order because the provider's own
	// revision counter increases; that is what lets the second one advance.
	correction := func(key, revision string, micros int64) error {
		return f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
			_, err := f.gateway.RecordMeasurementInTx(t.Context(), tx, view.ID, llmgateway.Measurement{
				AttemptID: attemptID, Key: key, Component: llmgateway.ComponentOutput, Currency: "USD",
				CostMicros: ptr(micros), Certainty: "known", Kind: llmgateway.EvidenceProviderReported,
				ProviderRev: revision, EvidenceDigest: "sha256-" + strings.Repeat("a", 64),
			})
			return err
		})
	}
	if err := correction("invoice-v1", "1", 8); err != nil {
		t.Fatal(err)
	}
	if err := correction("invoice-v2", "2", 7); err != nil {
		t.Fatal(err)
	}
	// Replaying identical evidence must never book twice.
	if err := correction("invoice-v2", "2", 7); err != nil {
		t.Fatal(err)
	}

	_, spentAfter, _ := f.budget(t, "gw-a")
	rate := usablePrice().RateAt(f.now)
	originalOutput := llmgateway.CostMicros(200, rate.Output)
	want := spentBefore - originalOutput + 7
	if spentAfter != want {
		t.Fatalf("booked %d, want %d: corrections must settle the difference from the current value", spentAfter, want)
	}
}

func TestSpendBeyondTheLimitIsBookedAndBlocksNewAdmission(t *testing.T) {
	f := setupGateway(t, llmgateway.BudgetPolicy{MonthlyLimitMicros: 500_000, MonthlyTokenLimit: 1_000_000})
	tiny := turnOptions{groupMicros: 500_000, groupTokens: 1_000_000, inputBound: 10, outputLimit: 16}

	view, _, err := f.turn(t, f.a, tiny)
	if err != nil {
		t.Fatal(err)
	}
	var attemptID string
	if err := f.db.QueryRow(`SELECT id FROM llm_attempts WHERE account_id='gw-a' AND request_id=$1`, view.ID).Scan(&attemptID); err != nil {
		t.Fatal(err)
	}
	// The provider's real invoice turns out to exceed the monthly limit.
	if err := f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		_, err := f.gateway.RecordMeasurementInTx(t.Context(), tx, view.ID, llmgateway.Measurement{
			AttemptID: attemptID, Key: "invoice", Component: llmgateway.ComponentOutput, Currency: "USD",
			CostMicros: ptr(int64(900_000)), Certainty: "known", Kind: llmgateway.EvidenceProviderReported,
			EvidenceDigest: "sha256-" + strings.Repeat("b", 64),
		})
		return err
	}); err != nil {
		t.Fatalf("real overspend must be recorded, not rejected: %v", err)
	}
	if _, spent, _ := f.budget(t, "gw-a"); spent < 900_000 {
		t.Fatalf("actual spend %d must be booked in full", spent)
	}

	err = f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		_, err := f.gateway.ReserveInTx(t.Context(), tx, llmgateway.ReserveInput{
			CallerService: "creative_agent", CallerOperationID: uuid.NewString(), CallerGroupID: "run-9",
			GroupLimitMicros: 500_000, GroupTokenLimit: 1_000_000, GroupDeadline: f.now.Add(time.Hour),
			ModelKey: "chat", InputTokenUpperBound: 10, OutputLimit: 16, ExpiresAt: f.now.Add(time.Hour),
		})
		return err
	})
	if !errors.Is(err, llmgateway.ErrBudget) {
		t.Fatalf("an overspent month must refuse new admission: %v", err)
	}
}

func TestCallerGroupCeilingDoesNotResetAcrossMonths(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	// The group ceiling admits two conservative reservations in total.
	bound := usablePrice().UpperBound()
	oneHold := llmgateway.CostMicros(100, bound.InputUncached) + llmgateway.CostMicros(256, bound.Output)
	o := turnOptions{group: "run-month", groupMicros: oneHold * 2, groupTokens: 100_000, inputBound: 100, outputLimit: 256}
	if _, _, err := f.turn(t, f.a, o); err != nil {
		t.Fatal(err)
	}

	// The same run continues in the next UTC month. Its already booked spend
	// still counts, so only the remaining part of the ceiling is available.
	f.now = f.now.AddDate(0, 1, 0)
	accepted := 0
	var lastErr error
	for range 5 {
		if _, _, err := f.turn(t, f.a, o); err != nil {
			lastErr = err
			break
		}
		accepted++
	}
	if !errors.Is(lastErr, llmgateway.ErrBudget) {
		t.Fatalf("the ceiling must eventually refuse, got %v", lastErr)
	}
	if accepted != 1 {
		t.Fatalf("accepted %d calls in the new month; a ceiling that reset per month would have accepted 2", accepted)
	}
}

func TestAccountIsolationOnEveryGatewayPort(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	view, _, err := f.turn(t, f.a, turnOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.gateway.Get(t.Context(), f.b, view.ID); !errors.Is(err, llmgateway.ErrNotFound) {
		t.Fatalf("another account read the request: %v", err)
	}
	if _, err := f.gateway.GetUsage(t.Context(), f.b, view.ID); !errors.Is(err, llmgateway.ErrNotFound) {
		t.Fatalf("another account read the money facts: %v", err)
	}
	if _, err := f.begin(t, f.b, view.ID); !errors.Is(err, llmgateway.ErrNotFound) {
		t.Fatalf("another account dispatched the request: %v", err)
	}
	err = f.b.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		_, err := f.gateway.CancelInTx(t.Context(), tx, view.ID)
		return err
	})
	if !errors.Is(err, llmgateway.ErrNotFound) {
		t.Fatalf("another account cancelled the request: %v", err)
	}
}

func TestCredentialNeverReachesThePersistedRecord(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	view, _, err := f.turn(t, f.a, turnOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var payload, model, price, result string
	if err := f.db.QueryRow(
		`SELECT request_payload::text,model_snapshot::text,price_snapshot::text,COALESCE(result_payload::text,'')
		 FROM llm_requests WHERE account_id='gw-a' AND id=$1`, view.ID).
		Scan(&payload, &model, &price, &result); err != nil {
		t.Fatal(err)
	}
	for name, blob := range map[string]string{"request": payload, "model": model, "price": price, "result": result} {
		if strings.Contains(blob, "secret-value") {
			t.Fatalf("%s payload persisted the credential", name)
		}
	}
	if strings.Contains(model, "CredentialEnv") {
		t.Fatal("the model snapshot must not carry any credential reference")
	}
}

func TestResultConsumptionIsIdempotent(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	reservation := f.reserve(t, f.a, uuid.NewString(), turnOptions{})
	operation := uuid.NewString()
	view, chat, err := f.prepare(t, f.a, reservation.ID, operation, turnOptions{})
	if err != nil {
		t.Fatal(err)
	}
	permit, err := f.begin(t, f.a, view.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.gateway.Execute(t.Context(), f.a, limitsOf, permit, chat, false); err != nil {
		t.Fatal(err)
	}

	saves := 0
	consume := func() (bool, error) {
		var replayed bool
		err := f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
			_, r, err := f.gateway.ConsumeInTx(t.Context(), tx, view.ID, "creative_agent", "step:"+operation,
				func(llmgateway.Result) error { saves++; return nil })
			replayed = r
			return err
		})
		return replayed, err
	}
	if replayed, err := consume(); err != nil || replayed {
		t.Fatalf("first consumption: replayed=%v err=%v", replayed, err)
	}
	if replayed, err := consume(); err != nil || !replayed {
		t.Fatalf("second consumption must replay instead of saving again: replayed=%v err=%v", replayed, err)
	}
	if saves != 1 {
		t.Fatalf("the caller persisted the result %d times", saves)
	}
}

func TestProviderConcurrencyIsSharedAcrossAccounts(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	// The test deployment allows two concurrent transports.
	var permits []llmgateway.Permit
	for _, scope := range []store.AccountScope{f.a, f.b, f.a} {
		reservation := f.reserve(t, scope, uuid.NewString(), turnOptions{})
		view, _, err := f.prepare(t, scope, reservation.ID, uuid.NewString(), turnOptions{})
		if err != nil {
			t.Fatal(err)
		}
		permit, err := f.begin(t, scope, view.ID)
		if err != nil {
			if len(permits) == 2 && errors.Is(err, llmgateway.ErrRateLimited) {
				return
			}
			t.Fatalf("dispatch %d: %v", len(permits), err)
		}
		permits = append(permits, permit)
	}
	t.Fatal("the shared provider limit must stop the third concurrent dispatch")
}

func TestRequestPayloadMismatchNeverDispatches(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	reservation := f.reserve(t, f.a, uuid.NewString(), turnOptions{})
	view, chat, err := f.prepare(t, f.a, reservation.ID, uuid.NewString(), turnOptions{})
	if err != nil {
		t.Fatal(err)
	}
	permit, err := f.begin(t, f.a, view.ID)
	if err != nil {
		t.Fatal(err)
	}
	tampered := chat
	tampered.Messages = []llmgateway.Message{{
		Role:   llmgateway.RoleUser,
		Blocks: []llmgateway.Block{{Kind: llmgateway.BlockText, Text: "换一个提示词"}},
	}}
	if _, err := f.gateway.Execute(t.Context(), f.a, limitsOf, permit, tampered, false); !errors.Is(err, llmgateway.ErrConflict) {
		t.Fatalf("a payload that differs from the prepared request must not be sent: %v", err)
	}
	if f.provider.attempts() != 0 {
		t.Fatal("the tampered payload reached the provider")
	}
}

func TestDispatchedRequestCarriesTheFixedDeploymentModel(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	var seen llmgateway.ProviderRequest
	f.provider.observe = func(r llmgateway.ProviderRequest) { seen = r }
	if _, _, err := f.turn(t, f.a, turnOptions{}); err != nil {
		t.Fatal(err)
	}
	if seen.Model.RequestModelID != "vendor-model" || seen.Snapshot.CatalogVersion != "cat-1" {
		t.Fatalf("dispatch used %+v", seen.Snapshot)
	}
	if seen.Credential == "" {
		t.Fatal("the adapter must receive the credential only at send time")
	}
	raw, err := json.Marshal(seen.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "secret-value") {
		t.Fatal("the snapshot carried the credential")
	}
}

func TestSettlementFailureNeverDiscardsAPaidResult(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	// The provider reports more cached input than total input, which is not a
	// billable split the recipe can settle.
	result := okResult()
	result.Usage.InputCachedTokens = ptr(int64(5000))
	f.provider.result = result

	view, got, err := f.turn(t, f.a, turnOptions{})
	if err != nil {
		t.Fatalf("a paid, complete answer must still be delivered: %v", err)
	}
	if got.Text != "三个方向" {
		t.Fatalf("result %+v", got)
	}
	stored, err := f.gateway.Get(t.Context(), f.a, view.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.State != llmgateway.StateSucceeded || stored.Result == nil {
		t.Fatalf("the charged result was discarded by a settlement problem: %+v", stored)
	}
	// The output component is still known and must be booked; only the suspect
	// input part keeps its hold.
	rate := usablePrice().RateAt(f.now)
	reserved, spent, _ := f.budget(t, "gw-a")
	if spent != llmgateway.CostMicros(200, rate.Output) {
		t.Fatalf("the known output component must still be booked, got %d", spent)
	}
	if reserved == 0 {
		t.Fatal("the unusable input split must keep its hold")
	}
}

// TestRetryAfterRejectionKeepsTheNewAttemptsHold pins the hold accounting
// across attempts: a previous attempt's zero-cost rejection evidence must not
// release the hold of the attempt that is actually running.
func TestRetryAfterRejectionKeepsTheNewAttemptsHold(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	f.provider.err = &llmgateway.ProviderError{Outcome: llmgateway.OutcomeUnaccepted, Class: "admission_refused", StatusCode: 429}

	reservation := f.reserve(t, f.a, uuid.NewString(), turnOptions{})
	view, chat, err := f.prepare(t, f.a, reservation.ID, uuid.NewString(), turnOptions{})
	if err != nil {
		t.Fatal(err)
	}
	permit, err := f.begin(t, f.a, view.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.gateway.Execute(t.Context(), f.a, limitsOf, permit, chat, false); err == nil {
		t.Fatal("want an unaccepted dispatch error")
	}
	if err := f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		_, err := f.gateway.RearmInTx(t.Context(), tx, view.ID, 1, 0, 0)
		return err
	}); err != nil {
		t.Fatalf("re-admission: %v", err)
	}

	// Attempt 2 succeeds but the provider only reports the output dimension.
	partial := okResult()
	partial.Usage = llmgateway.UsageEvidence{OutputTokens: ptr(int64(200))}
	f.provider.err, f.provider.result = nil, partial

	permit, err = f.begin(t, f.a, view.ID)
	if err != nil {
		t.Fatalf("second dispatch: %v", err)
	}
	if _, err := f.gateway.Execute(t.Context(), f.a, limitsOf, permit, chat, false); err != nil {
		t.Fatal(err)
	}

	usage, err := f.gateway.GetUsage(t.Context(), f.a, view.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Exactly the input part of the original bound stays held: the output part is
	// booked, and attempt 1's zero evidence may not release anything here.
	bound := usablePrice().UpperBound()
	inputBound := llmgateway.CostMicros(1200, bound.InputUncached)
	if usage.HoldMicros != inputBound {
		t.Fatalf("the unreported input of attempt 2 must keep exactly its own hold: %d, want %d",
			usage.HoldMicros, inputBound)
	}
	rate := usablePrice().RateAt(f.now)
	if _, spent, _ := f.budget(t, "gw-a"); spent != llmgateway.CostMicros(200, rate.Output) {
		t.Fatalf("only the reported output may be booked, got %d", spent)
	}
}

// TestLowerRankedEvidenceIsParkedNotBooked protects a provider invoice from
// being rolled back by a later, weaker number.
func TestLowerRankedEvidenceIsParkedNotBooked(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	view, _, err := f.turn(t, f.a, turnOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var attemptID string
	if err := f.db.QueryRow(`SELECT id FROM llm_attempts WHERE account_id='gw-a' AND request_id=$1`, view.ID).Scan(&attemptID); err != nil {
		t.Fatal(err)
	}
	record := func(key string, micros int64, kind llmgateway.EvidenceKind) error {
		return f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
			_, err := f.gateway.RecordMeasurementInTx(t.Context(), tx, view.ID, llmgateway.Measurement{
				AttemptID: attemptID, Key: key, Component: llmgateway.ComponentOutput, Currency: "USD",
				CostMicros: ptr(micros), Certainty: "known", Kind: kind,
				EvidenceDigest: "sha256-" + strings.Repeat("c", 64),
			})
			return err
		})
	}
	if err := record("invoice", 900_000, llmgateway.EvidenceProviderReported); err != nil {
		t.Fatal(err)
	}
	_, afterInvoice, _ := f.budget(t, "gw-a")
	before, err := f.gateway.GetUsage(t.Context(), f.a, view.ID)
	if err != nil {
		t.Fatal(err)
	}
	if before.OpenEvidence != 0 {
		t.Fatalf("the invoice must be applied, not parked: %+v", before)
	}

	if err := record("operator-guess", 7, llmgateway.EvidenceOperatorAttested); err != nil {
		t.Fatalf("weaker evidence must be accepted for the record: %v", err)
	}
	_, afterGuess, _ := f.budget(t, "gw-a")
	if afterGuess != afterInvoice {
		t.Fatalf("a lower ranked number rolled the ledger back from %d to %d", afterInvoice, afterGuess)
	}

	usage, err := f.gateway.GetUsage(t.Context(), f.a, view.ID)
	if err != nil {
		t.Fatal(err)
	}
	if usage.OpenEvidence != 1 {
		t.Fatalf("exactly the parked estimate must await human reconciliation: %+v", usage)
	}
}

// TestSameRankedEvidenceNeedsADecidableOrder covers the out-of-order case: two
// provider invoices of equal standing carry no order by arrival, so a restated
// one may not roll a booked amount back. Only the provider's own increasing
// revision, or an explicit chain to what is booked, may move money.
func TestSameRankedEvidenceNeedsADecidableOrder(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	view, _, err := f.turn(t, f.a, turnOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var attemptID string
	if err := f.db.QueryRow(`SELECT id FROM llm_attempts WHERE account_id='gw-a' AND request_id=$1`, view.ID).Scan(&attemptID); err != nil {
		t.Fatal(err)
	}
	invoice := func(key, revision, supersedes string, micros int64) error {
		return f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
			_, err := f.gateway.RecordMeasurementInTx(t.Context(), tx, view.ID, llmgateway.Measurement{
				AttemptID: attemptID, Key: key, Component: llmgateway.ComponentOutput, Currency: "USD",
				CostMicros: ptr(micros), Certainty: "known", Kind: llmgateway.EvidenceProviderReported,
				ProviderRev: revision, Supersedes: supersedes,
				EvidenceDigest: "sha256-" + strings.Repeat("d", 64),
			})
			return err
		})
	}
	if err := invoice("invoice", "9", "", 900_000); err != nil {
		t.Fatal(err)
	}
	_, booked, _ := f.budget(t, "gw-a")
	usage, err := f.gateway.GetUsage(t.Context(), f.a, view.ID)
	if err != nil {
		t.Fatal(err)
	}
	if usage.OpenEvidence != 0 {
		t.Fatalf("the invoice itself must be applied, not parked: %+v", usage)
	}

	// An older restatement of the same standing: kept as history, never booked.
	if err := invoice("invoice-restated", "3", "", 7); err != nil {
		t.Fatalf("undecidable evidence must still be accepted for the record: %v", err)
	}
	if _, after, _ := f.budget(t, "gw-a"); after != booked {
		t.Fatalf("an undecidable same ranked number moved the ledger from %d to %d", booked, after)
	}
	usage, err = f.gateway.GetUsage(t.Context(), f.a, view.ID)
	if err != nil {
		t.Fatal(err)
	}
	if usage.OpenEvidence != 1 {
		t.Fatalf("exactly the parked restatement must await reconciliation: %+v", usage)
	}

	// The explicit correction chain is the way through: it names what is booked.
	var current string
	if err := f.db.QueryRow(`SELECT current_measurement_id FROM llm_cost_positions
		 WHERE account_id='gw-a' AND attempt_id=$1 AND cost_component='output'`, attemptID).Scan(&current); err != nil {
		t.Fatal(err)
	}
	if err := invoice("invoice-confirmed", "", current, 7); err != nil {
		t.Fatal(err)
	}
	_, afterChain, _ := f.budget(t, "gw-a")
	if afterChain != booked-900_000+7 {
		t.Fatalf("a confirmed correction must settle the difference: %d, want %d", afterChain, booked-900_000+7)
	}
}

// TestForeignCurrencyEvidenceIsParkedNotConverted keeps a bucket meaning one
// currency: nothing silently adds 900_000 EUR micros to a USD month.
func TestForeignCurrencyEvidenceIsParkedNotConverted(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	// The provider reports only its output dimension, so the input components
	// have no booked position yet. That is where a foreign currency number would
	// open a fresh position and be added to the USD month unnoticed.
	partial := okResult()
	partial.Usage = llmgateway.UsageEvidence{OutputTokens: ptr(int64(200))}
	f.provider.result = partial
	view, _, err := f.turn(t, f.a, turnOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var attemptID string
	if err := f.db.QueryRow(`SELECT id FROM llm_attempts WHERE account_id='gw-a' AND request_id=$1`, view.ID).Scan(&attemptID); err != nil {
		t.Fatal(err)
	}
	_, booked, _ := f.budget(t, "gw-a")
	before, err := f.gateway.GetUsage(t.Context(), f.a, view.ID)
	if err != nil {
		t.Fatal(err)
	}

	if err := f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		_, err := f.gateway.RecordMeasurementInTx(t.Context(), tx, view.ID, llmgateway.Measurement{
			AttemptID: attemptID, Key: "invoice-eur", Component: llmgateway.ComponentInputUncached, Currency: "EUR",
			CostMicros: ptr(int64(900_000)), Certainty: "known", Kind: llmgateway.EvidenceProviderReported,
			EvidenceDigest: "sha256-" + strings.Repeat("e", 64),
		})
		return err
	}); err != nil {
		t.Fatalf("foreign currency evidence must be kept for reconciliation: %v", err)
	}
	if _, after, _ := f.budget(t, "gw-a"); after != booked {
		t.Fatalf("foreign currency evidence moved the USD bucket from %d to %d", booked, after)
	}
	var positions int
	if err := f.db.QueryRow(`SELECT count(*) FROM llm_cost_positions
		 WHERE account_id='gw-a' AND attempt_id=$1 AND currency='EUR'`, attemptID).Scan(&positions); err != nil {
		t.Fatal(err)
	}
	if positions != 0 {
		t.Fatalf("no position may be opened in a currency the request never reserved: %d", positions)
	}
	after, err := f.gateway.GetUsage(t.Context(), f.a, view.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.HoldMicros != before.HoldMicros {
		t.Fatalf("the unreported input must keep its hold: %d, was %d", after.HoldMicros, before.HoldMicros)
	}
	if after.OpenEvidence != before.OpenEvidence+1 {
		t.Fatalf("the parked foreign currency evidence must be visible: %+v, was %+v", after, before)
	}
}

func TestAttemptsAreExhaustedAfterTwoRealTries(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	f.provider.err = &llmgateway.ProviderError{Outcome: llmgateway.OutcomeUnaccepted, Class: "admission_refused", StatusCode: 429}

	reservation := f.reserve(t, f.a, uuid.NewString(), turnOptions{})
	view, chat, err := f.prepare(t, f.a, reservation.ID, uuid.NewString(), turnOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 1; attempt <= 2; attempt++ {
		if attempt == 2 {
			// The first rejection released the original generation 1 hold.
			if err := f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
				_, err := f.gateway.RearmInTx(t.Context(), tx, view.ID, 1, 0, 0)
				return err
			}); err != nil {
				t.Fatalf("re-admission %d: %v", attempt, err)
			}
		}
		permit, err := f.begin(t, f.a, view.ID)
		if err != nil {
			t.Fatalf("dispatch %d: %v", attempt, err)
		}
		if _, err := f.gateway.Execute(t.Context(), f.a, limitsOf, permit, chat, false); err == nil {
			t.Fatalf("dispatch %d must fail", attempt)
		}
	}
	stored, err := f.gateway.Get(t.Context(), f.a, view.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.State != llmgateway.StateFailed || stored.RetryEligible {
		t.Fatalf("after two real attempts the request must end: %+v", stored)
	}
	// The failed state is what stops a third admission; the explicit attempt
	// bound behind it is defence in depth and is not what this asserts.
	if err := f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		_, err := f.gateway.RearmInTx(t.Context(), tx, view.ID, 2, 0, 0)
		return err
	}); !errors.Is(err, llmgateway.ErrState) {
		t.Fatalf("a third admission must be refused for state, got %v", err)
	}
	if f.provider.attempts() != 2 {
		t.Fatalf("the provider saw %d real attempts", f.provider.attempts())
	}
}

// TestCancelledTurnStillReleasesTheSharedSlot covers the caller walking away
// mid-call: what the attempt proved has to be recorded and the provider's shared
// concurrency slot given back, neither of which may depend on the caller still
// waiting.
func TestCancelledTurnStillReleasesTheSharedSlot(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	// The provider call ends because the caller cancelled it in flight.
	f.provider.observe = func(llmgateway.ProviderRequest) { cancel() }
	f.provider.err = &llmgateway.ProviderError{
		Outcome: llmgateway.OutcomeUnknown, Class: "cancelled_in_flight", Detail: "context canceled",
	}

	reservation := f.reserve(t, f.a, uuid.NewString(), turnOptions{})
	view, chat, err := f.prepare(t, f.a, reservation.ID, uuid.NewString(), turnOptions{})
	if err != nil {
		t.Fatal(err)
	}
	permit, err := f.begin(t, f.a, view.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.gateway.Execute(ctx, f.a, limitsOf, permit, chat, false); err == nil {
		t.Fatal("a cancelled call must surface an error")
	}

	var dispatchState string
	if err := f.db.QueryRow(`SELECT dispatch_state FROM llm_attempts WHERE account_id='gw-a' AND request_id=$1`,
		view.ID).Scan(&dispatchState); err != nil {
		t.Fatal(err)
	}
	if dispatchState != "unknown" {
		t.Fatalf("a cancelled transport must be recorded as unknown, got %q", dispatchState)
	}
	var active int
	if err := f.db.QueryRow(`SELECT active_count FROM platform_llm_limits WHERE limit_key='dep-1'`).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if active != 0 {
		t.Fatalf("the shared provider slot leaked: active_count=%d", active)
	}
	// The money question stays open: local transport ended, the provider's side
	// did not necessarily.
	usage, err := f.gateway.GetUsage(t.Context(), f.a, view.ID)
	if err != nil {
		t.Fatal(err)
	}
	if usage.HoldMicros == 0 || usage.Settlement != llmgateway.SettlementUnknown {
		t.Fatalf("an unknown outcome must keep its hold and stay unknown: %+v", usage)
	}
}

// TestCatalogChangeNeverRedirectsAPreparedRequest covers a deployment edit
// landing between Prepare and the send: the call must not go to a different paid
// model, and refusing after the answer arrives would be too late.
func TestCatalogChangeNeverRedirectsAPreparedRequest(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	reservation := f.reserve(t, f.a, uuid.NewString(), turnOptions{})
	view, chat, err := f.prepare(t, f.a, reservation.ID, uuid.NewString(), turnOptions{})
	if err != nil {
		t.Fatal(err)
	}
	permit, err := f.begin(t, f.a, view.ID)
	if err != nil {
		t.Fatal(err)
	}

	// The same model key now names a different vendor model: a process restarted
	// with an updated catalog holds a permit that was frozen against the old one.
	updated, err := llmgateway.NewCatalog("cat-2", "USD", []llmgateway.ModelConfig{{
		ModelKey: "chat", DisplayName: "Chat", Provider: llmgateway.ProviderOpenAICompatible,
		DeploymentKey: "dep-2", BaseURL: "https://example.invalid", CredentialEnv: "TEST_KEY",
		RequestModelID: "different-paid-model", AcceptedModelIDs: []string{"different-paid-model"},
		Capability: llmgateway.Capability{ToolCalling: true, ContextTokens: 1000, MaxOutputTokens: 500},
		Price:      usablePrice(), LimitKey: "dep-1", Concurrency: 2, RateLimitPerMin: 10,
		QueryCapability: "none", CancelCapability: "none", Enabled: true,
	}})
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := llmgateway.New(llmgateway.Config{
		Catalog:       updated,
		Providers:     map[llmgateway.ProviderKey]llmgateway.Provider{llmgateway.ProviderOpenAICompatible: f.provider},
		Credential:    func(string) (string, bool) { return "secret-value", true },
		Clock:         func() time.Time { return f.now },
		DefaultBudget: defaultBudget(),
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := restarted.Execute(t.Context(), f.a, limitsOf, permit, chat, false); !errors.Is(err, llmgateway.ErrConflict) {
		t.Fatalf("a request prepared against another deployment must be refused, got %v", err)
	}
	if f.provider.attempts() != 0 {
		t.Fatalf("nothing may be sent to the new deployment: %d calls", f.provider.attempts())
	}
}

// TestTokenTotalIsBookedWithoutTheCacheSplit keeps the two budgets independent:
// a missing cache breakdown makes the cost unknown, not the token count.
func TestTokenTotalIsBookedWithoutTheCacheSplit(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	uncached := okResult()
	uncached.Usage = llmgateway.UsageEvidence{InputTotalTokens: ptr(int64(1000)), OutputTokens: ptr(int64(200))}
	f.provider.result = uncached

	view, _, err := f.turn(t, f.a, turnOptions{})
	if err != nil {
		t.Fatal(err)
	}
	usage, err := f.gateway.GetUsage(t.Context(), f.a, view.ID)
	if err != nil {
		t.Fatal(err)
	}
	if usage.BookedTokens != 1200 || usage.HoldTokens != 0 {
		t.Fatalf("the token total is known and must be settled: booked=%d hold=%d",
			usage.BookedTokens, usage.HoldTokens)
	}
	// The input cost still is not known, so that hold stays.
	bound := usablePrice().UpperBound()
	if usage.HoldMicros != llmgateway.CostMicros(1200, bound.InputUncached) {
		t.Fatalf("the unpriceable input must keep exactly its cost hold: %d", usage.HoldMicros)
	}
	rate := usablePrice().RateAt(f.now)
	if _, spent, _ := f.budget(t, "gw-a"); spent != llmgateway.CostMicros(200, rate.Output) {
		t.Fatalf("only the priceable output may be booked, got %d", spent)
	}
}

func TestAccountIsolationOnTheRemainingGatewayPorts(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	view, _, err := f.turn(t, f.a, turnOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var attemptID string
	if err := f.db.QueryRow(`SELECT id FROM llm_attempts WHERE account_id='gw-a' AND request_id=$1`, view.ID).Scan(&attemptID); err != nil {
		t.Fatal(err)
	}
	// Each port must refuse the other account because the row is not in its
	// scope at all, and must fail somewhere else for the owning account —
	// otherwise the assertion would still hold with isolation removed.
	cases := []struct {
		name    string
		foreign error
		call    func(tx store.TxAccountScope) error
	}{
		{"RecordMeasurementInTx", llmgateway.ErrConflict, func(tx store.TxAccountScope) error {
			_, err := f.gateway.RecordMeasurementInTx(t.Context(), tx, view.ID, llmgateway.Measurement{
				AttemptID: attemptID, Key: "x", Component: llmgateway.ComponentOutput, Currency: "USD",
				CostMicros: ptr(int64(1)), Certainty: "known", Kind: llmgateway.EvidenceOperatorAttested,
				EvidenceDigest: "sha256-" + strings.Repeat("f", 64),
			})
			return err
		}},
		{"ConsumeInTx", llmgateway.ErrNotFound, func(tx store.TxAccountScope) error {
			_, _, err := f.gateway.ConsumeInTx(t.Context(), tx, view.ID, "creative_agent", "x", nil)
			return err
		}},
		{"RearmInTx", llmgateway.ErrNotFound, func(tx store.TxAccountScope) error {
			_, err := f.gateway.RearmInTx(t.Context(), tx, view.ID, 1, 0, 0)
			return err
		}},
		{"VerifyUnacceptedInTx", llmgateway.ErrNotFound, func(tx store.TxAccountScope) error {
			_, err := f.gateway.VerifyUnacceptedInTx(t.Context(), tx, view.ID, "operator:x")
			return err
		}},
		{"CloseInTx", llmgateway.ErrNotFound, func(tx store.TxAccountScope) error {
			return f.gateway.CloseInTx(t.Context(), tx, view.ID)
		}},
	}
	for _, c := range cases {
		if err := f.b.WithTxScope(t.Context(), c.call); !errors.Is(err, c.foreign) {
			t.Fatalf("%s must refuse another account with %v, got %v", c.name, c.foreign, err)
		}
		if err := f.a.WithTxScope(t.Context(), c.call); errors.Is(err, c.foreign) {
			t.Fatalf("%s answers %v to its own account too, so the check above proves nothing about isolation",
				c.name, c.foreign)
		}
	}
}
