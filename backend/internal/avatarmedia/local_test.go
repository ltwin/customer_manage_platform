package avatarmedia

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func mustCustomerKey(t *testing.T, accountID, customerID, version, objectID string) Key {
	t.Helper()
	key, err := CustomerKey(accountID, customerID, ObjectRef{AvatarVersion: version, AvatarObjectID: objectID})
	if err != nil {
		t.Fatalf("CustomerKey: %v", err)
	}
	return key
}

func mustPrefix(t *testing.T, raw string) Prefix {
	t.Helper()
	prefix, err := ParsePrefix(raw)
	if err != nil {
		t.Fatalf("ParsePrefix: %v", err)
	}
	return prefix
}

func TestLocalStoreImmutableLifecycleAndStableList(t *testing.T) {
	ctx := context.Background()
	store, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	content, err := NewContent([]byte("normalized-local-avatar"), "image/png")
	if err != nil {
		t.Fatalf("content: %v", err)
	}
	meta := ObjectMeta{
		MediaType: "image/png", Size: content.Size(), Checksum: content.Checksum(),
		ModifiedAt: time.Unix(1_700_000_000, 0).UTC(),
	}
	keys := []Key{
		mustCustomerKey(t, "acc_test", "cus_test", content.Checksum(), "22222222222222222222222222222222"),
		mustCustomerKey(t, "acc_test", "cus_test", content.Checksum(), "11111111111111111111111111111111"),
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

	prefix := mustPrefix(t, "avatars/acc_test/customers/cus_test")
	page, err := store.List(ctx, prefix, Cursor{}, 1)
	if err != nil {
		t.Fatalf("List first page: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].Key.String() != keys[1].String() || page.Done || page.NextCursor.String() != keys[1].String() {
		t.Fatalf("first page mismatch: %+v", page)
	}
	page, err = store.List(ctx, prefix, page.NextCursor, 1)
	if err != nil {
		t.Fatalf("List second page: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].Key.String() != keys[0].String() || !page.Done {
		t.Fatalf("second page mismatch: %+v", page)
	}
	inventory, err := store.Inventory(ctx)
	if err != nil {
		t.Fatalf("Inventory: %v", err)
	}
	if len(inventory) != 2 || inventory[0].Key.String() != keys[1].String() || inventory[1].Key.String() != keys[0].String() {
		t.Fatalf("inventory mismatch: %+v", inventory)
	}

	if err := store.Delete(ctx, keys[0]); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.Stat(ctx, keys[0]); !errors.Is(err, ErrObjectNotFound) {
		t.Fatalf("Stat after Delete: want not found, got %v", err)
	}
	if err := store.Delete(ctx, keys[0]); err != nil {
		t.Fatalf("idempotent Delete: %v", err)
	}
}

func TestLocalInventoryRejectsIncompleteAndFreshTemp(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := NewLocal(root)
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	content, _ := NewContent([]byte("inventory"), "image/png")
	key := mustCustomerKey(t, "acc_test", "cus_test", content.Checksum(), "11111111111111111111111111111111")
	dir := filepath.Join(root, filepath.FromSlash(key.String()))
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("mkdir incomplete generation: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, contentFileName), content.Bytes(), 0o600); err != nil {
		t.Fatalf("write incomplete generation: %v", err)
	}
	if _, err := store.Inventory(ctx); !errors.Is(err, ErrObjectIntegrity) {
		t.Fatalf("incomplete inventory: want integrity error, got %v", err)
	}
	if err := os.RemoveAll(filepath.Join(root, "avatars")); err != nil {
		t.Fatalf("remove incomplete generation: %v", err)
	}
	// D11: NewLocal 之后再种新鲜 tmp，避免被构造清理。
	if err := os.Mkdir(filepath.Join(root, ".avatar-tmp-live"), 0o750); err != nil {
		t.Fatalf("mkdir fresh temp: %v", err)
	}
	if _, err := store.Inventory(ctx); !errors.Is(err, ErrObjectIntegrity) {
		t.Fatalf("fresh temp inventory: want integrity error, got %v", err)
	}
}

func TestLocalInventoryAcceptsBothSubjectKeys(t *testing.T) {
	ctx := context.Background()
	store, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	customerContent, _ := NewContent([]byte("customer-bytes"), "image/png")
	profileContent, _ := NewContent([]byte("profile-bytes"), "image/jpeg")
	customerKey := mustCustomerKey(t, "acc_test", "cus_test", customerContent.Checksum(), "11111111111111111111111111111111")
	profileKey, err := AccountProfileKey("acc_test", ObjectRef{
		AvatarVersion: profileContent.Checksum(), AvatarObjectID: "22222222222222222222222222222222",
	})
	if err != nil {
		t.Fatalf("AccountProfileKey: %v", err)
	}
	now := time.Unix(1_700_000_000, 0).UTC()
	if _, err := store.PutImmutable(ctx, customerKey, customerContent, ObjectMeta{
		MediaType: customerContent.MediaType(), Size: customerContent.Size(), Checksum: customerContent.Checksum(), ModifiedAt: now,
	}); err != nil {
		t.Fatalf("put customer: %v", err)
	}
	if _, err := store.PutImmutable(ctx, profileKey, profileContent, ObjectMeta{
		MediaType: profileContent.MediaType(), Size: profileContent.Size(), Checksum: profileContent.Checksum(), ModifiedAt: now,
	}); err != nil {
		t.Fatalf("put profile: %v", err)
	}
	inventory, err := store.Inventory(ctx)
	if err != nil {
		t.Fatalf("Inventory: %v", err)
	}
	if len(inventory) != 2 {
		t.Fatalf("want 2 inventory items, got %d", len(inventory))
	}
	parsed0, err := ParseKey(inventory[0].Key.String())
	if err != nil {
		t.Fatalf("ParseKey[0]: %v", err)
	}
	parsed1, err := ParseKey(inventory[1].Key.String())
	if err != nil {
		t.Fatalf("ParseKey[1]: %v", err)
	}
	kinds := map[string]bool{parsed0.SubjectKind: true, parsed1.SubjectKind: true}
	if !kinds[SubjectCustomer] || !kinds[SubjectAccountProfile] {
		t.Fatalf("inventory must classify both subjects: %+v %+v", parsed0, parsed1)
	}
}

func TestLocalStoreRejectsInvalidKeyAndIntegrityConflict(t *testing.T) {
	ctx := context.Background()
	store, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	content, _ := NewContent([]byte("one"), "image/png")
	meta := ObjectMeta{MediaType: "image/png", Size: content.Size(), Checksum: content.Checksum()}
	if _, err := ParseKey("../escape"); !errors.Is(err, ErrObjectKey) {
		t.Fatalf("traversal ParseKey: want invalid key, got %v", err)
	}
	key := mustCustomerKey(t, "acc_test", "cus_test", content.Checksum(), "11111111111111111111111111111111")
	if _, err := store.PutImmutable(ctx, key, content, meta); err != nil {
		t.Fatalf("first put: %v", err)
	}
	other, _ := NewContent([]byte("two"), "image/png")
	otherMeta := ObjectMeta{MediaType: "image/png", Size: other.Size(), Checksum: other.Checksum()}
	if _, err := store.PutImmutable(ctx, key, other, otherMeta); !errors.Is(err, ErrObjectIntegrity) {
		t.Fatalf("immutable conflict: want integrity error, got %v", err)
	}
}

func TestLocalStoreRejectsIntermediateSymlinksForOnlineOperations(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics require a unix-like filesystem")
	}
	ctx := context.Background()
	content, _ := NewContent([]byte("outside-sentinel"), "image/png")
	meta := ObjectMeta{MediaType: "image/png", Size: content.Size(), Checksum: content.Checksum()}
	key := mustCustomerKey(t, "acc_test", "cus_test", content.Checksum(), "11111111111111111111111111111111")
	layers := []string{
		"avatars",
		"avatars/acc_test",
		"avatars/acc_test/customers",
		"avatars/acc_test/customers/cus_test",
		"avatars/acc_test/customers/cus_test/" + content.Checksum(),
		key.String(),
	}
	operations := map[string]func(*Local) error{
		"put": func(store *Local) error {
			_, err := store.PutImmutable(ctx, key, content, meta)
			return err
		},
		"stat": func(store *Local) error {
			_, err := store.Stat(ctx, key)
			return err
		},
		"open": func(store *Local) error {
			stream, err := store.Open(ctx, key)
			if err != nil {
				return err
			}
			return stream.Close()
		},
		"delete": func(store *Local) error {
			return store.Delete(ctx, key)
		},
		"list": func(store *Local) error {
			_, err := store.List(ctx, mustPrefix(t, "avatars/acc_test"), Cursor{}, 10)
			return err
		},
	}
	for _, layer := range layers {
		for name, operation := range operations {
			t.Run(layer+"/"+name, func(t *testing.T) {
				root := t.TempDir()
				outside := t.TempDir()
				parts := strings.Split(layer, "/")
				parent := filepath.Join(root, filepath.FromSlash(strings.Join(parts[:len(parts)-1], "/")))
				if err := os.MkdirAll(parent, 0o750); err != nil {
					t.Fatalf("mkdir parent: %v", err)
				}
				if err := os.Symlink(outside, filepath.Join(root, filepath.FromSlash(layer))); err != nil {
					t.Fatalf("symlink: %v", err)
				}
				store, err := NewLocal(root)
				if err != nil {
					t.Fatalf("NewLocal: %v", err)
				}
				if err := operation(store); !errors.Is(err, ErrObjectKey) {
					t.Fatalf("%s via symlink layer %s: want key error, got %v", name, layer, err)
				}
			})
		}
	}
}
