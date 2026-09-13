package store_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store/storetest"
)

func TestCustomerAvatarMigrationUpDownAndConstraints(t *testing.T) {
	url := startPostgres(t)
	if err := store.MigrateUp(url); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `INSERT INTO accounts (id, password_hash) VALUES ('acc_avatar', 'hash')`); err != nil {
		t.Fatalf("insert account: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO customers (id, account_id, display_name, channel) VALUES ('cus_avatar', 'acc_avatar', '头像客户', 'other')`); err != nil {
		t.Fatalf("insert customer: %v", err)
	}
	var revision int64
	if err := db.QueryRowContext(ctx, `SELECT avatar_revision FROM customers WHERE id = 'cus_avatar'`).Scan(&revision); err != nil {
		t.Fatalf("read initial revision: %v", err)
	}
	if revision != 0 {
		t.Fatalf("initial avatar revision: want 0, got %d", revision)
	}

	version := "sha256-" + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	objectID := "0123456789abcdef0123456789abcdef"
	if err := db.QueryRowContext(ctx, `
		UPDATE customers
		SET avatar_revision = avatar_revision + 1,
		    avatar_version = $1,
		    avatar_object_id = $2,
		    avatar_media_type = 'image/jpeg',
		    avatar_size = 123,
		    avatar_updated_at = now()
		WHERE id = 'cus_avatar'
		RETURNING avatar_revision`, version, objectID).Scan(&revision); err != nil {
		t.Fatalf("write complete pointer: %v", err)
	}
	if revision != 1 {
		t.Fatalf("incremented avatar revision: want 1, got %d", revision)
	}
	if _, err := db.ExecContext(ctx, `UPDATE customers SET avatar_object_id = NULL WHERE id = 'cus_avatar'`); err == nil {
		t.Fatal("partial avatar pointer should violate completeness constraint")
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO avatar_object_gc (
			account_id, customer_id, avatar_version, avatar_object_id, not_before, next_attempt_at
		) VALUES ('acc_avatar', 'cus_avatar', $1, $2, now(), now())`, version, objectID); err != nil {
		t.Fatalf("insert GC row: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO avatar_object_gc (
			account_id, customer_id, avatar_version, avatar_object_id, not_before, next_attempt_at
		) VALUES ('acc_avatar', 'cus_avatar', $1, $2, now(), now())`, version, objectID); err == nil {
		t.Fatal("duplicate account/customer/object_id GC row should fail")
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO avatar_reconciliation_checkpoint (account_id, object_inventory_cycle, pointer_cycle)
		VALUES ('acc_avatar', -1, 0)`); err == nil {
		t.Fatal("negative reconciliation cycle should fail")
	}

	if err := db.Close(); err != nil {
		t.Fatalf("close before down migration: %v", err)
	}
	// avatar 是 0008，需连续回滚 0020-0016 planning / security migrations，再回滚账号与基础迁移。
	for i, label := range []string{
		"llm-gateway",
		"creative-canvas-events",
		"creative-media",
		"creative-canvas-commands",
		"creative-library-organization",
		"creative-text-canvas",
		"creative-foundation",
		"creative-workspace",
		"settings-health-tiers",
		"orders-attribution-snapshot",
		"orders-payment-facts",
		"orders-delivery-due",
		"media-gallery-rights-bindings",
		"plan-list-enrichment",
		"plan-business-feedback",
		"plan-assignment-reminder-digest-intent", "plan-assignment-reminder-temporal", "plan-assignment-reminder-ingestion", "plan-assignment-reminders",
		"share-assignments", "share-feedbacks", "share-anonymous", "share-generations",
		"security-attempt-budget", "plan-crm", "plan-ingestion", "planning-media", "shoot-planning",
		"account-profiles", "settings-availability", "auth-attempt-limiter", "account-auth",
		"telegram", "reminders", "settings", "avatar",
	} {
		if err := store.MigrateDownOneForTest(url); err != nil {
			t.Fatalf("migrate down one (%s step %d): %v", label, i+1, err)
		}
	}
	db, err = sql.Open("pgx", url)
	if err != nil {
		t.Fatalf("reopen after down: %v", err)
	}
	defer func() { _ = db.Close() }()
	var avatarColumns int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*) FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'customers' AND column_name LIKE 'avatar_%'`).Scan(&avatarColumns); err != nil {
		t.Fatalf("inspect down migration: %v", err)
	}
	if avatarColumns != 0 {
		t.Fatalf("down migration left %d avatar customer columns", avatarColumns)
	}
}

func TestMain(m *testing.M) { storetest.Main(m, store.MigrateUp) }

// startPostgres 返回一个空库的连接串（测试与 dev 库互不干扰，design D5）。
// 本包大量测试用 MigrateStepsForTest 从零推进到指定步数，故取空库而非
// storetest.NewURL 的已迁移克隆。库之间相互隔离，容器整包共享。
func startPostgres(t *testing.T) string {
	t.Helper()
	return storetest.NewRawURL(t)
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
