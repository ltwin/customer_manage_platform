package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/samson/customer-manage-platform/backend/internal/platform/txcap"
)

// LLMLimitView binds the shared provider admission record to this physical
// transaction. The table is infrastructure configuration for one credential or
// deployment: it has no account column and is never joined into an
// account-scoped business query.
func (sc TxAccountScope) LLMLimitView() txcap.LLMLimitView {
	return txcap.BindLLMLimitView(txcap.LLMLimitOps{
		Acquire: func(ctx context.Context, key string, capacity, rateLimit, windowSeconds int, now time.Time) (bool, error) {
			return sc.acquireLLMPermit(ctx, key, capacity, rateLimit, windowSeconds, now)
		},
		Lock: func(ctx context.Context, key string) error {
			return sc.lockLLMPermit(ctx, key)
		},
		Release: func(ctx context.Context, key string) error {
			return sc.releaseLLMPermit(ctx, key)
		},
	})
}

func (sc TxAccountScope) acquireLLMPermit(
	ctx context.Context,
	key string,
	capacity, rateLimit, windowSeconds int,
	now time.Time,
) (bool, error) {
	runner := sc.scope.execRunner()
	// The catalog is the source of truth for the shape of the limit, so a
	// deployment config change takes effect instead of being silently ignored;
	// the live counters are never reset by it.
	// Shrinking the configured capacity never pushes it below the slots that are
	// in flight: that would violate the table's own capacity check and turn a
	// routine config change into a hard stop for every dispatch on this key.
	// The reduction takes effect as the live count falls back on its own.
	if _, err := runner.Exec(ctx,
		`INSERT INTO platform_llm_limits (limit_key, capacity, rate_limit, window_seconds, window_start, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$5)
		 ON CONFLICT (limit_key) DO UPDATE SET
		   capacity=GREATEST(EXCLUDED.capacity, platform_llm_limits.active_count),
		   rate_limit=EXCLUDED.rate_limit,
		   window_seconds=EXCLUDED.window_seconds, revision=platform_llm_limits.revision+1,
		   updated_at=EXCLUDED.updated_at
		 WHERE platform_llm_limits.capacity<>GREATEST(EXCLUDED.capacity, platform_llm_limits.active_count)
		    OR platform_llm_limits.rate_limit<>EXCLUDED.rate_limit
		    OR platform_llm_limits.window_seconds<>EXCLUDED.window_seconds`,
		key, capacity, rateLimit, windowSeconds, now.UTC()); err != nil {
		return false, err
	}

	var (
		activeCount int
		windowCount int
		windowStart time.Time
		limit       int
		rate        int
		window      int
	)
	err := runner.QueryRow(ctx,
		`SELECT capacity, active_count, rate_limit, window_seconds, window_start, window_count
		 FROM platform_llm_limits WHERE limit_key=$1 FOR UPDATE`, key).
		Scan(&limit, &activeCount, &rate, &window, &windowStart, &windowCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if now.UTC().Sub(windowStart) >= time.Duration(window)*time.Second {
		windowStart, windowCount = now.UTC(), 0
	}
	// Every worker shares this record, so no process can hand itself a private
	// allowance for the same provider credential.
	if activeCount >= limit || windowCount >= rate {
		return false, nil
	}
	_, err = runner.Exec(ctx,
		`UPDATE platform_llm_limits
		 SET active_count=active_count+1, window_start=$2, window_count=$3, revision=revision+1, updated_at=$4
		 WHERE limit_key=$1`, key, windowStart, windowCount+1, now.UTC())
	return err == nil, err
}

// lockLLMPermit takes the shared admission row without touching a counter, so a
// transaction that releases a slot later can still take this row first and keep
// the global lock order.
func (sc TxAccountScope) lockLLMPermit(ctx context.Context, key string) error {
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
	_, err := sc.scope.execRunner().Exec(ctx,
		`UPDATE platform_llm_limits SET active_count=GREATEST(active_count-1,0), revision=revision+1,
		 updated_at=clock_timestamp() WHERE limit_key=$1`, key)
	return err
}
