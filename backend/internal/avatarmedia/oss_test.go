package avatarmedia

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/immutablefs"
)

// fakeBaseStore 是内存版 immutablefs.ObjectStore，语义对齐
// immutablefs.Local：PutImmutable 幂等收敛、keyset 游标、size/checksum 校验。
type fakeBaseStore struct {
	mu      sync.Mutex
	objects map[string]immutablefs.Metadata
	bodies  map[string][]byte
}

func newFakeBaseStore() *fakeBaseStore {
	return &fakeBaseStore{
		objects: make(map[string]immutablefs.Metadata),
		bodies:  make(map[string][]byte),
	}
}

func fakeChecksum(body []byte) string {
	sum := sha256.Sum256(body)
	return fmt.Sprintf("sha256-%x", sum)
}

func (f *fakeBaseStore) PutImmutable(ctx context.Context, key string, body []byte, expected immutablefs.Metadata) (immutablefs.Metadata, bool, error) {
	if err := ctx.Err(); err != nil {
		return immutablefs.Metadata{}, false, err
	}
	if expected.Size != int64(len(body)) || expected.Size <= 0 || expected.Checksum != fakeChecksum(body) {
		return immutablefs.Metadata{}, false, immutablefs.ErrIntegrity
	}
	if meta, ok := f.objects[key]; ok {
		if meta == expected {
			return meta, false, nil
		}
		return immutablefs.Metadata{}, false, immutablefs.ErrIntegrity
	}
	f.objects[key] = expected
	f.bodies[key] = append([]byte(nil), body...)
	return expected, true, nil
}

func (f *fakeBaseStore) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	body, ok := f.bodies[key]
	if !ok {
		return nil, immutablefs.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(body)), nil
}

func (f *fakeBaseStore) Stat(ctx context.Context, key string) (immutablefs.Metadata, error) {
	if err := ctx.Err(); err != nil {
		return immutablefs.Metadata{}, err
	}
	meta, ok := f.objects[key]
	if !ok {
		return immutablefs.Metadata{}, immutablefs.ErrNotFound
	}
	return meta, nil
}

func (f *fakeBaseStore) List(ctx context.Context, prefix, cursor string, limit int) (immutablefs.Page, error) {
	if limit < 1 || limit > 1000 {
		return immutablefs.Page{}, immutablefs.ErrInvalidKey
	}
	if err := ctx.Err(); err != nil {
		return immutablefs.Page{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	keys := make([]string, 0, len(f.objects))
	for key := range f.objects {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix && key > cursor {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	page := immutablefs.Page{Items: make([]immutablefs.Item, 0, limit)}
	for _, key := range keys {
		page.Items = append(page.Items, immutablefs.Item{Key: key, Meta: f.objects[key]})
		if len(page.Items) == limit {
			break
		}
	}
	if len(page.Items) == 0 {
		page.Done = true
		return page, nil
	}
	page.NextCursor = page.Items[len(page.Items)-1].Key
	page.Done = page.NextCursor == keys[len(keys)-1]
	return page, nil
}

func (f *fakeBaseStore) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	delete(f.objects, key)
	delete(f.bodies, key)
	return nil
}

func TestOSSStoreConformanceAgainstFakeBase(t *testing.T) {
	RunConformance(t, func(t *testing.T) (ObjectStore, string) {
		store, err := NewOSSStore(newFakeBaseStore())
		if err != nil {
			t.Fatalf("NewOSSStore: %v", err)
		}
		return store, "ossconf" + fmt.Sprintf("%d", time.Now().UnixNano()%1000)
	})
}

func TestOSSStoreMapsBaseSentinels(t *testing.T) {
	store, err := NewOSSStore(newFakeBaseStore())
	if err != nil {
		t.Fatalf("NewOSSStore: %v", err)
	}
	key, err := CustomerKey("acct1", "cust1", ObjectRef{
		AvatarVersion:  versionFor(1),
		AvatarObjectID: objectIDFor(1),
	})
	if err != nil {
		t.Fatalf("CustomerKey: %v", err)
	}
	if _, err := store.Stat(context.Background(), key); !errors.Is(err, ErrObjectNotFound) {
		t.Fatalf("缺失键应映射 ErrObjectNotFound, got %v", err)
	}
	invalid := Key{value: "not-an-avatar-key"}
	if _, err := store.Stat(context.Background(), invalid); !errors.Is(err, ErrObjectKey) {
		t.Fatalf("非法键应 ErrObjectKey, got %v", err)
	}
	if _, err := store.Open(context.Background(), invalid); !errors.Is(err, ErrObjectKey) {
		t.Fatalf("Open 非法键应 ErrObjectKey, got %v", err)
	}
	if err := store.Delete(context.Background(), invalid); !errors.Is(err, ErrObjectKey) {
		t.Fatalf("Delete 非法键应 ErrObjectKey, got %v", err)
	}
	content, err := NewContent([]byte("mapping-body"), "image/png")
	if err != nil {
		t.Fatalf("NewContent: %v", err)
	}
	expected := ObjectMeta{MediaType: content.MediaType(), Size: content.Size(), Checksum: content.Checksum()}
	if _, err := store.PutImmutable(context.Background(), key, content, expected); err != nil {
		t.Fatalf("PutImmutable: %v", err)
	}
	mutated := expected
	mutated.Checksum = "sha256-" + fmt.Sprintf("%064x", 42)
	if _, err := store.PutImmutable(context.Background(), key, content, mutated); !errors.Is(err, ErrObjectIntegrity) {
		t.Fatalf("meta 错配应映射 ErrObjectIntegrity, got %v", err)
	}
}

func TestOSSStoreInventoryScopedToAvatarNamespace(t *testing.T) {
	base := newFakeBaseStore()
	// 底座中预置一个非头像命名空间对象（模拟共享 bucket 的 planning/ 前缀）。
	planningBody := []byte("planning-asset")
	planningMeta := immutablefs.Metadata{
		MediaType: "image/jpeg",
		Size:      int64(len(planningBody)),
		Checksum:  fakeChecksum(planningBody),
	}
	if _, _, err := base.PutImmutable(context.Background(), "planning/acct1/assets/x/g1/original", planningBody, planningMeta); err != nil {
		t.Fatalf("seed planning object: %v", err)
	}
	store, err := NewOSSStore(base)
	if err != nil {
		t.Fatalf("NewOSSStore: %v", err)
	}
	key, err := CustomerKey("acct2", "cust1", ObjectRef{AvatarVersion: versionFor(2), AvatarObjectID: objectIDFor(2)})
	if err != nil {
		t.Fatalf("CustomerKey: %v", err)
	}
	content, err := NewContent([]byte("avatar-inventory"), "image/png")
	if err != nil {
		t.Fatalf("NewContent: %v", err)
	}
	if _, err := store.PutImmutable(context.Background(), key, content, ObjectMeta{
		MediaType: content.MediaType(), Size: content.Size(), Checksum: content.Checksum(),
	}); err != nil {
		t.Fatalf("PutImmutable: %v", err)
	}
	items, err := store.Inventory(context.Background())
	if err != nil {
		t.Fatalf("Inventory: %v", err)
	}
	if len(items) != 1 || items[0].Key.String() != key.String() {
		t.Fatalf("Inventory 应只含 avatars/ 命名空间对象, got %+v", items)
	}
}

func TestOSSStoreTypedPrefixesDoNotCrossSegmentBoundaries(t *testing.T) {
	base := newFakeBaseStore()
	store, err := NewOSSStore(base)
	if err != nil {
		t.Fatalf("NewOSSStore: %v", err)
	}
	content, err := NewContent([]byte("prefix-boundary"), "image/png")
	if err != nil {
		t.Fatalf("NewContent: %v", err)
	}
	meta := ObjectMeta{MediaType: content.MediaType(), Size: content.Size(), Checksum: content.Checksum()}
	refs := []struct {
		account  string
		customer string
		index    int
	}{
		{account: "acc", customer: "cust", index: 1},
		{account: "acc", customer: "cust2", index: 2},
		{account: "acc2", customer: "cust", index: 3},
	}
	for _, fixture := range refs {
		key, keyErr := CustomerKey(fixture.account, fixture.customer, ObjectRef{
			AvatarVersion: versionFor(fixture.index), AvatarObjectID: objectIDFor(fixture.index),
		})
		if keyErr != nil {
			t.Fatalf("CustomerKey: %v", keyErr)
		}
		if _, putErr := store.PutImmutable(context.Background(), key, content, meta); putErr != nil {
			t.Fatalf("PutImmutable: %v", putErr)
		}
	}
	customerPrefix, _ := CustomerPrefix("acc", "cust")
	customerPage, err := store.List(context.Background(), customerPrefix, Cursor{}, 100)
	if err != nil || len(customerPage.Items) != 1 {
		t.Fatalf("customer prefix crossed segment boundary: n=%d err=%v", len(customerPage.Items), err)
	}
	accountPrefix, _ := AccountPrefix("acc")
	accountPage, err := store.List(context.Background(), accountPrefix, Cursor{}, 100)
	if err != nil || len(accountPage.Items) != 2 {
		t.Fatalf("account prefix crossed segment boundary: n=%d err=%v", len(accountPage.Items), err)
	}
}

func TestNewOSSStoreRejectsNilBase(t *testing.T) {
	if _, err := NewOSSStore(nil); err == nil {
		t.Fatal("nil base must be rejected")
	}
}
