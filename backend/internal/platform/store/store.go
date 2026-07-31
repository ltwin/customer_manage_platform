// Package store 是数据库访问基座：连接管理、schema 迁移与账号隔离查询句柄
// （AccountScope）都在这里，pgx 仅允许本包 import（ADR-001，depguard 断言）。
package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Store 持有连接池，是所有仓储访问的入口。
type Store struct {
	pool *pgxpool.Pool
}

type ScopedAccount struct {
	AccountID string
	Scope     AccountScope
}

// Open 建立连接池并 ping 确认可达。
func Open(ctx context.Context, databaseURL string) (*Store, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("create pgx pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return &Store{pool: pool}, nil
}

// Close 释放连接池。
func (s *Store) Close() {
	s.pool.Close()
}

// Ping 供健康检查探测数据库可达性。
func (s *Store) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

// AccountCount 返回 accounts 表行数（seed 幂等判定用）。
func (s *Store) AccountCount(ctx context.Context) (int64, error) {
	var n int64
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM accounts`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count accounts: %w", err)
	}
	return n, nil
}

// CreateAccount 写入一个账号。
func (s *Store) CreateAccount(ctx context.Context, id, passwordHash string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin insert account: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, accountAdmissionLockSQL); err != nil {
		return fmt.Errorf("lock account admission: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO accounts (id, password_hash) VALUES ($1, $2)`, id, passwordHash); err != nil {
		return fmt.Errorf("insert account: %w", err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO password_credentials (account_id, password_hash)
		VALUES ($1, $2)`, id, passwordHash)
	if err != nil {
		return fmt.Errorf("insert account credential: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit insert account: %w", err)
	}
	return nil
}

// AccountScopes 是后台维护任务唯一的账号枚举入口；账号标识来自服务端 accounts 表。
func (s *Store) AccountScopes(ctx context.Context) ([]ScopedAccount, error) {
	rows, err := s.pool.Query(ctx, `SELECT id FROM accounts WHERE status = 'active' ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list account scopes: %w", err)
	}
	defer rows.Close()
	result := make([]ScopedAccount, 0)
	for rows.Next() {
		var accountID string
		if err := rows.Scan(&accountID); err != nil {
			return nil, fmt.Errorf("scan account scope: %w", err)
		}
		result = append(result, ScopedAccount{
			AccountID: accountID,
			Scope:     AccountScope{pool: s.pool, runner: s.pool, accountID: accountID},
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list account scopes: %w", err)
	}
	return result, nil
}
