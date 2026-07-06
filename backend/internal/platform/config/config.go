// Package config 从环境变量加载进程配置（12-factor，design D6）：
// 仓库只提交 .env.example，真实值经环境注入，永不入库、不入 git（硬规则 4）。
package config

import (
	"errors"
	"os"
)

// Config 是进程全部可配置项；key 清单与 .env.example 保持一致。
type Config struct {
	DatabaseURL       string // DATABASE_URL（必填）
	AuthTokenSecret   string // AUTH_TOKEN_SECRET（必填，JWT HS256 密钥）
	SeedAdminPassword string // SEED_ADMIN_PASSWORD（仅 accounts 为空的首次启动需要）
	HTTPAddr          string // HTTP_ADDR（默认 :8080）
}

// 启动期 fail-fast 错误：必填项缺失时进程不得继续。
var (
	ErrDatabaseURLMissing     = errors.New("DATABASE_URL 未设置")
	ErrAuthTokenSecretMissing = errors.New("AUTH_TOKEN_SECRET 未设置：JWT 签名密钥必须经环境变量注入")
)

// Load 读取环境变量并校验必填项。
func Load() (Config, error) {
	cfg := Config{
		DatabaseURL:       os.Getenv("DATABASE_URL"),
		AuthTokenSecret:   os.Getenv("AUTH_TOKEN_SECRET"),
		SeedAdminPassword: os.Getenv("SEED_ADMIN_PASSWORD"),
		HTTPAddr:          os.Getenv("HTTP_ADDR"),
	}
	if cfg.DatabaseURL == "" {
		return Config{}, ErrDatabaseURLMissing
	}
	if cfg.AuthTokenSecret == "" {
		return Config{}, ErrAuthTokenSecretMissing
	}
	if cfg.HTTPAddr == "" {
		cfg.HTTPAddr = ":8080"
	}
	return cfg, nil
}
