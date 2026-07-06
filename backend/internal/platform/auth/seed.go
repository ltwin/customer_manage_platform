package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// ErrSeedPasswordMissing：accounts 为空且未提供 seed 密码时的启动期 fail-fast 错误（design D3）。
var ErrSeedPasswordMissing = errors.New(
	"accounts 为空且 SEED_ADMIN_PASSWORD 缺失或为空：首次启动必须经该环境变量提供默认账号密码")

// AccountStore 是 seed 所需的最小仓储面（接口定义在消费方，Uber guide）。
type AccountStore interface {
	AccountCount(ctx context.Context) (int64, error)
	CreateAccount(ctx context.Context, id, passwordHash string) error
}

// EnsureDefaultAccount 保证系统存在默认账号：
// accounts 非空 → 不动作（幂等，重复启动零新增）；
// accounts 为空 → 用 seedPassword 创建；seedPassword 为空 → ErrSeedPasswordMissing。
func EnsureDefaultAccount(ctx context.Context, s AccountStore, seedPassword string) (created bool, err error) {
	n, err := s.AccountCount(ctx)
	if err != nil {
		return false, fmt.Errorf("ensure default account: %w", err)
	}
	if n > 0 {
		return false, nil
	}
	if seedPassword == "" {
		return false, ErrSeedPasswordMissing
	}
	hash, err := HashPassword(seedPassword)
	if err != nil {
		return false, fmt.Errorf("ensure default account: %w", err)
	}
	if err := s.CreateAccount(ctx, uuid.NewString(), hash); err != nil {
		return false, fmt.Errorf("ensure default account: %w", err)
	}
	return true, nil
}
