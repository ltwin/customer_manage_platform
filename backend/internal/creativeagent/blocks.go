package creativeagent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// Block types written in this milestone. The rest of the catalogue (question,
// confirm, proposal, changes, suggestions) registers with the item that owns
// its interaction; the table never enumerates types, this registry does.
const (
	BlockText     = "text"
	BlockThinking = "thinking"
	BlockTool     = "tool"
	BlockPlan     = "plan"
	BlockNotice   = "notice"
)

// BlockInterrupted is every type's shared terminal state: a run that stops or
// fails ends whatever was still in progress, so a refresh never shows a block
// spinning forever.
const BlockInterrupted = "interrupted"

// blockTextBytes keeps one text block under the column's 256 KiB payload
// ceiling with room for the JSON around it. Longer text continues in the next
// block of the same step.
const blockTextBytes = 240 << 10

// RunBlock is what the photographer sees of one piece of a run. It only ever
// grows: ordinal is assigned once, and a block is never deleted or reordered.
type RunBlock struct {
	ID             string               `json:"id"`
	RunID          string               `json:"run_id"`
	Ordinal        creativeops.Revision `json:"ordinal"`
	Type           string               `json:"type"`
	State          string               `json:"state"`
	Revision       creativeops.Revision `json:"revision"`
	PayloadVersion int                  `json:"payload_version"`
	Hidden         bool                 `json:"hidden"`
	Payload        json.RawMessage      `json:"payload"`
	StartedAt      time.Time            `json:"started_at"`
	FinishedAt     *time.Time           `json:"finished_at,omitempty"`
}

const blockColumns = "id,run_id,ordinal,type,state,revision,payload_version,hidden,payload,started_at,finished_at"

func scanBlock(row interface{ Scan(...any) error }) (RunBlock, error) {
	var b RunBlock
	var ordinal, revision int64
	if err := row.Scan(&b.ID, &b.RunID, &ordinal, &b.Type, &b.State, &revision, &b.PayloadVersion,
		&b.Hidden, &b.Payload, &b.StartedAt, &b.FinishedAt); err != nil {
		return RunBlock{}, err
	}
	b.Ordinal, b.Revision = creativeops.Revision(ordinal), creativeops.Revision(revision)
	return b, nil
}

// blockKind is one registered type: the states it may be in, which of them
// end it, and the payload shape at the version this server writes. A new
// interaction is one more entry here plus a component on the client.
type blockKind struct {
	version  int
	states   []string
	terminal []string
	// payload decodes into a fresh value with unknown fields refused, so a
	// payload the client would not understand is never written.
	payload func() any
}

func (k blockKind) allows(state string) bool {
	return state == BlockInterrupted || containsString(k.states, state)
}

func (k blockKind) ends(state string) bool {
	return state == BlockInterrupted || containsString(k.terminal, state)
}

func containsString(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}

type textPayload struct {
	Markdown string `json:"markdown"`
}

type thinkingPayload struct {
	Summary    string `json:"summary"`
	DurationMS int64  `json:"duration_ms"`
}

// NodeRef names a canvas node a tool produced; V6-039 renders it as a chip.
type NodeRef struct {
	NodeID     string `json:"node_id"`
	RevisionID string `json:"revision_id,omitempty"`
}

type toolPayload struct {
	StepID     string    `json:"step_id"`
	ToolKey    string    `json:"tool_key"`
	Title      string    `json:"title"`
	StartedAt  time.Time `json:"started_at"`
	DurationMS *int64    `json:"duration_ms,omitempty"`
	// DetailRef is what GET .../blocks/{id}/detail resolves; arguments and
	// results can be large and never travel in the block itself.
	DetailRef string    `json:"detail_ref"`
	NodeRefs  []NodeRef `json:"node_refs"`
	ErrorCode string    `json:"error_code,omitempty"`
}

// Plan step states.
const (
	PlanTodo    = "todo"
	PlanDoing   = "doing"
	PlanDone    = "done"
	PlanSkipped = "skipped"
)

type PlanStep struct {
	Key   string `json:"key"`
	Title string `json:"title"`
	State string `json:"state"`
}

type planPayload struct {
	Steps []PlanStep `json:"steps"`
}

type noticePayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

var blockKinds = map[string]blockKind{
	BlockText:     {version: 1, states: []string{"streaming", "complete"}, terminal: []string{"complete"}, payload: func() any { return &textPayload{} }},
	BlockThinking: {version: 1, states: []string{"streaming", "complete"}, terminal: []string{"complete"}, payload: func() any { return &thinkingPayload{} }},
	BlockTool:     {version: 1, states: []string{"running", "succeeded", "failed", "skipped"}, terminal: []string{"succeeded", "failed", "skipped"}, payload: func() any { return &toolPayload{} }},
	BlockPlan:     {version: 1, states: []string{"active", "complete"}, terminal: []string{"complete"}, payload: func() any { return &planPayload{} }},
	BlockNotice:   {version: 1, states: []string{"complete"}, terminal: []string{"complete"}, payload: func() any { return &noticePayload{} }},
}

// validatePayload refuses anything the registered shape does not describe,
// including fields it does not know.
func (k blockKind) validatePayload(raw json.RawMessage) error {
	if len(raw) == 0 || raw[0] != '{' {
		return creativeops.ErrValidation
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(k.payload()); err != nil {
		return fmt.Errorf("%w: block payload: %w", creativeops.ErrValidation, err)
	}
	return nil
}

// blockChange is one write to the block named by a run-local source key. Set
// replaces the payload; Append adds text to one string field. A change that
// creates the block must Set it.
type blockChange struct {
	Type   string
	State  string
	Set    any
	Append *textAppend
	Hidden bool
}

type textAppend struct {
	Path string `json:"path"`
	Text string `json:"text"`
}

type blockPatch struct {
	Append *textAppend      `json:"append,omitempty"`
	Set    *json.RawMessage `json:"set,omitempty"`
}

// blockUpdated carries every field the write changed and nothing else, so a
// reader that applies it holds exactly the row a fresh snapshot would return.
type blockUpdated struct {
	BlockID  string               `json:"block_id"`
	Revision creativeops.Revision `json:"revision"`
	State    string               `json:"state,omitempty"`
	// PayloadVersion accompanies a set: the payload is rewritten at the version
	// this server writes, which may be newer than the block was created with.
	PayloadVersion int `json:"payload_version,omitempty"`
	// FinishedAt is set by the write that ends the block. Durations are
	// computed from it, so it travels with the state that caused it.
	FinishedAt *time.Time  `json:"finished_at,omitempty"`
	Patch      *blockPatch `json:"patch,omitempty"`
}

// writeBlockInTx is the only way a block changes. The block row and the event
// that announces it commit together under the run's row lock, so a snapshot
// plus the events after its seq always rebuild exactly this list.
//
// The source key makes it idempotent: a worker redoing a step after a crash
// upserts the same block instead of adding a second one. A block that has
// already ended is not reopened, so a late write after a stop is a no-op.
func writeBlockInTx(ctx context.Context, tx store.TxAccountScope, runID, sourceKey string, change blockChange) (RunBlock, error) {
	kind, ok := blockKinds[change.Type]
	if !ok || sourceKey == "" || len(sourceKey) > 160 {
		return RunBlock{}, creativeops.ErrValidation
	}
	if change.State != "" && !kind.allows(change.State) {
		return RunBlock{}, creativeops.ErrValidation
	}
	var payload json.RawMessage
	if change.Set != nil {
		raw, err := json.Marshal(change.Set)
		if err != nil {
			return RunBlock{}, err
		}
		if err := kind.validatePayload(raw); err != nil {
			return RunBlock{}, err
		}
		payload = raw
	}
	var next int64
	if err := tx.QueryRowForUpdate(ctx, "creative_agent_runs", "next_block_ordinal", "id=$2", runID).Scan(&next); err != nil {
		if errors.Is(err, store.ErrNoRows) {
			return RunBlock{}, ErrNotFound
		}
		return RunBlock{}, err
	}
	now, err := tx.CreativeNow(ctx)
	if err != nil {
		return RunBlock{}, err
	}
	current, err := scanBlock(tx.QueryRow(ctx, "creative_agent_blocks", blockColumns, "run_id=$2 AND source_key=$3", runID, sourceKey))
	if errors.Is(err, store.ErrNoRows) {
		return createBlockInTx(ctx, tx, runID, sourceKey, kind, change, payload, next, now)
	}
	if err != nil {
		return RunBlock{}, err
	}
	if current.Type != change.Type {
		return RunBlock{}, errCheckpointJournal
	}
	if kind.ends(current.State) {
		return current, nil
	}
	state := current.State
	if change.State != "" {
		state = change.State
	}
	update := blockUpdated{BlockID: current.ID}
	if state != current.State {
		update.State = state
	}
	var finished any
	if kind.ends(state) {
		finished = now
	}
	switch {
	case change.Append != nil:
		if change.Append.Path == "" || !utf8.ValidString(change.Append.Text) {
			return RunBlock{}, creativeops.ErrValidation
		}
		var field map[string]json.RawMessage
		if err := json.Unmarshal(current.Payload, &field); err != nil {
			return RunBlock{}, err
		}
		if _, ok := field[change.Append.Path]; !ok {
			return RunBlock{}, creativeops.ErrValidation
		}
		if len(current.Payload)+len(change.Append.Text)*2 > blockTextBytes {
			// Display only: the final Set carries the whole text, split if needed.
			return current, nil
		}
		updated, err := tx.Update(ctx, "creative_agent_blocks",
			"payload=jsonb_set(payload,ARRAY[$4::text],to_jsonb(coalesce(payload->>$4,'')||$5)),state=$6,revision=revision+1,finished_at=$7,updated_at=$8",
			"id=$2 AND revision=$3", current.ID, int64(current.Revision), change.Append.Path, change.Append.Text, state, finished, now)
		if err != nil {
			return RunBlock{}, err
		}
		if updated == 0 {
			return RunBlock{}, errCheckpointConflict
		}
		update.Patch = &blockPatch{Append: change.Append}
	case payload != nil:
		if bytes.Equal(normalizedJSON(payload), normalizedJSON(current.Payload)) && update.State == "" {
			return current, nil
		}
		updated, err := tx.Update(ctx, "creative_agent_blocks",
			"payload=$4,payload_version=$5,state=$6,revision=revision+1,finished_at=$7,updated_at=$8",
			"id=$2 AND revision=$3", current.ID, int64(current.Revision), []byte(payload), kind.version, state, finished, now)
		if err != nil {
			return RunBlock{}, err
		}
		if updated == 0 {
			return RunBlock{}, errCheckpointConflict
		}
		update.Patch = &blockPatch{Set: &payload}
	default:
		if update.State == "" {
			return current, nil
		}
		updated, err := tx.Update(ctx, "creative_agent_blocks",
			"state=$4,revision=revision+1,finished_at=$5,updated_at=$6",
			"id=$2 AND revision=$3", current.ID, int64(current.Revision), state, finished, now)
		if err != nil {
			return RunBlock{}, err
		}
		if updated == 0 {
			return RunBlock{}, errCheckpointConflict
		}
	}
	block, err := scanBlock(tx.QueryRow(ctx, "creative_agent_blocks", blockColumns, "id=$2", current.ID))
	if err != nil {
		return RunBlock{}, err
	}
	// Taken from the row as stored, so the event and the snapshot agree to the
	// database's own precision.
	update.Revision, update.FinishedAt = block.Revision, block.FinishedAt
	if update.Patch != nil && update.Patch.Set != nil {
		update.PayloadVersion = block.PayloadVersion
	}
	if _, err := appendRunEventInTx(ctx, tx, runID, "block.updated", update); err != nil {
		return RunBlock{}, err
	}
	return block, nil
}

func createBlockInTx(ctx context.Context, tx store.TxAccountScope, runID, sourceKey string, kind blockKind,
	change blockChange, payload json.RawMessage, ordinal int64, now time.Time) (RunBlock, error) {
	if payload == nil || change.State == "" {
		return RunBlock{}, creativeops.ErrValidation
	}
	id, err := creativeops.NewResourceID("ccbk")
	if err != nil {
		return RunBlock{}, err
	}
	var finished any
	if kind.ends(change.State) {
		finished = now
	}
	if err := tx.Insert(ctx, "creative_agent_blocks",
		[]string{"id", "run_id", "ordinal", "type", "state", "payload_version", "source_key", "hidden", "payload", "started_at", "finished_at", "created_at", "updated_at"},
		id, runID, ordinal, change.Type, change.State, kind.version, sourceKey, change.Hidden, []byte(payload), now, finished, now, now); err != nil {
		return RunBlock{}, err
	}
	if _, err := tx.Update(ctx, "creative_agent_runs", "next_block_ordinal=$3", "id=$2", runID, ordinal+1); err != nil {
		return RunBlock{}, err
	}
	block, err := scanBlock(tx.QueryRow(ctx, "creative_agent_blocks", blockColumns, "id=$2", id))
	if err != nil {
		return RunBlock{}, err
	}
	if _, err := appendRunEventInTx(ctx, tx, runID, "block.created", block); err != nil {
		return RunBlock{}, err
	}
	return block, nil
}

// normalizedJSON compares payloads as JSONB stores them, so re-setting the
// same plan or tool state is not a change worth an event.
func normalizedJSON(raw json.RawMessage) []byte {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return raw
	}
	out, err := json.Marshal(value)
	if err != nil {
		return raw
	}
	return out
}

func textSourceKey(stepID string, part int) string {
	if part == 0 {
		return "step:" + stepID + ":text"
	}
	return "step:" + stepID + ":text:" + strconv.Itoa(part+1)
}

// finishTextBlocksInTx makes a model step's text blocks equal the validated
// result. Whatever was streamed before was only a preview; an empty or
// rejected result withdraws it.
func finishTextBlocksInTx(ctx context.Context, tx store.TxAccountScope, runID, stepID, text string) error {
	if text == "" {
		exists, err := tx.Exists(ctx, "creative_agent_blocks", "run_id=$2 AND source_key=$3", runID, textSourceKey(stepID, 0))
		if err != nil || !exists {
			return err
		}
		_, err = writeBlockInTx(ctx, tx, runID, textSourceKey(stepID, 0), blockChange{Type: BlockText, State: BlockInterrupted, Set: textPayload{Markdown: ""}})
		return err
	}
	for part := 0; text != ""; part++ {
		chunk := utf8Prefix(text, blockTextBytes/2)
		text = text[len(chunk):]
		if _, err := writeBlockInTx(ctx, tx, runID, textSourceKey(stepID, part), blockChange{Type: BlockText, State: "complete", Set: textPayload{Markdown: chunk}}); err != nil {
			return err
		}
	}
	return nil
}

// toolBlockStates maps the step journal onto what the photographer sees. The
// step stays the authority; the block is its projection.
var toolBlockStates = map[string]string{"prepared": "running", "dispatched": "running", "succeeded": "succeeded", "failed": "failed", "unknown": "failed"}

// hiddenTools are process tools whose effect is another block (a plan, a
// question). Showing their call as a tool row would say the same thing twice.
var hiddenTools = map[string]bool{updatePlanTool: true}

// writeToolBlockInTx projects one tool step into its block, in the same
// transaction that changed the step. It rebuilds the whole payload from the
// journal each time, so it also creates a block a step never had.
func (s *Service) writeToolBlockInTx(ctx context.Context, tx store.TxAccountScope, runID, stepID string) error {
	var state string
	var key, code *string
	var started, finished *time.Time
	var created time.Time
	if err := tx.QueryRow(ctx, "creative_agent_steps", "state,tool_key,error_code,started_at,finished_at,created_at",
		"id=$2 AND run_id=$3 AND kind='tool'", stepID, runID).Scan(&state, &key, &code, &started, &finished, &created); err != nil {
		return err
	}
	toolKey := ""
	if key != nil {
		toolKey = *key
	}
	if started == nil {
		started = &created
	}
	payload := toolPayload{StepID: stepID, ToolKey: toolKey, Title: s.toolTitle(toolKey), StartedAt: *started, DetailRef: stepID, NodeRefs: []NodeRef{}}
	if code != nil {
		payload.ErrorCode = *code
	}
	if finished != nil {
		duration := finished.Sub(*started).Milliseconds()
		payload.DurationMS = &duration
	}
	blockState, ok := toolBlockStates[state]
	if !ok {
		return errCheckpointJournal
	}
	_, err := writeBlockInTx(ctx, tx, runID, "call:"+stepID, blockChange{Type: BlockTool, State: blockState, Set: payload, Hidden: hiddenTools[toolKey]})
	return err
}

// writeToolBlocksForModelStepInTx projects every tool plan one model answer
// produced. It runs after that answer's text so the order on screen is the
// order the model spoke in.
func (s *Service) writeToolBlocksForModelStepInTx(ctx context.Context, tx store.TxAccountScope, runID, modelStep string) error {
	rows, err := tx.QueryPage(ctx, "creative_agent_steps", "id", "run_id=$2 AND parent_model_step_id=$3 AND kind='tool'",
		[]store.OrderBy{{Column: "tool_call_index"}}, s.limits.ToolCallsPerTurn, 0, runID, modelStep)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := s.writeToolBlockInTx(ctx, tx, runID, id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) toolTitle(key string) string {
	for _, entry := range s.registeredTools() {
		if entry.Key == key {
			return entry.DisplayName
		}
	}
	return key
}

// settleBlocksInTx ends every block still in progress when the run reaches a
// terminal state. A plan of a run that succeeded is complete; anything else
// still open was cut short.
func settleBlocksInTx(ctx context.Context, tx store.TxAccountScope, runID string, succeeded bool) error {
	rows, err := tx.QueryPage(ctx, "creative_agent_blocks", "source_key,type", "run_id=$2 AND finished_at IS NULL",
		[]store.OrderBy{{Column: "ordinal"}}, maxBlocksPerRun, 0, runID)
	if err != nil {
		return err
	}
	type open struct{ key, kind string }
	var blocks []open
	for rows.Next() {
		var b open
		if err = rows.Scan(&b.key, &b.kind); err != nil {
			rows.Close()
			return err
		}
		blocks = append(blocks, b)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, b := range blocks {
		state := BlockInterrupted
		if succeeded && b.kind == BlockPlan {
			state = "complete"
		}
		if _, err := writeBlockInTx(ctx, tx, runID, b.key, blockChange{Type: b.kind, State: state}); err != nil {
			return err
		}
	}
	return nil
}

// maxBlocksPerRun bounds every per-run block read. A run's blocks are bounded
// by its tool calls and model turns, each of which yields a handful at most.
const maxBlocksPerRun = 2000

// currentBlockID is the step the status bar should name: the block the run is
// waiting on, otherwise the latest one still in progress.
func currentBlockID(ctx context.Context, tx store.TxAccountScope, runID string) (string, error) {
	var waiting *string
	if err := tx.QueryRow(ctx, "creative_agent_runs", "waiting_block_id", "id=$2", runID).Scan(&waiting); err != nil {
		return "", err
	}
	if waiting != nil {
		return *waiting, nil
	}
	rows, err := tx.QueryPage(ctx, "creative_agent_blocks", "id", "run_id=$2 AND finished_at IS NULL AND NOT hidden",
		[]store.OrderBy{{Column: "ordinal", Desc: true}}, 1, 0, runID)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	id := ""
	if rows.Next() {
		if err := rows.Scan(&id); err != nil {
			return "", err
		}
	}
	return id, rows.Err()
}

// runStateChanged is the run.state_changed payload. current_block_id lets the
// status bar name the current step without scanning the list.
func runStateChangedInTx(ctx context.Context, tx store.TxAccountScope, runID string, fields map[string]string) error {
	current, err := currentBlockID(ctx, tx, runID)
	if err != nil {
		return err
	}
	if current != "" {
		fields["current_block_id"] = current
	}
	_, err = appendRunEventInTx(ctx, tx, runID, "run.state_changed", fields)
	return err
}

// validPlan bounds what update_plan may show. A plan is a short list the
// photographer reads at a glance, not a document.
func validPlan(steps []PlanStep) error {
	if len(steps) == 0 || len(steps) > 20 {
		return creativeops.ErrValidation
	}
	seen := map[string]bool{}
	for _, step := range steps {
		title := strings.TrimSpace(step.Title)
		if step.Key == "" || len(step.Key) > 64 || seen[step.Key] || title == "" || len(title) > 200 {
			return creativeops.ErrValidation
		}
		switch step.State {
		case PlanTodo, PlanDoing, PlanDone, PlanSkipped:
		default:
			return creativeops.ErrValidation
		}
		seen[step.Key] = true
	}
	return nil
}

// planState is complete once nothing is left to do.
func planState(steps []PlanStep) string {
	for _, step := range steps {
		if step.State == PlanTodo || step.State == PlanDoing {
			return "active"
		}
	}
	return "complete"
}
