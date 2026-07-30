package store

import (
	"context"
	"strings"
	"testing"
)

type nestedTransactionScope interface {
	WithinTx(context.Context, func(AccountScope) error) error
}

func TestTxAccountScopeCannotOpenNestedTransaction(t *testing.T) {
	if _, ok := any(TxAccountScope{}).(nestedTransactionScope); ok {
		t.Fatal("TxAccountScope must not expose WithinTx")
	}
}

// REV-006：table / columns 标识符校验在触达连接池前短路，
// 拦截请求派生字符串误入 SQL 片段位（pool 为 nil 即可测）。
func TestAccountScopeRejectsInvalidIdents(t *testing.T) {
	sc := AccountScope{accountID: "acct-a"}
	ctx := context.Background()

	badTables := []string{"probe_items; DROP TABLE accounts", "probe items", "Probe", ""}
	for _, table := range badTables {
		if _, err := sc.Query(ctx, table, "note", ""); err == nil || !strings.Contains(err.Error(), "非法表名") {
			t.Fatalf("table %q: want 非法表名 error, got %v", table, err)
		}
	}

	if _, err := sc.Query(ctx, "probe_items", "note, 1=1 --", ""); err == nil || !strings.Contains(err.Error(), "非法列名") {
		t.Fatalf("want 非法列名 error, got %v", err)
	}
	if err := sc.Insert(ctx, "probe_items", []string{"note) VALUES ('x'); --"}, "y"); err == nil || !strings.Contains(err.Error(), "非法列名") {
		t.Fatalf("insert: want 非法列名 error, got %v", err)
	}
	if _, err := sc.Update(ctx, "bad table", "note = $2", "", "x"); err == nil || !strings.Contains(err.Error(), "非法表名") {
		t.Fatalf("update: want 非法表名 error, got %v", err)
	}
	if _, err := sc.Delete(ctx, "bad table", ""); err == nil || !strings.Contains(err.Error(), "非法表名") {
		t.Fatalf("delete: want 非法表名 error, got %v", err)
	}
	if _, err := sc.Count(ctx, "bad table", ""); err == nil || !strings.Contains(err.Error(), "非法表名") {
		t.Fatalf("count: want 非法表名 error, got %v", err)
	}
	if _, err := sc.Exists(ctx, "bad table", ""); err == nil || !strings.Contains(err.Error(), "非法表名") {
		t.Fatalf("exists: want 非法表名 error, got %v", err)
	}
	if _, err := sc.InsertReturningID(ctx, "probe_items", []string{"note) VALUES ('x'); --"}, "y"); err == nil || !strings.Contains(err.Error(), "非法列名") {
		t.Fatalf("insert returning: want 非法列名 error, got %v", err)
	}
	var returned string
	err := sc.InsertOnConflictDoNothingReturning(
		ctx,
		"probe_items; DROP TABLE accounts",
		[]string{"id", "note"},
		[]string{"account_id", "id"},
		[]string{"id"},
		"p-1",
		"note",
	).Scan(&returned)
	if err == nil || !strings.Contains(err.Error(), "非法表名") {
		t.Fatalf("insert on conflict: want 非法表名 error, got %v", err)
	}
	err = sc.InsertOnConflictDoNothingReturning(
		ctx,
		"probe_items",
		[]string{"id", "note"},
		[]string{"id"},
		[]string{"id"},
		"p-1",
		"note",
	).Scan(&returned)
	if err == nil || !strings.Contains(err.Error(), "account_id") {
		t.Fatalf("insert on conflict: want account_id conflict target error, got %v", err)
	}

	// 合法标识符（含空格分隔的多列）不被误杀：校验通过后才会触到 nil pool，
	// 这里只断言错误不是标识符校验错误。
	if err := validateIdents("probe_items", strings.Split("id, note", ",")...); err != nil {
		t.Fatalf("legal idents must pass, got %v", err)
	}
}
