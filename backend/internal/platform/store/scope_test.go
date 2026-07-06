package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// probeNotes 读出 scope 可见的全部探针行 note。
func probeNotes(t *testing.T, sc store.AccountScope) []string {
	t.Helper()
	rows, err := sc.Query(context.Background(), "probe_items", "note", "")
	if err != nil {
		t.Fatalf("scoped query: %v", err)
	}
	defer rows.Close()
	var notes []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatalf("scan: %v", err)
		}
		notes = append(notes, n)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	return notes
}

// A9（core）：账号 A 写入探针表的数据经 B 的 AccountScope 不可见，反向同理。
func TestAccountScopeIsolation(t *testing.T) {
	url := startPostgres(t)
	s := openMigrated(t, url)
	if err := store.MigrateProbeUpForTest(url); err != nil {
		t.Fatalf("migrate probe table: %v", err)
	}
	ctx := context.Background()

	// 两个测试账号直接经 store 创建（与 A10 的默认账号断言互不干扰，各用各的一次性库）
	for _, id := range []string{"acct-a", "acct-b"} {
		if err := s.CreateAccount(ctx, id, "test-hash"); err != nil {
			t.Fatalf("create account %s: %v", id, err)
		}
	}
	scopeA := s.ScopeFor(auth.AccountContext{AccountID: "acct-a"})
	scopeB := s.ScopeFor(auth.AccountContext{AccountID: "acct-b"})

	if err := scopeA.Insert(ctx, "probe_items", []string{"id", "note"}, "p-a1", "note-of-A"); err != nil {
		t.Fatalf("insert as A: %v", err)
	}
	if err := scopeB.Insert(ctx, "probe_items", []string{"id", "note"}, "p-b1", "note-of-B"); err != nil {
		t.Fatalf("insert as B: %v", err)
	}

	gotA := probeNotes(t, scopeA)
	if len(gotA) != 1 || gotA[0] != "note-of-A" {
		t.Fatalf("scope A must see exactly its own data, got %v", gotA)
	}
	gotB := probeNotes(t, scopeB)
	if len(gotB) != 1 || gotB[0] != "note-of-B" {
		t.Fatalf("scope B must see exactly its own data, got %v", gotB)
	}
}

// 写路径隔离：B 的 scope 无法更新 / 删除 A 的行（受影响行数为 0）。
func TestAccountScopeWriteIsolation(t *testing.T) {
	url := startPostgres(t)
	s := openMigrated(t, url)
	if err := store.MigrateProbeUpForTest(url); err != nil {
		t.Fatalf("migrate probe table: %v", err)
	}
	ctx := context.Background()

	for _, id := range []string{"acct-a", "acct-b"} {
		if err := s.CreateAccount(ctx, id, "test-hash"); err != nil {
			t.Fatalf("create account %s: %v", id, err)
		}
	}
	scopeA := s.ScopeFor(auth.AccountContext{AccountID: "acct-a"})
	scopeB := s.ScopeFor(auth.AccountContext{AccountID: "acct-b"})

	if err := scopeA.Insert(ctx, "probe_items", []string{"id", "note"}, "p-a1", "note-of-A"); err != nil {
		t.Fatalf("insert as A: %v", err)
	}

	n, err := scopeB.Update(ctx, "probe_items", "note = $2", "id = $3", "hijacked", "p-a1")
	if err != nil {
		t.Fatalf("update as B: %v", err)
	}
	if n != 0 {
		t.Fatalf("scope B must not update A's row, affected %d", n)
	}
	n, err = scopeB.Delete(ctx, "probe_items", "id = $2", "p-a1")
	if err != nil {
		t.Fatalf("delete as B: %v", err)
	}
	if n != 0 {
		t.Fatalf("scope B must not delete A's row, affected %d", n)
	}
	if got := probeNotes(t, scopeA); len(got) != 1 || got[0] != "note-of-A" {
		t.Fatalf("A's data must survive B's write attempts, got %v", got)
	}
}

// 防御路径：空账号标识的 scope 拒绝一切读写。
func TestAccountScopeRejectsEmptyAccount(t *testing.T) {
	url := startPostgres(t)
	s := openMigrated(t, url)

	sc := s.ScopeFor(auth.AccountContext{})
	if _, err := sc.Query(context.Background(), "probe_items", "note", ""); !errors.Is(err, store.ErrEmptyAccountScope) {
		t.Fatalf("want ErrEmptyAccountScope, got: %v", err)
	}
	if err := sc.Insert(context.Background(), "probe_items", []string{"id", "note"}, "x", "y"); !errors.Is(err, store.ErrEmptyAccountScope) {
		t.Fatalf("want ErrEmptyAccountScope, got: %v", err)
	}
}
