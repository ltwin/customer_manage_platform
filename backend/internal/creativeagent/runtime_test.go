package creativeagent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func scriptedResult(r llmgateway.ProviderRequest, calls ...llmgateway.ToolCall) llmgateway.Result {
	input, output, cached := int64(120), int64(40), int64(0)
	result := llmgateway.Result{Text: "整理完成。", FinishReason: llmgateway.FinishStop, Model: r.Snapshot.RequestModelID, Usage: llmgateway.UsageEvidence{InputTotalTokens: &input, OutputTokens: &output, InputCachedTokens: &cached}}
	if len(calls) > 0 {
		result.Text = ""
		result.FinishReason = llmgateway.FinishToolCalls
		result.ToolCalls = calls
	}
	return result
}
func TestRuntimeToolsArePersistedAndBounded(t *testing.T) {
	for _, tc := range []struct {
		name   string
		rounds int
		state  string
		calls  int
	}{{"answer", 1, RunSucceeded, 2}, {"loop", 20, RunFailed, 13}} {
		t.Run(tc.name, func(t *testing.T) {
			f := setup(t)
			f.queue()
			c := f.conversation(f.alice, f.canvasID)
			consent := f.consent(f.alice, c.ID)
			f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, n int) (llmgateway.Result, error) {
				if len(r.Chat.Tools) == 0 {
					t.Error("runtime tool schema was not sent to provider")
				}
				if n <= tc.rounds {
					return scriptedResult(r, llmgateway.ToolCall{ID: "repeated-provider-call-id", Name: "read_run_result", Arguments: json.RawMessage(`{"item_id":"missing","offset":0,"limit":100}`)}), nil
				}
				return scriptedResult(r), nil
			}
			run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "整理参考"})
			if err != nil {
				t.Fatal(err)
			}
			if err = f.work(f.alice, run.ID); err != nil {
				t.Fatal(err)
			}
			got, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.State != tc.state || f.vendor.calls() != tc.calls {
				t.Fatalf("state=%s code=%s calls=%d", got.State, got.ErrorCode, f.vendor.calls())
			}
			if n := f.count("creative_agent_steps", "run_id=$1 AND kind='tool'", run.ID); n == 0 || n > 12 {
				t.Fatalf("plans=%d", n)
			}
			if err = f.work(f.alice, run.ID); err != nil {
				t.Fatal(err)
			}
			if f.vendor.calls() != tc.calls {
				t.Fatal("redelivery dispatched")
			}
			assertRunReleased(t, f, run.ID)
		})
	}
}

func TestRuntimeRechecksConsentBetweenTurns(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	consent := f.consent(f.alice, c.ID)
	f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, _ int) (llmgateway.Result, error) {
		if _, err := f.service.RevokeConsent(t.Context(), f.alice, command(t, map[string]any{"consent_id": consent.ID, "expected_revision": "1"})); err != nil {
			t.Fatal(err)
		}
		return scriptedResult(r, llmgateway.ToolCall{ID: "call-1", Name: "read_run_result", Arguments: json.RawMessage(`{"item_id":"missing","offset":0,"limit":100}`)}), nil
	}
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "整理"})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	got, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != RunFailed || got.ErrorCode != "creative_egress_revoked" || f.vendor.calls() != 1 {
		t.Fatalf("run=%+v calls=%d", got, f.vendor.calls())
	}
	if n := f.count("llm_result_consumers", "state='consumed'"); n != 1 {
		t.Fatalf("consumed=%d", n)
	}
	assertRunReleased(t, f, run.ID)
}

func TestRuntimeLoadsOnlyFrozenSkillAndReadsDeclaredResource(t *testing.T) {
	f := setup(t)
	f.queue()
	req := skillRequest("platform", "runtime-check", "运行指引")
	req.Activate = true
	req.Manifest.ToolAllowlist = []string{"load_skill@1", "read_skill_resource@1", "read_run_result@1"}
	v := f.runImport(f.platform, req)
	c := f.conversation(f.alice, f.canvasID)
	consent := f.consent(f.alice, c.ID)
	f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, n int) (llmgateway.Result, error) {
		switch n {
		case 1:
			return scriptedResult(r, llmgateway.ToolCall{ID: "load", Name: "load_skill", Arguments: json.RawMessage(fmt.Sprintf(`{"skill":%q}`, v.ID))}), nil
		case 2:
			return scriptedResult(r, llmgateway.ToolCall{ID: "read", Name: "read_skill_resource", Arguments: resourceArgumentsFromRequest(t, r)}), nil
		default:
			return scriptedResult(r), nil
		}
	}
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "整理"}, InstructionSegment{Type: "skill_ref", SkillID: v.SkillID, SkillVersionID: v.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	got, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != RunSucceeded || f.vendor.calls() != 3 || !strings.Contains(f.vendor.prompt(), string(skillBody)) {
		t.Fatalf("run=%+v calls=%d prompt=%s", got, f.vendor.calls(), f.vendor.prompt())
	}
	if n := f.count("creative_agent_context_items", "run_id=$1 AND kind='skill_resource'", run.ID); n != 1 {
		t.Fatalf("resource roots=%d", n)
	}
	assertRunReleased(t, f, run.ID)
}

func TestRuntimeRejectsToolRequestsBeforeAnyExecution(t *testing.T) {
	for _, tc := range []struct {
		name  string
		calls []llmgateway.ToolCall
		code  string
	}{
		{"unknown", []llmgateway.ToolCall{{ID: "bad", Name: "shell", Arguments: json.RawMessage(`{}`)}}, "creative_tool_not_allowed"},
		{"batch above four", []llmgateway.ToolCall{{ID: "1", Name: readResultTool, Arguments: json.RawMessage(`{}`)}, {ID: "2", Name: readResultTool, Arguments: json.RawMessage(`{}`)}, {ID: "3", Name: readResultTool, Arguments: json.RawMessage(`{}`)}, {ID: "4", Name: readResultTool, Arguments: json.RawMessage(`{}`)}, {ID: "5", Name: readResultTool, Arguments: json.RawMessage(`{}`)}}, "creative_tool_limit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setup(t)
			f.queue()
			c := f.conversation(f.alice, f.canvasID)
			consent := f.consent(f.alice, c.ID)
			f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, _ int) (llmgateway.Result, error) {
				return scriptedResult(r, tc.calls...), nil
			}
			run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "整理"})
			if err != nil {
				t.Fatal(err)
			}
			if err = f.work(f.alice, run.ID); err != nil {
				t.Fatal(err)
			}
			got, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.ErrorCode != tc.code || f.vendor.calls() != 1 {
				t.Fatalf("run=%+v calls=%d", got, f.vendor.calls())
			}
			if n := f.count("creative_agent_context_items", "run_id=$1", run.ID); n != 0 {
				t.Fatalf("executed %d tools", n)
			}
			if n := f.count("llm_result_consumers", "state='consumed'"); n != 1 {
				t.Fatalf("consumed=%d", n)
			}
			assertRunReleased(t, f, run.ID)
		})
	}
}

func TestRuntimeContextUnloadsAndReadsOnlyThisRun(t *testing.T) {
	f := setup(t)
	f.queue()
	body := []byte(strings.Repeat("中文资料。", 2000))
	req := skillRequest("platform", "long-runtime", "长资源")
	req.Activate = true
	req.Manifest.ToolAllowlist = []string{"read_skill_resource@1", "read_run_result@1"}
	sum := sha256.Sum256(body)
	req.Resources[0].ByteSize = int64(len(body))
	req.Resources[0].SHA256 = hex.EncodeToString(sum[:])
	pending, err := f.skills.BeginImport(t.Context(), f.platform, req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.skills.StageResource(t.Context(), f.platform, pending.ID, pending.Pending[0], bytes.NewReader(body)); err != nil {
		t.Fatal(err)
	}
	v, err := f.skills.FinalizeImport(t.Context(), f.platform, pending.ID)
	if err != nil {
		t.Fatal(err)
	}
	c := f.conversation(f.alice, f.canvasID)
	consent := f.consent(f.alice, c.ID)
	var item string
	f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, n int) (llmgateway.Result, error) {
		if n == 1 {
			return scriptedResult(r, llmgateway.ToolCall{ID: "same", Name: readSkillTool, Arguments: resourceArgumentsFromRequest(t, r)}), nil
		}
		if n == 2 {
			last := r.Chat.Messages[len(r.Chat.Messages)-1]
			var locator struct {
				ItemID string `json:"item_id"`
			}
			if len(last.Blocks) != 1 {
				t.Fatal("missing tool projection")
			}
			if err = json.Unmarshal([]byte(last.Blocks[0].Text), &locator); err != nil {
				t.Fatal(err)
			}
			item = locator.ItemID
			if item == "" || len(last.Blocks[0].Text) > 2000 {
				t.Fatal("full resource was not unloaded")
			}
			return scriptedResult(r, llmgateway.ToolCall{ID: "same", Name: readResultTool, Arguments: json.RawMessage(fmt.Sprintf(`{"item_id":%q,"offset":0,"limit":1000}`, item))}), nil
		}
		return scriptedResult(r), nil
	}
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "整理"}, InstructionSegment{Type: "skill_ref", SkillID: v.SkillID, SkillVersionID: v.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	got, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
	if err != nil || got.State != RunSucceeded || f.vendor.calls() != 3 {
		t.Fatalf("run=%+v err=%v", got, err)
	}
	var stored []byte
	if err = f.db.QueryRow(`SELECT payload FROM creative_agent_context_items WHERE id=$1`, item).Scan(&stored); err != nil || !bytes.Equal(stored, body) {
		t.Fatalf("stored resource mismatch: %v", err)
	}
	// A new run in the same account cannot read the previous run's locator.
	second, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "新任务"})
	if err != nil {
		t.Fatal(err)
	}
	held, err := f.service.claimRun(t.Context(), f.alice, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	h := &runtimeHandler{service: f.service, scope: f.alice, held: held}
	if _, err = h.readRunResult(t.Context(), fmt.Sprintf(`{"item_id":%q,"offset":0,"limit":1000}`, item)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other run was readable: %v", err)
	}
	h.scope = f.bob
	if _, err = h.readRunResult(t.Context(), fmt.Sprintf(`{"item_id":%q,"offset":0,"limit":1000}`, item)); err == nil {
		t.Fatal("other account was readable")
	}
}

func TestRuntimeResourceAccessStaysInsideFrozenPackage(t *testing.T) {
	f := setup(t)
	req := skillRequest("platform", "fixed", "固定版本")
	req.Activate = true
	req.Manifest.ToolAllowlist = []string{"read_skill_resource@1"}
	v := f.runImport(f.platform, req)
	snap, err := f.skills.ResolveVersion(t.Context(), f.alice, v.SkillID, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	model, err := f.service.models.Model(testModelKey)
	if err != nil {
		t.Fatal(err)
	}
	h := &runtimeHandler{service: f.service, scope: f.alice, held: claim{Skill: &snap, Model: model}}
	for _, raw := range []string{fmt.Sprintf(`{"digest":%q,"path":"../references/comparison-checklist.md"}`, v.Digest), `{"digest":"wrong","path":"references/comparison-checklist.md"}`, fmt.Sprintf(`{"digest":%q,"path":"not-registered"}`, v.Digest)} {
		if _, err = h.readSkillResource(t.Context(), raw); !errors.Is(err, ErrNotFound) {
			t.Fatalf("invalid resource: %v", err)
		}
	}
	backend := &runSkillBackend{handler: h}
	list, err := backend.List(t.Context())
	if err != nil || len(list) != 1 || list[0].Name != v.ID {
		t.Fatalf("list=%+v err=%v", list, err)
	}
	if _, err = backend.Get(t.Context(), "latest"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("resolved latest: %v", err)
	}
	req.OperationID = uuid.NewString()
	req.ExpectedSkillRevision = v.SkillRevision
	req.Instructions = "新版正文"
	f.runImport(f.platform, req)
	fixed, err := backend.Get(t.Context(), v.ID)
	if err != nil || fixed.Content != snap.Instructions || fixed.Model != "" || fixed.Context != "" {
		t.Fatalf("fixed=%+v err=%v", fixed, err)
	}
}

func TestRuntimeFinishingReleasesLaterUnclaimedReservations(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	consent := f.consent(f.alice, c.ID)
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "整理"})
	if err != nil {
		t.Fatal(err)
	}
	held, err := f.service.claimRun(t.Context(), f.alice, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	err = f.alice.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		_, err := f.service.gateway.ReserveInTx(t.Context(), tx, llmgateway.ReserveInput{CallerService: callerService, CallerOperationID: uuid.NewString(), CallerGroupID: run.ID, GroupTokenLimit: runTokenCeiling(f.service.limits, held.Model), GroupDeadline: held.Deadline, ModelKey: held.Model.ModelKey, InputTokenUpperBound: 100, OutputLimit: 100, ExpiresAt: held.Deadline})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := f.holds(); n != 2 {
		t.Fatalf("reservations=%d", n)
	}
	if err = f.service.finishRun(t.Context(), f.alice, held, turnOutcome{}, context.Canceled); err != nil {
		t.Fatal(err)
	}
	assertRunReleased(t, f, run.ID)
}

func TestRuntimeModelLimitUsesDurableCountNotPromptHash(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	consent := f.consent(f.alice, c.ID)
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "整理"})
	if err != nil {
		t.Fatal(err)
	}
	held, err := f.service.claimRun(t.Context(), f.alice, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	chat := llmgateway.ChatRequest{ContractVersion: llmgateway.ContractVersion, ModelKey: held.Model.ModelKey, Messages: []llmgateway.Message{{Role: llmgateway.RoleUser, Blocks: []llmgateway.Block{{Kind: llmgateway.BlockText, Text: "相同输入"}}}}, OutputLimit: 100}
	seen := map[string]bool{}
	previous := ""
	for i := 0; i < f.service.limits.ModelTurns; i++ {
		id, _, err := f.service.prepareModelStep(t.Context(), f.alice, held, previous, chat)
		if err != nil || seen[id] {
			t.Fatalf("step %d: %s %v", i, id, err)
		}
		seen[id] = true
		previous = id
	}
	if _, _, err = f.service.prepareModelStep(t.Context(), f.alice, held, previous, chat); !errors.Is(err, errModelLimit) {
		t.Fatalf("14th turn: %v", err)
	}
}

// Revoke after the second model step exists, before its gateway dispatch. This
// exercises a later *claimed* reservation, distinct from the unclaimed case.
func TestRuntimeSecondDispatchRevocationReturnsItsBudget(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	consent := f.consent(f.alice, c.ID)
	if _, err := f.db.Exec(`CREATE FUNCTION revoke_before_second_turn() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NEW.kind='model' AND NEW.ordinal>1 THEN UPDATE creative_egress_consents SET revoked_at=clock_timestamp() WHERE account_id=NEW.account_id; END IF; RETURN NEW; END $$;
 CREATE TRIGGER revoke_second_turn BEFORE INSERT ON creative_agent_steps FOR EACH ROW EXECUTE FUNCTION revoke_before_second_turn()`); err != nil {
		t.Fatal(err)
	}
	f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, _ int) (llmgateway.Result, error) {
		return scriptedResult(r, llmgateway.ToolCall{ID: "first", Name: readResultTool, Arguments: json.RawMessage(`{"item_id":"missing","offset":0,"limit":100}`)}), nil
	}
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "整理"})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	got, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
	if err != nil || got.ErrorCode != "creative_egress_revoked" || f.vendor.calls() != 1 {
		t.Fatalf("run=%+v err=%v calls=%d", got, err, f.vendor.calls())
	}
	if n, _ := f.holds(); n != 2 {
		t.Fatalf("reservations=%d", n)
	}
	if n := f.count("llm_result_consumers", "state='pending'"); n != 0 {
		t.Fatalf("pending consumers=%d", n)
	}
	assertRunReleased(t, f, run.ID)
	if n := f.count("creative_agent_steps", "run_id=$1 AND state='prepared'", run.ID); n != 0 {
		t.Fatalf("unfinished plans=%d", n)
	}
}

func TestRuntimeRepeatedContentRefsRemainUsable(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	rev := f.revision(f.alice, "重复引用")
	consent := f.consent(f.alice, c.ID, rev)
	f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, n int) (llmgateway.Result, error) {
		if n == 1 {
			return scriptedResult(r, llmgateway.ToolCall{ID: "r", Name: readResultTool, Arguments: json.RawMessage(`{"item_id":"missing","offset":0,"limit":100}`)}), nil
		}
		return scriptedResult(r), nil
	}
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "整理"}, InstructionSegment{Type: "content_ref", ContentRevisionID: rev}, InstructionSegment{Type: "content_ref", ContentRevisionID: rev})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	got, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
	if err != nil || got.State != RunSucceeded {
		t.Fatalf("run=%+v err=%v", got, err)
	}
}

func resourceArgumentsFromRequest(t *testing.T, r llmgateway.ProviderRequest) json.RawMessage {
	t.Helper()
	for _, tool := range r.Chat.Tools {
		if tool.Name != readSkillTool {
			continue
		}
		var schema struct {
			Properties map[string]struct {
				Enum []string `json:"enum"`
			} `json:"properties"`
		}
		if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
			t.Fatal(err)
		}
		digests, paths := schema.Properties["digest"].Enum, schema.Properties["path"].Enum
		if len(digests) != 1 || len(paths) == 0 {
			t.Fatal("model has no fixed digest/resource paths")
		}
		raw, err := json.Marshal(map[string]string{"digest": digests[0], "path": paths[0]})
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	t.Fatal("read_skill_resource absent from actual model request")
	return nil
}

func TestRuntimeResourceWithoutReadbackPermissionStaysInline(t *testing.T) {
	f := setup(t)
	f.queue()
	body := []byte(strings.Repeat("完整资料", 2500))
	req := skillRequest("platform", "inline-only", "仅资源读取")
	req.Activate = true
	req.Manifest.ToolAllowlist = []string{"read_skill_resource@1"}
	sum := sha256.Sum256(body)
	req.Resources[0].ByteSize = int64(len(body))
	req.Resources[0].SHA256 = hex.EncodeToString(sum[:])
	record, err := f.skills.BeginImport(t.Context(), f.platform, req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.skills.StageResource(t.Context(), f.platform, record.ID, record.Pending[0], bytes.NewReader(body)); err != nil {
		t.Fatal(err)
	}
	v, err := f.skills.FinalizeImport(t.Context(), f.platform, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	c := f.conversation(f.alice, f.canvasID)
	consent := f.consent(f.alice, c.ID)
	f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, n int) (llmgateway.Result, error) {
		if n == 1 {
			return scriptedResult(r, llmgateway.ToolCall{ID: "resource", Name: readSkillTool, Arguments: resourceArgumentsFromRequest(t, r)}), nil
		}
		last := r.Chat.Messages[len(r.Chat.Messages)-1]
		if len(last.Blocks) != 1 || last.Blocks[0].Text != string(body) {
			t.Error("resource cannot be read back and was not kept inline")
		}
		for _, tool := range r.Chat.Tools {
			if tool.Name == readResultTool {
				t.Error("readback was granted without permission")
			}
		}
		return scriptedResult(r), nil
	}
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "整理"}, InstructionSegment{Type: "skill_ref", SkillID: v.SkillID, SkillVersionID: v.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	got, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
	if err != nil || got.State != RunSucceeded || f.vendor.calls() != 2 {
		t.Fatalf("run=%+v err=%v", got, err)
	}
	assertRunReleased(t, f, run.ID)
}
