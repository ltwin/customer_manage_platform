package creativeagent

import (
	"context"
	"strings"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// historyItem is one earlier turn of the conversation as it will be sent. Only
// words travel: a reference block locates something, and re-sending what it
// located would be a second authorisation nobody asked for.
type historyItem struct {
	Role string
	Text string
}

// perMessageOverhead matches the gateway's own input estimate, which charges a
// fixed cost per message on top of its text. Counting history without it would
// let a hundred one-word turns look free and overrun the hold the run took.
const perMessageOverhead = 16

// freezeHistoryInTx picks the recent completed turns this run will carry, newest
// first and then reversed into reading order.
//
// It is selected once, inside the creation transaction, and written to
// creative_run_inputs — it is not re-read at dispatch. A run's inputs are
// historical facts: what the photographer saw when they pressed send is what
// the model is told, even if another window appends a message a moment later.
//
// budget is what is left of the run's per-request input ceiling after the
// submission and the skill instructions have taken their share, so the numbers
// are derived from one bound rather than split by a second invented one.
func (s *Service) freezeHistoryInTx(ctx context.Context, tx store.TxAccountScope, conversationID string, budget int) ([]historyItem, error) {
	if budget <= 0 || s.limits.HistoryMessages <= 0 {
		return nil, nil
	}
	rows, err := tx.QueryPage(ctx, "creative_agent_messages", "role,body",
		"conversation_id=$2 AND status='complete' AND role IN ('user','assistant')",
		[]store.OrderBy{{Column: "ordinal", Desc: true}}, s.limits.HistoryMessages, 0, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var newestFirst []historyItem
	for rows.Next() {
		var role string
		var payload []byte
		if err := rows.Scan(&role, &payload); err != nil {
			return nil, err
		}
		text := renderedText(payload)
		if text == "" {
			// A turn that showed only references has no words to re-send. It is
			// dropped rather than sent as an empty message, which some providers
			// refuse outright.
			continue
		}
		cost := len(text) + perMessageOverhead
		if cost > budget {
			// Older turns cost the same or more attention than newer ones and
			// are worth less; stopping here keeps the most recent exchange whole
			// instead of truncating one message into nonsense.
			break
		}
		budget -= cost
		newestFirst = append(newestFirst, historyItem{Role: role, Text: text})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i, j := 0, len(newestFirst)-1; i < j; i, j = i+1, j-1 {
		newestFirst[i], newestFirst[j] = newestFirst[j], newestFirst[i]
	}
	return newestFirst, nil
}

// renderedText is a stored body reduced to what a model can be told. Unknown
// block types from an older schema contribute nothing rather than being guessed
// at, and no reference is expanded into the words it points to.
func renderedText(payload []byte) string {
	var body Body
	if err := unmarshalBody(payload, &body); err != nil {
		return ""
	}
	var b strings.Builder
	for _, block := range body.Blocks {
		if block.Type != "text" || block.Text == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(block.Text)
	}
	return b.String()
}

// freezeInputsInTx writes the run's immutable input manifest. Ordinal is the
// order an item enters the prompt, which is the only ordering the assembler
// needs; history comes first because it is what the submission answers.
//
// A located revision is stored as a column rather than inside a payload,
// because these rows are retention roots: an id buried in JSON keeps nothing
// readable.
func (s *Service) freezeInputsInTx(ctx context.Context, tx store.TxAccountScope, runID string,
	history []historyItem, blocks []Block) error {
	ordinal := 0
	insert := func(role, readLevel string, body, revisionID *string, snapshot any) error {
		defer func() { ordinal++ }()
		return tx.Insert(ctx, "creative_run_inputs",
			[]string{"run_id", "batch_ordinal", "ordinal", "input_role", "read_level", "body", "content_revision_id", "source_revision_snapshot"},
			runID, initialInputBatch, ordinal, role, readLevel, body, revisionID, snapshot)
	}
	for _, item := range history {
		text := item.Text
		snapshot, err := jsonValue(map[string]string{"role": item.Role})
		if err != nil {
			return err
		}
		if err := insert("history", "text", &text, nil, snapshot); err != nil {
			return err
		}
	}
	for _, block := range blocks {
		switch block.Type {
		case "text":
			text := block.Text
			if err := insert("instruction", "text", &text, nil, nil); err != nil {
				return err
			}
		case "content_ref":
			revisionID := block.RefID
			if err := insert("instruction", "text", nil, &revisionID, nil); err != nil {
				return err
			}
		}
		// A skill block names the frozen version the run already carries as a
		// column; repeating it as an input would be a second place to disagree
		// about which instructions were followed.
	}
	return nil
}

// initialInputBatch is the group fixed when the run was created. A supplement
// appends a new batch rather than editing this one, so the original submission
// stays readable exactly as it was sent.
const initialInputBatch = 0
