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

// 事务路径：同一账号 scope 内多行写入要么一起提交，要么一起回滚。
func TestAccountScopeTransactionCommitAndRollback(t *testing.T) {
	url := startPostgres(t)
	s := openMigrated(t, url)
	if err := store.MigrateProbeUpForTest(url); err != nil {
		t.Fatalf("migrate probe table: %v", err)
	}
	ctx := context.Background()

	if err := s.CreateAccount(ctx, "acct-a", "test-hash"); err != nil {
		t.Fatalf("create account: %v", err)
	}
	scopeA := s.ScopeFor(auth.AccountContext{AccountID: "acct-a"})

	if err := scopeA.WithinTx(ctx, func(tx store.AccountScope) error {
		id, err := tx.InsertReturningID(ctx, "probe_items", []string{"id", "note"}, "p-a1", "first")
		if err != nil {
			return err
		}
		if id != "p-a1" {
			t.Fatalf("returning id = %q, want p-a1", id)
		}
		return tx.Insert(ctx, "probe_items", []string{"id", "note"}, "p-a2", "second")
	}); err != nil {
		t.Fatalf("commit tx: %v", err)
	}
	if got := probeNotes(t, scopeA); len(got) != 2 {
		t.Fatalf("committed tx should write two rows, got %v", got)
	}

	wantErr := errors.New("stop before commit")
	err := scopeA.WithinTx(ctx, func(tx store.AccountScope) error {
		if err := tx.Insert(ctx, "probe_items", []string{"id", "note"}, "p-rollback", "rollback"); err != nil {
			return err
		}
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("rollback tx: want sentinel error, got %v", err)
	}
	exists, err := scopeA.Exists(ctx, "probe_items", "id = $2", "p-rollback")
	if err != nil {
		t.Fatalf("exists after rollback: %v", err)
	}
	if exists {
		t.Fatal("rolled back row must not be visible")
	}

	// panic 路径：fn panic 时事务同样回滚，panic 继续向上传播（REV-003）。
	func() {
		defer func() {
			if p := recover(); p == nil {
				t.Fatal("WithinTx must re-panic after rollback")
			}
		}()
		_ = scopeA.WithinTx(ctx, func(tx store.AccountScope) error {
			if err := tx.Insert(ctx, "probe_items", []string{"id", "note"}, "p-panic", "panic"); err != nil {
				return err
			}
			panic("boom inside tx")
		})
	}()
	exists, err = scopeA.Exists(ctx, "probe_items", "id = $2", "p-panic")
	if err != nil {
		t.Fatalf("exists after panic rollback: %v", err)
	}
	if exists {
		t.Fatal("panicked tx row must not be visible")
	}
}

// 受控读模型：count / exists / 子查询搜索仍由当前账号 scope 绑定。
func TestAccountScopeCountExistsAndHandleSearchStayScoped(t *testing.T) {
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

	if err := scopeA.Insert(ctx, "probe_items", []string{"id", "note"}, "p-a1", "A-visible"); err != nil {
		t.Fatalf("insert A item: %v", err)
	}
	if err := scopeA.Insert(ctx, "probe_item_tags", []string{"id", "item_id", "handle"}, "tag-a1", "p-a1", "needle-from-A"); err != nil {
		t.Fatalf("insert A tag: %v", err)
	}
	if err := scopeB.Insert(ctx, "probe_items", []string{"id", "note"}, "p-b1", "B-visible"); err != nil {
		t.Fatalf("insert B item: %v", err)
	}
	if err := scopeB.Insert(ctx, "probe_item_tags", []string{"id", "item_id", "handle"}, "tag-b1", "p-b1", "needle-from-B"); err != nil {
		t.Fatalf("insert B tag: %v", err)
	}

	countA, err := scopeA.Count(ctx, "probe_items", "")
	if err != nil {
		t.Fatalf("count A: %v", err)
	}
	if countA != 1 {
		t.Fatalf("scope A count = %d, want 1", countA)
	}
	existsForA, err := scopeA.Exists(ctx, "probe_items", "id = $2", "p-a1")
	if err != nil {
		t.Fatalf("exists A: %v", err)
	}
	existsForB, err := scopeB.Exists(ctx, "probe_items", "id = $2", "p-a1")
	if err != nil {
		t.Fatalf("exists B: %v", err)
	}
	if !existsForA || existsForB {
		t.Fatalf("referrer visibility must be account scoped, A=%v B=%v", existsForA, existsForB)
	}

	rows, err := scopeA.Query(ctx, "probe_items", "id, note",
		`EXISTS (
			SELECT 1 FROM probe_item_tags
			WHERE probe_item_tags.account_id = probe_items.account_id
			  AND probe_item_tags.item_id = probe_items.id
			  AND probe_item_tags.handle ILIKE $2
		)`, "%needle%")
	if err != nil {
		t.Fatalf("handle search as A: %v", err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var id, note string
		if err := rows.Scan(&id, &note); err != nil {
			t.Fatalf("scan handle search: %v", err)
		}
		got = append(got, id+":"+note)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	if len(got) != 1 || got[0] != "p-a1:A-visible" {
		t.Fatalf("handle search must return only A row, got %v", got)
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
	if _, err := sc.Count(context.Background(), "probe_items", ""); !errors.Is(err, store.ErrEmptyAccountScope) {
		t.Fatalf("want ErrEmptyAccountScope, got: %v", err)
	}
	if _, err := sc.Exists(context.Background(), "probe_items", ""); !errors.Is(err, store.ErrEmptyAccountScope) {
		t.Fatalf("want ErrEmptyAccountScope, got: %v", err)
	}
	if _, err := sc.InsertReturningID(context.Background(), "probe_items", []string{"id", "note"}, "x", "y"); !errors.Is(err, store.ErrEmptyAccountScope) {
		t.Fatalf("want ErrEmptyAccountScope, got: %v", err)
	}
	if err := sc.QueryRow(context.Background(), "probe_items", "note", "").Scan(new(string)); !errors.Is(err, store.ErrEmptyAccountScope) {
		t.Fatalf("want ErrEmptyAccountScope, got: %v", err)
	}
}
