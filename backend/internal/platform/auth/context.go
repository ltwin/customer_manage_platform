package auth

import "context"

// AccountContext 是 auth 中间件校验通过后携带的当前账号标识；
// handler 及以下从 context 取，永不从客户端参数取（ADR-001 / design 2.1）。
type AccountContext struct {
	AccountID string
}

type accountContextKey struct{}

// WithAccountContext 把账号上下文注入请求 context（仅 auth 中间件调用）。
func WithAccountContext(ctx context.Context, ac AccountContext) context.Context {
	return context.WithValue(ctx, accountContextKey{}, ac)
}

// AccountContextFrom 取出账号上下文；未经过 auth 中间件的路径返回 false。
func AccountContextFrom(ctx context.Context) (AccountContext, bool) {
	ac, ok := ctx.Value(accountContextKey{}).(AccountContext)
	return ac, ok
}
