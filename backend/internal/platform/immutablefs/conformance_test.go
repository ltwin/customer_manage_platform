package immutablefs

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

// RunConformance 驱动 ObjectStore 实现必须满足的 exact-generation 生命周期契约；
// Local 与 OSS 实现共用。newStore 每次调用返回隔离存储与本轮对象的公共 key 前缀
// （本地实现为空串；共享 bucket 实现返回唯一前缀，Inventory 断言只覆盖该前缀）。
func RunConformance(t *testing.T, newStore func(t *testing.T) (ObjectStore, string)) {
	t.Helper()

	type verifiedOpener interface {
		OpenVerified(context.Context, string, Metadata) (io.ReadCloser, error)
	}
	type exactRemover interface {
		RemoveExact(context.Context, string, Metadata) error
	}

	confKey := func(t *testing.T, prefix, name string) string {
		t.Helper()
		return prefix + name
	}
	confBody := func(seed string) []byte {
		return []byte("immutablefs-conformance:" + seed)
	}
	confMeta := func(body []byte, width, height int) Metadata {
		return Metadata{
			MediaType:  "image/jpeg",
			Size:       int64(len(body)),
			Checksum:   checksum(body),
			Width:      width,
			Height:     height,
			ModifiedAt: mustTime("2026-08-25T10:00:00Z"),
		}
	}
	mustPut := func(t *testing.T, store ObjectStore, key string, seed string, width, height int) Metadata {
		t.Helper()
		body := confBody(seed)
		meta := confMeta(body, width, height)
		result, created, err := store.PutImmutable(context.Background(), key, body, meta)
		if err != nil {
			t.Fatalf("PutImmutable(%q): %v", key, err)
		}
		if !created {
			t.Fatalf("PutImmutable(%q): 首次写入应 created=true", key)
		}
		return result
	}

	t.Run("PutOpenStatLifecycle", func(t *testing.T) {
		store, prefix := newStore(t)
		key := confKey(t, prefix, "alpha/k001")
		body := confBody("one")
		expected := confMeta(body, 640, 480)
		result, created, err := store.PutImmutable(context.Background(), key, body, expected)
		if err != nil || !created {
			t.Fatalf("PutImmutable: err=%v created=%v", err, created)
		}
		if !sameMeta(result, expected) {
			t.Fatalf("PutImmutable 返回 meta 不符: %+v != %+v", result, expected)
		}
		reader, err := store.Open(context.Background(), key)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		got, err := io.ReadAll(reader)
		_ = reader.Close()
		if err != nil {
			t.Fatalf("read Open body: %v", err)
		}
		if string(got) != string(body) {
			t.Fatalf("Open 内容不匹配")
		}
		meta, err := store.Stat(context.Background(), key)
		if err != nil {
			t.Fatalf("Stat: %v", err)
		}
		if !sameMeta(meta, expected) {
			t.Fatalf("Stat meta 不符: %+v != %+v", meta, expected)
		}
	})

	t.Run("PutImmutableReplayIsIdempotent", func(t *testing.T) {
		store, prefix := newStore(t)
		key := confKey(t, prefix, "alpha/k002")
		body := confBody("two")
		expected := mustPutMeta(t, store, key, body)
		meta, created, err := store.PutImmutable(context.Background(), key, body, expected)
		if err != nil {
			t.Fatalf("重放 PutImmutable: %v", err)
		}
		if created {
			t.Fatalf("重放应 created=false")
		}
		if !sameMeta(meta, expected) {
			t.Fatalf("重放返回 meta 不符: %+v", meta)
		}
		other := confBody("different-bytes")
		if _, _, err := store.PutImmutable(context.Background(), key, other, confMeta(other, 0, 0)); !errors.Is(err, ErrIntegrity) {
			t.Fatalf("同 key 不同内容应 ErrIntegrity, got %v", err)
		}
	})

	t.Run("PutImmutableRejectsBodyMetaMismatch", func(t *testing.T) {
		store, prefix := newStore(t)
		key := confKey(t, prefix, "alpha/k003")
		body := confBody("three")
		wrongSize := confMeta(body, 0, 0)
		wrongSize.Size = wrongSize.Size + 1
		if _, _, err := store.PutImmutable(context.Background(), key, body, wrongSize); !errors.Is(err, ErrIntegrity) {
			t.Fatalf("size 不符应 ErrIntegrity, got %v", err)
		}
		wrongChecksum := confMeta(body, 0, 0)
		wrongChecksum.Checksum = alienChecksum()
		if _, _, err := store.PutImmutable(context.Background(), key, body, wrongChecksum); !errors.Is(err, ErrIntegrity) {
			t.Fatalf("checksum 不符应 ErrIntegrity, got %v", err)
		}
		if _, err := store.Stat(context.Background(), key); !errors.Is(err, ErrNotFound) {
			t.Fatalf("拒绝后不应有对象, got %v", err)
		}
	})

	t.Run("OpenStatMissingKeyNotFound", func(t *testing.T) {
		store, prefix := newStore(t)
		key := confKey(t, prefix, "missing/k004")
		if _, err := store.Open(context.Background(), key); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Open 缺失键应 ErrNotFound, got %v", err)
		}
		if _, err := store.Stat(context.Background(), key); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Stat 缺失键应 ErrNotFound, got %v", err)
		}
	})

	t.Run("InvalidKeyRejected", func(t *testing.T) {
		store, prefix := newStore(t)
		body := confBody("invalid")
		meta := confMeta(body, 0, 0)
		for _, key := range []string{"../escape", "/absolute", `back\slash`, "unclean/../key"} {
			if _, _, err := store.PutImmutable(context.Background(), key, body, meta); !errors.Is(err, ErrInvalidKey) {
				t.Fatalf("PutImmutable(%q) 应 ErrInvalidKey, got %v", key, err)
			}
			if _, err := store.Open(context.Background(), key); !errors.Is(err, ErrInvalidKey) {
				t.Fatalf("Open(%q) 应 ErrInvalidKey, got %v", key, err)
			}
			if _, err := store.Stat(context.Background(), key); !errors.Is(err, ErrInvalidKey) {
				t.Fatalf("Stat(%q) 应 ErrInvalidKey, got %v", key, err)
			}
			if err := store.Delete(context.Background(), key); !errors.Is(err, ErrInvalidKey) {
				t.Fatalf("Delete(%q) 应 ErrInvalidKey, got %v", key, err)
			}
		}
		if _, err := store.List(context.Background(), "../escape", "", 10); !errors.Is(err, ErrInvalidKey) {
			t.Fatalf("List 非法前缀应 ErrInvalidKey, got %v", err)
		}
		for _, limit := range []int{0, -1, 1001} {
			if _, err := store.List(context.Background(), prefix, "", limit); !errors.Is(err, ErrInvalidKey) {
				t.Fatalf("List limit=%d 应 ErrInvalidKey, got %v", limit, err)
			}
		}
	})

	t.Run("ListKeysetPagination", func(t *testing.T) {
		store, prefix := newStore(t)
		keys := []string{
			confKey(t, prefix, "list/a001"),
			confKey(t, prefix, "list/a002"),
			confKey(t, prefix, "list/a003"),
		}
		for i, key := range keys {
			mustPut(t, store, key, string(rune('a'+i)), 0, 0)
		}
		// 前缀外的键不得混入。
		mustPut(t, store, confKey(t, prefix, "outside/x001"), "outside", 0, 0)

		full, err := store.List(context.Background(), confKey(t, prefix, "list"), "", 1000)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(full.Items) != len(keys) || !full.Done || full.NextCursor != keys[len(keys)-1] {
			t.Fatalf("全量 List 不符: n=%d done=%v cursor=%q", len(full.Items), full.Done, full.NextCursor)
		}
		for i, item := range full.Items {
			if item.Key != keys[i] {
				t.Fatalf("List 顺序不符: 第 %d 项 %q != %q", i, item.Key, keys[i])
			}
		}

		first, err := store.List(context.Background(), confKey(t, prefix, "list"), "", 2)
		if err != nil {
			t.Fatalf("List page1: %v", err)
		}
		if len(first.Items) != 2 || first.Done || first.NextCursor != keys[1] {
			t.Fatalf("page1 不符: n=%d done=%v cursor=%q", len(first.Items), first.Done, first.NextCursor)
		}
		second, err := store.List(context.Background(), confKey(t, prefix, "list"), first.NextCursor, 2)
		if err != nil {
			t.Fatalf("List page2: %v", err)
		}
		if len(second.Items) != 1 || !second.Done || second.Items[0].Key != keys[2] {
			t.Fatalf("page2 不符: n=%d done=%v", len(second.Items), second.Done)
		}
		exhausted, err := store.List(context.Background(), confKey(t, prefix, "list"), second.NextCursor, 2)
		if err != nil {
			t.Fatalf("List page3: %v", err)
		}
		if len(exhausted.Items) != 0 || !exhausted.Done || exhausted.NextCursor != "" {
			t.Fatalf("page3 应空且 done, n=%d done=%v cursor=%q", len(exhausted.Items), exhausted.Done, exhausted.NextCursor)
		}
	})

	t.Run("DeleteIsIdempotentAndStatGone", func(t *testing.T) {
		store, prefix := newStore(t)
		key := confKey(t, prefix, "alpha/k005")
		body := confBody("five")
		mustPutMeta(t, store, key, body)
		if err := store.Delete(context.Background(), key); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		if _, err := store.Stat(context.Background(), key); !errors.Is(err, ErrNotFound) {
			t.Fatalf("删除后 Stat 应 ErrNotFound, got %v", err)
		}
		if err := store.Delete(context.Background(), key); err != nil {
			t.Fatalf("重复 Delete 应幂等: %v", err)
		}
	})

	t.Run("OpenVerifiedMatchesExactMetadata", func(t *testing.T) {
		store, prefix := newStore(t)
		verified, ok := store.(verifiedOpener)
		if !ok {
			t.Skip("实现未提供 OpenVerified")
		}
		key := confKey(t, prefix, "alpha/k006")
		body := confBody("six")
		expected := mustPutMeta(t, store, key, body)
		reader, err := verified.OpenVerified(context.Background(), key, expected)
		if err != nil {
			t.Fatalf("OpenVerified: %v", err)
		}
		got, err := io.ReadAll(reader)
		_ = reader.Close()
		if err != nil || string(got) != string(body) {
			t.Fatalf("OpenVerified 内容不符: %v", err)
		}
		mutated := expected
		mutated.Checksum = alienChecksum()
		if _, err := verified.OpenVerified(context.Background(), key, mutated); !errors.Is(err, ErrIntegrity) {
			t.Fatalf("OpenVerified 元数据错配应 ErrIntegrity, got %v", err)
		}
		if _, err := verified.OpenVerified(context.Background(), confKey(t, prefix, "missing/k007"), expected); !errors.Is(err, ErrNotFound) {
			t.Fatalf("OpenVerified 缺失键应 ErrNotFound, got %v", err)
		}
	})

	t.Run("RemoveExactGuardsAndIdempotent", func(t *testing.T) {
		store, prefix := newStore(t)
		exact, ok := store.(exactRemover)
		if !ok {
			t.Skip("实现未提供 RemoveExact")
		}
		key := confKey(t, prefix, "alpha/k008")
		body := confBody("eight")
		expected := mustPutMeta(t, store, key, body)
		if err := exact.RemoveExact(context.Background(), key, expected); err != nil {
			t.Fatalf("RemoveExact: %v", err)
		}
		if _, err := store.Stat(context.Background(), key); !errors.Is(err, ErrNotFound) {
			t.Fatalf("RemoveExact 后应 ErrNotFound, got %v", err)
		}
		if err := exact.RemoveExact(context.Background(), key, expected); err != nil {
			t.Fatalf("RemoveExact 已删应幂等返回 nil, got %v", err)
		}
		other := confBody("another")
		key2 := confKey(t, prefix, "alpha/k009")
		expected2 := mustPutMeta(t, store, key2, other)
		mutated := expected2
		mutated.Checksum = alienChecksum()
		if err := exact.RemoveExact(context.Background(), key2, mutated); !errors.Is(err, ErrIntegrity) {
			t.Fatalf("RemoveExact 错配应 ErrIntegrity, got %v", err)
		}
		if _, err := store.Stat(context.Background(), key2); err != nil {
			t.Fatalf("RemoveExact 错配后对象应保留: %v", err)
		}
	})

	t.Run("ListCoversRunPrefix", func(t *testing.T) {
		store, prefix := newStore(t)
		keys := []string{
			confKey(t, prefix, "inv/b001"),
			confKey(t, prefix, "inv/b002"),
		}
		for i, key := range keys {
			mustPut(t, store, key, string(rune('m'+i)), 800, 600)
		}
		items, err := listConformancePrefix(context.Background(), store, prefix)
		if err != nil {
			t.Fatalf("List prefix: %v", err)
		}
		got := make([]string, 0, len(keys))
		for _, item := range items {
			if prefix != "" && !strings.HasPrefix(item.Key, prefix) {
				continue
			}
			got = append(got, item.Key)
		}
		if len(got) != len(keys) {
			t.Fatalf("Inventory 应含本轮 %d 键, got %v", len(keys), got)
		}
		for i := range keys {
			if got[i] != keys[i] {
				t.Fatalf("Inventory 键不符: %v != %v", got, keys)
			}
		}
	})
}

func listConformancePrefix(ctx context.Context, store ObjectStore, prefix string) ([]Item, error) {
	items := make([]Item, 0)
	cursor := ""
	for {
		page, err := store.List(ctx, prefix, cursor, 1000)
		if err != nil {
			return nil, err
		}
		items = append(items, page.Items...)
		if page.Done {
			return items, nil
		}
		cursor = page.NextCursor
	}
}

func mustPutMeta(t *testing.T, store ObjectStore, key string, body []byte) Metadata {
	t.Helper()
	meta := Metadata{
		MediaType:  "image/jpeg",
		Size:       int64(len(body)),
		Checksum:   checksum(body),
		ModifiedAt: mustTime("2026-08-25T10:00:00Z"),
	}
	if _, created, err := store.PutImmutable(context.Background(), key, body, meta); err != nil || !created {
		t.Fatalf("PutImmutable(%q): err=%v created=%v", key, err, created)
	}
	return meta
}

func TestLocalConformance(t *testing.T) {
	RunConformance(t, func(t *testing.T) (ObjectStore, string) {
		store, err := NewLocal(t.TempDir())
		if err != nil {
			t.Fatalf("NewLocal: %v", err)
		}
		return store, ""
	})
}

func alienChecksum() string {
	return "sha256-" + strings.Repeat("0", 64)
}

func mustTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return parsed
}
