package creativemedia_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"io"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
	"github.com/samson/customer-manage-platform/backend/internal/creativemedia"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/platform/versionedfs"
)

type blobRow struct {
	id, key, version, state string
	size                    int64
}

func (f *fixture) blobsOf(t *testing.T, revisionID string) []blobRow {
	t.Helper()
	rows, err := f.db.Query(`SELECT b.id,b.object_key,b.object_version,b.state,b.byte_size FROM creative_blobs b
		JOIN creative_content_objects o ON o.account_id=b.account_id AND o.blob_id=b.id
		WHERE o.content_revision_id=$1 ORDER BY b.id`, revisionID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []blobRow
	for rows.Next() {
		var b blobRow
		if err := rows.Scan(&b.id, &b.key, &b.version, &b.state, &b.size); err != nil {
			t.Fatal(err)
		}
		out = append(out, b)
	}
	if len(out) == 0 {
		t.Fatal("revision has no blobs")
	}
	return out
}

func (f *fixture) storedBytes(t *testing.T, account string) int64 {
	t.Helper()
	var n int64
	if err := f.db.QueryRow(`SELECT stored_bytes FROM creative_media_quotas WHERE account_id=$1`, account).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (f *fixture) usage(t *testing.T) creativelibrary.Usage {
	t.Helper()
	u, err := creativelibrary.GetUsage(t.Context(), f.a, f.cfg.QuotaLimitBytes)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func (f *fixture) scalar(t *testing.T, query string, args ...any) string {
	t.Helper()
	var v string
	if err := f.db.QueryRow(query, args...).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func (f *fixture) collect(t *testing.T, mode creativemedia.GCMode) creativemedia.GCReport {
	t.Helper()
	r, err := f.svc.CollectGarbage(t.Context(), f.a, mode)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// uploadAndPurge leaves an image whose only holder was a library asset that the
// photographer has now permanently deleted.
func (f *fixture) uploadAndPurge(t *testing.T) creativelibrary.Asset {
	t.Helper()
	upload := f.uploadFile(t, f.a, "gc.png", "image/png", "image", sample(t, "sample.png"), creativemedia.Target{Kind: "asset", Asset: &creativemedia.AssetTarget{Title: "gc"}})
	asset, err := creativelibrary.GetAsset(t.Context(), f.a, upload.Publication.ID)
	if err != nil {
		t.Fatal(err)
	}
	_ = must[creativelibrary.LibraryResult](t)(creativelibrary.Trash(t.Context(), f.a, command(t, creativelibrary.AssetVersion{ID: asset.ID, Revision: asset.Revision})))
	if _, err := creativelibrary.Purge(t.Context(), f.a, command(t, creativelibrary.AssetVersion{ID: asset.ID, Revision: asset.Revision + 1})); err != nil {
		t.Fatal(err)
	}
	return asset
}

// A permanently deleted asset frees its object and its quota exactly once, and
// only after the grace period; the read-only scan reports it without touching
// a single row.
func TestGCReleasesAPurgedAssetsSpaceExactlyOnce(t *testing.T) {
	f := setup(t)
	asset := f.uploadAndPurge(t)
	blobs := f.blobsOf(t, asset.ContentRevisionID)
	var total int64
	for _, b := range blobs {
		total += b.size
	}
	if got := f.storedBytes(t, "media-a"); got != total {
		t.Fatalf("stored %d, blobs %d", got, total)
	}

	scan := f.collect(t, creativemedia.GCScan)
	if scan.UnheldRevisions != 1 || scan.ReclaimableBlobs != int64(len(blobs)) || scan.ReclaimableBytes != total {
		t.Fatalf("scan %+v", scan)
	}
	if f.scalar(t, `SELECT state FROM creative_content_revisions WHERE id=$1`, asset.ContentRevisionID) != "ready" ||
		f.scalar(t, `SELECT count(*) FROM creative_blobs WHERE account_id='media-a' AND state<>'ready'`) != "0" {
		t.Fatal("the read-only scan changed state")
	}

	marked := f.collect(t, creativemedia.GCDelete)
	if marked.UnheldRevisions != 1 || marked.ReclaimableBlobs != int64(len(blobs)) || marked.DeletedBlobs != 0 {
		t.Fatalf("mark %+v", marked)
	}
	if f.scalar(t, `SELECT state FROM creative_content_revisions WHERE id=$1`, asset.ContentRevisionID) != "deleted" {
		t.Fatal("unheld revision kept")
	}
	if f.scalar(t, `SELECT count(*) FROM creative_blobs WHERE account_id='media-a' AND state='deleting' AND delete_after>clock_timestamp()+interval '23 hours'`) != strconv.Itoa(len(blobs)) {
		t.Fatal("booked blobs did not get the 24 hour grace")
	}
	if f.storedBytes(t, "media-a") != total {
		t.Fatal("quota released before the object was deleted")
	}
	if u := f.usage(t); u.StoredBytes != total || u.PendingReleaseBytes != total {
		t.Fatalf("usage while deleting: %+v", u)
	}

	if _, err := f.db.Exec(`UPDATE creative_blobs SET delete_after=clock_timestamp()-interval '1 second' WHERE account_id='media-a' AND state='deleting'`); err != nil {
		t.Fatal(err)
	}
	deleted := f.collect(t, creativemedia.GCDelete)
	if deleted.DeletedBlobs != int64(len(blobs)) || deleted.ReleasedBytes != total {
		t.Fatalf("delete %+v", deleted)
	}
	if got := f.storedBytes(t, "media-a"); got != 0 {
		t.Fatalf("stored %d after release", got)
	}
	if u := f.usage(t); u.PendingReleaseBytes != 0 {
		t.Fatalf("usage after release: %+v", u)
	}
	for _, b := range blobs {
		if _, err := f.local.StatVersion(t.Context(), b.key, b.version); !errors.Is(err, versionedfs.ErrNotFound) {
			t.Fatalf("object %s still present: %v", b.id, err)
		}
	}
	if again := f.collect(t, creativemedia.GCDelete); again != (creativemedia.GCReport{}) || f.storedBytes(t, "media-a") != 0 {
		t.Fatalf("a second pass changed something: %+v", again)
	}
}

// Content still used anywhere stays: a canvas node keeps the revision, and a
// read in progress keeps an otherwise unused blob.
func TestGCKeepsWhatIsStillInUse(t *testing.T) {
	f := setup(t)
	asset := f.uploadAndPurge(t)
	p := project(t, f.a)
	if _, err := f.db.Exec(`INSERT INTO creative_nodes(id,account_id,canvas_id,type_key,x,y,content_id,content_revision_id) VALUES('gc-node','media-a',$1,'core.image',0,0,$2,$3)`, p.CanvasID, asset.ContentID, asset.ContentRevisionID); err != nil {
		t.Fatal(err)
	}
	if r := f.collect(t, creativemedia.GCDelete); r != (creativemedia.GCReport{}) {
		t.Fatalf("content on a canvas was collected: %+v", r)
	}

	blob := f.blobsOf(t, asset.ContentRevisionID)[0]
	if _, err := f.db.Exec(`DELETE FROM creative_nodes WHERE id='gc-node'`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`INSERT INTO creative_blob_read_pins(id,account_id,blob_id,owner_kind,expires_at) VALUES('gc-pin','media-a',$1,'download',clock_timestamp()+interval '2 minutes')`, blob.id); err != nil {
		t.Fatal(err)
	}
	f.collect(t, creativemedia.GCDelete)
	if f.scalar(t, `SELECT state FROM creative_blobs WHERE id=$1`, blob.id) != "ready" {
		t.Fatal("a blob being read was marked for deletion")
	}
}

// An import that stopped making progress is reset the way a new attempt would
// reset it: its reservation comes back, its objects go to reclamation, and the
// commit it was waiting for can no longer land.
func TestGCResetsAStaleGenerationImport(t *testing.T) {
	f := setup(t)
	body := sample(t, "sample.png")
	p, err := f.svc.PrepareGeneratedMedia(t.Context(), f.a, "gc-generation", "image", "image/png", int64(len(body)), true, func(context.Context) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil })
	if err != nil {
		t.Fatal(err)
	}
	if f.scalar(t, `SELECT reserved_bytes FROM creative_media_quotas WHERE account_id='media-a'`) == "0" {
		t.Fatal("the import reserved nothing")
	}
	if r := f.collect(t, creativemedia.GCDelete); r.StaleImports != 0 {
		t.Fatalf("a fresh import was reset: %+v", r)
	}
	if _, err := f.db.Exec(`UPDATE creative_generation_imports SET updated_at=clock_timestamp()-interval '49 hours' WHERE account_id='media-a'`); err != nil {
		t.Fatal(err)
	}
	if r := f.collect(t, creativemedia.GCScan); r.StaleImports != 1 {
		t.Fatalf("scan %+v", r)
	}
	if r := f.collect(t, creativemedia.GCDelete); r.StaleImports != 1 {
		t.Fatalf("delete %+v", r)
	}
	if f.scalar(t, `SELECT state||'/'||reserved_bytes||'/'||original_blob FROM creative_generation_imports WHERE account_id='media-a'`) != "writing/0/" ||
		f.scalar(t, `SELECT reserved_bytes FROM creative_media_quotas WHERE account_id='media-a'`) != "0" ||
		f.scalar(t, `SELECT count(*) FROM creative_blobs WHERE account_id='media-a' AND state<>'deleting'`) != "0" {
		t.Fatal("the stale import was not fully reset")
	}
	err = f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		if e := tx.LockCreativeWrite(t.Context()); e != nil {
			return e
		}
		_, e := f.svc.CommitGeneratedMediaInTx(t.Context(), tx, p, func(creativecontent.Revision) error { return nil })
		return e
	})
	if !errors.Is(err, creativemedia.ErrEpoch) {
		t.Fatalf("a late commit after the reset: %v", err)
	}
}

func TestGCDoesNotReleaseUnbookedRenditions(t *testing.T) {
	f := setup(t)
	var big bytes.Buffer
	if err := png.Encode(&big, image.NewRGBA(image.Rect(0, 0, 1800, 1200))); err != nil {
		t.Fatal(err)
	}
	target := creativemedia.Target{Kind: "asset", Asset: &creativemedia.AssetTarget{Title: "retained"}}
	first := f.uploadFile(t, f.a, "first.png", "image/png", "image", big.Bytes(), target)
	if first.State != "ready" {
		t.Fatalf("first: %+v", first)
	}
	before := f.storedBytes(t, "media-a")
	if _, err := f.db.Exec(`UPDATE creative_media_quotas SET limit_bytes=stored_bytes+$1 WHERE account_id='media-a'`, big.Len()); err != nil {
		t.Fatal(err)
	}
	rejected := f.uploadFile(t, f.a, "rejected.png", "image/png", "image", big.Bytes(), target)
	if rejected.State != "failed" || rejected.ErrorCode != "quota_exceeded" {
		t.Fatalf("rejected: %+v", rejected)
	}
	if got := f.scalar(t, `SELECT count(*) FROM creative_blobs WHERE state='deleting' AND quota_booked_at IS NULL`); got != "2" {
		t.Fatalf("rejected original and display must be unbooked and deleting: %s", got)
	}
	f.collect(t, creativemedia.GCDelete)
	if _, err := f.db.Exec(`UPDATE creative_blobs SET delete_after=clock_timestamp()-interval '1 second' WHERE state='deleting'`); err != nil {
		t.Fatal(err)
	}
	r := f.collect(t, creativemedia.GCDelete)
	after := f.storedBytes(t, "media-a")
	t.Logf("GC %+v; retained ready blobs=%s", r, f.scalar(t, `SELECT count(*) FROM creative_blobs WHERE state='ready'`))
	if after != before {
		t.Fatalf("GC debited bytes never booked: before=%d after=%d", before, after)
	}
}
func TestGCMismatchedStorageDoesNotStarve(t *testing.T) {
	f := setup(t)
	if _, err := f.db.Exec(`INSERT INTO creative_blobs(id,account_id,storage_driver,bucket,object_key,sha256,byte_size,mime,state,delete_after)
 SELECT 'foreign-'||n,'media-a','oss','old-bucket','creative-v2/media-a/blobs/foreign-'||n,'sha256-'||repeat('1',64),1,'image/png','deleting',clock_timestamp()-interval '2 days' FROM generate_series(1,100) n;
 INSERT INTO creative_blobs(id,account_id,storage_driver,object_key,sha256,byte_size,mime,state,delete_after)
 VALUES('local-due','media-a','local','creative-v2/media-a/blobs/local-due','sha256-'||repeat('1',64),1,'image/png','deleting',clock_timestamp()-interval '1 day')`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		r := f.collect(t, creativemedia.GCDelete)
		if r.SkippedBlobs != 100 || (i == 0 && r.DeletedBlobs != 1) || (i > 0 && r.DeletedBlobs != 0) {
			t.Fatalf("pass %d: %+v", i, r)
		}
	}
	if got := f.scalar(t, `SELECT state FROM creative_blobs WHERE id='local-due'`); got != "deleted" {
		t.Fatalf("eligible local blob starved for three passes: %s", got)
	}
}

// Corruption must stop both the dry-run report and deletion, without blocking
// a different account. The guard runs under the same barrier as marking.
func TestGCQuarantinesBrokenOwnerReferences(t *testing.T) {
	for _, owner := range []string{"asset", "node"} {
		t.Run(owner, func(t *testing.T) {
			f := setup(t)
			v := f.uploadFile(t, f.a, "held.png", "image/png", "image", sample(t, "sample.png"), creativemedia.Target{Kind: "asset", Asset: &creativemedia.AssetTarget{Title: "held"}})
			if v.State != "ready" {
				t.Fatalf("upload: %+v", v)
			}
			revision := f.scalar(t, `SELECT content_revision_id FROM creative_assets WHERE id=$1`, v.Publication.ID)
			if owner == "asset" {
				if _, err := f.db.Exec(`UPDATE creative_assets SET content_id='broken-content' WHERE id=$1`, v.Publication.ID); err != nil {
					t.Fatal(err)
				}
			} else {
				p := project(t, f.a)
				if _, err := f.db.Exec(`INSERT INTO creative_nodes(id,account_id,canvas_id,type_key,x,y,content_id,content_revision_id) VALUES('broken-node','media-a',$1,'core.image',0,0,'broken-content',$2);`, p.CanvasID, revision); err != nil {
					t.Fatal(err)
				}
				if _, err := f.db.Exec(`DELETE FROM creative_assets WHERE id=$1`, v.Publication.ID); err != nil {
					t.Fatal(err)
				}
			}
			for _, mode := range []creativemedia.GCMode{creativemedia.GCScan, creativemedia.GCDelete} {
				r, err := f.svc.CollectGarbage(t.Context(), f.a, mode)
				if !errors.Is(err, store.ErrCreativeTextReferences) || r != (creativemedia.GCReport{}) {
					t.Fatalf("mode %s: report=%+v err=%v", mode, r, err)
				}
			}
			if state := f.scalar(t, `SELECT state FROM creative_content_revisions WHERE id=$1`, revision); state != "ready" {
				t.Fatalf("corrupt owner's revision: %s", state)
			}
			if got := f.scalar(t, `SELECT count(*) FROM creative_blobs WHERE account_id='media-a' AND state<>'ready'`); got != "0" {
				t.Fatalf("quarantine changed blobs: %s", got)
			}
			if got := f.scalar(t, `SELECT count(*) FROM creative_content_objects WHERE content_revision_id=$1`, revision); got != "1" {
				t.Fatalf("quarantine lost binding: %s", got)
			}
			if _, err := f.svc.CollectGarbage(t.Context(), f.b, creativemedia.GCDelete); err != nil {
				t.Fatalf("another account was quarantined: %v", err)
			}
		})
	}
}

type failingGCDeletes struct {
	versionedfs.Adapter
	prefix      string
	failure     error
	afterDelete bool
}

func (a *failingGCDeletes) DeleteCurrent(ctx context.Context, key string) error {
	if a.failure != nil && strings.HasPrefix(key, a.prefix) {
		if a.afterDelete {
			if err := a.Adapter.DeleteCurrent(ctx, key); err != nil {
				return err
			}
		}
		return a.failure
	}
	return a.Adapter.DeleteCurrent(ctx, key)
}

func (f *fixture) failGCDeletes(t *testing.T, prefix string, failure error, afterDelete bool) *failingGCDeletes {
	t.Helper()
	adapter := &failingGCDeletes{Adapter: f.local, prefix: prefix, failure: failure, afterDelete: afterDelete}
	svc, err := creativemedia.NewService(f.cfg, adapter, creativemedia.NewVerifier(f.cfg), f.key, "/api/v1/creative/media")
	if err != nil {
		t.Fatal(err)
	}
	f.svc = svc
	return adapter
}

func TestGCFailedBatchYieldsToLaterDueObjects(t *testing.T) {
	f := setup(t)
	asset := f.uploadAndPurge(t)
	f.collect(t, creativemedia.GCDelete)
	stored := f.storedBytes(t, "media-a")
	if _, err := f.db.Exec(`UPDATE creative_blobs SET delete_after=clock_timestamp()-interval '1 day' WHERE state='deleting';
 INSERT INTO creative_blobs(id,account_id,storage_driver,object_key,sha256,byte_size,mime,state,delete_after)
 SELECT 'failed-'||n,'media-a','local','creative-v2/media-a/blobs/failed-'||n,'sha256-'||repeat('1',64),1,'image/png','deleting',clock_timestamp()-interval '2 days' FROM generate_series(1,100) n`); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("delete unavailable")
	adapter := f.failGCDeletes(t, "creative-v2/media-a/blobs/failed-", failure, false)
	r, err := f.svc.CollectGarbage(t.Context(), f.a, creativemedia.GCDelete)
	if !errors.Is(err, failure) || r.DeletedBlobs != 0 || f.storedBytes(t, "media-a") != stored {
		t.Fatalf("failed pass: %+v %v", r, err)
	}
	if got := f.scalar(t, `SELECT count(*) FROM creative_blobs WHERE id LIKE 'failed-%' AND state='deleting' AND delete_after>clock_timestamp()`); got != "100" {
		t.Fatalf("failures were not postponed: %s", got)
	}
	r = f.collect(t, creativemedia.GCDelete)
	if r.DeletedBlobs != 1 || r.ReleasedBytes != stored {
		t.Fatalf("later object starved: %+v", r)
	}
	if got := f.scalar(t, `SELECT state FROM creative_content_revisions WHERE id=$1`, asset.ContentRevisionID); got != "deleted" {
		t.Fatal(got)
	}
	adapter.failure = nil
	if _, err := f.db.Exec(`UPDATE creative_blobs SET delete_after=clock_timestamp()-interval '1 second' WHERE id LIKE 'failed-%'`); err != nil {
		t.Fatal(err)
	}
	if r = f.collect(t, creativemedia.GCDelete); r.DeletedBlobs != 100 || r.ReleasedBytes != 0 {
		t.Fatalf("retry: %+v", r)
	}
}

func TestGCDeletionFailureRetainsQuotaUntilConfirmedRetry(t *testing.T) {
	for _, afterDelete := range []bool{false, true} {
		t.Run(strconv.FormatBool(afterDelete), func(t *testing.T) {
			f := setup(t)
			asset := f.uploadAndPurge(t)
			blobs := f.blobsOf(t, asset.ContentRevisionID)
			stored := f.storedBytes(t, "media-a")
			f.collect(t, creativemedia.GCDelete)
			if _, err := f.db.Exec(`UPDATE creative_blobs SET delete_after=clock_timestamp()-interval '1 second' WHERE state='deleting'`); err != nil {
				t.Fatal(err)
			}
			adapter := f.failGCDeletes(t, "creative-v2/", versionedfs.ErrUnknownResult, afterDelete)
			r, err := f.svc.CollectGarbage(t.Context(), f.a, creativemedia.GCDelete)
			if !errors.Is(err, versionedfs.ErrUnknownResult) || r.ReleasedBytes != 0 || f.storedBytes(t, "media-a") != stored {
				t.Fatalf("unknown result released quota: %+v %v", r, err)
			}
			if got := f.scalar(t, `SELECT count(*) FROM creative_blobs WHERE state='deleting' AND quota_released_at IS NULL`); got != strconv.Itoa(len(blobs)) {
				t.Fatalf("unknown state: %s", got)
			}
			adapter.failure = nil
			if _, err := f.db.Exec(`UPDATE creative_blobs SET delete_after=clock_timestamp()-interval '1 second' WHERE state='deleting'`); err != nil {
				t.Fatal(err)
			}
			r = f.collect(t, creativemedia.GCDelete)
			if r.DeletedBlobs != int64(len(blobs)) || r.ReleasedBytes != stored || f.storedBytes(t, "media-a") != 0 {
				t.Fatalf("retry: %+v", r)
			}
			if r = f.collect(t, creativemedia.GCDelete); r != (creativemedia.GCReport{}) {
				t.Fatalf("duplicate release: %+v", r)
			}
		})
	}
}

func TestGCStillCollectsInactiveAccounts(t *testing.T) {
	f := setup(t)
	f.uploadAndPurge(t)
	if _, err := f.db.Exec(`UPDATE accounts SET status='pending_verification' WHERE id='media-a'`); err != nil {
		t.Fatal(err)
	}
	if r := f.collect(t, creativemedia.GCDelete); r.UnheldRevisions != 1 || r.ReclaimableBlobs != 1 {
		t.Fatalf("inactive account: %+v", r)
	}
}

// A new reference and GC never both win. Whoever holds the account write
// barrier first decides: GC waits for a writer that is adding a holder and then
// sees it; a reference that arrives after GC deleted the revision is refused
// instead of pointing at deleted content.
func TestGCSerializesWithANewReference(t *testing.T) {
	f := setup(t)
	asset := f.uploadAndPurge(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	locked, release, written := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	// Release and cancel before joining, including when a readiness wait fails.
	letGo := sync.OnceFunc(func() { close(release) })
	var workers sync.WaitGroup
	defer func() {
		cancel()
		letGo()
		workers.Wait()
	}()
	workers.Add(1)
	go func() {
		defer workers.Done()
		written <- f.a.WithTxScope(ctx, func(tx store.TxAccountScope) error {
			if err := tx.LockCreativeWrite(ctx); err != nil {
				return err
			}
			if err := tx.Insert(ctx, "creative_revision_holds", []string{"id", "content_revision_id", "reason"}, "gc-hold", asset.ContentRevisionID, "recovery"); err != nil {
				return err
			}
			close(locked)
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	select {
	case <-locked:
	case err := <-written:
		t.Fatalf("writer exited before acquiring the reference: %v", err)
	case <-ctx.Done():
		t.Fatalf("waiting for writer readiness: %v", ctx.Err())
	}
	type collectionResult struct {
		report creativemedia.GCReport
		err    error
	}
	collected := make(chan collectionResult, 1)
	workers.Add(1)
	go func() {
		defer workers.Done()
		r, err := f.svc.CollectGarbage(ctx, f.a, creativemedia.GCDelete)
		collected <- collectionResult{report: r, err: err}
	}()
	select {
	case r := <-collected:
		t.Fatalf("GC ran past a writer holding the barrier: %+v", r)
	case <-time.After(300 * time.Millisecond):
	case <-ctx.Done():
		t.Fatalf("waiting for GC to block: %v", ctx.Err())
	}
	letGo()
	select {
	case err := <-written:
		if err != nil {
			t.Fatalf("writer failed: %v", err)
		}
	case <-ctx.Done():
		t.Fatalf("waiting for writer completion: %v", ctx.Err())
	}
	select {
	case result := <-collected:
		if result.err != nil {
			t.Fatal(result.err)
		}
		if r := result.report; r.UnheldRevisions != 0 || r.ReclaimableBlobs != 0 {
			t.Fatalf("GC collected what the writer had just referenced: %+v", r)
		}
	case <-ctx.Done():
		t.Fatalf("waiting for GC completion: %v", ctx.Err())
	}
	if f.scalar(t, `SELECT state FROM creative_content_revisions WHERE id=$1`, asset.ContentRevisionID) != "ready" {
		t.Fatal("referenced revision deleted")
	}

	if _, err := f.db.Exec(`DELETE FROM creative_revision_holds WHERE id='gc-hold'`); err != nil {
		t.Fatal(err)
	}
	if r := f.collect(t, creativemedia.GCDelete); r.UnheldRevisions != 1 {
		t.Fatalf("unreferenced revision kept: %+v", r)
	}
	err := f.a.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.LockCreativeWrite(ctx); err != nil {
			return err
		}
		_, err := creativecontent.RequireUsable(ctx, tx, asset.ContentRevisionID, "display")
		return err
	})
	if !errors.Is(err, creativecontent.ErrNotFound) {
		t.Fatalf("reference to deleted content: want ErrNotFound, got %v", err)
	}
}
