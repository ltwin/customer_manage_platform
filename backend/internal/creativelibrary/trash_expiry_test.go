package creativelibrary_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/jobs"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store/storetest"
)

func TestTrashExpiryPreservesDeadlinesAndOtherAccounts(t *testing.T) {
	db, a, b := setup(t)
	expired := createAsset(t, a, "expired", nil)
	future := createAsset(t, a, "future", nil)
	forever := createAsset(t, a, "forever", nil)
	active := createAsset(t, a, "active", nil)
	foreign := createAsset(t, b, "foreign", nil)
	for _, id := range []string{expired.ID, future.ID, forever.ID} {
		r, e := creativelibrary.Trash(t.Context(), a, cmd(t, creativelibrary.AssetVersion{ID: id, Revision: 1}))
		_ = decode[creativelibrary.LibraryResult](t, r, e)
	}
	if _, err := db.Exec(`UPDATE creative_assets SET deleted_at=clock_timestamp()-interval '8 days',purge_after=clock_timestamp()-interval '1 hour' WHERE id=$1;`, expired.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE creative_assets SET purge_after=clock_timestamp()+interval '1 day' WHERE id=$1`, future.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE creative_assets SET deleted_at=clock_timestamp()-interval '8 days',purge_after=clock_timestamp()-interval '1 hour' WHERE id=$1`, foreign.ID); err != nil {
		t.Fatal(err)
	}
	before := search(t, a, creativelibrary.Search{View: "trash"})
	worker := creativelibrary.NewTrashExpiry(nil)
	job := jobs.Request{Kind: "library.trash_expiry", OperationID: uuid.NewString(), CreatedAt: time.Now(), Payload: json.RawMessage(`{}`)}
	for range 2 {
		if err := worker.Handler().Work(t.Context(), a, job); err != nil {
			t.Fatal(err)
		}
	}
	after := search(t, a, creativelibrary.Search{View: "trash"})
	if after.TotalCount != 2 || after.LibraryRevision != before.LibraryRevision+1 {
		t.Fatalf("expiry/replay: %+v", after)
	}
	for _, id := range []string{future.ID, forever.ID, active.ID} {
		if _, err := creativelibrary.GetAsset(t.Context(), a, id); err != nil {
			t.Fatal(err)
		}
	}
	if search(t, b, creativelibrary.Search{View: "trash"}).TotalCount != 1 {
		t.Fatal("cross-account deletion")
	}
}

func TestTrashExpiryBatchesAndContinuationAreAtomic(t *testing.T) {
	url := storetest.NewURL(t)
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`INSERT INTO accounts(id,password_hash,status) VALUES('library-a','test','active')`)
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.Context(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	if err := st.MigrateCreativeJobs(t.Context()); err != nil {
		t.Fatal(err)
	}
	a := st.ScopeFor(auth.AccountContext{AccountID: "library-a"})
	worker := creativelibrary.NewTrashExpiry(nil)
	runtime, err := st.NewJobRuntime([]store.JobHandler{worker.Handler()}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	seed := createAsset(t, a, "seed", nil)
	_, err = db.Exec(`INSERT INTO creative_assets(id,account_id,title,normalized_title,kind,content_id,content_revision_id,deleted_at,purge_after) SELECT 'expired-'||g,'library-a','old','old','text',content_id,content_revision_id,clock_timestamp()-interval '8 days',clock_timestamp()-interval '1 hour' FROM creative_assets CROSS JOIN generate_series(1,205) g WHERE id=$1`, seed.ID)
	if err != nil {
		t.Fatal(err)
	}
	job := jobs.Request{Kind: "library.trash_expiry", OperationID: uuid.NewString(), CreatedAt: time.Now(), Payload: json.RawMessage(`{}`)}
	// Missing queue capability forces a next-batch enqueue failure. Deletions
	// and the receipt must roll back together, then the same request can retry.
	if err := worker.Handler().Work(t.Context(), a, job); err == nil {
		t.Fatal("expected enqueue failure")
	}
	if search(t, a, creativelibrary.Search{View: "trash"}).TotalCount != 205 {
		t.Fatal("partial commit")
	}
	worker.SetRuntime(runtime)
	for range 2 {
		if err := worker.Handler().Work(t.Context(), a, job); err != nil {
			t.Fatal(err)
		}
	}
	if search(t, a, creativelibrary.Search{View: "trash"}).TotalCount != 105 {
		t.Fatal("replay deleted another batch")
	}
	var n int
	var raw []byte
	var queue string
	if err := db.QueryRow(`SELECT count(*) FROM creative_jobs.river_job`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("continuation duplicates %d %v", n, err)
	}
	if err := db.QueryRow(`SELECT args,queue FROM creative_jobs.river_job ORDER BY id DESC LIMIT 1`).Scan(&raw, &queue); err != nil {
		t.Fatal(err)
	}
	if queue != "maintenance" {
		t.Fatal(queue)
	}
	for range 2 {
		var next struct {
			Request jobs.Request `json:"request"`
		}
		if err := json.Unmarshal(raw, &next); err != nil {
			t.Fatal(err)
		}
		if err := worker.Handler().Work(t.Context(), a, next.Request); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(`SELECT args FROM creative_jobs.river_job ORDER BY id DESC LIMIT 1`).Scan(&raw); err != nil {
			t.Fatal(err)
		}
	}
	if search(t, a, creativelibrary.Search{View: "trash"}).TotalCount != 0 {
		t.Fatal("did not drain all batches")
	}
}

func TestTrashExpirySchedulesExpiredGroupSnapshotsWithoutAssets(t *testing.T) {
	url := storetest.NewURL(t)
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`INSERT INTO accounts(id,password_hash,status) VALUES('library-a','test','active')`)
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.Context(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	if err := st.MigrateCreativeJobs(t.Context()); err != nil {
		t.Fatal(err)
	}
	a := st.ScopeFor(auth.AccountContext{AccountID: "library-a"})
	for _, name := range []string{"expired", "fresh"} {
		groups, err := creativelibrary.ListGroups(t.Context(), a)
		if err != nil {
			t.Fatal(err)
		}
		r, e := creativelibrary.CreateGroup(t.Context(), a, cmd(t, creativelibrary.GroupInput{Name: name, HierarchyRevision: groups.HierarchyRevision}))
		g := decode[creativelibrary.LibraryResult](t, r, e)
		r, e = creativelibrary.DeleteGroup(t.Context(), a, cmd(t, creativelibrary.GroupInput{ID: g.ID, ExpectedRevision: 1, HierarchyRevision: g.HierarchyRevision}))
		_ = decode[creativelibrary.LibraryResult](t, r, e)
	}
	_, err = db.Exec(`UPDATE creative_asset_group_deletions SET created_at=clock_timestamp()-interval '31 days' WHERE group_snapshot->>'name'='expired'`)
	if err != nil {
		t.Fatal(err)
	}
	worker := creativelibrary.NewTrashExpiry(nil)
	runtime, err := st.NewJobRuntime([]store.JobHandler{worker.Handler()}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	worker.SetRuntime(runtime)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); worker.Run(ctx, st, slog.New(slog.DiscardHandler)) }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var count int
		if err := db.QueryRow(`SELECT count(*) FROM creative_jobs.river_job`).Scan(&count); err != nil {
			cancel()
			<-done
			t.Fatal(err)
		}
		if count > 0 {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			<-done
			t.Fatal("startup did not enqueue")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not stop")
	}
	for range 2 {
		if next, err := worker.SchedulePage(t.Context(), st, ""); err != nil || next != "" {
			t.Fatalf("schedule %q %v", next, err)
		}
	}
	var n int
	var raw []byte
	if err := db.QueryRow(`SELECT count(*) FROM creative_jobs.river_job`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("schedule dedup %d %v", n, err)
	}
	if err := db.QueryRow(`SELECT args FROM creative_jobs.river_job`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var next struct {
		Request jobs.Request `json:"request"`
	}
	if err := json.Unmarshal(raw, &next); err != nil {
		t.Fatal(err)
	}
	if err := worker.Handler().Work(t.Context(), a, next.Request); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM creative_asset_group_deletions`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("snapshot expiry %d %v", n, err)
	}
}

func TestRestoreKeepsFavoriteAndSurvivingGroups(t *testing.T) {
	_, a, _ := setup(t)
	r, e := creativelibrary.CreateGroup(t.Context(), a, cmd(t, creativelibrary.GroupInput{Name: "keep", HierarchyRevision: 1}))
	keep := decode[creativelibrary.LibraryResult](t, r, e)
	r, e = creativelibrary.CreateGroup(t.Context(), a, cmd(t, creativelibrary.GroupInput{Name: "removed", HierarchyRevision: keep.HierarchyRevision}))
	removed := decode[creativelibrary.LibraryResult](t, r, e)
	r, e = creativelibrary.CreateAsset(t.Context(), a, cmd(t, creativelibrary.CreateAssetInput{Title: "kept", Content: textDraft("hello"), GroupIDs: []string{keep.ID, removed.ID}}))
	asset := decode[creativelibrary.AssetResult](t, r, e)
	r, e = creativelibrary.Metadata(t.Context(), a, cmd(t, creativelibrary.MetadataInput{ID: asset.ID, ExpectedRevision: 1, Favorite: ptr(true)}))
	_ = decode[creativelibrary.LibraryResult](t, r, e)
	r, e = creativelibrary.Trash(t.Context(), a, cmd(t, creativelibrary.AssetVersion{ID: asset.ID, Revision: 2}))
	_ = decode[creativelibrary.LibraryResult](t, r, e)
	r, e = creativelibrary.DeleteGroup(t.Context(), a, cmd(t, creativelibrary.GroupInput{ID: removed.ID, ExpectedRevision: 1, HierarchyRevision: removed.HierarchyRevision}))
	_ = decode[creativelibrary.LibraryResult](t, r, e)
	before, err := creativelibrary.GetAsset(t.Context(), a, asset.ID)
	if err != nil {
		t.Fatal(err)
	}
	r, e = creativelibrary.Restore(t.Context(), a, cmd(t, creativelibrary.AssetVersion{ID: asset.ID, Revision: before.Revision}))
	_ = decode[creativelibrary.LibraryResult](t, r, e)
	after, err := creativelibrary.GetAsset(t.Context(), a, asset.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !after.Favorite || len(after.GroupIDs) != 1 || after.GroupIDs[0] != keep.ID {
		t.Fatalf("restore %+v", after)
	}
}
