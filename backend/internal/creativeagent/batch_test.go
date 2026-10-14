package creativeagent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/creativegeneration"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func planned(name, args string) plannedCall { return plannedCall{name: name, args: args} }

func generation(node string) plannedCall {
	return planned(generateNodeTool, fmt.Sprintf(`{"node_id":%q,"draft_revision":1}`, node))
}

// Two calls keep the model's order only when they touch the same thing and
// one of them writes it; anything that cannot be shown apart waits.
func TestCallsWaitOnlyForWhatTheyTouch(t *testing.T) {
	read := planned(readCanvasTool, `{"offset":0,"limit":50}`)
	edit := planned(editCanvasTool, `{"actions":[]}`)
	asset := planned(readAssetTool, `{"content_revision_id":"ccrv_1"}`)
	page := func(u string) plannedCall { return planned(readWebpageTool, fmt.Sprintf(`{"url":%q}`, u)) }
	// b reads a; a and c read nothing.
	canvas := map[string][]string{"a": {}, "b": {"a"}, "c": {}}
	for _, tc := range []struct {
		name     string
		calls    []plannedCall
		upstream map[string][]string
		want     [][]int
	}{
		{"reads run together", []plannedCall{read, read, planned(searchAssetsTool, `{}`), asset}, canvas, [][]int{nil, nil, nil, nil}},
		{"holding the run waits for a call that names nothing", []plannedCall{planned(readHistoryTool, `{}`), planned(conversationAssetsTool, `{}`)}, canvas, [][]int{nil, {0}}},
		{"a call that names nothing waits for one holding the run", []plannedCall{planned(conversationAssetsTool, `{}`), planned(readHistoryTool, `{}`)}, canvas, [][]int{nil, {0}}},
		{"history and skills run alongside anything else", []plannedCall{edit, planned(readHistoryTool, `{}`), planned(readSkillTool, `{}`)}, canvas, [][]int{nil, nil, nil}},
		{"an asset read waits for a trash", []plannedCall{planned(trashAssetsTool, `{}`), asset}, canvas, [][]int{nil, {0}}},
		{"an asset read waits for a generation", []plannedCall{generation("a"), asset}, canvas, [][]int{nil, {0}}},
		{"placing an asset waits for a restore", []plannedCall{planned(restoreAssetsTool, `{}`), edit}, canvas, [][]int{nil, {0}}},
		{"a trash waits for an earlier placement", []plannedCall{edit, planned(trashAssetsTool, `{}`)}, canvas, [][]int{nil, {0}}},
		{"a canvas read waits for an edit", []plannedCall{edit, read}, canvas, [][]int{nil, {0}}},
		{"a canvas read waits for a generation", []plannedCall{generation("a"), read}, canvas, [][]int{nil, {0}}},
		{"unconnected generations run together", []plannedCall{generation("a"), generation("c")}, canvas, [][]int{nil, nil}},
		{"one node generated twice waits", []plannedCall{generation("a"), generation("a")}, canvas, [][]int{nil, {0}}},
		{"downstream waits for upstream", []plannedCall{generation("a"), generation("b")}, canvas, [][]int{nil, {0}}},
		{"upstream waits for an earlier downstream", []plannedCall{generation("b"), generation("a")}, canvas, [][]int{nil, {0}}},
		{"a save waits for its node's generation", []plannedCall{generation("a"), planned(saveLibraryTool, `{"node_id":"a"}`)}, canvas, [][]int{nil, {0}}},
		{"a generation after an edit holds the run", []plannedCall{edit, generation("c"), generation("a")}, canvas, [][]int{nil, {0}, {0, 1}}},
		{"a generation without the canvas holds the run", []plannedCall{generation("a"), generation("c")}, nil, [][]int{nil, {0}}},
		{"the same page waits, another does not", []plannedCall{page("https://Example.com:443/a#x"), page("https://example.com/a"), page("https://example.com/b")}, canvas, [][]int{nil, {0}, nil}},
		{"library writes keep their order", []plannedCall{planned(organizeAssetsTool, `{}`), planned(searchAssetsTool, `{}`), planned(trashAssetsTool, `{}`)}, canvas, [][]int{nil, {0}, {0, 1}}},
		{"an unknown tool holds the run", []plannedCall{read, planned("mystery", `{}`), read}, canvas, [][]int{nil, {0}, {1}}},
		{"malformed arguments hold the run", []plannedCall{read, planned(generateNodeTool, `not json`)}, canvas, [][]int{nil, {0}}},
		{"a call meeting two earlier ones waits for both", []plannedCall{generation("a"), generation("c"), read}, canvas, [][]int{nil, nil, {0, 1}}},
		{"conversation assets wait for every earlier result", []plannedCall{generation("a"), generation("c"), planned(conversationAssetsTool, `{}`)}, canvas, [][]int{nil, nil, {0, 1}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for i := range tc.calls {
				tc.calls[i].id = fmt.Sprint("call-", i)
			}
			b := newToolBatch(tc.calls, tc.upstream)
			for i, want := range tc.want {
				if !slices.Equal(b.deps[i], want) {
					t.Fatalf("call %d waits for %v, want %v", i, b.deps[i], want)
				}
			}
		})
	}
}

// A resumed answer skips calls that already finished, so their signal comes
// from the journal, and an edit that already landed is in the canvas read.
func TestFinishedCallsReleaseTheirDependents(t *testing.T) {
	edit := planned(editCanvasTool, `{"actions":[]}`)
	edit.finished = true
	calls := []plannedCall{edit, generation("a"), generation("c")}
	for i := range calls {
		calls[i].id = fmt.Sprint("call-", i)
	}
	b := newToolBatch(calls, map[string][]string{"a": {}, "c": {}})
	if !slices.Equal(b.deps[2], []int{0}) {
		t.Fatalf("finished edit still held the run: %v", b.deps[2])
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := b.acquire(ctx, 1); err != nil {
		t.Fatal("waited for a finished call:", err)
	}
}

// meeting lets the stub generator prove calls overlap: everyone expected must
// be inside at once, or the wait gives up.
type meeting struct {
	mu      sync.Mutex
	arrived map[string]bool
	want    int
	all     chan struct{}
}

func newMeeting(want int) *meeting {
	return &meeting{arrived: map[string]bool{}, want: want, all: make(chan struct{})}
}

func (m *meeting) arrive(who string) bool {
	m.mu.Lock()
	m.arrived[who] = true
	if len(m.arrived) == m.want {
		close(m.all)
	}
	m.mu.Unlock()
	select {
	case <-m.all:
		return true
	case <-time.After(5 * time.Second):
		return false
	}
}

// Generations of unconnected nodes in one answer run at once; one that reads
// another's node waits for it and so sees its result, as it did one by one.
func TestUnconnectedGenerationsRunTogether(t *testing.T) {
	f := setup(t)
	f.queue()
	together := newMeeting(2)
	var mu sync.Mutex
	var log []string
	note := func(s string) {
		mu.Lock()
		defer mu.Unlock()
		log = append(log, s)
	}
	var created map[string]string
	generator := &stubGenerator{generate: func(_ context.Context, in creativegeneration.AgentGeneration) error {
		switch in.NodeID {
		case created["ref"]:
			if !together.arrive("ref") {
				note("ref alone")
			}
			time.Sleep(50 * time.Millisecond)
			note("ref done")
		case created["free"]:
			if !together.arrive("free") {
				note("free alone")
			}
		case created["img"]:
			note("img start")
		}
		return nil
	}}
	f.service.SetGenerator(generator)
	c := f.conversation(f.alice, f.canvasID)
	consent := contextConsent(t, f, c.ID)
	f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, n int) (llmgateway.Result, error) {
		switch n {
		case 1:
			return scriptedResult(r, call("edit", 0, editCanvasTool, `{"actions":[
				{"type":"add_node","key":"ref","node_type":"text","text":"金色逆光"},
				{"type":"add_node","key":"img","node_type":"image"},
				{"type":"add_node","key":"free","node_type":"image"},
				{"type":"connect","source_node_id":"ref","target_node_id":"img"}]}`)), nil
		case 2:
			created = createdNodes(t, toolResult(r, 0))
			// The stub generates whatever it is asked; the canvas decides the order.
			return scriptedResult(r,
				call("g0", 0, generateNodeTool, fmt.Sprintf(`{"node_id":%q,"draft_revision":1}`, created["ref"])),
				call("g1", 1, generateNodeTool, fmt.Sprintf(`{"node_id":%q,"draft_revision":1}`, created["img"])),
				call("g2", 2, generateNodeTool, fmt.Sprintf(`{"node_id":%q,"draft_revision":1}`, created["free"]))), nil
		}
		return scriptedResult(r), nil
	}
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "一起出图"})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	got, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
	if err != nil || got.State != RunSucceeded {
		t.Fatalf("run=%+v %v", got, err)
	}
	if strings.Join(log, ",") != "ref done,img start" {
		t.Fatalf("order %v", log)
	}
}

func createdNodes(t *testing.T, result string) map[string]string {
	t.Helper()
	var out struct {
		Created map[string]string `json:"created"`
	}
	if err := json.Unmarshal([]byte(result), &out); err != nil || len(out.Created) == 0 {
		t.Fatalf("edit result %q", result)
	}
	return out.Created
}

// generations answers n generate_node calls on nodes the canvas does not
// have, so nothing links them.
func generations(r llmgateway.ProviderRequest, n int) llmgateway.Result {
	calls := make([]llmgateway.ToolCall, n)
	for i := range calls {
		calls[i] = call(fmt.Sprint("g", i), i, generateNodeTool, fmt.Sprintf(`{"node_id":"cwnode_g%d","draft_revision":1}`, i))
	}
	return scriptedResult(r, calls...)
}

// A call that ends the run stops the calls still waiting for a slot; the
// ones already running finish.
func TestRunEndingCallStopsWaitingCalls(t *testing.T) {
	f := setup(t)
	f.queue()
	f.service.limits.ToolCallsPerTurn = max(f.service.limits.ToolCallsPerTurn, toolConcurrency+1)
	running := newMeeting(toolConcurrency)
	var mu sync.Mutex
	entered := 0
	generator := &stubGenerator{generate: func(_ context.Context, in creativegeneration.AgentGeneration) error {
		mu.Lock()
		entered++
		n := entered
		mu.Unlock()
		if !running.arrive(in.NodeID) {
			return fmt.Errorf("only %d calls ran together", n)
		}
		if n == toolConcurrency {
			return ErrLimit
		}
		time.Sleep(100 * time.Millisecond)
		return nil
	}}
	f.service.SetGenerator(generator)
	c := f.conversation(f.alice, f.canvasID)
	consent := contextConsent(t, f, c.ID)
	f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, n int) (llmgateway.Result, error) {
		if n == 1 {
			return generations(r, toolConcurrency+1), nil
		}
		return scriptedResult(r), nil
	}
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "出九张"})
	if err != nil {
		t.Fatal(err)
	}
	_ = f.work(f.alice, run.ID)
	got, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
	if err != nil || got.State != RunFailed {
		t.Fatalf("run=%+v %v", got, err)
	}
	if entered != toolConcurrency {
		t.Fatalf("%d generations started, want %d", entered, toolConcurrency)
	}
	if n := f.count("creative_agent_steps", "run_id=$1 AND tool_key=$2 AND output IS NULL AND state='failed'", run.ID, generateNodeTool); n != 2 {
		t.Fatalf("%d calls left without a result, want the failing one and the stopped one", n)
	}
}

// A stop asked for while a call waits on another is honoured before it runs:
// admission comes right before execution, not when the wait began. A stop
// that lands after a generation passed the run's admission but before its
// request was recorded is caught by generation admission. Stopping also
// withdraws the run's generations the gateway has not sent yet.
func TestStopWhileWaitingKeepsTheCallFromRunning(t *testing.T) {
	f := setup(t)
	f.queue()
	var runID string
	generator := &stubGenerator{}
	generator.generate = func(ctx context.Context, _ creativegeneration.AgentGeneration) error {
		_, err := f.service.CancelRun(ctx, f.alice, command(t, map[string]any{"run_id": runID}))
		return err
	}
	f.service.SetGenerator(generator)
	c := f.conversation(f.alice, f.canvasID)
	consent := contextConsent(t, f, c.ID)
	f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, n int) (llmgateway.Result, error) {
		if n == 1 {
			return scriptedResult(r,
				call("g", 0, generateNodeTool, `{"node_id":"cwnode_a","draft_revision":1}`),
				call("read", 1, readCanvasTool, `{"offset":0,"limit":50}`)), nil
		}
		return scriptedResult(r), nil
	}
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "出图后看看画布"})
	if err != nil {
		t.Fatal(err)
	}
	runID = run.ID
	_ = f.work(f.alice, run.ID)
	got, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
	if err != nil || got.State != RunCancelled {
		t.Fatalf("run=%+v %v", got, err)
	}
	if n := f.count("creative_agent_steps", "run_id=$1 AND tool_key=$2 AND output IS NULL", run.ID, readCanvasTool); n != 1 {
		t.Fatal("the waiting call ran after the stop")
	}
	if n := f.count("creative_agent_context_items", "run_id=$1 AND kind='tool_result'", run.ID); n != 0 {
		t.Fatalf("%d tool results recorded after the stop", n)
	}
	var step string
	if err = f.db.QueryRow(`SELECT id FROM creative_agent_steps WHERE run_id=$1 AND tool_key=$2`, run.ID, generateNodeTool).Scan(&step); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(generator.stopped, []string{run.ID + "/" + step}) {
		t.Fatalf("stopped %v", generator.stopped)
	}
	if generator.submitted != 0 {
		t.Fatal("a generation was accepted after the stop")
	}
}

// A run that ends without the photographer stopping it leaves its
// generations to finish.
func TestFinishedRunKeepsItsGenerations(t *testing.T) {
	f := setup(t)
	f.queue()
	generator := &stubGenerator{}
	f.service.SetGenerator(generator)
	c := f.conversation(f.alice, f.canvasID)
	consent := contextConsent(t, f, c.ID)
	f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, n int) (llmgateway.Result, error) {
		if n == 1 {
			return generations(r, 2), nil
		}
		return scriptedResult(r), nil
	}
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "出两张"})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.CancelRun(t.Context(), f.alice, command(t, map[string]any{"run_id": run.ID})); err != nil {
		t.Fatal(err)
	}
	if len(generator.stopped) != 0 {
		t.Fatalf("a late stop withdrew %v", generator.stopped)
	}
}

// Calls finishing together queue on the run and conversation rows; every
// result lands, numbered without gaps. A pool of two connections is enough,
// so no transaction holds one connection while waiting for another.
func TestCallsFinishingTogetherOnASmallPool(t *testing.T) {
	f := setup(t)
	f.service.SetGenerator(&stubGenerator{})
	small, err := store.Open(t.Context(), f.url+"&pool_max_conns=2")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(small.Close)
	if err = small.MigrateCreativeJobs(t.Context()); err != nil {
		t.Fatal(err)
	}
	// Jobs are enqueued in the run's own transactions, so they share its pool.
	runtime, err := small.NewJobRuntime(f.service.Handlers(), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	f.service.SetRuntime(runtime)
	scope := small.ScopeFor(auth.AccountContext{AccountID: "agent-a"})
	c := f.conversation(scope, f.canvasID)
	consent := contextConsent(t, f, c.ID)
	f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, n int) (llmgateway.Result, error) {
		if n == 1 {
			calls := generations(r, 2).ToolCalls
			for i := range toolConcurrency {
				calls = append(calls, call(fmt.Sprint("read", i), len(calls), readCanvasTool, `{"offset":0,"limit":50}`))
			}
			return scriptedResult(r, calls...), nil
		}
		return scriptedResult(r), nil
	}
	run, err := f.startRun(scope, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "一起读"})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- f.work(scope, run.ID) }()
	select {
	case err = <-done:
	case <-time.After(60 * time.Second):
		t.Fatal("calls deadlocked on a small pool")
	}
	if err != nil {
		t.Fatal(err)
	}
	got, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
	if err != nil || got.State != RunSucceeded {
		t.Fatalf("run=%+v %v", got, err)
	}
	if n := f.count("creative_agent_messages", "run_id=$1 AND role='tool'", run.ID); n != toolConcurrency+2 {
		t.Fatalf("%d tool results", n)
	}
	var count, low, high int
	if err = f.db.QueryRow(`SELECT count(*),min(ordinal),max(ordinal) FROM creative_agent_messages WHERE conversation_id=$1`, c.ID).Scan(&count, &low, &high); err != nil {
		t.Fatal(err)
	}
	if high-low+1 != count {
		t.Fatalf("message ordinals have gaps: %d messages in %d..%d", count, low, high)
	}
}
