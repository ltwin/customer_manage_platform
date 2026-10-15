package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrCommitOutcomeUnknown means a COMMIT acknowledgement failed. Callers must
// retain external objects until reconciliation proves they are unreferenced.
var ErrCommitOutcomeUnknown = errors.New("commit outcome unknown")

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

// BoundAccountScope returns the account-bound scope that owns this transaction.
// Trusted domain adapters may capture it for post-commit cleanup (for example
// planning-media read-pin release). HTTP handlers must never call this.
func (sc TxAccountScope) BoundAccountScope() AccountScope { return sc.scope }

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
	tx, err := beginTx(ctx, sc.pool, pgx.TxOptions{
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

// A short network drop should stall a request, not fail it. beginBudget bounds
// the whole wait, connecting included; the backoff's last step repeats.
// Variable only so tests need not wait it out.
var (
	beginBudget  = 10 * time.Second
	beginBackoff = []time.Duration{250 * time.Millisecond, 500 * time.Millisecond, time.Second, 2 * time.Second}
)

// beginTx retries only failures that sent nothing to the server, so no
// statement can run twice.
func beginTx(ctx context.Context, pool *pgxpool.Pool, opts pgx.TxOptions) (pgx.Tx, error) {
	deadline := time.Now().Add(beginBudget)
	tx, err := pool.BeginTx(ctx, opts)
	for attempt := 0; err != nil && unreachable(err); attempt++ {
		wait := beginBackoff[min(attempt, len(beginBackoff)-1)]
		if !time.Now().Add(wait).Before(deadline) {
			break
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, fmt.Errorf("%w: %w", ctx.Err(), err)
		case <-timer.C:
		}
		attemptCtx, cancel := context.WithDeadline(ctx, deadline)
		next, nextErr := pool.BeginTx(attemptCtx, opts)
		budgetCut := attemptCtx.Err() != nil && ctx.Err() == nil
		cancel()
		if nextErr != nil && budgetCut {
			// Cut short by the budget, not by the caller: report the outage
			// rather than a deadline the caller never set.
			break
		}
		tx, err = next, nextErr
	}
	return tx, err
}

// unreachable reports a failure of the network rather than of the request or
// the configuration: nothing was sent, and the server did not refuse.
func unreachable(err error) bool {
	var refused *pgconn.PgError
	if errors.As(err, &refused) {
		// Only refusals that pass on their own: too many connections, or a
		// server still starting up.
		return refused.Code == "53300" || refused.Code == "57P03"
	}
	var connect *pgconn.ConnectError
	return errors.As(err, &connect) || pgconn.SafeToRetry(err)
}

func (sc AccountScope) withinTx(ctx context.Context, fn func(AccountScope) error) error {
	if sc.accountID == "" {
		return ErrEmptyAccountScope
	}
	if sc.pool == nil {
		return errors.New("AccountScope 缺连接池，无法开启事务")
	}
	tx, err := beginTx(ctx, sc.pool, pgx.TxOptions{})
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
		return fmt.Errorf("commit scoped tx: %w: %w", ErrCommitOutcomeUnknown, err)
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

func (sc TxAccountScope) GroupCount(ctx context.Context, table, column, cond string, args ...any) (map[string]int64, error) {
	return sc.scope.GroupCount(ctx, table, column, cond, args...)
}

func (sc TxAccountScope) Exists(ctx context.Context, table, cond string, args ...any) (bool, error) {
	return sc.scope.Exists(ctx, table, cond, args...)
}

func (sc TxAccountScope) Insert(ctx context.Context, table string, cols []string, args ...any) error {
	return sc.scope.Insert(ctx, table, cols, args...)
}

func (sc TxAccountScope) InsertRows(ctx context.Context, table string, cols []string, rows [][]any) error {
	return sc.scope.InsertRows(ctx, table, cols, rows)
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

func (sc ReadTxAccountScope) GroupCount(ctx context.Context, table, column, cond string, args ...any) (map[string]int64, error) {
	return sc.scope.GroupCount(ctx, table, column, cond, args...)
}

func (sc ReadTxAccountScope) Exists(ctx context.Context, table, cond string, args ...any) (bool, error) {
	return sc.scope.Exists(ctx, table, cond, args...)
}
