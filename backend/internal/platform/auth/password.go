// Package auth 承载账号认证领域逻辑：密码哈希、默认账号 seed、JWT 与账号上下文。
// 本包不得 import 路由框架（ADR-003，depguard 断言）。
package auth

import (
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

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
