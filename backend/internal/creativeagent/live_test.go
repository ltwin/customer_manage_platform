package creativeagent

import (
	"encoding/json"
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

// The real model must accept the tool schemas and use them in order from
// plain words. Generation itself is stubbed: this spends chat tokens only.
func TestLiveAssistantBuildsAndGenerates(t *testing.T) {
	if os.Getenv("CREATIVE_AGENT_LIVE") != "1" || strings.TrimSpace(os.Getenv("AICODEWITH_API_KEY")) == "" {
		t.Skip("explicit live acceptance not enabled")
	}
	const model = "gpt-6.1-sol"
	f := setup(t)
	catalog, err := llmgateway.DefaultCatalog(nil)
	if err != nil {
		t.Fatal(err)
	}
	gateway, err := llmgateway.Build(llmgateway.Options{Catalog: catalog, MonthlyLimitMicros: 20_000_000, MonthlyTokenLimit: 5_000_000, Timeout: 15 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if f.service, err = NewService(gateway, catalog, f.skills); err != nil {
		t.Fatal(err)
	}
	generator := &stubGenerator{}
	f.service.SetGenerator(generator)
	f.queue()
	conversation := f.conversation(f.alice, f.canvasID)
	consent := contextConsent(t, f, conversation.ID)
	receipt, err := f.service.CreateRun(t.Context(), f.alice, command(t, map[string]any{
		"conversation_id": conversation.ID, "model_key": model, "egress_consent_id": consent.ID,
		"instruction": map[string]any{"schema_version": instructionSchemaVersion, "instruction_segments": []InstructionSegment{
			{Type: "text", Text: "直接动手，不用再问我：新建一个文字节点写上「金色逆光、胶片颗粒」作为参考，再新建一个图片节点连上它，用 GPT Image 2 生成海边日出的逆光人像，16:9，高画质，4K，出 2 张。"}}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	var run Run
	if err = json.Unmarshal(receipt.Outcome.Response, &run); err != nil {
		t.Fatal(err)
	}
	if err = f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	got, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
	if err != nil || got.State != RunSucceeded {
		t.Fatalf("live run state=%s error=%s err=%v", got.State, got.ErrorCode, err)
	}
	var params string
	if err = f.db.QueryRow(`SELECT parameters::text FROM creative_node_prompt_drafts WHERE account_id='agent-a' AND model_key='gpt-image-2'`).Scan(&params); err != nil {
		t.Fatal(err)
	}
	t.Logf("parameters %s, generate calls %+v", params, generator.calls)
	if len(generator.calls) != 1 || f.count("creative_nodes", "canvas_id=$1", f.canvasID) != 2 || f.count("creative_edges", "canvas_id=$1", f.canvasID) != 1 {
		t.Fatal("the canvas was not built and generated as asked")
	}
	for _, want := range []string{`"16:9"`, `"high"`, `"4K"`, `2`} {
		if !strings.Contains(params, want) {
			t.Fatalf("parameters %s missing %s", params, want)
		}
	}
}
