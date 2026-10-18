package creativeagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/cloudwego/eino/schema"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativedocument"
	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

type historyItem struct {
	Role  string
	Text  string
	Body  Body
	Group string
	// Reasoning is kept only for a thinking model, which needs it back.
	Reasoning string
	Ordinal   int64
}

const perMessageOverhead = 16

// historyMessageCap bounds how many messages the frozen history may become,
// counting each located reference as its own message. The gateway accepts at
// most 400 per request, and the submission and the run's own turns need room.
const historyMessageCap = 240

// historyBudget is what history may spend of one request: tokens against the
// model's window and bytes against the gateway's hold, both at once.
type historyBudget struct {
	Tokens int
	Bytes  int
	Ratio  tokenRatio
}

func (b historyBudget) without(bytes int) historyBudget {
	b.Tokens -= b.Ratio.tokens(bytes)
	b.Bytes -= bytes
	return b
}

func (b historyBudget) availableBytes() int {
	return max(0, min(b.Bytes, b.Ratio.bytesFor(b.Tokens)))
}

// historyTurn is one complete turn after the checkpoint, oldest first.
type historyTurn struct {
	Items  []historyItem
	Last   int64
	Bytes  int
	Tokens int
	Refs   int
}

// historyProjection is what a run will carry instead of the conversation
// itself: the latest checkpoint's summary, the creator's own words from before
// it, the index of skills used, and the newest whole turns that fit.
type historyProjection struct {
	Checkpoint *compactionCheckpoint
	// Originals are the creator's words before the checkpoint, oldest first.
	Originals []string
	// OriginalsCut says the oldest kept original was shortened to fit.
	OriginalsCut bool
	SkillIndex   []skillIndexEntry
	// Turns are every complete turn after the checkpoint, before the budget.
	Turns []historyTurn
	// Kept is how many of the newest Turns fit the budget.
	Kept int
	// Unscanned says the scan bound stopped before the checkpoint, so older
	// turns exist that Turns does not show.
	Unscanned bool
}

// Items are the retained turns flattened, oldest first.
func (p historyProjection) Items() []historyItem {
	var out []historyItem
	for _, turn := range p.Turns[len(p.Turns)-p.Kept:] {
		out = append(out, turn.Items...)
	}
	return out
}

// isCommand reports a message that asked the assistant to do something to the
// conversation rather than said something in it. It never becomes history.
func isCommand(b Body) bool {
	for _, block := range b.Blocks {
		if block.Type == "command" {
			return true
		}
	}
	return false
}

func itemBytes(item historyItem) int {
	return bodyBytes(item.Body) + len(item.Reasoning) + perMessageOverhead
}

// projectionQuery asks what one run would carry as history.
type projectionQuery struct {
	ConversationID string
	// Before bounds the messages considered (exclusive), so a worker freezing
	// history after its own trigger was appended still sees the conversation
	// as the creator left it; zero means everything.
	Before int64
	Budget historyBudget
	// WithReasoning keeps each assistant turn's reasoning for a thinking
	// model; it spends the same budget as the words around it.
	WithReasoning bool
	// CurrentSkill is the version the run fixes itself, left out of the index.
	CurrentSkill string
	// Checkpoint, when set, stands in for the latest stored one: a consumer
	// projects from the checkpoint it is about to insert.
	Checkpoint *compactionCheckpoint
}

func runProjectionQuery(held claim, before int64, budget historyBudget) projectionQuery {
	q := projectionQuery{ConversationID: held.ConversationID, Before: before, Budget: budget, WithReasoning: held.Model.HandsBackReasoning()}
	if held.Skill != nil {
		q.CurrentSkill = held.Skill.ID
	}
	return q
}

// projectHistoryInTx reads what a run would carry instead of the
// conversation. Whole run groups are retained together so no orphan tool
// response reaches the model.
func (s *Service) projectHistoryInTx(ctx context.Context, tx store.TxAccountScope, q projectionQuery) (historyProjection, error) {
	var out historyProjection
	if s.limits.HistoryMessages <= 0 {
		return out, nil
	}
	conversationID, before, budget, withReasoning := q.ConversationID, q.Before, q.Budget, q.WithReasoning
	checkpoint := q.Checkpoint
	if checkpoint == nil {
		var err error
		if checkpoint, err = latestCompactionInTx(ctx, tx, conversationID); err != nil {
			return out, err
		}
	}
	if checkpoint != nil && before > 0 && checkpoint.CoversThrough >= before {
		checkpoint = nil // A checkpoint can never cover the turn being asked.
	}
	out.Checkpoint = checkpoint
	// A smaller model or a larger submission may leave no room even for the
	// summary. Fall back to original turns for this projection, without changing
	// the stored checkpoint. Never freeze an untrimmable input that cannot fit.
	if out.fixedBytes() > budget.availableBytes() {
		checkpoint, out.Checkpoint = nil, nil
	}
	var err error
	if out.SkillIndex, err = skillIndexInTx(ctx, tx, conversationID, q.CurrentSkill); err != nil {
		return out, err
	}
	for len(out.SkillIndex) > 0 && out.fixedBytes() > budget.availableBytes() {
		out.SkillIndex = out.SkillIndex[:len(out.SkillIndex)-1]
	}
	from := int64(0)
	if checkpoint != nil {
		from = checkpoint.CoversThrough
	}
	if before <= 0 {
		before = 1<<62 - 1
	}
	rows, err := tx.QueryPage(ctx, "creative_agent_messages", "role,body,run_id,id,reasoning,ordinal",
		"conversation_id=$2 AND status='complete' AND role IN ('user','assistant','tool') AND ordinal>$3 AND ordinal<$4",
		[]store.OrderBy{{Column: "ordinal", Desc: true}}, s.limits.HistoryMessages, 0, conversationID, from, before)
	if err != nil {
		return out, err
	}
	var items []historyItem
	read := 0
	for rows.Next() {
		read++
		var item historyItem
		var payload []byte
		var runID *string
		var messageID string
		var reasoning *string
		if err = rows.Scan(&item.Role, &payload, &runID, &messageID, &reasoning, &item.Ordinal); err != nil {
			rows.Close()
			return out, err
		}
		item.Group = messageID
		if reasoning != nil && withReasoning {
			item.Reasoning = *reasoning
		}
		if runID != nil {
			item.Group = *runID
		}
		if err = json.Unmarshal(payload, &item.Body); err != nil {
			rows.Close()
			return out, err
		}
		if isCommand(item.Body) {
			continue
		}
		item.Text = renderedText(payload)
		items = append(items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	out.Unscanned = read == s.limits.HistoryMessages
	slices.Reverse(items)
	// Drop the leading partial run when the row bound starts inside a tool pair.
	for len(items) > 0 && items[0].Role != "user" {
		items = items[1:]
	}
	for begin := 0; begin < len(items); {
		end := begin + 1
		for end < len(items) && items[end].Group == items[begin].Group {
			end++
		}
		group := items[begin:end]
		begin = end
		turn := historyTurn{Items: group, Last: group[len(group)-1].Ordinal}
		calls := map[string]bool{}
		results := map[string]bool{}
		for _, item := range group {
			turn.Bytes += itemBytes(item)
			for _, call := range item.Body.ToolCalls {
				calls[call.ID] = true
			}
			if item.Body.ToolCallID != "" {
				results[item.Body.ToolCallID] = true
			}
			turn.Refs++
			if item.Role != "tool" {
				for _, block := range item.Body.Blocks {
					if block.Type == "content_ref" {
						turn.Refs++
					}
				}
			}
		}
		valid := len(calls) == len(results)
		for id := range calls {
			valid = valid && results[id]
		}
		if !valid {
			continue
		}
		turn.Tokens = budget.Ratio.tokens(turn.Bytes)
		out.Turns = append(out.Turns, turn)
	}
	if checkpoint != nil {
		// The turns deliberately left out of the summary take precedence over
		// duplicate creator originals. Reserve the newest complete suffix that
		// fits, before originals compete for the remaining room.
		originalBudget := budget
		recent := budget.without(out.fixedBytes())
		messages := historyMessageCap
		for i := len(out.Turns) - 1; i >= max(0, len(out.Turns)-s.limits.CompactRecentTurns); i-- {
			turn := out.Turns[i]
			if turn.Tokens > recent.Tokens || turn.Bytes > recent.Bytes || turn.Refs > messages {
				break
			}
			recent.Tokens, recent.Bytes, messages = recent.Tokens-turn.Tokens, recent.Bytes-turn.Bytes, messages-turn.Refs
			originalBudget.Tokens -= turn.Tokens
			originalBudget.Bytes -= turn.Bytes
		}
		// Reserve the rendered heading (including the truncation notice) before
		// filling the rest with originals. Newlines also cost indentation bytes.
		headed := out
		headed.Originals, headed.OriginalsCut = []string{""}, true
		room := originalBudget.availableBytes() - headed.fixedBytes() + originalBytes("")
		if out.Originals, out.OriginalsCut, err = s.creatorOriginalsInTx(ctx, tx, conversationID, checkpoint.CoversThrough, budget.Ratio, room); err != nil {
			return out, err
		}
	}
	// What the fixed parts cost is taken before any turn competes for the rest.
	fixed := out.fixedBytes()
	tokens, bytes, messages := budget.Tokens-budget.Ratio.tokens(fixed), budget.Bytes-fixed, historyMessageCap
	for i := len(out.Turns) - 1; i >= 0; i-- {
		turn := out.Turns[i]
		if turn.Tokens > tokens || turn.Bytes > bytes || turn.Refs > messages {
			break
		}
		tokens, bytes, messages = tokens-turn.Tokens, bytes-turn.Bytes, messages-turn.Refs
		out.Kept++
	}
	return out, nil
}

// fixedBytes is what the summary, the originals and the skill index cost.
func (p historyProjection) fixedBytes() int {
	total := 0
	if p.Checkpoint != nil {
		total += len(compactionText(p)) + perMessageOverhead
		if len(p.Originals) > 0 {
			total += perMessageOverhead // Originals are a separately trimmable message.
		}
	}
	if len(p.SkillIndex) > 0 {
		total += len(skillIndexText(p.SkillIndex)) + perMessageOverhead
	}
	return total
}

// estimatedTokens is the whole projection, every turn included, as the
// trigger sees it: what the conversation would cost if nothing were dropped.
func (p historyProjection) estimatedTokens(ratio tokenRatio) int {
	total := ratio.tokens(p.fixedBytes())
	for _, turn := range p.Turns {
		total += turn.Tokens
	}
	return total
}

// composition is the projection's share of a request, by part.
func (p historyProjection) composition() contextComposition {
	c := contextComposition{Bytes: map[string]int{}}
	if p.Checkpoint != nil {
		c.Bytes[partSummary] += len(compactionText(p)) + perMessageOverhead
		if len(p.Originals) > 0 {
			c.Bytes[partSummary] += perMessageOverhead
		}
	}
	if len(p.SkillIndex) > 0 {
		c.Bytes[partSkill] += len(skillIndexText(p.SkillIndex)) + perMessageOverhead
	}
	for _, item := range p.Items() {
		part := partMessages
		if item.Role == "tool" {
			part = partToolResults
		}
		c.Bytes[part] += itemBytes(item)
	}
	return c
}

// creatorOriginalsInTx keeps the creator's own words from before a checkpoint,
// newest first within the budget, so a preference is never only known
// through the summary's paraphrase of it (Codex keeps the same 20k).
func (s *Service) creatorOriginalsInTx(ctx context.Context, tx store.TxAccountScope, conversationID string, through int64, ratio tokenRatio, maxBytes int) ([]string, bool, error) {
	budget := s.limits.CompactUserTokens
	if budget <= 0 || maxBytes <= originalBytes("") {
		return nil, false, nil
	}
	rows, err := tx.QueryPage(ctx, "creative_agent_messages", "body",
		"conversation_id=$2 AND status='complete' AND role='user' AND ordinal<=$3",
		[]store.OrderBy{{Column: "ordinal", Desc: true}}, s.limits.HistoryMessages, 0, conversationID, through)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	var out []string
	cut := false
	for rows.Next() && budget > 0 && maxBytes > originalBytes("") {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return nil, false, err
		}
		var body Body
		if json.Unmarshal(payload, &body) != nil || isCommand(body) {
			continue
		}
		text := strings.TrimSpace(renderedText(payload))
		if text == "" {
			continue
		}
		original := text
		text = utf8Prefix(text, ratio.bytesFor(budget))
		text = fitOriginal(text, maxBytes)
		if text == "" {
			break
		}
		cut = cut || text != original
		budget -= ratio.tokens(len(text))
		maxBytes -= originalBytes(text)
		out = append(out, text)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	slices.Reverse(out)
	return out, cut, nil
}

// originalBytes matches the bullet and continuation indentation in
// compactionText. Budgeting just raw text misses the cost of multiline input.
func originalBytes(text string) int {
	return len(text) + 2*strings.Count(text, "\n") + 3
}

func fitOriginal(text string, bytes int) string {
	if originalBytes(text) <= bytes {
		return text
	}
	lo, hi := 0, len(text)
	for lo < hi {
		mid := lo + (hi-lo+1)/2
		if originalBytes(utf8Prefix(text, mid)) <= bytes {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return utf8Prefix(text, lo)
}

func renderedText(payload []byte) string {
	var body Body
	if unmarshalBody(payload, &body) != nil {
		return ""
	}
	var pieces []string
	for _, block := range body.Blocks {
		if block.Type == "text" && block.Text != "" {
			pieces = append(pieces, block.Text)
		}
	}
	return strings.Join(pieces, "\n")
}

// deferredHistoryOrdinal is where a run's own inputs start when its history
// is frozen later, by the worker, after compacting. The history then takes the
// ordinals below, so the frozen inputs still read in conversation order.
const deferredHistoryOrdinal = 1 << 20

// inputWriter collects one run's frozen inputs from a starting ordinal and
// writes them in one statement: a long history is hundreds of rows, and one
// round trip each made starting a run slow wherever the database is not
// next door.
type inputWriter struct {
	tx      store.TxAccountScope
	runID   string
	ordinal int
	rows    [][]any
}

var inputColumns = []string{"run_id", "batch_ordinal", "ordinal", "input_role", "read_level", "body", "content_revision_id", "source_revision_snapshot"}

func (w *inputWriter) insert(_ context.Context, role, level string, body, id *string, snapshot any) error {
	w.rows = append(w.rows, []any{w.runID, initialInputBatch, w.ordinal, role, level, body, id, snapshot})
	w.ordinal++
	return nil
}

// flush writes what was collected. Nothing is stored until it runs.
func (w *inputWriter) flush(ctx context.Context) error {
	rows := w.rows
	w.rows = nil
	return w.tx.InsertRows(ctx, "creative_run_inputs", inputColumns, rows)
}

// freezeHistoryInputsInTx writes a projection as the run's history: the skill
// index, the checkpoint's summary with the creator's words before it, then the
// retained turns. History ordinals start at zero; the next free one is
// returned for whatever the run freezes after it.
func freezeHistoryInputsInTx(ctx context.Context, tx store.TxAccountScope, runID string, p historyProjection) (int, error) {
	w := &inputWriter{tx: tx, runID: runID}
	if err := w.history(ctx, p); err != nil {
		return w.ordinal, err
	}
	return w.ordinal, w.flush(ctx)
}

func (w *inputWriter) history(ctx context.Context, p historyProjection) error {
	if len(p.SkillIndex) > 0 {
		text := skillIndexText(p.SkillIndex)
		snapshot, err := jsonValue(map[string]any{"entries": p.SkillIndex})
		if err != nil {
			return err
		}
		if err := w.insert(ctx, inputSkillIndex, "text", &text, nil, snapshot); err != nil {
			return err
		}
	}
	if p.Checkpoint != nil {
		if len(p.Originals) > 0 {
			text := compactionOriginalsText(p)
			snapshot, err := jsonValue(map[string]any{"compaction_id": p.Checkpoint.ID, "originals": true})
			if err != nil {
				return err
			}
			if err := w.insert(ctx, inputCompaction, "text", &text, nil, snapshot); err != nil {
				return err
			}
		}
		text := compactionText(historyProjection{Checkpoint: p.Checkpoint})
		snapshot, err := jsonValue(map[string]any{"compaction_id": p.Checkpoint.ID})
		if err != nil {
			return err
		}
		if err := w.insert(ctx, inputCompaction, "text", &text, nil, snapshot); err != nil {
			return err
		}
	}
	refs := map[string]bool{}
	for _, item := range p.Items() {
		frozen := map[string]any{"role": item.Role, "body": item.Body, "group": item.Group}
		if item.Reasoning != "" {
			frozen["reasoning"] = item.Reasoning
		}
		snapshot, err := jsonValue(frozen)
		if err != nil {
			return err
		}
		if err = w.insert(ctx, "history", "text", &item.Text, nil, snapshot); err != nil {
			return err
		}
		for _, block := range item.Body.Blocks {
			if block.Type == "content_ref" && !refs[block.RefID] {
				id := block.RefID
				if err = w.insert(ctx, "attachment", "metadata_only", nil, &id, nil); err != nil {
					return err
				}
				refs[id] = true
			}
		}
	}
	if w.ordinal >= deferredHistoryOrdinal {
		return ErrLimit
	}
	return nil
}

// freezeRequestInputsInTx writes the submission itself, the canvas line and
// the scene lines, from start: directly after the history when it was frozen
// first, or deferredHistoryOrdinal when the worker will freeze it after
// compacting.
func (s *Service) freezeRequestInputsInTx(ctx context.Context, tx store.TxAccountScope, runID string, blocks []Block, scenes []sceneInput, start int) error {
	w := &inputWriter{tx: tx, runID: runID, ordinal: start}
	for _, block := range blocks {
		switch block.Type {
		case "text":
			text := block.Text
			if err := w.insert(ctx, "instruction", "text", &text, nil, nil); err != nil {
				return err
			}
		case "content_ref":
			id := block.RefID
			revision, err := creativecontent.RequireUsable(ctx, tx, id, "display")
			if err != nil {
				return translateContent(err)
			}
			level := "text"
			if revision.Kind == "image" {
				level = "image_preview"
			}
			if err := w.insert(ctx, "instruction", level, nil, &id, nil); err != nil {
				return err
			}
		}
	}
	var canvasID, consentID string
	if err := tx.QueryRow(ctx, "creative_agent_runs", "canvas_id,egress_consent_id", "id=$2", runID).Scan(&canvasID, &consentID); err != nil {
		return err
	}
	consent, err := readConsentForUpdate(ctx, tx, consentID)
	if err != nil {
		return err
	}
	if consentHas(consent, "canvas_metadata") {
		canvas, err := canvasSummary(ctx, tx, canvasID)
		if err != nil {
			return err
		}
		text, err := marshalContext(canvas)
		if err != nil {
			return err
		}
		if err := w.insert(ctx, "selection", "metadata_only", &text, nil, nil); err != nil {
			return err
		}
	}
	for _, scene := range scenes {
		text := scene.Text
		snapshot, err := jsonValue(scene.Snapshot)
		if err != nil {
			return err
		}
		if err := w.insert(ctx, "selection", "metadata_only", &text, nil, snapshot); err != nil {
			return err
		}
	}
	return w.flush(ctx)
}

// SceneContext is where the photographer asked from: the scene open in the
// editor and the objects selected there. A run started from the canvas has
// none.
type SceneContext struct {
	DocumentID        string   `json:"document_id"`
	SelectedObjectIDs []string `json:"selected_object_ids"`
}

const (
	maxSceneSelection = 50
	// maxRecentScenes is how many scenes the conversation wrote last that a
	// new run hears the changes of, besides the one being edited.
	maxRecentScenes = 3
)

func (c *SceneContext) valid() bool {
	if c == nil {
		return true
	}
	if c.DocumentID == "" || len(c.SelectedObjectIDs) > maxSceneSelection {
		return false
	}
	return !slices.Contains(c.SelectedObjectIDs, "")
}

// sceneInput is one scene line a run is told at its start. It is frozen with
// the run's inputs, so every attempt sends the same request (a summary read
// afresh would change the request hash a rerun is checked against).
type sceneInput struct {
	Text     string
	Snapshot map[string]any
}

// sceneInputsInTx words what a new run is told about scenes: the scene being
// edited with its selection, then what others changed in it and in the scenes
// this conversation wrote last since the conversation's runs last wrote them.
func sceneInputsInTx(ctx context.Context, tx store.TxAccountScope, conversationID string, scene *SceneContext) ([]sceneInput, error) {
	var out []sceneInput
	var docs []string
	if scene != nil {
		editor, err := creativescene.EditorContextInTx(ctx, tx, scene.DocumentID, scene.SelectedObjectIDs)
		if errors.Is(err, creativedocument.ErrNotFound) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		out = append(out, sceneInput{Text: editor.Prompt, Snapshot: map[string]any{"kind": "scene_context",
			"document_id": editor.DocumentID, "revision": editor.Revision, "selected_object_ids": editor.Selected}})
		docs = append(docs, scene.DocumentID)
	}
	runs, err := conversationRunsInTx(ctx, tx, conversationID)
	if err != nil {
		return nil, err
	}
	limit := maxRecentScenes + len(docs)
	recent, err := creativedocument.RunDocuments(ctx, tx, creativescene.TypeKey, runs, limit)
	if err != nil {
		return nil, err
	}
	for _, id := range recent {
		if len(docs) < limit && !slices.Contains(docs, id) {
			docs = append(docs, id)
		}
	}
	for _, id := range docs {
		summary, err := creativescene.ChangesSinceInTx(ctx, tx, id, runs)
		if err != nil {
			return nil, err
		}
		if summary == nil || len(summary.Lines) == 0 {
			continue
		}
		out = append(out, sceneInput{Text: summary.Prompt(), Snapshot: map[string]any{"kind": "scene_changes",
			"document_id": id, "since_revision": summary.SinceRevision, "revision": summary.Revision}})
	}
	return out, nil
}

func sceneInputBytes(scenes []sceneInput) int {
	total := 0
	for _, s := range scenes {
		total += len(s.Text) + perMessageOverhead
	}
	return total
}

// conversationRunsInTx is every run of the conversation, the writers whose
// scene revisions are the conversation's own.
func conversationRunsInTx(ctx context.Context, tx store.TxAccountScope, conversationID string) ([]string, error) {
	rows, err := tx.Query(ctx, "creative_agent_runs", "id", "conversation_id=$2", conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func bodyMessage(body Body, role string) *schema.Message {
	m := &schema.Message{Role: schema.RoleType(role), ToolCallID: body.ToolCallID, ToolName: body.ToolName}
	var parts []string
	for _, b := range body.Blocks {
		if b.Type == "text" {
			parts = append(parts, b.Text)
		}
		if b.Type == "content_ref" && role != "tool" {
			parts = append(parts, referenceLabel(b.RefID))
		}
	}
	m.Content = strings.Join(parts, "\n")
	for i, c := range body.ToolCalls {
		index := i
		m.ToolCalls = append(m.ToolCalls, schema.ToolCall{Index: &index, ID: c.ID, Type: "function", Function: schema.FunctionCall{Name: c.Name, Arguments: c.Arguments}})
	}
	return m
}

func historyTag(m *schema.Message, group string) {
	if m.Extra == nil {
		m.Extra = map[string]any{}
	}
	m.Extra["creative_history_group"] = fmt.Sprint(group)
}

const initialInputBatch = 0
