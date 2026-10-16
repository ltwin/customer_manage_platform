package creativeagent

import (
	"context"
	"encoding/json"
	"log/slog"
	"slices"
	"strings"
	"sync"

	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// toolConcurrency bounds the calls of one answer that run at once. A call
// waiting for another holds no slot. It is a package constant rather than a
// limit, so checkpoints registered before it stay compatible.
const toolConcurrency = 8

// resource is one thing a tool call touches. Shared uses coexist; an
// exclusive one waits for, and holds off, every other use of the same key.
//
// Keys live within one run, which binds one canvas: "canvas" is its topology,
// "node:<id>" one node's content, "node:*" every node's content, "library"
// the asset library, "plan" the plan block, "search:<query>" and
// "webpage:<url>" one outside lookup, and "run" everything.
type resource struct {
	key       string
	exclusive bool
}

const (
	keyRun     = "run"
	keyCanvas  = "canvas"
	keyNodes   = "node:*"
	keyLibrary = "library"
	keyPlan    = "plan"
)

func nodeKey(id string) string { return "node:" + id }

func keysMeet(a, b string) bool {
	switch {
	case a == b:
		return true
	case a == keyNodes:
		return strings.HasPrefix(b, "node:")
	case b == keyNodes:
		return strings.HasPrefix(a, "node:")
	}
	return false
}

func holdsRun(rs []resource) bool {
	return slices.ContainsFunc(rs, func(r resource) bool { return r.key == keyRun && r.exclusive })
}

// conflicts says whether two calls must keep the order the model gave them:
// they meet on a key and at least one of them writes it. A call holding the
// run meets every call, including one that names nothing.
func conflicts(a, b []resource) bool {
	if holdsRun(a) || holdsRun(b) {
		return true
	}
	for _, x := range a {
		for _, y := range b {
			if (x.exclusive || y.exclusive) && keysMeet(x.key, y.key) {
				return true
			}
		}
	}
	return false
}

// stringArgument reads one string field of a call's arguments; "" when the
// arguments or the field do not parse.
func stringArgument(args, field string) string {
	var fields map[string]json.RawMessage
	if json.Unmarshal([]byte(args), &fields) != nil {
		return ""
	}
	var s string
	if json.Unmarshal(fields[field], &s) != nil {
		return ""
	}
	return strings.TrimSpace(s)
}

// plannedCall is one call of an answer as the journal recorded it.
type plannedCall struct {
	id, name, args string
	finished       bool
}

// toolBatch is the dispatch plan of one model answer. It is rebuilt from the
// journal and the canvas whenever execution (re)starts, so nothing about it
// is persisted.
type toolBatch struct {
	position map[string]int // framework call ID → place in the answer
	finished []bool         // already in the journal when the plan was made
	deps     [][]int
	done     []chan struct{}
	closing  []sync.Once
	slots    chan struct{}
	mu       sync.Mutex
	// stopped is the first error that ends the run. Calls that have not
	// started by then never will.
	stopped error
}

func newToolBatch(calls []plannedCall, upstream map[string][]string) *toolBatch {
	b := &toolBatch{position: map[string]int{}, finished: make([]bool, len(calls)), deps: make([][]int, len(calls)),
		done: make([]chan struct{}, len(calls)), closing: make([]sync.Once, len(calls)), slots: make(chan struct{}, toolConcurrency)}
	used := make([][]resource, len(calls))
	rewired := false
	for j, c := range calls {
		b.position[c.id] = j
		b.done[j] = make(chan struct{})
		used[j] = toolResources(c.name, c.args, upstream, rewired)
		for i := range j {
			if conflicts(used[i], used[j]) {
				b.deps[j] = append(b.deps[j], i)
			}
		}
		if c.finished {
			// Resumed answers skip finished calls entirely, so their signal
			// cannot wait for a replay that may never come.
			b.finished[j] = true
			b.finish(j)
			continue
		}
		// A finished call is already in the canvas the plan read.
		rewired = rewired || conflicts(used[j], []resource{{keyCanvas, false}})
	}
	return b
}

func (b *toolBatch) finish(i int) { b.closing[i].Do(func() { close(b.done[i]) }) }

// acquire waits for the calls i depends on, then for a slot.
func (b *toolBatch) acquire(ctx context.Context, i int) error {
	for _, dep := range b.deps[i] {
		select {
		case <-b.done[dep]:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	select {
	case b.slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (b *toolBatch) release() { <-b.slots }

// start is checked after the slot is taken, so a call that waited cannot
// slip past a stop that happened meanwhile.
func (b *toolBatch) start() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.stopped
}

func (b *toolBatch) stop(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.stopped == nil {
		b.stopped = err
	}
}

// pendingBatch is the plan of the answer being executed, built once by the
// first call to arrive while the others wait.
type pendingBatch struct {
	parent string
	ready  chan struct{}
	batch  *toolBatch
	err    error
}

// dispatch runs one call of the answer when what it depends on has finished
// and a slot is free. Admission still happens inside callTool, right before
// the call runs, exactly as when calls ran one by one.
func (h *runtimeHandler) dispatch(ctx context.Context, name, callID, args string, read func(context.Context) (string, error)) (string, error) {
	parent, _ := h.current.get()
	b, err := h.batchFor(ctx, parent)
	if err != nil {
		return "", err
	}
	i, ok := b.position[callID]
	if !ok || b.finished[i] {
		// Unknown calls fail in callTool; finished ones replay from the journal.
		return h.callTool(ctx, name, callID, args, read)
	}
	defer b.finish(i)
	if err := b.acquire(ctx, i); err != nil {
		b.stop(err)
		return "", err
	}
	defer b.release()
	if err := b.start(); err != nil {
		return "", err
	}
	result, err := h.callTool(ctx, name, callID, args, read)
	if err != nil {
		// Recorded before the slot is given back, so no waiting call starts.
		b.stop(err)
	}
	return result, err
}

func (h *runtimeHandler) batchFor(ctx context.Context, parent string) (*toolBatch, error) {
	h.mu.Lock()
	p := h.pending
	build := p == nil || p.parent != parent
	if build {
		// A new answer starts only after every call of the last one returned.
		p = &pendingBatch{parent: parent, ready: make(chan struct{})}
		h.pending = p
	}
	h.mu.Unlock()
	if build {
		p.batch, p.err = h.planBatch(ctx, parent)
		close(p.ready)
	}
	select {
	case <-p.ready:
		return p.batch, p.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// planBatch reads the answer's calls from the journal, and the canvas for the
// nodes its generations read.
func (h *runtimeHandler) planBatch(ctx context.Context, parent string) (*toolBatch, error) {
	var calls []plannedCall
	var canvasID *string
	err := h.scope.WithReadSnapshot(ctx, func(tx store.ReadTxAccountScope) error {
		if err := tx.QueryRow(ctx, "creative_agent_conversations", "canvas_id", "id=$2", h.held.ConversationID).Scan(&canvasID); err != nil {
			return err
		}
		rows, err := tx.QueryPage(ctx, "creative_agent_steps", "state,input,tool_call_index", "run_id=$2 AND parent_model_step_id=$3 AND retry_of_step_id IS NULL", []store.OrderBy{{Column: "tool_call_index"}}, h.service.limits.ToolCallsPerTurn, 0, h.held.RunID, parent)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var state string
			var raw []byte
			var index int
			if err = rows.Scan(&state, &raw, &index); err != nil {
				return err
			}
			var c llmgateway.ToolCall
			if err = json.Unmarshal(raw, &c); err != nil {
				return err
			}
			calls = append(calls, plannedCall{id: runtimeCallID(parent, index), name: c.Name, args: string(c.Arguments), finished: state == "succeeded" || state == "failed"})
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	var nodes []string
	for _, c := range calls {
		if c.name == generateNodeTool && !c.finished {
			if id := stringArgument(c.args, "node_id"); id != "" {
				nodes = append(nodes, id)
			}
		}
	}
	var upstream map[string][]string
	if len(nodes) > 0 && canvasID != nil {
		// Without the canvas the generations run one at a time, as before.
		if upstream, err = creativecanvas.GenerationUpstream(ctx, h.scope, *canvasID, nodes); err != nil {
			h.service.logger.WarnContext(ctx, "creative agent could not read generation upstream",
				slog.String("run_id", h.held.RunID), slog.String("error", err.Error()))
			upstream = nil
		}
	}
	return newToolBatch(calls, upstream), nil
}

type toolStepKey struct{}

// withToolStep names the journal row of the call ctx runs. Tools that write
// derive their idempotent identity from it, so it travels with the call.
func withToolStep(ctx context.Context, stepID string) context.Context {
	return context.WithValue(ctx, toolStepKey{}, stepID)
}

func toolStepFrom(ctx context.Context) string {
	s, _ := ctx.Value(toolStepKey{}).(string)
	return s
}
