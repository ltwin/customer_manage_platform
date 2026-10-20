package creativeagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"

	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativeglb"
	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
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
	if _, err := h.searchAssets(t.Context(), `{"view":"trash","limit":10}`); err != nil {
		t.Fatal("trash metadata refused", err)
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
	held, err := f.service.claimRun(t.Context(), f.alice, h.held.RunID)
	if err != nil {
		t.Fatal(err)
	}
	h.held = held
	for range 2 {
		result, err := h.runLibraryWrite(withToolStep(t.Context(), "save-step"), saveLibraryTool, fmt.Sprintf(`{"node_id":%q,"title":"已收藏","description":"素材描述"}`, canvas.Nodes[0].ID))
		if err != nil {
			t.Fatal(err)
		}
		var out struct{ Items []map[string]any }
		if err := json.Unmarshal([]byte(result), &out); err != nil {
			t.Fatal(err)
		}
		if len(out.Items) != 1 || out.Items[0]["asset_id"] != asset.ID || out.Items[0]["reused"] != true {
			t.Fatalf("save %+v", out)
		}
	}
	if _, err := h.prepareLibrarySave(t.Context(), fmt.Sprintf(`{"node_id":%q,"title":"新标签","new_tags":[]}`, canvas.Nodes[0].ID)); err == nil {
		t.Fatal("missing description accepted")
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
			return scriptedResult(r, call("save", 0, saveLibraryTool, fmt.Sprintf(`{"node_id":%q,"title":"参考","description":"素材参考"}`, node))), nil
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

// modelAsset files a complete model into the library, as a save from a scene
// does: the GLB, its thumbnail and the generator it was baked with.
func (f *fixture) modelAsset() (assetID, revisionID string) {
	f.t.Helper()
	glb, thumb := "ccbl_"+uuid.NewString(), "ccbl_"+uuid.NewString()
	if _, err := f.db.Exec(`INSERT INTO creative_blobs(id,account_id,storage_driver,object_key,object_version,sha256,byte_size,mime,state,verified_at) VALUES
 ($1,$3,'local','blobs/'||$1,'v1','sha256-'||repeat('1',64),100,'model/gltf-binary','ready',clock_timestamp()),
 ($2,$3,'local','blobs/'||$2,'v1','sha256-'||repeat('2',64),50,'image/png','ready',clock_timestamp())`, glb, thumb, f.alice.AccountID()); err != nil {
		f.t.Fatal(err)
	}
	var generator map[string]any
	if err := json.Unmarshal(testPillarModule, &generator); err != nil {
		f.t.Fatal(err)
	}
	generator["language"], generator["contract_version"], generator["lib_version"], generator["code"], generator["code_sha256"] = "three-module", 1, 1, testPillarCode, "sha256-"+strings.Repeat("c", 64)
	payload, err := json.Marshal(map[string]any{"format": "glb", "manifest": testManifest(f.t), "generator": generator, "name_hint": "哥特束柱"})
	if err != nil {
		f.t.Fatal(err)
	}
	err = f.alice.WithTxScope(f.t.Context(), func(tx store.TxAccountScope) error {
		r, err := creativecontent.WriteModelAndRetain(f.t.Context(), tx, creativecontent.Draft{Kind: "model", Payload: creativecontent.Payload{Model: payload}}, "",
			[]creativecontent.ObjectBinding{{Role: "original", BlobID: glb}, {Role: "thumbnail", BlobID: thumb}},
			func(r creativecontent.Revision) error {
				saved, err := creativelibrary.SaveRevisionInTx(f.t.Context(), tx, r, creativelibrary.ImportTarget{Title: "哥特束柱"})
				assetID = saved.ID
				return err
			})
		revisionID = r.ID
		return err
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return assetID, revisionID
}

// The assistant finds a model by kind with enough to choose it, reads its
// facts and thumbnail, and places two instances of the same revision.
func TestAssistantFindsReadsAndPlacesALibraryModel(t *testing.T) {
	f, c, _, doc := visualScene(t, true)
	consent := libraryConsent(t, f, c.ID)
	assetID, rev := f.modelAsset()
	instance := func(n int) map[string]any {
		return map[string]any{"id": fmt.Sprintf("sobj_L%020d", n), "kind": "model_instance", "name": "束柱", "parent_id": nil, "order": n,
			"transform": identityAt(float64(n)), "visible": true, "locked": false, "model": map[string]any{"content_revision_id": rev}}
	}
	s := &script{turns: []func(llmgateway.ProviderRequest) llmgateway.Result{
		func(r llmgateway.ProviderRequest) llmgateway.Result {
			return scriptedResult(r, call("search", 0, searchAssetsTool, `{"kind":"model","limit":20}`))
		},
		func(r llmgateway.ProviderRequest) llmgateway.Result {
			return scriptedResult(r, call("read", 0, readAssetTool, fmt.Sprintf(`{"content_revision_id":%q,"offset":0,"limit":1}`, rev)))
		},
		func(r llmgateway.ProviderRequest) llmgateway.Result {
			return scriptedResult(r, sceneCall("place", editSceneTool, map[string]any{"document_id": doc, "based_on_revision": 1,
				"operations": []any{map[string]any{"op": "add_objects", "objects": []any{instance(1), instance(2)}}}}))
		},
	}}
	f.vendor.script = s.answer
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "把哥特束柱放到门两边"})
	if err != nil {
		t.Fatal(err)
	}
	if got := f.resume(run.ID); got.State != RunSucceeded {
		t.Fatalf("run %s %s", got.State, got.ErrorCode)
	}

	var page struct {
		Items []map[string]json.RawMessage `json:"items"`
	}
	found := toolResult(s.request(2), 0)
	if err := json.Unmarshal([]byte(found), &page); err != nil || len(page.Items) != 1 {
		t.Fatalf("search %s %v", found, err)
	}
	item := page.Items[0]
	var brief map[string]any
	if err := json.Unmarshal(item["model"], &brief); err != nil {
		t.Fatalf("search item has no model brief: %s", found)
	}
	if string(item["asset_id"]) != strconv.Quote(assetID) || string(item["content_revision_id"]) != strconv.Quote(rev) || item["width"] != nil || item["height"] != nil {
		t.Fatalf("search item %s", found)
	}
	if brief["name"] != "哥特束柱" || brief["mount"] != "floor" || brief["size_m"] == nil || brief["triangles"] == nil || brief["slots"] == nil || brief["parts"] != nil || brief["anchors"] != nil ||
		fmt.Sprint(brief["params"]) != "[height]" || strings.Contains(found, "export const params") {
		t.Fatalf("model brief %s", item["model"])
	}

	var read struct {
		Model            creativecontent.ModelFacts `json:"model"`
		ImageRevisionID  string                     `json:"image_revision_id"`
		ThumbnailOmitted string                     `json:"thumbnail_omitted"`
	}
	result := toolResult(s.request(3), 0)
	if err := json.Unmarshal([]byte(result), &read); err != nil || read.ImageRevisionID != rev || read.ThumbnailOmitted != "" || strings.Contains(result, "export const params") {
		t.Fatalf("read_asset %s %v", result, err)
	}
	if len(read.Model.ParamsSchema) != 1 || read.Model.Params["height"] != 2.0 || read.Model.Name != "哥特束柱" {
		t.Fatalf("model facts %s", result)
	}
	if ok, _ := hasImage(s.request(3)); !ok {
		t.Fatal("thumbnail not sent with the read")
	}
	var baked creativescene.BakedModel
	if err := f.alice.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		var err error
		baked, err = creativescene.BakedModelInTx(t.Context(), tx, rev)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(baked, read.Model.ManifestFacts) {
		t.Fatalf("read_asset %+v and write_model %+v disagree", read.Model, baked)
	}

	placed := 0
	for _, o := range f.objectsOf(doc) {
		if o.Kind == creativescene.KindModelInstance && o.Model != nil && o.Model.ContentRevisionID == rev {
			placed++
		}
	}
	if placed != 2 {
		t.Fatalf("placed %d instances of the library model: %+v", placed, f.objectsOf(doc))
	}
}

// Without a thumbnail the facts still come, with the reason; without media
// metadata the read is refused.
func TestReadingAModelWithoutItsThumbnail(t *testing.T) {
	grant := func(t *testing.T, f *fixture, c string, classes ...string) Consent {
		t.Helper()
		receipt, err := f.service.GrantConsent(t.Context(), f.alice, command(t, map[string]any{"conversation_id": c, "purpose": "creative_assistance", "mode": ScopeAccountLibrary, "data_classes": classes}))
		if err != nil {
			t.Fatal(err)
		}
		var consent Consent
		if err := json.Unmarshal(receipt.Outcome.Response, &consent); err != nil {
			t.Fatal(err)
		}
		return consent
	}
	read := func(t *testing.T, h *runtimeHandler, rev string) map[string]any {
		t.Helper()
		out, err := h.readAsset(t.Context(), fmt.Sprintf(`{"content_revision_id":%q,"offset":0,"limit":1}`, rev))
		if err != nil {
			t.Fatal(err)
		}
		var result map[string]any
		if err := json.Unmarshal([]byte(out), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}

	t.Run("model cannot see images", func(t *testing.T) {
		f := setup(t)
		f.queue()
		_, rev := f.modelAsset()
		c := f.conversation(f.alice, f.canvasID)
		got := read(t, lookupHandler(t, f, c.ID, libraryConsent(t, f, c.ID)), rev)
		if got["thumbnail_omitted"] != "model_cannot_see_images" || got["image_revision_id"] != nil || got["model"] == nil {
			t.Fatalf("read %v", got)
		}
	})
	t.Run("image preview not allowed", func(t *testing.T) {
		f, c, _, _ := visualScene(t, true)
		_, rev := f.modelAsset()
		got := read(t, lookupHandler(t, f, c.ID, grant(t, f, c.ID, "text", "canvas_metadata", "media_metadata")), rev)
		if got["thumbnail_omitted"] != "image_preview_not_allowed" || got["image_revision_id"] != nil || got["model"] == nil {
			t.Fatalf("read %v", got)
		}
	})
	t.Run("no media metadata", func(t *testing.T) {
		f, c, _, _ := visualScene(t, true)
		_, rev := f.modelAsset()
		h := lookupHandler(t, f, c.ID, grant(t, f, c.ID, "text", "image_preview", "canvas_metadata"))
		if _, err := h.readAsset(t.Context(), fmt.Sprintf(`{"content_revision_id":%q,"offset":0,"limit":1}`, rev)); !errors.Is(err, ErrEgressRequired) {
			t.Fatalf("read without media metadata: %v", err)
		}
	})
}

// A thumbnail left out by the image window keeps the model's facts in the
// conversation; only the image goes, and thumbnail_omitted says why. A
// message that is the image alone is still replaced by its notice.
func TestModelFactsOutliveTheirThumbnail(t *testing.T) {
	call := &schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{
		{ID: "read", Function: schema.FunctionCall{Name: readAssetTool, Arguments: `{}`}},
		{ID: "render", Function: schema.FunctionCall{Name: renderViewTool, Arguments: `{}`}},
	}}
	read := schema.ToolMessage(`{"content_revision_id":"ccrv_m","image_revision_id":"ccrv_m","model":{"name":"柱","size_m":[0.5,2.4,0.5]},"reference":"<<<ccrv_m>>>"}`, "read")
	render := schema.ToolMessage(`{"content_revision_id":"ccrv_r","image_revision_id":"ccrv_r","scene_revision":3}`, "render")
	reference := schema.UserMessage(referenceLabel("ccrv_m") + "\n" + `{"model":{"name":"柱"}}`)
	reference.Extra = map[string]any{imageRefsKey: "ccrv_m"}
	historyTag(reference, "old")
	image := schema.UserMessage(referenceLabel("ccrv_i"))
	image.Extra = map[string]any{imageRefsKey: "ccrv_i"}
	historyTag(image, "old")

	limitHistoryImages([]*schema.Message{image, reference, call, read, render}, 1)
	if messageImageID(render) != "ccrv_r" {
		t.Fatal("the newest image was dropped")
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(read.Content), &got); err != nil || messageImageID(read) != "" ||
		got["thumbnail_omitted"] != "context_limit" || got["model"].(map[string]any)["name"] != "柱" {
		t.Fatalf("read_asset result %s %v", read.Content, err)
	}
	if messageImageID(reference) != "" || !strings.HasPrefix(reference.Content, referenceLabel("ccrv_m")+"\n") ||
		!strings.Contains(reference.Content, `"name":"柱"`) || !strings.Contains(reference.Content, `"thumbnail_omitted":"context_limit"`) {
		t.Fatalf("reference %s", reference.Content)
	}
	if messageImageID(image) != "" || !strings.Contains(image.Content, "未附加图片") {
		t.Fatalf("image %s", image.Content)
	}
	for _, content := range []string{referenceLabel("ccrv_i"), render.Content} {
		if _, ok := withoutThumbnail(content, "context_limit"); ok {
			t.Fatalf("%s read as a model", content)
		}
	}
}

// Facts too large for one result shed parts and anchors, then the parameter
// definitions, and name what they left out; past even that the read is
// refused instead of ending the run.
func TestModelFactsFitOneResult(t *testing.T) {
	small := creativecontent.ModelFacts{Mount: "floor", ManifestFacts: creativecontent.ManifestFacts{Parts: []string{"base"}, Anchors: []creativeglb.Anchor{{ID: "top"}}},
		ParamsSchema: map[string]json.RawMessage{"height": json.RawMessage(`{"type":"number","min":1,"max":3,"default":2}`)}}
	large := small
	large.Parts, large.Anchors = make([]string, 200), make([]creativeglb.Anchor, 200)
	for i := range 200 {
		large.Parts[i] = fmt.Sprintf("%064d", i)
		large.Anchors[i] = creativeglb.Anchor{ID: fmt.Sprintf("%064d", i), Position: [3]float64{1.0 / 3, 2.0 / 3, 1.0 / 7}}
	}
	options := make([]string, 32)
	for i := range options {
		options[i] = fmt.Sprintf("%s%02d", strings.Repeat("选", 62), i)
	}
	enum, err := json.Marshal(map[string]any{"type": "enum", "options": options, "default": options[0]})
	if err != nil {
		t.Fatal(err)
	}
	wide := large
	wide.ParamsSchema = map[string]json.RawMessage{}
	for i := range 32 {
		wide.ParamsSchema["p"+strconv.Itoa(i)] = enum
	}
	for _, tt := range []struct {
		name    string
		facts   creativecontent.ModelFacts
		limit   int
		omitted []string
	}{
		{"whole", small, 64 << 10, nil},
		{"without parts and anchors", large, 16 << 10, []string{"parts", "anchors"}},
		{"without definitions", wide, 64 << 10, []string{"parts", "anchors", "params_schema"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			result := map[string]any{"content_revision_id": "ccrv_m"}
			if err := fitModelFacts(result, tt.facts, tt.limit); err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			got := result["model"].(creativecontent.ModelFacts)
			if len(raw) > tt.limit || !reflect.DeepEqual(got.Omitted, tt.omitted) || got.Mount != "floor" {
				t.Fatalf("%d bytes, omitted %v", len(raw), got.Omitted)
			}
		})
	}
	var refusal *toolRefusal
	if err := fitModelFacts(map[string]any{}, small, 64); !errors.As(err, &refusal) {
		t.Fatalf("too small a limit: %v", err)
	}
}

// A model read in an earlier turn whose thumbnail can no longer be read: the
// later turn still gets the model's facts, without the image.
func TestEarlierModelReadWithAnUnreadableThumbnail(t *testing.T) {
	f, c, _, _ := visualScene(t, true)
	consent := libraryConsent(t, f, c.ID)
	_, rev := f.modelAsset()
	f.vendor.script = (&script{turns: []func(llmgateway.ProviderRequest) llmgateway.Result{
		func(r llmgateway.ProviderRequest) llmgateway.Result {
			return scriptedResult(r, call("read", 0, readAssetTool, fmt.Sprintf(`{"content_revision_id":%q,"offset":0,"limit":1}`, rev)))
		},
	}}).answer
	first, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "看看哥特束柱"})
	if err != nil {
		t.Fatal(err)
	}
	if got := f.resume(first.ID); got.State != RunSucceeded {
		t.Fatalf("first run %s %s", got.State, got.ErrorCode)
	}

	f.service.SetImageReader(&unreadableImages{})
	var prompt string
	f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, _ int) (llmgateway.Result, error) {
		prompt = ""
		for _, m := range r.Chat.Messages {
			for _, b := range m.Blocks {
				if b.Image != nil {
					t.Error("an unreadable thumbnail was attached")
				}
				prompt += b.Text
			}
		}
		return scriptedResult(r), nil
	}
	second, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "它多高"})
	if err != nil {
		t.Fatal(err)
	}
	if got := f.resume(second.ID); got.State != RunSucceeded {
		t.Fatalf("second run %s %s", got.State, got.ErrorCode)
	}
	if !strings.Contains(prompt, `"thumbnail_omitted":"thumbnail_unreadable"`) || !strings.Contains(prompt, `"size_m"`) || strings.Contains(prompt, "已无法读取") {
		t.Fatalf("the earlier read lost the model's facts: %s", prompt)
	}
}

func TestToolsSayHowToFindAndPlaceModels(t *testing.T) {
	search := assetToolInfo(searchAssetsTool)
	js, err := search.ToJSONSchema()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(js.Properties.Value("kind"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"model"`) || strings.Contains(string(raw), `"scene"`) || !strings.Contains(search.Desc, "read_asset") || !strings.Contains(search.Desc, "edit_scene") {
		t.Fatalf("search_assets %s / %s", search.Desc, raw)
	}
	edit, err := (&sceneTool{name: editSceneTool}).Info(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(edit.Desc, "kind:model") || !strings.Contains(edit.Desc, "model{content_revision_id}，可放多个实例") {
		t.Fatal("edit_scene does not say how to place a library model")
	}
}
