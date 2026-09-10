package store_test

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// TestOrdersAttributionSnapshotMigrationBackfillsExistingOrders 锁住 0034 归因快照的一次性
// best-effort 回填与取值域约束：存量订单按当前客户渠道/套系类型回填（可能与真实下单时不一致，
// 仅作展示）；无套系订单 shoot_type_snapshot 为 NULL（矩阵「未归因」桶）；channel_snapshot
// 恒有值——直写 SQL 缺省列时归 'other'（仅兜底，生产写路径恒显式提供）。
func TestOrdersAttributionSnapshotMigrationBackfillsExistingOrders(t *testing.T) {
	url := startPostgres(t)
	if err := store.MigrateStepsForTest(url, 33); err != nil {
		t.Fatalf("migrate pre-attribution-snapshot schema: %v", err)
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatalf("open pre-attribution-snapshot database: %v", err)
	}
	ctx := context.Background()

	const account = "attr-acct"
	if _, err := db.ExecContext(ctx,
		`INSERT INTO accounts (id, password_hash) VALUES ($1, 'hash')`, account); err != nil {
		t.Fatalf("insert account: %v", err)
	}
	for _, customer := range []struct {
		id      string
		channel string
	}{{"cus-attr-a", "douyin"}, {"cus-attr-b", "weibo"}} {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO customers (id, account_id, display_name, channel)
			 VALUES ($1, $2, $3, $4)`, customer.id, account, customer.id, customer.channel); err != nil {
			t.Fatalf("insert customer %s: %v", customer.id, err)
		}
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO packages (id, account_id, name, shoot_type, pricing_mode, base_price, status)
		 VALUES ('pkg-attr', $1, 'Cosplay 套系', 'cosplay', 'fixed', 68000, 'active')`, account); err != nil {
		t.Fatalf("insert package: %v", err)
	}
	for _, order := range []struct {
		id        string
		customer  string
		pkg       any
		channel   string
		shootType sql.NullString
	}{
		{id: "ord-attr-pkg", customer: "cus-attr-a", pkg: "pkg-attr", channel: "douyin",
			shootType: sql.NullString{String: "cosplay", Valid: true}},
		{id: "ord-attr-nopkg", customer: "cus-attr-b", pkg: nil, channel: "weibo"},
	} {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO orders (id, account_id, customer_id, package_id, status)
			 VALUES ($1, $2, $3, $4, 'consulting')`,
			order.id, account, order.customer, order.pkg); err != nil {
			t.Fatalf("insert order %s: %v", order.id, err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close pre-attribution-snapshot database: %v", err)
	}

	if err := store.MigrateUp(url); err != nil {
		t.Fatalf("migrate attribution-snapshot schema: %v", err)
	}
	db, err = sql.Open("pgx", url)
	if err != nil {
		t.Fatalf("reopen migrated database: %v", err)
	}
	for _, order := range []struct {
		id        string
		channel   string
		shootType sql.NullString
	}{
		{id: "ord-attr-pkg", channel: "douyin", shootType: sql.NullString{String: "cosplay", Valid: true}},
		{id: "ord-attr-nopkg", channel: "weibo"},
	} {
		var channel string
		var shootType sql.NullString
		if err := db.QueryRowContext(ctx,
			`SELECT channel_snapshot, shoot_type_snapshot FROM orders WHERE id = $1`, order.id,
		).Scan(&channel, &shootType); err != nil {
			t.Fatalf("read backfilled order %s: %v", order.id, err)
		}
		if channel != order.channel {
			t.Fatalf("order %s: channel_snapshot = %q, want %q", order.id, channel, order.channel)
		}
		if shootType.Valid != order.shootType.Valid || shootType.String != order.shootType.String {
			t.Fatalf("order %s: shoot_type_snapshot = %v, want %v", order.id, shootType, order.shootType)
		}
	}

	// 取值域 CHECK：两列枚举与 customers.channel / packages.shoot_type 同步。
	if _, err := db.ExecContext(ctx,
		`UPDATE orders SET channel_snapshot = 'telegram' WHERE id = 'ord-attr-pkg'`); err == nil {
		t.Fatal("channel_snapshot 越界值必须违反列 CHECK")
	}
	if _, err := db.ExecContext(ctx,
		`UPDATE orders SET shoot_type_snapshot = 'wedding' WHERE id = 'ord-attr-pkg'`); err == nil {
		t.Fatal("shoot_type_snapshot 越界值必须违反列 CHECK")
	}
	// 直写 SQL 缺省列：channel_snapshot 归 'other'、shoot_type_snapshot 保持 NULL。
	if _, err := db.ExecContext(ctx,
		`INSERT INTO orders (id, account_id, customer_id, status)
		 VALUES ('ord-attr-default', $1, 'cus-attr-a', 'consulting')`, account); err != nil {
		t.Fatalf("insert order without snapshot columns: %v", err)
	}
	var defaultChannel string
	var defaultShootType sql.NullString
	if err := db.QueryRowContext(ctx,
		`SELECT channel_snapshot, shoot_type_snapshot FROM orders WHERE id = 'ord-attr-default'`,
	).Scan(&defaultChannel, &defaultShootType); err != nil {
		t.Fatalf("read default-snapshot order: %v", err)
	}
	if defaultChannel != "other" || defaultShootType.Valid {
		t.Fatalf("缺省快照应为 other/NULL: channel=%q shoot_type=%v", defaultChannel, defaultShootType)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close before down migration: %v", err)
	}

	// 0034 之上已叠加 0035（健康度参数）：先退 0035 再退 0034，本测试锁的是 0034 自身的可逆性。
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("rollback creative-foundation migration: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("rollback creative-workspace migration: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("rollback settings-health-tiers migration: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("rollback attribution-snapshot migration: %v", err)
	}
	db, err = sql.Open("pgx", url)
	if err != nil {
		t.Fatalf("reopen rolled-back database: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close rolled-back database: %v", err)
		}
	}()
	var columns int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM information_schema.columns
		 WHERE table_name = 'orders' AND column_name IN ('channel_snapshot', 'shoot_type_snapshot')`,
	).Scan(&columns); err != nil {
		t.Fatalf("inspect rolled-back columns: %v", err)
	}
	if columns != 0 {
		t.Fatalf("rolled-back orders still has %d attribution-snapshot columns", columns)
	}
}
