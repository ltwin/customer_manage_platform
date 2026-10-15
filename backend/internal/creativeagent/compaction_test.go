package creativeagent

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
)

const testSummary = "## 创作目标与当前方向\n秋日人像，整体偏暖\n## 已定结论\n暖色调\n## 明确否决的方向\n冷调，理由：不够温暖\n" +
	"## 创作者的偏好与要求\n喜欢逆光\n## 关键引用\n无\n## Skill 使用记录\n无\n## 进行中与未完成\n无\n## 下一步\n出三版"

func isCompactionRequest(chat llmgateway.ChatRequest) bool {
	return len(chat.Messages) > 0 && chat.Messages[0].Role == llmgateway.RoleSystem &&
		len(chat.Messages[0].Blocks) > 0 && strings.Contains(chat.Messages[0].Blocks[0].Text, "上下文压缩")
}

// sentText flattens one request the vendor was actually shown.
func (v *stubVendor) sentText(i int) string {
	v.mu.Lock()
	defer v.mu.Unlock()
	var b strings.Builder
	for _, m := range v.sent[i].Messages {
		for _, block := range m.Blocks {
			b.WriteString(block.Text)
			b.WriteString("\n")
		}
	}
	return b.String()
}

// compactionScript answers a compaction request with summary and every other
// request with a numbered reply, so a test can tell which turn's words a later
// request still carries.
func compactionScript(summary string) func(context.Context, llmgateway.ProviderRequest, int) (llmgateway.Result, error) {
	return func(_ context.Context, r llmgateway.ProviderRequest, n int) (llmgateway.Result, error) {
		result := scriptedResult(r)
		if isCompactionRequest(r.Chat) {
			result.Text = summary
		} else {
			result.Text = fmt.Sprintf("第%d次回复", n)
		}
		return result, nil
	}
}

type compactionFixture struct {
	*fixture
	conversation Conversation
	consent      Consent
}

func newCompactionFixture(t *testing.T, summary string) *compactionFixture {
	f := setup(t)
	f.queue()
	f.service.limits.AutoCompactPercent = 0
	c := f.conversation(f.alice, f.canvasID)
	consent := f.consent(f.alice, c.ID)
	f.vendor.script = compactionScript(summary)
	return &compactionFixture{fixture: f, conversation: c, consent: consent}
}

func (f *compactionFixture) say(segments ...InstructionSegment) Run {
	f.t.Helper()
	run, err := f.startRun(f.alice, f.conversation.ID, f.consent.ID, segments...)
	if err != nil {
		f.t.Fatal(err)
	}
	if err := f.work(f.alice, run.ID); err != nil {
		f.t.Fatal(err)
	}
	got, err := f.service.ReadRun(f.t.Context(), f.alice, run.ID)
	if err != nil {
		f.t.Fatal(err)
	}
	return got
}

func (f *compactionFixture) text(text string) Run {
	return f.say(InstructionSegment{Type: "text", Text: text})
}

func (f *compactionFixture) threeTurns() {
	f.text("第一轮：秋日人像，暖色调")
	f.text("第二轮：加入逆光")
	f.text("第三轮：换成胶片质感")
}

// A run whose request would reach the compaction line compacts first: the
// turns before the newest two become a summary, the creator's own words from
// them stay verbatim, and the conversation itself is never rewritten.
func TestALongConversationCompactsBeforeTheTurn(t *testing.T) {
	f := newCompactionFixture(t, testSummary)
	f.threeTurns()
	messages := f.count("creative_agent_messages", "true")
	f.service.limits.AutoCompactPercent = 1
	run := f.text("第四轮：出三版")
	if run.State != RunSucceeded || run.Kind != runKindChat || f.vendor.calls() != 5 {
		t.Fatalf("run=%+v calls=%d", run, f.vendor.calls())
	}
	if n := f.count("creative_agent_compactions", "conversation_id=$1 AND trigger='auto' AND covers_through_ordinal=2 AND invalidated_at IS NULL", f.conversation.ID); n != 1 {
		t.Fatalf("checkpoints=%d", n)
	}
	summarised := f.vendor.sentText(3)
	if !strings.Contains(summarised, "第一轮：秋日人像") || !strings.Contains(summarised, "第1次回复") || strings.Contains(summarised, "第二轮") {
		t.Fatalf("compaction covered the wrong range:\n%s", summarised)
	}
	sent := f.vendor.sentText(4)
	for _, want := range []string{compactionPrefix, "秋日人像，整体偏暖", "压缩前创作者说过的话", "第一轮：秋日人像，暖色调",
		"第二轮：加入逆光", "第2次回复", "第三轮：换成胶片质感", "第3次回复", "第四轮：出三版"} {
		if !strings.Contains(sent, want) {
			t.Fatalf("turn request is missing %q:\n%s", want, sent)
		}
	}
	if strings.Contains(sent, "第1次回复") {
		t.Fatalf("a summarised answer was still sent:\n%s", sent)
	}
	// Messages only grow: the new turn adds its question and answer.
	if n := f.count("creative_agent_messages", "true"); n != messages+2 {
		t.Fatalf("messages %d, want %d", n, messages+2)
	}
	if n := f.count("creative_agent_blocks", "run_id=$1 AND type='compaction' AND state='complete'", run.ID); n != 1 {
		t.Fatalf("compaction blocks=%d", n)
	}
	if n := f.count("creative_agent_steps", "run_id=$1 AND kind='compaction' AND state='succeeded' AND llm_request_id IS NOT NULL", run.ID); n != 1 {
		t.Fatalf("compaction steps=%d", n)
	}
	assertRunReleased(t, f.fixture, run.ID)
}

// A summary that lost its shape is not carried on: the turn goes ahead with the
// old truncated history, and says so.
func TestAnUnusableSummaryFallsBackAndTheTurnGoesOn(t *testing.T) {
	f := newCompactionFixture(t, "随便写写")
	f.threeTurns()
	f.service.limits.AutoCompactPercent = 1
	run := f.text("第四轮：出三版")
	if run.State != RunSucceeded || f.vendor.calls() != 5 {
		t.Fatalf("run=%+v calls=%d", run, f.vendor.calls())
	}
	if n := f.count("creative_agent_compactions", "true"); n != 0 {
		t.Fatalf("an unusable summary became %d checkpoints", n)
	}
	if n := f.count("creative_agent_runs", "id=$1 AND compaction_state='fallback'", run.ID); n != 1 {
		t.Fatal("compaction was not settled as a fallback")
	}
	if n := f.count("creative_agent_blocks", "run_id=$1 AND type='compaction' AND state='fallback'", run.ID); n != 1 {
		t.Fatalf("fallback blocks=%d", n)
	}
	sent := f.vendor.sentText(4)
	if strings.Contains(sent, compactionPrefix) || !strings.Contains(sent, "第1次回复") {
		t.Fatalf("the fallback did not keep the old history:\n%s", sent)
	}
}

// The headings are asked for, not enforced. A model that renamed them or wrote
// them in bold still summarised the conversation (gpt-6-astra, 2026-10-15),
// and refusing that would throw away a paid, usable handoff.
func TestASummaryInTheModelsOwnHeadingsIsKept(t *testing.T) {
	summary := "**项目目标与创作方向**\n秋日人像，整体偏暖，逆光为主\n\n**已经确定的内容**\n暖色调；胶片质感\n\n**接下来要做的**\n按已定方向出三版"
	f := newCompactionFixture(t, summary)
	f.threeTurns()
	run := f.say(InstructionSegment{Type: "command", Command: compactCommand})
	if run.State != RunSucceeded {
		t.Fatalf("run=%+v", run)
	}
	if n := f.count("creative_agent_compactions", "summary=$1", summary); n != 1 {
		t.Fatalf("checkpoints with the model's summary=%d", n)
	}
}

// What makes a summary unusable is substance, not shape: nothing, a refusal,
// more than the stored bound, or bytes that are not text.
func TestSummaryValidityIsAboutSubstanceNotShape(t *testing.T) {
	const limit = 32 << 10
	cases := []struct {
		name string
		text string
		want bool
	}{
		{"the asked-for sections", testSummary, true},
		{"its own headings", "**目标**\n秋日人像，整体偏暖，逆光为主，胶片质感\n**下一步**\n按已定方向出三版，每版换一种构图", true},
		{"empty", "", false},
		{"a refusal", "抱歉，我无法完成这个请求。", false},
		{"over the bound", strings.Repeat("长", limit), false},
		{"not text", "\xff" + strings.Repeat("a", 200), false},
	}
	for _, c := range cases {
		if got := validSummary(c.text, limit); got != c.want {
			t.Errorf("%s: validSummary=%v, want %v", c.name, got, c.want)
		}
	}
}

// A /compact whose summary is refused says so on its divider, rather than
// the generic failure that hides why nothing changed.
func TestARefusedSummaryTellsTheCreatorWhy(t *testing.T) {
	f := newCompactionFixture(t, "随便写写")
	f.threeTurns()
	run := f.say(InstructionSegment{Type: "command", Command: compactCommand})
	if run.State != RunFailed || run.ErrorCode != "creative_compaction_failed" {
		t.Fatalf("run=%+v", run)
	}
	if n := f.count("creative_agent_blocks", "run_id=$1 AND type='compaction' AND state='failed' AND payload->>'message' LIKE '%摘要%'", run.ID); n != 1 {
		t.Fatalf("divider did not explain the refused summary: %d", n)
	}
}

// /compact summarises on request, keeps what the creator asked to keep, and
// answers nothing; the next turn then carries the summary.
func TestTheCompactCommandSummarisesOnRequest(t *testing.T) {
	f := newCompactionFixture(t, testSummary)
	f.threeTurns()
	run := f.say(InstructionSegment{Type: "command", Command: compactCommand, Text: "保留配色讨论"})
	if run.State != RunSucceeded || run.Kind != runKindCompact || f.vendor.calls() != 4 {
		t.Fatalf("run=%+v calls=%d", run, f.vendor.calls())
	}
	if n := f.count("creative_agent_compactions", "trigger='manual' AND focus='保留配色讨论'"); n != 1 {
		t.Fatalf("manual checkpoints=%d", n)
	}
	if !strings.Contains(f.vendor.sentText(3), "保留配色讨论") {
		t.Fatal("the focus did not reach the compaction request")
	}
	if n := f.count("creative_agent_messages", "role='assistant'"); n != 3 {
		t.Fatalf("a compact run answered: %d assistant messages", n)
	}
	next := f.text("第五轮：继续")
	sent := f.vendor.sentText(4)
	if next.State != RunSucceeded || !strings.Contains(sent, compactionPrefix) || strings.Contains(sent, "保留配色讨论") {
		t.Fatalf("the next turn did not build on the checkpoint, or carried the command:\n%s", sent)
	}
	// Withdrawing the authorisation retires the summary written under it.
	if _, err := f.service.RevokeConsent(t.Context(), f.alice, command(t, map[string]any{"consent_id": f.consent.ID, "expected_revision": f.consent.Revision})); err != nil {
		t.Fatal(err)
	}
	if n := f.count("creative_agent_compactions", "invalidated_reason='consent_revoked'"); n != 1 {
		t.Fatalf("invalidated=%d", n)
	}
}

// With nothing older than the turns compaction always keeps, /compact says so
// and calls no model.
func TestTheCompactCommandSkipsAShortConversation(t *testing.T) {
	f := newCompactionFixture(t, testSummary)
	f.text("第一轮：秋日人像")
	run := f.say(InstructionSegment{Type: "command", Command: compactCommand})
	if run.State != RunSucceeded || f.vendor.calls() != 1 {
		t.Fatalf("run=%+v calls=%d", run, f.vendor.calls())
	}
	if n := f.count("creative_agent_blocks", "run_id=$1 AND type='compaction' AND state='skipped'", run.ID); n != 1 {
		t.Fatalf("skipped blocks=%d", n)
	}
}

// A command stands alone, and only compact is a command.
func TestACommandIsTheWholeSubmission(t *testing.T) {
	for name, segments := range map[string][]InstructionSegment{
		"mixed":   {{Type: "command", Command: compactCommand}, {Type: "text", Text: "顺便说一句"}},
		"unknown": {{Type: "command", Command: "clear"}},
		"stray":   {{Type: "text", Text: "你好", Command: compactCommand}},
		"long":    {{Type: "command", Command: compactCommand, Text: strings.Repeat("长", compactFocusRunes+1)}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validSegments(segments); err == nil {
				t.Fatal("accepted")
			}
		})
	}
}

// The ring shows what the provider counted, split by what the request carried;
// a conversation that never sent anything shows nothing rather than a guess.
func TestContextUsageIsMeasuredNotGuessed(t *testing.T) {
	f := newCompactionFixture(t, testSummary)
	empty, err := f.service.ListMessages(t.Context(), f.alice, f.conversation.ID, "", 20)
	if err != nil {
		t.Fatal(err)
	}
	if empty.ContextUsage != nil {
		t.Fatalf("a new conversation showed usage %+v", empty.ContextUsage)
	}
	f.text("第一轮：秋日人像")
	page, err := f.service.ListMessages(t.Context(), f.alice, f.conversation.ID, "", 20)
	if err != nil {
		t.Fatal(err)
	}
	usage := page.ContextUsage
	if usage == nil || usage.Estimated || usage.UsedTokens != 120 || usage.WindowTokens != 128_000 || usage.CompactAtTokens != 0 {
		t.Fatalf("usage %+v", usage)
	}
	var sum int64
	keys := map[string]bool{}
	for _, part := range usage.Parts {
		sum += part.Tokens
		keys[part.Key] = true
	}
	if sum != usage.UsedTokens || !keys[partSystem] || !keys[partTools] || !keys[partMessages] {
		t.Fatalf("parts %+v", usage.Parts)
	}
	if n := f.count("creative_run_events", "event_type='context.usage'"); n != 1 {
		t.Fatalf("usage events=%d", n)
	}
	older, err := f.service.ListMessages(t.Context(), f.alice, f.conversation.ID, strconv.FormatInt(int64(page.Items[0].Ordinal), 10), 20)
	if err != nil {
		t.Fatal(err)
	}
	if older.ContextUsage != nil {
		t.Fatal("an older page carried usage")
	}
}

// A skill used earlier is indexed, not loaded; the model may load it again for
// its method, which brings no tool with it.
func TestAnEarlierSkillIsIndexedAndLoadedOnDemand(t *testing.T) {
	f := newCompactionFixture(t, testSummary)
	req := skillRequest("platform", "runtime-check", "运行指引")
	req.Activate = true
	req.Manifest.ToolAllowlist = []string{"load_skill@1", "read_skill_resource@1", "read_run_result@1"}
	v := f.runImport(f.platform, req)
	f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, _ int) (llmgateway.Result, error) {
		return scriptedResult(r), nil
	}
	f.say(InstructionSegment{Type: "text", Text: "整理"}, InstructionSegment{Type: "skill_ref", SkillID: v.SkillID, SkillVersionID: v.ID})
	var tools []string
	f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, n int) (llmgateway.Result, error) {
		if n == 2 {
			for _, tool := range r.Chat.Tools {
				tools = append(tools, tool.Name)
			}
			return scriptedResult(r, llmgateway.ToolCall{ID: "load", Name: "load_skill", Arguments: json.RawMessage(fmt.Sprintf(`{"skill":%q}`, v.ID))}), nil
		}
		return scriptedResult(r), nil
	}
	run := f.text("按刚才的方法再来一次")
	if run.State != RunSucceeded || f.vendor.calls() != 3 {
		t.Fatalf("run=%+v calls=%d", run, f.vendor.calls())
	}
	first := f.vendor.sentText(1)
	if !strings.Contains(first, "本会话较早使用过下面这些 Skill") || !strings.Contains(first, v.ID) || strings.Contains(first, req.Instructions) {
		t.Fatalf("the index was missing or carried the instructions:\n%s", first)
	}
	if !strings.Contains(f.vendor.sentText(2), req.Instructions) {
		t.Fatal("load_skill did not load the indexed version")
	}
	for _, name := range tools {
		if name == readSkillTool {
			t.Fatalf("an indexed skill added a tool: %v", tools)
		}
	}
}

// A conversation longer than the scan bound, as one from before compaction
// existed may be, is compacted rather than truncated forever: the checkpoint
// covers what the scan never reached.
func TestAConversationLongerThanTheScanStillCompacts(t *testing.T) {
	f := newCompactionFixture(t, testSummary)
	f.threeTurns()
	f.service.limits.HistoryMessages = 4
	f.service.limits.AutoCompactPercent = 99
	run := f.text("第四轮：出三版")
	if run.State != RunSucceeded || f.vendor.calls() != 5 {
		t.Fatalf("run=%+v calls=%d", run, f.vendor.calls())
	}
	if n := f.count("creative_agent_compactions", "covers_through_ordinal=2"); n != 1 {
		t.Fatalf("checkpoints covering the unscanned turn=%d", n)
	}
	if !strings.Contains(f.vendor.sentText(3), "第一轮：秋日人像") {
		t.Fatal("the unscanned turn was not summarised")
	}
}
