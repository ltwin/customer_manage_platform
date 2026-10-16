package creativecanvas

import (
	"context"
	"encoding/json"
	"errors"
	"sort"

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativedocument"
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
		if n.DocumentID != nil {
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
	if err := fillVersionSummaries(ctx, tx, c.ID, nodes); err != nil {
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
	documents, err := creativedocument.SummaryBatch(ctx, tx, documentIDs)
	if err != nil {
		return err
	}
	content, err := creativecontent.ReadDisplayBatch(ctx, tx, revisionIDs)
	if err != nil {
		return err
	}
	for i := range c.Nodes {
		n := &c.Nodes[i]
		if n.DocumentID != nil {
			fillDocument(n, documents[*n.DocumentID])
			continue
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

// fillDocument shows the document behind a node. A purged document, an
// unregistered type or content no longer allowed for display reads as
// unavailable; a trashed document still shows its summary.
func fillDocument(n *Node, s creativedocument.NodeSummary) {
	trashed := s.Trashed
	n.DocumentTrashed = &trashed
	if !s.Usable {
		n.Unavailable = true
		return
	}
	n.DocumentSummary = s.Summary
	// The internal document card keeps its content preview.
	if s.TypeKey == creativedocument.InternalTypeKey {
		preview := creativecontent.Preview(s.Content)
		n.Content = &preview
	}
}

// versionRow 是汇总版本元信息所需的最少字段。
type versionRow struct {
	id         string
	number     int64
	provenance json.RawMessage
}

// fillVersionSummaries 一次读取本画布未删除的版本，按节点汇总版本元信息。
func fillVersionSummaries(ctx context.Context, tx canvasReader, canvas string, nodes map[string]*Node) error {
	byNode := map[string][]versionRow{}
	total := 0
	err := snapshotRows(ctx, tx, "creative_node_versions", "id,node_id,version_no,generation_provenance", "canvas_id=$2 AND deleted_at IS NULL", func(r store.Row) error {
		var v versionRow
		var node string
		if err := r.Scan(&v.id, &node, &v.number, &v.provenance); err != nil {
			return err
		}
		total++
		if nodes[node] != nil {
			byNode[node] = append(byNode[node], v)
		}
		return nil
	}, canvas)
	if err != nil {
		return err
	}
	// 与 loadVersions 同一上限；超限时只是不显示元信息，不让快照失败。
	if total > 10000 {
		return nil
	}
	for id, versions := range byNode {
		nodes[id].Versions = summarizeVersions(versions, nodes[id].SelectedVersionID)
	}
	return nil
}

// summarizeVersions：主版本为 selected，为空或找不到时取 version_no 最大的一版。
func summarizeVersions(versions []versionRow, selected *string) *VersionSummary {
	if len(versions) == 0 {
		return nil
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i].number < versions[j].number })
	main := len(versions) - 1
	if selected != nil {
		for i, v := range versions {
			if v.id == *selected {
				main = i
			}
		}
	}
	var provenance struct {
		Prompt struct {
			ModelKey string `json:"model_key"`
		} `json:"prompt"`
	}
	var model *string
	if json.Unmarshal(versions[main].provenance, &provenance) == nil && provenance.Prompt.ModelKey != "" {
		model = &provenance.Prompt.ModelKey
	}
	return &VersionSummary{Count: len(versions), MainOrdinal: main + 1, MainModelKey: model}
}
