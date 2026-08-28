// Package config 从环境变量加载进程配置（12-factor，design D6）：
// 仓库只提交 .env.example，真实值经环境注入，永不入库、不入 git（硬规则 4）。
package config

import (
	"bufio"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// Config 是进程全部可配置项；key 清单与 .env.example 保持一致。
type Config struct {
	DatabaseURL                   string // DATABASE_URL（必填）
	AuthTokenSecret               string // AUTH_TOKEN_SECRET（必填，JWT HS256 密钥）
	AuthTokenIssuer               string // AUTH_TOKEN_ISSUER（access JWT 固定 issuer）
	PublicBaseURL                 string // PUBLIC_BASE_URL（action URL 与 Origin 唯一权威）
	AuthPublicRegistrationEnabled bool   // AUTH_PUBLIC_REGISTRATION_ENABLED（默认 false）
	AuthMailDriver                string // AUTH_MAIL_DRIVER（production 必须是真实 adapter）
	AuthMailFrom                  string // AUTH_MAIL_FROM（真实 adapter 的 sender）
	ResendAPIKey                  string // RESEND_API_KEY（仅 AUTH_MAIL_DRIVER=resend 时必填）
	TrustedProxyCIDRs             string // TRUSTED_PROXY_CIDRS（预留给统一 source 解析）
	TrustedProxyPrefixes          []netip.Prefix
	HTTPAddr                      string // HTTP_ADDR（默认 :8080）
	AvatarStorageDriver           string // AVATAR_STORAGE_DRIVER（local|oss，统管头像与策划素材对象存储）
	AvatarLocalRoot               string // AVATAR_LOCAL_ROOT（local 对象根目录）
	AvatarLocalRequireMount       bool   // AVATAR_LOCAL_REQUIRE_MOUNT（production 必须为 true）
	PlanningMediaLocalRoot        string // PLANNING_MEDIA_LOCAL_ROOT（可选；缺省沿用 AvatarLocalRoot 同级 planning-media）
	OSSRegion                     string // OSS_REGION（oss driver 必填）
	OSSEndpoint                   string // OSS_ENDPOINT（可选；ECS 同地域建议内网 endpoint）
	OSSUseCName                   bool   // OSS_USE_CNAME（endpoint 是 bucket 自定义 CNAME 时为 true）
	OSSBucket                     string // OSS_BUCKET（oss driver 必填；头像与策划素材共用，键前缀天然分区）
	TelegramBotToken              string // TELEGRAM_BOT_TOKEN（可选，仅服务端环境）
	TelegramBotUsername           string // TELEGRAM_BOT_USERNAME（可选，不含 @）
}

// AccountAuthConfig 是 server/accountctl 共享的认证 composition 配置。
// 它不触碰头像存储，因此 accountctl dry-run 不会产生文件系统写入。
type AccountAuthConfig struct {
	DatabaseURL                   string
	AuthTokenSecret               string
	AuthTokenIssuer               string
	PublicBaseURL                 string
	AuthPublicRegistrationEnabled bool
	AuthMailDriver                string
	AuthMailFrom                  string
	ResendAPIKey                  string
	TrustedProxyCIDRs             string
	TrustedProxyPrefixes          []netip.Prefix
}

// 对象存储驱动取值（AVATAR_STORAGE_DRIVER）。
const (
	StorageDriverLocal = "local"
	StorageDriverOSS   = "oss"
)

// 启动期 fail-fast 错误：必填项缺失时进程不得继续。
var (
	ErrDatabaseURLMissing             = errors.New("DATABASE_URL 未设置")
	ErrAuthTokenSecretMissing         = errors.New("AUTH_TOKEN_SECRET 未设置：JWT 签名密钥必须经环境变量注入")
	ErrPublicBaseURLMissing           = errors.New("PUBLIC_BASE_URL 未设置")
	ErrPublicBaseURLInvalid           = errors.New("PUBLIC_BASE_URL 必须是 canonical HTTPS origin；仅 localhost 开发允许 HTTP")
	ErrAuthPublicRegistrationInvalid  = errors.New("AUTH_PUBLIC_REGISTRATION_ENABLED 必须是 true 或 false")
	ErrAuthMailDriverInvalid          = errors.New("AUTH_MAIL_DRIVER 非法或不适用于当前 PUBLIC_BASE_URL")
	ErrResendAPIKeyMissing            = errors.New("RESEND_API_KEY 未设置")
	ErrTrustedProxyCIDRsInvalid       = errors.New("TRUSTED_PROXY_CIDRS 必须是逗号分隔的 canonical CIDR")
	ErrAvatarStorageDriverInvalid     = errors.New("AVATAR_STORAGE_DRIVER 非法：仅支持 local 或 oss")
	ErrOSSRegionMissing               = errors.New("OSS_REGION 未设置：oss driver 必填")
	ErrOSSBucketMissing               = errors.New("OSS_BUCKET 未设置：oss driver 必填")
	ErrOSSUseCNameInvalid             = errors.New("OSS_USE_CNAME 必须是 true 或 false")
	ErrOSSCNameEndpointMissing        = errors.New("OSS_ENDPOINT 未设置：OSS_USE_CNAME=true 时必填")
	ErrAvatarLocalRootMissing         = errors.New("AVATAR_LOCAL_ROOT 未设置")
	ErrAvatarLocalRequireMountInvalid = errors.New("AVATAR_LOCAL_REQUIRE_MOUNT 必须是 true 或 false")
	ErrAvatarLocalRootNotMount        = errors.New("AVATAR_LOCAL_ROOT 不是可验证的独立挂载点")
	ErrAvatarLocalRootUnavailable     = errors.New("AVATAR_LOCAL_ROOT 不可用")
)

// Load 读取环境变量并校验必填项。
func Load() (Config, error) {
	authConfig, err := LoadAccountAuth()
	if err != nil {
		return Config{}, err
	}
	cfg := Config{
		DatabaseURL:                   authConfig.DatabaseURL,
		AuthTokenSecret:               authConfig.AuthTokenSecret,
		AuthTokenIssuer:               authConfig.AuthTokenIssuer,
		PublicBaseURL:                 authConfig.PublicBaseURL,
		AuthPublicRegistrationEnabled: authConfig.AuthPublicRegistrationEnabled,
		AuthMailDriver:                authConfig.AuthMailDriver,
		AuthMailFrom:                  authConfig.AuthMailFrom,
		ResendAPIKey:                  authConfig.ResendAPIKey,
		TrustedProxyCIDRs:             authConfig.TrustedProxyCIDRs,
		TrustedProxyPrefixes:          authConfig.TrustedProxyPrefixes,
		HTTPAddr:                      os.Getenv("HTTP_ADDR"),
		AvatarStorageDriver:           strings.TrimSpace(os.Getenv("AVATAR_STORAGE_DRIVER")),
		AvatarLocalRoot:               strings.TrimSpace(os.Getenv("AVATAR_LOCAL_ROOT")),
		TelegramBotToken:              os.Getenv("TELEGRAM_BOT_TOKEN"),
		TelegramBotUsername:           strings.TrimSpace(os.Getenv("TELEGRAM_BOT_USERNAME")),
	}
	if cfg.HTTPAddr == "" {
		cfg.HTTPAddr = ":8080"
	}
	cfg.PlanningMediaLocalRoot = strings.TrimSpace(os.Getenv("PLANNING_MEDIA_LOCAL_ROOT"))
	cfg.OSSRegion = strings.TrimSpace(os.Getenv("OSS_REGION"))
	cfg.OSSEndpoint = strings.TrimSpace(os.Getenv("OSS_ENDPOINT"))
	cfg.OSSBucket = strings.TrimSpace(os.Getenv("OSS_BUCKET"))
	if cfg.AvatarStorageDriver == "" {
		cfg.AvatarStorageDriver = StorageDriverLocal
	}
	switch cfg.AvatarStorageDriver {
	case StorageDriverLocal:
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
		if cfg.PlanningMediaLocalRoot == "" {
			cfg.PlanningMediaLocalRoot = filepath.Join(filepath.Dir(cfg.AvatarLocalRoot), "planning-media")
		} else {
			absRoot, err := filepath.Abs(cfg.PlanningMediaLocalRoot)
			if err != nil {
				return Config{}, fmt.Errorf("%w: resolve PLANNING_MEDIA_LOCAL_ROOT: %w", ErrAvatarLocalRootUnavailable, err)
			}
			cfg.PlanningMediaLocalRoot = absRoot
		}
	case StorageDriverOSS:
		if cfg.OSSRegion == "" {
			return Config{}, ErrOSSRegionMissing
		}
		if cfg.OSSBucket == "" {
			return Config{}, ErrOSSBucketMissing
		}
		useCName, err := parseOptionalBool("OSS_USE_CNAME", os.Getenv("OSS_USE_CNAME"), ErrOSSUseCNameInvalid)
		if err != nil {
			return Config{}, err
		}
		cfg.OSSUseCName = useCName
		if useCName && cfg.OSSEndpoint == "" {
			return Config{}, ErrOSSCNameEndpointMissing
		}
	default:
		return Config{}, ErrAvatarStorageDriverInvalid
	}
	return cfg, nil
}

// LoadAccountAuth 读取并验证 account-auth 的共享运行时配置，不执行迁移或文件探针。
func LoadAccountAuth() (AccountAuthConfig, error) {
	cfg := AccountAuthConfig{
		DatabaseURL:       os.Getenv("DATABASE_URL"),
		AuthTokenSecret:   os.Getenv("AUTH_TOKEN_SECRET"),
		AuthTokenIssuer:   strings.TrimSpace(os.Getenv("AUTH_TOKEN_ISSUER")),
		PublicBaseURL:     strings.TrimSpace(os.Getenv("PUBLIC_BASE_URL")),
		AuthMailDriver:    strings.TrimSpace(os.Getenv("AUTH_MAIL_DRIVER")),
		AuthMailFrom:      strings.TrimSpace(os.Getenv("AUTH_MAIL_FROM")),
		ResendAPIKey:      os.Getenv("RESEND_API_KEY"),
		TrustedProxyCIDRs: strings.TrimSpace(os.Getenv("TRUSTED_PROXY_CIDRS")),
	}
	if cfg.DatabaseURL == "" {
		return AccountAuthConfig{}, ErrDatabaseURLMissing
	}
	if cfg.AuthTokenSecret == "" {
		return AccountAuthConfig{}, ErrAuthTokenSecretMissing
	}
	if cfg.AuthTokenIssuer == "" {
		cfg.AuthTokenIssuer = "photographer-crm"
	}
	if cfg.PublicBaseURL == "" {
		return AccountAuthConfig{}, ErrPublicBaseURLMissing
	}
	publicBaseURL, err := canonicalPublicBaseURL(cfg.PublicBaseURL)
	if err != nil {
		return AccountAuthConfig{}, err
	}
	cfg.PublicBaseURL = publicBaseURL
	registrationEnabled, err := parseAuthPublicRegistrationEnabled(os.Getenv("AUTH_PUBLIC_REGISTRATION_ENABLED"))
	if err != nil {
		return AccountAuthConfig{}, err
	}
	cfg.AuthPublicRegistrationEnabled = registrationEnabled
	trustedProxyPrefixes, err := parseTrustedProxyCIDRs(cfg.TrustedProxyCIDRs)
	if err != nil {
		return AccountAuthConfig{}, err
	}
	cfg.TrustedProxyPrefixes = trustedProxyPrefixes
	if cfg.AuthMailDriver == "" {
		cfg.AuthMailDriver = "unavailable"
	}
	switch cfg.AuthMailDriver {
	case "unavailable":
	case "sink":
		parsed, parseErr := url.Parse(cfg.PublicBaseURL)
		if parseErr != nil || !isLoopbackHostname(parsed.Hostname()) {
			return AccountAuthConfig{}, ErrAuthMailDriverInvalid
		}
	case "resend":
		if strings.TrimSpace(cfg.ResendAPIKey) == "" {
			return AccountAuthConfig{}, ErrResendAPIKeyMissing
		}
		if cfg.AuthMailFrom == "" {
			cfg.AuthMailFrom = "Photographer CRM <onboarding@resend.dev>"
		}
	default:
		return AccountAuthConfig{}, ErrAuthMailDriverInvalid
	}
	return cfg, nil
}

func parseTrustedProxyCIDRs(raw string) ([]netip.Prefix, error) {
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	result := make([]netip.Prefix, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, ErrTrustedProxyCIDRsInvalid
		}
		prefix, err := netip.ParsePrefix(part)
		if err != nil || prefix != prefix.Masked() {
			return nil, ErrTrustedProxyCIDRsInvalid
		}
		result = append(result, prefix)
	}
	return result, nil
}

func parseAuthPublicRegistrationEnabled(raw string) (bool, error) {
	if raw == "" {
		return false, nil
	}
	if raw != "true" && raw != "false" {
		return false, ErrAuthPublicRegistrationInvalid
	}
	return raw == "true", nil
}

func canonicalPublicBaseURL(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.User != nil ||
		(parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", ErrPublicBaseURLInvalid
	}
	scheme := strings.ToLower(parsed.Scheme)
	host := strings.ToLower(parsed.Host)
	if (scheme == "https" && parsed.Port() == "443") || (scheme == "http" && parsed.Port() == "80") {
		return "", ErrPublicBaseURLInvalid
	}
	if scheme != "https" {
		hostname := strings.ToLower(parsed.Hostname())
		if scheme != "http" || !isLoopbackHostname(hostname) {
			return "", ErrPublicBaseURLInvalid
		}
	}
	return scheme + "://" + host, nil
}

func isLoopbackHostname(hostname string) bool {
	return hostname == "localhost" || hostname == "127.0.0.1" || hostname == "::1"
}

// TelegramStatus 返回 Telegram 外部能力是否可启动。配置问题不阻断主应用启动。
func (c Config) TelegramStatus() (bool, error) {
	tokenSet := c.TelegramBotToken != ""
	usernameSet := c.TelegramBotUsername != ""
	if !tokenSet && !usernameSet {
		return false, nil
	}
	if !tokenSet || !usernameSet {
		return false, errors.New("telegram 配置不完整")
	}
	if !validTelegramBotUsername(c.TelegramBotUsername) {
		return false, errors.New("TELEGRAM_BOT_USERNAME 非法")
	}
	return true, nil
}

func validTelegramBotUsername(value string) bool {
	if len(value) < 5 || len(value) > 32 || !strings.HasSuffix(strings.ToLower(value), "bot") {
		return false
	}
	for _, r := range value {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' {
			return false
		}
	}
	return true
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

func parseOptionalBool(name, raw string, invalid error) (bool, error) {
	if raw == "" {
		return false, nil
	}
	if raw != "true" && raw != "false" {
		return false, fmt.Errorf("%w: %s", invalid, name)
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%w: %s", invalid, name)
	}
	return value, nil
}

func prepareAvatarLocalRoot(root string, requireMount bool) (string, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("%w: resolve: %w", ErrAvatarLocalRootUnavailable, err)
	}
	if err := os.MkdirAll(absRoot, 0o750); err != nil {
		return "", fmt.Errorf("%w: create: %w", ErrAvatarLocalRootUnavailable, err)
	}
	realRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return "", fmt.Errorf("%w: resolve real path: %w", ErrAvatarLocalRootUnavailable, err)
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
		return "", fmt.Errorf("%w: writable attestation: %w", ErrAvatarLocalRootUnavailable, err)
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
