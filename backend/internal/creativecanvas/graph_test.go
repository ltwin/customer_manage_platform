package creativecanvas_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func graph(t *testing.T, scope store.AccountScope, canvas string, actions ...creativecanvas.Action) creativecanvas.ChangeResult {
	t.Helper()
	snapshot, err := creativecanvas.GetCanvas(t.Context(), scope, canvas)
	if err != nil {
		t.Fatal(err)
	}
	input := creativecanvas.BatchInput{CanvasID: canvas, ExpectedTopologyRevision: snapshot.TopologyRevision, ReadSet: creativecanvas.ReadSetFor(snapshot), Actions: actions}
	receipt, err := creativecanvas.ApplyCommands(t.Context(), scope, command(t, input))
	if err != nil {
		t.Fatal(err)
	}
	var result creativecanvas.ChangeResult
	if err = json.Unmarshal(receipt.Outcome.Response, &result); err != nil {
		t.Fatal(err)
	}
	return result
}
func TestGraphGroupMoveAndConsecutiveUndo(t *testing.T) {
	_, scope, _ := setup(t)
	p := project(t, scope)
	a, b := "cwnode_"+uuid.NewString(), "cwnode_"+uuid.NewString()
	graph(t, scope, p.CanvasID, creativecanvas.Action{Type: "add_node", NodeID: a, TypeKey: "core.text", X: 100, Y: 100}, creativecanvas.Action{Type: "add_node", NodeID: b, TypeKey: "core.text", X: 450, Y: 120})
	grouped := graph(t, scope, p.CanvasID, creativecanvas.Action{Type: "group_nodes", NodeIDs: []string{a, b}})
	if len(grouped.CreatedIDs) != 1 {
		t.Fatalf("group result: %+v", grouped)
	}
	g := grouped.CreatedIDs[0]
	before, _ := creativecanvas.GetCanvas(t.Context(), scope, p.CanvasID)
	pos := creativecanvas.WorldPositions(before.Nodes)
	if pos[a].X != 100 || pos[b].Y != 120 {
		t.Fatalf("group shifted children: %+v", pos)
	}
	moved := graph(t, scope, p.CanvasID, creativecanvas.Action{Type: "move_nodes", NodeIDs: []string{g, a}, DX: 50, DY: 25})
	after, _ := creativecanvas.GetCanvas(t.Context(), scope, p.CanvasID)
	pos = creativecanvas.WorldPositions(after.Nodes)
	if pos[a].X != 150 || pos[b].Y != 145 {
		t.Fatal("parent and child moved twice", pos)
	}
	_, err := creativecanvas.Undo(t.Context(), scope, command(t, creativecanvas.UndoInput{CanvasID: p.CanvasID, ChangeID: moved.ChangeID, ReadSet: creativecanvas.ReadSetFor(after)}))
	if err != nil {
		t.Fatal(err)
	}
	after, _ = creativecanvas.GetCanvas(t.Context(), scope, p.CanvasID)
	_, err = creativecanvas.Undo(t.Context(), scope, command(t, creativecanvas.UndoInput{CanvasID: p.CanvasID, ChangeID: grouped.ChangeID, ReadSet: creativecanvas.ReadSetFor(after)}))
	if err != nil {
		t.Fatal("consecutive undo must follow logical states", err)
	}
	after, _ = creativecanvas.GetCanvas(t.Context(), scope, p.CanvasID)
	pos = creativecanvas.WorldPositions(after.Nodes)
	if len(after.Nodes) != 2 || pos[a].X != 100 {
		t.Fatal("undo group changed world position", after)
	}
}
func TestReferenceCycleAtomicityAndUnrelatedUndo(t *testing.T) {
	_, scope, other := setup(t)
	p := project(t, scope)
	a, b := "cwnode_"+uuid.NewString(), "cwnode_"+uuid.NewString()
	graph(t, scope, p.CanvasID, creativecanvas.Action{Type: "add_node", NodeID: a, TypeKey: "core.text"}, creativecanvas.Action{Type: "add_node", NodeID: b, TypeKey: "core.text", X: 400})
	edge := graph(t, scope, p.CanvasID, creativecanvas.Action{Type: "connect_reference", SourceNodeID: a, TargetNodeID: b, SourcePort: "output", TargetPort: "reference", Role: "reference"})
	snapshot, _ := creativecanvas.GetCanvas(t.Context(), scope, p.CanvasID)
	bad := creativecanvas.BatchInput{CanvasID: p.CanvasID, ExpectedTopologyRevision: snapshot.TopologyRevision, ReadSet: creativecanvas.ReadSetFor(snapshot), Actions: []creativecanvas.Action{{Type: "connect_reference", SourceNodeID: b, TargetNodeID: a, SourcePort: "output", TargetPort: "reference", Role: "reference"}}}
	if _, err := creativecanvas.ApplyCommands(t.Context(), scope, command(t, bad)); !errors.Is(err, creativeops.ErrValidation) {
		t.Fatal("cycle accepted", err)
	}
	graph(t, scope, p.CanvasID, creativecanvas.Action{Type: "move_nodes", NodeIDs: []string{a}, DX: 30})
	snapshot, _ = creativecanvas.GetCanvas(t.Context(), scope, p.CanvasID)
	input := creativecanvas.UndoInput{CanvasID: p.CanvasID, ChangeID: edge.ChangeID, ReadSet: creativecanvas.ReadSetFor(snapshot)}
	if _, err := creativecanvas.Undo(t.Context(), other, command(t, input)); err == nil {
		t.Fatal("cross account undo")
	}
	if _, err := creativecanvas.Undo(t.Context(), scope, command(t, input)); err != nil {
		t.Fatal("unrelated placement must survive undo", err)
	}
	snapshot, _ = creativecanvas.GetCanvas(t.Context(), scope, p.CanvasID)
	if len(snapshot.Edges) != 0 || creativecanvas.WorldPositions(snapshot.Nodes)[a].X != 30 {
		t.Fatal("undo clobbered placement")
	}
}
