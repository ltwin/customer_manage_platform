package creativecanvas_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
)

func findNode(t *testing.T, c creativecanvas.Canvas, id string) creativecanvas.Node {
	t.Helper()
	for _, n := range c.Nodes {
		if n.ID == id {
			return n
		}
	}
	t.Fatal("node missing", id)
	return creativecanvas.Node{}
}
func TestDeleteRestoreUsesNewPhysicalVersionsAndRejectsReusedID(t *testing.T) {
	_, scope, _ := setup(t)
	p := project(t, scope)
	a, b := "cwnode_"+uuid.NewString(), "cwnode_"+uuid.NewString()
	content := draft("应由撤销历史保留")
	graph(t, scope, p.CanvasID, creativecanvas.Action{Type: "add_node", NodeID: a, TypeKey: "core.text", Content: &content}, creativecanvas.Action{Type: "add_node", NodeID: b, TypeKey: "core.text"})
	graph(t, scope, p.CanvasID, creativecanvas.Action{Type: "set_node_inputs", NodeID: b, Inputs: []creativecanvas.InputSource{{Slot: "reference", Role: "reference", SourceNodeID: &a}}})
	before, _ := creativecanvas.GetCanvas(t.Context(), scope, p.CanvasID)
	source := findNode(t, before, a)
	target := findNode(t, before, b)
	removed := graph(t, scope, p.CanvasID, creativecanvas.Action{Type: "remove_nodes", NodeIDs: []string{a}})
	deleted, _ := creativecanvas.GetCanvas(t.Context(), scope, p.CanvasID)
	if len(deleted.Inputs) != 0 || findNode(t, deleted, b).DataRevision <= target.DataRevision {
		t.Fatal("dynamic input cleanup did not invalidate target")
	}
	if _, err := creativecontent.Read(t.Context(), scope, *source.ContentRevisionID); err != nil {
		t.Fatal("delete lost undo root", err)
	}
	if _, err := creativecanvas.AddNode(t.Context(), scope, command(t, creativecanvas.AddNodeInput{CanvasID: p.CanvasID, NodeID: a, TypeKey: "core.text", ExpectedTopologyRevision: deleted.TopologyRevision})); !errors.Is(err, creativecanvas.ErrVersionConflict) {
		t.Fatal("deleted identity reused", err)
	}
	undo, err := creativecanvas.Undo(t.Context(), scope, command(t, creativecanvas.UndoInput{CanvasID: p.CanvasID, ChangeID: removed.ChangeID, ReadSet: creativecanvas.ReadSetFor(deleted)}))
	if err != nil {
		t.Fatal(err)
	}
	restored, _ := creativecanvas.GetCanvas(t.Context(), scope, p.CanvasID)
	if len(restored.Inputs) != 1 || findNode(t, restored, a).DataRevision <= source.DataRevision {
		t.Fatal("restore regressed physical version")
	}
	var change creativecanvas.ChangeResult
	if err = json.Unmarshal(undo.Outcome.Response, &change); err != nil {
		t.Fatal(err)
	}
	if _, err = creativecanvas.Redo(t.Context(), scope, command(t, creativecanvas.UndoInput{CanvasID: p.CanvasID, ChangeID: change.ChangeID, ReadSet: creativecanvas.ReadSetFor(restored)})); err != nil {
		t.Fatal(err)
	}
}
func TestPromptRefsShareCycleGuardAndTextCASDoesNotChangeData(t *testing.T) {
	_, scope, _ := setup(t)
	p := project(t, scope)
	a, b := "cwnode_"+uuid.NewString(), "cwnode_"+uuid.NewString()
	graph(t, scope, p.CanvasID, creativecanvas.Action{Type: "add_node", NodeID: a, TypeKey: "core.text"}, creativecanvas.Action{Type: "add_node", NodeID: b, TypeKey: "core.text"})
	c, _ := creativecanvas.GetCanvas(t.Context(), scope, p.CanvasID)
	before := findNode(t, c, b)
	input := creativecanvas.SavePromptInput{CanvasID: p.CanvasID, NodeID: b, ExpectedTopologyRevision: c.TopologyRevision, ReadSet: creativecanvas.ReadSetFor(c), ActionKey: "generate", Text: "保留草稿", Parameters: json.RawMessage(`{"seed":9007199254740993}`), References: []creativecanvas.InputSource{{Slot: "reference", Role: "reference", SourceNodeID: &a}}}
	r, err := creativecanvas.SaveNodePrompt(t.Context(), scope, command(t, input))
	if err != nil {
		t.Fatal(err)
	}
	var prompt creativecanvas.NodePrompt
	if err = json.Unmarshal(r.Outcome.Response, &prompt); err != nil {
		t.Fatal(err)
	}
	c, _ = creativecanvas.GetCanvas(t.Context(), scope, p.CanvasID)
	if findNode(t, c, b).DataRevision != before.DataRevision {
		t.Fatal("prompt changed node data")
	}
	topology := c.TopologyRevision
	input.ExpectedRevision = &prompt.Revision
	input.ExpectedTopologyRevision = c.TopologyRevision
	input.ReadSet = creativecanvas.ReadSetFor(c)
	input.Text = "下一次的提示词"
	if _, err = creativecanvas.SaveNodePrompt(t.Context(), scope, command(t, input)); err != nil {
		t.Fatal(err)
	}
	c, _ = creativecanvas.GetCanvas(t.Context(), scope, p.CanvasID)
	if c.TopologyRevision != topology {
		t.Fatal("text-only prompt save changed topology")
	}
	if string(findNode(t, c, b).Prompt.Parameters) != `{"seed": 9007199254740993}` {
		t.Fatal("prompt integer lost precision", string(findNode(t, c, b).Prompt.Parameters))
	}
	bad := creativecanvas.BatchInput{CanvasID: p.CanvasID, ExpectedTopologyRevision: c.TopologyRevision, ReadSet: creativecanvas.ReadSetFor(c), Actions: []creativecanvas.Action{{Type: "connect_reference", SourceNodeID: b, TargetNodeID: a, SourcePort: "output", TargetPort: "reference", Role: "reference"}}}
	if _, err = creativecanvas.ApplyCommands(t.Context(), scope, command(t, bad)); !errors.Is(err, creativeops.ErrValidation) {
		t.Fatal("prompt/edge cycle accepted", err)
	}
}
func TestGroupDuplicationKeepsOnlyInternalReferences(t *testing.T) {
	_, scope, _ := setup(t)
	p := project(t, scope)
	a, b, c := "cwnode_"+uuid.NewString(), "cwnode_"+uuid.NewString(), "cwnode_"+uuid.NewString()
	graph(t, scope, p.CanvasID, creativecanvas.Action{Type: "add_node", NodeID: a, TypeKey: "core.text", X: 100, Y: 100}, creativecanvas.Action{Type: "add_node", NodeID: b, TypeKey: "core.text", X: 500, Y: 120}, creativecanvas.Action{Type: "add_node", NodeID: c, TypeKey: "core.text", X: -300})
	graph(t, scope, p.CanvasID, creativecanvas.Action{Type: "connect_reference", SourceNodeID: a, TargetNodeID: b, SourcePort: "output", TargetPort: "reference", Role: "reference"}, creativecanvas.Action{Type: "connect_reference", SourceNodeID: c, TargetNodeID: a, SourcePort: "output", TargetPort: "reference", Role: "reference"})
	group := graph(t, scope, p.CanvasID, creativecanvas.Action{Type: "group_nodes", NodeIDs: []string{a, b}})
	copied := graph(t, scope, p.CanvasID, creativecanvas.Action{Type: "duplicate_selection", NodeIDs: []string{group.CreatedIDs[0]}, DX: 50, DY: 60})
	if len(copied.IDMapping) != 3 || len(copied.OmittedReferenceIDs) != 1 {
		t.Fatal("duplicate scope", copied)
	}
	snapshot, _ := creativecanvas.GetCanvas(t.Context(), scope, p.CanvasID)
	if len(snapshot.Edges) != 3 {
		t.Fatal("copied external edge")
	}
	world := creativecanvas.WorldPositions(snapshot.Nodes)
	if world[copied.IDMapping[a]].X != world[a].X+50 {
		t.Fatal("copy world coordinates")
	}
	graph(t, scope, p.CanvasID, creativecanvas.Action{Type: "ungroup_nodes", NodeID: copied.IDMapping[group.CreatedIDs[0]]})
}
func TestDocumentIdentitySurvivesSummaryRemoval(t *testing.T) {
	_, scope, other := setup(t)
	p := project(t, scope)
	r, err := creativecanvas.CreateInternalDocument(t.Context(), scope, command(t, creativecanvas.CreateDocumentInput{Title: "独立文档", Content: draft("文档的正文")}))
	if err != nil {
		t.Fatal(err)
	}
	var d creativecanvas.Document
	if err = json.Unmarshal(r.Outcome.Response, &d); err != nil {
		t.Fatal(err)
	}
	addDoc := func(id string) {
		snapshot, _ := creativecanvas.GetCanvas(t.Context(), scope, p.CanvasID)
		if _, err := creativecanvas.ReferenceInternalDocument(t.Context(), scope, command(t, creativecanvas.ReferenceDocumentInput{CanvasID: p.CanvasID, NodeID: id, DocumentID: d.ID, ExpectedTopologyRevision: snapshot.TopologyRevision})); err != nil {
			t.Fatal(err)
		}
	}
	id := "cwnode_" + uuid.NewString()
	addDoc(id)
	graph(t, scope, p.CanvasID, creativecanvas.Action{Type: "remove_nodes", NodeIDs: []string{id}})
	if _, err = creativecanvas.ReadDocument(t.Context(), scope, d.ID); err != nil {
		t.Fatal("summary deletion deleted document", err)
	}
	if _, err = creativecanvas.ReadDocument(t.Context(), other, d.ID); err == nil {
		t.Fatal("document crosses account")
	}
	addDoc("cwnode_" + uuid.NewString())
}

func TestSourceRemovalAndUndoInvalidatePromptReferences(t *testing.T) {
	_, scope, _ := setup(t)
	p := project(t, scope)
	a, b := "cwnode_"+uuid.NewString(), "cwnode_"+uuid.NewString()
	graph(t, scope, p.CanvasID, creativecanvas.Action{Type: "add_node", NodeID: a, TypeKey: "core.text"}, creativecanvas.Action{Type: "add_node", NodeID: b, TypeKey: "core.text"})
	c, _ := creativecanvas.GetCanvas(t.Context(), scope, p.CanvasID)
	if _, err := creativecanvas.SaveNodePrompt(t.Context(), scope, command(t, creativecanvas.SavePromptInput{CanvasID: p.CanvasID, NodeID: b, ActionKey: "generate", Parameters: json.RawMessage(`{}`), ExpectedTopologyRevision: c.TopologyRevision, ReadSet: creativecanvas.ReadSetFor(c), References: []creativecanvas.InputSource{{Slot: "reference", Role: "reference", SourceNodeID: &a}}})); err != nil {
		t.Fatal(err)
	}
	c, _ = creativecanvas.GetCanvas(t.Context(), scope, p.CanvasID)
	before := findNode(t, c, b)
	deleted := graph(t, scope, p.CanvasID, creativecanvas.Action{Type: "remove_nodes", NodeIDs: []string{a}})
	c, _ = creativecanvas.GetCanvas(t.Context(), scope, p.CanvasID)
	after := findNode(t, c, b)
	if len(after.Prompt.References) != 0 || after.Prompt.Revision <= before.Prompt.Revision || after.DataRevision <= before.DataRevision {
		t.Fatal("source removal did not invalidate prompt/data", before, after)
	}
	if _, err := creativecanvas.Undo(t.Context(), scope, command(t, creativecanvas.UndoInput{CanvasID: p.CanvasID, ChangeID: deleted.ChangeID, ReadSet: creativecanvas.ReadSetFor(c)})); err != nil {
		t.Fatal(err)
	}
	c, _ = creativecanvas.GetCanvas(t.Context(), scope, p.CanvasID)
	restored := findNode(t, c, b)
	if len(restored.Prompt.References) != 1 || restored.Prompt.Revision <= after.Prompt.Revision || restored.DataRevision <= after.DataRevision {
		t.Fatal("undo did not restore refs with monotonic guards", restored)
	}
}
