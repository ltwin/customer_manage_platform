package creativeagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativeskill"
	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func TestCurrentBudgetIncludesSkillMiddlewareAndCurrentTools(t *testing.T) {
	f := newCompactionFixture(t, testSummary)
	req := skillRequest("platform", "budget-check", "当前预算指引")
	req.Activate = true
	req.Manifest.ToolAllowlist = []string{"load_skill@1", "read_skill_resource@1", "read_run_result@1"}
	version := f.runImport(f.platform, req)
	f.consent = libraryConsent(t, f.fixture, f.conversation.ID)
	run := f.say(InstructionSegment{Type: "text", Text: "整理"},
		InstructionSegment{Type: "skill_ref", SkillID: version.SkillID, SkillVersionID: version.ID})
	if run.State != RunSucceeded {
		t.Fatalf("runtime: %s / %s", run.State, run.ErrorCode)
	}
	var raw []byte
	if err := f.db.QueryRow("SELECT skill_snapshot FROM creative_agent_runs WHERE id=$1", run.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var skill creativeskill.Snapshot
	if err := json.Unmarshal(raw, &skill); err != nil {
		t.Fatal(err)
	}
	model, err := f.service.models.Model(testModelKey)
	if err != nil {
		t.Fatal(err)
	}
	var measured contextMeasure
	if err := f.alice.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		var err error
		measured, err = f.service.currentMeasureInTx(t.Context(), tx, f.conversation.ID, f.consent.ID, model, &skill)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	f.vendor.mu.Lock()
	request := f.vendor.sent[len(f.vendor.sent)-1]
	f.vendor.mu.Unlock()
	actual := toolDefinitionBytes(request.Tools)
	for _, message := range request.Messages {
		if message.Role == llmgateway.RoleSystem {
			bytes, _ := messageBytes(message)
			actual += bytes
		}
	}
	if estimated := measured.Fixed + len(skill.Instructions); estimated != actual {
		t.Fatalf("budget fixed inputs=%d, actual system and tools=%d", estimated, actual)
	}
}

func TestHistoryGuardUsesCurrentToolsAfterConsentUpgrade(t *testing.T) {
	f := newCompactionFixture(t, testSummary)
	useModel(t, f.fixture, func(m *llmgateway.ModelConfig) { m.ContextWindowTokens = 16000 })
	f.queue()
	f.service.limits.AutoCompactPercent = 0
	f.vendor.script = func(_ context.Context, request llmgateway.ProviderRequest, _ int) (llmgateway.Result, error) {
		result := scriptedResult(request)
		// A valid measured ratio makes the byte ceiling the tighter bound.
		input := llmgateway.EstimateInputTokens(request.Chat) / 5
		result.Usage.InputTotalTokens = &input
		return result, nil
	}
	if run := f.text("measure the initial tool set"); run.State != RunSucceeded {
		t.Fatalf("initial measurement: %s / %s", run.State, run.ErrorCode)
	}
	model, err := f.service.models.Model(testModelKey)
	if err != nil {
		t.Fatal(err)
	}
	var budget historyBudget
	if err := f.alice.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		measure, err := conversationMeasureInTx(t.Context(), tx, f.conversation.ID)
		budget = f.service.historyBudget(model, measure)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if run := f.text(strings.Repeat("x", (budget.Bytes-512)/2)); run.State != RunSucceeded {
			t.Fatalf("near-ceiling history: %s / %s", run.State, run.ErrorCode)
		}
	}
	f.vendor.mu.Lock()
	oldTools := len(f.vendor.sent[len(f.vendor.sent)-1].Tools)
	f.vendor.mu.Unlock()
	f.consent = libraryConsent(t, f.fixture, f.conversation.ID)
	if run := f.text("continue with library access"); run.State != RunSucceeded {
		t.Fatalf("expanded tool set: %s / %s", run.State, run.ErrorCode)
	}
	f.vendor.mu.Lock()
	sent := f.vendor.sent[len(f.vendor.sent)-1]
	f.vendor.mu.Unlock()
	if len(sent.Tools) <= oldTools {
		t.Fatal("consent upgrade did not expand the tool set")
	}
	if bytes := llmgateway.EstimateInputTokens(sent); bytes > int64(requestByteCeiling(f.service.limits, model)) {
		t.Fatalf("actual request exceeded the byte ceiling: %d", bytes)
	}
}

func TestCompactionSmallWindowCanContinue(t *testing.T) {
	f := newCompactionFixture(t, testSummary+"\n"+strings.Repeat("摘要", 4000))
	// The window leaves the same room as before the scene tools (about 7 KB of
	// definitions in every request), the repaint guidance for export images
	// (about 1 KB) and the materials, lighting and lit look (about 3 KB) were added.
	useModel(t, f.fixture, func(m *llmgateway.ModelConfig) { m.ContextWindowTokens = 22000 })
	f.queue()
	f.service.limits.AutoCompactPercent = 0
	for i := 0; i < 4; i++ {
		if run := f.text(strings.Repeat("x", 20000)); run.State != RunSucceeded {
			t.Fatalf("setup turn %d: %s / %s", i, run.State, run.ErrorCode)
		}
	}
	compact := f.say(InstructionSegment{Type: "command", Command: compactCommand})
	if compact.State != RunSucceeded {
		t.Fatalf("compaction failed: %s / %s", compact.State, compact.ErrorCode)
	}
	next := f.text("继续")
	if next.State != RunSucceeded {
		t.Fatalf("valid 16000-token window: compaction succeeded, next short turn failed: %s / %s", next.State, next.ErrorCode)
	}
}

func TestCompactionOriginalsLeaveRoomForRecentTurns(t *testing.T) {
	f := newCompactionFixture(t, testSummary+"\n"+strings.Repeat("摘要", 4000))
	// The window leaves the same room as before the scene tools (about 7 KB of
	// definitions in every request), the repaint guidance for export images
	// (about 1 KB) and the materials, lighting and lit look (about 3 KB) were added.
	useModel(t, f.fixture, func(m *llmgateway.ModelConfig) { m.ContextWindowTokens = 22000 })
	f.queue()
	f.service.limits.AutoCompactPercent = 0
	for _, text := range []string{strings.Repeat("x", 20000), strings.Repeat("y", 20000), "最近方案甲", "最近方案乙"} {
		if run := f.text(text); run.State != RunSucceeded {
			t.Fatalf("setup: %s / %s", run.State, run.ErrorCode)
		}
	}
	if run := f.say(InstructionSegment{Type: "command", Command: compactCommand}); run.State != RunSucceeded {
		t.Fatalf("compact: %s / %s", run.State, run.ErrorCode)
	}
	run := f.text("继续刚才的方案")
	if run.State != RunSucceeded {
		t.Fatalf("continue: %s / %s", run.State, run.ErrorCode)
	}
	if n := f.count("creative_run_inputs", "run_id=$1 AND input_role='history'", run.ID); n != 4 {
		t.Fatalf("recent turns lost before dispatch: frozen history messages=%d, want 4", n)
	}
	sent := f.vendor.sentText(f.vendor.calls() - 1)
	for _, want := range []string{"最近方案甲", "第3次回复", "最近方案乙", "第4次回复"} {
		if !strings.Contains(sent, want) {
			t.Errorf("recent context missing %q from actual provider request", want)
		}
	}
}

func TestCompactionOriginalsYieldToExpandedTools(t *testing.T) {
	f := newCompactionFixture(t, testSummary+"\n"+strings.Repeat("摘要", 4000))
	// Widened by the materials, lighting and lit look's tool text (about 3 KB).
	useModel(t, f.fixture, func(m *llmgateway.ModelConfig) { m.ContextWindowTokens = 19000 })
	f.queue()
	f.service.limits.AutoCompactPercent = 0
	script := f.vendor.script
	f.vendor.script = func(ctx context.Context, request llmgateway.ProviderRequest, n int) (llmgateway.Result, error) {
		result, err := script(ctx, request, n)
		input := llmgateway.EstimateInputTokens(request.Chat) / 5
		result.Usage.InputTotalTokens = &input
		return result, err
	}
	for _, text := range []string{strings.Repeat("x", 20000), strings.Repeat("y", 20000), "最近方案甲", "最近方案乙"} {
		if run := f.text(text); run.State != RunSucceeded {
			t.Fatalf("setup: %s / %s", run.State, run.ErrorCode)
		}
	}
	if run := f.say(InstructionSegment{Type: "command", Command: compactCommand}); run.State != RunSucceeded {
		t.Fatalf("compact: %s / %s", run.State, run.ErrorCode)
	}
	f.consent = libraryConsent(t, f.fixture, f.conversation.ID)
	if run := f.text("继续刚才的方案"); run.State != RunSucceeded {
		t.Fatalf("continue with expanded tools: %s / %s", run.State, run.ErrorCode)
	}
	sent := f.vendor.sentText(f.vendor.calls() - 1)
	for _, want := range []string{testSummary, "最近方案甲", "第3次回复", "最近方案乙", "第4次回复"} {
		if !strings.Contains(sent, want) {
			t.Errorf("actual request lost %q", want)
		}
	}
	f.vendor.mu.Lock()
	request := f.vendor.sent[len(f.vendor.sent)-1]
	f.vendor.mu.Unlock()
	model, err := f.service.models.Model(testModelKey)
	if err != nil {
		t.Fatal(err)
	}
	if bytes := llmgateway.EstimateInputTokens(request); bytes > int64(requestByteCeiling(f.service.limits, model)) {
		t.Fatalf("actual request exceeded the byte ceiling: %d", bytes)
	}
}

func TestCompactionSummaryFallsBackWhenCurrentToolsOutgrowWindow(t *testing.T) {
	f := newCompactionFixture(t, testSummary)
	// The window leaves the same room as before the scene tools (about 7 KB of
	// definitions in every request) and the materials, lighting and lit look
	// (about 3 KB) were added.
	useModel(t, f.fixture, func(m *llmgateway.ModelConfig) { m.ContextWindowTokens = 13000 })
	f.queue()
	f.service.limits.AutoCompactPercent = 0
	summary := testSummary
	f.vendor.script = func(_ context.Context, request llmgateway.ProviderRequest, n int) (llmgateway.Result, error) {
		result := scriptedResult(request)
		result.Text = fmt.Sprintf("第%d次回复", n)
		if isCompactionRequest(request.Chat) {
			result.Text = summary
		}
		input := llmgateway.EstimateInputTokens(request.Chat) / 5
		result.Usage.InputTotalTokens = &input
		return result, nil
	}
	for _, text := range []string{"旧方向", "最近方案甲", "最近方案乙"} {
		if run := f.text(text); run.State != RunSucceeded {
			t.Fatalf("setup: %s / %s", run.State, run.ErrorCode)
		}
	}
	model, err := f.service.models.Model(testModelKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.alice.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		measure, err := conversationMeasureInTx(t.Context(), tx, f.conversation.ID)
		if err != nil {
			return err
		}
		room := f.service.historyBudget(model, measure).availableBytes() - len(compactionPrefix) - perMessageOverhead - 600
		summary += strings.Repeat("x", room-len(summary))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if run := f.say(InstructionSegment{Type: "command", Command: compactCommand}); run.State != RunSucceeded {
		t.Fatalf("compact: %s / %s", run.State, run.ErrorCode)
	}
	f.consent = libraryConsent(t, f.fixture, f.conversation.ID)
	if run := f.text("继续"); run.State != RunSucceeded {
		t.Fatalf("summary no longer fits current tools: %s / %s", run.State, run.ErrorCode)
	}
	sent := f.vendor.sentText(f.vendor.calls() - 1)
	if strings.Contains(sent, compactionPrefix) {
		t.Fatal("oversized summary was still sent")
	}
	for _, want := range []string{"最近方案甲", "第2次回复", "最近方案乙", "第3次回复"} {
		if !strings.Contains(sent, want) {
			t.Errorf("raw history fallback lost %q", want)
		}
	}
	f.vendor.mu.Lock()
	request := f.vendor.sent[len(f.vendor.sent)-1]
	f.vendor.mu.Unlock()
	if bytes := llmgateway.EstimateInputTokens(request); bytes > int64(requestByteCeiling(f.service.limits, model)) {
		t.Fatalf("actual request exceeded byte ceiling: %d", bytes)
	}
}

func TestCompactionReplacementAtSameCoverageUsesNewSummary(t *testing.T) {
	f := newCompactionFixture(t, testSummary+"\nold-large-summary\n"+strings.Repeat("摘要", 4000))
	f.threeTurns()
	first := f.say(InstructionSegment{Type: "command", Command: compactCommand})
	if first.State != RunSucceeded {
		t.Fatalf("first compact: %s / %s", first.State, first.ErrorCode)
	}
	// The window leaves the same room as before the scene tools (about 7 KB of
	// definitions in every request), the repaint guidance for export images
	// (about 1 KB) and the materials, lighting and lit look (about 3 KB) were added.
	useModel(t, f.fixture, func(m *llmgateway.ModelConfig) { m.ContextWindowTokens = 14000 })
	f.queue()
	f.service.limits.AutoCompactPercent = 0
	f.vendor.script = compactionScript(testSummary + "\nreplacement-small-summary")
	second := f.say(InstructionSegment{Type: "command", Command: compactCommand})
	if second.State != RunSucceeded {
		t.Fatalf("replacement compact: %s / %s", second.State, second.ErrorCode)
	}
	var covered, replacements int
	if err := f.db.QueryRow("SELECT count(DISTINCT covers_through_ordinal),count(*) FROM creative_agent_compactions WHERE conversation_id=$1", f.conversation.ID).Scan(&covered, &replacements); err != nil {
		t.Fatal(err)
	}
	if covered != 1 || replacements != 2 {
		t.Fatalf("expected same coverage replacement: ranges=%d summaries=%d", covered, replacements)
	}
	if run := f.text("继续"); run.State != RunSucceeded {
		t.Fatalf("continue: %s / %s", run.State, run.ErrorCode)
	}
	sent := f.vendor.sentText(f.vendor.calls() - 1)
	if !strings.Contains(sent, "replacement-small-summary") || strings.Contains(sent, "old-large-summary") {
		t.Fatal("next request did not use the replacement summary")
	}
}

// Every boundary before the framework starts must recover from the compaction
// journal, without buying an already completed provider call a second time.
func TestCompactionRecoversBeforeFrameworkStarts(t *testing.T) {
	for _, tc := range []struct {
		name      string
		phase     string
		manual    bool
		summary   string
		wantState string
		wantCalls int
	}{
		{name: "prepared", phase: "prepared", summary: testSummary, wantState: RunSucceeded, wantCalls: 5},
		{name: "provider_completed", phase: "provider_completed", summary: testSummary, wantState: RunSucceeded, wantCalls: 5},
		{name: "consumed", phase: "consumed", summary: testSummary, wantState: RunSucceeded, wantCalls: 5},
		{name: "manual_completed", phase: "consumed", manual: true, summary: testSummary, wantState: RunSucceeded, wantCalls: 4},
		{name: "manual_failed", phase: "consumed", manual: true, summary: "invalid summary", wantState: RunFailed, wantCalls: 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newCompactionFixture(t, tc.summary)
			f.threeTurns()
			f.service.limits.AutoCompactPercent = 1
			instruction := InstructionSegment{Type: "text", Text: "continue after compact"}
			if tc.manual {
				instruction = InstructionSegment{Type: "command", Command: compactCommand}
			}
			run, err := f.startRun(f.alice, f.conversation.ID, f.consent.ID, instruction)
			if err != nil {
				t.Fatal(err)
			}
			held, err := f.service.claimRun(t.Context(), f.alice, run.ID)
			if err != nil {
				t.Fatal(err)
			}
			planned, err := readCompactionRun(t.Context(), f.alice, run.ID)
			if err != nil {
				t.Fatal(err)
			}
			if tc.phase == "consumed" {
				_, err = f.service.compactInRun(t.Context(), f.alice, held, planned)
				if tc.wantState == RunFailed {
					if !errors.Is(err, errCompactionFailed) {
						t.Fatalf("invalid summary: %v", err)
					}
				} else if err != nil {
					t.Fatal(err)
				}
			} else {
				var chat llmgateway.ChatRequest
				var ordinal int64
				err = f.alice.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
					var err error
					chat, err = f.service.compactionRequestInTx(t.Context(), tx, held.ConversationID, planned.Plan, requestByteCeiling(held.Limits, held.Model))
					if err != nil {
						return err
					}
					chat.ContractVersion, chat.ModelKey = llmgateway.ContractVersion, held.Model.ModelKey
					chat.OutputLimit = min(compactionOutputTokens, held.Model.Capability.MaxOutputTokens)
					_, ordinal, err = f.service.prepareCompactionStepInTx(t.Context(), tx, held, planned.Plan, chat)
					return err
				})
				if err != nil {
					t.Fatal(err)
				}
				if tc.phase == "provider_completed" {
					crash := errors.New("worker stopped before consuming the paid result")
					_, err = f.service.gateway.Call(t.Context(), llmgateway.CallSession{
						Scope: f.alice, Limits: func(tx store.TxAccountScope) llmgateway.LimitView { return tx.LLMLimitView() },
						CallerService: callerService, CallerGroupID: run.ID, GroupDeadline: held.Deadline,
						ModelKey: held.Model.ModelKey, OutputLimit: chat.OutputLimit, Deadline: held.Deadline,
						Admit: func(tx store.TxAccountScope) error { return f.service.admitRunInTx(t.Context(), tx, held) },
					}, llmgateway.CallInput{BindingKey: turnBindingKey(run.ID, ordinal), Chat: chat,
						Consume: func(store.TxAccountScope, llmgateway.Result) error { return crash },
					})
					if !errors.Is(err, crash) {
						t.Fatalf("provider completion boundary: %v", err)
					}
				}
			}
			if _, err = f.db.Exec("UPDATE creative_agent_runs SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1", run.ID); err != nil {
				t.Fatal(err)
			}
			if _, err = f.service.SweepRuns(t.Context(), f.alice, 10); err != nil {
				t.Fatal(err)
			}
			queued, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
			if err != nil {
				t.Fatal(err)
			}
			if queued.State != RunQueued {
				t.Fatalf("compaction did not resume: %s / %s", queued.State, queued.ErrorCode)
			}
			if err = f.work(f.alice, run.ID); err != nil {
				t.Fatal(err)
			}
			got, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.State != tc.wantState || f.vendor.calls() != tc.wantCalls {
				t.Fatalf("resumed state=%s code=%s calls=%d", got.State, got.ErrorCode, f.vendor.calls())
			}
			if tc.wantState == RunFailed && got.ErrorCode != "creative_compaction_failed" {
				t.Fatalf("failure lost: %s", got.ErrorCode)
			}
			wantCheckpoints := 1
			if tc.wantState == RunFailed {
				wantCheckpoints = 0
			}
			if n := f.count("creative_agent_compactions", "run_id=$1", run.ID); n != wantCheckpoints {
				t.Fatalf("checkpoints=%d, want %d", n, wantCheckpoints)
			}
			assertRunReleased(t, f.fixture, run.ID)
		})
	}
}

func TestCompactionPrecedesHistoryMessageLimit(t *testing.T) {
	f := newCompactionFixture(t, testSummary)
	f.queue()
	f.service.limits.AutoCompactPercent = 80
	f.text("first-original-constraint")
	for i := 1; i < 122; i++ {
		if run := f.text("继续"); run.State != RunSucceeded {
			t.Fatalf("turn %d failed: %s / %s", i, run.State, run.ErrorCode)
		}
	}
	if f.count("creative_agent_compactions", "true") == 0 && !strings.Contains(f.vendor.sentText(f.vendor.calls()-1), "first-original-constraint") {
		t.Fatalf("122 short turns: earliest constraint was dropped with no compaction; provider calls=%d", f.vendor.calls())
	}
}

func TestCompactionTranscriptExcludesSkillBodies(t *testing.T) {
	for _, name := range []string{loadSkillTool, readSkillTool} {
		text := transcriptLine("tool", Body{ToolName: name, Blocks: []Block{{Type: "text", Text: "skill-instructions-must-not-be-summarized"}}})
		if strings.Contains(text, "skill-instructions-must-not-be-summarized") {
			t.Errorf("%s body entered compaction transcript: %s", name, text)
		}
	}
}

func TestCompactionRunUsesFrozenWindow(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		name := "explicit"
		if legacy {
			name = "legacy_capability_remainder"
		}
		t.Run(name, func(t *testing.T) {
			f := newCompactionFixture(t, testSummary)
			run, err := f.startRun(f.alice, f.conversation.ID, f.consent.ID, InstructionSegment{Type: "text", Text: "hello"})
			if err != nil {
				t.Fatal(err)
			}
			model, err := f.service.models.Model(testModelKey)
			if err != nil {
				t.Fatal(err)
			}
			frozenWindow := model.InputWindowTokens()
			if legacy {
				frozenWindow = model.Capability.ContextTokens - model.Capability.MaxOutputTokens
				if _, err = f.db.Exec("UPDATE creative_agent_runs SET model_snapshot=model_snapshot-'context_window_tokens' WHERE id=$1", run.ID); err != nil {
					t.Fatal(err)
				}
			}
			model.ContextWindowTokens = 16000
			changed, err := llmgateway.NewCatalog("changed-window", model.Price.Currency, []llmgateway.ModelConfig{model})
			if err != nil {
				t.Fatal(err)
			}
			f.service.models = changed
			held, err := f.service.claimRun(t.Context(), f.alice, run.ID)
			if err != nil {
				t.Fatal(err)
			}
			if held.Model.InputWindowTokens() != frozenWindow {
				t.Fatalf("queued run switched from frozen %d to live %d", frozenWindow, held.Model.InputWindowTokens())
			}
		})
	}
}

// A summary which alone exceeds the history budget must not become a durable
// checkpoint that prevents later turns from using this otherwise usable model.
func TestCompactionRejectsSummaryBeyondWindow(t *testing.T) {
	for _, manual := range []bool{false, true} {
		name := "auto"
		if manual {
			name = "manual"
		}
		t.Run(name, func(t *testing.T) {
			f := newCompactionFixture(t, testSummary+"\n"+strings.Repeat("摘要", 4000))
			// Room for the tool definitions (W2's physical sky, then W3's night sky and automatic exposure made edit_scene's longer), not for the summary.
			useModel(t, f.fixture, func(m *llmgateway.ModelConfig) { m.ContextWindowTokens = 10000 })
			f.queue()
			f.service.limits.AutoCompactPercent = 0
			f.threeTurns()
			var run Run
			if manual {
				run = f.say(InstructionSegment{Type: "command", Command: compactCommand})
				if run.State != RunFailed || run.ErrorCode != "creative_compaction_failed" {
					t.Fatalf("oversized summary accepted: %+v", run)
				}
			} else {
				f.service.limits.AutoCompactPercent = 1
				run = f.text("continue")
				if run.State != RunSucceeded {
					t.Fatalf("fallback failed: %+v", run)
				}
			}
			if n := f.count("creative_agent_compactions", "true"); n != 0 {
				t.Fatalf("saved %d oversized checkpoints", n)
			}
			f.service.limits.AutoCompactPercent = 0
			if next := f.text("继续"); next.State != RunSucceeded {
				t.Fatalf("conversation poisoned: %+v", next)
			}
		})
	}
}

func TestCompactionOriginalsBudgetIncludesIndentation(t *testing.T) {
	f := newCompactionFixture(t, testSummary)
	for i := 0; i < 4; i++ {
		f.text(strings.Repeat("文\n", 4000))
	}
	f.say(InstructionSegment{Type: "command", Command: compactCommand})
	err := f.alice.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		budget := historyBudget{Tokens: 1000, Bytes: 3000, Ratio: defaultTokensPerByte}
		p, err := f.service.projectHistoryInTx(t.Context(), tx, projectionQuery{ConversationID: f.conversation.ID, Budget: budget})
		if err != nil {
			return err
		}
		if p.Checkpoint == nil || len(p.Originals) == 0 || !p.OriginalsCut {
			t.Fatalf("missing bounded originals: %+v", p)
		}
		if p.fixedBytes() > budget.Bytes || budget.Ratio.tokens(p.fixedBytes()) > budget.Tokens {
			t.Fatalf("rendered %d bytes exceed budget %+v", p.fixedBytes(), budget)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
