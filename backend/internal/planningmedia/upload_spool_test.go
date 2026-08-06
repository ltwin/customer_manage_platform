package planningmedia

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" //nolint:depguard // fault-injection fixture needs a raw DDL connection
	"github.com/samson/customer-manage-platform/backend/internal/platform/idempotency"
	"github.com/samson/customer-manage-platform/backend/internal/platform/immutablefs"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func TestUploadSpoolBoundsSniffsAndCleansPrivateSource(t *testing.T) {
	body := pngFixture(t)
	spool, err := NewUploadSpool(context.Background(), bytes.NewReader(body), "image/png")
	if err != nil {
		t.Fatal(err)
	}
	path := spool.path
	if spool.size != int64(len(body)) || spool.mediaType != "image/png" || spool.checksum != digest(body) {
		t.Fatalf("spool metadata=%+v", spool)
	}
	read, err := spool.readAll(context.Background())
	if err != nil || !bytes.Equal(read, body) || spool.opens.Load() != 1 {
		t.Fatalf("read=%v opens=%d err=%v", read, spool.opens.Load(), err)
	}
	if err := spool.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("spool file remains: %v", err)
	}
	if _, err := NewUploadSpool(context.Background(), bytes.NewReader(body), "image/jpeg"); !errors.Is(err, ErrImageFormatInvalid) {
		t.Fatalf("declared mime mismatch err=%v", err)
	}
	if _, err := NewUploadSpool(context.Background(), bytes.NewReader(make([]byte, MaxUploadBytes+1)), "image/png"); !errors.Is(err, ErrImageSizeInvalid) {
		t.Fatalf("oversize spool err=%v", err)
	}
}

func TestPostgresUploadSingleCanonicalClaimReplayAndObjectFaultCleanup(t *testing.T) {
	ctx := context.Background()
	database := openPlanningMediaStore(t)
	scope := createPlanningMediaAccount(t, database, "planning-media-upload")
	if err := scope.Insert(ctx, "shoot_plans", []string{"id", "title", "subject"}, "upload-plan", "上传策划", "角色"); err != nil {
		t.Fatal(err)
	}
	authorizer := allowPlanningMediaHolder{}
	objects := newLocalStore(t)
	app := NewApplication(Repository{}, idempotency.NewExecutor(), objects, WithHolderAuthorizer(authorizer), WithClock(func() time.Time { return time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC) }))
	body := pngFixture(t)
	spool, err := NewUploadSpool(ctx, bytes.NewReader(body), "image/png")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = spool.Close() }()
	input := UploadInput{PlanID: "upload-plan", ExpectedPlanRevision: 1, DisplayName: "参考图", Content: spool, Rights: RightsDeclarationInput{SourceClass: SourceOfficial, RightsBasis: RightsCitationOrDisplay}, IntendedPurpose: PurposeMoodboardDisplay}
	first, err := app.Upload(ctx, scope, "upload-replay", input)
	if err != nil {
		t.Fatalf("first upload: %v", err)
	}
	replay, err := app.Upload(ctx, scope, "upload-replay", input)
	if err != nil || replay.Asset.ID != first.Asset.ID || spool.opens.Load() != 1 {
		t.Fatalf("replay=%+v err=%v spool opens=%d", replay, err, spool.opens.Load())
	}
	if first.Original.objectKey != "" || first.Display.objectKey != "" {
		t.Fatal("upload response exposed object key")
	}
	items, err := objects.Inventory(ctx)
	if err != nil || len(items) != 2 {
		t.Fatalf("physical inventory=%+v err=%v", items, err)
	}
	expected, err := (Repository{}).ExpectedInventory(ctx, scope)
	if err != nil || len(expected) != 2 {
		t.Fatalf("database inventory=%+v err=%v", expected, err)
	}
	orphanKey, err := planningObjectKey(scope.AccountID(), "orphan-s3", 1, RenditionDisplay)
	if err != nil {
		t.Fatal(err)
	}
	orphanBody := []byte("orphan-physical")
	old := time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)
	if _, _, err := objects.PutImmutable(ctx, orphanKey, orphanBody, immutablefs.Metadata{MediaType: "image/png", Size: int64(len(orphanBody)), Checksum: digest(orphanBody), Width: 1, Height: 1, ModifiedAt: old}); err != nil {
		t.Fatal(err)
	}
	firstReconcile, err := app.ReconcilePhysicalOrphans(ctx, scope, time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC))
	if err != nil || len(firstReconcile) != 1 || firstReconcile[0].Classification != InventoryOrphan || firstReconcile[0].Deleted {
		t.Fatalf("first orphan reconciliation=%+v err=%v", firstReconcile, err)
	}
	secondReconcile, err := app.ReconcilePhysicalOrphans(ctx, scope, time.Date(2026, 8, 7, 1, 0, 0, 0, time.UTC))
	if err != nil || len(secondReconcile) != 1 || !secondReconcile[0].Deleted {
		t.Fatalf("second orphan reconciliation=%+v err=%v", secondReconcile, err)
	}
	if _, err := objects.Stat(ctx, orphanKey); !errors.Is(err, immutablefs.ErrNotFound) {
		t.Fatalf("old orphan remains after exact cleanup: %v", err)
	}

	conflictSpool, err := NewUploadSpool(ctx, bytes.NewReader(append(body, 0)), "image/png")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conflictSpool.Close() }()
	conflictInput := input
	conflictInput.Content = conflictSpool
	if _, err := app.Upload(ctx, scope, "upload-replay", conflictInput); !errors.Is(err, idempotency.ErrConflict) {
		t.Fatalf("same key changed body err=%v", err)
	}

	faultObjects := &failPlanningMediaPut{Local: newLocalStore(t), failAt: 2}
	faultApp := NewApplication(Repository{}, idempotency.NewExecutor(), faultObjects, WithHolderAuthorizer(authorizer), WithClock(func() time.Time { return time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC) }))
	faultSpool, err := NewUploadSpool(ctx, bytes.NewReader(body), "image/png")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = faultSpool.Close() }()
	_, err = faultApp.Upload(ctx, scope, "upload-display-fault", UploadInput{PlanID: "upload-plan", ExpectedPlanRevision: 1, Content: faultSpool, Rights: input.Rights, IntendedPurpose: PurposeMoodboardDisplay})
	if !errors.Is(err, immutablefs.ErrTemporary) {
		t.Fatalf("display fault err=%v", err)
	}
	left, err := faultObjects.Inventory(ctx)
	if err != nil || len(left) != 0 {
		t.Fatalf("display fault left physical orphan=%+v err=%v", left, err)
	}
}

func TestPostgresUploadLedgerStoreFailureCleansPublishedObjects(t *testing.T) {
	ctx := context.Background()
	database, url := openPlanningMediaStoreWithURL(t)
	scope := createPlanningMediaAccount(t, database, "planning-media-ledger-fault")
	if err := scope.Insert(ctx, "shoot_plans", []string{"id", "title", "subject"}, "ledger-plan", "幂等失败", "角色"); err != nil {
		t.Fatal(err)
	}
	rawDB, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rawDB.Close() }()
	_, err = rawDB.ExecContext(ctx, `
CREATE OR REPLACE FUNCTION fail_planning_media_ledger_store() RETURNS trigger AS $$
BEGIN
    IF NEW.response_status IS NOT NULL THEN
        RAISE EXCEPTION 'injected planning media ledger store failure';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER planning_media_ledger_store_fault
BEFORE UPDATE ON idempotency_records
FOR EACH ROW EXECUTE FUNCTION fail_planning_media_ledger_store()`)
	if err != nil {
		t.Fatal(err)
	}
	objects := newLocalStore(t)
	app := NewApplication(Repository{}, idempotency.NewExecutor(), objects, WithHolderAuthorizer(allowPlanningMediaHolder{}), WithClock(func() time.Time { return time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC) }))
	body := pngFixture(t)
	spool, err := NewUploadSpool(ctx, bytes.NewReader(body), "image/png")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = spool.Close() }()
	_, err = app.Upload(ctx, scope, "ledger-fault-key", UploadInput{PlanID: "ledger-plan", ExpectedPlanRevision: 1, Content: spool, Rights: RightsDeclarationInput{SourceClass: SourceOfficial, RightsBasis: RightsCitationOrDisplay}, IntendedPurpose: PurposeMoodboardDisplay})
	if err == nil {
		t.Fatal("injected ledger store failure unexpectedly succeeded")
	}
	items, err := objects.Inventory(ctx)
	if err != nil || len(items) != 0 {
		t.Fatalf("ledger failure left published objects: %+v err=%v", items, err)
	}
	assetCount, err := scope.Count(ctx, "planning_media_assets", "upload_context_plan_id = $2", "ledger-plan")
	if err != nil || assetCount != 0 {
		t.Fatalf("ledger failure committed asset count=%d err=%v", assetCount, err)
	}
}

type allowPlanningMediaHolder struct{}

func (allowPlanningMediaHolder) AuthorizeMediaHolderInScope(_ context.Context, tx store.TxAccountScope, request HolderRequest) (HolderProof, error) {
	return NewHolderProof(tx.AccountID(), request.PlanID, request.HolderID, request.Kind, request.ExpectedPlanRevision, "draft"), nil
}

type failPlanningMediaPut struct {
	*immutablefs.Local
	failAt int
	puts   atomic.Int32
}

func (s *failPlanningMediaPut) PutImmutable(ctx context.Context, key string, body []byte, metadata immutablefs.Metadata) (immutablefs.Metadata, bool, error) {
	if int(s.puts.Add(1)) == s.failAt {
		return immutablefs.Metadata{}, false, immutablefs.ErrTemporary
	}
	return s.Local.PutImmutable(ctx, key, body, metadata)
}

var _ immutablefs.ObjectStore = (*failPlanningMediaPut)(nil)

func pngFixture(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, color.RGBA{uint8(x * 20), uint8(y * 20), 80, 255})
		}
	}
	var output bytes.Buffer
	if err := png.Encode(&output, img); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
