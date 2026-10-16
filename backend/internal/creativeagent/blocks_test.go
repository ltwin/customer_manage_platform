package creativeagent

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/realtime"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func usage() llmgateway.UsageEvidence {
	input, output, cached := int64(120), int64(40), int64(0)
	return llmgateway.UsageEvidence{InputTotalTokens: &input, OutputTokens: &output, InputCachedTokens: &cached}
}

// stream sends text through the sink with pauses long enough to defeat the
// harness's throttle, so each fragment is its own write.
func stream(r llmgateway.ProviderRequest, parts ...string) string {
	for _, part := range parts {
		if r.Sink != nil {
			r.Sink.Delta(llmgateway.DeltaText, part)
		}
		time.Sleep(streamFlushInterval + 20*time.Millisecond)
	}
	return strings.Join(parts, "")
}

// replayedBlock is a client's copy of one block: everything block.created
// carried, then only what block.updated said changed.
type replayedBlock struct {
	RunBlock
	fields map[string]any
}

// replay rebuilds the block list from the event log alone, applying the one
// merge rule a client has: revision must be exactly local+1.
func replay(t *testing.T, f *fixture, runID string) (map[string]*replayedBlock, []string, int) {
	t.Helper()
	rows, err := f.db.Query(`SELECT seq,event_type,payload FROM creative_run_events WHERE run_id=$1 ORDER BY seq`, runID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	blocks := map[string]*replayedBlock{}
	var types []string
	appends := 0
	expected := int64(1)
	for rows.Next() {
		var seq int64
		var kind string
		var raw []byte
		if err := rows.Scan(&seq, &kind, &raw); err != nil {
			t.Fatal(err)
		}
		if seq != expected {
			t.Fatalf("seq gap: got %d want %d", seq, expected)
		}
		expected++
		types = append(types, kind)
		switch kind {
		case "block.created":
			var b RunBlock
			if err := json.Unmarshal(raw, &b); err != nil {
				t.Fatal(err)
			}
			if _, dup := blocks[b.ID]; dup || b.Revision != 1 {
				t.Fatalf("created twice or not at revision 1: %+v", b)
			}
			payload := map[string]any{}
			if err := json.Unmarshal(b.Payload, &payload); err != nil {
				t.Fatal(err)
			}
			blocks[b.ID] = &replayedBlock{RunBlock: b, fields: payload}
		case "block.updated":
			var u struct {
				BlockID        string               `json:"block_id"`
				Revision       creativeops.Revision `json:"revision"`
				State          string               `json:"state"`
				PayloadVersion int                  `json:"payload_version"`
				FinishedAt     *time.Time           `json:"finished_at"`
				Patch          *struct {
					Append *textAppend     `json:"append"`
					Set    json.RawMessage `json:"set"`
				} `json:"patch"`
			}
			if err := json.Unmarshal(raw, &u); err != nil {
				t.Fatal(err)
			}
			b := blocks[u.BlockID]
			if b == nil || u.Revision != b.Revision+1 {
				t.Fatalf("update out of order: %s", raw)
			}
			b.Revision = u.Revision
			if u.State != "" {
				b.State = u.State
			}
			if u.PayloadVersion != 0 {
				b.PayloadVersion = u.PayloadVersion
			}
			if u.FinishedAt != nil {
				b.FinishedAt = u.FinishedAt
			}
			if u.Patch != nil && u.Patch.Append != nil {
				appends++
				current, _ := b.fields[u.Patch.Append.Path].(string)
				b.fields[u.Patch.Append.Path] = current + u.Patch.Append.Text
			}
			if u.Patch != nil && u.Patch.Set != nil {
				b.fields = map[string]any{}
				if err := json.Unmarshal(u.Patch.Set, &b.fields); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	return blocks, types, appends
}

func TestBlocksAndEventsRebuildTheSameList(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	consent := f.consent(f.alice, c.ID)
	plan := func(a, b string) json.RawMessage {
		return json.RawMessage(`{"steps":[{"key":"read","title":"读取画布","state":"` + a + `"},{"key":"sort","title":"整理参考","state":"` + b + `"}]}`)
	}
	f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, n int) (llmgateway.Result, error) {
		result := llmgateway.Result{FinishReason: llmgateway.FinishToolCalls, Model: r.Snapshot.RequestModelID, Usage: usage()}
		switch n {
		case 1:
			result.Text = stream(r, "我先", "看看画布")
			result.ToolCalls = []llmgateway.ToolCall{
				{ID: "p", Name: updatePlanTool, Arguments: plan(PlanDoing, PlanTodo)},
				{ID: "c", Index: 1, Name: readHistoryTool, Arguments: json.RawMessage(`{"before_ordinal":0,"limit":10}`)},
			}
		case 2:
			result.ToolCalls = []llmgateway.ToolCall{{ID: "p2", Name: updatePlanTool, Arguments: plan(PlanDone, PlanDone)}}
		default:
			result.FinishReason, result.Text = llmgateway.FinishStop, stream(r, "整理", "完成。**三张**参考")
		}
		return result, nil
	}
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "整理参考"})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	snapshot, err := f.service.ReadRunSnapshot(t.Context(), f.alice, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.State != RunSucceeded || snapshot.SnapshotSeq != snapshot.LastEventSeq {
		t.Fatalf("run %s code=%s seq=%d/%d", snapshot.State, snapshot.ErrorCode, snapshot.SnapshotSeq, snapshot.LastEventSeq)
	}
	var visible []string
	for i, b := range snapshot.Blocks {
		if b.Ordinal != creativeops.Revision(i+1) || b.FinishedAt == nil {
			t.Fatalf("block %d: ordinal %d finished %v", i, b.Ordinal, b.FinishedAt)
		}
		if !b.Hidden {
			visible = append(visible, b.Type+":"+b.State)
		}
	}
	// The order on screen is the order things happened in.
	if got := strings.Join(visible, ","); got != "text:complete,tool:succeeded,plan:complete,text:complete" {
		t.Fatalf("visible blocks %s", got)
	}
	var first, last textPayload
	_ = json.Unmarshal(snapshot.Blocks[0].Payload, &first)
	_ = json.Unmarshal(snapshot.Blocks[len(snapshot.Blocks)-1].Payload, &last)
	if first.Markdown != "我先看看画布" || last.Markdown != "整理完成。**三张**参考" {
		t.Fatalf("text blocks %q / %q", first.Markdown, last.Markdown)
	}
	// The block and the message the model will read again agree.
	if n := f.count("creative_agent_messages", "run_id=$1 AND role='assistant' AND body->'blocks'->0->>'text'=$2", run.ID, last.Markdown); n != 1 {
		t.Fatalf("assistant message does not match its block: %d", n)
	}
	var tool toolPayload
	for _, b := range snapshot.Blocks {
		if b.Type == BlockTool && !b.Hidden {
			_ = json.Unmarshal(b.Payload, &tool)
		}
		if b.Type == BlockPlan && !strings.Contains(string(b.Payload), `"done"`) {
			t.Fatalf("plan not updated: %s", b.Payload)
		}
	}
	if tool.Title != "读取对话历史" || tool.DurationMS == nil || tool.ToolKey != readHistoryTool {
		t.Fatalf("tool row %+v", tool)
	}
	detail, err := f.service.ReadBlockDetail(t.Context(), f.alice, run.ID, snapshot.Blocks[2].ID)
	var arguments struct {
		Limit int `json:"limit"`
	}
	_ = json.Unmarshal(detail.Arguments, &arguments)
	if err != nil || detail.ToolKey != readHistoryTool || arguments.Limit != 10 || detail.Result == "" {
		t.Fatalf("detail %+v %v", detail, err)
	}
	if _, err = f.service.ReadBlockDetail(t.Context(), f.bob, run.ID, snapshot.Blocks[2].ID); err == nil {
		t.Fatal("another account read a tool detail")
	}

	replayed, types, appends := replay(t, f, run.ID)
	if appends == 0 {
		t.Fatal("streamed text never reached the log as appends")
	}
	if len(replayed) != len(snapshot.Blocks) {
		t.Fatalf("replay has %d blocks, snapshot %d", len(replayed), len(snapshot.Blocks))
	}
	// The whole block, not just its content: a reader that followed along and
	// one that refreshed must compute the same durations from the same times.
	for _, b := range snapshot.Blocks {
		got := replayed[b.ID].RunBlock
		got.Payload, _ = json.Marshal(replayed[b.ID].fields)
		want := b
		want.Payload = normalizedJSON(b.Payload)
		gotJSON, _ := json.Marshal(got)
		wantJSON, _ := json.Marshal(want)
		if string(gotJSON) != string(wantJSON) {
			t.Fatalf("replay diverged from the snapshot:\n replay   %s\n snapshot %s", gotJSON, wantJSON)
		}
	}
	for _, kind := range types {
		if kind == "step.state_changed" || kind == "message.completed" {
			t.Fatalf("retired event %s still written", kind)
		}
	}

	// The stream source serves the same log and knows when the run is over.
	source := RunEventSource{}
	if _, err = source.Cursor(t.Context(), f.alice, run.ID, strconv.FormatInt(int64(snapshot.LastEventSeq)+1, 10)); !errors.Is(err, realtime.ErrSnapshotRequired) {
		t.Fatalf("cursor beyond the log: %v", err)
	}
	if _, err = source.Cursor(t.Context(), f.alice, run.ID, "01"); !errors.Is(err, creativeops.ErrValidation) {
		t.Fatalf("malformed cursor: %v", err)
	}
	events, done, err := source.ReadAfter(t.Context(), f.alice, run.ID, "0", 1000)
	if err != nil || !done || len(events) != int(snapshot.LastEventSeq) {
		t.Fatalf("read after: %d events done=%v err=%v", len(events), done, err)
	}
	var envelope RunEvent
	if err = json.Unmarshal(events[0].Data, &envelope); err != nil || envelope.Seq != 1 || envelope.RunID != run.ID || envelope.SchemaVersion != runEventSchemaVersion {
		t.Fatalf("envelope %+v %v", envelope, err)
	}
	if _, _, err = source.ReadAfter(t.Context(), f.bob, run.ID, "0", 10); !errors.Is(err, realtime.ErrUnavailable) {
		t.Fatalf("another account followed the run: %v", err)
	}
	if _, err = f.db.Exec(`UPDATE creative_agent_runs SET pruned_through_seq=3 WHERE id=$1`, run.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err = source.ReadAfter(t.Context(), f.alice, run.ID, "2", 10); !errors.Is(err, realtime.ErrSnapshotRequired) {
		t.Fatalf("pruned cursor: %v", err)
	}
}

// Whatever was streamed is only a preview: a replaced attempt withdraws it and
// the validated result decides the block.
func TestStreamedPreviewYieldsToTheResult(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	consent := f.consent(f.alice, c.ID)
	f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, n int) (llmgateway.Result, error) {
		if n == 1 {
			stream(r, "被替换的")
			return llmgateway.Result{}, &llmgateway.ProviderError{Outcome: llmgateway.OutcomeUnaccepted, Class: "connect_refused"}
		}
		stream(r, "正确")
		return llmgateway.Result{Text: "正确的回答", FinishReason: llmgateway.FinishStop, Model: r.Snapshot.RequestModelID, Usage: usage()}, nil
	}
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "回答"})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	snapshot, err := f.service.ReadRunSnapshot(t.Context(), f.alice, run.ID)
	if err != nil || len(snapshot.Blocks) != 1 || !strings.Contains(string(snapshot.Blocks[0].Payload), "正确的回答") {
		t.Fatalf("blocks %+v err %v", snapshot.Blocks, err)
	}
	if n := f.count("creative_run_events", "run_id=$1 AND event_type='block.updated' AND payload->'patch'->'set'->>'markdown'=''", run.ID); n != 1 {
		t.Fatalf("the replaced attempt's preview was not withdrawn: %d", n)
	}
}

// A worker redoing a step after a crash finds its old preview and replaces it
// instead of appending to it or adding a second block.
func TestRedoneStepUpsertsItsBlock(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	consent := f.consent(f.alice, c.ID)
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "回答"})
	if err != nil {
		t.Fatal(err)
	}
	held, err := f.service.claimRun(t.Context(), f.alice, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	var current currentStep
	current.set("ccst_00000000-0000-0000-0000-00000000aaaa", "")
	crashed := &textSink{service: f.service, scope: f.alice, held: held, current: &current, ctx: t.Context()}
	crashed.Delta(llmgateway.DeltaText, "崩溃前")
	time.Sleep(streamFlushInterval + 20*time.Millisecond)
	crashed.Delta(llmgateway.DeltaText, "写了一半")
	redo := &textSink{service: f.service, scope: f.alice, held: held, current: &current, ctx: t.Context()}
	redo.Delta(llmgateway.DeltaText, "重做")
	var markdown string
	if err := f.db.QueryRow(`SELECT payload->>'markdown' FROM creative_agent_blocks WHERE run_id=$1`, run.ID).Scan(&markdown); err != nil || markdown != "重做" {
		t.Fatalf("redo preview %q %v", markdown, err)
	}
	if n := f.count("creative_agent_blocks", "run_id=$1", run.ID); n != 1 {
		t.Fatalf("redo created %d blocks", n)
	}
	// Only the current holder may write: a superseded epoch is ignored.
	stale := held
	stale.Epoch--
	(&textSink{service: f.service, scope: f.alice, held: stale, current: &current, ctx: t.Context()}).Delta(llmgateway.DeltaText, "旧")
	if err := f.db.QueryRow(`SELECT payload->>'markdown' FROM creative_agent_blocks WHERE run_id=$1`, run.ID).Scan(&markdown); err != nil || markdown != "重做" {
		t.Fatalf("stale holder wrote %q %v", markdown, err)
	}
}

// Stopping ends every block still in progress in the same transaction, so a
// refresh never shows one spinning, and nothing reopens it afterwards.
func TestStopInterruptsEveryOpenBlock(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	consent := f.consent(f.alice, c.ID)
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "回答"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.claimRun(t.Context(), f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	err = f.alice.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		if _, err := writeBlockInTx(t.Context(), tx, run.ID, "a", blockChange{Type: BlockText, State: "streaming", Set: textPayload{Markdown: "写到"}}); err != nil {
			return err
		}
		if _, err := writeBlockInTx(t.Context(), tx, run.ID, "plan", blockChange{Type: BlockPlan, State: "active", Set: planPayload{Steps: []PlanStep{{Key: "k", Title: "步骤", State: PlanDoing}}}}); err != nil {
			return err
		}
		_, err := writeBlockInTx(t.Context(), tx, run.ID, "n", blockChange{Type: BlockNotice, State: "complete", Set: noticePayload{Code: "c", Message: "已完成的块不受影响"}})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.CancelRun(t.Context(), f.alice, command(t, map[string]any{"run_id": run.ID})); err != nil {
		t.Fatal(err)
	}
	if n := f.count("creative_agent_blocks", "run_id=$1 AND state='interrupted' AND finished_at IS NOT NULL", run.ID); n != 2 {
		t.Fatalf("interrupted blocks %d", n)
	}
	// A reader following along learns when each block ended, not just that it did.
	if n := f.count("creative_run_events", "run_id=$1 AND event_type='block.updated' AND payload->>'state'='interrupted' AND payload ? 'finished_at'", run.ID); n != 2 {
		t.Fatalf("interruptions announced with an end time: %d", n)
	}
	if n := f.count("creative_agent_blocks", "run_id=$1 AND type='notice' AND state='complete'", run.ID); n != 1 {
		t.Fatal("a finished block was rewritten")
	}
	before := f.count("creative_run_events", "run_id=$1", run.ID)
	err = f.alice.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		_, err := writeBlockInTx(t.Context(), tx, run.ID, "a", blockChange{Type: BlockText, Append: &textAppend{Path: "markdown", Text: "迟到"}})
		return err
	})
	if err != nil || f.count("creative_run_events", "run_id=$1", run.ID) != before {
		t.Fatalf("an ended block was reopened: %v", err)
	}
}

func TestBlockRegistryRefusesWhatItDoesNotDescribe(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	consent := f.consent(f.alice, c.ID)
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "回答"})
	if err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]blockChange{
		"unknown type":  {Type: "question", State: "waiting", Set: map[string]any{}},
		"unknown state": {Type: BlockText, State: "waiting", Set: textPayload{}},
		"unknown field": {Type: BlockText, State: "complete", Set: map[string]any{"markdown": "x", "html": "<b>"}},
		"no payload":    {Type: BlockText, State: "complete"},
		"bad plan":      {Type: BlockPlan, State: "active", Set: map[string]any{"steps": "all"}},
	} {
		err := f.alice.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
			_, err := writeBlockInTx(t.Context(), tx, run.ID, "k", change)
			return err
		})
		if !errors.Is(err, creativeops.ErrValidation) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if err := validPlan([]PlanStep{{Key: "a", Title: "x", State: "maybe"}}); err == nil {
		t.Fatal("plan step state outside the set")
	}
}

func TestToolTitlesDoNotDependOnDeploymentAvailability(t *testing.T) {
	for _, tc := range []struct{ key, title string }{
		{setNodePromptTool, "写入提示词"}, {generateNodeTool, "生成"},
		{compressImageTool, "压缩图片"}, {searchWebTool, "搜索网页"},
	} {
		if got := toolTitle(tc.key); got != tc.title {
			t.Errorf("%s title=%q, want %q", tc.key, got, tc.title)
		}
	}
	if got := toolTitle("future_tool"); got != "future_tool" {
		t.Fatalf("unknown tool: %q", got)
	}
}

func TestHistoricalToolTitlesProjectWithoutRewritingRecords(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	run, err := f.startRun(f.alice, c.ID, f.consent(f.alice, c.ID).ID, InstructionSegment{Type: "text", Text: "查看历史"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.alice.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		for _, state := range []string{"running", "succeeded"} {
			_, err := writeBlockInTx(t.Context(), tx, run.ID, "call:old-step", blockChange{Type: BlockTool, State: state, Set: toolPayload{StepID: "old-step", ToolKey: setNodePromptTool, Title: setNodePromptTool, StartedAt: time.Now(), DetailRef: "old-step", NodeRefs: []NodeRef{}}})
			if err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := f.service.ReadRunSnapshot(t.Context(), f.alice, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range snapshot.Blocks {
		if b.Type == BlockTool && !strings.Contains(string(b.Payload), "写入提示词") {
			t.Errorf("snapshot title: %s", b.Payload)
		}
	}
	events, _, err := (RunEventSource{}).ReadAfter(t.Context(), f.alice, run.ID, "0", 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if (event.Type == "block.created" || event.Type == "block.updated") && strings.Contains(string(event.Data), "old-step") && !strings.Contains(string(event.Data), "写入提示词") {
			t.Errorf("event title: %s", event.Data)
		}
	}
	var stored string
	if err := f.db.QueryRow(`SELECT payload->>'title' FROM creative_agent_blocks WHERE run_id=$1 AND type='tool'`, run.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != setNodePromptTool {
		t.Fatalf("historical record rewritten: %s", stored)
	}
}
