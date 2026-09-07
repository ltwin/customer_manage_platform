package store

import (
	"context"
	"errors"
	"fmt"
)

var ErrLegacyReadOnly = errors.New("legacy_read_only")

// WithLegacyPlanningWrite rechecks the capability at every transaction boundary.
// Read-side maintenance and order-to-legacy reconciliation use an unmarked scope.
func (sc AccountScope) WithLegacyPlanningWrite() AccountScope {
	sc.legacyPlanningWrite = true
	return sc
}

// LockCreativeWrite serializes capability transitions and participating business
// transactions across processes. It must precede all business row locks.
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

func (sc TxAccountScope) RequireLegacyPlanningWrite(ctx context.Context) error {
	if err := sc.LockCreativeWrite(ctx); err != nil {
		return err
	}
	var state string
	err := sc.QueryRow(ctx, "creative_pilot_accounts", "state", "").Scan(&state)
	if errors.Is(err, ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if state != "legacy_write" {
		return ErrLegacyReadOnly
	}
	return nil
}
