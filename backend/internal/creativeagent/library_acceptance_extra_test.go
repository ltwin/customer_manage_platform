package creativeagent

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
)

func TestLibraryBatchLimitsRejectBeforeEffects(t *testing.T) {
	f, h, a := organizeFixture(t)
	before := f.count("creative_operation_receipts", `account_id='agent-a'`)
	items := make([]map[string]any, 51)
	for i := range items {
		items[i] = map[string]any{"asset_id": fmt.Sprintf("asset-limit-%d", i), "expected_revision": "1", "title": "new"}
	}
	actions := make([]map[string]any, 21)
	for i := range actions {
		actions[i] = map[string]any{"action": "create_group", "name": fmt.Sprint(i)}
	}
	for _, tc := range []struct {
		tool    string
		payload any
	}{{organizeAssetsTool, map[string]any{"items": items}}, {manageLibraryTool, map[string]any{"actions": actions}}} {
		raw, err := json.Marshal(tc.payload)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = h.runLibraryWrite(withToolStep(t.Context(), organizeStep), tc.tool, string(raw)); err == nil {
			t.Fatal("oversized batch accepted", tc.tool)
		}
	}
	if f.count("creative_agent_library_calls", `account_id='agent-a'`) != 0 || f.count("creative_operation_receipts", `account_id='agent-a'`) != before {
		t.Fatal("rejected request wrote effects")
	}
	asset, err := creativelibrary.GetAsset(t.Context(), f.alice, a.ID)
	if err != nil || asset.Revision != 1 {
		t.Fatal(asset, err)
	}
}

func TestLibrarySaveFreezesContentAndReportsActualTagFacts(t *testing.T) {
	f, h, _ := organizeFixture(t)
	edit, err := h.editCanvas(t.Context(), f.canvasID, "save-fixture", `{"actions":[{"type":"add_node","key":"ref","node_type":"text","text":"original","title":"reference"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	var nodes struct {
		Created map[string]string `json:"created"`
	}
	encoded, err := json.Marshal(edit)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(encoded, &nodes); err != nil {
		t.Fatal(err)
	}
	node := nodes.Created["ref"]
	raw := fmt.Sprintf(`{"node_id":%q,"title":"planned","description":"planned description"}`, node)
	interrupted := withToolStep(t.Context(), "save-interrupted")
	if _, err = h.startLibraryCall(interrupted, saveLibraryTool, raw); err != nil {
		t.Fatal(err)
	}
	if _, err = h.editCanvas(t.Context(), f.canvasID, "changed-node", fmt.Sprintf(`{"actions":[{"type":"set_text","node_id":%q,"text":"new content"}]}`, node)); err != nil {
		t.Fatal(err)
	}
	before := f.count("creative_assets", `account_id='agent-a'`)
	out, err := h.runLibraryWrite(interrupted, saveLibraryTool, raw)
	if err != nil || !strings.Contains(out, `"status":"conflict"`) {
		t.Fatal(out, err)
	}
	if f.count("creative_assets", `account_id='agent-a'`) != before {
		t.Fatal("resumed save used changed content")
	}
	settings, err := creativelibrary.GetSettings(t.Context(), f.alice)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = creativelibrary.CreateTag(t.Context(), f.alice, command(t, creativelibrary.TagInput{Name: "existing", Color: "#112233", HierarchyRevision: settings.HierarchyRevision})); err != nil {
		t.Fatal(err)
	}
	raw = fmt.Sprintf(`{"node_id":%q,"title":"actual","description":"actual description","new_tags":[{"name":"existing"},{"name":"created"}]}`, node)
	out, err = h.runLibraryWrite(withToolStep(t.Context(), "save-current"), saveLibraryTool, raw)
	if err != nil {
		t.Fatal(err)
	}
	var summary struct {
		ID    string `json:"result_id"`
		Items []struct {
			AssetID string `json:"asset_id"`
		}
	}
	if err = json.Unmarshal([]byte(out), &summary); err != nil {
		t.Fatal(err)
	}
	if len(summary.Items) != 1 || summary.Items[0].AssetID == "" {
		t.Fatal(out)
	}
	detail, err := h.readLibraryResult(t.Context(), fmt.Sprintf(`{"result_id":%q}`, summary.ID))
	if err != nil {
		t.Fatal(err)
	}
	var page struct {
		Items []struct {
			Type  string          `json:"type"`
			Value json.RawMessage `json:"value"`
		}
	}
	if err = json.Unmarshal([]byte(detail), &page); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range page.Items {
		if item.Type == "action_result" {
			var facts creativelibrary.AssetResult
			if err = json.Unmarshal(item.Value, &facts); err != nil {
				t.Fatal(err)
			}
			if len(facts.TagsCreated) != 1 || facts.TagsCreated[0].Name != "created" || len(facts.TagsReused) != 1 || facts.TagsReused[0].Name != "existing" || facts.TagsReused[0].Color != "#112233" {
				t.Fatalf("inaccurate tag facts: %+v", facts)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("missing save facts")
	}
	_, err = h.runLibraryWrite(withToolStep(t.Context(), "save-reuse"), saveLibraryTool, fmt.Sprintf(`{"node_id":%q,"title":"must not replace","description":"must not replace"}`, node))
	if err != nil {
		t.Fatal(err)
	}
	asset, err := creativelibrary.GetAsset(t.Context(), f.alice, summary.Items[0].AssetID)
	if err != nil || asset.Title != "actual" || asset.Description != "actual description" {
		t.Fatal("reuse replaced text", asset, err)
	}
	if f.count("creative_assets", `account_id='agent-a'`) != before+1 {
		t.Fatal("reuse created a duplicate")
	}
}
