package creativeagent

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
)

func TestConversationAssetFiltersPrecedeDeduplication(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	ids := []string{f.revision(f.alice, "素材一"), f.revision(f.alice, "素材二"), f.revision(f.alice, "素材三")}
	segments := []InstructionSegment{{Type: "text", Text: "红裙子的参考"}}
	for _, id := range ids {
		segments = append(segments, InstructionSegment{Type: "content_ref", ContentRevisionID: id})
	}
	f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, n int) (llmgateway.Result, error) {
		if n == 1 {
			calls := []llmgateway.ToolCall{}
			for i, id := range ids {
				calls = append(calls, call(fmt.Sprintf("read-%d", i), i, readAssetTool, fmt.Sprintf(`{"content_revision_id":%q,"offset":0,"limit":100}`, id)))
			}
			return scriptedResult(r, calls...), nil
		}
		return scriptedResult(r), nil
	}
	consent := contextConsent(t, f, c.ID, ids...)
	run, err := f.startRun(f.alice, c.ID, consent.ID, segments...)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	got, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
	if err != nil || got.State != RunSucceeded {
		t.Fatalf("read run: %+v %v", got, err)
	}
	h := lookupHandler(t, f, c.ID, contextConsent(t, f, c.ID))
	for _, tc := range []struct{ name, source, query, wantSource string }{
		{"latest", "", "", "assistant"},
		{"photographer", "photographer", "", "photographer"},
		{"keyword", "", "红裙子", "photographer"},
		{"combined", "photographer", "红裙子", "photographer"},
		{"assistant", "assistant", "", "assistant"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			seen := map[string]bool{}
			before := int64(0)
			beforeID := ""
			for page := 0; page < 4; page++ {
				raw := fmt.Sprintf(`{"limit":1,"before_ordinal":%d,"before_revision_id":%q,"source":%q,"query":%q}`, before, beforeID, tc.source, tc.query)
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
				if len(out.Items) > 1 {
					t.Fatal("page exceeds requested limit")
				}
				for _, item := range out.Items {
					if seen[item.RevisionID] || item.Source != tc.wantSource {
						t.Fatalf("duplicate/wrong source: %+v", item)
					}
					seen[item.RevisionID] = true
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
			for _, id := range ids {
				if !seen[id] {
					t.Errorf("missing %s after assistant read", id)
				}
			}
		})
	}
}

func TestLargeAssetSearchKeepsPaginationAndRunAlive(t *testing.T) {
	f := setup(t)
	f.queue()
	tags := []creativelibrary.NewTag{}
	for i := range 50 {
		tags = append(tags, creativelibrary.NewTag{ClientKey: strconv.Itoa(i), Name: fmt.Sprintf("标签%02d", i), Color: "#123456"})
	}
	ids := map[string]bool{}
	for i := range 21 {
		body := "参考正文"
		receipt, err := creativelibrary.CreateAsset(t.Context(), f.alice, command(t, creativelibrary.CreateAssetInput{Title: fmt.Sprintf("参考%02d", i), Content: creativecontent.Draft{Kind: "text", Payload: creativecontent.Payload{Body: &body}}, NewTags: tags}))
		if err != nil {
			t.Fatal(err)
		}
		var asset creativelibrary.AssetResult
		if err := json.Unmarshal(receipt.Outcome.Response, &asset); err != nil {
			t.Fatal(err)
		}
		ids[asset.ID] = false
	}
	c := f.conversation(f.alice, f.canvasID)
	consent := libraryConsent(t, f, c.ID)
	h := lookupHandler(t, f, c.ID, consent)
	cursor := ""
	for page := 0; page < 3; page++ {
		result, err := h.searchAssets(t.Context(), fmt.Sprintf(`{"limit":20,"cursor":%q}`, cursor))
		if err != nil {
			t.Fatal(err)
		}
		if len(result) > f.service.limits.ToolResultBytes {
			t.Errorf("result %d exceeds %d bytes", len(result), f.service.limits.ToolResultBytes)
		}
		var out struct {
			Items []struct {
				ID      string        `json:"asset_id"`
				Tags    []libraryName `json:"tags"`
				Omitted int           `json:"tags_omitted"`
			} `json:"items"`
			Cursor string `json:"next_cursor"`
			More   bool   `json:"has_more"`
			Total  int    `json:"total_count"`
		}
		if err := json.Unmarshal([]byte(result), &out); err != nil {
			t.Fatal(err)
		}
		if out.Total != 21 {
			t.Fatalf("total=%d", out.Total)
		}
		for _, item := range out.Items {
			seen, ok := ids[item.ID]
			if !ok || seen {
				t.Fatalf("duplicate/unknown asset %s", item.ID)
			}
			ids[item.ID] = true
			if len(item.Tags)+item.Omitted != 50 {
				t.Fatalf("unmarked omitted tags: %+v", item)
			}
		}
		if !out.More {
			break
		}
		if out.Cursor == "" || out.Cursor == cursor {
			t.Fatal("pagination did not advance")
		}
		cursor = out.Cursor
	}
	for id, seen := range ids {
		if !seen {
			t.Errorf("missing %s", id)
		}
	}
	req := skillRequest("platform", "large-library-search", "查找素材")
	req.Activate = true
	req.Manifest.ToolAllowlist = []string{"search_assets@1"}
	skill := f.runImport(f.platform, req)
	if err := f.work(f.alice, h.held.RunID); err != nil {
		t.Fatal(err)
	}
	baselineCalls := f.vendor.calls()
	f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, n int) (llmgateway.Result, error) {
		n -= baselineCalls
		if n == 1 {
			return scriptedResult(r, call("large", 0, searchAssetsTool, `{"limit":20}`)), nil
		}
		if n == 2 {
			return scriptedResult(r, call("narrow", 0, searchAssetsTool, `{"limit":20,"query":"参考00"}`)), nil
		}
		var result struct {
			Total int `json:"total_count"`
		}
		if err := json.Unmarshal([]byte(toolResult(r, 0)), &result); err != nil || result.Total != 1 {
			t.Fatalf("cannot narrow query: %s %v", toolResult(r, 0), err)
		}
		return scriptedResult(r), nil
	}
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "查找素材后缩小查询"}, InstructionSegment{Type: "skill_ref", SkillID: skill.SkillID, SkillVersionID: skill.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	got, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
	if err != nil || got.State != RunSucceeded || f.vendor.calls()-baselineCalls != 3 {
		t.Fatalf("run %+v calls=%d %v", got, f.vendor.calls()-baselineCalls, err)
	}
}

func TestConversationAssetSourceWithinSameMessage(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	id := f.revision(f.alice, "正文")
	appendLookupMessage(t, f, c.ID, []ContentRef{{RevisionID: id, Role: "input"}, {RevisionID: id, Role: "result"}}, "混合来源")
	h := lookupHandler(t, f, c.ID, contextConsent(t, f, c.ID))
	for _, source := range []string{"photographer", "assistant"} {
		t.Run(source, func(t *testing.T) {
			result, err := h.listConversationAssets(t.Context(), fmt.Sprintf(`{"limit":20,"source":%q}`, source))
			if err != nil {
				t.Fatal(err)
			}
			var out struct {
				Items []conversationAsset `json:"items"`
			}
			if err := json.Unmarshal([]byte(result), &out); err != nil {
				t.Fatal(err)
			}
			if len(out.Items) != 1 || out.Items[0].Source != source || out.Items[0].RevisionID != id {
				t.Fatalf("lost matching role: %s", result)
			}
		})
	}
}

func TestAssetSearchBudgetIncludesEscapedClassificationNames(t *testing.T) {
	f := setup(t)
	f.queue()
	groups := []string{}
	for i := range 100 {
		receipt, err := creativelibrary.CreateGroup(t.Context(), f.alice, command(t, creativelibrary.GroupInput{Name: strings.Repeat("<", 76) + fmt.Sprintf("%04d", i), HierarchyRevision: creativeops.Revision(i + 1)}))
		if err != nil {
			t.Fatal(err)
		}
		var group creativelibrary.LibraryResult
		if err := json.Unmarshal(receipt.Outcome.Response, &group); err != nil {
			t.Fatal(err)
		}
		groups = append(groups, group.ID)
	}
	tags := []creativelibrary.NewTag{}
	for i := range 50 {
		tags = append(tags, creativelibrary.NewTag{ClientKey: strconv.Itoa(i), Name: strings.Repeat("<", 76) + fmt.Sprintf("%04d", i), Color: "#123456"})
	}
	body := "正文"
	_, err := creativelibrary.CreateAsset(t.Context(), f.alice, command(t, creativelibrary.CreateAssetInput{Title: "大归类", Content: creativecontent.Draft{Kind: "text", Payload: creativecontent.Payload{Body: &body}}, GroupIDs: groups, NewTags: tags}))
	if err != nil {
		t.Fatal(err)
	}
	c := f.conversation(f.alice, f.canvasID)
	h := lookupHandler(t, f, c.ID, libraryConsent(t, f, c.ID))
	result, err := h.searchAssets(t.Context(), `{"limit":20}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) > f.service.limits.ToolResultBytes {
		t.Fatalf("encoded result %d exceeds limit", len(result))
	}
	var out struct {
		Items []struct {
			Tags          []libraryName `json:"tags"`
			Groups        []libraryName `json:"groups"`
			TagsOmitted   int           `json:"tags_omitted"`
			GroupsOmitted int           `json:"groups_omitted"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(result), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Items) != 1 {
		t.Fatalf("lost asset: %s", result)
	}
	item := out.Items[0]
	if item.TagsOmitted == 0 || item.GroupsOmitted == 0 || len(item.Tags)+item.TagsOmitted != 50 || len(item.Groups)+item.GroupsOmitted != 100 {
		t.Fatalf("incorrect omissions: %s", result)
	}
}
