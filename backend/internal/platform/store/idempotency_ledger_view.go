package store

import (
	"context"

	"github.com/samson/customer-manage-platform/backend/internal/platform/txcap"
)

func (sc TxAccountScope) IdempotencyLedgerView() txcap.LedgerTxView {
	return txcap.BindTrustedLedgerView(txcap.TrustedLedgerOperations{
		InsertOnConflictDoNothingReturning: func(ctx context.Context, table string, cols, conflictCols, returningCols []string, args ...any) txcap.Row {
			return sc.InsertOnConflictDoNothingReturning(ctx, table, cols, conflictCols, returningCols, args...)
		},
		QueryRowForUpdate: func(ctx context.Context, table, columns, cond string, args ...any) txcap.Row {
			return sc.QueryRowForUpdate(ctx, table, columns, cond, args...)
		},
		Update: sc.Update,
		Delete: sc.Delete,
	})
}
