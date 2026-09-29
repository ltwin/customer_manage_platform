package creativecanvas

import (
	"context"
	"errors"

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func snapshotRows(ctx context.Context, tx canvasReader, table, columns, condition string, scan func(store.Row) error, args ...any) error {
	rows, err := tx.Query(ctx, table, columns, condition, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := scan(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}

func fillNodeViews(ctx context.Context, tx store.ReadTxAccountScope, c *Canvas) error {
	if len(c.Nodes) == 0 {
		return nil
	}
	nodes := make(map[string]*Node, len(c.Nodes))
	revisionIDs, documentIDs, executionIDs := []string{}, []string{}, []string{}
	for i := range c.Nodes {
		n := &c.Nodes[i]
		nodes[n.ID] = n
		n.ReadOnly = c.Archived
		if n.ContentRevisionID != nil && knownType(n.TypeKey) {
			revisionIDs = append(revisionIDs, *n.ContentRevisionID)
		}
		if n.DocumentID != nil && n.TypeKey == "internal.document" {
			documentIDs = append(documentIDs, *n.DocumentID)
		}
		if n.LatestExecutionID != nil {
			executionIDs = append(executionIDs, *n.LatestExecutionID)
		}
	}
	prompts := map[string]*NodePrompt{}
	err := snapshotRows(ctx, tx, "creative_node_prompt_drafts", "id,node_id,revision,action_key,text,model_key,parameters", "canvas_id=$2", func(row store.Row) error {
		var p NodePrompt
		var node string
		var revision int64
		if err := row.Scan(&p.ID, &node, &revision, &p.ActionKey, &p.Text, &p.ModelKey, &p.Parameters); err != nil {
			return err
		}
		p.Revision = creativeops.Revision(revision)
		p.References = []NodeInput{}
		if n := nodes[node]; n != nil {
			n.Prompt = &p
			prompts[p.ID] = &p
		}
		return nil
	}, c.ID)
	if err != nil {
		return err
	}
	rows, err := tx.QueryPage(ctx, "creative_node_prompt_refs", "id,node_id,slot,ordinal,role,source_node_id,content_revision_id,revision,draft_id", "canvas_id=$2", []store.OrderBy{{Column: "ordinal"}, {Column: "id"}}, len(c.Nodes)*51, 0, c.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var input NodeInput
		var revision int64
		if err := rows.Scan(&input.ID, &input.NodeID, &input.Slot, &input.Ordinal, &input.Role, &input.SourceNodeID, &input.ContentRevisionID, &revision, &input.DraftID); err != nil {
			rows.Close()
			return err
		}
		input.Revision = creativeops.Revision(revision)
		if input.DraftID != nil {
			if p := prompts[*input.DraftID]; p != nil {
				p.References = append(p.References, input)
			}
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	foundExecutions := map[string]bool{}
	if len(executionIDs) > 0 {
		err = snapshotRows(ctx, tx, "creative_node_executions", "id,node_id,state,apply_state,error", "canvas_id=$2 AND id=ANY($3::text[])", func(row store.Row) error {
			var id, node, state, apply, message string
			if err := row.Scan(&id, &node, &state, &apply, &message); err != nil {
				return err
			}
			if n := nodes[node]; n != nil && n.LatestExecutionID != nil && *n.LatestExecutionID == id {
				n.Status = NodeStatus{GenerationState: state, ApplyState: &apply}
				if message != "" {
					n.Status.Error = &message
				}
				foundExecutions[id] = true
			}
			return nil
		}, c.ID, executionIDs)
		if err != nil {
			return err
		}
		for _, id := range executionIDs {
			if !foundExecutions[id] {
				return store.ErrNoRows
			}
		}
	}
	documents := map[string]string{}
	if len(documentIDs) > 0 {
		err = snapshotRows(ctx, tx, "creative_documents", "id,content_revision_id", "id=ANY($2::text[])", func(row store.Row) error {
			var id, revision string
			if err := row.Scan(&id, &revision); err != nil {
				return err
			}
			documents[id] = revision
			revisionIDs = append(revisionIDs, revision)
			return nil
		}, documentIDs)
		if err != nil {
			return err
		}
	}
	content, err := creativecontent.ReadDisplayBatch(ctx, tx, revisionIDs)
	if err != nil {
		return err
	}
	for i := range c.Nodes {
		n := &c.Nodes[i]
		if n.DocumentID != nil && n.TypeKey == "internal.document" {
			revision, ok := documents[*n.DocumentID]
			if !ok {
				n.Unavailable = true
				continue
			}
			result := content[revision]
			if errors.Is(result.Err, creativecontent.ErrUsageDenied) {
				n.Unavailable = true
				continue
			}
			if result.Err != nil {
				return result.Err
			}
			preview := creativecontent.Preview(result.Revision)
			n.Content = &preview
		}
		if n.ContentRevisionID == nil || !knownType(n.TypeKey) {
			continue
		}
		result := content[*n.ContentRevisionID]
		if errors.Is(result.Err, creativecontent.ErrUsageDenied) {
			n.Unavailable = true
			continue
		}
		if result.Err != nil {
			return result.Err
		}
		r := result.Revision
		if n.ContentID == nil || r.ContentID != *n.ContentID || n.TypeKey != "core."+r.Kind {
			return creativecontent.ErrMissingRoot
		}
		preview := creativecontent.Preview(r)
		n.Content = &preview
	}
	return nil
}
