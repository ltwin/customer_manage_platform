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
		{name: "unknown driver", mutate: func(t *testing.T) { t.Setenv("AVATAR_STORAGE_DRIVER", "oss") }, want: ErrAvatarStorageDriverInvalid},
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
