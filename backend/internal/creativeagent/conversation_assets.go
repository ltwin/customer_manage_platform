package creativeagent

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// The latest matching occurrence is picked within the run's visibility fence,
// after source/text filtering and before pagination. A secondary revision cursor
// preserves multiple refs in one message.
const conversationRefFilter = `
 ($5='' OR content_revision_id IN (SELECT id FROM creative_content_revisions WHERE account_id=$1 AND content_id IN (SELECT id FROM creative_contents WHERE account_id=$1 AND kind=$5)))
 AND ($6='' OR ($6='assistant' AND role='result') OR ($6='photographer' AND role IN ('input','attachment')))
 AND NOT EXISTS (SELECT 1 FROM creative_message_content_refs sibling WHERE sibling.account_id=$1 AND sibling.message_id=creative_message_content_refs.message_id AND sibling.content_revision_id=creative_message_content_refs.content_revision_id AND sibling.role<creative_message_content_refs.role AND ($6='' OR ($6='assistant' AND sibling.role='result') OR ($6='photographer' AND sibling.role IN ('input','attachment'))))
 AND NOT EXISTS (SELECT 1 FROM creative_message_content_refs newer JOIN creative_agent_messages m ON m.account_id=newer.account_id AND m.id=newer.message_id WHERE newer.account_id=$1 AND newer.content_revision_id=creative_message_content_refs.content_revision_id AND m.conversation_id=$2 AND m.status='complete' AND (m.ordinal<$3 OR m.run_id=$4) AND ($6='' OR ($6='assistant' AND newer.role='result') OR ($6='photographer' AND newer.role IN ('input','attachment'))) AND ($9='' OR m.body::text ILIKE '%'||$9||'%') AND m.ordinal>(SELECT ordinal FROM creative_agent_messages WHERE account_id=$1 AND id=creative_message_content_refs.message_id))`

type conversationAsset struct {
	RevisionID string    `json:"content_revision_id"`
	Kind       string    `json:"kind"`
	Source     string    `json:"source"`
	Role       string    `json:"role"`
	Tool       string    `json:"tool,omitempty"`
	Ordinal    string    `json:"message_ordinal"`
	Excerpt    string    `json:"excerpt"`
	CreatedAt  time.Time `json:"created_at"`
}

func (h *runtimeHandler) listConversationAssets(ctx context.Context, raw string) (string, error) {
	var a struct {
		Kind     string `json:"kind"`
		Source   string `json:"source"`
		Query    string `json:"query"`
		Before   int64  `json:"before_ordinal"`
		BeforeID string `json:"before_revision_id"`
		Limit    int    `json:"limit"`
	}
	if err := strictArguments(raw, &a); err != nil {
		return "", err
	}
	if a.Limit < 1 || a.Limit > 20 || a.Before < 0 || a.Before == 0 && a.BeforeID != "" || a.Kind != "" && !slices.Contains(nodeTypes, a.Kind) || a.Source != "" && a.Source != "photographer" && a.Source != "assistant" || len([]rune(a.Query)) > 200 {
		return "", creativeops.ErrValidation
	}
	items := []conversationAsset{}
	err := h.scope.WithReadSnapshot(ctx, func(tx store.ReadTxAccountScope) error {
		var triggerID string
		var trigger int64
		if err := tx.QueryRow(ctx, "creative_agent_runs", "trigger_message_id", "id=$2 AND conversation_id=$3", h.held.RunID, h.held.ConversationID).Scan(&triggerID); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, "creative_agent_messages", "ordinal", "id=$2", triggerID).Scan(&trigger); err != nil {
			return err
		}
		cond := `conversation_id=$2 AND status='complete' AND (ordinal<$3 OR run_id=$4) AND ($7::bigint=0 OR ordinal<$7 OR (ordinal=$7 AND $8<>'')) AND ($9='' OR body::text ILIKE '%'||$9||'%') AND id IN (SELECT message_id FROM creative_message_content_refs WHERE account_id=$1 AND ` + conversationRefFilter + ` AND ($7::bigint=0 OR message_id NOT IN (SELECT id FROM creative_agent_messages WHERE account_id=$1 AND conversation_id=$2 AND ordinal=$7) OR content_revision_id>$8))`
		rows, err := tx.QueryPage(ctx, "creative_agent_messages", "id,ordinal,body,created_at", cond, []store.OrderBy{{Column: "ordinal", Desc: true}}, a.Limit+1, 0, h.held.ConversationID, trigger, h.held.RunID, a.Kind, a.Source, a.Before, a.BeforeID, a.Query)
		if err != nil {
			return err
		}
		type message struct {
			id      string
			ordinal int64
			body    []byte
			created time.Time
		}
		var messages []message
		for rows.Next() {
			var m message
			if err := rows.Scan(&m.id, &m.ordinal, &m.body, &m.created); err != nil {
				rows.Close()
				return err
			}
			messages = append(messages, m)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, m := range messages {
			refs, err := tx.QueryPage(ctx, "creative_message_content_refs", "content_revision_id,role", "message_id=$7 AND "+conversationRefFilter+" AND ($8='' OR content_revision_id>$8)", []store.OrderBy{{Column: "content_revision_id"}}, a.Limit+1-len(items), 0, h.held.ConversationID, trigger, h.held.RunID, a.Kind, a.Source, m.id, func() string {
				if m.ordinal == a.Before {
					return a.BeforeID
				}
				return ""
			}(), a.Query)
			if err != nil {
				return err
			}
			var found []conversationAsset
			var body Body
			if err := json.Unmarshal(m.body, &body); err != nil {
				refs.Close()
				return err
			}
			for refs.Next() {
				item := conversationAsset{Ordinal: strconv.FormatInt(m.ordinal, 10), Excerpt: excerpt(renderedText(m.body), 200), Tool: body.ToolName, CreatedAt: m.created}
				if contentReadTool(body.ToolName) {
					item.Excerpt = "曾读取内容；正文请用 read_asset 重新读取"
				}
				if err := refs.Scan(&item.RevisionID, &item.Role); err != nil {
					refs.Close()
					return err
				}
				item.Source = "photographer"
				if item.Role == "result" {
					item.Source = "assistant"
				}
				found = append(found, item)
			}
			err = refs.Err()
			refs.Close()
			if err != nil {
				return err
			}
			for _, item := range found {
				if err := tx.QueryRow(ctx, "creative_contents", "kind", "id IN (SELECT content_id FROM creative_content_revisions WHERE account_id=$1 AND id=$2)", item.RevisionID).Scan(&item.Kind); err != nil {
					return err
				}
				items = append(items, item)
			}
			if len(items) > a.Limit {
				break
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	more := len(items) > a.Limit
	if more {
		items = items[:a.Limit]
	}
	next, nextID := "0", ""
	if more {
		last := items[len(items)-1]
		next, nextID = last.Ordinal, last.RevisionID
	}
	out, err := json.Marshal(map[string]any{"items": items, "has_more": more, "next_before_ordinal": next, "next_before_revision_id": nextID})
	return string(out), err
}
