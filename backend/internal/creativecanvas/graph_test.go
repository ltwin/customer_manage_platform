package creativecanvas_test

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
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

// modelAsset files the complete form of a model into the library, as a save
// from a scene does.
func modelAsset(t *testing.T, db *sql.DB, a store.AccountScope) creativelibrary.AssetReference {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO creative_blobs(id,account_id,storage_driver,object_key,object_version,sha256,byte_size,mime,state,verified_at) VALUES
 ('blob-glb','canvas-a','local','creative-v2/canvas-a/blobs/glb','v1','sha256-'||repeat('1',64),100,'model/gltf-binary','ready',clock_timestamp()),
 ('blob-png','canvas-a','local','creative-v2/canvas-a/blobs/png','v1','sha256-'||repeat('2',64),50,'image/png','ready',clock_timestamp())`); err != nil {
		t.Fatal(err)
	}
	d := draft("")
	d.Kind, d.Payload = "model", creativecontent.Payload{Model: json.RawMessage(`{"format":"glb","manifest":{},"generator":{"code":"x"}}`)}
	var saved creativelibrary.AssetResult
	err := a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		_, err := creativecontent.WriteModelAndRetain(t.Context(), tx, d, "", []creativecontent.ObjectBinding{{Role: "original", BlobID: "blob-glb"}, {Role: "thumbnail", BlobID: "blob-png"}},
			func(r creativecontent.Revision) error {
				var err error
				saved, err = creativelibrary.SaveRevisionInTx(t.Context(), tx, r, creativelibrary.ImportTarget{Title: "柱"})
				return err
			})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return creativelibrary.AssetReference{ID: saved.ID, Revision: saved.Revision, ContentRevisionID: saved.ContentRevisionID}
}

func TestModelAssetIsRefusedOnTheCanvas(t *testing.T) {
	db, a, _ := setup(t)
	p := project(t, a)
	ref := modelAsset(t, db, a)
	empty := "cwnode_" + uuid.NewString()
	graph(t, a, p.CanvasID, creativecanvas.Action{Type: "add_node", NodeID: empty, TypeKey: "core.image"})

	for name, action := range map[string]creativecanvas.Action{
		"add_node":        {Type: "add_node", NodeID: "cwnode_" + uuid.NewString(), TypeKey: "core.image", Asset: &ref},
		"fill_from_asset": {Type: "fill_from_asset", NodeID: empty, Asset: &ref},
	} {
		snapshot, err := creativecanvas.GetCanvas(t.Context(), a, p.CanvasID)
		if err != nil {
			t.Fatal(err)
		}
		input := creativecanvas.BatchInput{CanvasID: p.CanvasID, ExpectedTopologyRevision: snapshot.TopologyRevision, ReadSet: creativecanvas.ReadSetFor(snapshot), Actions: []creativecanvas.Action{action}}
		_, err = creativecanvas.ApplyCommands(t.Context(), a, command(t, input))
		if !errors.Is(err, creativeops.ErrValidation) || !strings.Contains(err.Error(), "模型只能放进 3D 场景") || !strings.Contains(err.Error(), "edit_scene") {
			t.Fatalf("%s: %v", name, err)
		}
		after, err := creativecanvas.GetCanvas(t.Context(), a, p.CanvasID)
		if err != nil {
			t.Fatal(err)
		}
		if after.TopologyRevision != snapshot.TopologyRevision || len(after.Nodes) != 1 || after.Nodes[0].ContentRevisionID != nil {
			t.Fatalf("%s changed the canvas", name)
		}
	}
}
