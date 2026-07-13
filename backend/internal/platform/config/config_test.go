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
