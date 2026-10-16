package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/samson/customer-manage-platform/backend/internal/platform/txcap"
)

// LLMLimitView binds the shared provider admission record to this physical
// transaction. The table is infrastructure configuration for one credential or
// deployment: it has no account column and is never joined into an
// account-scoped business query.
func (sc TxAccountScope) LLMLimitView() txcap.LLMLimitView {
	return txcap.BindLLMLimitView(txcap.LLMLimitOps{
		Lock: func(ctx context.Context, key string) error {
			return sc.lockLLMPermit(ctx, key)
		},
		Release: func(ctx context.Context, key string) error {
			return sc.releaseLLMPermit(ctx, key)
		},
	})
}

// lockLLMPermit takes the shared admission row without touching a counter, so a
// transaction that releases a slot later can still take this row first and keep
// the global lock order.
func (sc TxAccountScope) lockLLMPermit(ctx context.Context, key string) error {
	if key == "" {
		return nil
	}
	var present int
	err := sc.scope.execRunner().QueryRow(ctx,
		`SELECT 1 FROM platform_llm_limits WHERE limit_key=$1 FOR UPDATE`, key).Scan(&present)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	return err
}

// releaseLLMPermit frees one shared slot after local transport finished. It
// never claims the provider stopped working on its side.
func (sc TxAccountScope) releaseLLMPermit(ctx context.Context, key string) error {
	if key == "" {
		return nil
	}
	_, err := sc.scope.execRunner().Exec(ctx,
		`UPDATE platform_llm_limits SET active_count=GREATEST(active_count-1,0), revision=revision+1,
		 updated_at=clock_timestamp() WHERE limit_key=$1`, key)
	return err
}
