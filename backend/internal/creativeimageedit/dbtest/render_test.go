// Package dbtest holds the database tests of creativeimageedit. They live
// apart from the codec tests, which run inside the vips toolchain container
// where no Postgres container can be started.
package dbtest

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"log/slog"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativeimageedit"
	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
	"github.com/samson/customer-manage-platform/backend/internal/creativemedia"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/jobs"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store/storetest"
	"github.com/samson/customer-manage-platform/backend/internal/platform/versionedfs"
)

func TestMain(m *testing.M) { storetest.Main(m, store.MigrateUp) }

// fakeRenderer re-encodes the source as a smaller JPEG of the same size, so
// the verifier sees a real file. hook runs before encoding.
type fakeRenderer struct {
	mu   sync.Mutex
	err  []error
	hook func()
	root string
}

func (f *fakeRenderer) fail(errs ...error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = append(f.err, errs...)
}

func (f *fakeRenderer) Render(_ context.Context, req creativeimageedit.Request) (creativeimageedit.Result, error) {
	f.mu.Lock()
	hook := f.hook
	f.hook = nil
	var err error
	if len(f.err) > 0 {
		err, f.err = f.err[0], f.err[1:]
	}
	f.mu.Unlock()
	if hook != nil {
		hook()
	}
	if err != nil {
		return creativeimageedit.Result{}, err
	}
	src, err := os.Open(req.Source)
	if err != nil {
		return creativeimageedit.Result{}, err
	}
	defer func() { _ = src.Close() }()
	img, _, err := image.Decode(src)
	if err != nil {
		return creativeimageedit.Result{}, err
	}
	out := filepath.Join(f.root, "fake-render-"+uuid.NewString()+".jpg")
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: req.Quality}); err != nil {
		return creativeimageedit.Result{}, err
	}
	if err := os.WriteFile(out, buf.Bytes(), 0o600); err != nil {
		return creativeimageedit.Result{}, err
	}
	b := img.Bounds()
	return creativeimageedit.Result{Path: out, Mime: "image/jpeg", Width: b.Dx(), Height: b.Dy(), Size: int64(buf.Len())}, nil
}

// flakyAdapter fails the next n object writes, as a storage outage would.
type flakyAdapter struct {
	versionedfs.Adapter
	mu    sync.Mutex
	fails int
	hook  func()
}

func (a *flakyAdapter) PublishVerified(ctx context.Context, key string, body io.Reader, size int64, mime string) (string, error) {
	a.mu.Lock()
	hook := a.hook
	a.hook = nil
	failing := a.fails > 0
	if failing {
		a.fails--
	}
	a.mu.Unlock()
	if hook != nil {
		hook()
	}
	if failing {
		return "", versionedfs.ErrUnknownResult
	}
	return a.Adapter.PublishVerified(ctx, key, body, size, mime)
}

type fixture struct {
	db       *sql.DB
	a        store.AccountScope
	media    *creativemedia.Service
	svc      *creativeimageedit.Service
	renderer *fakeRenderer
	adapter  *flakyAdapter
	canvas   string
}

func setup(t *testing.T) *fixture {
	t.Helper()
	url := storetest.NewURL(t)
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err = db.Exec(`INSERT INTO accounts(id,password_hash,status) VALUES ('edit-a','test','active')`); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.Context(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	if err = st.MigrateCreativeJobs(t.Context()); err != nil {
		t.Fatal(err)
	}
	cfg := creativemedia.DefaultConfig()
	cfg.QuotaLimitBytes = 64 << 20
	key := bytes.Repeat([]byte{7}, 32)
	signer, err := creativemedia.NewPartSigner(key, "http://127.0.0.1/part")
	if err != nil {
		t.Fatal(err)
	}
	local, err := versionedfs.NewLocal(filepath.Join(t.TempDir(), "media"), signer)
	if err != nil {
		t.Fatal(err)
	}
	adapter := &flakyAdapter{Adapter: local}
	media, err := creativemedia.NewService(cfg, adapter, creativemedia.NewVerifier(cfg), key, "/api/v1/creative/media")
	if err != nil {
		t.Fatal(err)
	}
	renderer := &fakeRenderer{root: t.TempDir()}
	svc := creativeimageedit.NewService(media, renderer, t.TempDir())
	runtime, err := st.NewJobRuntime(svc.Handlers(), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	svc.SetRuntime(runtime)
	f := &fixture{db: db, a: st.ScopeFor(auth.AccountContext{AccountID: "edit-a"}), media: media, svc: svc, renderer: renderer, adapter: adapter}
	r, err := creativecanvas.CreateProject(t.Context(), f.a, command(t, creativecanvas.CreateProjectInput{Name: "编辑"}))
	if err != nil {
		t.Fatal(err)
	}
	var p creativecanvas.ProjectResult
	if err := json.Unmarshal(r.Outcome.Response, &p); err != nil {
		t.Fatal(err)
	}
	f.canvas = p.CanvasID
	return f
}

func command(t *testing.T, in any) creativeops.Command {
	t.Helper()
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	return creativeops.Command{OperationID: uuid.NewString(), CreatedAt: time.Now(), Payload: raw}
}

// noisyPNG compresses badly, so a JPEG of it is always smaller.
func noisyPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 96, 64))
	rng := rand.New(rand.NewPCG(3, 4))
	for i := range img.Pix {
		img.Pix[i] = uint8(rng.IntN(256))
	}
	for i := 3; i < len(img.Pix); i += 4 {
		img.Pix[i] = 255
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// imageNode stores an image as a library asset and places it on the canvas,
// returning the node and its revision.
func (f *fixture) imageNode(t *testing.T, identity string) (string, string) {
	t.Helper()
	body := noisyPNG(t)
	p, err := f.media.PrepareGeneratedMedia(t.Context(), f.a, identity, "image", "image/png", int64(len(body)), true, func(context.Context) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil })
	if err != nil {
		t.Fatal(err)
	}
	var asset creativelibrary.AssetResult
	err = f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		if e := tx.LockCreativeWrite(t.Context()); e != nil {
			return e
		}
		if _, e := creativelibrary.LockLibraryInTx(t.Context(), tx); e != nil {
			return e
		}
		_, e := f.media.CommitGeneratedMediaInTx(t.Context(), tx, p, func(r creativecontent.Revision) error {
			a, e := creativelibrary.CreateFromRevisionInTx(t.Context(), tx, r, creativelibrary.ImportTarget{Title: identity})
			asset = a
			return e
		})
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	var topology int64
	if err := f.db.QueryRow(`SELECT topology_revision FROM creative_canvases WHERE id=$1`, f.canvas).Scan(&topology); err != nil {
		t.Fatal(err)
	}
	node := "cwnode_" + uuid.NewString()
	if _, err := creativecanvas.AddNode(t.Context(), f.a, command(t, creativecanvas.AddNodeInput{CanvasID: f.canvas, NodeID: node, TypeKey: "core.image", ExpectedTopologyRevision: creativeops.Revision(topology), Asset: &creativelibrary.AssetReference{ID: asset.ID, Revision: asset.Revision, ContentRevisionID: asset.ContentRevisionID}})); err != nil {
		t.Fatal(err)
	}
	return node, asset.ContentRevisionID
}

func (f *fixture) create(t *testing.T, node, source string, quality int) (creativeimageedit.Render, error) {
	t.Helper()
	return f.svc.Create(t.Context(), f.a, creativeimageedit.CreateInput{OperationID: uuid.NewString(), CanvasID: f.canvas, NodeID: node, SourceRevisionID: source, Action: creativeimageedit.ActionCompress, Quality: quality, CallerKind: "photographer"})
}

// work runs every queued image edit job once, as the worker would; it
// returns the first handler error and leaves failed jobs for a later call.
func (f *fixture) work(t *testing.T) error {
	t.Helper()
	rows, err := f.db.Query(`SELECT id,args FROM creative_jobs.river_job WHERE state='available' AND queue='image_edit' ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	type queued struct {
		id   int64
		args []byte
	}
	var pending []queued
	for rows.Next() {
		var q queued
		if err := rows.Scan(&q.id, &q.args); err != nil {
			t.Fatal(err)
		}
		pending = append(pending, q)
	}
	_ = rows.Close()
	var first error
	for _, q := range pending {
		var envelope struct {
			Request jobs.Request `json:"request"`
		}
		if err := json.Unmarshal(q.args, &envelope); err != nil {
			t.Fatal(err)
		}
		if err := f.svc.Work(t.Context(), f.a, envelope.Request); err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		if _, err := f.db.Exec(`UPDATE creative_jobs.river_job SET state='completed',finalized_at=now() WHERE id=$1`, q.id); err != nil {
			t.Fatal(err)
		}
	}
	return first
}

func (f *fixture) get(t *testing.T, id string) creativeimageedit.Render {
	t.Helper()
	r, err := f.svc.Get(t.Context(), f.a, id)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func (f *fixture) scalar(t *testing.T, query string, args ...any) string {
	t.Helper()
	var v string
	if err := f.db.QueryRow(query, args...).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func (f *fixture) collect(t *testing.T) creativemedia.GCReport {
	t.Helper()
	r, err := f.media.CollectGarbage(t.Context(), f.a, creativemedia.GCDelete)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// The encode result is a real, verified blob that nobody pays for until it is
// applied; while the candidate waits, GC leaves both it and the source alone.
func TestRenderBecomesReadyWithoutBookingQuota(t *testing.T) {
	f := setup(t)
	node, source := f.imageNode(t, "ready")
	stored := f.scalar(t, `SELECT stored_bytes FROM creative_media_quotas WHERE account_id='edit-a'`)
	r, err := f.create(t, node, source, 50)
	if err != nil {
		t.Fatal(err)
	}
	if r.State != "queued" || r.SourceRevisionID != source {
		t.Fatalf("created %+v", r)
	}
	if err := f.work(t); err != nil {
		t.Fatal(err)
	}
	r = f.get(t, r.ID)
	if r.State != "ready" || r.Output == nil || r.Output.Mime != "image/jpeg" || r.Output.Width != 96 || r.Output.Height != 64 || r.Output.Size >= r.Output.SourceSize {
		t.Fatalf("ready %+v %+v", r, r.Output)
	}
	if until := time.Until(r.ExpiresAt); until < 25*time.Minute || until > 31*time.Minute {
		t.Fatalf("ready candidate expires in %v, want about 30 minutes", until)
	}
	if got := f.scalar(t, `SELECT count(*) FROM creative_blobs b JOIN creative_image_edit_renders r ON r.account_id=b.account_id AND b.id=r.original_blob_id WHERE r.id=$1 AND b.state='ready' AND b.quota_booked_at IS NULL`, r.ID); got != "1" {
		t.Fatalf("result blob ready and unbooked: %s", got)
	}
	if got := f.scalar(t, `SELECT stored_bytes FROM creative_media_quotas WHERE account_id='edit-a'`); got != stored {
		t.Fatalf("stored bytes changed from %s to %s before anything was applied", stored, got)
	}
	if report := f.collect(t); report.ReclaimableBlobs != 0 || report.UnheldRevisions != 0 {
		t.Fatalf("GC touched a waiting candidate: %+v", report)
	}
}

func TestCreateIsIdempotentAndGuardsTheNodeAndTheAccount(t *testing.T) {
	f := setup(t)
	node, source := f.imageNode(t, "guard")
	in := creativeimageedit.CreateInput{OperationID: uuid.NewString(), CanvasID: f.canvas, NodeID: node, SourceRevisionID: source, Action: creativeimageedit.ActionCompress, Quality: 70, CallerKind: "photographer"}
	first, err := f.svc.Create(t.Context(), f.a, in)
	if err != nil {
		t.Fatal(err)
	}
	again, err := f.svc.Create(t.Context(), f.a, in)
	if err != nil || again.ID != first.ID {
		t.Fatalf("replay %+v %v", again, err)
	}
	changed := in
	changed.Quality = 40
	if _, err := f.svc.Create(t.Context(), f.a, changed); !errors.Is(err, creativeimageedit.ErrIdempotency) {
		t.Fatalf("same operation, other quality: %v", err)
	}
	if _, err := f.create(t, node, source, 40); !errors.Is(err, creativeimageedit.ErrNodeBusy) {
		t.Fatalf("second candidate on a busy node: %v", err)
	}
	other, otherSource := f.imageNode(t, "other")
	if _, err := f.create(t, other, source, 40); !errors.Is(err, creativeimageedit.ErrSourceChanged) {
		t.Fatalf("source that is not the node's content: %v", err)
	}
	if _, err := f.create(t, other, otherSource, 40); err != nil {
		t.Fatal(err)
	}
	third, thirdSource := f.imageNode(t, "third")
	if _, err := f.create(t, third, thirdSource, 40); !errors.Is(err, creativeimageedit.ErrBusy) {
		t.Fatalf("a third concurrent candidate for the account: %v", err)
	}
	if n := f.scalar(t, `SELECT count(*) FROM creative_jobs.river_job WHERE queue='image_edit'`); n != "2" {
		t.Fatalf("queued %s jobs, want one per accepted candidate", n)
	}
	if _, err := f.svc.Discard(t.Context(), f.a, first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.create(t, node, source, 40); err != nil {
		t.Fatalf("after discard the node takes a new edit: %v", err)
	}
}

// Discarded and expired results lose their holder and fall to GC's
// never-booked grace; the source stays with the node.
func TestUnappliedResultsAreCollected(t *testing.T) {
	f := setup(t)
	node, source := f.imageNode(t, "collect")
	discarded, err := f.create(t, node, source, 50)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.work(t); err != nil {
		t.Fatal(err)
	}
	if r, err := f.svc.Discard(t.Context(), f.a, discarded.ID); err != nil || r.State != "discarded" {
		t.Fatalf("discard %+v %v", r, err)
	}
	if again, err := f.svc.Discard(t.Context(), f.a, discarded.ID); err != nil || again.State != "discarded" {
		t.Fatalf("discard is idempotent: %+v %v", again, err)
	}
	expired, err := f.create(t, node, source, 60)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.work(t); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`UPDATE creative_image_edit_renders SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, expired.ID); err != nil {
		t.Fatal(err)
	}
	if r := f.get(t, expired.ID); r.State != "expired" {
		t.Fatalf("an overdue candidate reads as %s", r.State)
	}
	if n, err := f.svc.SweepExpired(t.Context(), f.a); err != nil || n != 1 {
		t.Fatalf("sweep %d %v", n, err)
	}
	report := f.collect(t)
	if report.ReclaimableBlobs != 2 || report.UnheldRevisions != 0 {
		t.Fatalf("GC %+v, want both unapplied results and no revision", report)
	}
	if got := f.scalar(t, `SELECT count(*) FROM creative_blobs WHERE account_id='edit-a' AND state='deleting' AND quota_booked_at IS NULL AND delete_after<clock_timestamp()+interval '16 minutes'`); got != "2" {
		t.Fatalf("unapplied results marked with the 15 minute grace: %s", got)
	}
	if f.scalar(t, `SELECT state FROM creative_content_revisions WHERE id=$1`, source) != "ready" {
		t.Fatal("the source left with the candidate")
	}
}

// A storage failure after the claim releases it and hands the half-written
// blobs to GC; the retry claims again and finishes.
func TestFailedWriteReleasesTheClaimForTheRetry(t *testing.T) {
	f := setup(t)
	node, source := f.imageNode(t, "retry")
	r, err := f.create(t, node, source, 50)
	if err != nil {
		t.Fatal(err)
	}
	f.adapter.mu.Lock()
	f.adapter.fails = 1
	f.adapter.mu.Unlock()
	if err := f.work(t); !errors.Is(err, versionedfs.ErrUnknownResult) {
		t.Fatalf("first attempt: %v", err)
	}
	if got := f.scalar(t, `SELECT state||':'||coalesce(original_blob_id,'none') FROM creative_image_edit_renders WHERE id=$1`, r.ID); got != "running:none" {
		t.Fatalf("after a failed write the candidate is %s", got)
	}
	if got := f.scalar(t, `SELECT count(*) FROM creative_blobs WHERE account_id='edit-a' AND state='deleting'`); got != "1" {
		t.Fatalf("half-written blobs handed to GC: %s", got)
	}
	if err := f.work(t); err != nil {
		t.Fatal(err)
	}
	if got := f.get(t, r.ID); got.State != "ready" {
		t.Fatalf("retry left %s", got.State)
	}
}

// A failed ready transaction leaves the published blobs attached to a running
// candidate. The next delivery must recover it, not acknowledge unfinished work.
func TestReadyTransactionFailureRecoversOnRedelivery(t *testing.T) {
	f := setup(t)
	node, source := f.imageNode(t, "ready-transaction-retry")
	r, err := f.create(t, node, source, 50)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`CREATE FUNCTION reject_edit_ready() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN RAISE EXCEPTION 'injected ready transaction failure'; END $$;
		CREATE TRIGGER reject_edit_ready BEFORE UPDATE ON creative_image_edit_renders
		FOR EACH ROW WHEN (NEW.state='ready') EXECUTE FUNCTION reject_edit_ready()`); err != nil {
		t.Fatal(err)
	}
	if err := f.work(t); err == nil {
		t.Fatal("ready transaction unexpectedly succeeded")
	}
	old := f.scalar(t, `SELECT original_blob_id FROM creative_image_edit_renders WHERE id=$1 AND state='running'`, r.ID)
	if f.scalar(t, `SELECT coalesce(object_version,'') FROM creative_blobs WHERE id=$1`, old) == "" {
		t.Fatal("failure did not happen after object publication")
	}
	if _, err := f.db.Exec(`DROP TRIGGER reject_edit_ready ON creative_image_edit_renders`); err != nil {
		t.Fatal(err)
	}
	if err := f.work(t); err != nil {
		t.Fatal(err)
	}
	if got := f.get(t, r.ID); got.State != "ready" {
		t.Fatalf("retry acknowledged unfinished candidate: %s", got.State)
	}
	if got := f.scalar(t, `SELECT state FROM creative_blobs WHERE id=$1`, old); got != "deleting" {
		t.Fatalf("abandoned output is %s, want deleting", got)
	}
}

// Redelivery can arrive while the old writer is still alive. Reclaiming its
// blob identity must fence its late completion, including its cleanup path.
func TestRedeliveryFencesThePreviousWriter(t *testing.T) {
	f := setup(t)
	node, source := f.imageNode(t, "concurrent-retry")
	r, err := f.create(t, node, source, 50)
	if err != nil {
		t.Fatal(err)
	}
	started, resume := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(resume) }) }
	f.adapter.hook = func() { close(started); <-resume }
	payload, _ := json.Marshal(map[string]string{"render_id": r.ID})
	req := jobs.Request{Kind: creativeimageedit.JobKind, OperationID: uuid.NewString(), CreatedAt: time.Now(), Payload: payload}
	done := make(chan struct{})
	var workErr error
	ctx, cancel := context.WithCancel(t.Context())
	defer func() {
		cancel()
		unblock()
		select {
		case <-done:
		case <-time.After(30 * time.Second):
			t.Error("old writer did not stop before fixture cleanup")
		}
	}()
	go func() {
		defer close(done)
		workErr = f.svc.Work(ctx, f.a, req)
	}()
	select {
	case <-started:
	case <-time.After(30 * time.Second):
		t.Fatal("first writer did not reach publication")
	}
	old := f.scalar(t, `SELECT original_blob_id FROM creative_image_edit_renders WHERE id=$1`, r.ID)
	if err := f.svc.Work(t.Context(), f.a, req); err != nil {
		t.Fatal(err)
	}
	if got := f.get(t, r.ID); got.State != "ready" {
		t.Fatalf("redelivery left %s", got.State)
	}
	newBlob := f.scalar(t, `SELECT original_blob_id FROM creative_image_edit_renders WHERE id=$1`, r.ID)
	if old == newBlob {
		t.Fatal("redelivery did not replace the unfinished claim")
	}
	unblock()
	select {
	case <-done:
		if workErr != nil {
			t.Fatal(workErr)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("old writer did not finish")
	}
	if got := f.scalar(t, `SELECT state||':'||original_blob_id FROM creative_image_edit_renders WHERE id=$1`, r.ID); got != "ready:"+newBlob {
		t.Fatalf("late writer changed result: %s", got)
	}
	if got := f.scalar(t, `SELECT state FROM creative_blobs WHERE id=$1`, old); got != "deleting" {
		t.Fatalf("old writer escaped GC: %s", got)
	}
}

func TestCancelledAttemptBeforeDeadlineCanRetry(t *testing.T) {
	f := setup(t)
	node, source := f.imageNode(t, "cancel-retry")
	r, err := f.create(t, node, source, 50)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	f.renderer.hook = cancel
	f.renderer.fail(context.Canceled)
	payload, _ := json.Marshal(map[string]string{"render_id": r.ID})
	req := jobs.Request{Kind: creativeimageedit.JobKind, OperationID: uuid.NewString(), CreatedAt: time.Now(), Payload: payload}
	if err := f.svc.Work(ctx, f.a, req); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled attempt acknowledged instead of retrying: %v", err)
	}
	if got := f.get(t, r.ID); got.State != "running" {
		t.Fatalf("cancellation before deadline left %s", got.State)
	}
	if err := f.work(t); err != nil {
		t.Fatal(err)
	}
	if got := f.get(t, r.ID); got.State != "ready" {
		t.Fatalf("retry after cancellation left %s", got.State)
	}
}

func TestCancelledOldRenderDoesNotAbandonNewWriter(t *testing.T) {
	f := setup(t)
	node, source := f.imageNode(t, "cancel-old-render")
	r, err := f.create(t, node, source, 50)
	if err != nil {
		t.Fatal(err)
	}
	renderStarted, renderResume := make(chan struct{}), make(chan struct{})
	publishStarted, publishResume := make(chan struct{}), make(chan struct{})
	var renderOnce, publishOnce sync.Once
	unblockRender := func() { renderOnce.Do(func() { close(renderResume) }) }
	unblockPublish := func() { publishOnce.Do(func() { close(publishResume) }) }
	f.renderer.hook = func() { close(renderStarted); <-renderResume }
	f.renderer.fail(context.Canceled)
	f.adapter.hook = func() { close(publishStarted); <-publishResume }
	payload, _ := json.Marshal(map[string]string{"render_id": r.ID})
	req := jobs.Request{Kind: creativeimageedit.JobKind, OperationID: uuid.NewString(), CreatedAt: time.Now(), Payload: payload}
	oldCtx, cancelOld := context.WithCancel(t.Context())
	newCtx, cancelNew := context.WithCancel(t.Context())
	var attempts sync.WaitGroup
	defer func() {
		cancelOld()
		cancelNew()
		unblockRender()
		unblockPublish()
		attempts.Wait()
	}()
	oldDone, newDone := make(chan error, 1), make(chan error, 1)
	attempts.Add(1)
	go func() { defer attempts.Done(); oldDone <- f.svc.Work(oldCtx, f.a, req) }()
	select {
	case <-renderStarted:
	case <-time.After(30 * time.Second):
		t.Fatal("old attempt did not reach rendering")
	}
	attempts.Add(1)
	go func() { defer attempts.Done(); newDone <- f.svc.Work(newCtx, f.a, req) }()
	select {
	case <-publishStarted:
	case <-time.After(30 * time.Second):
		t.Fatal("new attempt did not reach publication")
	}
	newBlob := f.scalar(t, `SELECT original_blob_id FROM creative_image_edit_renders WHERE id=$1`, r.ID)
	cancelOld()
	unblockRender()
	select {
	case err := <-oldDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("old cancellation acknowledged: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("cancelled attempt did not return")
	}
	if got := f.scalar(t, `SELECT state||':'||original_blob_id FROM creative_image_edit_renders WHERE id=$1`, r.ID); got != "running:"+newBlob {
		t.Fatalf("old cancellation changed new claim: %s", got)
	}
	if got := f.scalar(t, `SELECT state FROM creative_blobs WHERE id=$1`, newBlob); got != "verifying" {
		t.Fatalf("old cancellation abandoned new blob: %s", got)
	}
	unblockPublish()
	select {
	case err := <-newDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("new writer did not finish")
	}
	if got := f.get(t, r.ID); got.State != "ready" {
		t.Fatalf("new writer left %s", got.State)
	}
}

func TestOutcomesThatAreNotAResult(t *testing.T) {
	f := setup(t)
	node, source := f.imageNode(t, "outcomes")
	run := func(cause error) creativeimageedit.Render {
		t.Helper()
		r, err := f.create(t, node, source, 50)
		if err != nil {
			t.Fatal(err)
		}
		f.renderer.fail(cause)
		if err := f.work(t); err != nil {
			t.Fatal(err)
		}
		return f.get(t, r.ID)
	}
	if r := run(creativeimageedit.ErrNotReduced); r.State != "not_reduced" || r.Output == nil || r.Output.SourceSize == 0 {
		t.Fatalf("not reduced %+v", r)
	}
	if r := run(creativeimageedit.ErrColour); r.State != "failed" || r.ErrorCode != "colour_unknown" {
		t.Fatalf("colour %+v", r)
	}
	if got := f.scalar(t, `SELECT count(*) FROM creative_image_edit_renders WHERE original_blob_id IS NOT NULL`); got != "0" {
		t.Fatalf("an outcome without a result wrote blobs: %s", got)
	}
	// A transient failure keeps the same candidate for River's retry.
	r, err := f.create(t, node, source, 50)
	if err != nil {
		t.Fatal(err)
	}
	f.renderer.fail(errors.New("worker restarted"))
	if err := f.work(t); err == nil {
		t.Fatal("a transient failure was swallowed")
	}
	if got := f.get(t, r.ID); got.State != "running" {
		t.Fatalf("transient failure left %s", got.State)
	}
	if err := f.work(t); err != nil {
		t.Fatal(err)
	}
	if got := f.get(t, r.ID); got.State != "ready" {
		t.Fatalf("retry left %s", got.State)
	}
	// A second delivery of a finished job writes nothing.
	blobs := f.scalar(t, `SELECT count(*) FROM creative_blobs WHERE account_id='edit-a'`)
	payload, _ := json.Marshal(map[string]string{"render_id": r.ID})
	if err := f.svc.Work(t.Context(), f.a, jobs.Request{Kind: creativeimageedit.JobKind, OperationID: uuid.NewString(), CreatedAt: time.Now(), Payload: payload}); err != nil {
		t.Fatal(err)
	}
	if got := f.scalar(t, `SELECT count(*) FROM creative_blobs WHERE account_id='edit-a'`); got != blobs {
		t.Fatalf("redelivery wrote blobs: %s -> %s", blobs, got)
	}
}

// Discarding while the encode runs wins: the late result is never claimed.
func TestDiscardDuringRenderWins(t *testing.T) {
	f := setup(t)
	node, source := f.imageNode(t, "late")
	r, err := f.create(t, node, source, 50)
	if err != nil {
		t.Fatal(err)
	}
	f.renderer.hook = func() {
		if _, err := f.svc.Discard(context.Background(), f.a, r.ID); err != nil {
			t.Error(err)
		}
	}
	blobs := f.scalar(t, `SELECT count(*) FROM creative_blobs WHERE account_id='edit-a'`)
	if err := f.work(t); err != nil {
		t.Fatal(err)
	}
	if got := f.get(t, r.ID); got.State != "discarded" || got.Output != nil {
		t.Fatalf("late result %+v", got)
	}
	if got := f.scalar(t, `SELECT count(*) FROM creative_blobs WHERE account_id='edit-a'`); got != blobs {
		t.Fatalf("a discarded candidate's result was stored: %s -> %s", blobs, got)
	}
}
