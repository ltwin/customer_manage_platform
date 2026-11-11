package creativecanvas_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
	"github.com/samson/customer-manage-platform/backend/internal/creativemedia"
	"github.com/samson/customer-manage-platform/backend/internal/creativereference"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/platform/versionedfs"
)

func TestCharacterCanvasAndGeneration(t *testing.T) {
	db, scope, _ := setup(t)
	ctx := t.Context()
	if _, err := db.Exec(`INSERT INTO creative_blobs(id,account_id,storage_driver,object_key,object_version,sha256,byte_size,mime,state,verified_at)
		VALUES('character-canvas-png','canvas-a','local','test-character','v1','sha256-'||repeat('1',64),50,'image/png','ready',clock_timestamp())`); err != nil {
		t.Fatal(err)
	}
	writeImage := func(tx store.TxAccountScope, retain func(creativecontent.Revision) error) (creativecontent.Revision, error) {
		return creativecontent.WriteMediaAndRetain(ctx, tx, creativecontent.Draft{Kind: "image"},
			[]creativecontent.ObjectBinding{{Role: "original", BlobID: "character-canvas-png"}}, retain)
	}
	var images []string
	if err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		for range 3 {
			r, err := writeImage(tx, func(r creativecontent.Revision) error {
				_, err := creativelibrary.SaveRevisionInTx(ctx, tx, r, creativelibrary.ImportTarget{Title: "立绘"})
				return err
			})
			if err != nil {
				return err
			}
			images = append(images, r.ID)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	payload := creativecontent.CharacterPayload{Name: "云", Appearance: "青绿色短发", Sources: []creativecontent.CharacterSource{}, Views: []creativecontent.CharacterView{
		{ContentRevisionID: images[0], View: "front"}, {ContentRevisionID: images[1], View: "side"},
	}}
	asset, err := creativelibrary.CreateCharacter(ctx, scope, command(t, creativelibrary.CreateCharacterInput{Character: payload}))
	if err != nil {
		t.Fatal(err)
	}
	var saved creativelibrary.AssetResult
	if err := json.Unmarshal(asset.Outcome.Response, &saved); err != nil {
		t.Fatal(err)
	}
	p := project(t, scope)
	nodeID, targetID, otherID := "cwnode_"+uuid.NewString(), "cwnode_"+uuid.NewString(), "cwnode_"+uuid.NewString()
	graph(t, scope, p.CanvasID,
		creativecanvas.Action{Type: "add_node", NodeID: nodeID, TypeKey: "core.character", Asset: &creativelibrary.AssetReference{ID: saved.ID, Revision: saved.Revision, ContentRevisionID: saved.ContentRevisionID}},
		creativecanvas.Action{Type: "add_node", NodeID: targetID, TypeKey: "core.image", X: 400},
		creativecanvas.Action{Type: "add_node", NodeID: otherID, TypeKey: "core.text", X: 800},
	)
	read := func() creativecanvas.Canvas {
		t.Helper()
		c, err := creativecanvas.GetCanvas(ctx, scope, p.CanvasID)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	node := func(c creativecanvas.Canvas, id string) creativecanvas.Node {
		t.Helper()
		for _, n := range c.Nodes {
			if n.ID == id {
				return n
			}
		}
		t.Fatalf("missing node %s", id)
		return creativecanvas.Node{}
	}
	card := node(read(), nodeID)
	if card.Character == nil || card.Character.ViewCount != 2 || card.Character.CoverRevisionID != images[0] || card.Character.AssetID != saved.ID {
		t.Fatalf("card snapshot: %+v", card.Character)
	}
	bad := creativecanvas.Action{Type: "connect_reference", SourceNodeID: nodeID, TargetNodeID: otherID, SourcePort: "output", TargetPort: "reference", Role: "reference"}
	c := read()
	_, err = creativecanvas.ApplyCommands(ctx, scope, command(t, creativecanvas.BatchInput{CanvasID: p.CanvasID, ExpectedTopologyRevision: c.TopologyRevision, ReadSet: creativecanvas.ReadSetFor(c), Actions: []creativecanvas.Action{bad}}))
	if !errors.Is(err, creativeops.ErrValidation) {
		t.Fatalf("character connected to a text node: %v", err)
	}
	bad.TargetNodeID = targetID
	graph(t, scope, p.CanvasID, bad)

	payload.Appearance = "银发"
	latest, err := creativelibrary.SaveCharacterRevision(ctx, scope, command(t, creativelibrary.SaveCharacterRevisionInput{ID: saved.ID, ExpectedRevision: saved.Revision, Character: payload}))
	if err != nil {
		t.Fatal(err)
	}
	var newer creativelibrary.AssetResult
	if err := json.Unmarshal(latest.Outcome.Response, &newer); err != nil {
		t.Fatal(err)
	}
	card = node(read(), nodeID)
	if card.Content.ID != saved.ContentRevisionID || card.Character.LatestRevisionID != newer.ContentRevisionID {
		t.Fatalf("card did not stay pinned: %+v", card.Character)
	}
	update := graph(t, scope, p.CanvasID, creativecanvas.Action{Type: "update_character", NodeID: nodeID, ContentRevisionID: newer.ContentRevisionID})
	c = read()
	if n := node(c, nodeID); n.Content.ID != newer.ContentRevisionID || n.Versions == nil || n.Versions.Count != 2 {
		t.Fatalf("updated card: %+v", n)
	}
	if _, err := creativecanvas.Undo(ctx, scope, command(t, creativecanvas.UndoInput{CanvasID: p.CanvasID, ChangeID: update.ChangeID, ReadSet: creativecanvas.ReadSetFor(c)})); err != nil {
		t.Fatal(err)
	}
	if _, err := creativelibrary.Trash(ctx, scope, command(t, creativelibrary.AssetVersion{ID: newer.ID, Revision: newer.Revision})); err != nil {
		t.Fatal(err)
	}
	c = read()
	card = node(c, nodeID)
	if card.Content.ID != saved.ContentRevisionID || card.Character.AssetID != "" || card.Character.LatestRevisionID != "" {
		t.Fatalf("after undo and trash: %+v", card)
	}
	const model = "test-image"
	for _, text := range []string{"站在台阶上", "@[" + nodeID + "] 站在台阶上"} {
		target := node(c, targetID)
		var expected *creativeops.Revision
		if target.Prompt != nil {
			expected = &target.Prompt.Revision
		}
		if _, err := creativecanvas.SaveNodePrompt(ctx, scope, command(t, creativecanvas.SavePromptInput{
			CanvasID: p.CanvasID, NodeID: targetID, ExpectedRevision: expected, ExpectedTopologyRevision: c.TopologyRevision,
			ReadSet: creativecanvas.ReadSetFor(c), ActionKey: creativecanvas.GenerationAction, Text: text,
			ModelKey: model, Parameters: json.RawMessage(`{}`),
		})); err != nil {
			t.Fatal(err)
		}
		c = read()
		if text == "站在台阶上" {
			target = node(c, targetID)
			if err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
				draft, err := creativecanvas.GenerationDraftInTx(ctx, tx, creativecanvas.RequestExecutionInput{
					CanvasID: c.ID, NodeID: targetID, DraftRevision: target.Prompt.Revision,
				})
				if err == nil && (!strings.Contains(draft.Prompt, "角色设定：") || len(draft.References) != 2) {
					t.Fatalf("prompt without @ dropped the card: %+v", draft)
				}
				return err
			}); err != nil {
				t.Fatal(err)
			}
		}
	}
	target := node(c, targetID)
	input := creativecanvas.RequestExecutionInput{CanvasID: p.CanvasID, NodeID: target.ID, ActionKey: creativecanvas.GenerationAction,
		DraftRevision: target.Prompt.Revision, ExpectedDataRevision: target.DataRevision, ReadSet: creativecanvas.ReadSetFor(c)}
	execution := creativecanvas.ExecutionTarget{CanvasID: p.CanvasID, ExecutionID: "cwexec_" + uuid.NewString()}
	if err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		draft, err := creativecanvas.GenerationDraftInTx(ctx, tx, input)
		if err != nil {
			return err
		}
		if len(draft.References) != 2 || draft.References[0].RevisionID != images[0] || draft.References[1].RevisionID != images[1] ||
			!strings.HasPrefix(draft.Prompt, "「云」（[图1]–[图2]）") || !strings.Contains(draft.Prompt, "青绿色短发") {
			t.Fatalf("expanded draft: %+v", draft)
		}
		compiled, err := creativecanvas.CompileGenerationPromptInTx(ctx, tx, input)
		if err != nil || compiled.Text != draft.Prompt {
			t.Fatalf("compiled prompt mismatch: %+v %v", compiled, err)
		}
		now, err := tx.CreativeNow(ctx)
		if err != nil {
			return err
		}
		if _, err := creativecanvas.AdmitGenerationInTx(ctx, tx, input, creativecanvas.GenerationSpec{Kind: "image", Prompt: draft.Prompt, ModelKey: model,
			Parameters: draft.Parameters, References: draft.References}, execution.ExecutionID, uuid.NewString(), "request", now.Add(time.Hour)); err != nil {
			return err
		}
		if err := creativecanvas.GuardGenerationInTx(ctx, tx, execution); err != nil {
			return err
		}
		_, err = writeImage(tx, func(r creativecontent.Revision) error {
			return creativecanvas.RetainGenerationOutputInTx(ctx, tx, execution, 0, r)
		})
		if err != nil {
			return err
		}
		return creativecanvas.CompleteGenerationInTx(ctx, tx, execution, false, "")
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := creativecanvas.PublishGenerationExecution(ctx, scope, execution, func(context.Context, store.TxAccountScope) (bool, error) { return true, nil }); err != nil {
		t.Fatal(err)
	}
	versions, err := creativecanvas.ListNodeVersions(ctx, scope, p.CanvasID, target.ID)
	if err != nil || len(versions.Items) != 1 {
		t.Fatalf("published versions: %+v %v", versions, err)
	}
	version := versions.Items[0]
	if len(version.Inputs) != 3 || version.Inputs[2].Role != "character" || version.Inputs[2].RevisionID != saved.ContentRevisionID ||
		!strings.Contains(string(version.Provenance), saved.ContentRevisionID) {
		t.Fatalf("missing character provenance: %+v", version)
	}
	if _, err := creativelibrary.Purge(ctx, scope, command(t, creativelibrary.AssetVersion{ID: newer.ID, Revision: newer.Revision + 1})); err != nil {
		t.Fatal(err)
	}
	graph(t, scope, p.CanvasID, creativecanvas.Action{Type: "remove_nodes", NodeIDs: []string{nodeID}, GroupMode: "subtree"})
	if _, err := db.Exec("UPDATE creative_node_executions SET retained_until=clock_timestamp()-interval '1 hour' WHERE id=$1", execution.ExecutionID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE creative_change_content_refs SET expires_at=clock_timestamp()-interval '1 hour' WHERE account_id='canvas-a'"); err != nil {
		t.Fatal(err)
	}
	if err := scope.WithReadSnapshot(ctx, func(tx store.ReadTxAccountScope) error {
		owners, err := creativereference.Live(ctx, tx, []string{saved.ContentRevisionID, newer.ContentRevisionID})
		if err == nil && (!owners[saved.ContentRevisionID] || owners[newer.ContentRevisionID]) {
			t.Fatalf("only the card used in the result must survive: %+v", owners)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	key := make([]byte, 32)
	signer, err := creativemedia.NewPartSigner(key, "http://localhost/part")
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := versionedfs.NewLocal(t.TempDir(), signer)
	if err != nil {
		t.Fatal(err)
	}
	cfg := creativemedia.Config{}
	media, err := creativemedia.NewService(cfg, adapter, creativemedia.NewVerifier(cfg), key, "/media")
	if err != nil {
		t.Fatal(err)
	}
	if report, err := media.CollectGarbage(ctx, scope, creativemedia.GCDelete); err != nil || report.UnheldRevisions != 1 {
		t.Fatalf("GC should collect the unused character revision: %+v %v", report, err)
	}
	if card, err := creativecontent.Read(ctx, scope, saved.ContentRevisionID); err != nil || card.Payload.Character == nil ||
		card.Payload.Character.Appearance != "青绿色短发" {
		t.Fatalf("published card is no longer readable: %+v %v", card, err)
	}
	c = read()
	draftRevision := node(c, targetID).Prompt.Revision
	if _, err := creativecanvas.ReuseVersionPrompt(ctx, scope, command(t, creativecanvas.ReuseVersionInput{
		CanvasID: c.ID, NodeID: targetID, VersionID: version.ID, ExpectedDraftRevision: &draftRevision,
		ExpectedTopologyRevision: c.TopologyRevision, ReadSet: creativecanvas.ReadSetFor(c),
	})); err != nil {
		t.Fatal(err)
	}
	c = read()
	if err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		draft, err := creativecanvas.GenerationDraftInTx(ctx, tx, creativecanvas.RequestExecutionInput{
			CanvasID: c.ID, NodeID: targetID, DraftRevision: node(c, targetID).Prompt.Revision,
		})
		if err == nil && (len(draft.References) != 2 || !strings.HasPrefix(draft.Prompt, "「云」（[图1]–[图2]）")) {
			t.Fatalf("reused card: %+v", draft)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}

}
