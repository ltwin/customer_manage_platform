package dbtest

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/creativeimageedit"
	"github.com/samson/customer-manage-platform/backend/internal/platform/jobs"
)

// workInBackground plays the worker while an agent call waits: it runs the
// first image edit job that shows up. It never touches t from its goroutine.
func (f *fixture) workInBackground(ctx context.Context) <-chan error {
	done := make(chan error, 1)
	go func() {
		for {
			var id int64
			var args []byte
			err := f.db.QueryRowContext(ctx, `SELECT id,args FROM creative_jobs.river_job WHERE state='available' AND queue='image_edit' ORDER BY id LIMIT 1`).Scan(&id, &args)
			if err == nil {
				var envelope struct {
					Request jobs.Request `json:"request"`
				}
				if err = json.Unmarshal(args, &envelope); err == nil {
					err = f.svc.Work(ctx, f.a, envelope.Request)
				}
				if err == nil {
					_, err = f.db.ExecContext(ctx, `UPDATE creative_jobs.river_job SET state='completed',finalized_at=now() WHERE id=$1`, id)
				}
				done <- err
				return
			}
			select {
			case <-ctx.Done():
				done <- ctx.Err()
				return
			case <-time.After(50 * time.Millisecond):
			}
		}
	}()
	return done
}

// The assistant's compress goes through the panel's own path: one candidate,
// one synchronous adoption, real sizes in the report. A recovered call finds
// what it already did instead of encoding or adopting again.
func TestAgentCompressAdoptsOnceAndReportsRealSizes(t *testing.T) {
	f := setup(t)
	node, source := f.imageNode(t, "agent")
	worker := f.workInBackground(t.Context())
	in := creativeimageedit.AgentCompress{Seed: "run-1/step-1", CanvasID: f.canvas, NodeID: node, Quality: 60, Wait: 30 * time.Second}
	got, err := f.svc.CompressForAgent(t.Context(), f.a, in)
	if err != nil {
		t.Fatal(err)
	}
	if err = <-worker; err != nil {
		t.Fatal(err)
	}
	if got.State != creativeimageedit.AgentApplied || got.SourceRevisionID != source || got.OutputMIME != "image/jpeg" ||
		got.OutputSize <= 0 || got.OutputSize >= got.SourceSize || got.ReductionPercent <= 0 || got.VersionID == "" || got.ContentRevisionID == "" {
		t.Fatalf("result %+v", got)
	}
	if shown := f.scalar(t, `SELECT content_revision_id FROM creative_nodes WHERE id=$1`, node); shown != got.ContentRevisionID {
		t.Fatalf("node shows %s, want %s", shown, got.ContentRevisionID)
	}
	again, err := f.svc.CompressForAgent(t.Context(), f.a, in)
	if err != nil || again.State != creativeimageedit.AgentApplied || again.VersionID != got.VersionID {
		t.Fatalf("replay %+v %v", again, err)
	}
	if n := f.scalar(t, `SELECT count(*)::text FROM creative_image_edit_renders WHERE node_id=$1`, node); n != "1" {
		t.Fatalf("renders %s", n)
	}
	if n := f.scalar(t, `SELECT count(*)::text FROM creative_node_versions WHERE node_id=$1 AND origin_kind='edit'`, node); n != "1" {
		t.Fatalf("edit versions %s", n)
	}
}

// A wait that runs out before adoption discards the candidate: the node stays
// as it was and is free for the photographer. Replaying the call does not
// queue the encode again.
func TestAgentCompressTimeoutLeavesTheNodeAlone(t *testing.T) {
	f := setup(t)
	node, source := f.imageNode(t, "agent-timeout")
	in := creativeimageedit.AgentCompress{Seed: "run-2/step-1", CanvasID: f.canvas, NodeID: node, Wait: 0}
	got, err := f.svc.CompressForAgent(t.Context(), f.a, in)
	if err != nil || got.State != creativeimageedit.AgentNotApplied || got.Quality != creativeimageedit.QualityDefault {
		t.Fatalf("result %+v %v", got, err)
	}
	if state := f.get(t, got.RenderID).State; state != "discarded" {
		t.Fatalf("candidate %s", state)
	}
	if shown := f.scalar(t, `SELECT content_revision_id FROM creative_nodes WHERE id=$1`, node); shown != source {
		t.Fatal("node changed")
	}
	if _, err = f.create(t, node, source, 70); err != nil {
		t.Fatal("node still busy after the agent gave up:", err)
	}
	again, err := f.svc.CompressForAgent(t.Context(), f.a, in)
	if err != nil || again.State != creativeimageedit.AgentNotApplied || again.RenderID != got.RenderID {
		t.Fatalf("replay %+v %v", again, err)
	}
}

// A source the node no longer shows, or a node with work in flight, is
// refused in words before anything is queued.
func TestAgentCompressRefusesWhatItCannotDo(t *testing.T) {
	f := setup(t)
	node, _ := f.imageNode(t, "agent-refuse")
	_, other := f.imageNode(t, "agent-other")
	if _, err := f.create(t, node, f.scalar(t, `SELECT content_revision_id FROM creative_nodes WHERE id=$1`, node), 70); err != nil {
		t.Fatal(err)
	}
	var refusal *creativeimageedit.Refusal
	for name, in := range map[string]creativeimageedit.AgentCompress{
		"stale source":  {Seed: "run-3/a", CanvasID: f.canvas, NodeID: node, SourceRevisionID: other},
		"busy node":     {Seed: "run-3/b", CanvasID: f.canvas, NodeID: node},
		"missing node":  {Seed: "run-3/c", CanvasID: f.canvas, NodeID: "cwnode_missing"},
		"quality range": {Seed: "run-3/d", CanvasID: f.canvas, NodeID: node, Quality: 101},
	} {
		if _, err := f.svc.CompressForAgent(t.Context(), f.a, in); !errors.As(err, &refusal) {
			t.Fatalf("%s: %v", name, err)
		}
	}
}
