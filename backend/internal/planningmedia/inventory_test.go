package planningmedia

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

	orphanBody := []byte("orphan")
	orphanKey, _ := planningObjectKey("acc_restore", "orphan", 1, RenditionDisplay, digest(orphanBody))
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
	key, err := planningObjectKey(accountID, assetID, generation, kind, digest(body))
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
	key, _ := planningObjectKey("acc_restore", "asset-a", 1, RenditionDisplay, digest([]byte("display")))
	_, err := openVerified(context.Background(), local, key, immutablefs.Metadata{MediaType: "image/png", Size: 1, Checksum: "sha256-wrong"})
	if !errors.Is(err, immutablefs.ErrIntegrity) {
		t.Fatalf("metadata mismatch err=%v", err)
	}
}

func TestBuildManifestIgnoresOtherSharedBucketNamespaces(t *testing.T) {
	objects := newLocalStore(t)
	body := []byte("planning")
	putFixture(t, objects, "acc_manifest", "asset-a", 1, RenditionDisplay, body, time.Now())
	avatarBody := []byte("avatar")
	avatarKey := "avatars/acc_manifest/customers/customer-a/sha256-" + strings.Repeat("0", 64) + "/" + strings.Repeat("0", 32)
	if _, _, err := objects.PutImmutable(context.Background(), avatarKey, avatarBody, fixtureMetadata(avatarBody, time.Now())); err != nil {
		t.Fatalf("seed avatar namespace: %v", err)
	}

	manifest, err := BuildManifest(context.Background(), objects)
	if err != nil {
		t.Fatalf("BuildManifest 不应读取 avatars/ 命名空间: %v", err)
	}
	if len(manifest.Entries) != 1 || !strings.HasPrefix(manifest.Entries[0].Key, "planning/") {
		t.Fatalf("planning manifest 命名空间不符: %+v", manifest.Entries)
	}
}

func TestCreativeAssetsParticipateInExactBackupRestore(t *testing.T) {
	ctx := context.Background()
	source, err := immutablefs.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target, err := immutablefs.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("creative image")
	sum := sha256.Sum256(body)
	checksum := "sha256-" + hex.EncodeToString(sum[:])
	key := "creative/account-1/assets/cas-1/" + checksum + "/display"
	_, _, err = source.PutImmutable(ctx, key, body, immutablefs.Metadata{MediaType: "image/png", Size: int64(len(body)), Checksum: checksum})
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := BuildManifest(ctx, source)
	if err != nil || len(manifest.Entries) != 1 {
		t.Fatalf("creative missing from backup: %v %+v", err, manifest)
	}
	if err := VerifyAndRestoreFixture(ctx, manifest, source, target); err != nil {
		t.Fatal(err)
	}
	if _, err := target.Stat(ctx, key); err != nil {
		t.Fatal(err)
	}
}

func TestGoManifestVerifiesWithPythonBackupReader(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is required for cross-language backup verification")
	}
	objects := newLocalStore(t)
	putFixture(t, objects, "account-1", "asset-1", 1, RenditionDisplay, []byte("legacy"), time.Now().UTC())
	body := []byte("creative")
	sum := sha256.Sum256(body)
	checksum := "sha256-" + hex.EncodeToString(sum[:])
	key := "creative/account-1/assets/cas-1/" + checksum + "/display"
	_, _, err = objects.PutImmutable(context.Background(), key, body, fixtureMetadata(body, time.Now().UTC()))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := BuildManifest(context.Background(), objects)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	reader, err := filepath.Abs("../../../scripts/lib/planningbackup")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), python, "-c", "import sys; from pathlib import Path; sys.path.insert(0,sys.argv[2]); from package import validate_planning_manifest; validate_planning_manifest(Path(sys.argv[1]))", file, reader)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Go manifest rejected by Python: %v: %s", err, output)
	}
}
