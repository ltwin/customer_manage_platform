package store

import (
	"context"
	"fmt"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
)

// 编译期校验：Store 同时满足 seed 与认证的仓储面。
var (
	_ auth.AccountStore = (*Store)(nil)
)

// FirstAccount 返回唯一账号的认证信息（首版单账号；按创建时间取最早一条）。
func (s *Store) FirstAccount(ctx context.Context) (id, passwordHash string, err error) {
	err = s.pool.QueryRow(ctx,
		`SELECT id, password_hash FROM accounts ORDER BY created_at ASC LIMIT 1`).
		Scan(&id, &passwordHash)
	if err != nil {
		return "", "", fmt.Errorf("first account: %w", err)
	}
	return id, passwordHash, nil
}

// AccountByID 读取账号信息；不选取 password_hash（/me 永不返回它）。
func (s *Store) AccountByID(ctx context.Context, id string) (auth.Account, error) {
	var acct auth.Account
	err := s.pool.QueryRow(ctx,
		`SELECT id, created_at FROM accounts WHERE id = $1`, id).
		Scan(&acct.ID, &acct.CreatedAt)
	if err != nil {
		return auth.Account{}, fmt.Errorf("account by id: %w", err)
	}
	return acct, nil
}
