package llmgateway_test

import (
	"database/sql"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store/storetest"
)

// liveTestBudgetMicros caps what this whole test may ever spend: 0.05 USD.
const liveTestBudgetMicros = 50_000

// liveFixture drives the real, approved DeepSeek deployment. It is skipped
// unless the credential is present in the environment, so the default gate
// never makes a paid call.
func liveFixture(t *testing.T) (*llmgateway.Service, store.AccountScope, *sql.DB) {
	t.Helper()
	if strings.TrimSpace(os.Getenv("DEEPSEEK_API_KEY")) == "" {
		t.Skip("DEEPSEEK_API_KEY not set: skipping the real provider acceptance")
	}
	url := storetest.NewURL(t)
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`INSERT INTO accounts(id,password_hash,status) VALUES ('live-a','test','active')`); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.Context(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)

	gateway, err := llmgateway.Build(llmgateway.Options{
		MonthlyLimitMicros: liveTestBudgetMicros,
		MonthlyTokenLimit:  200_000,
		Timeout:            90 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	return gateway, st.ScopeFor(auth.AccountContext{AccountID: "live-a"}), db
}

func liveTurn(
	t *testing.T,
	gateway *llmgateway.Service,
	scope store.AccountScope,
	group string,
	chat llmgateway.ChatRequest,
	stream bool,
) (llmgateway.RequestView, llmgateway.Result) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Minute)
	var reservation llmgateway.Reservation
	if err := scope.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		var err error
		reservation, err = gateway.ReserveInTx(t.Context(), tx, llmgateway.ReserveInput{
			CallerService:        "creative_agent",
			CallerOperationID:    uuid.NewString(),
			CallerGroupID:        group,
			GroupLimitMicros:     liveTestBudgetMicros,
			GroupTokenLimit:      200_000,
			GroupDeadline:        deadline,
			ModelKey:             chat.ModelKey,
			InputTokenUpperBound: llmgateway.EstimateInputTokens(chat),
			OutputLimit:          chat.OutputLimit,
			ExpiresAt:            deadline,
		})
		return err
	}); err != nil {
		t.Fatalf("reserve: %v", err)
	}

	operation := uuid.NewString()
	var view llmgateway.RequestView
	if err := scope.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		var err error
		view, err = gateway.PrepareInTx(t.Context(), tx, llmgateway.PrepareInput{
			CallerService:     "creative_agent",
			CallerOperationID: operation,
			CallerGroupID:     group,
			ReservationID:     reservation.ID,
			ConsumerKey:       "live:" + operation,
			Chat:              chat,
			Deadline:          deadline,
		})
		return err
	}); err != nil {
		t.Fatalf("prepare: %v", err)
	}

	var permit llmgateway.Permit
	if err := scope.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		var err error
		permit, err = gateway.BeginDispatchInTx(t.Context(), tx, tx.LLMLimitView(), view.ID)
		return err
	}); err != nil {
		t.Fatalf("begin dispatch: %v", err)
	}

	result, err := gateway.Execute(t.Context(), scope, limitsOf, permit, chat, stream)
	if err != nil {
		t.Fatalf("execute against the real deployment: %v", err)
	}
	return view, result
}

func liveChat(outputLimit int, prompt string) llmgateway.ChatRequest {
	return llmgateway.ChatRequest{
		ContractVersion: llmgateway.ContractVersion,
		ModelKey:        "deepseek-flash",
		OutputLimit:     outputLimit,
		Messages: []llmgateway.Message{
			{Role: llmgateway.RoleSystem, Blocks: []llmgateway.Block{{Kind: llmgateway.BlockText, Text: "你是摄影创作助手，回答尽量简短。"}}},
			{Role: llmgateway.RoleUser, Blocks: []llmgateway.Block{{Kind: llmgateway.BlockText, Text: prompt}}},
		},
	}
}

// TestLiveDeepSeekCompletesAndSettles is the real-provider admission evidence
// required by GH-01 before a deployment may be enabled.
func TestLiveDeepSeekCompletesAndSettles(t *testing.T) {
	gateway, scope, db := liveFixture(t)

	view, result := liveTurn(t, gateway, scope, "live-plain", liveChat(200, "用一句话说明逆光人像的关键。"), false)
	if strings.TrimSpace(result.Text) == "" {
		t.Fatalf("the real deployment returned no text: %+v", result)
	}
	if !result.Consumable() {
		t.Fatalf("finish reason %q", result.FinishReason)
	}
	if !result.Usage.Complete() {
		t.Fatalf("DeepSeek reports all three billable dimensions; got %+v", result.Usage)
	}
	if *result.Usage.InputCachedTokens > *result.Usage.InputTotalTokens {
		t.Fatal("cached input cannot exceed total input")
	}

	stored, err := gateway.Get(t.Context(), scope, view.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.State != llmgateway.StateSucceeded || stored.Settlement != llmgateway.SettlementSettled {
		t.Fatalf("state %s settlement %s", stored.State, stored.Settlement)
	}

	usage, err := gateway.GetUsage(t.Context(), scope, view.ID)
	if err != nil {
		t.Fatal(err)
	}
	if usage.BookedMicros <= 0 || usage.HoldMicros != 0 {
		t.Fatalf("a settled real call must book real money and hold nothing: %+v", usage)
	}

	// Recompute the invoice from the published recipe and the reported usage.
	catalog, err := llmgateway.DefaultCatalog(nil)
	if err != nil {
		t.Fatal(err)
	}
	model, err := catalog.Model("deepseek-flash")
	if err != nil {
		t.Fatal(err)
	}
	var dispatchedAt time.Time
	if err := db.QueryRow(
		`SELECT dispatched_at FROM llm_attempts WHERE account_id='live-a' AND request_id=$1`, view.ID).
		Scan(&dispatchedAt); err != nil {
		t.Fatal(err)
	}
	rate := model.Price.RateAt(dispatchedAt)
	cached, total, output := *result.Usage.InputCachedTokens, *result.Usage.InputTotalTokens, *result.Usage.OutputTokens
	want := llmgateway.CostMicros(cached, rate.InputCached) +
		llmgateway.CostMicros(total-cached, rate.InputUncached) +
		llmgateway.CostMicros(output, rate.Output)
	if usage.BookedMicros != want {
		t.Fatalf("booked %d micros but the price recipe yields %d", usage.BookedMicros, want)
	}
	t.Logf("real DeepSeek call: input=%d cached=%d output=%d booked=%d micros USD", total, cached, output, usage.BookedMicros)
}

func TestLiveDeepSeekStreamsAndReassembles(t *testing.T) {
	gateway, scope, _ := liveFixture(t)
	_, result := liveTurn(t, gateway, scope, "live-stream", liveChat(200, "列出两种常见的逆光补光方式。"), true)
	if strings.TrimSpace(result.Text) == "" {
		t.Fatalf("the reassembled stream carried no text: %+v", result)
	}
	if !result.Usage.Complete() {
		t.Fatalf("streamed usage must still be complete: %+v", result.Usage)
	}
}

func TestLiveDeepSeekProducesValidatedToolCalls(t *testing.T) {
	gateway, scope, _ := liveFixture(t)
	chat := liveChat(256, "帮我找三张逆光人像参考。")
	chat.Tools = []llmgateway.ToolDefinition{{
		Name:          "search_assets",
		Description:   "在摄影师的个人素材库里检索参考素材",
		SchemaVersion: 1,
		InputSchema: json.RawMessage(`{"type":"object","additionalProperties":false,` +
			`"properties":{"query":{"type":"string","maxLength":40}},"required":["query"]}`),
	}}
	chat.ToolChoice = "required"

	_, result := liveTurn(t, gateway, scope, "live-tools", chat, true)
	if len(result.ToolCalls) == 0 {
		t.Fatalf("tool_choice=required produced no call: %+v", result)
	}
	call := result.ToolCalls[0]
	if call.ID == "" || call.Name != "search_assets" {
		t.Fatalf("tool call identity %+v", call)
	}
	var arguments struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(call.Arguments, &arguments); err != nil {
		t.Fatalf("reassembled arguments are not valid json: %s (%v)", call.Arguments, err)
	}
	if arguments.Query == "" {
		t.Fatalf("the model produced empty arguments: %s", call.Arguments)
	}
	if !result.Consumable() {
		t.Fatalf("finish reason %q must allow tool execution", result.FinishReason)
	}
	t.Logf("real DeepSeek tool call: %s(%s)", call.Name, call.Arguments)
}
