package store

import (
	"context"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
)

func TestCanvasEventsCommitIsolationAndReconnect(t *testing.T) {
	db, scope := openReadSnapshotTestStore(t, "events-a")
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	other := db.ScopeFor(auth.AccountContext{AccountID: "events-b"})
	if err := scope.Insert(ctx, "creative_canvases", []string{"id", "project_id", "name"}, "canvas-a", "project", "A"); err != nil {
		t.Fatal(err)
	}
	subscribe := func(s AccountScope, id string) <-chan struct{} {
		t.Helper()
		ch, stop, err := db.SubscribeCanvas(ctx, s, id)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(stop)
		select {
		case <-ch:
		case <-ctx.Done():
			t.Fatal("missing initial resync")
		}
		return ch
	}
	a := subscribe(scope, "canvas-a")
	b := subscribe(other, "canvas-a")
	c := subscribe(scope, "canvas-c")
	quiet := func(ch <-chan struct{}) {
		t.Helper()
		select {
		case <-ch:
			t.Fatal("unexpected event before commit / outside scope")
		case <-time.After(100 * time.Millisecond):
		}
	}
	event := func(ch <-chan struct{}) {
		t.Helper()
		select {
		case <-ch:
		case <-ctx.Done():
			t.Fatal("missing committed event", ctx.Err())
		}
	}
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, "UPDATE creative_canvases SET revision=revision+1 WHERE id='canvas-a'"); err != nil {
		t.Fatal(err)
	}
	quiet(a)
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	event(a)
	quiet(b)
	quiet(c)
	tx, err = db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, "UPDATE creative_canvases SET revision=revision+1 WHERE id='canvas-a'"); err != nil {
		t.Fatal(err)
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	quiet(a)
	// Status-only writes do not advance canvas.revision but must still notify.
	if err := scope.Insert(ctx, "creative_nodes", []string{"id", "canvas_id", "type_key", "x", "y"}, "node-a", "canvas-a", "core.text", 0, 0); err != nil {
		t.Fatal(err)
	}
	event(a)
	if _, err := scope.Update(ctx, "creative_nodes", "status_revision=status_revision+1", "id=$2", "node-a"); err != nil {
		t.Fatal(err)
	}
	event(a)
	// Project changes invalidate all subscribed canvases of this account only.
	if err := scope.Insert(ctx, "creative_projects", []string{"id", "name"}, "project", "Renamed"); err != nil {
		t.Fatal(err)
	}
	event(a)
	event(c)
	quiet(b)
	// Break the listener from another connection, as a DB failover would.
	var terminated int
	err = db.pool.QueryRow(ctx, `WITH listeners AS MATERIALIZED (SELECT pid FROM pg_stat_activity WHERE datname=current_database() AND query='LISTEN creative_canvas_changed' AND pid <> pg_backend_pid()) SELECT count(*) FROM listeners WHERE pg_terminate_backend(pid)`).Scan(&terminated)
	if err != nil || terminated != 1 {
		t.Fatalf("terminate listener: %d %v", terminated, err)
	}
	event(a)
	event(b)
	event(c) // listener recovery always forces resync
	if _, err := scope.Update(ctx, "creative_canvases", "revision=revision+1", "id=$2", "canvas-a"); err != nil {
		t.Fatal(err)
	}
	event(a)
}
