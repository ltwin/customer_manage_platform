package dbtest

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativeimageedit"
	"github.com/samson/customer-manage-platform/backend/internal/creativemedia"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func TestApplyAtomicAndReplay(t *testing.T) {
	f := setup(t)
	node, source := f.imageNode(t, "apply")
	r, err := f.create(t, node, source, 60)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.work(t); err != nil {
		t.Fatal(err)
	}
	var data int64
	if err = f.db.QueryRow(`SELECT data_revision FROM creative_nodes WHERE id=$1`, node).Scan(&data); err != nil {
		t.Fatal(err)
	}
	cmd := command(t, creativeimageedit.ApplyInput{CanvasID: f.canvas, NodeID: node, RenderID: r.ID, ExpectedSourceRevisionID: source, ExpectedDataRevision: creativeops.Revision(data)})
	var stored, total int64
	if err = f.db.QueryRow(`SELECT stored_bytes FROM creative_media_quotas WHERE account_id='edit-a'`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(`UPDATE creative_media_quotas SET limit_bytes=stored_bytes WHERE account_id='edit-a'`); err != nil {
		t.Fatal(err)
	}
	if _, err = f.svc.Apply(t.Context(), f.a, cmd); !errors.Is(err, creativemedia.ErrQuota) {
		t.Fatalf("quota: %v", err)
	}
	if got := f.get(t, r.ID); got.State != "ready" {
		t.Fatalf("rollback: %+v", got)
	}
	if _, err = f.db.Exec(`UPDATE creative_media_quotas SET limit_bytes=67108864 WHERE account_id='edit-a'`); err != nil {
		t.Fatal(err)
	}
	if err = f.db.QueryRow(`SELECT sum(b.byte_size) FROM creative_blobs b JOIN creative_image_edit_renders r ON b.id IN (r.original_blob_id,r.display_blob_id) WHERE r.id=$1`, r.ID).Scan(&total); err != nil {
		t.Fatal(err)
	}
	receipt, err := f.svc.Apply(t.Context(), f.a, cmd)
	if err != nil {
		t.Fatal(err)
	}
	var result creativeimageedit.ApplyResult
	if err = json.Unmarshal(receipt.Outcome.Response, &result); err != nil {
		t.Fatal(err)
	}
	replay, err := f.svc.Apply(t.Context(), f.a, cmd)
	if err != nil {
		t.Fatal(err)
	}
	var replayResult creativeimageedit.ApplyResult
	if err = json.Unmarshal(replay.Outcome.Response, &replayResult); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result, replayResult) {
		t.Fatal("replay differs")
	}
	var current int64
	if err = f.db.QueryRow(`SELECT stored_bytes FROM creative_media_quotas WHERE account_id='edit-a'`).Scan(&current); err != nil {
		t.Fatal(err)
	}
	if current != stored+total {
		t.Fatalf("quota got %d want %d", current, stored+total)
	}
	got := f.get(t, r.ID)
	if got.State != "applied" || got.AppliedVersionID == nil || *got.AppliedVersionID != result.VersionID {
		t.Fatalf("render: %+v", got)
	}
	var origin, selected, revision string
	if err = f.db.QueryRow(`SELECT v.origin_kind,n.selected_version_id,n.content_revision_id FROM creative_nodes n JOIN creative_node_versions v ON v.id=n.selected_version_id WHERE n.id=$1`, node).Scan(&origin, &selected, &revision); err != nil {
		t.Fatal(err)
	}
	if origin != "edit" || selected != result.VersionID || revision != result.ContentRevisionID {
		t.Fatalf("node: %s %s %s", origin, selected, revision)
	}
}

func (f *fixture) applyCommand(t *testing.T, r creativeimageedit.Render) creativeops.Command {
	t.Helper()
	var data int64
	if err := f.db.QueryRow(`SELECT data_revision FROM creative_nodes WHERE id=$1`, r.NodeID).Scan(&data); err != nil {
		t.Fatal(err)
	}
	return command(t, creativeimageedit.ApplyInput{CanvasID: r.CanvasID, NodeID: r.NodeID, RenderID: r.ID, ExpectedSourceRevisionID: r.SourceRevisionID, ExpectedDataRevision: creativeops.Revision(data)})
}

func TestApplyCropUndoRedoAndRights(t *testing.T) {
	f := setup(t)
	node, source := f.imageNode(t, "crop")
	r, err := f.svc.Create(t.Context(), f.a, creativeimageedit.CreateInput{OperationID: uuid.NewString(), CanvasID: f.canvas, NodeID: node, SourceRevisionID: source, Action: "crop", Crop: creativeimageedit.Crop{Left: 8, Top: 8, Width: 32, Height: 48}, CallerKind: "photographer"})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.work(t); err != nil {
		t.Fatal(err)
	}
	r = f.get(t, r.ID)
	cmd := f.applyCommand(t, r)
	var original, display string
	if err = f.db.QueryRow(`SELECT original_blob_id,coalesce(display_blob_id,'') FROM creative_image_edit_renders WHERE id=$1`, r.ID).Scan(&original, &display); err != nil {
		t.Fatal(err)
	}
	before, err := creativecanvas.GetCanvas(t.Context(), f.a, f.canvas)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := f.svc.Apply(t.Context(), f.a, cmd)
	if err != nil {
		t.Fatal(err)
	}
	var result creativeimageedit.ApplyResult
	if err = json.Unmarshal(receipt.Outcome.Response, &result); err != nil {
		t.Fatal(err)
	}
	after, err := creativecanvas.GetCanvas(t.Context(), f.a, f.canvas)
	if err != nil {
		t.Fatal(err)
	}
	n := after.Nodes[0]
	if n.Height != n.Width*48/32 || n.DataRevision != before.Nodes[0].DataRevision+1 {
		t.Fatalf("geometry/data: %+v", n)
	}
	var bound, hash string
	if err = f.db.QueryRow(`SELECT o.blob_id,b.sha256 FROM creative_content_objects o JOIN creative_blobs b ON b.id=o.blob_id WHERE o.content_revision_id=$1 AND o.role='original'`, result.ContentRevisionID).Scan(&bound, &hash); err != nil {
		t.Fatal(err)
	}
	if bound != original || hash != r.Output.SHA256 {
		t.Fatal("apply replaced encoded bytes", bound, hash)
	}
	versions, err := creativecanvas.ListNodeVersions(t.Context(), f.a, f.canvas, node)
	if err != nil {
		t.Fatal(err)
	}
	var provenance map[string]any
	for _, v := range versions.Items {
		if v.ID == result.VersionID {
			if err = json.Unmarshal(v.Provenance, &provenance); err != nil {
				t.Fatal(err)
			}
		}
	}
	if provenance["source_revision_id"] != source || provenance["encoder_policy"] != creativeimageedit.PolicyVersion || provenance["render_id"] != r.ID {
		t.Fatalf("provenance: %v", provenance)
	}
	undo, err := creativecanvas.Undo(t.Context(), f.a, command(t, creativecanvas.UndoInput{CanvasID: f.canvas, ChangeID: result.ChangeID, ReadSet: creativecanvas.ReadSetFor(after)}))
	if err != nil {
		t.Fatal(err)
	}
	undone, err := creativecanvas.GetCanvas(t.Context(), f.a, f.canvas)
	if err != nil {
		t.Fatal(err)
	}
	if *undone.Nodes[0].ContentRevisionID != source || undone.Nodes[0].Height != before.Nodes[0].Height {
		t.Fatal("undo lost source geometry")
	}
	// A lost response can be recovered even after undo; it must not apply again.
	if _, err = f.svc.Apply(t.Context(), f.a, cmd); err != nil {
		t.Fatal(err)
	}
	var ur creativecanvas.ChangeResult
	if err = json.Unmarshal(undo.Outcome.Response, &ur); err != nil {
		t.Fatal(err)
	}
	if _, err = creativecanvas.Redo(t.Context(), f.a, command(t, creativecanvas.UndoInput{CanvasID: f.canvas, ChangeID: ur.ChangeID, ReadSet: creativecanvas.ReadSetFor(undone)})); err != nil {
		t.Fatal(err)
	}
	redone, err := creativecanvas.GetCanvas(t.Context(), f.a, f.canvas)
	if err != nil {
		t.Fatal(err)
	}
	if *redone.Nodes[0].ContentRevisionID != result.ContentRevisionID || redone.Nodes[0].Height != n.Height {
		t.Fatal("redo lost edited version geometry")
	}
	if _, err = f.db.Exec(`UPDATE creative_usage_grants SET revoked_at=clock_timestamp() WHERE declaration_id=(SELECT rights_declaration_id FROM creative_content_revisions WHERE id=$1)`, source); err != nil {
		t.Fatal(err)
	}
	err = f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		_, e := creativecontent.RequireUsable(t.Context(), tx, result.ContentRevisionID, "display")
		return e
	})
	if !errors.Is(err, creativecontent.ErrUsageDenied) {
		t.Fatalf("derivative escaped rights: %v", err)
	}
}

func TestApplyGuardsRollback(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*testing.T, *fixture, creativeimageedit.Render)
		want   error
	}{
		{"not_ready", func(t *testing.T, f *fixture, r creativeimageedit.Render) {}, creativeimageedit.ErrRenderNotReady},
		{"expired", func(t *testing.T, f *fixture, r creativeimageedit.Render) {
			execSQL(t, f, `UPDATE creative_image_edit_renders SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, r.ID)
		}, creativeimageedit.ErrExpired},
		{"discarded", func(t *testing.T, f *fixture, r creativeimageedit.Render) {
			if _, e := f.svc.Discard(t.Context(), f.a, r.ID); e != nil {
				t.Fatal(e)
			}
		}, creativeimageedit.ErrExpired},
		{"revision", func(t *testing.T, f *fixture, r creativeimageedit.Render) {
			execSQL(t, f, `UPDATE creative_nodes SET data_revision=data_revision+1 WHERE id=$1`, r.NodeID)
		}, creativecanvas.ErrVersionConflict},
		{"source", func(t *testing.T, f *fixture, r creativeimageedit.Render) {
			_, other := f.imageNode(t, "other")
			execSQL(t, f, `UPDATE creative_nodes SET content_revision_id=$2 WHERE id=$1`, r.NodeID, other)
		}, creativeimageedit.ErrSourceChanged},
		{"busy", func(t *testing.T, f *fixture, r creativeimageedit.Render) {
			execSQL(t, f, `UPDATE creative_nodes SET active_execution_id='ccec_busy' WHERE id=$1`, r.NodeID)
		}, creativeimageedit.ErrNodeBusy},
		{"archived", func(t *testing.T, f *fixture, r creativeimageedit.Render) {
			execSQL(t, f, `UPDATE creative_projects SET archived_at=clock_timestamp() WHERE id=(SELECT project_id FROM creative_canvases WHERE id=$1)`, f.canvas)
		}, creativecanvas.ErrArchived},
		{"rights", func(t *testing.T, f *fixture, r creativeimageedit.Render) {
			execSQL(t, f, `UPDATE creative_usage_grants SET revoked_at=clock_timestamp() WHERE declaration_id=(SELECT rights_declaration_id FROM creative_content_revisions WHERE id=$1)`, r.SourceRevisionID)
		}, creativecontent.ErrUsageDenied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setup(t)
			node, source := f.imageNode(t, "guard")
			r, err := f.create(t, node, source, 60)
			if err != nil {
				t.Fatal(err)
			}
			if tc.name != "not_ready" {
				if err = f.work(t); err != nil {
					t.Fatal(err)
				}
			}
			cmd := f.applyCommand(t, r)
			tc.mutate(t, f, r)
			before := f.scalar(t, `SELECT stored_bytes FROM creative_media_quotas WHERE account_id='edit-a'`)
			_, err = f.svc.Apply(t.Context(), f.a, cmd)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v want %v", err, tc.want)
			}
			if got := f.scalar(t, `SELECT stored_bytes FROM creative_media_quotas WHERE account_id='edit-a'`); got != before {
				t.Fatal("failed apply charged quota")
			}
			if got := f.scalar(t, `SELECT count(*) FROM creative_node_versions WHERE origin_kind='edit'`); got != "0" {
				t.Fatal("failed apply created version")
			}
			if got := f.scalar(t, `SELECT count(*) FROM creative_operation_receipts WHERE operation_type='canvas.apply_image_edit'`); got != "0" {
				t.Fatal("failed apply recorded receipt")
			}
		})
	}
}

func execSQL(t *testing.T, f *fixture, q string, args ...any) {
	t.Helper()
	if _, err := f.db.Exec(q, args...); err != nil {
		t.Fatal(err)
	}
}

func TestApplyConcurrentReplayDiscardAndGC(t *testing.T) {
	for _, kind := range []string{"replay", "discard", "gc", "expired_gc"} {
		t.Run(kind, func(t *testing.T) {
			f := setup(t)
			node, source := f.imageNode(t, "race")
			r, err := f.create(t, node, source, 60)
			if err != nil {
				t.Fatal(err)
			}
			if err = f.work(t); err != nil {
				t.Fatal(err)
			}
			cmd := f.applyCommand(t, r)
			if kind == "expired_gc" {
				execSQL(t, f, `UPDATE creative_image_edit_renders SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, r.ID)
			}
			start := make(chan struct{})
			other := make(chan error, 1)
			go func() {
				<-start
				var e error
				switch kind {
				case "replay":
					_, e = f.svc.Apply(t.Context(), f.a, cmd)
				case "discard":
					_, e = f.svc.Discard(t.Context(), f.a, r.ID)
				default:
					_, e = f.media.CollectGarbage(t.Context(), f.a, creativemedia.GCDelete)
				}
				other <- e
			}()
			close(start)
			_, err = f.svc.Apply(t.Context(), f.a, cmd)
			if e := <-other; e != nil {
				t.Fatal(e)
			}
			if kind == "expired_gc" {
				if !errors.Is(err, creativeimageedit.ErrExpired) {
					t.Fatal(err)
				}
			} else if kind == "discard" {
				if err != nil && !errors.Is(err, creativeimageedit.ErrExpired) {
					t.Fatal(err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			count := f.scalar(t, `SELECT count(*) FROM creative_node_versions WHERE origin_kind='edit'`)
			if err == nil && count != "1" || err != nil && count != "0" {
				t.Fatalf("partial apply: %v versions=%s", err, count)
			}
			var booked int64
			if e := f.db.QueryRow(`SELECT count(*) FROM creative_blobs b JOIN creative_image_edit_renders r ON b.id IN (r.original_blob_id,r.display_blob_id) WHERE r.id=$1 AND b.quota_booked_at IS NOT NULL`, r.ID).Scan(&booked); e != nil {
				t.Fatal(e)
			}
			if err == nil && booked < 1 || err != nil && booked != 0 {
				t.Fatalf("partial quota: %v booked=%d", err, booked)
			}
		})
	}
}

func TestApplyRetainsTransitiveRights(t *testing.T) {
	f := setup(t)
	node, source := f.imageNode(t, "derived-source")
	_, ancestor := f.imageNode(t, "ancestor")
	execSQL(t, f, `INSERT INTO creative_content_required_grants(account_id,content_revision_id,declaration_id) SELECT account_id,$1,rights_declaration_id FROM creative_content_revisions WHERE id=$2`, source, ancestor)
	r, err := f.create(t, node, source, 60)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.work(t); err != nil {
		t.Fatal(err)
	}
	receipt, err := f.svc.Apply(t.Context(), f.a, f.applyCommand(t, r))
	if err != nil {
		t.Fatal(err)
	}
	var result creativeimageedit.ApplyResult
	if err = json.Unmarshal(receipt.Outcome.Response, &result); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = f.db.QueryRow(`SELECT count(*) FROM creative_content_required_grants WHERE content_revision_id=$1`, result.ContentRevisionID).Scan(&count); err != nil || count != 2 {
		t.Fatal("missing inherited declarations", count, err)
	}
	execSQL(t, f, `UPDATE creative_usage_grants SET revoked_at=clock_timestamp() WHERE declaration_id=(SELECT rights_declaration_id FROM creative_content_revisions WHERE id=$1)`, ancestor)
	err = f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		_, err := creativecontent.RequireUsable(t.Context(), tx, result.ContentRevisionID, "display")
		return err
	})
	if !errors.Is(err, creativecontent.ErrUsageDenied) {
		t.Fatal("ancestor revocation bypassed", err)
	}
}
