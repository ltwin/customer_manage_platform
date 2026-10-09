package llmgateway_test

import (
	"bytes"
	"compress/zlib"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"hash/crc32"
	"io"
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
	return liveFixtureFor(t, "DEEPSEEK_API_KEY", liveTestBudgetMicros)
}

// liveFixtureFor is liveFixture for any vendor: skipped without its
// credential, and capped at budget for the whole test account.
func liveFixtureFor(t *testing.T, credential string, budget int64) (*llmgateway.Service, store.AccountScope, *sql.DB) {
	t.Helper()
	if strings.TrimSpace(os.Getenv(credential)) == "" {
		t.Skip(credential + " not set: skipping the real provider acceptance")
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
		MonthlyLimitMicros: budget,
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
			CallerService:     "creative_agent",
			CallerOperationID: uuid.NewString(),
			CallerGroupID:     group,
			// Zero takes the account's monthly cap, which the fixture set.
			GroupLimitMicros:     0,
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

// TestLiveDeepSeekThinkingToolLoop is the evidence that the thinking
// deployment can carry a tool loop: the second request hands the first turn's
// reasoning back, and an earlier answer without any reasoning goes back as an
// empty one. Before the gateway did this DeepSeek answered 400 "The
// reasoning_content in the thinking mode must be passed back to the API".
func TestLiveDeepSeekThinkingToolLoop(t *testing.T) {
	gateway, scope, _ := liveFixture(t)
	chat := liveChat(2000, "先调用 read_canvas 看看画布，再用一句话打招呼。")
	chat.ModelKey = "deepseek-flash-thinking"
	chat.Messages = append(chat.Messages[:1],
		llmgateway.Message{Role: llmgateway.RoleUser, Blocks: []llmgateway.Block{{Kind: llmgateway.BlockText, Text: "你好"}}},
		llmgateway.Message{Role: llmgateway.RoleAssistant, Blocks: []llmgateway.Block{{Kind: llmgateway.BlockText, Text: "你好！"}}},
		chat.Messages[1],
	)
	chat.Tools = []llmgateway.ToolDefinition{{
		Name: "read_canvas", Description: "读取画布上的节点", SchemaVersion: 1,
		InputSchema: json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{}}`),
	}}
	_, first := liveTurn(t, gateway, scope, "live-thinking", chat, true)
	if len(first.ToolCalls) == 0 || first.Reasoning == "" {
		t.Fatalf("expected a reasoned tool call: %+v", first)
	}
	answer := llmgateway.Message{Role: llmgateway.RoleAssistant, Reasoning: first.Reasoning, ToolCalls: first.ToolCalls}
	if first.Text != "" {
		answer.Blocks = []llmgateway.Block{{Kind: llmgateway.BlockText, Text: first.Text}}
	}
	chat.Messages = append(chat.Messages, answer)
	for _, call := range first.ToolCalls {
		chat.Messages = append(chat.Messages, llmgateway.Message{Role: llmgateway.RoleTool, ToolCallID: call.ID, Blocks: []llmgateway.Block{{Kind: llmgateway.BlockText, Text: "画布上有 3 个节点"}}})
	}
	_, second := liveTurn(t, gateway, scope, "live-thinking", chat, true)
	if strings.TrimSpace(second.Text) == "" {
		t.Fatalf("the second turn carried no answer: %+v", second)
	}
	t.Logf("reasoning %d bytes; answer: %s", len(first.Reasoning), second.Text)
}

// TestLiveDeepSeekFlashSeesAnImage is the evidence behind enabling image input
// on DeepSeek Flash: the image is billed within the per-image bound and the
// answer is about its pixels.
func TestLiveDeepSeekFlashSeesAnImage(t *testing.T) {
	gateway, scope, _ := liveFixture(t)
	raw := liveGradientPNG(400, 400)
	sum := sha256.Sum256(raw)
	chat := liveChat(200, "这张图左半边和右半边分别是什么颜色？一句话。")
	chat.Messages[1].Blocks = append(chat.Messages[1].Blocks, llmgateway.Block{Kind: llmgateway.BlockImageInput, Image: &llmgateway.ImageInput{
		RevisionID: "ccrv_live", Digest: "sha256-" + hex.EncodeToString(sum[:]), MIME: "image/png", ByteSize: int64(len(raw)),
		Open: func(context.Context) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(raw)), nil },
	}})
	_, result := liveTurn(t, gateway, scope, "live-vision", chat, true)
	if result.Usage.InputTotalTokens == nil || *result.Usage.InputTotalTokens < 100 || *result.Usage.InputTotalTokens > 4096+200 {
		t.Fatalf("image input tokens outside the measured range: %+v", result.Usage)
	}
	if !strings.Contains(result.Text, "红") || !strings.Contains(result.Text, "蓝") {
		t.Fatalf("the answer does not describe the image: %s", result.Text)
	}
	t.Logf("input tokens %d; answer: %s", *result.Usage.InputTotalTokens, result.Text)
}

// liveGradientPNG is a left-red, right-blue image built without image libraries.
func liveGradientPNG(w, h int) []byte {
	row := make([]byte, 1+w*3)
	for x := 0; x < w; x++ {
		if x < w/2 {
			row[1+x*3] = 220
		} else {
			row[1+x*3+2] = 220
		}
	}
	var pixels bytes.Buffer
	for y := 0; y < h; y++ {
		pixels.Write(row)
	}
	var compressed bytes.Buffer
	z := zlib.NewWriter(&compressed)
	_, _ = z.Write(pixels.Bytes())
	_ = z.Close()
	var out bytes.Buffer
	out.WriteString("\x89PNG\r\n\x1a\n")
	chunk := func(kind string, data []byte) {
		_ = binary.Write(&out, binary.BigEndian, uint32(len(data)))
		out.WriteString(kind)
		out.Write(data)
		_ = binary.Write(&out, binary.BigEndian, crc32.ChecksumIEEE(append([]byte(kind), data...)))
	}
	header := make([]byte, 13)
	binary.BigEndian.PutUint32(header[0:], uint32(w))
	binary.BigEndian.PutUint32(header[4:], uint32(h))
	header[8], header[9] = 8, 2
	chunk("IHDR", header)
	chunk("IDAT", compressed.Bytes())
	chunk("IEND", nil)
	return out.Bytes()
}
