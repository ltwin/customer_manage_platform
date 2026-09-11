package creativecanvas_test

import (
	"encoding/json"
	"sort"
	"testing"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/platform/jobs"
)

func TestInputSlotReplacementAndUndo(t *testing.T) {
	_, scope, _ := setup(t)
	p := project(t, scope)
	a, b, c := "cwnode_"+uuid.NewString(), "cwnode_"+uuid.NewString(), "cwnode_"+uuid.NewString()
	graph(t, scope, p.CanvasID, creativecanvas.Action{Type: "add_node", NodeID: a, TypeKey: "core.text"}, creativecanvas.Action{Type: "add_node", NodeID: b, TypeKey: "core.text"}, creativecanvas.Action{Type: "add_node", NodeID: c, TypeKey: "core.text"})
	graph(t, scope, p.CanvasID, creativecanvas.Action{Type: "set_node_inputs", NodeID: c, Inputs: []creativecanvas.InputSource{{Slot: "reference", Role: "reference", SourceNodeID: &a}}})
	replacement := graph(t, scope, p.CanvasID, creativecanvas.Action{Type: "set_node_inputs", NodeID: c, Inputs: []creativecanvas.InputSource{{Slot: "reference", Role: "reference", SourceNodeID: &b}}})
	snap, err := creativecanvas.GetCanvas(t.Context(), scope, p.CanvasID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = creativecanvas.Undo(t.Context(), scope, command(t, creativecanvas.UndoInput{CanvasID: p.CanvasID, ChangeID: replacement.ChangeID, ReadSet: creativecanvas.ReadSetFor(snap)})); err != nil {
		t.Fatal("replacement undo", err)
	}
	snap, err = creativecanvas.GetCanvas(t.Context(), scope, p.CanvasID)
	if err != nil || len(snap.Inputs) != 1 || snap.Inputs[0].SourceNodeID == nil || *snap.Inputs[0].SourceNodeID != a {
		t.Fatal("input slot not restored", snap.Inputs, err)
	}
}
func TestMoveReceiptIncludesUnchangedAndChangedTargets(t *testing.T) {
	_, scope, _ := setup(t)
	p := project(t, scope)
	a, b := "cwnode_"+uuid.NewString(), "cwnode_"+uuid.NewString()
	graph(t, scope, p.CanvasID, creativecanvas.Action{Type: "add_node", NodeID: a, TypeKey: "core.text"}, creativecanvas.Action{Type: "add_node", NodeID: b, TypeKey: "core.text"})
	for _, moves := range [][]creativecanvas.Action{
		{{Type: "move_node", NodeID: a, X: 0, Y: 0}},
		{{Type: "move_node", NodeID: a, X: 0, Y: 0}, {Type: "move_node", NodeID: b, X: 80, Y: 20}},
	} {
		result := graph(t, scope, p.CanvasID, moves...)
		snap, err := creativecanvas.GetCanvas(t.Context(), scope, p.CanvasID)
		if err != nil {
			t.Fatal(err)
		}
		for _, move := range moves {
			n := findNode(t, snap, move.NodeID)
			found := false
			for _, r := range result.ObjectResults {
				if r.ID == move.NodeID {
					found = r.IsLive && r.PlacementRevision == n.PlacementRevision
				}
			}
			if !found {
				t.Fatal("successful move omitted actual target revision", move.NodeID, result)
			}
		}
	}
}

func TestExecutionPreservesInputOrder(t *testing.T) {
	_, s, runtime, svc, p, ids := executionFixture(t)
	c, _ := creativecanvas.GetCanvas(t.Context(), s, p.CanvasID)
	actions := []creativecanvas.Action{}
	for _, edge := range c.Edges {
		actions = append(actions, creativecanvas.Action{Type: "disconnect_reference", EdgeID: edge.ID})
	}
	graph(t, s, p.CanvasID, actions...)
	c, _ = creativecanvas.GetCanvas(t.Context(), s, p.CanvasID)
	sources := []creativecanvas.Node{}
	for _, n := range c.Nodes {
		if n.ContentRevisionID != nil {
			sources = append(sources, n)
		}
	}
	sort.Slice(sources, func(i, j int) bool { return *sources[i].ContentRevisionID > *sources[j].ContentRevisionID })
	refs := []creativecanvas.InputSource{}
	for ordinal, n := range sources {
		refs = append(refs, creativecanvas.InputSource{Slot: "reference", Ordinal: ordinal, Role: "reference", ContentRevisionID: n.ContentRevisionID})
	}
	graph(t, s, p.CanvasID, creativecanvas.Action{Type: "set_node_inputs", NodeID: ids[2], Inputs: refs})
	e, cmd := requestExecution(t, s, runtime, svc, p, ids[2])
	raw, _ := json.Marshal(creativecanvas.ExecutionTarget{CanvasID: p.CanvasID, ExecutionID: e.ID})
	if err := svc.Work(t.Context(), s, jobs.Request{Kind: "canvas.node_execution", OperationID: cmd.OperationID, Payload: raw}); err != nil {
		t.Fatal(err)
	}
	c, _ = creativecanvas.GetCanvas(t.Context(), s, p.CanvasID)
	expected := *sources[0].Content.Payload.Body + "\n\n" + *sources[1].Content.Payload.Body
	for _, n := range c.Nodes {
		if n.ID == ids[2] && n.Content != nil && *n.Content.Payload.Body != expected {
			t.Fatalf("ordinal ignored: expected=%q got=%q", expected, *n.Content.Payload.Body)
		}
	}
	versions, err := creativecanvas.ListNodeVersions(t.Context(), s, p.CanvasID, ids[2])
	if err != nil || len(versions.Items) != 1 {
		t.Fatal(versions, err)
	}
	for ordinal, ref := range versions.Items[0].Inputs {
		if ref.RevisionID != *sources[ordinal].ContentRevisionID {
			t.Fatal("version reordered inputs", versions.Items[0].Inputs)
		}
	}
	n := findNode(t, c, ids[2])
	if _, err = creativecanvas.ReuseVersionPrompt(t.Context(), s, command(t, creativecanvas.ReuseVersionInput{CanvasID: p.CanvasID, NodeID: ids[2], VersionID: versions.Items[0].ID, ExpectedDraftRevision: &n.Prompt.Revision, ExpectedTopologyRevision: c.TopologyRevision, ReadSet: creativecanvas.ReadSetFor(c)})); err != nil {
		t.Fatal(err)
	}
	c, err = creativecanvas.GetCanvas(t.Context(), s, p.CanvasID)
	if err != nil {
		t.Fatal(err)
	}
	for ordinal, ref := range findNode(t, c, ids[2]).Prompt.References {
		if ref.ContentRevisionID == nil || *ref.ContentRevisionID != *sources[ordinal].ContentRevisionID {
			t.Fatal("reuse reordered inputs", ref)
		}
	}

}

func TestOldTombstonesDoNotInflateCanvasCommandReads(t *testing.T) {
	db, scope, _ := setup(t)
	p := project(t, scope)
	if _, err := db.Exec(`INSERT INTO creative_graph_identities(account_id,id,canvas_id,kind,is_live,placement_revision,data_revision,effect_heads) SELECT 'canvas-a','old-identity-'||i,$1,'node',false,1,1,'{}'::jsonb FROM generate_series(1,5100) i`, p.CanvasID); err != nil {
		t.Fatal(err)
	}
	a, b := "cwnode_"+uuid.NewString(), "cwnode_"+uuid.NewString()
	graph(t, scope, p.CanvasID, creativecanvas.Action{Type: "add_node", NodeID: a, TypeKey: "core.text"}, creativecanvas.Action{Type: "add_node", NodeID: b, TypeKey: "core.text"})
	c, err := creativecanvas.GetCanvas(t.Context(), scope, p.CanvasID)
	if err != nil || len(creativecanvas.ReadSetFor(c)) > 10 {
		t.Fatal("unrelated tombstones escaped into command reads", len(creativecanvas.ReadSetFor(c)), err)
	}
	grouped := graph(t, scope, p.CanvasID, creativecanvas.Action{Type: "group_nodes", NodeIDs: []string{a, b}})
	c, err = creativecanvas.GetCanvas(t.Context(), scope, p.CanvasID)
	if err != nil {
		t.Fatal(err)
	}
	var reads []creativecanvas.ObjectRead
	for _, change := range c.Changes {
		if change.ID == grouped.ChangeID {
			reads = change.ReadSet
		}
	}
	if len(reads) == 0 || len(reads) > 3 {
		t.Fatal("undo read set not bounded to effect", reads)
	}
	if _, err = creativecanvas.Undo(t.Context(), scope, command(t, creativecanvas.UndoInput{CanvasID: p.CanvasID, ChangeID: grouped.ChangeID, ReadSet: reads})); err != nil {
		t.Fatal(err)
	}
}

func TestPromptSlotReplacementAndEdgeReconnect(t *testing.T) {
	_, scope, _, _, p, ids := executionFixture(t)
	for i := 0; i < 8; i++ {
		c, err := creativecanvas.GetCanvas(t.Context(), scope, p.CanvasID)
		if err != nil {
			t.Fatal(err)
		}
		n := findNode(t, c, ids[2])
		source := ids[i%2]
		if _, err = creativecanvas.SaveNodePrompt(t.Context(), scope, command(t, creativecanvas.SavePromptInput{CanvasID: p.CanvasID, NodeID: ids[2], ActionKey: "internal.text-compose", ExpectedRevision: &n.Prompt.Revision, ExpectedTopologyRevision: c.TopologyRevision, ReadSet: creativecanvas.ReadSetFor(c), Parameters: json.RawMessage(`{}`), References: []creativecanvas.InputSource{{Slot: "reference", Role: "reference", SourceNodeID: &source}}})); err != nil {
			t.Fatal("replacing same prompt slot", err)
		}
	}
	c, err := creativecanvas.GetCanvas(t.Context(), scope, p.CanvasID)
	if err != nil {
		t.Fatal(err)
	}
	edge := c.Edges[0]
	replaced := graph(t, scope, p.CanvasID, creativecanvas.Action{Type: "disconnect_reference", EdgeID: edge.ID}, creativecanvas.Action{Type: "connect_reference", SourceNodeID: edge.SourceNodeID, TargetNodeID: edge.TargetNodeID, SourcePort: edge.SourcePort, TargetPort: edge.TargetPort, Role: edge.Role})
	c, err = creativecanvas.GetCanvas(t.Context(), scope, p.CanvasID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = creativecanvas.Undo(t.Context(), scope, command(t, creativecanvas.UndoInput{CanvasID: p.CanvasID, ChangeID: replaced.ChangeID, ReadSet: creativecanvas.ReadSetFor(c)})); err != nil {
		t.Fatal("edge replacement undo", err)
	}
}

func TestActiveCopyVersionSurvivesUndoProjectionWindow(t *testing.T) {
	_, scope, _ := setup(t)
	p := project(t, scope)
	id := "cwnode_" + uuid.NewString()
	content := draft("长期保留的作品版本")
	graph(t, scope, p.CanvasID, creativecanvas.Action{Type: "add_node", NodeID: id, TypeKey: "core.text", Content: &content})
	copy := graph(t, scope, p.CanvasID, creativecanvas.Action{Type: "duplicate_selection", NodeIDs: []string{id}, DX: 36, DY: 36})
	copyID := copy.IDMapping[id]
	for i := 1; i <= 101; i++ {
		c, err := creativecanvas.GetCanvas(t.Context(), scope, p.CanvasID)
		if err != nil {
			t.Fatal(err)
		}
		n := findNode(t, c, id)
		if _, err = creativecanvas.MoveNode(t.Context(), scope, command(t, creativecanvas.MoveNodeInput{CanvasID: p.CanvasID, NodeID: id, ExpectedPlacementRevision: n.PlacementRevision, X: float64(i), Y: 0})); err != nil {
			t.Fatal(err)
		}
	}
	c, err := creativecanvas.GetCanvas(t.Context(), scope, p.CanvasID)
	if err != nil {
		t.Fatal(err)
	}
	copiedNode := findNode(t, c, copyID)
	found := false
	for _, state := range c.ObjectStates {
		if state.ID == *copiedNode.SelectedVersionID {
			found = state.NodeID == copyID && state.IsLive && state.Revision > 0
		}
	}
	if !found {
		t.Fatal("active version lost with old undo entry")
	}
	graph(t, scope, p.CanvasID, creativecanvas.Action{Type: "duplicate_selection", NodeIDs: []string{copyID}, DX: 36, DY: 36})
	graph(t, scope, p.CanvasID, creativecanvas.Action{Type: "remove_nodes", NodeIDs: []string{copyID}})
}
