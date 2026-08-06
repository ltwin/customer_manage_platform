package immutablefs

import (
	"context"
	"errors"
	"testing"
)

func TestLocalImmutableExactLifecycleAndTraversal(t *testing.T) {
	s, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("object")
	meta := Metadata{MediaType: "image/png", Size: int64(len(body)), Checksum: checksum(body)}
	key := "planning/acct/asset/g1/display"
	got, created, err := s.PutImmutable(context.Background(), key, body, meta)
	if err != nil || !created || !sameMeta(got, meta) {
		t.Fatalf("put=%+v created=%v err=%v", got, created, err)
	}
	if _, created, err = s.PutImmutable(context.Background(), key, body, meta); err != nil || created {
		t.Fatalf("replay created=%v err=%v", created, err)
	}
	if _, _, err = s.PutImmutable(context.Background(), "../escape", body, meta); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("traversal err=%v", err)
	}
	reader, err := s.OpenVerified(context.Background(), key, meta)
	if err != nil {
		t.Fatal(err)
	}
	_ = reader.Close()
	items, err := s.Inventory(context.Background())
	if err != nil || len(items) != 1 {
		t.Fatalf("inventory=%+v err=%v", items, err)
	}
	if err := s.RemoveExact(context.Background(), key, meta); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Stat(context.Background(), key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after remove=%v", err)
	}
}
