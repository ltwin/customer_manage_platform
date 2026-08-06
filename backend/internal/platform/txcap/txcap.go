// Package txcap contains types-only transaction capability contracts. It does
// not import store, idempotency, planshare, pgx, or HTTP packages.
package txcap

import (
	"context"
)

type Row interface {
	Scan(...any) error
}

type LedgerTxView interface {
	InsertOnConflictDoNothingReturning(
		context.Context,
		string,
		[]string,
		[]string,
		[]string,
		...any,
	) Row
	QueryRowForUpdate(context.Context, string, string, string, ...any) Row
	Update(context.Context, string, string, string, ...any) (int64, error)
	Delete(context.Context, string, string, ...any) (int64, error)
	ledgerTxViewSeal()
}

type TrustedLedgerOperations struct {
	InsertOnConflictDoNothingReturning func(context.Context, string, []string, []string, []string, ...any) Row
	QueryRowForUpdate                  func(context.Context, string, string, string, ...any) Row
	Update                             func(context.Context, string, string, string, ...any) (int64, error)
	Delete                             func(context.Context, string, string, ...any) (int64, error)
}

type ledgerView struct{ ops TrustedLedgerOperations }

func BindTrustedLedgerView(ops TrustedLedgerOperations) LedgerTxView { return ledgerView{ops: ops} }

func (ledgerView) ledgerTxViewSeal() {}

func (v ledgerView) InsertOnConflictDoNothingReturning(ctx context.Context, table string, cols, conflictCols, returningCols []string, args ...any) Row {
	return v.ops.InsertOnConflictDoNothingReturning(ctx, table, cols, conflictCols, returningCols, args...)
}
func (v ledgerView) QueryRowForUpdate(ctx context.Context, table, columns, cond string, args ...any) Row {
	return v.ops.QueryRowForUpdate(ctx, table, columns, cond, args...)
}
func (v ledgerView) Update(ctx context.Context, table, setClause, cond string, args ...any) (int64, error) {
	return v.ops.Update(ctx, table, setClause, cond, args...)
}
func (v ledgerView) Delete(ctx context.Context, table, cond string, args ...any) (int64, error) {
	return v.ops.Delete(ctx, table, cond, args...)
}

type ValidatedShareContext interface {
	SelectorFingerprint() string
	validatedShareContextSeal()
}

type ShareTransactionCapability interface {
	Context() ValidatedShareContext
	shareTransactionCapabilitySeal()
}

type TransactionRunner[T any] interface {
	Run(context.Context, ShareTransactionCapability, func(LedgerTxView, T) error) error
}

type validatedContext struct{ fingerprint string }
type capability struct{ context ValidatedShareContext }

func NewValidatedShareContext(fingerprint string) ValidatedShareContext {
	return validatedContext{fingerprint: fingerprint}
}

func NewShareTransactionCapability(context ValidatedShareContext) ShareTransactionCapability {
	return capability{context: context}
}

func (validatedContext) validatedShareContextSeal()    {}
func (c validatedContext) SelectorFingerprint() string { return c.fingerprint }
func (capability) shareTransactionCapabilitySeal()     {}
func (c capability) Context() ValidatedShareContext    { return c.context }
