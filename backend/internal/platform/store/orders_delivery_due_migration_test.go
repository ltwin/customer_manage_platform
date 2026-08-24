package store_test

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// TestOrdersDeliveryDueMigrationBackfillsExistingOrders 锁住 0032 的一次性 backfill：
// 应交付日按各账号自身时区落本地自然日，缺 settings 行回落 Asia/Shanghai + 14，
// cancelled 与无 shot_at 的订单不回填。
func TestOrdersDeliveryDueMigrationBackfillsExistingOrders(t *testing.T) {
	url := startPostgres(t)
	if err := store.MigrateStepsForTest(url, 31); err != nil {
		t.Fatalf("migrate pre-delivery-due schema: %v", err)
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatalf("open pre-delivery-due database: %v", err)
	}
	ctx := context.Background()

	// 两个账号：东京（+09）、洛杉矶（-07/-08）；另有无 settings 行账号（回落上海）在下方单独插入。
	// 注意：backfill 对存量 settings 行恒见默认 14——delivery_sla_days 列在本迁移内以 DEFAULT 14
	// 创建，不存在「存量行带非默认 SLA 参与回填」的路径；COALESCE 只兜 LEFT JOIN 未命中的
	// 无 settings 行账号（due-nosettings 已覆盖）。
	for _, account := range []struct {
		id       string
		timezone string
	}{
		{id: "due-tokyo", timezone: "Asia/Tokyo"},
		{id: "due-la", timezone: "America/Los_Angeles"},
	} {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO accounts (id, password_hash) VALUES ($1, 'hash')`, account.id); err != nil {
			t.Fatalf("insert account %s: %v", account.id, err)
		}
		if _, err := db.ExecContext(ctx,
			`INSERT INTO settings (account_id, timezone) VALUES ($1, $2)`,
			account.id, account.timezone); err != nil {
			t.Fatalf("insert settings %s: %v", account.id, err)
		}
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO accounts (id, password_hash) VALUES ('due-nosettings', 'hash')`); err != nil {
		t.Fatalf("insert account without settings: %v", err)
	}

	for _, account := range []string{"due-tokyo", "due-la", "due-nosettings"} {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO customers (id, account_id, display_name, channel)
			 VALUES ($1, $2, 'legacy', 'other')`,
			"cus-"+account, account); err != nil {
			t.Fatalf("insert customer %s: %v", account, err)
		}
	}

	// 2026-07-06T16:00:00Z：东京为 07-07 凌晨 01:00，洛杉矶为 07-06 上午 09:00。
	// 两地本地自然日相差一天，backfill 必须各按各的时区落日期。
	const shotAt = "2026-07-06T16:00:00Z"
	orders := []struct {
		id       string
		account  string
		status   string
		shotAt   any
		expected string // 期望 delivery_due_at；空串表示应为 NULL
	}{
		{id: "ord-tokyo", account: "due-tokyo", status: "shot", shotAt: shotAt, expected: "2026-07-21"},
		{id: "ord-la", account: "due-la", status: "shot", shotAt: shotAt, expected: "2026-07-20"},
		{id: "ord-nosettings", account: "due-nosettings", status: "shot", shotAt: shotAt, expected: "2026-07-21"},
		{id: "ord-closed", account: "due-tokyo", status: "closed", shotAt: shotAt, expected: "2026-07-21"},
		{id: "ord-cancelled", account: "due-tokyo", status: "cancelled", shotAt: shotAt, expected: ""},
		{id: "ord-consulting", account: "due-tokyo", status: "consulting", shotAt: nil, expected: ""},
	}
	for _, order := range orders {
		deliveredAt := any(nil)
		balancePaid := false
		if order.status == "closed" {
			deliveredAt = shotAt
			balancePaid = true
		}
		if _, err := db.ExecContext(ctx,
			`INSERT INTO orders (id, account_id, customer_id, status, shot_at, delivered_at, balance_paid)
			 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			order.id, order.account, "cus-"+order.account, order.status,
			order.shotAt, deliveredAt, balancePaid); err != nil {
			t.Fatalf("insert order %s: %v", order.id, err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close pre-delivery-due database: %v", err)
	}

	if err := store.MigrateUp(url); err != nil {
		t.Fatalf("migrate delivery-due schema: %v", err)
	}
	db, err = sql.Open("pgx", url)
	if err != nil {
		t.Fatalf("reopen migrated database: %v", err)
	}
	for _, order := range orders {
		var due sql.NullString
		var isOverride bool
		if err := db.QueryRowContext(ctx,
			`SELECT to_char(delivery_due_at, 'YYYY-MM-DD'), delivery_due_is_override
			 FROM orders WHERE id = $1`, order.id).Scan(&due, &isOverride); err != nil {
			t.Fatalf("read backfilled order %s: %v", order.id, err)
		}
		if order.expected == "" {
			if due.Valid {
				t.Fatalf("order %s: delivery_due_at = %s, want NULL", order.id, due.String)
			}
		} else {
			if !due.Valid || due.String != order.expected {
				t.Fatalf("order %s: delivery_due_at = %v, want %s", order.id, due, order.expected)
			}
		}
		if isOverride {
			t.Fatalf("order %s: backfill 必须标记为自动派生而非订单级覆盖", order.id)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close before down migration: %v", err)
	}

	// 0032 之上已叠加 0033（支付事实）：先退 0033 再退 0032，本测试锁的是 0032 自身的可逆性。
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("rollback payment-facts migration: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("rollback delivery-due migration: %v", err)
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
		 WHERE table_name = 'orders' AND column_name IN ('delivery_due_at', 'delivery_due_is_override')`,
	).Scan(&columns); err != nil {
		t.Fatalf("inspect rolled-back columns: %v", err)
	}
	if columns != 0 {
		t.Fatalf("rolled-back orders still has %d delivery-due columns", columns)
	}
	var slaColumns int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM information_schema.columns
		 WHERE table_name = 'settings' AND column_name = 'delivery_sla_days'`,
	).Scan(&slaColumns); err != nil {
		t.Fatalf("inspect rolled-back settings column: %v", err)
	}
	if slaColumns != 0 {
		t.Fatalf("rolled-back settings still has delivery_sla_days")
	}
}
