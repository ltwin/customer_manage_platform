package immutablefs

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

// TestOSSConformanceRealBucket 对真实 bucket 跑完整契约套件。
// 需要环境变量：OSS_TEST_BUCKET、OSS_TEST_REGION，凭证走 OSS_ACCESS_KEY_ID/
// OSS_ACCESS_KEY_SECRET（或 ECS RAM Role）；可选 OSS_TEST_ENDPOINT（内网）。
// 未配置时跳过，CI 不依赖外网。
func TestOSSConformanceRealBucket(t *testing.T) {
	bucket := os.Getenv("OSS_TEST_BUCKET")
	region := os.Getenv("OSS_TEST_REGION")
	if bucket == "" || region == "" {
		t.Skip("未设置 OSS_TEST_BUCKET / OSS_TEST_REGION，跳过真实 bucket conformance")
	}
	store, err := NewOSS(OSSConfig{
		Region:   region,
		Endpoint: os.Getenv("OSS_TEST_ENDPOINT"),
		Bucket:   bucket,
		UseCName: os.Getenv("OSS_TEST_USE_CNAME") == "true",
	})
	if err != nil {
		t.Fatalf("NewOSS: %v", err)
	}
	if err := store.Probe(context.Background()); err != nil {
		t.Fatalf("Probe: %v", err)
	}
	prefix := fmt.Sprintf("immutablefs-conf/%d/", time.Now().UnixNano())
	t.Cleanup(func() {
		cursor := ""
		for {
			page, err := store.List(context.Background(), prefix, cursor, 1000)
			if err != nil {
				t.Errorf("list OSS cleanup prefix: %v", err)
				return
			}
			for _, item := range page.Items {
				if err := store.Delete(context.Background(), item.Key); err != nil {
					t.Errorf("cleanup OSS object %q: %v", item.Key, err)
				}
			}
			if page.Done {
				return
			}
			cursor = page.NextCursor
		}
	})
	RunConformance(t, func(t *testing.T) (ObjectStore, string) {
		return store, prefix
	})
}
