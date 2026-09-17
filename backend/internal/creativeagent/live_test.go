package creativeagent

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
)

// Explicit opt-in only. The live fixture caps the output at 128 tokens and its
// isolated account at USD 0.05; no production account or content is involved.
func TestCDeepSeekLiveHarness(t *testing.T) {
	if os.Getenv("CREATIVE_AGENT_LIVE") != "1" || strings.TrimSpace(os.Getenv("DEEPSEEK_API_KEY")) == "" {
		t.Skip("explicit live acceptance not enabled")
	}
	f := setup(t)
	defaults, err := llmgateway.DefaultCatalog(nil)
	if err != nil {
		t.Fatal(err)
	}
	model, err := defaults.Model(testModelKey)
	if err != nil {
		t.Fatal(err)
	}
	model.Capability.MaxOutputTokens = 128
	catalog, err := llmgateway.NewCatalog("creative-agent-live-128", "USD", []llmgateway.ModelConfig{model})
	if err != nil {
		t.Fatal(err)
	}
	gateway, err := llmgateway.Build(llmgateway.Options{Catalog: catalog, MonthlyLimitMicros: 50_000, MonthlyTokenLimit: 10_000, Timeout: 60 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	f.service, err = NewService(gateway, catalog, f.skills)
	if err != nil {
		t.Fatal(err)
	}
	f.service.limits.InputTextBytes = 4096
	f.service.limits.ModelTurns = 1
	f.queue()
	conversation := f.conversation(f.alice, f.canvasID)
	consent := f.consent(f.alice, conversation.ID)
	run, err := f.startRun(f.alice, conversation.ID, consent.ID, InstructionSegment{Type: "text", Text: "这是连接验收。请只回复：创作助手已就绪。不要调用任何工具。"})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	got, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
	if err != nil || got.State != RunSucceeded || got.SettlementState != "settled" {
		t.Fatalf("live run state=%s settlement=%s error=%s err=%v", got.State, got.SettlementState, got.ErrorCode, err)
	}
	if n := f.count("creative_agent_messages", "run_id=$1 AND role='assistant' AND status='complete'", run.ID); n != 1 {
		t.Fatalf("complete replies=%d", n)
	}
	if err = f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	if n := f.count("llm_requests", "caller_group_id=$1", run.ID); n != 1 {
		t.Fatalf("duplicate requests=%d", n)
	}
	assertRunReleased(t, f, run.ID)
	t.Log("real Gateway → Eino → persisted assistant reply succeeded; redelivery did not create a second request")
}
