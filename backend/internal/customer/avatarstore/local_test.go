package avatarstore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	customerdomain "github.com/samson/customer-manage-platform/backend/internal/customer"
)

func TestLocalStoreImmutableLifecycleAndStableList(t *testing.T) {
	ctx := context.Background()
	store, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	content, err := customerdomain.NewAvatarContent([]byte("normalized-local-avatar"), "image/png")
	if err != nil {
		t.Fatalf("content: %v", err)
	}
	meta := customerdomain.ObjectMeta{
		MediaType: "image/png", Size: content.Size(), Checksum: content.Checksum(),
		ModifiedAt: time.Unix(1_700_000_000, 0).UTC(),
	}
	keys := []string{
		"avatars/acc_test/customers/cus_test/" + content.Checksum() + "/22222222222222222222222222222222",
		"avatars/acc_test/customers/cus_test/" + content.Checksum() + "/11111111111111111111111111111111",
	}
	for _, key := range keys {
		result, err := store.PutImmutable(ctx, key, content, meta)
		if err != nil || !result.Created {
			t.Fatalf("first PutImmutable %s: result=%+v err=%v", key, result, err)
		}
		replayed, err := store.PutImmutable(ctx, key, content, meta)
		if err != nil || replayed.Created {
			t.Fatalf("replayed PutImmutable %s: result=%+v err=%v", key, replayed, err)
		}
	}

	page, err := store.List(ctx, "avatars/acc_test/customers/cus_test", "", 1)
	if err != nil {
		t.Fatalf("List first page: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].Key != keys[1] || page.Done || page.NextCursor != keys[1] {
		t.Fatalf("first page mismatch: %+v", page)
	}
	page, err = store.List(ctx, "avatars/acc_test/customers/cus_test", page.NextCursor, 1)
	if err != nil {
		t.Fatalf("List second page: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].Key != keys[0] || !page.Done {
		t.Fatalf("second page mismatch: %+v", page)
	}
	inventory, err := store.Inventory(ctx)
	if err != nil {
		t.Fatalf("Inventory: %v", err)
	}
	if len(inventory) != 2 || inventory[0].Key != keys[1] || inventory[1].Key != keys[0] {
		t.Fatalf("inventory mismatch: %+v", inventory)
	}

	if err := store.Delete(ctx, keys[0]); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.Stat(ctx, keys[0]); !errors.Is(err, customerdomain.ErrAvatarObjectNotFound) {
		t.Fatalf("Stat after Delete: want not found, got %v", err)
	}
	if err := store.Delete(ctx, keys[0]); err != nil {
		t.Fatalf("idempotent Delete: %v", err)
	}
}

func TestLocalInventoryRejectsIncompleteAndStalePhysicalState(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := NewLocal(root)
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	content, _ := customerdomain.NewAvatarContent([]byte("inventory"), "image/png")
	key := "avatars/acc_test/customers/cus_test/" + content.Checksum() + "/11111111111111111111111111111111"
	dir := filepath.Join(root, filepath.FromSlash(key))
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("mkdir incomplete generation: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, contentFileName), content.Bytes(), 0o600); err != nil {
		t.Fatalf("write incomplete generation: %v", err)
	}
	if _, err := store.Inventory(ctx); !errors.Is(err, customerdomain.ErrAvatarObjectIntegrity) {
		t.Fatalf("incomplete inventory: want integrity error, got %v", err)
	}
	if err := os.RemoveAll(filepath.Join(root, "avatars")); err != nil {
		t.Fatalf("remove incomplete generation: %v", err)
	}
	if err := os.Mkdir(filepath.Join(root, ".avatar-tmp-live"), 0o750); err != nil {
		t.Fatalf("mkdir stale temp: %v", err)
	}
	if _, err := store.Inventory(ctx); !errors.Is(err, customerdomain.ErrAvatarObjectIntegrity) {
		t.Fatalf("stale temp inventory: want integrity error, got %v", err)
	}
}

func TestLocalStoreRejectsTraversalAndIntegrityConflict(t *testing.T) {
	ctx := context.Background()
	store, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	content, _ := customerdomain.NewAvatarContent([]byte("one"), "image/png")
	meta := customerdomain.ObjectMeta{MediaType: "image/png", Size: content.Size(), Checksum: content.Checksum()}
	if _, err := store.PutImmutable(ctx, "../escape", content, meta); !errors.Is(err, customerdomain.ErrAvatarObjectKey) {
		t.Fatalf("traversal: want invalid key, got %v", err)
	}
	key := "avatars/acc_test/customers/cus_test/" + content.Checksum() + "/11111111111111111111111111111111"
	if _, err := store.PutImmutable(ctx, key, content, meta); err != nil {
		t.Fatalf("first put: %v", err)
	}
	other, _ := customerdomain.NewAvatarContent([]byte("two"), "image/png")
	otherMeta := customerdomain.ObjectMeta{MediaType: "image/png", Size: other.Size(), Checksum: other.Checksum()}
	if _, err := store.PutImmutable(ctx, key, other, otherMeta); !errors.Is(err, customerdomain.ErrAvatarObjectIntegrity) {
		t.Fatalf("immutable conflict: want integrity error, got %v", err)
	}
}

func TestLocalStoreRejectsIntermediateSymlinksForOnlineOperations(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics require a unix-like filesystem")
	}
	ctx := context.Background()
	content, _ := customerdomain.NewAvatarContent([]byte("outside-sentinel"), "image/png")
	meta := customerdomain.ObjectMeta{MediaType: "image/png", Size: content.Size(), Checksum: content.Checksum()}
	key := "avatars/acc_test/customers/cus_test/" + content.Checksum() + "/11111111111111111111111111111111"
	layers := []string{
		"avatars",
		"avatars/acc_test",
		"avatars/acc_test/customers",
		"avatars/acc_test/customers/cus_test",
		"avatars/acc_test/customers/cus_test/" + content.Checksum(),
		key,
	}
	operations := map[string]func(*Local) error{
		"put": func(store *Local) error {
			_, err := store.PutImmutable(ctx, key, content, meta)
			return err
		},
		"open": func(store *Local) error {
			reader, err := store.Open(ctx, key)
			if reader != nil {
				_ = reader.Close()
			}
			return err
		},
		"stat": func(store *Local) error {
			_, err := store.Stat(ctx, key)
			return err
		},
		"list": func(store *Local) error {
			_, err := store.List(ctx, "avatars/acc_test/customers/cus_test", "", 10)
			return err
		},
		"delete": func(store *Local) error {
			return store.Delete(ctx, key)
		},
	}

	for _, layer := range layers {
		for operation, run := range operations {
			t.Run(filepath.ToSlash(layer)+"/"+operation, func(t *testing.T) {
				root := t.TempDir()
				outside := t.TempDir()
				store, err := NewLocal(root)
				if err != nil {
					t.Fatalf("NewLocal root: %v", err)
				}
				outsideStore, err := NewLocal(outside)
				if err != nil {
					t.Fatalf("NewLocal outside: %v", err)
				}
				if _, err := outsideStore.PutImmutable(ctx, key, content, meta); err != nil {
					t.Fatalf("seed outside generation: %v", err)
				}
				linkPath := filepath.Join(root, filepath.FromSlash(layer))
				if err := os.MkdirAll(filepath.Dir(linkPath), 0o750); err != nil {
					t.Fatalf("mkdir link parent: %v", err)
				}
				if err := os.Symlink(filepath.Join(outside, filepath.FromSlash(layer)), linkPath); err != nil {
					t.Fatalf("symlink controlled layer: %v", err)
				}

				if err := run(store); !errors.Is(err, customerdomain.ErrAvatarObjectKey) {
					t.Fatalf("%s through %s: want object key error, got %v", operation, layer, err)
				}
				if _, err := outsideStore.Stat(ctx, key); err != nil {
					t.Fatalf("outside sentinel changed: %v", err)
				}
			})
		}
	}
}

func TestLocalStoreTemporaryErrorsDoNotExposeRootPath(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("unix permission semantics required")
	}
	ctx := context.Background()
	root := t.TempDir()
	store, err := NewLocal(root)
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	content, _ := customerdomain.NewAvatarContent([]byte("safe-error"), "image/png")
	key := "avatars/acc_test/customers/cus_test/" + content.Checksum() + "/11111111111111111111111111111111"
	meta := customerdomain.ObjectMeta{MediaType: "image/png", Size: content.Size(), Checksum: content.Checksum()}
	if _, err := store.PutImmutable(ctx, key, content, meta); err != nil {
		t.Fatalf("seed generation: %v", err)
	}
	metadataPath := filepath.Join(root, filepath.FromSlash(key), metadataFileName)
	if err := os.Chmod(metadataPath, 0); err != nil {
		t.Fatalf("chmod metadata: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(metadataPath, 0o600) })

	_, err = store.Stat(ctx, key)
	if !errors.Is(err, customerdomain.ErrAvatarObjectTemporary) {
		t.Fatalf("Stat: want temporary error, got %v", err)
	}
	if strings.Contains(err.Error(), root) {
		t.Fatalf("temporary error exposed avatar root %q: %v", root, err)
	}
}

func TestLocalStoreRejectsGenerationLeafSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics require a unix-like filesystem")
	}
	ctx := context.Background()
	content, _ := customerdomain.NewAvatarContent([]byte("outside-generation"), "image/png")
	meta := customerdomain.ObjectMeta{MediaType: "image/png", Size: content.Size(), Checksum: content.Checksum()}
	key := "avatars/acc_test/customers/cus_test/" + content.Checksum() + "/11111111111111111111111111111111"
	leaves := []string{contentFileName, metadataFileName}
	operations := map[string]func(*Local) error{
		"put": func(store *Local) error {
			_, err := store.PutImmutable(ctx, key, content, meta)
			return err
		},
		"open": func(store *Local) error {
			reader, err := store.Open(ctx, key)
			if reader != nil {
				_ = reader.Close()
			}
			return err
		},
		"stat": func(store *Local) error {
			_, err := store.Stat(ctx, key)
			return err
		},
		"delete": func(store *Local) error {
			return store.Delete(ctx, key)
		},
		"inventory": func(store *Local) error {
			_, err := store.Inventory(ctx)
			return err
		},
	}

	for _, leaf := range leaves {
		for operation, run := range operations {
			t.Run(leaf+"/"+operation, func(t *testing.T) {
				root := t.TempDir()
				outside := t.TempDir()
				store, err := NewLocal(root)
				if err != nil {
					t.Fatalf("NewLocal root: %v", err)
				}
				outsideStore, err := NewLocal(outside)
				if err != nil {
					t.Fatalf("NewLocal outside: %v", err)
				}
				if _, err := store.PutImmutable(ctx, key, content, meta); err != nil {
					t.Fatalf("seed root generation: %v", err)
				}
				if _, err := outsideStore.PutImmutable(ctx, key, content, meta); err != nil {
					t.Fatalf("seed outside generation: %v", err)
				}
				rootLeaf := filepath.Join(root, filepath.FromSlash(key), leaf)
				outsideLeaf := filepath.Join(outside, filepath.FromSlash(key), leaf)
				outsideBefore, err := os.ReadFile(outsideLeaf)
				if err != nil {
					t.Fatalf("read outside sentinel: %v", err)
				}
				if err := os.Remove(rootLeaf); err != nil {
					t.Fatalf("remove root leaf: %v", err)
				}
				if err := os.Symlink(outsideLeaf, rootLeaf); err != nil {
					t.Fatalf("symlink generation leaf: %v", err)
				}

				if err := run(store); !errors.Is(err, customerdomain.ErrAvatarObjectKey) {
					t.Fatalf("%s through %s: want object key error, got %v", operation, leaf, err)
				}
				outsideAfter, err := os.ReadFile(outsideLeaf)
				if err != nil {
					t.Fatalf("outside sentinel removed: %v", err)
				}
				if string(outsideAfter) != string(outsideBefore) {
					t.Fatal("outside sentinel content changed")
				}
				if _, err := outsideStore.Stat(ctx, key); err != nil {
					t.Fatalf("outside generation changed: %v", err)
				}
			})
		}
	}
}
