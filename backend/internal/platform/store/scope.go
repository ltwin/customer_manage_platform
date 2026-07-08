package store

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
)

// ErrEmptyAccountScope：无账号标识的 scope 发起查询（不应发生，auth 中间件保证非空）。
var ErrEmptyAccountScope = errors.New("AccountScope 缺 account_id，拒绝执行业务表查询")

// ErrNoRows 暴露 store 边界内的空结果哨兵，避免业务域直接 import pgx。
var ErrNoRows = pgx.ErrNoRows

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
	runner    scopedRunner
	accountID string
}

type scopedRunner interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

// ScopeFor 是 AccountScope 的唯一构造入口：只接受 AccountContext，
// 保证账号标识来自 auth 中间件而非客户端参数。
func (s *Store) ScopeFor(ac auth.AccountContext) AccountScope {
	return AccountScope{pool: s.pool, runner: s.pool, accountID: ac.AccountID}
}

func (sc AccountScope) execRunner() scopedRunner {
	if sc.runner != nil {
		return sc.runner
	}
	return sc.pool
}

// WithinTx 在同一账号 scope 内执行事务；fn 返回错误时整体回滚。
func (sc AccountScope) WithinTx(ctx context.Context, fn func(AccountScope) error) error {
	if sc.accountID == "" {
		return ErrEmptyAccountScope
	}
	if sc.pool == nil {
		return errors.New("AccountScope 缺连接池，无法开启事务")
	}
	tx, err := sc.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin scoped tx: %w", err)
	}
	// fn panic 时同样回滚，避免连接带着未结事务泄漏（review REV-003）。
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p)
		}
	}()
	txScope := AccountScope{pool: sc.pool, runner: tx, accountID: sc.accountID}
	if err := fn(txScope); err != nil {
		if rbErr := tx.Rollback(ctx); rbErr != nil {
			return fmt.Errorf("scoped tx rollback after %w: %w", err, rbErr)
		}
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit scoped tx: %w", err)
	}
	return nil
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
	rows, err := sc.execRunner().Query(ctx, sql, append([]any{sc.accountID}, args...)...)
	if err != nil {
		return nil, fmt.Errorf("scoped query %s: %w", table, err)
	}
	return rows, nil
}

// QueryRow 查询单行业务表：基座拼接 WHERE account_id = $1。
func (sc AccountScope) QueryRow(ctx context.Context, table, columns, cond string, args ...any) pgx.Row {
	if sc.accountID == "" {
		return errRow{err: ErrEmptyAccountScope}
	}
	if err := validateIdents(table, strings.Split(columns, ",")...); err != nil {
		return errRow{err: err}
	}
	sql := fmt.Sprintf(`SELECT %s FROM %s WHERE account_id = $1`, columns, table)
	if cond != "" {
		sql += " AND (" + cond + ")"
	}
	return sc.execRunner().QueryRow(ctx, sql, append([]any{sc.accountID}, args...)...)
}

// QueryRowForUpdate 同 QueryRow，但对命中行加 FOR UPDATE 行锁：
// 供「先锁父行、再查数改写」的同事务不变量守护使用（如客户末位身份守护），
// 只应在 WithinTx 内调用，锁随事务结束释放。
func (sc AccountScope) QueryRowForUpdate(ctx context.Context, table, columns, cond string, args ...any) pgx.Row {
	if sc.accountID == "" {
		return errRow{err: ErrEmptyAccountScope}
	}
	if err := validateIdents(table, strings.Split(columns, ",")...); err != nil {
		return errRow{err: err}
	}
	sql := fmt.Sprintf(`SELECT %s FROM %s WHERE account_id = $1`, columns, table)
	if cond != "" {
		sql += " AND (" + cond + ")"
	}
	sql += " FOR UPDATE"
	return sc.execRunner().QueryRow(ctx, sql, append([]any{sc.accountID}, args...)...)
}

// QueryPage 查询业务表并在数据库侧完成排序与分页（review REV-001）：
// orderBy 必须是编译期常量的列名标识符序列（如 "created_at DESC, id DESC" 由
// 调用方拆成 [{col, desc}]），limit / offset 走占位符参数，
// 请求派生值永远进不了 SQL 片段位。
func (sc AccountScope) QueryPage(ctx context.Context, table, columns, cond string, order []OrderBy, limit, offset int, args ...any) (pgx.Rows, error) {
	if sc.accountID == "" {
		return nil, ErrEmptyAccountScope
	}
	if err := validateIdents(table, strings.Split(columns, ",")...); err != nil {
		return nil, err
	}
	if len(order) == 0 {
		return nil, fmt.Errorf("scoped query page %s: 缺 order by（分页必须有稳定排序）", table)
	}
	orderCols := make([]string, 0, len(order))
	for _, o := range order {
		if !identPattern.MatchString(o.Column) {
			return nil, fmt.Errorf("scoped query page %s: 非法排序列 %q", table, o.Column)
		}
		dir := "ASC"
		if o.Desc {
			dir = "DESC"
		}
		orderCols = append(orderCols, o.Column+" "+dir)
	}
	if limit < 1 || offset < 0 {
		return nil, fmt.Errorf("scoped query page %s: 非法 limit=%d offset=%d", table, limit, offset)
	}
	sql := fmt.Sprintf(`SELECT %s FROM %s WHERE account_id = $1`, columns, table)
	if cond != "" {
		sql += " AND (" + cond + ")"
	}
	limitPos := len(args) + 2
	sql += fmt.Sprintf(" ORDER BY %s LIMIT $%d OFFSET $%d", strings.Join(orderCols, ", "), limitPos, limitPos+1)
	rows, err := sc.execRunner().Query(ctx, sql, append(append([]any{sc.accountID}, args...), limit, offset)...)
	if err != nil {
		return nil, fmt.Errorf("scoped query page %s: %w", table, err)
	}
	return rows, nil
}

// OrderBy 是 QueryPage 的排序项；Column 必须是编译期常量列名。
type OrderBy struct {
	Column string
	Desc   bool
}

// Count 返回当前账号可见的业务表行数；cond 占位符从 $2 起编号。
func (sc AccountScope) Count(ctx context.Context, table, cond string, args ...any) (int64, error) {
	if sc.accountID == "" {
		return 0, ErrEmptyAccountScope
	}
	if err := validateIdents(table); err != nil {
		return 0, err
	}
	sql := fmt.Sprintf(`SELECT count(*) FROM %s WHERE account_id = $1`, table)
	if cond != "" {
		sql += " AND (" + cond + ")"
	}
	var count int64
	if err := sc.execRunner().QueryRow(ctx, sql, append([]any{sc.accountID}, args...)...).Scan(&count); err != nil {
		return 0, fmt.Errorf("scoped count %s: %w", table, err)
	}
	return count, nil
}

// Exists 判断当前账号 scope 内是否存在匹配行；cond 占位符从 $2 起编号。
func (sc AccountScope) Exists(ctx context.Context, table, cond string, args ...any) (bool, error) {
	if sc.accountID == "" {
		return false, ErrEmptyAccountScope
	}
	if err := validateIdents(table); err != nil {
		return false, err
	}
	sql := fmt.Sprintf(`SELECT EXISTS (SELECT 1 FROM %s WHERE account_id = $1`, table)
	if cond != "" {
		sql += " AND (" + cond + ")"
	}
	sql += ")"
	var exists bool
	if err := sc.execRunner().QueryRow(ctx, sql, append([]any{sc.accountID}, args...)...).Scan(&exists); err != nil {
		return false, fmt.Errorf("scoped exists %s: %w", table, err)
	}
	return exists, nil
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
	if _, err := sc.execRunner().Exec(ctx, sql, append([]any{sc.accountID}, args...)...); err != nil {
		return fmt.Errorf("scoped insert %s: %w", table, err)
	}
	return nil
}

// InsertReturningID 向业务表插入一行并返回服务端生成或调用方提供的 id。
func (sc AccountScope) InsertReturningID(ctx context.Context, table string, cols []string, args ...any) (string, error) {
	if sc.accountID == "" {
		return "", ErrEmptyAccountScope
	}
	if err := validateIdents(table, cols...); err != nil {
		return "", err
	}
	if len(cols) != len(args) {
		return "", fmt.Errorf("scoped insert %s: %d columns but %d values", table, len(cols), len(args))
	}
	placeholders := make([]string, 0, len(args)+1)
	for i := range len(args) + 1 {
		placeholders = append(placeholders, fmt.Sprintf("$%d", i+1))
	}
	sql := fmt.Sprintf(`INSERT INTO %s (account_id, %s) VALUES (%s) RETURNING id`,
		table, strings.Join(cols, ", "), strings.Join(placeholders, ", "))
	var id string
	if err := sc.execRunner().QueryRow(ctx, sql, append([]any{sc.accountID}, args...)...).Scan(&id); err != nil {
		return "", fmt.Errorf("scoped insert returning id %s: %w", table, err)
	}
	return id, nil
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
	tag, err := sc.execRunner().Exec(ctx, sql, append([]any{sc.accountID}, args...)...)
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
	tag, err := sc.execRunner().Exec(ctx, sql, append([]any{sc.accountID}, args...)...)
	if err != nil {
		return 0, fmt.Errorf("scoped delete %s: %w", table, err)
	}
	return tag.RowsAffected(), nil
}

type errRow struct {
	err error
}

func (r errRow) Scan(_ ...any) error {
	return r.err
}
