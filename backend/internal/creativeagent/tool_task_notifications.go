package creativeagent

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// finishTaskInTx writes a task's terminal state. A background task also
// publishes its completion notice in the same transaction (design: 完成通知与
// 送达); a foreground task's result goes to the call that waits on it.
func (s *Service) finishTaskInTx(ctx context.Context, tx store.TxAccountScope, task toolTask, outcome toolTaskOutcome) error {
	if err := finishTaskRow(ctx, tx, task, outcome); err != nil {
		return err
	}
	if task.Mode != modeBackground {
		return nil
	}
	return s.publishNoticeInTx(ctx, tx, task, outcome)
}

// publishNoticeInTx writes a finished background task's notice: one system
// message in its conversation holding the result's revisions, and the task
// marked pending delivery. A project archived or a conversation deleted
// meanwhile suppresses it for good: restoring the project does not send it
// later. The (step, role) key on messages keeps a repeated settle to one
// notice. The caller holds the project lock and the task row.
func (s *Service) publishNoticeInTx(ctx context.Context, tx store.TxAccountScope, task toolTask, outcome toolTaskOutcome) error {
	suppress := func() error {
		_, err := tx.Update(ctx, "creative_agent_tool_tasks", "notification_state='suppressed',updated_at=clock_timestamp()",
			"id=$2 AND notification_state='none'", task.ID)
		return err
	}
	err := creativecanvas.LockProjectForWorkInTx(ctx, tx, task.CanvasID)
	if errors.Is(err, creativecanvas.ErrArchived) {
		return suppress()
	}
	if err != nil {
		return err
	}
	refs, err := resultRevisionsInTx(ctx, tx, outcome.Result)
	if err != nil {
		return err
	}
	message, err := s.appendMessageInTx(ctx, tx, task.ConversationID, newMessage{Role: "system", Status: "complete",
		Body:         Body{Blocks: []Block{{Type: "text", Text: noticeText(task.ToolKey, outcome.State)}}},
		SourceStepID: task.ToolStepID, ContentRefs: refs})
	if errors.Is(err, ErrNotFound) {
		return suppress()
	}
	if err != nil {
		return err
	}
	_, err = tx.Update(ctx, "creative_agent_tool_tasks", "notification_state='pending',notification_message_id=$3,updated_at=clock_timestamp()",
		"id=$2 AND notification_state='none'", task.ID, message.ID)
	return err
}

// noticeText is the one line the conversation shows; the structured result
// travels on the message's background_task.
func noticeText(toolKey, state string) string {
	verdict := map[string]string{taskSucceeded: "已完成", taskFailed: "失败", taskCancelled: "已取消", taskExpired: "已过期"}[state]
	return "后台任务" + verdict + "：" + toolTitle(toolKey)
}

// resultRevisionsInTx is what a tool result produced that the notice keeps
// readable after the run's payloads expire: the revisions it names that
// exist in this account.
func resultRevisionsInTx(ctx context.Context, tx store.TxAccountScope, result string) ([]ContentRef, error) {
	var fields struct {
		ContentRevisionIDs []string `json:"content_revision_ids"`
		ContentRevisionID  string   `json:"content_revision_id"`
		ImageRevisionID    string   `json:"image_revision_id"`
	}
	if err := json.Unmarshal([]byte(result), &fields); err != nil {
		return nil, err
	}
	named := append([]string{}, fields.ContentRevisionIDs...)
	for _, id := range []string{fields.ContentRevisionID, fields.ImageRevisionID} {
		if id != "" {
			named = append(named, id)
		}
	}
	if len(named) == 0 {
		return nil, nil
	}
	rows, err := tx.Query(ctx, "creative_content_revisions", "id", "id = ANY($2::text[])", named)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	exists := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		exists[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var refs []ContentRef
	seen := map[string]bool{}
	for _, id := range named {
		if exists[id] && !seen[id] {
			seen[id] = true
			refs = append(refs, ContentRef{RevisionID: id, Role: "result"})
		}
	}
	return refs, nil
}

// toolTasksByInTx reads the tasks a set of rows points at, keyed by the
// column they are found by (tool_step_id for cards, notification_message_id
// for notices).
func toolTasksByInTx(ctx context.Context, tx interface {
	Query(context.Context, string, string, string, ...any) (store.Rows, error)
}, column string, keys []string) (map[string]ToolTask, error) {
	out := map[string]ToolTask{}
	if len(keys) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, "creative_agent_tool_tasks", "id,mode,state,revision,result,finished_at,tool_key,source_run_id,tool_step_id,"+column,
		column+" = ANY($2::text[])", keys)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var t ToolTask
		var revision int64
		var result []byte
		var key string
		if err := rows.Scan(&t.Handle, &t.Mode, &t.State, &revision, &result, &t.FinishedAt, &t.ToolKey, &t.RunID, &t.ToolStepID, &key); err != nil {
			return nil, err
		}
		t.Revision, t.Result = creativeops.Revision(revision), result
		out[key] = t
	}
	return out, rows.Err()
}

// projectTaskOnPayload puts a card's current task into its tool block
// payload. The task's own revision orders it, not the block's.
func projectTaskOnPayload(payload json.RawMessage, task ToolTask) json.RawMessage {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		return payload
	}
	raw, err := json.Marshal(task)
	if err != nil {
		return payload
	}
	fields["task"] = raw
	out, err := json.Marshal(fields)
	if err != nil {
		return payload
	}
	return out
}
