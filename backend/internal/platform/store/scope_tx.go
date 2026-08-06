package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// TxAccountScope 是已开启事务内的账号隔离句柄。它刻意不暴露开启事务的方法，
// 避免幂等创建 callback 意外把业务写放进另一个事务。
type TxAccountScope struct {
	scope AccountScope
}

// ReadTxAccountScope is the narrow account-scoped handle for a consistent read snapshot.
// It intentionally has no write, row-locking, nested-transaction, or raw pgx transaction surface.
type ReadTxAccountScope struct {
	scope AccountScope
}

// AccountID returns the authenticated account bound to this physical transaction.
// It never accepts or derives an account identifier from request input.
func (sc TxAccountScope) AccountID() string { return sc.scope.accountID }

// WithinTx 保留既有领域代码的事务入口；新建的跨资源创建编排使用 WithTxScope。
func (sc AccountScope) WithinTx(ctx context.Context, fn func(AccountScope) error) error {
	return sc.withinTx(ctx, fn)
}

// WithTxScope 是 TxAccountScope 的唯一生产构造入口。
func (sc AccountScope) WithTxScope(ctx context.Context, fn func(TxAccountScope) error) error {
	return sc.withinTx(ctx, func(tx AccountScope) error {
		return fn(TxAccountScope{scope: tx})
	})
}

// WithReadSnapshot runs all callback reads in one REPEATABLE READ, READ ONLY transaction.
func (sc AccountScope) WithReadSnapshot(ctx context.Context, fn func(ReadTxAccountScope) error) error {
	if sc.accountID == "" {
		return ErrEmptyAccountScope
	}
	if sc.pool == nil {
		return errors.New("AccountScope 缺连接池，无法开启只读快照")
	}
	if fn == nil {
		return errors.New("AccountScope 只读快照 callback 不能为空")
	}
	tx, err := sc.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return fmt.Errorf("begin scoped read snapshot: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p)
		}
	}()
	readScope := ReadTxAccountScope{scope: AccountScope{
		pool: sc.pool, runner: tx, accountID: sc.accountID,
	}}
	if err := fn(readScope); err != nil {
		if rbErr := tx.Rollback(ctx); rbErr != nil {
			return fmt.Errorf("scoped read snapshot rollback after %w: %w", err, rbErr)
		}
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit scoped read snapshot: %w", err)
	}
	return nil
}

func (sc AccountScope) withinTx(ctx context.Context, fn func(AccountScope) error) error {
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

func (sc TxAccountScope) Query(ctx context.Context, table, columns, cond string, args ...any) (pgx.Rows, error) {
	return sc.scope.Query(ctx, table, columns, cond, args...)
}

func (sc TxAccountScope) QueryRow(ctx context.Context, table, columns, cond string, args ...any) pgx.Row {
	return sc.scope.QueryRow(ctx, table, columns, cond, args...)
}

func (sc TxAccountScope) QueryRowForUpdate(ctx context.Context, table, columns, cond string, args ...any) pgx.Row {
	return sc.scope.QueryRowForUpdate(ctx, table, columns, cond, args...)
}

func (sc TxAccountScope) QueryPage(ctx context.Context, table, columns, cond string, order []OrderBy, limit, offset int, args ...any) (pgx.Rows, error) {
	return sc.scope.QueryPage(ctx, table, columns, cond, order, limit, offset, args...)
}

func (sc TxAccountScope) Count(ctx context.Context, table, cond string, args ...any) (int64, error) {
	return sc.scope.Count(ctx, table, cond, args...)
}

func (sc TxAccountScope) ScalarAggregate(ctx context.Context, table, op, column, cond string, args ...any) pgx.Row {
	return sc.scope.ScalarAggregate(ctx, table, op, column, cond, args...)
}

func (sc TxAccountScope) Exists(ctx context.Context, table, cond string, args ...any) (bool, error) {
	return sc.scope.Exists(ctx, table, cond, args...)
}

func (sc TxAccountScope) Insert(ctx context.Context, table string, cols []string, args ...any) error {
	return sc.scope.Insert(ctx, table, cols, args...)
}

func (sc TxAccountScope) InsertReturningID(ctx context.Context, table string, cols []string, args ...any) (string, error) {
	return sc.scope.InsertReturningID(ctx, table, cols, args...)
}

func (sc TxAccountScope) InsertOnConflictDoNothingReturning(
	ctx context.Context,
	table string,
	cols, conflictCols, returningCols []string,
	args ...any,
) pgx.Row {
	return sc.scope.InsertOnConflictDoNothingReturning(ctx, table, cols, conflictCols, returningCols, args...)
}

func (sc TxAccountScope) Upsert(
	ctx context.Context,
	table string,
	cols, conflictCols, updateCols []string,
	args ...any,
) error {
	return sc.scope.Upsert(ctx, table, cols, conflictCols, updateCols, args...)
}

func (sc TxAccountScope) Update(ctx context.Context, table, setClause, cond string, args ...any) (int64, error) {
	return sc.scope.Update(ctx, table, setClause, cond, args...)
}

func (sc TxAccountScope) Delete(ctx context.Context, table, cond string, args ...any) (int64, error) {
	return sc.scope.Delete(ctx, table, cond, args...)
}

func (sc ReadTxAccountScope) Query(ctx context.Context, table, columns, cond string, args ...any) (pgx.Rows, error) {
	return sc.scope.Query(ctx, table, columns, cond, args...)
}

func (sc ReadTxAccountScope) QueryRow(ctx context.Context, table, columns, cond string, args ...any) pgx.Row {
	return sc.scope.QueryRow(ctx, table, columns, cond, args...)
}

func (sc ReadTxAccountScope) QueryPage(ctx context.Context, table, columns, cond string, order []OrderBy, limit, offset int, args ...any) (pgx.Rows, error) {
	return sc.scope.QueryPage(ctx, table, columns, cond, order, limit, offset, args...)
}

func (sc ReadTxAccountScope) Count(ctx context.Context, table, cond string, args ...any) (int64, error) {
	return sc.scope.Count(ctx, table, cond, args...)
}

func (sc ReadTxAccountScope) ScalarAggregate(ctx context.Context, table, op, column, cond string, args ...any) pgx.Row {
	return sc.scope.ScalarAggregate(ctx, table, op, column, cond, args...)
}

func (sc ReadTxAccountScope) Exists(ctx context.Context, table, cond string, args ...any) (bool, error) {
	return sc.scope.Exists(ctx, table, cond, args...)
}
