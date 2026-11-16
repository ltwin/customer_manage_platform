package creativecanvas_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func createdAt(t *testing.T, scope store.AccountScope, canvas, id string) time.Time {
	t.Helper()
	n := findNode(t, canvasNow(t, scope, canvas), id)
	if n.CreatedAt.IsZero() {
		t.Fatalf("node %s has no created_at", id)
	}
	// The API field is what the outline sorts by.
	raw, err := json.Marshal(n)
	if err != nil {
		t.Fatal(err)
	}
	var view struct {
		CreatedAt time.Time `json:"created_at"`
	}
	if err = json.Unmarshal(raw, &view); err != nil {
		t.Fatal(err)
	}
	if !view.CreatedAt.Equal(n.CreatedAt) || view.CreatedAt.Location() != time.UTC {
		t.Fatalf("created_at serialized as %v, want %v in UTC", view.CreatedAt, n.CreatedAt)
	}
	return n.CreatedAt
}

func canvasNow(t *testing.T, scope store.AccountScope, canvas string) creativecanvas.Canvas {
	t.Helper()
	c, err := creativecanvas.GetCanvas(t.Context(), scope, canvas)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func changeOf(t *testing.T, r []byte) creativecanvas.ChangeResult {
	t.Helper()
	var change creativecanvas.ChangeResult
	if err := json.Unmarshal(r, &change); err != nil {
		t.Fatal(err)
	}
	return change
}

// A node keeps one creation time for its whole life: edits, undoing its
// removal and redoing it do not move it; a copy is a new node with a new time.
func TestNodeCreatedAtSurvivesEditsAndUndoRemove(t *testing.T) {
	_, scope, _ := setup(t)
	p := project(t, scope)
	a := "cwnode_" + uuid.NewString()
	graph(t, scope, p.CanvasID, creativecanvas.Action{Type: "add_node", NodeID: a, TypeKey: "core.text", X: 10, Y: 10})
	born := createdAt(t, scope, p.CanvasID, a)

	graph(t, scope, p.CanvasID, creativecanvas.Action{Type: "move_node", NodeID: a, X: 300, Y: 200})
	graph(t, scope, p.CanvasID, creativecanvas.Action{Type: "update_metadata", NodeID: a, Title: "晨光"})
	if got := createdAt(t, scope, p.CanvasID, a); !got.Equal(born) {
		t.Fatalf("edits moved created_at %v -> %v", born, got)
	}

	removed := graph(t, scope, p.CanvasID, creativecanvas.Action{Type: "remove_nodes", NodeIDs: []string{a}})
	time.Sleep(5 * time.Millisecond) // a reset would land visibly later
	deleted := canvasNow(t, scope, p.CanvasID)
	undo, err := creativecanvas.Undo(t.Context(), scope, command(t, creativecanvas.UndoInput{CanvasID: p.CanvasID, ChangeID: removed.ChangeID, ReadSet: creativecanvas.ReadSetFor(deleted)}))
	if err != nil {
		t.Fatal(err)
	}
	if got := createdAt(t, scope, p.CanvasID, a); !got.Equal(born) {
		t.Fatalf("undo remove reset created_at %v -> %v", born, got)
	}

	restored := canvasNow(t, scope, p.CanvasID)
	redo, err := creativecanvas.Redo(t.Context(), scope, command(t, creativecanvas.UndoInput{CanvasID: p.CanvasID, ChangeID: changeOf(t, undo.Outcome.Response).ChangeID, ReadSet: creativecanvas.ReadSetFor(restored)}))
	if err != nil {
		t.Fatal(err)
	}
	deleted = canvasNow(t, scope, p.CanvasID)
	if _, err = creativecanvas.Undo(t.Context(), scope, command(t, creativecanvas.UndoInput{CanvasID: p.CanvasID, ChangeID: changeOf(t, redo.Outcome.Response).ChangeID, ReadSet: creativecanvas.ReadSetFor(deleted)})); err != nil {
		t.Fatal(err)
	}
	if got := createdAt(t, scope, p.CanvasID, a); !got.Equal(born) {
		t.Fatalf("undo after redo reset created_at %v -> %v", born, got)
	}

	copied := graph(t, scope, p.CanvasID, creativecanvas.Action{Type: "duplicate_selection", NodeIDs: []string{a}})
	if len(copied.CreatedIDs) != 1 {
		t.Fatalf("duplicate result: %+v", copied)
	}
	if got := createdAt(t, scope, p.CanvasID, copied.CreatedIDs[0]); !got.After(born) {
		t.Fatalf("copy created_at %v, want later than %v", got, born)
	}
}
