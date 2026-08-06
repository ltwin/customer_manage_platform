package planningmedia

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/immutablefs"
)

func TestRestoreEmptyTargetIsExactAndAllOrNothing(t *testing.T) {
	ctx := context.Background()
	source := newLocalStore(t)
	target := newLocalStore(t)
	now := time.Date(2026, 8, 5, 1, 2, 3, 0, time.UTC)
	putFixture(t, source, "acc_restore", "asset-a", 1, RenditionOriginal, []byte("original"), now)
	putFixture(t, source, "acc_restore", "asset-a", 1, RenditionDisplay, []byte("display"), now)

	manifest, err := BuildManifest(ctx, source)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Entries) != 2 || manifest.Entries[0].AssetID != "asset-a" || manifest.Entries[0].Generation != 1 || manifest.Digest == "" {
		t.Fatalf("manifest is not exact: %+v", manifest)
	}
	if err := VerifyAndRestoreFixture(ctx, manifest, source, target); err != nil {
		t.Fatal(err)
	}
	restored, err := BuildManifest(ctx, target)
	if err != nil || restored.Digest != manifest.Digest {
		t.Fatalf("restored=%+v err=%v", restored, err)
	}

	nonEmpty := newLocalStore(t)
	putFixture(t, nonEmpty, "acc_restore", "other", 1, RenditionDisplay, []byte("existing"), now)
	if err := VerifyAndRestoreFixture(ctx, manifest, source, nonEmpty); err == nil || err.Error() != "restore_target_not_empty" {
		t.Fatalf("non-empty target accepted: %v", err)
	}
}

func TestRestoreRejectsMissingOrphanCorruptAndCleansPartialTarget(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 5, 1, 2, 3, 0, time.UTC)
	source := newLocalStore(t)
	target := newLocalStore(t)
	putFixture(t, source, "acc_restore", "asset-a", 1, RenditionOriginal, []byte("original"), now)
	putFixture(t, source, "acc_restore", "asset-a", 1, RenditionDisplay, []byte("display"), now)
	manifest, err := BuildManifest(ctx, source)
	if err != nil {
		t.Fatal(err)
	}

	orphanKey, _ := planningObjectKey("acc_restore", "orphan", 1, RenditionDisplay)
	orphanBody := []byte("orphan")
	if _, _, err := source.PutImmutable(ctx, orphanKey, orphanBody, fixtureMetadata(orphanBody, now)); err != nil {
		t.Fatal(err)
	}
	if err := VerifyAndRestoreFixture(ctx, manifest, source, target); err == nil || err.Error() != "restore_source_inventory_mismatch" {
		t.Fatalf("orphan source accepted: %v", err)
	}
	items, err := target.Inventory(ctx)
	if err != nil || len(items) != 0 {
		t.Fatalf("failed restore left target objects: %+v err=%v", items, err)
	}

	expected := manifest.Entries
	actual := []immutablefs.Item{
		{Key: expected[0].Key, Meta: expected[0].Metadata},
		{Key: expected[1].Key, Meta: immutablefs.Metadata{MediaType: "image/png", Size: 999, Checksum: "sha256-corrupt", Width: 1, Height: 1}},
		{Key: orphanKey, Meta: fixtureMetadata(orphanBody, now)},
	}
	findings := ClassifyInventory(expected, actual)
	if len(findings) != 2 || findings[0].Classification != InventoryCorrupt || findings[1].Classification != InventoryOrphan {
		t.Fatalf("unexpected corrupt/orphan classification: %+v", findings)
	}
	missing := ClassifyInventory(expected, actual[:1])
	if len(missing) != 1 || missing[0].Classification != InventoryMissing {
		t.Fatalf("unexpected missing classification: %+v", missing)
	}

	tampered := manifest
	tampered.Entries = append([]ManifestEntry(nil), manifest.Entries...)
	tampered.Entries[0].Metadata.Checksum = "sha256-tampered"
	if err := VerifyAndRestoreFixture(ctx, tampered, source, target); err == nil || err.Error() != "manifest_digest_invalid" {
		t.Fatalf("tampered manifest accepted: %v", err)
	}

	cleanSource := newLocalStore(t)
	putFixture(t, cleanSource, "acc_restore", "asset-a", 1, RenditionOriginal, []byte("original"), now)
	putFixture(t, cleanSource, "acc_restore", "asset-a", 1, RenditionDisplay, []byte("display"), now)
	cleanManifest, err := BuildManifest(ctx, cleanSource)
	if err != nil {
		t.Fatal(err)
	}
	failingLocal := newLocalStore(t)
	failingTarget := &failOnSecondPut{Local: failingLocal}
	if err := VerifyAndRestoreFixture(ctx, cleanManifest, cleanSource, failingTarget); !errors.Is(err, immutablefs.ErrTemporary) {
		t.Fatalf("injected restore failure err=%v", err)
	}
	items, err = failingLocal.Inventory(ctx)
	if err != nil || len(items) != 0 {
		t.Fatalf("partial restore was not cleaned: %+v err=%v", items, err)
	}
}

type failOnSecondPut struct {
	*immutablefs.Local
	puts int
}

func (s *failOnSecondPut) PutImmutable(ctx context.Context, key string, body []byte, metadata immutablefs.Metadata) (immutablefs.Metadata, bool, error) {
	s.puts++
	if s.puts == 2 {
		return immutablefs.Metadata{}, false, immutablefs.ErrTemporary
	}
	return s.Local.PutImmutable(ctx, key, body, metadata)
}

var _ immutablefs.ObjectStore = (*failOnSecondPut)(nil)

func newLocalStore(t *testing.T) *immutablefs.Local {
	t.Helper()
	local, err := immutablefs.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return local
}

func putFixture(t *testing.T, local *immutablefs.Local, accountID, assetID string, generation int, kind RenditionKind, body []byte, now time.Time) {
	t.Helper()
	key, err := planningObjectKey(accountID, assetID, generation, kind)
	if err != nil {
		t.Fatal(err)
	}
	if _, created, err := local.PutImmutable(context.Background(), key, body, fixtureMetadata(body, now)); err != nil || !created {
		t.Fatalf("put fixture created=%v err=%v", created, err)
	}
}

func fixtureMetadata(body []byte, now time.Time) immutablefs.Metadata {
	return immutablefs.Metadata{MediaType: "image/png", Size: int64(len(body)), Checksum: digest(body), Width: 1, Height: 1, ModifiedAt: now}
}

func TestOpenVerifiedRejectsMetadataMismatch(t *testing.T) {
	local := newLocalStore(t)
	now := time.Now().UTC()
	putFixture(t, local, "acc_restore", "asset-a", 1, RenditionDisplay, []byte("display"), now)
	key, _ := planningObjectKey("acc_restore", "asset-a", 1, RenditionDisplay)
	_, err := openVerified(context.Background(), local, key, immutablefs.Metadata{MediaType: "image/png", Size: 1, Checksum: "sha256-wrong"})
	if !errors.Is(err, immutablefs.ErrIntegrity) {
		t.Fatalf("metadata mismatch err=%v", err)
	}
}
