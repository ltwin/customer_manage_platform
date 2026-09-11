package creativecanvas

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

type Edge struct {
	ID           string               `json:"id"`
	SourceNodeID string               `json:"source_node_id"`
	TargetNodeID string               `json:"target_node_id"`
	SourcePort   string               `json:"source_port"`
	TargetPort   string               `json:"target_port"`
	Role         string               `json:"role"`
	Ordinal      int                  `json:"ordinal"`
	Revision     creativeops.Revision `json:"revision,omitempty"`
}
type NodeInput struct {
	DraftID           *string              `json:"draft_id,omitempty"`
	ID                string               `json:"id"`
	NodeID            string               `json:"node_id"`
	Slot              string               `json:"slot"`
	Ordinal           int                  `json:"ordinal"`
	Role              string               `json:"role"`
	SourceNodeID      *string              `json:"source_node_id"`
	ContentRevisionID *string              `json:"content_revision_id"`
	Revision          creativeops.Revision `json:"revision,omitempty"`
}
type ObjectRead struct {
	Kind              string               `json:"kind"`
	ID                string               `json:"id"`
	PlacementRevision creativeops.Revision `json:"placement_revision,omitempty"`
	DataRevision      creativeops.Revision `json:"data_revision,omitempty"`
	Revision          creativeops.Revision `json:"revision,omitempty"`
}
type Action struct {
	Type         string                                  `json:"type"`
	NodeID       string                                  `json:"node_id,omitempty"`
	NodeIDs      []string                                `json:"node_ids,omitempty"`
	TypeKey      string                                  `json:"type_key,omitempty"`
	Title        string                                  `json:"title,omitempty"`
	Intent       string                                  `json:"intent,omitempty"`
	ParentID     *string                                 `json:"parent_id,omitempty"`
	X            float64                                 `json:"x,omitempty"`
	Y            float64                                 `json:"y,omitempty"`
	DX           float64                                 `json:"dx,omitempty"`
	DY           float64                                 `json:"dy,omitempty"`
	Width        float64                                 `json:"width,omitempty"`
	Height       float64                                 `json:"height,omitempty"`
	SourceNodeID string                                  `json:"source_node_id,omitempty"`
	TargetNodeID string                                  `json:"target_node_id,omitempty"`
	SourcePort   string                                  `json:"source_port,omitempty"`
	TargetPort   string                                  `json:"target_port,omitempty"`
	Role         string                                  `json:"role,omitempty"`
	EdgeID       string                                  `json:"edge_id,omitempty"`
	GroupMode    string                                  `json:"group_mode,omitempty"`
	Inputs       []InputSource                           `json:"inputs,omitempty"`
	Asset        *creativelibrary.AssetReference         `json:"asset,omitempty"`
	Content      *creativecontent.Draft                  `json:"content,omitempty"`
	Payload      *creativecontent.Payload                `json:"payload,omitempty"`
	Rights       *creativecontent.RightsDeclarationInput `json:"rights,omitempty"`
	VersionID    string                                  `json:"version_id,omitempty"`
}
type BatchInput struct {
	CanvasID                 string               `json:"canvas_id"`
	ExpectedTopologyRevision creativeops.Revision `json:"expected_topology_revision,omitempty"`
	ReadSet                  []ObjectRead         `json:"read_set"`
	Actions                  []Action             `json:"actions"`
	ChangeGroupID            string               `json:"change_group_id,omitempty"`
}
type UndoInput struct {
	CanvasID      string       `json:"canvas_id"`
	ChangeID      string       `json:"change_id,omitempty"`
	ChangeGroupID string       `json:"change_group_id,omitempty"`
	ReadSet       []ObjectRead `json:"read_set"`
}
type ObjectResult struct {
	NodeID            string               `json:"node_id,omitempty"`
	Kind              string               `json:"kind"`
	ID                string               `json:"id"`
	IsLive            bool                 `json:"is_live"`
	PlacementRevision creativeops.Revision `json:"placement_revision,omitempty"`
	DataRevision      creativeops.Revision `json:"data_revision,omitempty"`
	Revision          creativeops.Revision `json:"revision,omitempty"`
}
type ChangeResult struct {
	ChangeID               string               `json:"change_id"`
	ResultRevision         creativeops.Revision `json:"result_revision"`
	BeforeTopologyRevision creativeops.Revision `json:"before_topology_revision"`
	ResultTopologyRevision creativeops.Revision `json:"result_topology_revision"`
	ObjectResults          []ObjectResult       `json:"object_results"`
	CreatedIDs             []string             `json:"created_ids"`
	RemovedIDs             []string             `json:"removed_ids"`
	IDMapping              map[string]string    `json:"id_mapping"`
	OmittedReferenceIDs    []string             `json:"omitted_reference_ids"`
}
type ChangeSummary struct {
	ReadSet   []ObjectRead         `json:"read_set"`
	ID        string               `json:"id"`
	InverseOf *string              `json:"inverse_of"`
	GroupID   *string              `json:"change_group_id"`
	Revision  creativeops.Revision `json:"result_revision"`
}
type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

func WorldPositions(nodes []Node) map[string]Point {
	by := map[string]Node{}
	for _, n := range nodes {
		by[n.ID] = n
	}
	result := map[string]Point{}
	for _, n := range nodes {
		p := Point{X: n.X, Y: n.Y}
		seen := map[string]bool{n.ID: true}
		for parent := n.ParentID; parent != nil; {
			if seen[*parent] {
				break
			}
			seen[*parent] = true
			a, ok := by[*parent]
			if !ok {
				break
			}
			p.X += a.X
			p.Y += a.Y
			parent = a.ParentID
		}
		result[n.ID] = p
	}
	return result
}
func ReadSetFor(c Canvas) []ObjectRead {
	r := []ObjectRead{}
	for _, s := range c.ObjectStates {
		if !s.IsLive || s.Kind == "version" {
			r = append(r, ObjectRead{ID: s.ID, Kind: s.Kind, PlacementRevision: s.PlacementRevision, DataRevision: s.DataRevision, Revision: s.Revision})
		}
	}
	for _, n := range c.Nodes {
		r = append(r, ObjectRead{Kind: "node", ID: n.ID, PlacementRevision: n.PlacementRevision, DataRevision: n.DataRevision})
	}
	for _, e := range c.Edges {
		r = append(r, ObjectRead{Kind: "edge", ID: e.ID, Revision: e.Revision})
	}
	for _, i := range c.Inputs {
		r = append(r, ObjectRead{Kind: "input", ID: i.ID, Revision: i.Revision})
	}
	return r
}

const nodeColumns = "id,type_key,title,x,y,width,height,placement_revision,data_revision,content_id,content_revision_id,parent_id,type_version,z_order,intent,config,selected_version_id,document_id,status_revision,active_execution_id,latest_execution_id"

func scanNode(row store.Row) (Node, error) {
	var n Node
	var p, d, s int64
	err := row.Scan(&n.ID, &n.TypeKey, &n.Title, &n.X, &n.Y, &n.Width, &n.Height, &p, &d, &n.ContentID, &n.ContentRevisionID, &n.ParentID, &n.TypeVersion, &n.ZOrder, &n.Intent, &n.Config, &n.SelectedVersionID, &n.DocumentID, &s, &n.ActiveExecutionID, &n.LatestExecutionID)
	n.PlacementRevision = creativeops.Revision(p)
	n.DataRevision = creativeops.Revision(d)
	n.StatusRevision = creativeops.Revision(s)
	return n, notFound(err)
}
func fillGraphSnapshot(ctx context.Context, tx store.TxAccountScope, c *Canvas) error {
	edges, inputs, err := loadRelations(ctx, tx, c.ID)
	if err != nil {
		return err
	}
	c.Edges = edges
	c.Inputs = inputs
	c.Changes = []ChangeSummary{}
	c.ObjectStates = []ObjectResult{}
	graph, identities, err := loadGraph(ctx, tx, c.ID)
	if err != nil {
		return err
	}
	// Live versions remain addressable regardless of the undo window. Ownership
	// lets clients include only versions affected by this copy/remove operation.
	for id, version := range graph.Versions {
		i, ok := identities[id]
		if !ok || !i.Live {
			return ErrGraphIntegrity
		}
		c.ObjectStates = append(c.ObjectStates, ObjectResult{ID: id, Kind: "version", NodeID: version.NodeID, IsLive: true, Revision: i.D})
	}
	retained := map[string]bool{}
	rows, err := tx.QueryPage(ctx, "creative_changes", "id,inverse_of,change_group_id,result_revision,before_after", "canvas_id=$2 AND reversible AND expires_at>clock_timestamp()", []store.OrderBy{{Column: "result_revision", Desc: true}}, 100, 0, c.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var s ChangeSummary
		var r int64
		var detail changeDetail
		var raw json.RawMessage
		if err = rows.Scan(&s.ID, &s.InverseOf, &s.GroupID, &r, &raw); err != nil {
			return err
		}
		if err = json.Unmarshal(raw, &detail); err != nil || detail.SchemaVersion != 1 {
			return ErrGraphIntegrity
		}
		s.ReadSet = []ObjectRead{}
		seen := map[string]bool{}
		for _, field := range detail.Fields {
			if seen[field.ID] {
				continue
			}
			seen[field.ID] = true
			retained[field.ID] = true
			i, ok := identities[field.ID]
			if !ok {
				return ErrGraphIntegrity
			}
			read := ObjectRead{Kind: i.Kind, ID: i.ID}
			if i.Kind == "node" {
				read.PlacementRevision = i.P
				read.DataRevision = i.D
			} else {
				read.Revision = i.D
			}
			s.ReadSet = append(s.ReadSet, read)
		}
		s.Revision = creativeops.Revision(r)
		c.Changes = append(c.Changes, s)
	}
	for id := range retained {
		i := identities[id]
		if i.Live {
			continue
		}
		r := ObjectResult{ID: i.ID, Kind: i.Kind, IsLive: i.Live}
		if i.Kind == "node" {
			r.PlacementRevision = i.P
			r.DataRevision = i.D
		} else {
			r.Revision = i.D
		}
		c.ObjectStates = append(c.ObjectStates, r)
	}
	sort.Slice(c.ObjectStates, func(i, j int) bool { return c.ObjectStates[i].ID < c.ObjectStates[j].ID })
	return rows.Err()
}
func loadRelations(ctx context.Context, tx store.TxAccountScope, id string) ([]Edge, []NodeInput, error) {
	edges := []Edge{}
	inputs := []NodeInput{}
	rows, err := tx.QueryPage(ctx, "creative_edges", "id,source_node_id,target_node_id,source_port,target_port,role,ordinal,revision", "canvas_id=$2", []store.OrderBy{{Column: "id"}}, 2001, 0, id)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var e Edge
		var r int64
		if err = rows.Scan(&e.ID, &e.SourceNodeID, &e.TargetNodeID, &e.SourcePort, &e.TargetPort, &e.Role, &e.Ordinal, &r); err != nil {
			rows.Close()
			return nil, nil, err
		}
		e.Revision = creativeops.Revision(r)
		edges = append(edges, e)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, nil, err
	}
	rows, err = tx.QueryPage(ctx, "creative_node_inputs", "id,node_id,slot,ordinal,role,source_node_id,content_revision_id,revision", "canvas_id=$2", []store.OrderBy{{Column: "id"}}, 2001, 0, id)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var i NodeInput
		var r int64
		if err = rows.Scan(&i.ID, &i.NodeID, &i.Slot, &i.Ordinal, &i.Role, &i.SourceNodeID, &i.ContentRevisionID, &r); err != nil {
			rows.Close()
			return nil, nil, err
		}
		i.Revision = creativeops.Revision(r)
		inputs = append(inputs, i)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, nil, err
	}
	rows, err = tx.QueryPage(ctx, "creative_node_prompt_refs", "id,node_id,slot,ordinal,role,source_node_id,content_revision_id,revision,draft_id", "canvas_id=$2", []store.OrderBy{{Column: "id"}}, 2001, 0, id)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var i NodeInput
		var r int64
		if err = rows.Scan(&i.ID, &i.NodeID, &i.Slot, &i.Ordinal, &i.Role, &i.SourceNodeID, &i.ContentRevisionID, &r, &i.DraftID); err != nil {
			rows.Close()
			return nil, nil, err
		}
		i.Revision = creativeops.Revision(r)
		inputs = append(inputs, i)
	}
	rows.Close()
	return edges, inputs, rows.Err()
}
func nodeSlice(nodes map[string]Node) []Node {
	out := make([]Node, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Commands are a strict named union; irrelevant fields cannot silently disappear.
func (a *Action) UnmarshalJSON(raw []byte) error {
	type actionAlias Action
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return creativeops.ErrValidation
	}
	var kind string
	if err := json.Unmarshal(fields["type"], &kind); err != nil {
		return creativeops.ErrValidation
	}
	allowedByType := map[string]string{
		"add_node":  "node_id type_key title x y parent_id asset content",
		"move_node": "node_id x y", "move_nodes": "node_ids dx dy", "resize_node": "node_id width height",
		"update_metadata": "node_id title intent", "clear_content": "node_id", "replace_content": "node_id payload rights",
		"connect_reference": "source_node_id target_node_id source_port target_port role", "disconnect_reference": "edge_id",
		"set_node_inputs": "node_id inputs", "group_nodes": "node_ids", "ungroup_nodes": "node_id", "reparent_nodes": "node_ids parent_id",
		"duplicate_selection": "node_ids dx dy", "select_version": "node_id version_id", "delete_version": "node_id version_id",
		"remove_nodes": "node_ids group_mode",
	}
	if kind == "add_node" || kind == "move_node" {
		for _, key := range []string{"x", "y"} {
			if _, ok := fields[key]; !ok {
				return creativeops.ErrValidation
			}
		}
	}
	list, ok := allowedByType[kind]
	if !ok {
		return creativeops.ErrValidation
	}
	allowed := map[string]bool{"type": true}
	for _, key := range strings.Fields(list) {
		allowed[key] = true
	}
	for key, value := range fields {
		if !allowed[key] || string(value) == "null" && key != "parent_id" {
			return creativeops.ErrValidation
		}
	}
	var value actionAlias
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	*a = Action(value)
	return nil
}

type InputSource struct {
	Slot              string  `json:"slot"`
	Ordinal           int     `json:"ordinal"`
	Role              string  `json:"role"`
	SourceNodeID      *string `json:"source_node_id"`
	ContentRevisionID *string `json:"content_revision_id"`
}

func inputFromSource(s InputSource) NodeInput {
	return NodeInput{Slot: s.Slot, Ordinal: s.Ordinal, Role: s.Role, SourceNodeID: s.SourceNodeID, ContentRevisionID: s.ContentRevisionID}
}
func (a Action) MarshalJSON() ([]byte, error) {
	type alias Action
	b, err := json.Marshal(alias(a))
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(b, &fields); err != nil {
		return nil, err
	}
	if a.Type == "add_node" || a.Type == "move_node" {
		fields["x"], err = json.Marshal(a.X)
		if err != nil {
			return nil, err
		}
		fields["y"], err = json.Marshal(a.Y)
		if err != nil {
			return nil, err
		}
	}
	return json.Marshal(fields)
}
