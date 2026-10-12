package creativemedia_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
	"github.com/samson/customer-manage-platform/backend/internal/creativemedia"
	"github.com/samson/customer-manage-platform/backend/internal/creativepalette"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/jobs"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/platform/versionedfs"
)

func solidPNG(t *testing.T, c color.NRGBA) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 20, 20))
	for y := 0; y < 20; y++ {
		for x := 0; x < 20; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func paletteJob() jobs.Request {
	return jobs.Request{Kind: creativemedia.PaletteBackfillKind, OperationID: uuid.NewString(), CreatedAt: time.Now(), Payload: json.RawMessage(`{"algorithm":"` + creativepalette.Algorithm + `"}`)}
}
func paletteWork(t *testing.T, s *creativemedia.Service) func(context.Context, store.AccountScope, jobs.Request) error {
	t.Helper()
	for _, h := range s.Handlers() {
		if h.Kind == creativemedia.PaletteBackfillKind {
			return h.Work
		}
	}
	t.Fatal("missing palette handler")
	return nil
}
func paletteCount(t *testing.T, f *fixture, where string) int {
	t.Helper()
	var n int
	if err := f.db.QueryRow("SELECT count(*) FROM creative_blob_palettes WHERE " + where).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
func TestPalettePublicationAndAuthorizedFilters(t *testing.T) {
	f := setup(t)
	makeAsset := func(scope store.AccountScope, name string, c color.NRGBA, w, h int) creativelibrary.Asset {
		u := f.uploadFile(t, scope, name+".png", "image/png", "image", solidPNG(t, c), creativemedia.Target{Kind: "asset", Asset: &creativemedia.AssetTarget{Title: name}})
		a, err := creativelibrary.GetAsset(t.Context(), scope, u.Publication.ID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.db.Exec(`UPDATE creative_blobs SET width=$2,height=$3 WHERE id=$1`, a.Content.Media[0].BlobID, w, h); err != nil {
			t.Fatal(err)
		}
		return a
	}
	warm := makeAsset(f.a, "暖调", color.NRGBA{224, 128, 64, 255}, 111, 100)
	gray := makeAsset(f.a, "黑白", color.NRGBA{128, 128, 128, 255}, 110, 100)
	transparent := makeAsset(f.a, "透明", color.NRGBA{}, 100, 111)
	_ = makeAsset(f.b, "另一个账号", color.NRGBA{224, 128, 64, 255}, 111, 100)
	if len(warm.Palette) != 1 || warm.Palette[0].Hex != "#e08040" || len(transparent.Palette) != 0 {
		t.Fatalf("palettes %+v %+v", warm.Palette, transparent.Palette)
	}
	search := func(q creativelibrary.Search) creativelibrary.AssetPage {
		q.Limit = 1
		q.IncludeFacets = q.Cursor == ""
		p, e := creativelibrary.SearchAssets(t.Context(), f.a, q)
		if e != nil {
			t.Fatal(e)
		}
		return p
	}
	for _, tc := range []struct {
		q  creativelibrary.Search
		id string
	}{{creativelibrary.Search{Color: "#e08040"}, warm.ID}, {creativelibrary.Search{Color: "#808080"}, gray.ID}, {creativelibrary.Search{Shape: "square"}, gray.ID}, {creativelibrary.Search{Shape: "landscape"}, warm.ID}, {creativelibrary.Search{Shape: "portrait"}, transparent.ID}} {
		p := search(tc.q)
		if p.TotalCount != 1 || p.Items[0].ID != tc.id || p.Facets.Kinds["image"] != 1 {
			t.Fatalf("q=%+v page=%+v", tc.q, p)
		}
	}
	for _, q := range []creativelibrary.Search{{Color: "#gggggg"}, {Color: "red"}, {Shape: "wide"}} {
		q.Limit = 1
		if _, err := creativelibrary.SearchAssets(t.Context(), f.a, q); !errors.Is(err, creativeops.ErrValidation) {
			t.Fatal("invalid accepted", err)
		}
	}
	page := search(creativelibrary.Search{})
	if _, err := creativelibrary.SearchAssets(t.Context(), f.a, creativelibrary.Search{Limit: 1, Cursor: page.NextCursor, Color: "#e08040"}); !errors.Is(err, creativeops.ErrValidation) {
		t.Fatal("cursor not bound", err)
	}
	// A revoked display grant removes derived matches and counts, not metadata.
	if _, err := f.db.Exec(`UPDATE creative_usage_grants SET revoked_at=clock_timestamp() WHERE account_id='media-a' AND declaration_id=(SELECT rights_declaration_id FROM creative_content_revisions WHERE id=$1)`, warm.ContentRevisionID); err != nil {
		t.Fatal(err)
	}
	for _, q := range []creativelibrary.Search{{Color: "#e08040"}, {Shape: "landscape"}} {
		p := search(q)
		if p.TotalCount != 0 || p.Facets.Kinds["image"] != 0 {
			t.Fatalf("revoked leaked %+v", p)
		}
	}
	a, err := creativelibrary.GetAsset(t.Context(), f.a, warm.ID)
	if err != nil || !a.Unavailable || len(a.Palette) != 0 || a.Content != nil {
		t.Fatalf("revoked detail %+v %v", a, err)
	}
	// Dependency grant revocation follows the same guard in list, counts and detail.
	if _, err := f.db.Exec(`INSERT INTO creative_content_required_grants(account_id,content_revision_id,declaration_id) SELECT 'media-a',$1,rights_declaration_id FROM creative_content_revisions WHERE account_id='media-a' AND id=$2`, gray.ContentRevisionID, warm.ContentRevisionID); err != nil {
		t.Fatal(err)
	}
	if p := search(creativelibrary.Search{Color: "#808080"}); p.TotalCount != 0 || p.Facets.Kinds["image"] != 0 {
		t.Fatal("dependent color leaked")
	}
	if p := search(creativelibrary.Search{Shape: "square"}); p.TotalCount != 0 {
		t.Fatal("dependent dimensions leaked")
	}
	if a, err := creativelibrary.GetAsset(t.Context(), f.a, gray.ID); err != nil || !a.Unavailable || len(a.Palette) != 0 {
		t.Fatalf("dependent palette leaked %+v %v", a, err)
	}

}

func TestGeneratedImportPublishesPaletteInSameTransaction(t *testing.T) {
	f := setup(t)
	body := solidPNG(t, color.NRGBA{224, 128, 64, 255})
	p, err := f.svc.PrepareGeneratedMedia(t.Context(), f.a, "palette-generation", "image", "image/png", int64(len(body)), true, func(context.Context) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil })
	if err != nil {
		t.Fatal(err)
	}
	var id string
	commit := func(fail bool) error {
		return f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
			if e := tx.LockCreativeWrite(t.Context()); e != nil {
				return e
			}
			if _, e := creativelibrary.LockLibraryInTx(t.Context(), tx); e != nil {
				return e
			}
			_, e := f.svc.CommitGeneratedMediaInTx(t.Context(), tx, p, func(r creativecontent.Revision) error {
				a, e := creativelibrary.CreateFromRevisionInTx(t.Context(), tx, r, creativelibrary.ImportTarget{Title: "生成图片"})
				id = a.ID
				return e
			})
			if e != nil {
				return e
			}
			if fail {
				return errors.New("rollback")
			}
			return nil
		})
	}
	if err = commit(true); err == nil {
		t.Fatal("expected rollback")
	}
	if paletteCount(t, f, "account_id='media-a'") != 0 {
		t.Fatal("palette escaped transaction")
	}
	if err = commit(false); err != nil {
		t.Fatal(err)
	}
	a, err := creativelibrary.GetAsset(t.Context(), f.a, id)
	if err != nil || len(a.Palette) != 1 {
		t.Fatalf("%+v %v", a, err)
	}
}

type paletteAdapter struct {
	versionedfs.Adapter
	open func(context.Context, string, string, *versionedfs.ByteRange) (io.ReadCloser, versionedfs.ObjectStat, error)
}

func (a paletteAdapter) OpenVersion(ctx context.Context, key, version string, rng *versionedfs.ByteRange) (io.ReadCloser, versionedfs.ObjectStat, error) {
	return a.open(ctx, key, version, rng)
}
func paletteService(t *testing.T, f *fixture, adapter versionedfs.Adapter) *creativemedia.Service {
	t.Helper()
	s, e := creativemedia.NewService(f.cfg, adapter, creativemedia.NewVerifier(f.cfg), f.key, "/api/v1/creative/media")
	if e != nil {
		t.Fatal(e)
	}
	s.SetRuntime(f.runtime)
	return s
}
func seedPaletteBlobs(t *testing.T, f *fixture, n int) {
	t.Helper()
	body := sample(t, "sample.png")
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("palette-%03d", i)
		key := "palette-test/" + id
		version, e := f.local.PublishVerified(t.Context(), key, bytes.NewReader(body), int64(len(body)), "image/png")
		if e != nil {
			t.Fatal(e)
		}
		_, e = f.db.Exec(`INSERT INTO creative_blobs(id,account_id,storage_driver,object_key,object_version,sha256,byte_size,mime,width,height,state,verified_at) VALUES($1,'media-a','local',$2,$3,'sha256-'||repeat('a',64),$4,'image/png',64,64,'ready',now());`, id, key, version, len(body))
		if e != nil {
			t.Fatal(e)
		}
		_, e = f.db.Exec(`INSERT INTO creative_content_objects(account_id,content_revision_id,blob_id,role,position) VALUES('media-a',$1,$2,'original',0)`, "rev-"+id, id)
		if e != nil {
			t.Fatal(e)
		}
	}
}
func TestPaletteBackfillRecoveryAndAtomicContinuation(t *testing.T) {
	f := setup(t)
	seedPaletteBlobs(t, f, 105)
	calls := 0
	fail := true
	adapter := paletteAdapter{Adapter: f.local, open: func(ctx context.Context, key, version string, rng *versionedfs.ByteRange) (io.ReadCloser, versionedfs.ObjectStat, error) {
		calls++
		if fail && calls == 2 {
			return nil, versionedfs.ObjectStat{}, context.DeadlineExceeded
		}
		var pins int
		if e := f.db.QueryRow(`SELECT count(*) FROM creative_blob_read_pins WHERE account_id='media-a' AND expires_at>now()`).Scan(&pins); e != nil || pins == 0 {
			t.Fatal("read without pin", e)
		}
		return f.local.OpenVersion(ctx, key, version, rng)
	}}
	svc := paletteService(t, f, adapter)
	work := paletteWork(t, svc)
	job := paletteJob()
	if e := work(t.Context(), f.a, job); !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal(e)
	}
	if paletteCount(t, f, "true") != 0 {
		t.Fatal("transient failure wrote partial or failed records")
	}
	fail = false
	svc.SetRuntime(nil)
	if e := work(t.Context(), f.a, job); e == nil {
		t.Fatal("continuation enqueue must fail")
	}
	if paletteCount(t, f, "true") != 0 {
		t.Fatal("enqueue rollback lost")
	}
	svc.SetRuntime(f.runtime)
	if e := work(t.Context(), f.a, job); e != nil {
		t.Fatal(e)
	}
	if paletteCount(t, f, "state='ready'") != 50 {
		t.Fatal("batch size")
	}
	if e := work(t.Context(), f.a, job); e != nil {
		t.Fatal(e)
	}
	if paletteCount(t, f, "true") != 50 {
		t.Fatal("replay wrote another batch")
	}
	// A fresh service resumes using the persisted queue continuation.
	f.drain(t, f.a)
	if paletteCount(t, f, "state='ready'") != 105 {
		t.Fatal("incomplete backfill")
	}
	if e := paletteWork(t, f.svc)(t.Context(), f.a, paletteJob()); e != nil {
		t.Fatal(e)
	}
	var pins int
	if e := f.db.QueryRow(`SELECT count(*) FROM creative_blob_read_pins`).Scan(&pins); e != nil || pins != 0 {
		t.Fatalf("pins %d %v", pins, e)
	}
	// Rebuild touches only the requested account and supported algorithm.
	if _, e := f.db.Exec(`INSERT INTO creative_blob_palettes(account_id,blob_id,algorithm,state) VALUES('media-b','other-account',$1,'failed'),('media-a','other-version','legacy','failed')`, creativepalette.Algorithm); e != nil {
		t.Fatal(e)
	}
	if e := creativemedia.RebuildPalettes(t.Context(), f.a, f.runtime, "unknown"); !errors.Is(e, creativeops.ErrValidation) {
		t.Fatal(e)
	}
	if e := creativemedia.RebuildPalettes(t.Context(), f.a, nil, creativepalette.Algorithm); e == nil {
		t.Fatal("expected enqueue failure")
	}
	if paletteCount(t, f, "account_id='media-a' AND state='ready'") != 105 {
		t.Fatal("rebuild rollback")
	}
	if e := creativemedia.RebuildPalettes(t.Context(), f.a, f.runtime, creativepalette.Algorithm); e != nil {
		t.Fatal(e)
	}
	if paletteCount(t, f, "true") != 2 {
		t.Fatal("rebuild crossed account/algorithm")
	}
	f.drain(t, f.a)
	if paletteCount(t, f, "account_id='media-a' AND state='ready'") != 105 {
		t.Fatal("rebuild incomplete")
	}
}
func TestPaletteBackfillBadImageAndDeletingBlob(t *testing.T) {
	f := setup(t)
	seedPaletteBlobs(t, f, 3)
	svc := paletteService(t, f, paletteAdapter{Adapter: f.local, open: func(ctx context.Context, key, version string, rng *versionedfs.ByteRange) (io.ReadCloser, versionedfs.ObjectStat, error) {
		if _, e := f.db.Exec(`UPDATE creative_blobs SET state='deleting' WHERE id='palette-002'`); e != nil {
			t.Fatal(e)
		}
		if key == "palette-test/palette-001" {
			return io.NopCloser(bytes.NewBufferString("broken png")), versionedfs.ObjectStat{Size: 10}, nil
		}
		return f.local.OpenVersion(ctx, key, version, rng)
	}})
	if e := paletteWork(t, svc)(t.Context(), f.a, paletteJob()); e != nil {
		t.Fatal(e)
	}
	if paletteCount(t, f, "state='failed' AND failure_reason='image_decode'") != 1 || paletteCount(t, f, "state='ready'") != 1 || paletteCount(t, f, "blob_id='palette-002'") != 0 {
		t.Fatal("failure classification or deleting skip")
	}
	var pins int
	if e := f.db.QueryRow(`SELECT count(*) FROM creative_blob_read_pins`).Scan(&pins); e != nil || pins != 0 {
		t.Fatal("pins leaked", e)
	}
}

func TestPaletteStartupPagesAccountsAndDeduplicates(t *testing.T) {
	f := setup(t)
	seedPaletteBlobs(t, f, 1)
	if _, err := f.db.Exec(`INSERT INTO accounts(id,password_hash,status) SELECT 'empty-'||g,'test','active' FROM generate_series(1,105)g`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := f.svc.SchedulePalettes(t.Context(), f.store); err != nil {
			t.Fatal(err)
		}
	}
	var jobs int
	if err := f.db.QueryRow(`SELECT count(*) FROM creative_jobs.river_job WHERE args->'request'->>'kind'=$1`, creativemedia.PaletteBackfillKind).Scan(&jobs); err != nil || jobs != 1 {
		t.Fatalf("jobs %d %v", jobs, err)
	}
	f.drain(t, f.a)
	if paletteCount(t, f, "state='ready'") != 1 {
		t.Fatal("startup missed later account page")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	done := make(chan struct{})
	go func() { defer close(done); f.svc.RunPaletteBackfill(ctx, f.store, slog.New(slog.DiscardHandler)) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not stop")
	}
}
func TestPaletteBackfillPrefersPinnedDisplay(t *testing.T) {
	f := setup(t)
	f.cfg.DisplayMaxEdge = 16
	f.svc = paletteService(t, f, f.local)
	f.uploadFile(t, f.a, "display.jpg", "image/jpeg", "image", sample(t, "sample.jpg"), creativemedia.Target{Kind: "asset", Asset: &creativemedia.AssetTarget{Title: "显示图"}})
	if _, err := f.db.Exec(`DELETE FROM creative_blob_palettes`); err != nil {
		t.Fatal(err)
	}
	seen := false
	svc := paletteService(t, f, paletteAdapter{Adapter: f.local, open: func(ctx context.Context, key, version string, rng *versionedfs.ByteRange) (io.ReadCloser, versionedfs.ObjectStat, error) {
		seen = true
		if !strings.HasSuffix(key, "/display") {
			t.Fatalf("read %s", key)
		}
		var pins int
		if err := f.db.QueryRow(`SELECT count(*) FROM creative_blob_read_pins WHERE expires_at>now()`).Scan(&pins); err != nil || pins != 2 {
			t.Fatalf("original/display pins %d %v", pins, err)
		}
		return f.local.OpenVersion(ctx, key, version, rng)
	}})
	if err := paletteWork(t, svc)(t.Context(), f.a, paletteJob()); err != nil {
		t.Fatal(err)
	}
	if !seen || paletteCount(t, f, "state='ready'") != 1 {
		t.Fatal("display not used")
	}
}

func TestPaletteShortObjectReadIsRetryable(t *testing.T) {
	f := setup(t)
	seedPaletteBlobs(t, f, 1)
	svc := paletteService(t, f, paletteAdapter{Adapter: f.local, open: func(context.Context, string, string, *versionedfs.ByteRange) (io.ReadCloser, versionedfs.ObjectStat, error) {
		return io.NopCloser(bytes.NewBufferString("part")), versionedfs.ObjectStat{Size: 100}, nil
	}})
	if err := paletteWork(t, svc)(t.Context(), f.a, paletteJob()); err == nil {
		t.Fatal("short stream accepted")
	}
	if paletteCount(t, f, "true") != 0 {
		t.Fatal("short stream marked permanent failure")
	}
}

func TestPaletteShapeExcludesVideoUntilOrientationIsKnown(t *testing.T) {
	f := setup(t)
	supported := false
	for _, format := range f.svc.Capabilities().Formats {
		supported = supported || format.Mime == "video/mp4"
	}
	if !supported {
		t.Skip("ffprobe unavailable")
	}
	u := f.uploadFile(t, f.a, "video.mp4", "video/mp4", "video", sample(t, "sample.mp4"), creativemedia.Target{Kind: "asset", Asset: &creativemedia.AssetTarget{Title: "视频"}})
	a, err := creativelibrary.GetAsset(t.Context(), f.a, u.Publication.ID)
	if err != nil {
		t.Fatal(err)
	}

	if a.Kind != "video" || a.Content == nil {
		t.Fatal("uploaded video missing")
	}
	for _, shape := range []string{"landscape", "square", "portrait"} {
		p, err := creativelibrary.SearchAssets(t.Context(), f.a, creativelibrary.Search{Limit: 10, Shape: shape, IncludeFacets: true})
		if err != nil || p.TotalCount != 0 || len(p.Items) != 0 || p.Facets.Kinds["video"] != 0 {
			t.Fatalf("shape %s included video: %+v %v", shape, p, err)
		}
	}
	p, err := creativelibrary.SearchAssets(t.Context(), f.a, creativelibrary.Search{Limit: 10, Kind: "video", IncludeFacets: true})
	if err != nil || p.TotalCount != 1 || p.Items[0].ID != a.ID || p.Facets.Kinds["video"] != 1 {
		t.Fatalf("unfiltered video lost %+v %v", p, err)
	}

}

func TestPaletteSearchScale(t *testing.T) {
	if os.Getenv("CREATIVE_PALETTE_BENCHMARK") != "1" {
		t.Skip("set CREATIVE_PALETTE_BENCHMARK=1 for 5000 distinct image facts")
	}
	f := setup(t)
	u := f.uploadFile(t, f.a, "warm.png", "image/png", "image", solidPNG(t, color.NRGBA{224, 128, 64, 255}), creativemedia.Target{Kind: "asset", Asset: &creativemedia.AssetTarget{Title: "暖调"}})
	a, err := creativelibrary.GetAsset(t.Context(), f.a, u.Publication.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, seed := range []struct {
		sql string
		id  string
	}{
		{`INSERT INTO creative_contents SELECT (jsonb_populate_record(NULL::creative_contents,to_jsonb(r)||jsonb_build_object('id','bench-content-'||n))).* FROM creative_contents r CROSS JOIN generate_series(1,5000)n WHERE r.id=$1`, a.ContentID},
		{`INSERT INTO creative_content_revisions SELECT (jsonb_populate_record(NULL::creative_content_revisions,to_jsonb(r)||jsonb_build_object('id','bench-rev-'||n,'content_id','bench-content-'||n))).* FROM creative_content_revisions r CROSS JOIN generate_series(1,5000)n WHERE r.id=$1`, a.ContentRevisionID},
		{`INSERT INTO creative_blobs SELECT (jsonb_populate_record(NULL::creative_blobs,to_jsonb(r)||jsonb_build_object('id','bench-blob-'||n,'object_key','bench-key-'||n))).* FROM creative_blobs r CROSS JOIN generate_series(1,5000)n WHERE r.id=$1`, a.Content.Media[0].BlobID},
		{`INSERT INTO creative_content_objects SELECT (jsonb_populate_record(NULL::creative_content_objects,to_jsonb(r)||jsonb_build_object('content_revision_id','bench-rev-'||n,'blob_id','bench-blob-'||n))).* FROM creative_content_objects r CROSS JOIN generate_series(1,5000)n WHERE r.content_revision_id=$1 AND r.role='original'`, a.ContentRevisionID},
		{`INSERT INTO creative_blob_palettes SELECT (jsonb_populate_record(NULL::creative_blob_palettes,to_jsonb(r)||jsonb_build_object('blob_id','bench-blob-'||n))).* FROM creative_blob_palettes r CROSS JOIN generate_series(1,5000)n WHERE r.blob_id=$1`, a.Content.Media[0].BlobID},
		{`INSERT INTO creative_assets SELECT (jsonb_populate_record(NULL::creative_assets,to_jsonb(r)||jsonb_build_object('id','bench-asset-'||n,'content_id','bench-content-'||n,'content_revision_id','bench-rev-'||n))).* FROM creative_assets r CROSS JOIN generate_series(1,5000)n WHERE r.id=$1`, a.ID},
	} {
		if _, err = f.db.Exec(seed.sql, seed.id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = f.db.Exec(`ANALYZE creative_assets;ANALYZE creative_content_objects;ANALYZE creative_content_revisions;ANALYZE creative_blobs;ANALYZE creative_blob_palettes`); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		q    creativelibrary.Search
	}{{"baseline", creativelibrary.Search{}}, {"shape", creativelibrary.Search{Shape: "square"}}, {"color", creativelibrary.Search{Color: "#e08040"}}, {"combined", creativelibrary.Search{Color: "#e08040", Shape: "square"}}} {
		var samples []time.Duration
		for range 20 {
			q := tc.q
			q.Limit = 60
			q.IncludeFacets = true
			start := time.Now()
			p, e := creativelibrary.SearchAssets(t.Context(), f.a, q)
			if e != nil || p.TotalCount != 5001 {
				t.Fatalf("%s: %d %v", tc.name, p.TotalCount, e)
			}
			samples = append(samples, time.Since(start))
		}
		slices.Sort(samples)
		t.Logf("%s 5001 images + facets: p50=%s p95=%s", tc.name, samples[10], samples[18])
	}
}

func TestPaletteAlphaSurvivesBackfillAndRebuild(t *testing.T) {
	for _, tc := range []struct {
		name, mime string
		body       func(*testing.T) []byte
	}{
		{"transparent.png", "image/png", func(t *testing.T) []byte { return solidPNG(t, color.NRGBA{}) }},
		{"partial.png", "image/png", func(t *testing.T) []byte { return solidPNG(t, color.NRGBA{R: 224, G: 128, B: 64, A: 128}) }},
		{"source.webp", "image/webp", func(t *testing.T) []byte { return sample(t, "sample.webp") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setup(t)
			f.cfg.DisplayMaxEdge = 16
			f.svc = paletteService(t, f, f.local)
			u := f.uploadFile(t, f.a, tc.name, tc.mime, "image", tc.body(t), creativemedia.Target{Kind: "asset", Asset: &creativemedia.AssetTarget{Title: tc.name}})
			before, err := creativelibrary.GetAsset(t.Context(), f.a, u.Publication.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(before.Content.Media) != 2 {
				t.Fatal("fixture did not generate lossy display")
			}
			original := ""
			for _, m := range before.Content.Media {
				if m.Role == "original" {
					original = m.BlobID
				}
			}
			state := func() string {
				var s string
				if err := f.db.QueryRow(`SELECT state||'/'||failure_reason FROM creative_blob_palettes WHERE blob_id=$1`, original).Scan(&s); err != nil {
					t.Fatal(err)
				}
				return s
			}
			originalState := state()
			for _, mode := range []string{"backfill", "rebuild"} {
				if mode == "backfill" {
					if _, err = f.db.Exec(`DELETE FROM creative_blob_palettes WHERE account_id='media-a'`); err != nil {
						t.Fatal(err)
					}
					err = paletteWork(t, f.svc)(t.Context(), f.a, paletteJob())
				} else {
					err = creativemedia.RebuildPalettes(t.Context(), f.a, f.runtime, creativepalette.Algorithm)
				}
				if err != nil {
					t.Fatal(err)
				}
				if mode == "rebuild" {
					f.drain(t, f.a)
				}
				after, err := creativelibrary.GetAsset(t.Context(), f.a, before.ID)
				if err != nil || !reflect.DeepEqual(before.Palette, after.Palette) || state() != originalState {
					t.Fatalf("%s before=%+v after=%+v states %s/%s err=%v", mode, before.Palette, after.Palette, originalState, state(), err)
				}
				if tc.name == "transparent.png" {
					p, err := creativelibrary.SearchAssets(t.Context(), f.a, creativelibrary.Search{Limit: 10, Color: "#000000"})
					if err != nil || p.TotalCount != 0 {
						t.Fatal("transparent image matched black", err)
					}
				}
			}
		})
	}
}

func TestPaletteMissingObjectDoesNotBlockLaterBatches(t *testing.T) {
	f := setup(t)
	seedPaletteBlobs(t, f, 55)
	svc := paletteService(t, f, paletteAdapter{Adapter: f.local, open: func(ctx context.Context, key, version string, rng *versionedfs.ByteRange) (io.ReadCloser, versionedfs.ObjectStat, error) {
		if key == "palette-test/palette-000" {
			return nil, versionedfs.ObjectStat{}, fmt.Errorf("missing version: %w", versionedfs.ErrNotFound)
		}
		return f.local.OpenVersion(ctx, key, version, rng)
	}})
	if err := paletteWork(t, svc)(t.Context(), f.a, paletteJob()); err != nil {
		t.Fatal(err)
	}
	if paletteCount(t, f, "blob_id='palette-000' AND state='failed' AND failure_reason='object_missing'") != 1 {
		t.Fatal("missing object not recorded")
	}
	if paletteCount(t, f, "state='ready'") != 49 {
		t.Fatal("first batch did not advance")
	}
	f.drain(t, f.a)
	if paletteCount(t, f, "state='ready'") != 54 {
		t.Fatal("later batch starved")
	}
	if err := paletteWork(t, svc)(t.Context(), f.a, paletteJob()); err != nil {
		t.Fatal("repeat retried missing object", err)
	}
	var pins int
	if err := f.db.QueryRow(`SELECT count(*) FROM creative_blob_read_pins`).Scan(&pins); err != nil || pins != 0 {
		t.Fatal("read pins leaked", err)
	}
}
