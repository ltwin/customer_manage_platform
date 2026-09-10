package store

import (
	"context"
	"fmt"
)

// LockCreativeWrite serializes account-scoped canvas transactions.
func (sc TxAccountScope) LockCreativeWrite(ctx context.Context) error {
	if sc.scope.accountID == "" {
		return ErrEmptyAccountScope
	}
	_, err := sc.scope.execRunner().Exec(ctx,
		"SELECT pg_advisory_xact_lock(hashtextextended('creative-write:' || $1::text, 0))", sc.scope.accountID)
	if err != nil {
		return fmt.Errorf("lock creative write: %w", err)
	}
	return nil
}
