package store

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
)

func insertSceneJob(t *testing.T, scope AccountScope, n int, document, kind string) string {
	t.Helper()
	id := fmt.Sprintf("csjob_00000000-0000-4000-8000-%012d", n)
	err := scope.WithTxScope(t.Context(), func(tx TxAccountScope) error {
		return tx.Insert(t.Context(), "creative_scene_jobs", []string{"id", "document_id", "kind", "request", "deadline", "created_at"},
			id, document, kind, `{}`, time.Now().Add(30*time.Minute), time.Now().Add(time.Duration(n)*time.Millisecond))
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func claimSceneJob(t *testing.T, scope AccountScope, c SceneJobClaim) (ClaimedSceneJob, bool) {
	t.Helper()
	var job ClaimedSceneJob
	var ok bool
	err := scope.WithTxScope(t.Context(), func(tx TxAccountScope) error {
		var err error
		job, ok, err = tx.ClaimCreativeSceneJob(t.Context(), c)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return job, ok
}

func claimFor(session, document string, kinds ...string) SceneJobClaim {
	if len(kinds) == 0 {
		kinds = []string{"bake_model", "render_view"}
	}
	return SceneJobClaim{DocumentID: document, Kinds: kinds, Token: "token-" + session, Session: session, Lease: 30 * time.Second}
}

func TestClaimSceneJobOrderScopeAndKinds(t *testing.T) {
	db, scope := openReadSnapshotTestStore(t, "jobs-a")
	other := db.ScopeFor(auth.AccountContext{AccountID: "jobs-b"})
	if err := db.CreateAccount(t.Context(), "jobs-b", "test-hash"); err != nil {
		t.Fatal(err)
	}
	if _, ok := claimSceneJob(t, scope, claimFor("tab_aaaaaaaaaaaaaaaa", "")); ok {
		t.Fatal("claimed from an empty queue")
	}
	render := insertSceneJob(t, scope, 1, "doc-1", "render_view")
	bake := insertSceneJob(t, scope, 2, "doc-2", "bake_model")
	insertSceneJob(t, other, 3, "doc-3", "bake_model")

	if _, ok := claimSceneJob(t, scope, claimFor("tab_aaaaaaaaaaaaaaaa", "doc-9")); ok {
		t.Fatal("claimed a job of another scene")
	}
	got, ok := claimSceneJob(t, scope, claimFor("tab_aaaaaaaaaaaaaaaa", "", "bake_model"))
	if !ok || got.ID != bake {
		t.Fatalf("kinds filter: %+v %v", got, ok)
	}
	got, ok = claimSceneJob(t, scope, claimFor("tab_bbbbbbbbbbbbbbbb", "doc-1"))
	if !ok || got.ID != render || got.DocumentID != "doc-1" || got.Attempt != 0 {
		t.Fatalf("document claim: %+v %v", got, ok)
	}
	if !got.LeaseUntil.After(time.Now()) || got.LeaseUntil.After(got.Deadline) {
		t.Fatalf("lease %v deadline %v", got.LeaseUntil, got.Deadline)
	}
	if _, ok := claimSceneJob(t, scope, claimFor("tab_cccccccccccccccc", "")); ok {
		t.Fatal("claimed a job twice or across accounts")
	}
	var state, token, session string
	if err := scope.QueryRow(t.Context(), "creative_scene_jobs", "state,claim_token,editor_session_id", "id=$2", render).Scan(&state, &token, &session); err != nil {
		t.Fatal(err)
	}
	if state != "claimed" || token != "token-tab_bbbbbbbbbbbbbbbb" || session != "tab_bbbbbbbbbbbbbbbb" {
		t.Fatalf("row %s %s %s", state, token, session)
	}
}

// Tabs asking at once each get a different job, or none.
func TestClaimSceneJobConcurrentClaimersSplitTheQueue(t *testing.T) {
	_, scope := openReadSnapshotTestStore(t, "jobs-race")
	for round := range 20 {
		insertSceneJob(t, scope, round*2+1, "doc", "bake_model")
		insertSceneJob(t, scope, round*2+2, "doc", "bake_model")
		var wg sync.WaitGroup
		got := make([]string, 3)
		for i := range 3 {
			wg.Go(func() {
				err := scope.WithTxScope(context.Background(), func(tx TxAccountScope) error {
					job, ok, err := tx.ClaimCreativeSceneJob(context.Background(), claimFor(fmt.Sprintf("tab_%016d", i), ""))
					if ok {
						got[i] = job.ID
					}
					return err
				})
				if err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		seen := map[string]bool{}
		for _, id := range got {
			if id == "" {
				continue
			}
			if seen[id] {
				t.Fatalf("round %d: %s claimed twice: %v", round, id, got)
			}
			seen[id] = true
		}
		if len(seen) != 2 {
			t.Fatalf("round %d: claimed %v", round, got)
		}
	}
}

func TestSavepointRollsBackOnlyItsOwnWork(t *testing.T) {
	_, scope := openReadSnapshotTestStore(t, "jobs-savepoint")
	boom := errors.New("boom")
	err := scope.WithTxScope(t.Context(), func(tx TxAccountScope) error {
		if err := tx.Insert(t.Context(), "creative_canvases", []string{"id", "project_id", "name"}, "kept", "p1", "A"); err != nil {
			return err
		}
		err := tx.Savepoint(t.Context(), "scene_write", func() error {
			if err := tx.Insert(t.Context(), "creative_canvases", []string{"id", "project_id", "name"}, "undone", "p2", "B"); err != nil {
				return err
			}
			return boom
		})
		if !errors.Is(err, boom) {
			return fmt.Errorf("savepoint error %v", err)
		}
		// A failed statement inside the savepoint leaves the transaction usable too.
		err = tx.Savepoint(t.Context(), "scene_write", func() error {
			return tx.Insert(t.Context(), "creative_canvases", []string{"id", "project_id", "name"}, "kept", "p3", "dup")
		})
		if err == nil {
			return errors.New("duplicate insert succeeded")
		}
		return tx.Savepoint(t.Context(), "scene_write", func() error {
			return tx.Insert(t.Context(), "creative_canvases", []string{"id", "project_id", "name"}, "after", "p4", "C")
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]bool{"kept": true, "undone": false, "after": true} {
		exists, err := scope.Exists(t.Context(), "creative_canvases", "id=$2", id)
		if err != nil {
			t.Fatal(err)
		}
		if exists != want {
			t.Errorf("%s exists=%v", id, exists)
		}
	}
	if err := scope.WithTxScope(t.Context(), func(tx TxAccountScope) error {
		return tx.Savepoint(t.Context(), "bad name;", func() error { return nil })
	}); err == nil {
		t.Fatal("unsafe savepoint name accepted")
	}
}
