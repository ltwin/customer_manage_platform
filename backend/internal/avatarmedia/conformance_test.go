package avatarmedia

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"testing"
	"time"
)

// RunConformance 驱动 typed ObjectStore 实现必须满足的头像对象生命周期契约；
// Local 与 OSS 适配实现共用。newStore 每次调用返回隔离存储与本轮隔离 accountID
// （共享 bucket 实现返回唯一 run id，所有键都落在 avatars/{accountID}/ 命名空间下）。
func RunConformance(t *testing.T, newStore func(t *testing.T) (ObjectStore, string)) {
	t.Helper()

	confCustomerKey := func(t *testing.T, accountID string, seq int) Key {
		t.Helper()
		key, err := CustomerKey(accountID, fmt.Sprintf("cust%03d", seq), ObjectRef{
			AvatarVersion:  versionFor(seq),
			AvatarObjectID: objectIDFor(seq),
		})
		if err != nil {
			t.Fatalf("CustomerKey: %v", err)
		}
		return key
	}
	confAccountKey := func(t *testing.T, accountID string) Key {
		t.Helper()
		key, err := AccountProfileKey(accountID, ObjectRef{
			AvatarVersion:  versionFor(999),
			AvatarObjectID: objectIDFor(999),
		})
		if err != nil {
			t.Fatalf("AccountProfileKey: %v", err)
		}
		return key
	}
	confContent := func(seed string) Content {
		body := []byte("avatarmedia-conformance:" + seed)
		content, err := NewContent(body, "image/png")
		if err != nil {
			t.Fatalf("NewContent: %v", err)
		}
		return content
	}
	confMeta := func(content Content) ObjectMeta {
		return ObjectMeta{
			MediaType:  content.MediaType(),
			Size:       content.Size(),
			Checksum:   content.Checksum(),
			ModifiedAt: mustAvatarTime("2026-08-25T10:00:00Z"),
		}
	}
	mustAvatarPut := func(t *testing.T, store ObjectStore, key Key, content Content) ObjectMeta {
		t.Helper()
		expected := confMeta(content)
		result, err := store.PutImmutable(context.Background(), key, content, expected)
		if err != nil {
			t.Fatalf("PutImmutable(%q): %v", key.String(), err)
		}
		if !result.Created {
			t.Fatalf("PutImmutable(%q): 首次写入应 created=true", key.String())
		}
		return result.Meta
	}

	readAll := func(t *testing.T, reader io.ReadCloser) []byte {
		t.Helper()
		defer func() { _ = reader.Close() }()
		body, err := io.ReadAll(reader)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		return body
	}

	t.Run("CustomerKeyLifecycle", func(t *testing.T) {
		store, accountID := newStore(t)
		key := confCustomerKey(t, accountID, 1)
		content := confContent("one")
		expected := confMeta(content)
		result, err := store.PutImmutable(context.Background(), key, content, expected)
		if err != nil {
			t.Fatalf("PutImmutable: %v", err)
		}
		if !SameObjectMeta(result.Meta, expected) {
			t.Fatalf("PutImmutable 返回 meta 不符: %+v", result.Meta)
		}
		reader, err := store.Open(context.Background(), key)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		if string(readAll(t, reader)) != string(content.Bytes()) {
			t.Fatalf("Open 内容不匹配")
		}
		meta, err := store.Stat(context.Background(), key)
		if err != nil {
			t.Fatalf("Stat: %v", err)
		}
		if !SameObjectMeta(meta, expected) {
			t.Fatalf("Stat meta 不符: %+v != %+v", meta, expected)
		}
	})

	t.Run("AccountProfileKeyLifecycle", func(t *testing.T) {
		store, accountID := newStore(t)
		key := confAccountKey(t, accountID)
		content := confContent("acct")
		expected := confMeta(content)
		if _, err := store.PutImmutable(context.Background(), key, content, expected); err != nil {
			t.Fatalf("PutImmutable: %v", err)
		}
		reader, err := store.Open(context.Background(), key)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		readAll(t, reader)
		if _, err := store.Stat(context.Background(), key); err != nil {
			t.Fatalf("Stat: %v", err)
		}
	})

	t.Run("PutImmutableReplayIsIdempotent", func(t *testing.T) {
		store, accountID := newStore(t)
		key := confCustomerKey(t, accountID, 2)
		content := confContent("two")
		expected := mustAvatarPut(t, store, key, content)
		result, err := store.PutImmutable(context.Background(), key, content, expected)
		if err != nil {
			t.Fatalf("重放 PutImmutable: %v", err)
		}
		if result.Created {
			t.Fatalf("重放应 created=false")
		}
		if !SameObjectMeta(result.Meta, expected) {
			t.Fatalf("重放返回 meta 不符: %+v", result.Meta)
		}
		other := confContent("different-bytes")
		if _, err := store.PutImmutable(context.Background(), key, other, confMeta(other)); !errors.Is(err, ErrObjectIntegrity) {
			t.Fatalf("同 key 不同内容应 ErrObjectIntegrity, got %v", err)
		}
		mediaMismatch := confMeta(content)
		mediaMismatch.MediaType = "image/webp"
		if _, err := store.PutImmutable(context.Background(), key, content, mediaMismatch); !errors.Is(err, ErrObjectIntegrity) {
			t.Fatalf("MediaType 错配应 ErrObjectIntegrity, got %v", err)
		}
	})

	t.Run("OpenStatMissingKeyNotFound", func(t *testing.T) {
		store, accountID := newStore(t)
		key := confCustomerKey(t, accountID, 404)
		if _, err := store.Open(context.Background(), key); !errors.Is(err, ErrObjectNotFound) {
			t.Fatalf("Open 缺失键应 ErrObjectNotFound, got %v", err)
		}
		if _, err := store.Stat(context.Background(), key); !errors.Is(err, ErrObjectNotFound) {
			t.Fatalf("Stat 缺失键应 ErrObjectNotFound, got %v", err)
		}
	})

	t.Run("InvalidKeyRejected", func(t *testing.T) {
		store, _ := newStore(t)
		content := confContent("bad")
		for _, raw := range []string{
			"avatars/acct1/customers/cust/bad/version/obj",
			"avatars/acct1/unknown-subject/sha256-" + strings.Repeat("0", 64) + "/" + strings.Repeat("0", 32),
			"outside/namespace",
		} {
			key := Key{value: raw} // 包内测试直接构造未解析 Key，验证各方法 fail closed。
			if key.IsZero() || key.String() != raw {
				t.Fatalf("测试 key 构造失败: %q", raw)
			}
			if _, err := store.PutImmutable(context.Background(), key, content, confMeta(content)); !errors.Is(err, ErrObjectKey) {
				t.Fatalf("PutImmutable(%q) 应 ErrObjectKey, got %v", raw, err)
			}
			if _, err := store.Open(context.Background(), key); !errors.Is(err, ErrObjectKey) {
				t.Fatalf("Open(%q) 应 ErrObjectKey, got %v", raw, err)
			}
			if err := store.Delete(context.Background(), key); !errors.Is(err, ErrObjectKey) {
				t.Fatalf("Delete(%q) 应 ErrObjectKey, got %v", raw, err)
			}
		}
	})

	t.Run("ListKeysetPagination", func(t *testing.T) {
		store, accountID := newStore(t)
		keys := []Key{
			confCustomerKey(t, accountID, 11),
			confCustomerKey(t, accountID, 12),
			confCustomerKey(t, accountID, 13),
		}
		for i, key := range keys {
			mustAvatarPut(t, store, key, confContent(fmt.Sprintf("list-%d", i)))
		}
		// 账号资料头像同前缀更深处，不应混入 customer 前缀列举。
		mustAvatarPut(t, store, confAccountKey(t, accountID), confContent("account"))

		prefix, err := CustomerPrefix(accountID, "cust011")
		if err != nil {
			t.Fatalf("CustomerPrefix: %v", err)
		}
		full, err := store.List(context.Background(), prefix, Cursor{}, 1000)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(full.Items) != 1 || !full.Done {
			t.Fatalf("单键前缀 List 不符: n=%d done=%v", len(full.Items), full.Done)
		}
		customerPrefix, err := CustomerPrefix(accountID, "cust012")
		if err != nil {
			t.Fatalf("CustomerPrefix: %v", err)
		}
		first, err := store.List(context.Background(), customerPrefix, Cursor{}, 1000)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(first.Items) != 1 {
			t.Fatalf("前缀隔离失败: n=%d", len(first.Items))
		}

		// 账号根前缀应同时覆盖 customer 与 account-profile 键，且 keyset 分页成立。
		root, err := AccountPrefix(accountID)
		if err != nil {
			t.Fatalf("AccountPrefix: %v", err)
		}
		page1, err := store.List(context.Background(), root, Cursor{}, 2)
		if err != nil {
			t.Fatalf("List page1: %v", err)
		}
		if len(page1.Items) != 2 || page1.Done || page1.NextCursor.IsZero() {
			t.Fatalf("page1 不符: n=%d done=%v", len(page1.Items), page1.Done)
		}
		if page1.NextCursor.String() != page1.Items[1].Key.String() {
			t.Fatalf("NextCursor 应等于末项 key: %q != %q", page1.NextCursor.String(), page1.Items[1].Key.String())
		}
		page2, err := store.List(context.Background(), root, page1.NextCursor, 2)
		if err != nil {
			t.Fatalf("List page2: %v", err)
		}
		if len(page2.Items) != 2 || !page2.Done || page2.NextCursor.IsZero() {
			t.Fatalf("page2 不符: n=%d done=%v", len(page2.Items), page2.Done)
		}
		if page2.NextCursor.String() != page2.Items[1].Key.String() {
			t.Fatalf("page2 NextCursor 应等于末项 key: %q", page2.NextCursor.String())
		}
		total := append(append([]ObjectItem{}, page1.Items...), page2.Items...)
		if len(total) != 4 {
			t.Fatalf("账号根应共 4 键, got %d", len(total))
		}
		ordered := make([]string, 0, len(total))
		for _, item := range total {
			ordered = append(ordered, item.Key.String())
		}
		if !sort.StringsAreSorted(ordered) {
			t.Fatalf("跨页顺序应递增: %v", ordered)
		}

		// zero prefix 与非法 limit 拒绝。
		if _, err := store.List(context.Background(), Prefix{}, Cursor{}, 10); !errors.Is(err, ErrObjectKey) {
			t.Fatalf("zero prefix 应 ErrObjectKey, got %v", err)
		}
		for _, limit := range []int{0, 1001} {
			if _, err := store.List(context.Background(), root, Cursor{}, limit); !errors.Is(err, ErrObjectKey) {
				t.Fatalf("limit=%d 应 ErrObjectKey, got %v", limit, err)
			}
		}
	})

	t.Run("DeleteIsIdempotentAndStatGone", func(t *testing.T) {
		store, accountID := newStore(t)
		key := confCustomerKey(t, accountID, 21)
		mustAvatarPut(t, store, key, confContent("delete"))
		if err := store.Delete(context.Background(), key); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		if _, err := store.Stat(context.Background(), key); !errors.Is(err, ErrObjectNotFound) {
			t.Fatalf("删除后 Stat 应 ErrObjectNotFound, got %v", err)
		}
		if err := store.Delete(context.Background(), key); err != nil {
			t.Fatalf("重复 Delete 应幂等: %v", err)
		}
	})

	t.Run("InventoryCoversAccountNamespace", func(t *testing.T) {
		store, accountID := newStore(t)
		inventorier, ok := store.(ObjectInventory)
		if !ok {
			t.Skip("实现未提供 Inventory")
		}
		keys := []Key{
			confCustomerKey(t, accountID, 31),
			confCustomerKey(t, accountID, 32),
			confAccountKey(t, accountID),
		}
		for i, key := range keys {
			mustAvatarPut(t, store, key, confContent(fmt.Sprintf("inv-%d", i)))
		}
		items, err := inventorier.Inventory(context.Background())
		if err != nil {
			t.Fatalf("Inventory: %v", err)
		}
		got := make([]string, 0, len(keys))
		for _, item := range items {
			parsed, err := ParseKey(item.Key.String())
			if err != nil {
				t.Fatalf("Inventory 含不可解析键 %q: %v", item.Key.String(), err)
			}
			if parsed.AccountID != accountID {
				continue
			}
			got = append(got, item.Key.String())
		}
		if len(got) != len(keys) {
			t.Fatalf("Inventory 应含本轮 %d 键, got %v", len(keys), got)
		}
		if !sort.StringsAreSorted(got) {
			t.Fatalf("Inventory 应按 key 排序: %v", got)
		}
	})
}

func versionFor(seed int) string {
	return "sha256-" + fmt.Sprintf("%064x", seed)
}

func objectIDFor(seed int) string {
	return fmt.Sprintf("%032x", seed)
}

func mustAvatarTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return parsed
}

func TestLocalAvatarConformance(t *testing.T) {
	RunConformance(t, func(t *testing.T) (ObjectStore, string) {
		store, err := NewLocal(t.TempDir())
		if err != nil {
			t.Fatalf("NewLocal: %v", err)
		}
		return store, "confacct" + fmt.Sprintf("%d", time.Now().UnixNano()%1000)
	})
}
