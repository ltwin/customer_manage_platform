package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func setRequiredEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("AUTH_TOKEN_SECRET", "test-secret")
	t.Setenv("PUBLIC_BASE_URL", "https://app.example.invalid")
	t.Setenv("AVATAR_STORAGE_DRIVER", "local")
	t.Setenv("AVATAR_LOCAL_ROOT", filepath.Join(t.TempDir(), "avatars"))
	t.Setenv("AVATAR_LOCAL_REQUIRE_MOUNT", "false")
}

func TestPrepareAvatarLocalRootRejectsReadOnlyDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permission semantics required")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory write permission bits")
	}
	root := t.TempDir()
	if err := os.Chmod(root, 0o500); err != nil {
		t.Fatalf("chmod root: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o700) })
	if _, err := prepareAvatarLocalRoot(root, false); err == nil {
		t.Fatal("read-only avatar root must fail writable attestation")
	} else if !errors.Is(err, ErrAvatarLocalRootUnavailable) {
		t.Fatalf("avatar root failure must retain a stable class: %v", err)
	}
}

func TestLoadAvatarStorageDirectBinary(t *testing.T) {
	setRequiredEnvironment(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AvatarStorageDriver != "local" || cfg.AvatarLocalRoot == "" || cfg.AvatarLocalRequireMount {
		t.Fatalf("avatar config mismatch: %+v", cfg)
	}
}

func TestLoadAvatarStorageDirectBinaryDefaultsRequireMountToFalse(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("AVATAR_LOCAL_REQUIRE_MOUNT", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AvatarLocalRequireMount {
		t.Fatal("unset AVATAR_LOCAL_REQUIRE_MOUNT must default to false")
	}
}

func TestLoadAvatarStorageFailsFast(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T)
		want   error
	}{
		{name: "unknown driver", mutate: func(t *testing.T) { t.Setenv("AVATAR_STORAGE_DRIVER", "s3") }, want: ErrAvatarStorageDriverInvalid},
		{name: "oss driver missing region", mutate: func(t *testing.T) {
			t.Setenv("AVATAR_STORAGE_DRIVER", "oss")
			t.Setenv("OSS_BUCKET", "crm-objects")
		}, want: ErrOSSRegionMissing},
		{name: "oss driver missing bucket", mutate: func(t *testing.T) {
			t.Setenv("AVATAR_STORAGE_DRIVER", "oss")
			t.Setenv("OSS_REGION", "cn-hangzhou")
		}, want: ErrOSSBucketMissing},
		{name: "oss invalid cname switch", mutate: func(t *testing.T) {
			t.Setenv("AVATAR_STORAGE_DRIVER", "oss")
			t.Setenv("OSS_REGION", "cn-hangzhou")
			t.Setenv("OSS_BUCKET", "crm-objects")
			t.Setenv("OSS_USE_CNAME", "sometimes")
		}, want: ErrOSSUseCNameInvalid},
		{name: "oss cname missing endpoint", mutate: func(t *testing.T) {
			t.Setenv("AVATAR_STORAGE_DRIVER", "oss")
			t.Setenv("OSS_REGION", "cn-hangzhou")
			t.Setenv("OSS_BUCKET", "crm-objects")
			t.Setenv("OSS_USE_CNAME", "true")
		}, want: ErrOSSCNameEndpointMissing},
		{name: "missing root", mutate: func(t *testing.T) { t.Setenv("AVATAR_LOCAL_ROOT", "") }, want: ErrAvatarLocalRootMissing},
		{name: "invalid require mount", mutate: func(t *testing.T) { t.Setenv("AVATAR_LOCAL_REQUIRE_MOUNT", "sometimes") }, want: ErrAvatarLocalRequireMountInvalid},
		{name: "require mount without real mount", mutate: func(t *testing.T) { t.Setenv("AVATAR_LOCAL_REQUIRE_MOUNT", "true") }, want: ErrAvatarLocalRootNotMount},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setRequiredEnvironment(t)
			tt.mutate(t)
			_, err := Load()
			if !errors.Is(err, tt.want) {
				t.Fatalf("want %v, got %v", tt.want, err)
			}
		})
	}
}

func TestLoadOSSDriverSkipsLocalRootRequirements(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("AVATAR_STORAGE_DRIVER", "oss")
	t.Setenv("OSS_REGION", "cn-hangzhou")
	t.Setenv("OSS_BUCKET", "crm-objects")
	t.Setenv("OSS_ENDPOINT", "oss-cn-hangzhou-internal.aliyuncs.com")
	t.Setenv("OSS_USE_CNAME", "false")
	t.Setenv("AVATAR_LOCAL_ROOT", "")
	t.Setenv("AVATAR_LOCAL_REQUIRE_MOUNT", "true")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AvatarStorageDriver != StorageDriverOSS || cfg.OSSRegion != "cn-hangzhou" ||
		cfg.OSSBucket != "crm-objects" || cfg.OSSEndpoint != "oss-cn-hangzhou-internal.aliyuncs.com" {
		t.Fatalf("oss config mismatch: %+v", cfg)
	}
	if cfg.PlanningMediaLocalRoot != "" {
		t.Fatalf("oss driver 不应推导本地 planning-media 根: %q", cfg.PlanningMediaLocalRoot)
	}
}

func TestLoadPlanningMediaLocalRoot(t *testing.T) {
	t.Run("缺省沿用 AvatarLocalRoot 同级推导", func(t *testing.T) {
		setRequiredEnvironment(t)
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		want := filepath.Join(filepath.Dir(cfg.AvatarLocalRoot), "planning-media")
		if cfg.PlanningMediaLocalRoot != want {
			t.Fatalf("planning root mismatch: want %q got %q", want, cfg.PlanningMediaLocalRoot)
		}
	})
	t.Run("显式配置优先", func(t *testing.T) {
		setRequiredEnvironment(t)
		explicit := filepath.Join(t.TempDir(), "media-extra")
		t.Setenv("PLANNING_MEDIA_LOCAL_ROOT", explicit)
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.PlanningMediaLocalRoot != explicit {
			t.Fatalf("planning root mismatch: want %q got %q", explicit, cfg.PlanningMediaLocalRoot)
		}
	})
}

func TestLoadTelegramConfigurationIsOptional(t *testing.T) {
	tests := []struct {
		name       string
		token      string
		username   string
		wantActive bool
		wantIssue  bool
	}{
		{name: "disabled when both values are absent"},
		{name: "enabled when both values are valid", token: "test-token", username: "studio_digest_bot", wantActive: true},
		{name: "incomplete token only", token: "test-token", wantIssue: true},
		{name: "incomplete username only", username: "studio_digest_bot", wantIssue: true},
		{name: "invalid username", token: "test-token", username: "not a bot", wantIssue: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setRequiredEnvironment(t)
			t.Setenv("TELEGRAM_BOT_TOKEN", tt.token)
			t.Setenv("TELEGRAM_BOT_USERNAME", tt.username)

			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load must keep the main application available: %v", err)
			}
			active, issue := cfg.TelegramStatus()
			if active != tt.wantActive {
				t.Fatalf("active: want %v, got %v", tt.wantActive, active)
			}
			if (issue != nil) != tt.wantIssue {
				t.Fatalf("issue presence: want %v, got %v", tt.wantIssue, issue)
			}
			if cfg.TelegramBotToken != tt.token || cfg.TelegramBotUsername != tt.username {
				t.Fatal("Telegram values were not loaded verbatim from the environment")
			}
		})
	}
}

func TestLoadAuthPublicConfiguration(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("PUBLIC_BASE_URL", "HTTPS://APP.Example.Invalid/")
	t.Setenv("AUTH_TOKEN_ISSUER", "crm-test-issuer")
	t.Setenv("AUTH_PUBLIC_REGISTRATION_ENABLED", "true")
	t.Setenv("AUTH_MAIL_DRIVER", "unavailable")
	t.Setenv("TRUSTED_PROXY_CIDRS", "192.0.2.0/24")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load auth configuration: %v", err)
	}
	if cfg.PublicBaseURL != "https://app.example.invalid" || !cfg.AuthPublicRegistrationEnabled ||
		cfg.AuthTokenIssuer != "crm-test-issuer" || cfg.AuthMailDriver != "unavailable" ||
		cfg.TrustedProxyCIDRs != "192.0.2.0/24" {
		t.Fatalf("auth public configuration mismatch: base=%q enabled=%t issuer=%q mail=%q proxies=%q",
			cfg.PublicBaseURL, cfg.AuthPublicRegistrationEnabled, cfg.AuthTokenIssuer,
			cfg.AuthMailDriver, cfg.TrustedProxyCIDRs)
	}
}

func TestLoadAuthTrustedProxyCIDRsAreValidatedAndParsed(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("TRUSTED_PROXY_CIDRS", "192.0.2.0/24, 2001:db8:1::/48")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load trusted proxy configuration: %v", err)
	}
	if len(cfg.TrustedProxyPrefixes) != 2 || cfg.TrustedProxyPrefixes[0].String() != "192.0.2.0/24" ||
		cfg.TrustedProxyPrefixes[1].String() != "2001:db8:1::/48" {
		t.Fatalf("trusted proxy prefixes = %#v", cfg.TrustedProxyPrefixes)
	}

	for _, invalid := range []string{"192.0.2.1/24", "192.0.2.0/24,,2001:db8::/32", "not-a-cidr"} {
		t.Run(invalid, func(t *testing.T) {
			setRequiredEnvironment(t)
			t.Setenv("TRUSTED_PROXY_CIDRS", invalid)
			if _, err := Load(); !errors.Is(err, ErrTrustedProxyCIDRsInvalid) {
				t.Fatalf("trusted proxy error = %v", err)
			}
		})
	}
}

func TestLoadAuthSinkRequiresLoopbackPublicBase(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("AUTH_MAIL_DRIVER", "sink")
	if _, err := Load(); !errors.Is(err, ErrAuthMailDriverInvalid) {
		t.Fatalf("non-loopback sink must fail: %v", err)
	}
	t.Setenv("PUBLIC_BASE_URL", "http://localhost:8080")
	cfg, err := Load()
	if err != nil || cfg.AuthMailDriver != "sink" {
		t.Fatalf("localhost sink configuration: driver=%q err=%v", cfg.AuthMailDriver, err)
	}
}

func TestLoadAuthResendConfiguration(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("AUTH_MAIL_DRIVER", "resend")
	t.Setenv("RESEND_API_KEY", "test-resend-key")
	t.Setenv("AUTH_MAIL_FROM", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load Resend configuration: %v", err)
	}
	if cfg.AuthMailDriver != "resend" || cfg.ResendAPIKey != "test-resend-key" ||
		cfg.AuthMailFrom != "Photographer CRM <onboarding@resend.dev>" {
		t.Fatal("Resend configuration mismatch")
	}
}

func TestLoadAuthResendRequiresAPIKey(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("AUTH_MAIL_DRIVER", "resend")
	t.Setenv("RESEND_API_KEY", "")
	if _, err := Load(); !errors.Is(err, ErrResendAPIKeyMissing) {
		t.Fatalf("missing Resend API key must fail fast: %v", err)
	}
}

func TestLoadAuthPublicConfigurationDefaultsFailClosed(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("AUTH_TOKEN_ISSUER", "")
	t.Setenv("AUTH_PUBLIC_REGISTRATION_ENABLED", "")
	t.Setenv("AUTH_MAIL_DRIVER", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load default auth configuration: %v", err)
	}
	if cfg.AuthPublicRegistrationEnabled || cfg.AuthTokenIssuer != "photographer-crm" || cfg.AuthMailDriver != "unavailable" {
		t.Fatalf("auth defaults must fail closed: enabled=%t issuer=%q mail=%q",
			cfg.AuthPublicRegistrationEnabled, cfg.AuthTokenIssuer, cfg.AuthMailDriver)
	}
}

func TestLoadAuthPublicConfigurationRejectsUnsafeValues(t *testing.T) {
	tests := []struct {
		name    string
		base    string
		enabled string
		want    error
	}{
		{name: "missing public base", want: ErrPublicBaseURLMissing},
		{name: "non-local HTTP", base: "http://app.example.invalid", want: ErrPublicBaseURLInvalid},
		{name: "userinfo", base: "https://name@app.example.invalid", want: ErrPublicBaseURLInvalid},
		{name: "path", base: "https://app.example.invalid/path", want: ErrPublicBaseURLInvalid},
		{name: "query", base: "https://app.example.invalid?x=1", want: ErrPublicBaseURLInvalid},
		{name: "fragment", base: "https://app.example.invalid#fragment", want: ErrPublicBaseURLInvalid},
		{name: "invalid registration bool", base: "https://app.example.invalid", enabled: "yes", want: ErrAuthPublicRegistrationInvalid},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			setRequiredEnvironment(t)
			t.Setenv("PUBLIC_BASE_URL", test.base)
			t.Setenv("AUTH_PUBLIC_REGISTRATION_ENABLED", test.enabled)
			_, err := Load()
			if !errors.Is(err, test.want) {
				t.Fatalf("want %v, got %v", test.want, err)
			}
		})
	}
}

func TestCanonicalPublicBaseURLAllowsExplicitLocalhostHTTP(t *testing.T) {
	for _, raw := range []string{"http://localhost:8080", "http://127.0.0.1:8080", "http://[::1]:8080"} {
		canonical, err := canonicalPublicBaseURL(raw)
		if err != nil || canonical != raw {
			t.Fatalf("canonical localhost URL: raw=%q canonical=%q err=%v", raw, canonical, err)
		}
	}
}

func TestCanonicalPublicBaseURLRejectsExplicitDefaultPorts(t *testing.T) {
	for _, raw := range []string{
		"https://app.example.invalid:443",
		"https://[2001:db8::1]:443",
		"http://localhost:80",
		"http://127.0.0.1:80",
		"http://[::1]:80",
	} {
		if _, err := canonicalPublicBaseURL(raw); !errors.Is(err, ErrPublicBaseURLInvalid) {
			t.Errorf("canonicalPublicBaseURL(%q) error = %v; want invalid", raw, err)
		}
	}
}

func TestCanonicalPublicBaseURLPreservesNonDefaultPortsAndIPv6(t *testing.T) {
	for _, raw := range []string{
		"https://app.example.invalid:8443",
		"https://[2001:db8::1]:8443",
		"http://localhost:8080",
		"http://[::1]:8080",
	} {
		canonical, err := canonicalPublicBaseURL(raw)
		if err != nil || canonical != raw {
			t.Errorf("canonicalPublicBaseURL(%q) = %q, %v", raw, canonical, err)
		}
	}
}
