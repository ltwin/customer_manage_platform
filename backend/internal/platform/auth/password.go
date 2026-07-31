// Package auth 承载账号认证领域逻辑：密码哈希、默认账号 seed、JWT 与账号上下文。
// 本包不得 import 路由框架（ADR-003，depguard 断言）。
package auth

import (
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// 固定 dummy bcrypt 路径用于不存在邮箱，避免账号枚举的显著计算差异。
const dummyPasswordHash = "$2y$10$lNULji2xZgvMjtVuxKtdzu2hilYXd60KQGiqPajFBpzuQXY9usGSS"

// HashPassword 生成 bcrypt 派生验证子；入库的是它而非明文（硬规则 4）。
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(hash), nil
}

// VerifyPassword 校验明文密码与哈希是否匹配。
func VerifyPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func validateDummyPasswordHash() error {
	cost, err := bcrypt.Cost([]byte(dummyPasswordHash))
	if err != nil {
		return fmt.Errorf("parse dummy password hash: %w", err)
	}
	if cost != bcrypt.DefaultCost {
		return fmt.Errorf("dummy password cost %d does not match production cost %d", cost, bcrypt.DefaultCost)
	}
	return nil
}
