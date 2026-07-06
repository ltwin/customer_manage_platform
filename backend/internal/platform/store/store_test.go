package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// startPostgres 起一个一次性 PG 容器并返回连接串（测试与 dev 库互不干扰，design D5）。
func startPostgres(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	ctr, err := tcpostgres.Run(ctx, "postgres:17-alpine",
		tcpostgres.WithDatabase("crm_test"),
		tcpostgres.WithUsername("crm_test"),
		tcpostgres.WithPassword("crm_test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(60*time.Second)),
	)
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(ctr); err != nil {
			t.Logf("terminate postgres container: %v", err)
		}
	})
	url, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("container connection string: %v", err)
	}
	return url
}

// openMigrated 对空库执行 migrate up 并打开 Store。
func openMigrated(t *testing.T, url string) *store.Store {
	t.Helper()
	if err := store.MigrateUp(url); err != nil {
		t.Fatalf("migrate up on empty database: %v", err)
	}
	s, err := store.Open(context.Background(), url)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(s.Close)
	return s
}

// A10：空库 migrate up 成功，且迁移幂等（重复执行零错误）。
func TestMigrateUpOnEmptyDatabase(t *testing.T) {
	url := startPostgres(t)
	s := openMigrated(t, url)

	if err := store.MigrateUp(url); err != nil {
		t.Fatalf("second migrate up should be no-op, got: %v", err)
	}
	n, err := s.AccountCount(context.Background())
	if err != nil {
		t.Fatalf("accounts table should exist after migrate: %v", err)
	}
	if n != 0 {
		t.Fatalf("fresh database should have 0 accounts, got %d", n)
	}
}

// A10：连续「启动」两次（migrate + ensure），accounts 有且仅有 1 条默认账号。
func TestEnsureDefaultAccountIdempotent(t *testing.T) {
	url := startPostgres(t)
	s := openMigrated(t, url)
	ctx := context.Background()

	created, err := auth.EnsureDefaultAccount(ctx, s, "first-boot-password")
	if err != nil {
		t.Fatalf("first ensure: %v", err)
	}
	if !created {
		t.Fatal("first ensure on empty accounts should create the default account")
	}

	created, err = auth.EnsureDefaultAccount(ctx, s, "first-boot-password")
	if err != nil {
		t.Fatalf("second ensure: %v", err)
	}
	if created {
		t.Fatal("second ensure should be a no-op")
	}

	n, err := s.AccountCount(ctx)
	if err != nil {
		t.Fatalf("count accounts: %v", err)
	}
	if n != 1 {
		t.Fatalf("accounts should have exactly 1 row after two boots, got %d", n)
	}
}

// A10：空库且缺 SEED_ADMIN_PASSWORD → 启动 fail-fast 且错误信息明确。
func TestEnsureDefaultAccountMissingSeedPassword(t *testing.T) {
	url := startPostgres(t)
	s := openMigrated(t, url)

	_, err := auth.EnsureDefaultAccount(context.Background(), s, "")
	if err == nil {
		t.Fatal("empty accounts without seed password must fail fast")
	}
	if !errors.Is(err, auth.ErrSeedPasswordMissing) {
		t.Fatalf("want ErrSeedPasswordMissing, got: %v", err)
	}
}
