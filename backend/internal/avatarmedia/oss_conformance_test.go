package avatarmedia

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/immutablefs"
)

// TestOSSStoreConformanceRealBucket 对真实 bucket 跑 typed 头像契约套件。
// 需要环境变量：OSS_TEST_BUCKET、OSS_TEST_REGION，凭证走 OSS_ACCESS_KEY_ID/
// OSS_ACCESS_KEY_SECRET（或 ECS RAM Role）；可选 OSS_TEST_ENDPOINT（内网）。
func TestOSSStoreConformanceRealBucket(t *testing.T) {
	bucket := os.Getenv("OSS_TEST_BUCKET")
	region := os.Getenv("OSS_TEST_REGION")
	if bucket == "" || region == "" {
		t.Skip("未设置 OSS_TEST_BUCKET / OSS_TEST_REGION，跳过真实 bucket conformance")
	}
	base, err := immutablefs.NewOSS(immutablefs.OSSConfig{
		Region:   region,
		Endpoint: os.Getenv("OSS_TEST_ENDPOINT"),
		Bucket:   bucket,
		UseCName: os.Getenv("OSS_TEST_USE_CNAME") == "true",
	})
	if err != nil {
		t.Fatalf("NewOSS: %v", err)
	}
	if err := base.Probe(context.Background()); err != nil {
		t.Fatalf("Probe: %v", err)
	}
	store, err := NewOSSStore(base)
	if err != nil {
		t.Fatalf("NewOSSStore: %v", err)
	}
	// 共享 bucket 下 store 全局唯一，但每个子测试必须用独立 accountID 命名空间，
	// 否则先前子测试的遗留键会让计数型断言失败。
	runPrefix := fmt.Sprintf("conf%d", time.Now().UnixNano())
	var seq int
	RunConformance(t, func(t *testing.T) (ObjectStore, string) {
		seq++
		return store, fmt.Sprintf("%s%04d", runPrefix, seq)
	})
	t.Cleanup(func() {
		items, err := store.Inventory(context.Background())
		if err != nil {
			return
		}
		for _, item := range items {
			parsed, err := ParseKey(item.Key.String())
			if err == nil && strings.HasPrefix(parsed.AccountID, runPrefix) {
				if err := store.Delete(context.Background(), item.Key); err != nil {
					t.Errorf("cleanup avatar OSS object %q: %v", item.Key.String(), err)
				}
			}
		}
	})
}
