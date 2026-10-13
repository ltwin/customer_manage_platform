package creativeagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func libraryConsent(t *testing.T, f *fixture, conversation string, ids ...string) Consent {
	t.Helper()
	receipt, err := f.service.GrantConsent(t.Context(), f.alice, command(t, map[string]any{"conversation_id": conversation, "purpose": "creative_assistance", "mode": ScopeAccountLibrary, "data_classes": []string{"text", "image_preview", "canvas_metadata", "media_metadata"}, "selected_revision_ids": ids}))
	if err != nil {
		t.Fatal(err)
	}
	var c Consent
	if err := json.Unmarshal(receipt.Outcome.Response, &c); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestLibraryConsentAcceptsExplicitConversationInput(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	id := f.revision(f.alice, "会话上传内容")
	consent := libraryConsent(t, f, c.ID, id)
	if _, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "content_ref", ContentRevisionID: id}); err != nil {
		t.Fatal(err)
	}
}

func TestConversationReferenceOutlivesHistoryWindow(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	id := f.revision(f.alice, "早期参考")
	err := f.alice.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		_, err := f.service.appendMessageInTx(t.Context(), tx, c.ID, newMessage{Role: "user", Status: "complete", Body: Body{Blocks: []Block{{Type: "content_ref", RefID: id}}}, ContentRefs: []ContentRef{{RevisionID: id, Role: "input"}}})
		if err != nil {
			return err
		}
		for range 25 {
			if _, err = f.service.appendMessageInTx(t.Context(), tx, c.ID, newMessage{Role: "user", Status: "complete", Body: Body{Blocks: []Block{{Type: "text", Text: "后续讨论"}}}}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	consent := contextConsent(t, f, c.ID)
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "找回参考"})
	if err != nil {
		t.Fatal(err)
	}
	err = f.alice.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		ok, err := readableInTx(t.Context(), tx, run.ID, consent.ID, id)
		if err == nil && !ok {
			t.Error("older conversation reference is unreadable")
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func appendLookupMessage(t *testing.T, f *fixture, c string, refs []ContentRef, text string) Message {
	t.Helper()
	var m Message
	err := f.alice.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		var err error
		m, err = f.service.appendMessageInTx(t.Context(), tx, c, newMessage{Role: "user", Status: "complete", Body: Body{Blocks: []Block{{Type: "text", Text: text}}}, ContentRefs: refs})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return m
}
func lookupHandler(t *testing.T, f *fixture, c string, consent Consent) *runtimeHandler {
	t.Helper()
	run, err := f.startRun(f.alice, c, consent.ID, InstructionSegment{Type: "text", Text: "查找素材"})
	if err != nil {
		t.Fatal(err)
	}
	model, err := f.service.models.Model(testModelKey)
	if err != nil {
		t.Fatal(err)
	}
	return &runtimeHandler{service: f.service, scope: f.alice, held: claim{RunID: run.ID, ConversationID: c, ConsentID: consent.ID, Model: model}}
}

func TestConversationAssetsPaginationAndIsolation(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	refs := []ContentRef{}
	for i := 0; i < 23; i++ {
		refs = append(refs, ContentRef{RevisionID: f.revision(f.alice, fmt.Sprintf("素材 %d", i)), Role: "input"})
	}
	appendLookupMessage(t, f, c.ID, refs, "参考集")
	appendLookupMessage(t, f, c.ID, refs[:1], "最新参考")
	other := f.conversation(f.alice, f.canvasID)
	otherID := f.revision(f.alice, "另一个对话")
	appendLookupMessage(t, f, other.ID, []ContentRef{{RevisionID: otherID, Role: "input"}}, "不应出现")
	h := lookupHandler(t, f, c.ID, contextConsent(t, f, c.ID))
	futureID := f.revision(f.alice, "未来消息")
	appendLookupMessage(t, f, c.ID, []ContentRef{{RevisionID: futureID, Role: "input"}}, "另一个窗口后发")
	seen := map[string]bool{}
	before := int64(0)
	beforeID := ""
	for page := 0; page < 10; page++ {
		raw := fmt.Sprintf(`{"before_ordinal":%d,"before_revision_id":%q,"limit":4}`, before, beforeID)
		result, err := h.listConversationAssets(t.Context(), raw)
		if err != nil {
			t.Fatal(err)
		}
		var out struct {
			Items  []conversationAsset `json:"items"`
			More   bool                `json:"has_more"`
			Next   string              `json:"next_before_ordinal"`
			NextID string              `json:"next_before_revision_id"`
		}
		if err := json.Unmarshal([]byte(result), &out); err != nil {
			t.Fatal(err)
		}
		if len(out.Items) > 4 {
			t.Fatal("page exceeds limit")
		}
		for _, item := range out.Items {
			if seen[item.RevisionID] || item.RevisionID == otherID || item.RevisionID == futureID {
				t.Fatalf("duplicate or out of scope: %+v", item)
			}
			seen[item.RevisionID] = true
			if item.Kind != "text" || item.Source != "photographer" {
				t.Fatalf("wrong metadata: %+v", item)
			}
		}
		if !out.More {
			break
		}
		before, err = strconv.ParseInt(out.Next, 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		beforeID = out.NextID
	}
	if len(seen) != 23 {
		t.Fatalf("retrieved %d of 23", len(seen))
	}
	for _, raw := range []string{`{"before_ordinal":0,"limit":4,"kind":"image"}`, `{"before_ordinal":0,"limit":4,"source":"assistant"}`, `{"before_ordinal":0,"limit":4,"query":"无匹配"}`} {
		result, err := h.listConversationAssets(t.Context(), raw)
		if err != nil || !strings.Contains(result, `"items":[]`) {
			t.Fatalf("empty query: %s %v", result, err)
		}
	}
	for _, id := range []string{otherID, futureID, f.revision(f.bob, "其他账号")} {
		if _, err := h.readAsset(t.Context(), fmt.Sprintf(`{"content_revision_id":%q,"offset":0,"limit":100}`, id)); !errors.Is(err, errReadRefused) {
			t.Fatalf("foreign read: %v", err)
		}
	}
}

func TestRevokedConversationReferenceCannotUseLibraryBranch(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	id := f.revision(f.alice, "撤销的参考")
	previous := contextConsent(t, f, c.ID, id)
	appendLookupMessage(t, f, c.ID, []ContentRef{{RevisionID: id, Role: "input"}}, "原引用")
	if _, err := f.service.RevokeConsent(t.Context(), f.alice, command(t, map[string]any{"consent_id": previous.ID, "expected_revision": previous.Revision})); err != nil {
		t.Fatal(err)
	}
	h := lookupHandler(t, f, c.ID, libraryConsent(t, f, c.ID, id))
	if _, err := h.readAsset(t.Context(), fmt.Sprintf(`{"content_revision_id":%q,"offset":0,"limit":100}`, id)); !errors.Is(err, errReadRefused) {
		t.Fatalf("revocation bypassed: %v", err)
	}
}

func TestLibraryToolsAndCanvasRoundTrip(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	body := "日落参考资料"
	receipt, err := creativelibrary.CreateAsset(t.Context(), f.alice, command(t, creativelibrary.CreateAssetInput{Title: "日落参考", Content: creativecontent.Draft{Kind: "text", Payload: creativecontent.Payload{Body: &body}}}))
	if err != nil {
		t.Fatal(err)
	}
	var asset creativelibrary.AssetResult
	if err := json.Unmarshal(receipt.Outcome.Response, &asset); err != nil {
		t.Fatal(err)
	}
	h := lookupHandler(t, f, c.ID, libraryConsent(t, f, c.ID))
	result, err := h.searchAssets(t.Context(), `{"query":"日落","limit":10}`)
	if err != nil || !strings.Contains(result, asset.ID) || !strings.Contains(result, asset.ContentRevisionID) {
		t.Fatalf("search %s %v", result, err)
	}
	if _, err := h.readLibrary(t.Context(), `{}`); err != nil {
		t.Fatal(err)
	}
	if _, err := h.searchAssets(t.Context(), `{"view":"trash","limit":10}`); err == nil {
		t.Fatal("trash allowed")
	}
	if _, err := h.editCanvas(t.Context(), f.canvasID, "wrong", fmt.Sprintf(`{"actions":[{"type":"add_node","key":"a","node_type":"text","asset_id":%q,"content_revision_id":"stale"}]}`, asset.ID)); err == nil {
		t.Fatal("stale asset allowed")
	}
	if _, err := h.editCanvas(t.Context(), f.canvasID, "bypass", fmt.Sprintf(`{"actions":[{"type":"add_node","key":"a","node_type":"text","content_revision_id":%q}]}`, asset.ContentRevisionID)); err == nil {
		t.Fatal("library without asset id bypassed optimistic check")
	}
	raw := fmt.Sprintf(`{"actions":[{"type":"add_node","key":"a","node_type":"text","asset_id":%q,"content_revision_id":%q},{"type":"add_node","key":"b","node_type":"text"},{"type":"fill_node","node_id":"b","asset_id":%q,"content_revision_id":%q}]}`, asset.ID, asset.ContentRevisionID, asset.ID, asset.ContentRevisionID)
	if _, err := h.editCanvas(t.Context(), f.canvasID, "place", raw); err != nil {
		t.Fatal(err)
	}
	canvas, err := creativecanvas.GetCanvas(t.Context(), f.alice, f.canvasID)
	if err != nil {
		t.Fatal(err)
	}
	if len(canvas.Nodes) != 2 {
		t.Fatalf("nodes %d", len(canvas.Nodes))
	}
	for _, n := range canvas.Nodes {
		if n.ContentRevisionID == nil || *n.ContentRevisionID != asset.ContentRevisionID {
			t.Fatal("wrong placed content")
		}
	}
	for range 2 {
		result, err := h.saveToLibrary(t.Context(), f.canvasID, "save", fmt.Sprintf(`{"node_id":%q,"title":"已收藏"}`, canvas.Nodes[0].ID))
		if err != nil {
			t.Fatal(err)
		}
		out, _ := result.(map[string]any)
		if out["asset_id"] != asset.ID || out["reused"] != true {
			t.Fatalf("save %+v", out)
		}
	}
	if _, err := h.saveToLibrary(t.Context(), f.canvasID, "tags", fmt.Sprintf(`{"node_id":%q,"title":"新标签","new_tags":[]}`, canvas.Nodes[0].ID)); err == nil {
		t.Fatal("new_tags accepted")
	}
	if _, err := creativelibrary.Trash(t.Context(), f.alice, command(t, creativelibrary.AssetVersion{ID: asset.ID, Revision: asset.Revision})); err != nil {
		t.Fatal(err)
	}
	if _, err := h.editCanvas(t.Context(), f.canvasID, "trashed", raw); err == nil {
		t.Fatal("trashed asset placed")
	}
}

func TestAppliedToolResultsBecomeConversationContent(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	h := lookupHandler(t, f, c.ID, contextConsent(t, f, c.ID))
	ids := []string{f.revision(f.alice, "生成一"), f.revision(f.alice, "生成二"), f.revision(f.alice, "压缩"), f.revision(f.alice, "未采用")}
	cases := []struct {
		name, raw string
		count     int
	}{
		{generateNodeTool, fmt.Sprintf(`{"apply_state":"applied","content_revision_ids":[%q,%q]}`, ids[0], ids[1]), 2},
		{compressImageTool, fmt.Sprintf(`{"state":"applied","content_revision_id":%q}`, ids[2]), 1},
		{generateNodeTool, fmt.Sprintf(`{"apply_state":"conflicted","content_revision_ids":[%q]}`, ids[3]), 0},
		{compressImageTool, fmt.Sprintf(`{"state":"not_applied","content_revision_id":%q}`, ids[3]), 0},
	}
	for i, tc := range cases {
		err := f.alice.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
			m, err := h.persistToolMessage(t.Context(), tx, fmt.Sprintf("step-%d", i), fmt.Sprintf("call-%d", i), tc.name, tc.raw, tc.raw)
			if err != nil {
				return err
			}
			count, err := tx.Count(t.Context(), "creative_message_content_refs", "message_id=$2", m.ID)
			if err == nil && int(count) != tc.count {
				t.Errorf("%s refs %d want %d", tc.name, count, tc.count)
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	result, err := h.listConversationAssets(t.Context(), `{"before_ordinal":0,"limit":20,"source":"assistant"}`)
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Items []conversationAsset `json:"items"`
	}
	if err := json.Unmarshal([]byte(result), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Items) != 3 {
		t.Fatalf("results %s", result)
	}
	if _, err := h.editCanvas(t.Context(), f.canvasID, "conversation-placement", fmt.Sprintf(`{"actions":[{"type":"add_node","key":"a","node_type":"text","content_revision_id":%q}]}`, ids[0])); err != nil {
		t.Fatal(err)
	}
}

func TestLibraryToolExposureAndPolicyUpgrade(t *testing.T) {
	for _, mode := range []string{ScopeSelectedRevisions, ScopeAccountLibrary} {
		t.Run(mode, func(t *testing.T) {
			f := setup(t)
			f.queue()
			c := f.conversation(f.alice, f.canvasID)
			consent := contextConsent(t, f, c.ID)
			if mode == ScopeAccountLibrary {
				consent = libraryConsent(t, f, c.ID)
			}
			h := lookupHandler(t, f, c.ID, consent)
			if _, _, err := h.configure(t.Context()); err != nil {
				t.Fatal(err)
			}
			if h.allowed(searchAssetsTool) != (mode == ScopeAccountLibrary) || h.allowed(readLibraryTool) != (mode == ScopeAccountLibrary) {
				t.Fatal("tool visibility violates mode")
			}
			if mode == ScopeSelectedRevisions {
				if _, err := h.searchAssets(t.Context(), `{"limit":1}`); !errors.Is(err, ErrEgressRequired) {
					t.Fatalf("legacy direct invocation: %v", err)
				}
			}
			if _, err := f.db.Exec(`UPDATE creative_egress_consents SET policy_version='egress-2026-10-1' WHERE id=$1`, consent.ID); err != nil {
				t.Fatal(err)
			}
			if err := h.requireLibrary(t.Context(), true); !errors.Is(err, ErrEgressRequired) {
				t.Fatalf("old policy accepted: %v", err)
			}
		})
	}
}

func TestAssistantFindsReadsPlacesAndSavesLibraryContent(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	body := "逆光创作参考"
	receipt, err := creativelibrary.CreateAsset(t.Context(), f.alice, command(t, creativelibrary.CreateAssetInput{Title: "逆光", Content: creativecontent.Draft{Kind: "text", Payload: creativecontent.Payload{Body: &body}}}))
	if err != nil {
		t.Fatal(err)
	}
	var asset creativelibrary.AssetResult
	if err := json.Unmarshal(receipt.Outcome.Response, &asset); err != nil {
		t.Fatal(err)
	}
	var node string
	f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, n int) (llmgateway.Result, error) {
		switch n {
		case 1:
			if !offered(r, searchAssetsTool) || !offered(r, conversationAssetsTool) || !offered(r, saveLibraryTool) {
				t.Fatal("missing asset tools")
			}
			return scriptedResult(r, call("search", 0, searchAssetsTool, `{"query":"逆光","limit":5}`)), nil
		case 2:
			if !strings.Contains(toolResult(r, 0), asset.ID) {
				t.Fatalf("search result %s", toolResult(r, 0))
			}
			return scriptedResult(r, call("read", 0, readAssetTool, fmt.Sprintf(`{"content_revision_id":%q,"offset":0,"limit":1000}`, asset.ContentRevisionID))), nil
		case 3:
			if !strings.Contains(toolResult(r, 0), body) {
				t.Fatal("content not delivered")
			}
			return scriptedResult(r, call("place", 0, editCanvasTool, fmt.Sprintf(`{"actions":[{"type":"add_node","key":"ref","node_type":"text","asset_id":%q,"content_revision_id":%q}]}`, asset.ID, asset.ContentRevisionID))), nil
		case 4:
			var placed struct {
				Created map[string]string `json:"created"`
			}
			if err := json.Unmarshal([]byte(toolResult(r, 0)), &placed); err != nil {
				t.Fatal(err)
			}
			node = placed.Created["ref"]
			return scriptedResult(r, call("save", 0, saveLibraryTool, fmt.Sprintf(`{"node_id":%q,"title":"参考"}`, node))), nil
		case 5:
			if !strings.Contains(toolResult(r, 0), `"reused":true`) {
				t.Fatalf("save %s", toolResult(r, 0))
			}
		}
		return scriptedResult(r), nil
	}
	consent := libraryConsent(t, f, c.ID)
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "找逆光参考并放上画布"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	got, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
	if err != nil || got.State != RunSucceeded || node == "" {
		t.Fatalf("run %+v %v", got, err)
	}
	if f.count("creative_message_content_refs", "content_revision_id=$1 AND role='result'", asset.ContentRevisionID) != 1 {
		t.Fatal("read lost conversation retention")
	}
}

func TestRevokedTextCannotLeaveThroughCanvasPreview(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	initial := &runtimeHandler{service: f.service, scope: f.alice}
	secret := "不应再次外发的画布正文"
	if _, err := initial.editCanvas(t.Context(), f.canvasID, "secret-node", fmt.Sprintf(`{"actions":[{"type":"add_node","key":"secret","node_type":"text","text":%q}]}`, secret)); err != nil {
		t.Fatal(err)
	}
	canvas, err := creativecanvas.GetCanvas(t.Context(), f.alice, f.canvasID)
	if err != nil {
		t.Fatal(err)
	}
	id := *canvas.Nodes[0].ContentRevisionID
	previous := contextConsent(t, f, c.ID, id)
	if _, err := f.service.RevokeConsent(t.Context(), f.alice, command(t, map[string]any{"consent_id": previous.ID, "expected_revision": previous.Revision})); err != nil {
		t.Fatal(err)
	}
	h := lookupHandler(t, f, c.ID, contextConsent(t, f, c.ID))
	result, err := h.readCanvas(t.Context(), `{"offset":0,"limit":10}`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result, secret) {
		t.Fatal("revoked text leaked through read_canvas")
	}
}

func TestPagedReadResultCannotReenterRevokedHistory(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	secret := "不能再次外发的分页正文"
	id := f.revision(f.alice, strings.Repeat(secret, 500))
	previous := contextConsent(t, f, c.ID, id)
	f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, n int) (llmgateway.Result, error) {
		switch n {
		case 1:
			return scriptedResult(r, call("read", 0, readAssetTool, fmt.Sprintf(`{"content_revision_id":%q,"offset":0,"limit":8192}`, id))), nil
		case 2:
			var result struct {
				ItemID string `json:"item_id"`
			}
			if err := json.Unmarshal([]byte(toolResult(r, 0)), &result); err != nil || result.ItemID == "" {
				t.Fatalf("large result locator: %s %v", toolResult(r, 0), err)
			}
			return scriptedResult(r, call("page", 0, readResultTool, fmt.Sprintf(`{"item_id":%q,"offset":0,"limit":8192}`, result.ItemID))), nil
		case 3:
			if !strings.Contains(toolResult(r, 0), secret) {
				t.Fatal("page did not contain source text")
			}
		default:
			for _, m := range r.Chat.Messages {
				for _, b := range m.Blocks {
					if strings.Contains(b.Text, secret) {
						t.Fatal("revoked page body reached next run")
					}
				}
			}
			if n == 4 {
				return scriptedResult(r, call("history", 0, readHistoryTool, `{"before_ordinal":0,"limit":20}`)), nil
			}
		}
		return scriptedResult(r), nil
	}
	run, err := f.startRun(f.alice, c.ID, previous.ID, InstructionSegment{Type: "text", Text: "读取参考"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.RevokeConsent(t.Context(), f.alice, command(t, map[string]any{"consent_id": previous.ID, "expected_revision": previous.Revision})); err != nil {
		t.Fatal(err)
	}
	consent := contextConsent(t, f, c.ID)
	run, err = f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "继续讨论"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	got, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
	if err != nil || got.State != RunSucceeded {
		t.Fatalf("run %+v %v", got, err)
	}
}

func TestLargeCanvasReadRetainsAllSourcesWithinMessageBudget(t *testing.T) {
	f := setup(t)
	f.queue()
	req := skillRequest("platform", "canvas-reader", "画布阅读")
	req.Activate = true
	req.Manifest.ToolAllowlist = []string{"read_canvas@1"}
	v := f.runImport(f.platform, req)
	initial := &runtimeHandler{service: f.service, scope: f.alice}
	for i := range 50 {
		input := fmt.Sprintf(`{"actions":[{"type":"add_node","key":"text","node_type":"text","text":%q}]}`, strings.Repeat("文字", 100)+strconv.Itoa(i))
		if _, err := initial.editCanvas(t.Context(), f.canvasID, fmt.Sprintf("text-%d", i), input); err != nil {
			t.Fatal(err)
		}
	}
	c := f.conversation(f.alice, f.canvasID)
	consent := contextConsent(t, f, c.ID)
	f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, n int) (llmgateway.Result, error) {
		if n == 1 {
			return scriptedResult(r, call("canvas", 0, readCanvasTool, `{"offset":0,"limit":50}`)), nil
		}
		return scriptedResult(r), nil
	}
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "读画布"}, InstructionSegment{Type: "skill_ref", SkillID: v.SkillID, SkillVersionID: v.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	got, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
	if err != nil || got.State != RunSucceeded {
		t.Fatalf("run %+v %v", got, err)
	}
	if n := f.count("creative_message_content_refs", "role='result'"); n != 50 {
		t.Fatalf("retained sources = %d, want 50", n)
	}
}

func TestSearchTaggedImageReturnsGroupNames(t *testing.T) {
	f := setup(t)
	f.queue()
	receipt, err := creativelibrary.CreateGroup(t.Context(), f.alice, command(t, creativelibrary.GroupInput{Name: "角色参考", HierarchyRevision: 1}))
	if err != nil {
		t.Fatal(err)
	}
	var group creativelibrary.LibraryResult
	if err := json.Unmarshal(receipt.Outcome.Response, &group); err != nil {
		t.Fatal(err)
	}
	receipt, err = creativelibrary.CreateTag(t.Context(), f.alice, command(t, creativelibrary.TagInput{Name: "角色", Color: "#123456", HierarchyRevision: group.HierarchyRevision}))
	if err != nil {
		t.Fatal(err)
	}
	var tag creativelibrary.LibraryResult
	if err := json.Unmarshal(receipt.Outcome.Response, &tag); err != nil {
		t.Fatal(err)
	}
	id := f.mediaRevision(f.alice, "image", "image/png")
	var assetID string
	if err := f.db.QueryRow(`SELECT id FROM creative_assets WHERE account_id=$1 AND content_revision_id=$2`, f.alice.AccountID(), id).Scan(&assetID); err != nil {
		t.Fatal(err)
	}
	if _, err := creativelibrary.Organize(t.Context(), f.alice, command(t, creativelibrary.OrganizeInput{Assets: []creativelibrary.AssetVersion{{ID: assetID, Revision: 1}}, AddTags: []string{tag.ID}})); err != nil {
		t.Fatal(err)
	}
	c := f.conversation(f.alice, f.canvasID)
	h := lookupHandler(t, f, c.ID, libraryConsent(t, f, c.ID))
	query := fmt.Sprintf(`{"kind":"image","limit":20,"tag_ids":[%q]}`, tag.ID)
	for _, grouped := range []bool{false, true} {
		t.Run(fmt.Sprintf("grouped=%t", grouped), func(t *testing.T) {
			if grouped {
				if _, err := creativelibrary.Organize(t.Context(), f.alice, command(t, creativelibrary.OrganizeInput{Assets: []creativelibrary.AssetVersion{{ID: assetID, Revision: 2}}, AddGroups: []string{group.ID}})); err != nil {
					t.Fatal(err)
				}
			}
			result, err := h.searchAssets(t.Context(), query)
			if err != nil {
				t.Fatal(err)
			}
			var page struct {
				Items []struct {
					ID     string        `json:"asset_id"`
					Tags   []libraryName `json:"tags"`
					Groups []libraryName `json:"groups"`
				} `json:"items"`
			}
			if err := json.Unmarshal([]byte(result), &page); err != nil {
				t.Fatal(err)
			}
			if len(page.Items) != 1 || page.Items[0].ID != assetID || len(page.Items[0].Tags) != 1 || page.Items[0].Tags[0] != (libraryName{ID: tag.ID, Name: "角色"}) {
				t.Fatalf("unexpected tagged image: %s", result)
			}
			groups := page.Items[0].Groups
			if !grouped && len(groups) != 0 || grouped && (len(groups) != 1 || groups[0] != (libraryName{ID: group.ID, Name: "角色参考"})) {
				t.Fatalf("unexpected groups: %s", result)
			}
		})
	}
}
