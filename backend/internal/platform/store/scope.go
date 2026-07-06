package store

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
)

// ErrEmptyAccountScope：无账号标识的 scope 发起查询（不应发生，auth 中间件保证非空）。
var ErrEmptyAccountScope = errors.New("AccountScope 缺 account_id，拒绝执行业务表查询")

// identPattern：table / columns 标识符白名单（review REV-006）；
// 拦截把请求派生字符串误传进 SQL 片段位的低级错误。
var identPattern = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

// validateIdents 校验表名与列名是标识符字面量。
func validateIdents(table string, cols ...string) error {
	if !identPattern.MatchString(table) {
		return fmt.Errorf("scoped sql: 非法表名 %q（table 必须是编译期标识符字面量）", table)
	}
	for _, col := range cols {
		if !identPattern.MatchString(strings.TrimSpace(col)) {
			return fmt.Errorf("scoped sql: 非法列名 %q（columns 必须是编译期标识符字面量）", col)
		}
	}
	return nil
}

// AccountScope 是业务表读写的唯一句柄：account_id 过滤由基座拼接，
// 域代码无法绕开（ADR-001 结构性强制，design D5）。
//
// SQL 片段契约（review REV-006）：table / columns / cond / setClause 必须是
// 编译期常量，禁止拼接任何请求派生输入——请求值一律走 args 占位符；
// table / columns 有标识符校验兜底，cond / setClause 无法机器校验，靠本契约约束。
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
	if err := validateIdents(table, strings.Split(columns, ",")...); err != nil {
		return nil, err
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
	if err := validateIdents(table, cols...); err != nil {
		return err
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
	if err := validateIdents(table); err != nil {
		return 0, err
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
	if err := validateIdents(table); err != nil {
		return 0, err
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
