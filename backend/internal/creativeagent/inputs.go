package creativeagent

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/cloudwego/eino/schema"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

type historyItem struct {
	Role  string
	Text  string
	Body  Body
	Group string
}

const perMessageOverhead = 16

// Freeze recent messages, including tool calls and results. Whole run groups
// are retained together so no orphan tool response reaches the model.
func (s *Service) freezeHistoryInTx(ctx context.Context, tx store.TxAccountScope, conversationID string, budget int) ([]historyItem, error) {
	if budget <= 0 || s.limits.HistoryMessages <= 0 {
		return nil, nil
	}
	rows, err := tx.QueryPage(ctx, "creative_agent_messages", "role,body,run_id,id", "conversation_id=$2 AND status='complete' AND role IN ('user','assistant','tool')", []store.OrderBy{{Column: "ordinal", Desc: true}}, s.limits.HistoryMessages, 0, conversationID)
	if err != nil {
		return nil, err
	}
	var items []historyItem
	for rows.Next() {
		var item historyItem
		var payload []byte
		var runID *string
		var messageID string
		if err = rows.Scan(&item.Role, &payload, &runID, &messageID); err != nil {
			rows.Close()
			return nil, err
		}
		item.Group = messageID
		if runID != nil {
			item.Group = *runID
		}
		if err = json.Unmarshal(payload, &item.Body); err != nil {
			rows.Close()
			return nil, err
		}
		item.Text = renderedText(payload)
		items = append(items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	slices.Reverse(items)
	// Drop the leading partial run when the row bound starts inside a tool pair.
	for len(items) > 0 && items[0].Role != "user" {
		items = items[1:]
	}
	var retained []historyItem
	for end := len(items); end > 0; {
		start := end - 1
		for start > 0 && items[start-1].Group == items[end-1].Group {
			start--
		}
		group := items[start:end]
		cost := 0
		calls := map[string]bool{}
		results := map[string]bool{}
		for _, item := range group {
			cost += bodyBytes(item.Body) + perMessageOverhead
			for _, call := range item.Body.ToolCalls {
				calls[call.ID] = true
			}
			if item.Body.ToolCallID != "" {
				results[item.Body.ToolCallID] = true
			}
		}
		valid := true
		for id := range calls {
			if !results[id] {
				valid = false
			}
		}
		for id := range results {
			if !calls[id] {
				valid = false
			}
		}
		if cost > budget {
			break
		}
		if valid {
			budget -= cost
			retained = append(append([]historyItem(nil), group...), retained...)
		}
		end = start
	}
	return retained, nil
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

func (s *Service) freezeInputsInTx(ctx context.Context, tx store.TxAccountScope, runID string, history []historyItem, blocks []Block) error {
	ordinal := 0
	insert := func(role, level string, body, id *string, snapshot any) error {
		defer func() { ordinal++ }()
		return tx.Insert(ctx, "creative_run_inputs", []string{"run_id", "batch_ordinal", "ordinal", "input_role", "read_level", "body", "content_revision_id", "source_revision_snapshot"}, runID, initialInputBatch, ordinal, role, level, body, id, snapshot)
	}
	refs := map[string]bool{}
	for _, item := range history {
		snapshot, err := jsonValue(map[string]any{"role": item.Role, "body": item.Body, "group": item.Group})
		if err != nil {
			return err
		}
		if err = insert("history", "text", &item.Text, nil, snapshot); err != nil {
			return err
		}
		for _, block := range item.Body.Blocks {
			if block.Type == "content_ref" && !refs[block.RefID] {
				id := block.RefID
				if err = insert("attachment", "metadata_only", nil, &id, nil); err != nil {
					return err
				}
				refs[id] = true
			}
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
			id := block.RefID
			revision, err := creativecontent.RequireUsable(ctx, tx, id, "display")
			if err != nil {
				return translateContent(err)
			}
			level := "text"
			if revision.Kind == "image" {
				level = "image_preview"
			}
			if err := insert("instruction", level, nil, &id, nil); err != nil {
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
		canvas, err := readCanvasContext(ctx, tx, canvasID, 0, 0)
		if err != nil {
			return err
		}
		text, err := marshalContext(canvas)
		if err != nil {
			return err
		}
		return insert("selection", "metadata_only", &text, nil, nil)
	}
	return nil
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
