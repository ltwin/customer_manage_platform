package txcap

import (
	"context"
)

// LLMLimitOps contains only cleanup operations for historical supplier slots. The
// store binds to one physical transaction. The underlying record is
// infrastructure configuration for one credential or deployment, so it carries
// no account column and never joins an account-scoped business query.
type LLMLimitOps struct {
	Lock    func(ctx context.Context, key string) error
	Release func(ctx context.Context, key string) error
}

// LLMLimitView is a sealed capability; only the store can produce one.
type LLMLimitView interface {
	// Lock takes the shared admission row first, without changing a counter, so
	// a transaction that later releases a slot still follows the global lock
	// order instead of taking this row after an account-scoped one.
	Lock(ctx context.Context, key string) error
	Release(ctx context.Context, key string) error
	llmLimitViewSeal()
}

type llmLimitView struct{ ops LLMLimitOps }

// BindLLMLimitView is the store's entry point for the sealed capability.
func BindLLMLimitView(ops LLMLimitOps) LLMLimitView { return llmLimitView{ops: ops} }

func (llmLimitView) llmLimitViewSeal() {}

func (v llmLimitView) Lock(ctx context.Context, key string) error { return v.ops.Lock(ctx, key) }

func (v llmLimitView) Release(ctx context.Context, key string) error { return v.ops.Release(ctx, key) }
