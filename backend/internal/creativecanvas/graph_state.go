package creativecanvas

import (
	"context"
	"encoding/json"
	"errors"
	"sort"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativegraph"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

var ErrGraphIntegrity = errors.New("creative graph state integrity failure")

type identity struct {
	ID, Kind string
	Live     bool
	P, D     creativeops.Revision
	Heads    map[string]creativegraph.Head
}
type graphState struct {
	Versions map[string]NodeVersion
	Nodes    map[string]Node
	Edges    map[string]Edge
	Inputs   map[string]NodeInput
}
type fieldChange struct {
	ID     string             `json:"id"`
	Kind   string             `json:"kind"`
	Field  string             `json:"field"`
	Before creativegraph.Head `json:"before"`
	After  creativegraph.Head `json:"after"`
}
type changeDetail struct {
	SchemaVersion int           `json:"schema_version"`
	Fields        []fieldChange `json:"fields"`
}

func loadGraph(ctx context.Context, tx store.TxAccountScope, id string) (graphState, map[string]identity, error) {
	g := graphState{Versions: map[string]NodeVersion{}, Nodes: map[string]Node{}, Edges: map[string]Edge{}, Inputs: map[string]NodeInput{}}
	ids := map[string]identity{}
	rows, err := tx.QueryPage(ctx, "creative_nodes", nodeColumns, "canvas_id=$2", []store.OrderBy{{Column: "id"}}, maxNodes+1, 0, id)
	if err != nil {
		return g, nil, err
	}
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			rows.Close()
			return g, nil, err
		}
		g.Nodes[n.ID] = n
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return g, nil, err
	}
	versions, err := loadVersions(ctx, tx, id)
	if err != nil {
		return g, nil, err
	}
	g.Versions = versions
	edges, inputs, err := loadRelations(ctx, tx, id)
	if err != nil {
		return g, nil, err
	}
	for _, e := range edges {
		g.Edges[e.ID] = e
	}
	for _, i := range inputs {
		g.Inputs[i.ID] = i
	}
	rows, err = tx.QueryPage(ctx, "creative_graph_identities", "id,kind,is_live,placement_revision,data_revision,effect_heads", "canvas_id=$2", []store.OrderBy{{Column: "id"}}, 100000, 0, id)
	if err != nil {
		return g, nil, err
	}
	for rows.Next() {
		var i identity
		var p, d int64
		var raw json.RawMessage
		if err = rows.Scan(&i.ID, &i.Kind, &i.Live, &p, &d, &raw); err != nil {
			rows.Close()
			return g, nil, err
		}
		if err = json.Unmarshal(raw, &i.Heads); err != nil {
			rows.Close()
			return g, nil, err
		}
		i.P = creativeops.Revision(p)
		i.D = creativeops.Revision(d)
		ids[i.ID] = i
	}
	rows.Close()
	return g, ids, rows.Err()
}
func cloneGraph(g graphState) graphState {
	result := graphState{Nodes: map[string]Node{}, Edges: map[string]Edge{}, Inputs: map[string]NodeInput{}, Versions: map[string]NodeVersion{}}
	for id, n := range g.Nodes {
		result.Nodes[id] = n
	}
	for id, e := range g.Edges {
		result.Edges[id] = e
	}
	for id, i := range g.Inputs {
		result.Inputs[id] = i
	}
	for id, v := range g.Versions {
		result.Versions[id] = v
	}
	return result
}
func graphFields(g graphState) map[string]map[string]json.RawMessage {
	result := map[string]map[string]json.RawMessage{}
	relations := map[string][]string{}
	references := map[string][]string{}
	for _, n := range g.Nodes {
		if n.ParentID != nil {
			relations[*n.ParentID] = append(relations[*n.ParentID], "child:"+n.ID)
		}
	}
	for _, e := range g.Edges {
		copy := e
		copy.Revision = 1
		r := "edge:" + string(creativegraph.Canonical(copy))
		references[e.TargetNodeID] = append(references[e.TargetNodeID], r)
		relations[e.SourceNodeID] = append(relations[e.SourceNodeID], r)
		relations[e.TargetNodeID] = append(relations[e.TargetNodeID], r)
		result[e.ID] = map[string]json.RawMessage{"existence": creativegraph.Canonical(true), "data": creativegraph.Canonical(copy)}
	}
	for _, i := range g.Inputs {
		copy := i
		copy.Revision = 1
		r := "input:" + string(creativegraph.Canonical(copy))
		if i.DraftID == nil {
			references[i.NodeID] = append(references[i.NodeID], r)
		}
		relations[i.NodeID] = append(relations[i.NodeID], r)
		if i.SourceNodeID != nil {
			relations[*i.SourceNodeID] = append(relations[*i.SourceNodeID], r)
		}
		result[i.ID] = map[string]json.RawMessage{"existence": creativegraph.Canonical(true), "data": creativegraph.Canonical(copy)}
	}
	for id, v := range g.Versions {
		copy := v
		copy.Revision = 1
		result[id] = map[string]json.RawMessage{"existence": creativegraph.Canonical(true), "data": creativegraph.Canonical(copy)}
		relations[v.NodeID] = append(relations[v.NodeID], "version:"+id)
	}
	for id, n := range g.Nodes {
		var raw map[string]json.RawMessage
		_ = json.Unmarshal(nodeStateJSON(n), &raw)
		refs := references[id]
		if refs == nil {
			refs = []string{}
		}
		sort.Strings(refs)
		raw["reference_state"] = creativegraph.Canonical(refs)
		result[id] = creativegraph.NodeFields(creativegraph.Canonical(raw), relations[id])
	}
	return result
}
func fieldValue(fields map[string]json.RawMessage, key string) json.RawMessage {
	if v, ok := fields[key]; ok {
		return v
	}
	if key == "existence" {
		return creativegraph.Canonical(false)
	}
	return json.RawMessage("null")
}
func equalValue(a, b json.RawMessage) bool {
	return string(creativegraph.Canonical(a)) == string(creativegraph.Canonical(b))
}
func kindOf(g graphState, id string) string {
	if _, ok := g.Nodes[id]; ok {
		return "node"
	}
	if _, ok := g.Edges[id]; ok {
		return "edge"
	}
	if _, ok := g.Versions[id]; ok {
		return "version"
	}
	return "input"
}
func planChanges(before, after graphState, identities map[string]identity) ([]fieldChange, error) {
	a, b := graphFields(before), graphFields(after)
	keys := map[string]bool{}
	for id := range a {
		keys[id] = true
	}
	for id := range b {
		keys[id] = true
	}
	ordered := make([]string, 0, len(keys))
	for id := range keys {
		ordered = append(ordered, id)
	}
	sort.Strings(ordered)
	var changes []fieldChange
	for _, id := range ordered {
		kind := kindOf(after, id)
		if b[id] == nil {
			kind = kindOf(before, id)
		}
		ident, known := identities[id]
		if a[id] != nil && (!known || !ident.Live || ident.Kind != kind) {
			return nil, ErrGraphIntegrity
		}
		fields := []string{"existence", "data"}
		if kind == "node" {
			fields = append(fields, "placement", "relations")
		}
		for _, field := range fields {
			old, next := fieldValue(a[id], field), fieldValue(b[id], field)
			head, has := ident.Heads[field]
			if known && (!has || head.Hash != creativegraph.Hash(old) || !equalValue(head.Value, old)) {
				return nil, ErrGraphIntegrity
			}
			if equalValue(old, next) {
				continue
			}
			if !has {
				head = creativegraph.NewHead(old)
			}
			changes = append(changes, fieldChange{ID: id, Kind: kind, Field: field, Before: head, After: creativegraph.NewHead(next)})
		}
	}
	return changes, nil
}
func retainChange(ctx context.Context, tx store.TxAccountScope, changeID, revisionID string) error {
	exists, err := tx.Exists(ctx, "creative_change_content_refs", "change_id=$2 AND content_revision_id=$3", changeID, revisionID)
	if err != nil || exists {
		return err
	}
	now, err := tx.CreativeNow(ctx)
	if err != nil {
		return err
	}
	return tx.Insert(ctx, "creative_change_content_refs", []string{"change_id", "content_revision_id", "expires_at"}, changeID, revisionID, now.Add(30*24*60*60*1e9))
}
func retainGraphContent(ctx context.Context, tx store.TxAccountScope, changeID string, g graphState) error {
	refs := map[string]bool{}
	for _, v := range g.Versions {
		refs[v.ContentRevisionID] = true
		for _, i := range v.Inputs {
			refs[i.RevisionID] = true
		}
	}
	for _, n := range g.Nodes {
		if n.ContentRevisionID != nil {
			refs[*n.ContentRevisionID] = true
		}
	}
	for _, i := range g.Inputs {
		if i.ContentRevisionID != nil {
			refs[*i.ContentRevisionID] = true
		}
	}
	for id := range refs {
		if err := retainChange(ctx, tx, changeID, id); err != nil {
			return err
		}
	}
	return nil
}
func persistGraph(ctx context.Context, tx store.TxAccountScope, c Canvas, before, after graphState, ids map[string]identity, changes []fieldChange, operation, changeID, group, inverse string, mapping map[string]string, omitted []string, options ...graphPersistence) (ChangeResult, error) {
	result := ChangeResult{ChangeID: changeID, ResultRevision: c.Revision + 1, BeforeTopologyRevision: c.TopologyRevision, ResultTopologyRevision: c.TopologyRevision, ObjectResults: []ObjectResult{}, CreatedIDs: []string{}, RemovedIDs: []string{}, IDMapping: mapping, OmittedReferenceIDs: omitted}
	if result.IDMapping == nil {
		result.IDMapping = map[string]string{}
	}
	if result.OmittedReferenceIDs == nil {
		result.OmittedReferenceIDs = []string{}
	}
	touched := map[string]map[string]bool{}
	structural := !equalValue(topologyState(before), topologyState(after))
	for _, f := range changes {
		if touched[f.ID] == nil {
			touched[f.ID] = map[string]bool{}
		}
		touched[f.ID][f.Field] = true

	}
	promptSave := len(options) > 0 && options[0].PromptSave
	drafts := map[string]bool{}
	if !promptSave {
		for id := range touched {
			for _, g := range []graphState{before, after} {
				if input, ok := g.Inputs[id]; ok && input.DraftID != nil {
					drafts[*input.DraftID] = true
					if _, live := g.Nodes[input.NodeID]; live {
						if touched[input.NodeID] == nil {
							touched[input.NodeID] = map[string]bool{}
						}
						touched[input.NodeID]["data"] = true
					}
				}
			}
		}
	}

	if len(touched) > 100 {
		return result, ErrLimit
	}
	// Keep precisely the affected revisions, not an entire-canvas content snapshot.
	oldRoots, newRoots := graphState{Versions: map[string]NodeVersion{}, Nodes: map[string]Node{}, Inputs: map[string]NodeInput{}}, graphState{Versions: map[string]NodeVersion{}, Nodes: map[string]Node{}, Inputs: map[string]NodeInput{}}
	for id := range touched {
		if v, ok := before.Versions[id]; ok {
			oldRoots.Versions[id] = v
		}
		if v, ok := after.Versions[id]; ok {
			newRoots.Versions[id] = v
		}
		if n, ok := before.Nodes[id]; ok {
			oldRoots.Nodes[id] = n
		}
		if n, ok := after.Nodes[id]; ok {
			newRoots.Nodes[id] = n
		}
		if i, ok := before.Inputs[id]; ok {
			oldRoots.Inputs[id] = i
		}
		if i, ok := after.Inputs[id]; ok {
			newRoots.Inputs[id] = i
		}
	}
	if err := retainGraphContent(ctx, tx, changeID, oldRoots); err != nil {
		return result, err
	}
	if err := retainGraphContent(ctx, tx, changeID, newRoots); err != nil {
		return result, err
	}
	ordered := make([]string, 0, len(touched))
	for id := range touched {
		ordered = append(ordered, id)
	}
	sort.Strings(ordered)
	// Release every affected relation slot before installing the final graph.
	// IDs are identities, never an ordering rule for immediate unique constraints.
	for _, id := range ordered {
		old, exists := ids[id]
		if !exists || (old.Kind != "input" && old.Kind != "edge") {
			continue
		}
		table := "creative_edges"
		if old.Kind == "input" {
			table = "creative_node_inputs"
			if promptTable(id) {
				table = "creative_node_prompt_refs"
			}
		}
		if _, err := tx.Delete(ctx, table, "id=$2 AND canvas_id=$3", id, c.ID); err != nil {
			return result, err
		}
	}
	for _, id := range ordered {
		fields := touched[id]
		ident, known := ids[id]
		if !known {
			ident = identity{ID: id, Kind: kindOf(after, id), Heads: map[string]creativegraph.Head{}}
			for _, key := range []string{"existence", "data"} {
				ident.Heads[key] = creativegraph.NewHead(fieldValue(nil, key))
			}
			if ident.Kind == "node" {
				for _, key := range []string{"placement", "relations"} {
					ident.Heads[key] = creativegraph.NewHead(fieldValue(nil, key))
				}
			}
		}
		for _, f := range changes {
			if f.ID == id {
				ident.Heads[f.Field] = f.After
			}
		}
		wasLive := ident.Live
		ident.Live = equalValue(ident.Heads["existence"].Value, creativegraph.Canonical(true))
		if ident.Kind == "node" {
			if fields["placement"] || fields["existence"] {
				ident.P++
			}
			if fields["data"] || fields["existence"] {
				ident.D++
			}
		} else {
			ident.D++
		}
		if !wasLive && ident.Live {
			result.CreatedIDs = append(result.CreatedIDs, id)
		}
		if wasLive && !ident.Live {
			result.RemovedIDs = append(result.RemovedIDs, id)
		}
		if err := persistObject(ctx, tx, c.ID, id, ident, after); err != nil {
			return result, err
		}
		raw := creativegraph.Canonical(ident.Heads)
		if known {
			_, err := tx.Update(ctx, "creative_graph_identities", "is_live=$2,placement_revision=$3,data_revision=$4,effect_heads=$5", "id=$6 AND canvas_id=$7", ident.Live, int64(ident.P), int64(ident.D), raw, id, c.ID)
			if err != nil {
				return result, err
			}
		} else {
			if err := tx.Insert(ctx, "creative_graph_identities", []string{"id", "canvas_id", "kind", "is_live", "placement_revision", "data_revision", "effect_heads"}, id, c.ID, ident.Kind, ident.Live, int64(ident.P), int64(ident.D), raw); err != nil {
				return result, err
			}
		}
		r := ObjectResult{Kind: ident.Kind, ID: id, IsLive: ident.Live}
		if ident.Kind == "node" {
			r.PlacementRevision = ident.P
			r.DataRevision = ident.D
		} else {
			r.Revision = ident.D
		}
		result.ObjectResults = append(result.ObjectResults, r)
	}
	for id := range drafts {
		if _, err := tx.Update(ctx, "creative_node_prompt_drafts", "revision=revision+1", "id=$2 AND canvas_id=$3", id, c.ID); err != nil {
			return result, err
		}
	}
	if err := advanceCanvas(ctx, tx, c, structural); err != nil {
		return result, err
	}
	if structural {
		result.ResultTopologyRevision++
	}
	var groupID, inverseID *string
	if group != "" {
		groupID = &group
	}
	if inverse != "" {
		inverseID = &inverse
	}
	if err := tx.Insert(ctx, "creative_changes", []string{"id", "canvas_id", "operation_id", "change_group_id", "inverse_of", "result_revision", "before_after"}, changeID, c.ID, operation, groupID, inverseID, int64(result.ResultRevision), creativegraph.Canonical(changeDetail{SchemaVersion: 1, Fields: changes})); err != nil {
		return result, err
	}
	return result, nil
}
func persistObject(ctx context.Context, tx store.TxAccountScope, canvas, id string, i identity, g graphState) error {
	if i.Kind == "version" {
		return persistVersion(ctx, tx, canvas, id, i, g)
	}
	table := map[string]string{"node": "creative_nodes", "edge": "creative_edges", "input": "creative_node_inputs"}[i.Kind]
	if i.Kind == "input" && promptTable(id) {
		table = "creative_node_prompt_refs"
	}
	if table == "" {
		return ErrGraphIntegrity
	}
	if !i.Live {
		if i.Kind == "node" {
			if _, err := tx.Update(ctx, "creative_node_executions", "state='cancelled',apply_state='discarded',execution_epoch=execution_epoch+1,lease_until=NULL", "canvas_id=$2 AND node_id=$3 AND apply_state='pending'", canvas, id); err != nil {
				return err
			}
		}
		_, err := tx.Delete(ctx, table, "id=$2 AND canvas_id=$3", id, canvas)
		return err
	}
	switch i.Kind {
	case "node":
		n := g.Nodes[id]
		exists, err := tx.Exists(ctx, table, "id=$2 AND canvas_id=$3", id, canvas)
		if err != nil {
			return err
		}
		if !exists {
			if err = tx.Insert(ctx, table, []string{"id", "canvas_id", "type_key", "x", "y"}, id, canvas, n.TypeKey, n.X, n.Y); err != nil {
				return err
			}
		}
		_, err = tx.Update(ctx, table, "type_key=$2,type_version=$3,title=$4,intent=$5,x=$6,y=$7,width=$8,height=$9,parent_id=$10,z_order=$11,config=$12,content_id=$13,content_revision_id=$14,selected_version_id=$15,document_id=$16,placement_revision=$17,data_revision=$18,updated_at=clock_timestamp()", "id=$19 AND canvas_id=$20", n.TypeKey, n.TypeVersion, n.Title, n.Intent, n.X, n.Y, n.Width, n.Height, n.ParentID, n.ZOrder, n.Config, n.ContentID, n.ContentRevisionID, n.SelectedVersionID, n.DocumentID, int64(i.P), int64(i.D), id, canvas)
		return err
	case "edge":
		e := g.Edges[id]
		if _, err := tx.Delete(ctx, table, "id=$2 AND canvas_id=$3", id, canvas); err != nil {
			return err
		}
		return tx.Insert(ctx, table, []string{"id", "canvas_id", "source_node_id", "target_node_id", "source_port", "target_port", "role", "ordinal", "revision"}, id, canvas, e.SourceNodeID, e.TargetNodeID, e.SourcePort, e.TargetPort, e.Role, e.Ordinal, int64(i.D))
	default:
		v := g.Inputs[id]
		if _, err := tx.Delete(ctx, table, "id=$2 AND canvas_id=$3", id, canvas); err != nil {
			return err
		}
		if promptTable(id) {
			return tx.Insert(ctx, table, []string{"id", "canvas_id", "node_id", "slot", "ordinal", "role", "source_node_id", "content_revision_id", "revision", "draft_id"}, id, canvas, v.NodeID, v.Slot, v.Ordinal, v.Role, v.SourceNodeID, v.ContentRevisionID, int64(i.D), v.DraftID)
		}
		return tx.Insert(ctx, table, []string{"id", "canvas_id", "node_id", "slot", "ordinal", "role", "source_node_id", "content_revision_id", "revision"}, id, canvas, v.NodeID, v.Slot, v.Ordinal, v.Role, v.SourceNodeID, v.ContentRevisionID, int64(i.D))
	}
}
func newChangeID() string { return "cwch_" + uuid.NewString() }
func validateContent(ctx context.Context, tx store.TxAccountScope, g graphState) error {
	for _, n := range g.Nodes {
		if n.ContentRevisionID != nil {
			r, err := creativecontent.RequireUsable(ctx, tx, *n.ContentRevisionID, "display")
			if err != nil {
				return err
			}
			if n.ContentID == nil || *n.ContentID != r.ContentID || n.TypeKey != "core."+r.Kind {
				return creativecontent.ErrMissingRoot
			}
		}
	}
	return nil
}

func topologyState(g graphState) json.RawMessage {
	parents := map[string]*string{}
	for id, n := range g.Nodes {
		parents[id] = n.ParentID
	}
	edges := map[string]Edge{}
	for id, e := range g.Edges {
		e.Revision = 1
		edges[id] = e
	}
	inputs := map[string]NodeInput{}
	for id, i := range g.Inputs {
		i.Revision = 1
		inputs[id] = i
	}
	return creativegraph.Canonical(map[string]any{"parents": parents, "edges": edges, "inputs": inputs})
}

type graphPersistence struct{ PromptSave bool }
