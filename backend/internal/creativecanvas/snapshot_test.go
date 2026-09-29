package creativecanvas_test

import (
	"context"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func TestCanvasSnapshotDoesNotWaitForAccountWriter(t *testing.T) {
	_, a, _ := setup(t)
	p := project(t, a)
	// The writer remains open until after the read completes; a locking read
	// times out. The timeout is a deadlock guard, not a performance assertion.
	err := a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		if err := tx.LockCreativeWrite(t.Context()); err != nil {
			return err
		}
		if _, err := tx.Update(t.Context(), "creative_projects", "name=$2", "id=$3", "uncommitted", p.ProjectID); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		canvas, err := creativecanvas.GetCanvas(ctx, a, p.CanvasID)
		if err != nil {
			return err
		}
		if canvas.ProjectName == "uncommitted" {
			t.Fatal("snapshot read uncommitted data")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	canvas, err := creativecanvas.GetCanvas(t.Context(), a, p.CanvasID)
	if err != nil {
		t.Fatal(err)
	}
	if canvas.ProjectName != "uncommitted" {
		t.Fatalf("later read missed commit: %q", canvas.ProjectName)
	}
}
