// Package config 从环境变量加载进程配置（12-factor，design D6）：
// 仓库只提交 .env.example，真实值经环境注入，永不入库、不入 git（硬规则 4）。
package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// Config 是进程全部可配置项；key 清单与 .env.example 保持一致。
type Config struct {
	DatabaseURL             string // DATABASE_URL（必填）
	AuthTokenSecret         string // AUTH_TOKEN_SECRET（必填，JWT HS256 密钥）
	SeedAdminPassword       string // SEED_ADMIN_PASSWORD（仅 accounts 为空的首次启动需要）
	HTTPAddr                string // HTTP_ADDR（默认 :8080）
	AvatarStorageDriver     string // AVATAR_STORAGE_DRIVER（首版仅 local）
	AvatarLocalRoot         string // AVATAR_LOCAL_ROOT（local 对象根目录）
	AvatarLocalRequireMount bool   // AVATAR_LOCAL_REQUIRE_MOUNT（production 必须为 true）
}

// 启动期 fail-fast 错误：必填项缺失时进程不得继续。
var (
	ErrDatabaseURLMissing             = errors.New("DATABASE_URL 未设置")
	ErrAuthTokenSecretMissing         = errors.New("AUTH_TOKEN_SECRET 未设置：JWT 签名密钥必须经环境变量注入")
	ErrAvatarStorageDriverInvalid     = errors.New("AVATAR_STORAGE_DRIVER 非法：首版仅支持 local")
	ErrAvatarLocalRootMissing         = errors.New("AVATAR_LOCAL_ROOT 未设置")
	ErrAvatarLocalRequireMountInvalid = errors.New("AVATAR_LOCAL_REQUIRE_MOUNT 必须是 true 或 false")
	ErrAvatarLocalRootNotMount        = errors.New("AVATAR_LOCAL_ROOT 不是可验证的独立挂载点")
)

// Load 读取环境变量并校验必填项。
func Load() (Config, error) {
	cfg := Config{
		DatabaseURL:         os.Getenv("DATABASE_URL"),
		AuthTokenSecret:     os.Getenv("AUTH_TOKEN_SECRET"),
		SeedAdminPassword:   os.Getenv("SEED_ADMIN_PASSWORD"),
		HTTPAddr:            os.Getenv("HTTP_ADDR"),
		AvatarStorageDriver: strings.TrimSpace(os.Getenv("AVATAR_STORAGE_DRIVER")),
		AvatarLocalRoot:     strings.TrimSpace(os.Getenv("AVATAR_LOCAL_ROOT")),
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
	if cfg.AvatarStorageDriver == "" {
		cfg.AvatarStorageDriver = "local"
	}
	if cfg.AvatarStorageDriver != "local" {
		return Config{}, ErrAvatarStorageDriverInvalid
	}
	if cfg.AvatarLocalRoot == "" {
		return Config{}, ErrAvatarLocalRootMissing
	}
	requireMount, err := parseRequiredBool("AVATAR_LOCAL_REQUIRE_MOUNT", os.Getenv("AVATAR_LOCAL_REQUIRE_MOUNT"))
	if err != nil {
		return Config{}, err
	}
	cfg.AvatarLocalRequireMount = requireMount
	root, err := prepareAvatarLocalRoot(cfg.AvatarLocalRoot, requireMount)
	if err != nil {
		return Config{}, err
	}
	cfg.AvatarLocalRoot = root
	return cfg, nil
}

func parseRequiredBool(name, raw string) (bool, error) {
	if raw == "" {
		return false, nil
	}
	if raw != "true" && raw != "false" {
		return false, fmt.Errorf("%w: %s", ErrAvatarLocalRequireMountInvalid, name)
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%w: %s", ErrAvatarLocalRequireMountInvalid, name)
	}
	return value, nil
}

func prepareAvatarLocalRoot(root string, requireMount bool) (string, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve AVATAR_LOCAL_ROOT: %w", err)
	}
	if err := os.MkdirAll(absRoot, 0o750); err != nil {
		return "", fmt.Errorf("create AVATAR_LOCAL_ROOT: %w", err)
	}
	realRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return "", fmt.Errorf("resolve real AVATAR_LOCAL_ROOT: %w", err)
	}
	if requireMount {
		mounted, err := isLinuxMountPoint(realRoot)
		if err != nil {
			return "", err
		}
		if !mounted {
			return "", ErrAvatarLocalRootNotMount
		}
	}
	if err := verifyAvatarLocalRootWritable(realRoot); err != nil {
		return "", err
	}
	return realRoot, nil
}

func verifyAvatarLocalRootWritable(root string) error {
	probe, err := os.CreateTemp(root, ".avatar-write-probe-")
	if err != nil {
		return fmt.Errorf("verify AVATAR_LOCAL_ROOT writable: %w", err)
	}
	name := probe.Name()
	defer func() { _ = os.Remove(name) }()
	if err := probe.Chmod(0o600); err != nil {
		_ = probe.Close()
		return fmt.Errorf("verify AVATAR_LOCAL_ROOT permissions: %w", err)
	}
	if err := probe.Sync(); err != nil {
		_ = probe.Close()
		return fmt.Errorf("verify AVATAR_LOCAL_ROOT durable write: %w", err)
	}
	if err := probe.Close(); err != nil {
		return fmt.Errorf("verify AVATAR_LOCAL_ROOT close: %w", err)
	}
	if err := os.Remove(name); err != nil {
		return fmt.Errorf("verify AVATAR_LOCAL_ROOT cleanup: %w", err)
	}
	dir, err := os.Open(root)
	if err != nil {
		return fmt.Errorf("verify AVATAR_LOCAL_ROOT directory sync: %w", err)
	}
	defer func() { _ = dir.Close() }()
	if err := dir.Sync(); err != nil {
		return fmt.Errorf("verify AVATAR_LOCAL_ROOT directory sync: %w", err)
	}
	return nil
}

func isLinuxMountPoint(root string) (bool, error) {
	if runtime.GOOS != "linux" {
		return false, ErrAvatarLocalRootNotMount
	}
	file, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return false, fmt.Errorf("%w: read mount table: %v", ErrAvatarLocalRootNotMount, err)
	}
	defer func() { _ = file.Close() }()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) > 4 && unescapeMountInfoPath(fields[4]) == root {
			return true, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return false, fmt.Errorf("%w: scan mount table: %v", ErrAvatarLocalRootNotMount, err)
	}
	return false, nil
}

func unescapeMountInfoPath(value string) string {
	replacer := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)
	return replacer.Replace(value)
}
