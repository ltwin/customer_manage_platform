package creativeagent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
)

func organizeFixture(t *testing.T) (*fixture, *runtimeHandler, creativelibrary.AssetResult) {
	t.Helper()
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	h := lookupHandler(t, f, c.ID, libraryConsent(t, f, c.ID))
	held, err := f.service.claimRun(t.Context(), f.alice, h.held.RunID)
	if err != nil {
		t.Fatal(err)
	}
	h.held = held
	h.step = "organize-test-step"
	body := "原始正文"
	r, err := creativelibrary.CreateAsset(t.Context(), f.alice, command(t, creativelibrary.CreateAssetInput{Title: "原图", Content: creativecontent.Draft{Kind: "text", Payload: creativecontent.Payload{Body: &body}}}))
	if err != nil {
		t.Fatal(err)
	}
	var a creativelibrary.AssetResult
	if err = json.Unmarshal(r.Outcome.Response, &a); err != nil {
		t.Fatal(err)
	}
	return f, h, a
}
func TestOrganizeLibraryReplayAndFullDetails(t *testing.T) {
	f, h, a := organizeFixture(t)
	description := strings.Repeat("新", 1000)
	raw := fmt.Sprintf(`{"items":[{"asset_id":%q,"expected_revision":"1","title":"新标题","description":%q}]}`, a.ID, description)
	output, err := h.runLibraryWrite(t.Context(), organizeAssetsTool, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, `"status":"applied"`) {
		t.Fatal(output)
	}
	// Subsequent external mutation does not change original replay or before values.
	title := "摄影师随后修改"
	_, err = creativelibrary.Metadata(t.Context(), f.alice, command(t, creativelibrary.MetadataInput{ID: a.ID, ExpectedRevision: 2, Title: &title}))
	if err != nil {
		t.Fatal(err)
	}
	again, err := h.runLibraryWrite(t.Context(), organizeAssetsTool, raw)
	if err != nil {
		t.Fatal(err)
	}
	var first, second any
	_ = json.Unmarshal([]byte(output), &first)
	_ = json.Unmarshal([]byte(again), &second)
	one, _ := json.Marshal(first)
	two, _ := json.Marshal(second)
	if string(one) != string(two) {
		t.Fatal("replay changed")
	}
	c, err := h.loadLibraryCall(t.Context(), toolOperation(h.held.RunID+"/"+h.step+"/library-result"))
	if err != nil {
		t.Fatal(err)
	}
	details, err := h.readLibraryResult(t.Context(), fmt.Sprintf(`{"result_id":%q}`, c.ID))
	if err != nil || !strings.Contains(details, description) || !strings.Contains(details, "原图") {
		t.Fatalf("details %s %v", details, err)
	}
	asset, err := creativelibrary.GetAsset(t.Context(), f.alice, a.ID)
	if err != nil || asset.Title != title || asset.Revision != 3 {
		t.Fatalf("replay wrote %+v %v", asset, err)
	}
}
func TestLibraryUnknownFencesAndQueryOnlySettles(t *testing.T) {
	f, h, a := organizeFixture(t)
	raw := fmt.Sprintf(`{"items":[{"asset_id":%q,"expected_revision":"1","title":"计划标题"}]}`, a.ID)
	c, err := h.startLibraryCall(t.Context(), organizeAssetsTool, raw)
	if err != nil {
		t.Fatal(err)
	}
	c, err = h.freezeLibraryCommand(t.Context(), c, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	command := *c.Progress[0][0].Command
	command.Guard = h.libraryGuard(c.ID, 0, 0)
	unknown, err := h.unknownLibraryCall(t.Context(), c, fmt.Errorf("simulated DB disconnect"))
	if err != nil || !strings.Contains(unknown, `"effect":"unknown"`) {
		t.Fatal(unknown, err)
	}
	if _, err = executeLibraryPhase(t.Context(), f.alice, "metadata", command); err != errLibraryFenced {
		t.Fatalf("late writer: %v", err)
	}
	output, err := h.readLibraryResult(t.Context(), fmt.Sprintf(`{"result_id":%q}`, c.ID))
	if err != nil || !strings.Contains(output, `"status":"not_run"`) {
		t.Fatal(output, err)
	}
	asset, err := creativelibrary.GetAsset(t.Context(), f.alice, a.ID)
	if err != nil || asset.Revision != 1 {
		t.Fatalf("read wrote %+v %v", asset, err)
	}
}
func TestLibraryCommittedBeforeJournalUpdateRecovers(t *testing.T) {
	f, h, a := organizeFixture(t)
	raw := fmt.Sprintf(`{"items":[{"asset_id":%q,"expected_revision":"1","title":"已提交"}]}`, a.ID)
	c, err := h.startLibraryCall(t.Context(), organizeAssetsTool, raw)
	if err != nil {
		t.Fatal(err)
	}
	c, err = h.freezeLibraryCommand(t.Context(), c, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	command := *c.Progress[0][0].Command
	command.Guard = h.libraryGuard(c.ID, 0, 0)
	if _, err = executeLibraryPhase(t.Context(), f.alice, "metadata", command); err != nil {
		t.Fatal(err)
	}
	output, err := h.runLibraryWrite(t.Context(), organizeAssetsTool, raw)
	if err != nil || !strings.Contains(output, `"status":"applied"`) {
		t.Fatal(output, err)
	}
	asset, err := creativelibrary.GetAsset(t.Context(), f.alice, a.ID)
	if err != nil || asset.Revision != creativeops.Revision(2) {
		t.Fatalf("duplicate effect %+v %v", asset, err)
	}
}

func TestLibraryBatchDetailsPaginationAndPreflight(t *testing.T) {
	f, h, _ := organizeFixture(t)
	tags := []creativelibrary.NewTag{}
	for i := range 25 {
		tags = append(tags, creativelibrary.NewTag{ClientKey: fmt.Sprint(i), Name: strings.Repeat("<", 76) + fmt.Sprintf("%04d", i), Color: "#a1a1a7"})
	}
	body := "正文"
	receipt, err := creativelibrary.CreateAsset(t.Context(), f.alice, command(t, creativelibrary.CreateAssetInput{Title: "标签来源", Content: creativecontent.Draft{Kind: "text", Payload: creativecontent.Payload{Body: &body}}, NewTags: tags}))
	if err != nil {
		t.Fatal(err)
	}
	var source creativelibrary.AssetResult
	if err = json.Unmarshal(receipt.Outcome.Response, &source); err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, id := range source.TagMapping {
		ids = append(ids, id)
	}
	items := []organizeItem{}
	for i := range 50 {
		receipt, err := creativelibrary.CreateAsset(t.Context(), f.alice, command(t, creativelibrary.CreateAssetInput{Title: strings.Repeat("原", 197) + fmt.Sprintf("%03d", i), Content: creativecontent.Draft{Kind: "text", Payload: creativecontent.Payload{Body: &body}}}))
		if err != nil {
			t.Fatal(err)
		}
		var a creativelibrary.AssetResult
		if err = json.Unmarshal(receipt.Outcome.Response, &a); err != nil {
			t.Fatal(err)
		}
		items = append(items, organizeItem{ID: a.ID, Revision: 1, AddTags: ids})
	}
	raw, err := json.Marshal(map[string]any{"items": items})
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) > f.service.limits.ToolArgumentBytes {
		t.Fatal("test input too large", len(raw))
	}
	output, err := h.runLibraryWrite(t.Context(), organizeAssetsTool, string(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(output) > f.service.limits.ToolResultBytes {
		t.Fatal("summary too large")
	}
	var summary struct {
		ID    string `json:"result_id"`
		Items []struct {
			Status string `json:"status"`
		}
	}
	if err = json.Unmarshal([]byte(output), &summary); err != nil {
		t.Fatal(err)
	}
	if len(summary.Items) != 50 {
		t.Fatal("lost summary items")
	}
	for _, v := range summary.Items {
		if v.Status != "applied" {
			t.Fatal(output)
		}
	}
	before := f.count("creative_operation_receipts", "account_id='agent-a'")
	cursor := ""
	count := 0
	for {
		raw, _ := json.Marshal(map[string]any{"result_id": summary.ID, "limit": 50, "cursor": cursor})
		out, err := h.readLibraryResult(t.Context(), string(raw))
		if err != nil {
			t.Fatal(err)
		}
		if len(out) > f.service.limits.ToolResultBytes {
			t.Fatal("detail too large")
		}
		var page struct {
			Items []map[string]any
			Next  string `json:"next_cursor"`
		}
		if err = json.Unmarshal([]byte(out), &page); err != nil {
			t.Fatal(err)
		}
		count += len(page.Items)
		if page.Next == "" {
			break
		}
		cursor = page.Next
	}
	if count != 50+50*25 {
		t.Fatal("missing detail records", count)
	}
	if f.count("creative_operation_receipts", "account_id='agent-a'") != before {
		t.Fatal("reading wrote commands")
	}
	h.step = "small-budget"
	f.service.limits.ToolResultBytes = 300
	if _, err = h.runLibraryWrite(t.Context(), trashAssetsTool, fmt.Sprintf(`{"items":[{"asset_id":%q,"expected_revision":"2"}]}`, items[0].ID)); err == nil {
		t.Fatal("tiny budget accepted")
	}
	a, err := creativelibrary.GetAsset(t.Context(), f.alice, items[0].ID)
	if err != nil || a.DeletedAt != nil {
		t.Fatal("budget checked after effects", err)
	}
}

func TestLibraryStructureActionsTrashAndRetention(t *testing.T) {
	f, h, a := organizeFixture(t)
	run := func(name, raw string) map[string]any {
		t.Helper()
		h.step += "x"
		out, err := h.runLibraryWrite(t.Context(), name, raw)
		if err != nil {
			t.Fatal(err)
		}
		var summary struct{ Items []map[string]any }
		if err = json.Unmarshal([]byte(out), &summary); err != nil || len(summary.Items) != 1 {
			t.Fatal(out, err)
		}
		if summary.Items[0]["status"] != "applied" {
			t.Fatal(out)
		}
		return summary.Items[0]
	}
	group := run(manageLibraryTool, `{"actions":[{"action":"create_group","name":"参考"}]}`)
	run(manageLibraryTool, fmt.Sprintf(`{"actions":[{"action":"rename_group","group_id":%q,"expected_revision":"1","name":"改名"}]}`, group["id"]))
	run(manageLibraryTool, fmt.Sprintf(`{"actions":[{"action":"move_group","group_id":%q,"expected_revision":"2","parent_id":null,"position":0}]}`, group["id"]))
	cat := run(manageLibraryTool, `{"actions":[{"action":"create_category","name":"风格"}]}`)
	run(manageLibraryTool, fmt.Sprintf(`{"actions":[{"action":"rename_category","category_id":%q,"expected_revision":"1","name":"风格二"}]}`, cat["id"]))
	tag := run(manageLibraryTool, fmt.Sprintf(`{"actions":[{"action":"create_tag","name":"标签","category_id":%q}]}`, cat["id"]))
	reused := run(manageLibraryTool, `{"actions":[{"action":"create_tag","name":"标签"}]}`)
	if reused["existing"] != true {
		t.Fatal("reuse not recorded")
	}
	run(manageLibraryTool, fmt.Sprintf(`{"actions":[{"action":"edit_tag","tag_id":%q,"expected_revision":"1","name":"新标签"}]}`, tag["id"]))
	run(organizeAssetsTool, fmt.Sprintf(`{"items":[{"asset_id":%q,"expected_revision":"1","add_tag_ids":[%q],"add_group_ids":[%q]}]}`, a.ID, tag["id"], group["id"]))
	run(trashAssetsTool, fmt.Sprintf(`{"items":[{"asset_id":%q,"expected_revision":"2"}]}`, a.ID))
	deleted := run(manageLibraryTool, fmt.Sprintf(`{"actions":[{"action":"delete_tag","tag_id":%q,"expected_revision":"2"}]}`, tag["id"]))
	if deleted["affected_asset_count"] != float64(1) {
		t.Fatal(deleted)
	}
	run(manageLibraryTool, fmt.Sprintf(`{"actions":[{"action":"delete_category","category_id":%q,"expected_revision":"2"}]}`, cat["id"]))
	run(manageLibraryTool, fmt.Sprintf(`{"actions":[{"action":"delete_group","group_id":%q,"expected_revision":"2"}]}`, group["id"]))
	run(restoreAssetsTool, fmt.Sprintf(`{"items":[{"asset_id":%q,"expected_revision":"5"}]}`, a.ID))
	id := toolOperation(h.held.RunID + "/" + h.step + "/library-result")
	if _, err := f.db.Exec("UPDATE creative_agent_library_calls SET retained_until=now()-interval '1 day' WHERE account_id='agent-a' AND id=$1", id); err != nil {
		t.Fatal(err)
	}
	if err := pruneLibraryResults(t.Context(), f.alice, 100); err != nil {
		t.Fatal(err)
	}
	if _, err := h.readLibraryResult(t.Context(), fmt.Sprintf(`{"result_id":%q}`, id)); err == nil || !strings.Contains(err.Error(), "result_expired") {
		t.Fatal("expiry", err)
	}
}

func TestPersistedUnknownIsSettledByLaterReadTool(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	body := "参考正文"
	receipt, err := creativelibrary.CreateAsset(t.Context(), f.alice, command(t, creativelibrary.CreateAssetInput{Title: "原题", Content: creativecontent.Draft{Kind: "text", Payload: creativecontent.Payload{Body: &body}}}))
	if err != nil {
		t.Fatal(err)
	}
	var asset creativelibrary.AssetResult
	if err = json.Unmarshal(receipt.Outcome.Response, &asset); err != nil {
		t.Fatal(err)
	}
	_, err = f.db.Exec(`CREATE FUNCTION reject_library_progress() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.state='executing' AND NEW.progress #>> '{0,0,status}'='applied' THEN RAISE EXCEPTION 'simulated journal outage after command commit'; END IF; RETURN NEW; END $$;
 CREATE TRIGGER reject_library_progress BEFORE UPDATE ON creative_agent_library_calls FOR EACH ROW EXECUTE FUNCTION reject_library_progress()`)
	if err != nil {
		t.Fatal(err)
	}
	f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, n int) (llmgateway.Result, error) {
		switch n {
		case 1:
			return scriptedResult(r, call("organize", 0, organizeAssetsTool, fmt.Sprintf(`{"items":[{"asset_id":%q,"expected_revision":"1","title":"新标题"}]}`, asset.ID))), nil
		case 2:
			var unknown struct {
				Effect string `json:"effect"`
				ID     string `json:"result_id"`
			}
			if err := json.Unmarshal([]byte(toolResult(r, 0)), &unknown); err != nil || unknown.Effect != "unknown" || unknown.ID == "" {
				t.Fatal("expected persisted unknown", toolResult(r, 0), err)
			}
			if _, err := f.db.Exec(`DROP TRIGGER reject_library_progress ON creative_agent_library_calls`); err != nil {
				t.Fatal(err)
			}
			return scriptedResult(r, call("reconcile", 0, readLibraryResultTool, fmt.Sprintf(`{"result_id":%q}`, unknown.ID))), nil
		case 3:
			if !strings.Contains(toolResult(r, 0), `"state":"complete"`) || !strings.Contains(toolResult(r, 0), `"status":"applied"`) {
				t.Fatal("read did not settle", toolResult(r, 0))
			}
		}
		return scriptedResult(r), nil
	}
	consent := libraryConsent(t, f, c.ID)
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "已确认把标题改成新标题"})
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
	var state string
	var output []byte
	if err := f.db.QueryRow(`SELECT state,output FROM creative_agent_steps WHERE account_id='agent-a' AND run_id=$1 AND tool_key=$2`, run.ID, organizeAssetsTool).Scan(&state, &output); err != nil || state != "succeeded" || !strings.Contains(string(output), "unknown") {
		t.Fatal("history rewritten", state, string(output), err)
	}
	final, err := creativelibrary.GetAsset(t.Context(), f.alice, asset.ID)
	if err != nil || final.Revision != 2 {
		t.Fatal("duplicate write", final, err)
	}
}

func TestTwoPhaseRecoveryAndConcurrentMetadataConflict(t *testing.T) {
	for _, mode := range []string{"after_metadata", "after_organize", "concurrent_change"} {
		t.Run(mode, func(t *testing.T) {
			f, h, a := organizeFixture(t)
			r, err := creativelibrary.CreateTag(t.Context(), f.alice, command(t, creativelibrary.TagInput{Name: "参考", Color: "#a1a1a7", HierarchyRevision: 1}))
			if err != nil {
				t.Fatal(err)
			}
			var tag creativelibrary.LibraryResult
			if err = json.Unmarshal(r.Outcome.Response, &tag); err != nil {
				t.Fatal(err)
			}
			raw := fmt.Sprintf(`{"items":[{"asset_id":%q,"expected_revision":"1","title":"新标题","add_tag_ids":[%q]}]}`, a.ID, tag.ID)
			c, err := h.startLibraryCall(t.Context(), organizeAssetsTool, raw)
			if err != nil {
				t.Fatal(err)
			}
			count := 1
			if mode == "after_organize" {
				count = 2
			}
			for j := 0; j < count; j++ {
				c, err = h.freezeLibraryCommand(t.Context(), c, 0, j)
				if err != nil {
					t.Fatal(err)
				}
				command := *c.Progress[0][j].Command
				command.Guard = h.libraryGuard(c.ID, 0, j)
				if _, err := executeLibraryPhase(t.Context(), f.alice, c.Plan.Items[0].Phases[j].Name, command); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "concurrent_change" {
				title := "摄影师修改"
				if _, err := creativelibrary.Metadata(t.Context(), f.alice, command(t, creativelibrary.MetadataInput{ID: a.ID, ExpectedRevision: 2, Title: &title})); err != nil {
					t.Fatal(err)
				}
			}
			output, err := h.runLibraryWrite(t.Context(), organizeAssetsTool, raw)
			if err != nil {
				t.Fatal(err)
			}
			final, err := creativelibrary.GetAsset(t.Context(), f.alice, a.ID)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "concurrent_change" {
				if !strings.Contains(output, `"status":"partial"`) || !strings.Contains(output, `"current_revision":"3"`) || final.Title != "摄影师修改" || len(final.TagIDs) != 0 {
					t.Fatal(output, final)
				}
			} else if !strings.Contains(output, `"status":"applied"`) || final.Revision != 3 || len(final.TagIDs) != 1 {
				t.Fatal(output, final)
			}
		})
	}
}
func TestLibraryResultsStayWithinConversationAndConsent(t *testing.T) {
	f, h, a := organizeFixture(t)
	raw := fmt.Sprintf(`{"items":[{"asset_id":%q,"expected_revision":"1","title":"确认过的新标题"}]}`, a.ID)
	output, err := h.runLibraryWrite(t.Context(), organizeAssetsTool, raw)
	if err != nil {
		t.Fatal(err)
	}
	var summary struct {
		ID string `json:"result_id"`
	}
	if err = json.Unmarshal([]byte(output), &summary); err != nil {
		t.Fatal(err)
	}
	query := fmt.Sprintf(`{"result_id":%q}`, summary.ID)
	other := runtimeHandler{service: h.service, scope: h.scope, held: h.held}
	other.held.RunID = "later-run"
	if _, err := other.readLibraryResult(t.Context(), query); err != nil {
		t.Fatal("same conversation later run", err)
	}
	conversation := f.conversation(f.alice, f.canvasID)
	other.held.ConversationID = conversation.ID
	other.held.ConsentID = libraryConsent(t, f, conversation.ID).ID
	if _, err := other.readLibraryResult(t.Context(), query); err == nil {
		t.Fatal("cross conversation")
	}
	other.scope = f.bob
	if _, err := other.readLibraryResult(t.Context(), query); err == nil {
		t.Fatal("cross account")
	}
	if _, err := f.db.Exec(`UPDATE creative_egress_consents SET revoked_at=now() WHERE account_id='agent-a' AND id=$1`, h.held.ConsentID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.readLibraryResult(t.Context(), query); err == nil {
		t.Fatal("revoked consent")
	}
}
