package creativeagent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativeimageedit"
	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
)

func (f *fixture) notice(handle string) string {
	f.t.Helper()
	var state string
	if err := f.db.QueryRow(`SELECT notification_state FROM creative_agent_tool_tasks WHERE id=$1`, handle).Scan(&state); err != nil {
		f.t.Fatal(err)
	}
	return state
}

// systemNotices are the conversation's completion notices as the panel
// lists them.
func (f *fixture) systemNotices(conversationID string) []Message {
	f.t.Helper()
	page, err := f.service.ListMessages(f.t.Context(), f.alice, conversationID, "", 50)
	if err != nil {
		f.t.Fatal(err)
	}
	var out []Message
	for _, m := range page.Items {
		if m.Role == "system" {
			out = append(out, m)
		}
	}
	return out
}

// detachedGeneration parks a generation and moves it to the background, so
// its run has ended by the time the task finishes.
func detachedGeneration(t *testing.T, f *fixture) (Run, *stubGenerator, string) {
	t.Helper()
	run, generator, _ := startParkedGeneration(t, f)
	handle := f.sceneTasks(run.ID)[0].ID
	if _, _, err := f.detach(handle); err != nil {
		t.Fatal(err)
	}
	if err := f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	return run, generator, handle
}

// A finished background task writes one notice in its conversation, holding
// the result's revisions, and its card shows the result after the run
// ended. A second settle of the same source adds nothing.
func TestBackgroundResultBecomesOneNotice(t *testing.T) {
	f := setup(t)
	run, generator, handle := detachedGeneration(t, f)
	generator.mu.Lock()
	generator.pending = false
	generator.mu.Unlock()
	for range 2 {
		f.markDue(handle)
		if _, err := f.service.FinishDueToolTasks(t.Context(), f.st, 10); err != nil {
			t.Fatal(err)
		}
	}
	notices := f.systemNotices(run.ConversationID)
	if len(notices) != 1 || notices[0].BackgroundTask == nil || notices[0].BackgroundTask.Handle != handle || notices[0].BackgroundTask.State != taskSucceeded ||
		!strings.Contains(notices[0].Body.Blocks[0].Text, "后台任务已完成") {
		t.Fatalf("notices %+v", notices)
	}
	if f.notice(handle) != "pending" {
		t.Fatalf("notice state %s", f.notice(handle))
	}
	snapshot, err := f.service.ReadRunSnapshot(t.Context(), f.alice, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	var card struct {
		Task *ToolTask `json:"task"`
	}
	for _, b := range snapshot.Blocks {
		if b.Type == BlockTool {
			if err := json.Unmarshal(b.Payload, &card); err != nil {
				t.Fatal(err)
			}
		}
	}
	if card.Task == nil || card.Task.State != taskSucceeded || card.Task.Mode != modeBackground || !strings.Contains(string(card.Task.Result), "ccrv_out") {
		t.Fatalf("card task %+v", card.Task)
	}
}

// A failed compress and an expired scene job report their results too.
func TestFailedAndExpiredBackgroundResultsAreNoticed(t *testing.T) {
	f := setup(t)
	run, compressor, _ := startParkedCompress(t, f)
	handle := f.sceneTasks(run.ID)[0].ID
	if _, _, err := f.detach(handle); err != nil {
		t.Fatal(err)
	}
	compressor.mu.Lock()
	compressor.pending = false
	compressor.conclude = creativeimageedit.AgentCompressResult{NodeID: "cwnode_photo", State: creativeimageedit.AgentFailed, Reason: "编码失败"}
	compressor.mu.Unlock()
	f.markDue(handle)
	if _, err := f.service.FinishDueToolTasks(t.Context(), f.st, 10); err != nil {
		t.Fatal(err)
	}
	notices := f.systemNotices(run.ConversationID)
	if len(notices) != 1 || notices[0].BackgroundTask.State != taskFailed || !strings.Contains(string(notices[0].BackgroundTask.Result), "编码失败") ||
		!strings.Contains(notices[0].Body.Blocks[0].Text, "失败") {
		t.Fatalf("notices %+v", notices)
	}
}

func TestExpiredBackgroundSceneIsNoticed(t *testing.T) {
	f, c, consent, doc := visualScene(t, false)
	s := &script{turns: []func(llmgateway.ProviderRequest) llmgateway.Result{
		func(r llmgateway.ProviderRequest) llmgateway.Result {
			return scriptedResult(r, sceneCall("a", readSceneTool, map[string]any{"document_id": doc}))
		},
		func(r llmgateway.ProviderRequest) llmgateway.Result {
			return scriptedResult(r, sceneCall("b", writeModelTool, map[string]any{"document_id": doc, "based_on_revision": lastTool(r)["revision"],
				"target": map[string]any{"add": map[string]any{"name": "石柱", "transform": identityAt(0)}}, "code": testPillarCode, "run_in_background": true}))
		},
	}}
	f.vendor.script = s.answer
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "搭一根石柱"})
	if err != nil {
		t.Fatal(err)
	}
	if got := f.resume(run.ID); got.State != RunSucceeded {
		t.Fatalf("run %s %s", got.State, got.ErrorCode)
	}
	if _, err := f.db.Exec(`UPDATE creative_scene_jobs SET deadline=now()-interval '1 second' WHERE run_id=$1`, run.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := creativescene.SweepAllAccounts(t.Context(), f.st, "", 10); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.FinishDueToolTasks(t.Context(), f.st, 10); err != nil {
		t.Fatal(err)
	}
	notices := f.systemNotices(c.ID)
	if len(notices) != 1 || notices[0].BackgroundTask.State != taskExpired || !strings.Contains(notices[0].Body.Blocks[0].Text, "已过期") {
		t.Fatalf("notices %+v", notices)
	}
}

// A deleted conversation suppresses the notice.
func TestNoticeOfADeletedConversationIsSuppressed(t *testing.T) {
	f := setup(t)
	run, generator, handle := detachedGeneration(t, f)
	if _, err := f.db.Exec(`DELETE FROM creative_agent_conversations WHERE id=$1`, run.ConversationID); err != nil {
		t.Fatal(err)
	}
	generator.mu.Lock()
	generator.pending = false
	generator.mu.Unlock()
	f.markDue(handle)
	if _, err := f.service.FinishDueToolTasks(t.Context(), f.st, 10); err != nil {
		t.Fatal(err)
	}
	if got := f.taskRow(handle); got.State != taskSucceeded || f.notice(handle) != "suppressed" || f.count("creative_agent_messages", "role='system'") != 0 {
		t.Fatalf("task=%+v notice=%s", got, f.notice(handle))
	}
}
