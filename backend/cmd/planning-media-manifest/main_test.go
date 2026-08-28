package main

import (
	"context"
	"strings"
	"testing"
)

func TestRunRejectsOSSDriverBeforeOpeningLocalVolume(t *testing.T) {
	t.Setenv("AVATAR_STORAGE_DRIVER", "oss")
	err := run(context.Background(), []string{"generate"})
	if err == nil || !strings.Contains(err.Error(), "暂不支持 OSS") {
		t.Fatalf("OSS mode must fail closed before local volume access: %v", err)
	}
}
