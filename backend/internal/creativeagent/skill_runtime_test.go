package creativeagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/google/uuid"

	"github.com/samson/customer-manage-platform/backend/internal/creativeskill"
	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func skillHandler(t *testing.T, f *fixture, conversation string, segments ...InstructionSegment) *runtimeHandler {
	t.Helper()
	f.queue()
	if conversation == "" {
		conversation = f.conversation(f.alice, f.canvasID).ID
	}
	run, err := f.startRun(f.alice, conversation, f.consent(f.alice, conversation).ID, segments...)
	if err != nil {
		t.Fatal(err)
	}
	held, err := f.service.claimRun(t.Context(), f.alice, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	h := &runtimeHandler{BaseChatModelAgentMiddleware: &adk.BaseChatModelAgentMiddleware{}, service: f.service, scope: f.alice, held: held}
	err = f.alice.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		var err error
		h.skillIndex, err = frozenSkillIndexInTx(t.Context(), tx, run.ID)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return h
}
func publishMethod(t *testing.T, f *fixture, slug string) creativeskill.Version {
	t.Helper()
	req := skillRequest("platform", slug, slug)
	req.Activate = true
	req.Manifest = creativeskill.Manifest{ManifestSchemaVersion: 2}
	return f.runImport(f.platform, req)
}
func resourceCall(version string) string {
	return fmt.Sprintf(`{"skill_version_id":%q,"path":"references/comparison-checklist.md"}`, version)
}

func TestSkillDiscoveryActivationAndResourceBoundaries(t *testing.T) {
	f := setup(t)
	v := publishMethod(t, f, "method-a")
	other := publishMethod(t, f, "method-b")
	h := skillHandler(t, f, "", InstructionSegment{Type: "text", Text: "按方法处理"})
	text, err := h.initializeSkills(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, v.ID) || strings.Contains(text, "先检索，再比较") {
		t.Fatal("discovery missing or leaked instructions")
	}
	if _, err := h.readSkillResource(t.Context(), resourceCall(v.ID)); err == nil {
		t.Fatal("read unactivated skill")
	}
	if _, err := h.activateSkill(t.Context(), v.ID); err != nil {
		t.Fatal(err)
	}
	body, err := h.readSkillResource(t.Context(), resourceCall(v.ID))
	if err != nil || body != string(skillBody) {
		t.Fatalf("resource %q %v", body, err)
	}
	if _, err := h.activateSkill(t.Context(), other.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.readSkillResource(t.Context(), resourceCall(other.ID)); err != nil {
		t.Fatal(err)
	}
	foreign := &runtimeHandler{service: h.service, scope: f.bob, held: h.held}
	if _, err := foreign.activateSkill(t.Context(), v.ID); err == nil {
		t.Fatal("another account read run candidates")
	}
	if err := f.skills.DisableVersion(t.Context(), f.platform, v.ID, "withdrawn"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.readSkillResource(t.Context(), resourceCall(other.ID)); err == nil {
		t.Fatal("dispatch ignored a withdrawn active skill")
	}
}

func TestSkillDiscoveryPinsPagesAcrossRecoveryAndPublication(t *testing.T) {
	f := setup(t)
	v := publishMethod(t, f, "method")
	h := skillHandler(t, f, "", InstructionSegment{Type: "text", Text: "找方法"})
	first, err := h.initializeSkills(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	page, err := h.discoverSkills(t.Context(), "method", "", "search-step")
	if err != nil {
		t.Fatal(err)
	}
	req := skillRequest("platform", "method", "new name")
	req.Activate = true
	req.ExpectedSkillRevision = v.SkillRevision
	req.Manifest = creativeskill.Manifest{ManifestSchemaVersion: 2}
	req.Instructions = "new instructions"
	f.runImport(f.platform, req)
	restored := &runtimeHandler{service: f.service, scope: f.alice, held: h.held}
	replay, err := restored.initializeSkills(t.Context())
	if err != nil || replay != first {
		t.Fatalf("initial page moved: %v", err)
	}
	again, err := restored.discoverSkills(t.Context(), "method", "", "search-step")
	if err != nil || again.Items[0].VersionID != page.Items[0].VersionID {
		t.Fatalf("search page moved: %v", err)
	}
	loaded, err := restored.activateSkill(t.Context(), v.ID)
	if err != nil || loaded.Instructions == "new instructions" {
		t.Fatalf("version moved: %v", err)
	}
}

func TestSkillActivationLimitAndHistoricalResources(t *testing.T) {
	f := newCompactionFixture(t, testSummary)
	a := publishMethod(t, f.fixture, "method-a")
	b := publishMethod(t, f.fixture, "method-b")
	first := f.say(InstructionSegment{Type: "text", Text: "方法 A"}, InstructionSegment{Type: "skill_ref", SkillID: a.SkillID, SkillVersionID: a.ID})
	if first.State != RunSucceeded {
		t.Fatalf("first: %+v", first)
	}
	h := skillHandler(t, f.fixture, first.ConversationID, InstructionSegment{Type: "text", Text: "按刚才的方法"})
	h.service.limits.ActivatedSkillsPerRun = 1
	if _, err := h.initializeSkills(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := h.activateSkill(t.Context(), a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.activateSkill(t.Context(), a.ID); err != nil {
		t.Fatal("repeated activation counted twice", err)
	}
	if _, err := h.activateSkill(t.Context(), b.ID); err == nil {
		t.Fatal("activation limit ignored")
	}
	if _, err := h.readSkillResource(t.Context(), resourceCall(a.ID)); err != nil {
		t.Fatal("historical resource inaccessible", err)
	}
}

func TestSkillSearchPaginationAndUnavailableDependencies(t *testing.T) {
	f := setup(t)
	for i := 0; i < 21; i++ {
		publishMethod(t, f, fmt.Sprintf("method-%02d", i))
	}
	req := skillRequest("platform", "blocked", "blocked")
	req.Activate = true
	req.Manifest = creativeskill.Manifest{ManifestSchemaVersion: 2, RequiredTools: []string{"not_installed@1"}}
	bad := f.runImport(f.platform, req)
	h := skillHandler(t, f, "", InstructionSegment{Type: "text", Text: "找方法"})
	if _, err := h.initializeSkills(t.Context()); err != nil {
		t.Fatal(err)
	}
	page, err := h.discoverSkills(t.Context(), "", "", "page1")
	if err != nil {
		t.Fatal(err)
	}
	if page.NextCursor == "" {
		t.Fatal("lost next page")
	}
	for _, e := range page.Items {
		if e.VersionID == bad.ID {
			t.Fatal("offered unavailable skill")
		}
	}
	next, err := h.discoverSkills(t.Context(), "", page.NextCursor, "page2")
	if err != nil || len(next.Items) == 0 {
		t.Fatalf("second page: %+v %v", next, err)
	}
}

func TestSkillLoadAndReadShareTheDurableToolJournal(t *testing.T) {
	f := setup(t)
	f.queue()
	v := publishMethod(t, f, "method")
	c := f.conversation(f.alice, f.canvasID)
	f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, n int) (llmgateway.Result, error) {
		if n == 1 {
			return scriptedResult(r,
				llmgateway.ToolCall{ID: "activate", Name: loadSkillTool, Arguments: json.RawMessage(fmt.Sprintf(`{"skill_version_id":%q}`, v.ID))},
				llmgateway.ToolCall{ID: "resource", Name: readSkillTool, Arguments: json.RawMessage(resourceCall(v.ID))}), nil
		}
		var prompt strings.Builder
		for _, message := range r.Chat.Messages {
			for _, block := range message.Blocks {
				prompt.WriteString(block.Text)
			}
		}
		if !strings.Contains(prompt.String(), string(skillBody)) {
			t.Error("resource not delivered after activation")
		}
		return scriptedResult(r), nil
	}
	run, err := f.startRun(f.alice, c.ID, f.consent(f.alice, c.ID).ID, InstructionSegment{Type: "text", Text: "使用合适方法"})
	if err != nil {
		t.Fatal(err)
	}
	held, err := f.service.claimRun(t.Context(), f.alice, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	execution, err := f.service.newRunExecution(t.Context(), f.alice, held)
	if err != nil {
		t.Fatal(err)
	}
	execution.config.Handlers = append([]adk.ChatModelAgentMiddleware{&checkpointPause{BaseChatModelAgentMiddleware: &adk.BaseChatModelAgentMiddleware{}, after: true}}, execution.config.Handlers...)
	if _, err := execution.run(t.Context()); !errors.Is(err, errRunInterrupted) {
		t.Fatalf("interrupt: %v", err)
	}
	restored, err := f.service.newRunExecution(t.Context(), f.alice, held)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restored.run(t.Context()); err != nil {
		t.Fatal(err)
	}
	if n := f.count("creative_run_skills", "run_id=$1 AND activated_at IS NOT NULL", run.ID); n != 1 {
		t.Fatalf("activations=%d", n)
	}
	if n := f.count("creative_agent_steps", "run_id=$1 AND kind='tool' AND state='succeeded'", run.ID); n != 2 {
		t.Fatalf("completed tools=%d", n)
	}
}

func TestToolRegistryRejectsDuplicatesAndPreservesResourceOrdering(t *testing.T) {
	defs := agentToolDefinitions()
	if err := validateToolDefinitions(append(defs, defs[0])); err == nil {
		t.Fatal("accepted duplicate")
	}
	calls := []plannedCall{{id: uuid.NewString(), name: loadSkillTool}, {id: uuid.NewString(), name: readSkillTool}}
	batch := newToolBatch(calls, nil)
	if len(batch.deps[1]) != 1 || batch.deps[1][0] != 0 {
		t.Fatal("resource read can race activation")
	}
	if len(toolResources("unknown", "", nil, false)) != 1 {
		t.Fatal("unknown tool did not hold run")
	}
}

func TestUnknownModelCapabilityIsReportedBeforeActivation(t *testing.T) {
	f := setup(t)
	req := skillRequest("platform", "future-model", "future model")
	req.Activate = true
	req.Manifest = creativeskill.Manifest{ManifestSchemaVersion: 2, RequiredModelCapabilities: []string{"not_implemented"}}
	v := f.runImport(f.platform, req)
	view, err := f.service.SkillVersion(t.Context(), f.alice, v.SkillID, v.ID)
	if err != nil || view.Available || !strings.Contains(view.UnavailableWhy, "not_implemented") {
		t.Fatalf("availability: %+v %v", view, err)
	}
	h := skillHandler(t, f, "", InstructionSegment{Type: "text", Text: "方法"})
	text, err := h.initializeSkills(t.Context())
	if err != nil || strings.Contains(text, v.ID) {
		t.Fatalf("offered unsupported skill: %s %v", text, err)
	}
}

func TestSkillHistoryDoesNotResendWithdrawnBodies(t *testing.T) {
	f := setup(t)
	v := publishMethod(t, f, "history-private")
	c := f.conversation(f.alice, f.canvasID)
	if err := f.alice.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		for _, tool := range []string{loadSkillTool, readSkillTool} {
			_, err := f.service.appendMessageInTx(t.Context(), tx, c.ID, newMessage{Role: "tool", Status: "complete", Body: Body{ToolName: tool, ToolCallID: tool, Blocks: []Block{{Type: "text", Text: "WITHDRAWN_PRIVATE_BODY"}}}})
			if err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.skills.DisableVersion(t.Context(), f.platform, v.ID, "withdrawn"); err != nil {
		t.Fatal(err)
	}
	h := skillHandler(t, f, c.ID, InstructionSegment{Type: "text", Text: "读取历史"})
	text, err := h.readHistory(t.Context(), `{"before_ordinal":0,"limit":20}`)
	if err != nil || strings.Contains(text, "WITHDRAWN_PRIVATE_BODY") || !strings.Contains(text, "固定版本") {
		t.Fatalf("history leaked: %s %v", text, err)
	}
}

func TestSkillDiscoveryTrimsDirectoryBeforeSelectedResources(t *testing.T) {
	page := skillDiscoveryPage{}
	for i := 0; i < 20; i++ {
		page.Items = append(page.Items, skillIndexEntry{VersionID: fmt.Sprintf("version-%d", i), DisplayName: strings.Repeat("<", 96), Description: strings.Repeat("<", 192)})
	}
	selected := &creativeskill.Snapshot{Version: creativeskill.Version{ID: "selected-version"}}
	for i := 0; i < 20; i++ {
		selected.Resources = append(selected.Resources, creativeskill.Resource{Path: fmt.Sprintf("references/%02d-%s.md", i, strings.Repeat("p", 210)), SHA256: strings.Repeat("a", 64), Mime: "text/markdown", ByteSize: 1})
	}
	text, err := skillDiscoveryText(page, selected)
	if err != nil || len(text) > skillDiscoveryBytes || !strings.Contains(text, selected.Resources[19].Path) || !strings.Contains(text, selected.ID) {
		t.Fatalf("selected lost: bytes=%d err=%v", len(text), err)
	}
	again, err := skillDiscoveryText(page, selected)
	if err != nil || again != text || len(page.Items) != 20 {
		t.Fatal("directory replay mutated")
	}
}

// A method loaded in one turn stays loaded in the next: its body is resent and
// its resources read without a second load_skill. Withdrawal ends that.
func TestLoadedSkillCarriesIntoLaterTurns(t *testing.T) {
	f := setup(t)
	f.queue()
	v := publishMethod(t, f, "method")
	c := f.conversation(f.alice, f.canvasID)
	consent := f.consent(f.alice, c.ID)
	prompt := func(r llmgateway.ProviderRequest) string {
		var b strings.Builder
		for _, message := range r.Chat.Messages {
			for _, block := range message.Blocks {
				b.WriteString(block.Text)
			}
		}
		return b.String()
	}
	calls := 0
	f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, _ int) (llmgateway.Result, error) {
		calls++
		switch calls {
		case 1:
			return scriptedResult(r, llmgateway.ToolCall{ID: "activate", Name: loadSkillTool, Arguments: json.RawMessage(fmt.Sprintf(`{"skill_version_id":%q}`, v.ID))}), nil
		case 3:
			if !strings.Contains(prompt(r), "先检索，再比较") {
				t.Error("loaded method dropped from the next turn")
			}
			return scriptedResult(r, llmgateway.ToolCall{ID: "resource", Name: readSkillTool, Arguments: json.RawMessage(resourceCall(v.ID))}), nil
		case 4:
			if !strings.Contains(prompt(r), string(skillBody)) {
				t.Error("resource refused without a second load")
			}
		case 5:
			if p := prompt(r); strings.Contains(p, "先检索，再比较") || !strings.Contains(p, skillHistoryUnloaded) {
				t.Error("withdrawn method resent")
			}
		}
		return scriptedResult(r), nil
	}
	turn := func(text string) Run {
		run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: text})
		if err != nil {
			t.Fatal(err)
		}
		if err := f.work(f.alice, run.ID); err != nil {
			t.Fatal(err)
		}
		return run
	}
	turn("用合适的方法")
	second := turn("继续，核对比较清单")
	if n := f.count("creative_agent_steps", "run_id=$1 AND kind='tool' AND tool_key=$2", second.ID, loadSkillTool); n != 0 {
		t.Fatalf("second turn reloaded %d times", n)
	}
	if n := f.count("creative_run_skills", "run_id=$1 AND skill_version_id=$2 AND source='history' AND activated_at IS NOT NULL", second.ID, v.ID); n != 1 {
		t.Fatalf("carried activations=%d", n)
	}
	if err := f.skills.DisableVersion(t.Context(), f.platform, v.ID, "withdrawn"); err != nil {
		t.Fatal(err)
	}
	turn("再来一次")
	if calls != 5 {
		t.Fatalf("model calls=%d", calls)
	}
}
