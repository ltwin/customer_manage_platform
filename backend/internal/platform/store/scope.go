package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
)

// ErrEmptyAccountScope：无账号标识的 scope 发起查询（不应发生，auth 中间件保证非空）。
var ErrEmptyAccountScope = errors.New("AccountScope 缺 account_id，拒绝执行业务表查询")

// AccountScope 是业务表读写的唯一句柄：account_id 过滤由基座拼接，
// 域代码无法绕开（ADR-001 结构性强制，design D5）。
type AccountScope struct {
	pool      *pgxpool.Pool
	accountID string
}

// ScopeFor 是 AccountScope 的唯一构造入口：只接受 AccountContext，
// 保证账号标识来自 auth 中间件而非客户端参数。
func (s *Store) ScopeFor(ac auth.AccountContext) AccountScope {
	return AccountScope{pool: s.pool, accountID: ac.AccountID}
}

// Query 查询业务表：基座拼接 WHERE account_id = $1；
// cond 为附加条件（可空），其占位符从 $2 起编号。
func (sc AccountScope) Query(ctx context.Context, table, columns, cond string, args ...any) (pgx.Rows, error) {
	if sc.accountID == "" {
		return nil, ErrEmptyAccountScope
	}
	sql := fmt.Sprintf(`SELECT %s FROM %s WHERE account_id = $1`, columns, table)
	if cond != "" {
		sql += " AND (" + cond + ")"
	}
	rows, err := sc.pool.Query(ctx, sql, append([]any{sc.accountID}, args...)...)
	if err != nil {
		return nil, fmt.Errorf("scoped query %s: %w", table, err)
	}
	return rows, nil
}

// Insert 向业务表插入一行：account_id 列由基座写入，调用方不提供、也提供不了。
func (sc AccountScope) Insert(ctx context.Context, table string, cols []string, args ...any) error {
	if sc.accountID == "" {
		return ErrEmptyAccountScope
	}
	if len(cols) != len(args) {
		return fmt.Errorf("scoped insert %s: %d columns but %d values", table, len(cols), len(args))
	}
	placeholders := make([]string, 0, len(args)+1)
	for i := range len(args) + 1 {
		placeholders = append(placeholders, fmt.Sprintf("$%d", i+1))
	}
	sql := fmt.Sprintf(`INSERT INTO %s (account_id, %s) VALUES (%s)`,
		table, strings.Join(cols, ", "), strings.Join(placeholders, ", "))
	if _, err := sc.pool.Exec(ctx, sql, append([]any{sc.accountID}, args...)...); err != nil {
		return fmt.Errorf("scoped insert %s: %w", table, err)
	}
	return nil
}

// Update 更新业务表：基座拼接 WHERE account_id = $1；
// setClause 与 cond 的占位符从 $2 起统一编号。返回受影响行数。
func (sc AccountScope) Update(ctx context.Context, table, setClause, cond string, args ...any) (int64, error) {
	if sc.accountID == "" {
		return 0, ErrEmptyAccountScope
	}
	sql := fmt.Sprintf(`UPDATE %s SET %s WHERE account_id = $1`, table, setClause)
	if cond != "" {
		sql += " AND (" + cond + ")"
	}
	tag, err := sc.pool.Exec(ctx, sql, append([]any{sc.accountID}, args...)...)
	if err != nil {
		return 0, fmt.Errorf("scoped update %s: %w", table, err)
	}
	return tag.RowsAffected(), nil
}

// Delete 删除业务表行：基座拼接 WHERE account_id = $1；cond 占位符从 $2 起。返回受影响行数。
func (sc AccountScope) Delete(ctx context.Context, table, cond string, args ...any) (int64, error) {
	if sc.accountID == "" {
		return 0, ErrEmptyAccountScope
	}
	sql := fmt.Sprintf(`DELETE FROM %s WHERE account_id = $1`, table)
	if cond != "" {
		sql += " AND (" + cond + ")"
	}
	tag, err := sc.pool.Exec(ctx, sql, append([]any{sc.accountID}, args...)...)
	if err != nil {
		return 0, fmt.Errorf("scoped delete %s: %w", table, err)
	}
	return tag.RowsAffected(), nil
}
