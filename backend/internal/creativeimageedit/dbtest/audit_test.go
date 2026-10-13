package dbtest

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/creativeimageedit"
	"github.com/samson/customer-manage-platform/backend/internal/creativemedia"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func TestApplyMetadataConflictCanRetryOriginalCandidate(t *testing.T) {
	f := setup(t)
	node, source := f.imageNode(t, "metadata")
	r, err := f.create(t, node, source, 60)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.work(t); err != nil {
		t.Fatal(err)
	}
	old := f.applyCommand(t, r)
	canvas, err := creativecanvas.GetCanvas(t.Context(), f.a, f.canvas)
	if err != nil {
		t.Fatal(err)
	}
	_, err = creativecanvas.ApplyCommands(t.Context(), f.a, command(t, creativecanvas.BatchInput{CanvasID: f.canvas, ReadSet: creativecanvas.ReadSetFor(canvas), Actions: []creativecanvas.Action{{Type: "update_metadata", NodeID: node, Title: "新标题", Intent: "新意图"}}}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.svc.Apply(t.Context(), f.a, old); !errors.Is(err, creativecanvas.ErrVersionConflict) {
		t.Fatalf("metadata conflict: got %v want revision conflict", err)
	}
	var in creativeimageedit.ApplyInput
	if err = json.Unmarshal(old.Payload, &in); err != nil {
		t.Fatal(err)
	}
	in.ExpectedDataRevision++
	old.Payload, _ = json.Marshal(in)
	if _, err = f.svc.Apply(t.Context(), f.a, old); err != nil {
		t.Fatal("retry original candidate", err)
	}
}

func TestApplyClearedSourceIsSourceConflict(t *testing.T) {
	f := setup(t)
	node, source := f.imageNode(t, "clear")
	r, err := f.create(t, node, source, 60)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.work(t); err != nil {
		t.Fatal(err)
	}
	cmd := f.applyCommand(t, r)
	execSQL(t, f, `UPDATE creative_nodes SET content_id=NULL,content_revision_id=NULL,selected_version_id=NULL,data_revision=data_revision+1 WHERE id=$1`, node)
	if _, err = f.svc.Apply(t.Context(), f.a, cmd); !errors.Is(err, creativeimageedit.ErrSourceChanged) {
		t.Fatalf("cleared source: %v", err)
	}
}

// Accepting an edit refuses a cleared or busy node with the same errors as
// adopting one, so callers handle both with one branch.
func TestCreateSharesApplySourceErrors(t *testing.T) {
	f := setup(t)
	// Both nodes exist before either is altered behind the canvas's back.
	cleared, clearedSource := f.imageNode(t, "create-cleared")
	busy, busySource := f.imageNode(t, "create-busy")
	execSQL(t, f, `UPDATE creative_nodes SET content_id=NULL,content_revision_id=NULL,selected_version_id=NULL,data_revision=data_revision+1 WHERE id=$1`, cleared)
	if _, err := f.create(t, cleared, clearedSource, 60); !errors.Is(err, creativeimageedit.ErrSourceChanged) {
		t.Fatalf("cleared node: %v", err)
	}
	execSQL(t, f, `UPDATE creative_nodes SET active_execution_id='ccec_busy' WHERE id=$1`, busy)
	_, err := f.create(t, busy, busySource, 60)
	var candidate creativeimageedit.BusyError
	if !errors.Is(err, creativeimageedit.ErrNodeBusy) || errors.As(err, &candidate) {
		t.Fatalf("executing node: %v", err)
	}
}

func TestOversizedCropRejectedBeforeQueue(t *testing.T) {
	f := setup(t)
	node, source := f.imageNode(t, "oversized")
	_, err := f.svc.Create(t.Context(), f.a, creativeimageedit.CreateInput{OperationID: command(t, map[string]string{}).OperationID, CanvasID: f.canvas, NodeID: node, SourceRevisionID: source, Action: "crop", Crop: creativeimageedit.Crop{Left: math.MaxInt, Width: 2, Height: 2}, CallerKind: "photographer"})
	if !errors.Is(err, creativeops.ErrValidation) {
		t.Fatalf("oversized crop accepted: %v", err)
	}
}

func TestRenderPollingDoesNotTakeWriteBarrier(t *testing.T) {
	f := setup(t)
	node, source := f.imageNode(t, "poll")
	r, err := f.create(t, node, source, 60)
	if err != nil {
		t.Fatal(err)
	}
	err = f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		if err := tx.LockCreativeWrite(t.Context()); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		defer cancel()
		_, err := f.svc.Get(ctx, f.a, r.ID)
		return err
	})
	if err != nil {
		t.Fatalf("poll blocked by writer: %v", err)
	}
}

func TestFailedRenderHasSourceSizeWithoutOutput(t *testing.T) {
	for _, cause := range []error{creativeimageedit.ErrNotReduced, creativeimageedit.ErrColour} {
		t.Run(cause.Error(), func(t *testing.T) {
			f := setup(t)
			node, source := f.imageNode(t, "failed")
			f.renderer.fail(cause)
			r, err := f.create(t, node, source, 60)
			if err != nil {
				t.Fatal(err)
			}
			if err = f.work(t); err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(f.get(t, r.ID))
			if err != nil {
				t.Fatal(err)
			}
			var out map[string]any
			if err = json.Unmarshal(raw, &out); err != nil {
				t.Fatal(err)
			}
			if out["output"] != nil || out["source_size"] == nil {
				t.Fatalf("fake output: %s", raw)
			}
		})
	}
}

func TestCapabilitiesUseRunningVerifierLimits(t *testing.T) {
	f := setupWithConfig(t, func(cfg *creativemedia.Config) {
		cfg.MaxImageEdge = 100
		cfg.MaxImagePixels = 8000
		cfg.ImageMaxBytes = 20000
	})
	cap := f.svc.Capabilities()
	if cap.MaxEdge != 100 || cap.MaxPixels != 8000 || cap.MaxOutputBytes != 20000 || cap.ReadyTTLSeconds != 1800 {
		t.Fatalf("wrong limits: %+v", cap)
	}
	node, source := f.imageNode(t, "limits")
	_, err := f.svc.Create(t.Context(), f.a, creativeimageedit.CreateInput{OperationID: command(t, map[string]string{}).OperationID, CanvasID: f.canvas, NodeID: node, SourceRevisionID: source, Action: "crop", Crop: creativeimageedit.Crop{Width: 101, Height: 2}, CallerKind: "photographer"})
	if !errors.Is(err, creativeops.ErrValidation) {
		t.Fatalf("configured crop limit not enforced: %v", err)
	}
}
