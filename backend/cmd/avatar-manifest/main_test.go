package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRunRejectsOSSDriverBeforeDatabaseOrLocalVolumeAccess(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://unused")
	t.Setenv("AUTH_TOKEN_SECRET", "test-secret")
	t.Setenv("PUBLIC_BASE_URL", "https://app.example.invalid")
	t.Setenv("AVATAR_STORAGE_DRIVER", "oss")
	t.Setenv("OSS_REGION", "cn-hangzhou")
	t.Setenv("OSS_BUCKET", "crm-objects")
	err := run(context.Background(), []string{"generate"}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "暂不支持 OSS") {
		t.Fatalf("OSS mode must fail closed before database/local access: %v", err)
	}
}
